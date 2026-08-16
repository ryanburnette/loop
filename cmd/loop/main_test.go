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
	run("commit", "-qm", "init")
}

// newFixtureLoop builds a throwaway git repo with a minimal manifest-mode
// loop dir wired to fake-pi, and returns the loop dir path.
func newFixtureLoop(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "myloop")
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
	return dir
}

func TestVersion(t *testing.T) {
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
	var out, errb bytes.Buffer
	code := mainErr([]string{"help"}, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	s := out.String()
	for _, want := range []string{"run", "status", "freeze", "frozen?", "help", "version"} {
		if !strings.Contains(s, want) {
			t.Fatalf("help missing subcommand %q:\n%s", want, s)
		}
	}
}

func TestRunFlagsBeforeAndAfterDir(t *testing.T) {
	dir := newFixtureLoop(t)
	fp := fakePI(t)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "--pi", fp, "-q", dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("flag-before-dir: code=%d stderr=%s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code = mainErr([]string{"run", dir, "--pi", fp, "-q"}, &out, &errb)
	if code != 0 {
		t.Fatalf("flag-after-dir: code=%d stderr=%s", code, errb.String())
	}
}

func TestRunRejectsExtraPositional(t *testing.T) {
	dir := newFixtureLoop(t)
	fp := fakePI(t)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", dir, "--pi", fp, "bogus-extra-arg"}, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "unexpected arguments") {
		t.Fatalf("stderr=%q, want it to name the extra argument", errb.String())
	}
}

func TestRunUnknownFlagErrors(t *testing.T) {
	dir := newFixtureLoop(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", dir, "--this-flag-does-not-exist"}, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d want 2, stderr=%s", code, errb.String())
	}
}

func TestRunMissingDirNamesTheProblem(t *testing.T) {
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "/definitely/does/not/exist/anywhere"}, &out, &errb)
	if code == 0 {
		t.Fatal("missing dir should not succeed")
	}
	if !strings.Contains(errb.String(), "does/not/exist") {
		t.Fatalf("stderr should name the missing path, got %q", errb.String())
	}
}

func TestStatusAfterRun(t *testing.T) {
	dir := newFixtureLoop(t)
	fp := fakePI(t)

	var out, errb bytes.Buffer
	if code := mainErr([]string{"run", dir, "--pi", fp, "-q"}, &out, &errb); code != 0 {
		t.Fatalf("run: code=%d stderr=%s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := mainErr([]string{"status", dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("status: code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "SUCCESS=1") {
		t.Fatalf("status output missing SUCCESS=1:\n%s", out.String())
	}
}

func TestFrozenSubcommandNamesTheDriftedPattern(t *testing.T) {
	root := t.TempDir()
	frozenFile := filepath.Join(root, "guard_test.go")
	if err := os.WriteFile(frozenFile, []byte("package guard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	state := filepath.Join(root, "state", "runid")
	// Reuse the public freeze package the same way run.go does, via the
	// loop binary's own "freeze <dir>" command, to snapshot *_test.go.
	dir := filepath.Join(root, "myloop")
	if err := os.MkdirAll(filepath.Join(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop.env"), []byte("LOOP_FREEZE=*_test.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := mainErr([]string{"freeze", dir}, &out, &errb); code != 0 {
		t.Fatalf("freeze: code=%d stderr=%s", code, errb.String())
	}

	// Drift the frozen file.
	if err := os.WriteFile(frozenFile, []byte("package guard\n// drifted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOOP_STATE_DIR", filepath.Join(dir, "state", ".freeze-tmp"))
	t.Setenv("LOOP_WORKROOT", root)
	_ = state // reserved for clarity; actual state path is the freeze-tmp dir above

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
// above: `loop freeze <dir>` prints where it wrote the snapshot, but not how
// to actually use it. A user has to already know frozen? needs
// LOOP_STATE_DIR and LOOP_WORKROOT set to the right paths.
func TestFreezePrintsHowToCheckIt(t *testing.T) {
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
	if code := mainErr([]string{"freeze", dir}, &out, &errb); code != 0 {
		t.Fatalf("freeze: code=%d stderr=%s", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "LOOP_STATE_DIR=") || !strings.Contains(s, "LOOP_WORKROOT=") ||
		!strings.Contains(s, "frozen?") {
		t.Fatalf("freeze output should print the exact frozen? invocation, got:\n%s", s)
	}
}
