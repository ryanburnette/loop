package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// clearLoopEnv drops ambient LOOP_* so these tests stay honest when a
// parent loop (e.g. covloop, which sets LOOP_BRANCH=1 for itself) exports
// those vars into this test binary's environment via the gate script.
func clearLoopEnv(t *testing.T) {
	t.Helper()
	for _, e := range os.Environ() {
		k, _, ok := strings.Cut(e, "=")
		if !ok || !strings.HasPrefix(k, "LOOP_") {
			continue
		}
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func fakePI(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "testdata", "fake-pi")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fake-pi missing at %s: %v", p, err)
	}
	return p
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("add", ".")
	run("commit", "-qm", "init", "--allow-empty")
}

// newFixtureLoop builds a throwaway git repo with a minimal manifest-mode
// loop dir wired to fake-pi, at reldir (e.g. "myloop" or ".loop") under the
// repo root, and returns (repoRoot, loopDirAbsPath).
func newFixtureLoop(t *testing.T, reldir string) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, reldir)
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "gates"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"loop.env":          "LOOP_MAX_ITER=2\nLOOP_SESSION=none\nLOOP_BRANCH=0\n",
		"manifest":          "turn writer prompts/writer.md\ngate tests gates/tests.sh\n",
		"prompts/writer.md": "irrelevant, fake-pi drives this\n",
		"gates/tests.sh":    "#!/bin/sh\nexit 0\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "gates", "tests.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	return root, dir
}

func TestVersion(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"version"}, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if strings.TrimSpace(out.String()) != version {
		t.Fatalf("version output=%q want %q", out.String(), version)
	}
}

func TestHelpListsAllSubcommands(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"help"}, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	s := out.String()
	for _, want := range []string{"run", "status", "freeze", "frozen?", "init", "help", "version"} {
		if !strings.Contains(s, want) {
			t.Fatalf("help missing subcommand %q:\n%s", want, s)
		}
	}
}

func TestRunFlagsAnyOrderWithDashC(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	fp := fakePI(t)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "--pi", fp, "-q", "-C", dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("-C last: code=%d stderr=%s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code = mainErr([]string{"run", "-C", dir, "--pi", fp, "-q"}, &out, &errb)
	if code != 0 {
		t.Fatalf("-C first: code=%d stderr=%s", code, errb.String())
	}
}

func TestRunRejectsExtraPositional(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	fp := fakePI(t)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "-C", dir, "--pi", fp, "bogus-extra-arg"}, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "unexpected arguments") {
		t.Fatalf("stderr=%q, want it to name the extra argument", errb.String())
	}
}

func TestRunUnknownFlagErrors(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "-C", dir, "--this-flag-does-not-exist"}, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d want 2, stderr=%s", code, errb.String())
	}
}

func TestRunMissingLoopDirNamesItAndSuggestsInit(t *testing.T) {
	clearLoopEnv(t)
	missing := filepath.Join(t.TempDir(), "nope")
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "-C", missing}, &out, &errb)
	if code == 0 {
		t.Fatal("missing loop dir should not succeed")
	}
	if !strings.Contains(errb.String(), missing) {
		t.Fatalf("stderr should name the missing path, got %q", errb.String())
	}
	if !strings.Contains(errb.String(), "loop init") {
		t.Fatalf("stderr should suggest loop init, got %q", errb.String())
	}
}

func TestRunUsesCWDDotLoopByDefault(t *testing.T) {
	clearLoopEnv(t)
	root, _ := newFixtureLoop(t, ".loop")
	fp := fakePI(t)
	t.Chdir(root)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "--pi", fp, "-q"}, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".loop", "state", "CURRENT_ID")); err != nil {
		t.Fatalf("expected .loop/state/CURRENT_ID to exist: %v", err)
	}
}

func TestStatusAfterRun(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	fp := fakePI(t)

	var out, errb bytes.Buffer
	if code := mainErr([]string{"run", "-C", dir, "--pi", fp, "-q"}, &out, &errb); code != 0 {
		t.Fatalf("run: code=%d stderr=%s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := mainErr([]string{"status", "-C", dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("status: code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "SUCCESS=1") {
		t.Fatalf("status output missing SUCCESS=1:\n%s", out.String())
	}
}

func TestStatusMissingLoopDirMentionsInit(t *testing.T) {
	clearLoopEnv(t)
	missing := filepath.Join(t.TempDir(), "nope")
	var out, errb bytes.Buffer
	code := mainErr([]string{"status", "-C", missing}, &out, &errb)
	if code == 0 {
		t.Fatal("missing loop dir should not succeed")
	}
	if !strings.Contains(errb.String(), "loop init") {
		t.Fatalf("stderr should suggest loop init, got %q", errb.String())
	}
}

func TestFrozenSubcommandNamesTheDriftedPattern(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	frozenFile := filepath.Join(root, "guard_test.go")
	if err := os.WriteFile(frozenFile, []byte("package guard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	dir := filepath.Join(root, "myloop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop.env"), []byte("LOOP_FREEZE=*_test.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := mainErr([]string{"freeze", "-C", dir}, &out, &errb); code != 0 {
		t.Fatalf("freeze: code=%d stderr=%s", code, errb.String())
	}

	// Drift the frozen file.
	if err := os.WriteFile(frozenFile, []byte("package guard\n// drifted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOOP_STATE_DIR", filepath.Join(dir, "state", ".freeze-tmp"))
	t.Setenv("LOOP_WORKROOT", root)

	out.Reset()
	errb.Reset()
	code := mainErr([]string{"frozen?"}, &out, &errb)
	if code != 1 {
		t.Fatalf("frozen?: code=%d want 1, stdout=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "*_test.go") {
		t.Fatalf("frozen? output should name the drifted pattern, got %q", out.String())
	}
	if strings.TrimSpace(out.String()) == "drift" {
		t.Fatal("frozen? regressed to the bare \"drift\" message")
	}
}

// TestFreezePrintsHowToCheckIt is a real gap found while writing the test
// above: `loop freeze` prints where it wrote the snapshot, but not how to
// actually use it. A user has to already know frozen? needs
// LOOP_STATE_DIR and LOOP_WORKROOT set to the right paths.
func TestFreezePrintsHowToCheckIt(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a_test.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	dir := filepath.Join(root, "myloop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop.env"), []byte("LOOP_FREEZE=*_test.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := mainErr([]string{"freeze", "-C", dir}, &out, &errb); code != 0 {
		t.Fatalf("freeze: code=%d stderr=%s", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "LOOP_STATE_DIR=") || !strings.Contains(s, "LOOP_WORKROOT=") ||
		!strings.Contains(s, "frozen?") {
		t.Fatalf("freeze output should print the exact frozen? invocation, got:\n%s", s)
	}
}

func TestInitScaffoldsDefaultTemplate(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	gitInit(t, root)
	t.Chdir(root)

	var out, errb bytes.Buffer
	code := mainErr([]string{"init"}, &out, &errb)
	if code != 0 {
		t.Fatalf("init: code=%d stderr=%s", code, errb.String())
	}
	for _, rel := range []string{"loop.env", "gates"} {
		if _, err := os.Stat(filepath.Join(root, ".loop", rel)); err != nil {
			t.Fatalf(".loop/%s missing: %v", rel, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".loop", "prompts"))
	if err != nil || len(entries) == 0 {
		t.Fatalf(".loop/prompts should have at least one file: %v", err)
	}
}

func TestInitWithNamedTemplate(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	gitInit(t, root)

	var out, errb bytes.Buffer
	code := mainErr([]string{"init", "two-model-critique", "-C", filepath.Join(root, ".loop")}, &out, &errb)
	if code != 0 {
		t.Fatalf("init: code=%d stderr=%s", code, errb.String())
	}
	b, err := os.ReadFile(filepath.Join(root, ".loop", "loop.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "LOOP_REVIEWER_MODEL") {
		t.Fatalf("two-model-critique template should mention LOOP_REVIEWER_MODEL, got:\n%s", b)
	}
}

func TestInitRefusesToOverwriteExistingLoopDir(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	gitInit(t, root)
	t.Chdir(root)

	var out, errb bytes.Buffer
	if code := mainErr([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("first init: code=%d stderr=%s", code, errb.String())
	}
	// Mark it so we can tell if a second init clobbered it.
	marker := filepath.Join(root, ".loop", "loop.env")
	if err := os.WriteFile(marker, []byte("LOOP_MAX_ITER=999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errb.Reset()
	code := mainErr([]string{"init"}, &out, &errb)
	if code == 0 {
		t.Fatal("second init should refuse to overwrite an existing .loop/")
	}
	b, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(b), "999") {
		t.Fatalf("existing .loop/loop.env should be untouched, got %q (err=%v)", b, err)
	}
}

func TestNoColorDisablesANSI(t *testing.T) {
	clearLoopEnv(t)
	t.Setenv("NO_COLOR", "1")
	_, dir := newFixtureLoop(t, "myloop")
	fp := fakePI(t)

	var out, errb bytes.Buffer
	if code := mainErr([]string{"run", "-C", dir, "--pi", fp}, &out, &errb); code != 0 {
		t.Fatalf("run: code=%d stderr=%s", code, errb.String())
	}
	if strings.ContainsRune(out.String(), '\x1b') {
		t.Fatalf("NO_COLOR=1 should produce zero ESC bytes, got:\n%q", out.String())
	}
}
