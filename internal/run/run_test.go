package run

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ryanburnette/loop/internal/config"
	"github.com/ryanburnette/loop/internal/ui"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
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

func TestUntilGreenSucceeds(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	// Copy the fixture loop into a throwaway git repo so workroot resolves.
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	code, err := Run(Options{
		Dir:   dst,
		Pi:    filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d want 0", code)
	}
}

func TestCompactionFailPolicy(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	t.Setenv("FAKE_PI_COMPACT", "1")
	code, err := Run(Options{
		Dir:     dst,
		Pi:      filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet:   true,
		Compact: "fail",
		MaxIter: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("compact=fail should not succeed when fake-pi compacts")
	}
}

func TestHandoffReadsGoalFromLoopDir(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Goal and constraints are part of the recipe, so they live in the loop
	// dir alongside loop.env — everything needed to set up a loop is in one
	// directory. Decoys at the workroot root must be ignored.
	if err := os.WriteFile(filepath.Join(dst, "TODO.md"), []byte("fix the csv loader\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "CONSTRAINTS.md"), []byte("- do not edit tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "TODO.md"), []byte("stale root goal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	code, err := Run(Options{
		Dir:   dst,
		Pi:    filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}

	// Find the run's handoff.
	matches, err := filepath.Glob(filepath.Join(dst, "state", "*", "handoff.md"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no handoff.md written: %v %v", matches, err)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "fix the csv loader") {
		t.Fatalf("handoff missing the loop dir's TODO.md goal:\n%s", s)
	}
	if !strings.Contains(s, "do not edit tests") {
		t.Fatalf("handoff missing the loop dir's CONSTRAINTS.md:\n%s", s)
	}
	if strings.Contains(s, "stale root goal") {
		t.Fatalf("handoff picked up a TODO.md outside the loop dir:\n%s", s)
	}
}

func TestWritesStatusFile(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	code, err := Run(Options{
		Dir:   dst,
		Pi:    filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	matches, err := filepath.Glob(filepath.Join(dst, "state", "*", "status"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no status file written: %v %v", matches, err)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "success") && !strings.Contains(string(b), "iteration") {
		t.Fatalf("status file empty or unhelpful: %q", b)
	}
}

func TestResumeDoesNotRefreeze(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A frozen file we will edit between start and resume.
	frozen := filepath.Join(root, "keep_test.go")
	if err := os.WriteFile(frozen, []byte("package keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Enable freeze via the fixture loop.env and require the built-in gate.
	envPath := filepath.Join(dst, "loop.env")
	b, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, append(b, []byte("\nLOOP_FREEZE=*_test.go\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	manPath := filepath.Join(dst, "manifest")
	mb, err := os.ReadFile(manPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manPath, append(mb, []byte("\ngate frozen loop:frozen\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	code, err := Run(Options{
		Dir:     dst,
		Pi:      filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet:   true,
		MaxIter: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("first run exit %d", code)
	}

	idb, err := os.ReadFile(filepath.Join(dst, "state", "CURRENT_ID"))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(idb))

	sumPath := filepath.Join(dst, "state", id, "frozen", "1.sum")
	indexPath := filepath.Join(dst, "state", id, "frozen", "index")
	sumBefore, err := os.ReadFile(sumPath)
	if err != nil {
		t.Fatal(err)
	}
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	// Edit the frozen file after the original snapshot.
	if err := os.WriteFile(frozen, []byte("package keep\n// drifted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Resume must compare against the original snapshot, not re-hash now.
	code, err = Run(Options{
		Dir:      dst,
		Pi:       filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet:    true,
		MaxIter:  2,
		ResumeID: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("resume should fail frozen gate after drift; re-freeze on resume is a bug")
	}
	sumAfter, err := os.ReadFile(sumPath)
	if err != nil {
		t.Fatal(err)
	}
	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sumBefore, sumAfter) || !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("resume rewrote freeze hashes")
	}
}

func TestFrozenGateRejectsRewrittenSum(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	weakened := []byte("package a\n// weakened\n")
	sum := sha256.Sum256(weakened)
	hash := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(root, "a_test.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "myloop")
	if err := os.MkdirAll(filepath.Join(dst, "gates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "loop.env"), []byte("LOOP_MAX_ITER=1\nLOOP_BRANCH=0\nLOOP_FREEZE=*_test.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Weaken the frozen file and rewrite 1.sum to match, so a check that
	// re-reads the sum would pass.
	script := fmt.Sprintf(`#!/bin/sh
set -eu
printf 'package a\n// weakened\n' > "$LOOP_WORKROOT/a_test.go"
printf '%s  a_test.go\n' > "$LOOP_STATE_DIR/frozen/1.sum"
`, hash)
	if err := os.WriteFile(filepath.Join(dst, "gates", "tamper.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "manifest"), []byte("gate tamper gates/tamper.sh\ngate frozen loop:frozen\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	code, err := Run(Options{
		Dir:     dst,
		Pi:      filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet:   true,
		MaxIter: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("rewritten freeze sum should fail loop:frozen")
	}
	idb, err := os.ReadFile(filepath.Join(dst, "state", "CURRENT_ID"))
	if err != nil {
		t.Fatal(err)
	}
	logb, err := os.ReadFile(filepath.Join(dst, "state", strings.TrimSpace(string(idb)), "gate-log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logb), "freeze store modified") {
		t.Fatalf("gate log:\n%s", logb)
	}
}

// A sessionful turn started from a cwd that is not the workroot must not
// leave a second *_<id>.jsonl. The probe opens the file the turn wrote.
func TestProbeDoesNotCreateSecondSessionFromOtherCwd(t *testing.T) {
	_, loopDir := scratchLoop(t,
		"turn writer prompts/w.md\ngate ok gates/ok.sh\n",
		map[string]string{
			"loop.env":     "LOOP_MAX_ITER=1\nLOOP_SESSION=shared\nLOOP_BRANCH=0\nLOOP_SESSION_TURNS=8\n",
			"prompts/w.md": "go\n",
			"gates/ok.sh":  "#!/bin/sh\nexit 0\n",
		})
	t.Setenv("FAKE_PI_PERCENT", "32")
	piPath, logPath := wrapFakePi(t)
	away := t.TempDir()
	t.Chdir(away)

	var errBuf bytes.Buffer
	code, err := Run(Options{Dir: loopDir, Pi: piPath, Quiet: true, Err: &errBuf})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, errBuf.String())
	}
	files := sessionJSONLs(t, loopDir)
	if len(files) != 1 {
		t.Fatalf("want the one jsonl the turn wrote, got %v", files)
	}
	workroot := gitTop(t, loopDir)
	if strings.Contains(headerLine(t, files[0]), `"cwd":"`+away+`"`) {
		t.Fatalf("session file belongs to the outside cwd: %s", headerLine(t, files[0]))
	}
	inv := piInvocations(t, logPath)
	var probes int
	for _, rec := range inv {
		if len(rec) == 0 {
			continue
		}
		args := rec[1:]
		if hasArg(args, "--fork") {
			t.Fatalf("runner set a fork flag: %v", args)
		}
		if !modeIs(args, "rpc") {
			continue
		}
		probes++
		if !samePath(rec[0], workroot) {
			t.Fatalf("probe cwd %q, want workroot %q", rec[0], workroot)
		}
		if hasArg(args, "--session-id") || hasArg(args, "switch_session") {
			t.Fatalf("probe argv: %v", args)
		}
		if argValue(args, "--session") != files[0] {
			t.Fatalf("probe --session %q, want %q", argValue(args, "--session"), files[0])
		}
		if hasArg(args, "--model") {
			t.Fatal("turn passed no model; probe invented one")
		}
		for _, flag := range []string{"--offline", "--no-tools", "--no-extensions", "--session-dir"} {
			if !hasArg(args, flag) {
				t.Fatalf("probe missing %s: %v", flag, args)
			}
		}
	}
	if probes != 1 {
		t.Fatalf("probes=%d, want 1 (one sessionful turn)", probes)
	}
	if strings.Contains(errBuf.String(), "context unknown") {
		t.Fatalf("known percent warned:\n%s", errBuf.String())
	}
	h := readHandoff(t, loopDir)
	if !strings.Contains(h, "context percent: 32") {
		t.Fatalf("handoff:\n%s", h)
	}
	// none, from the same outside cwd, does not probe and writes no jsonl.
	_, loopNone := scratchLoop(t,
		"turn writer prompts/w.md\ngate ok gates/ok.sh\n",
		map[string]string{
			"loop.env":     "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=0\n",
			"prompts/w.md": "go\n",
			"gates/ok.sh":  "#!/bin/sh\nexit 0\n",
		})
	piNone, logNone := wrapFakePi(t)
	errBuf.Reset()
	code, err = Run(Options{Dir: loopNone, Pi: piNone, Quiet: true, Err: &errBuf})
	if err != nil || code != 0 {
		t.Fatalf("none exit %d err %v\n%s", code, err, errBuf.String())
	}
	if files := sessionJSONLs(t, loopNone); len(files) != 0 {
		t.Fatalf("none wrote a session file: %v", files)
	}
	for _, rec := range piInvocations(t, logNone) {
		if modeIs(rec[1:], "rpc") {
			t.Fatal("none probed")
		}
	}
	if strings.Contains(errBuf.String(), "context unknown") {
		t.Fatalf("none warned:\n%s", errBuf.String())
	}
	if h := readHandoff(t, loopNone); !strings.Contains(h, "context percent: n/a") {
		t.Fatalf("none handoff:\n%s", h)
	}
}

// Percent cuts are ActionNew. The runner must not set ForkID or pass --fork.
func TestForkOnPercentDoesNotSetForkID(t *testing.T) {
	cases := []struct {
		name      string
		percent   string
		forkPct   string
		wantFiles int
		wantWarn  int
		handoff   string
	}{
		{name: "known 41 cuts", percent: "41", wantFiles: 2, handoff: "context percent: 41"},
		{name: "known 10 continues", percent: "10", wantFiles: 1, handoff: "context percent: 10"},
		{name: "unknown continues", percent: "unknown", wantFiles: 1, wantWarn: 1, handoff: "context percent: unknown"},
		{name: "known 0 continues", percent: "0", wantFiles: 1, handoff: "context percent: 0"},
		{name: "threshold off at 100", percent: "100", forkPct: "0", wantFiles: 1, handoff: "context percent: 100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := "LOOP_MAX_ITER=1\nLOOP_SESSION=fork\nLOOP_BRANCH=0\nLOOP_SESSION_TURNS=8\n"
			if tc.forkPct != "" {
				env += "LOOP_FORK_PERCENT=" + tc.forkPct + "\n"
			}
			_, loopDir := scratchLoop(t,
				"turn writer prompts/w.md\nturn fixer prompts/w.md\ngate ok gates/ok.sh\n",
				map[string]string{
					"loop.env":     env,
					"prompts/w.md": "go\n",
					"gates/ok.sh":  "#!/bin/sh\nexit 0\n",
				})
			t.Setenv("FAKE_PI_PERCENT", tc.percent)
			piPath, logPath := wrapFakePi(t)
			var errBuf bytes.Buffer
			code, err := Run(Options{Dir: loopDir, Pi: piPath, Quiet: true, Err: &errBuf})
			if err != nil {
				t.Fatal(err)
			}
			if code != 0 {
				t.Fatalf("exit %d; a probe must not fail the iteration\n%s", code, errBuf.String())
			}
			files := sessionJSONLs(t, loopDir)
			if len(files) != tc.wantFiles {
				t.Fatalf("session files=%d want %d: %v", len(files), tc.wantFiles, files)
			}
			for _, rec := range piInvocations(t, logPath) {
				if hasArg(rec[1:], "--fork") {
					t.Fatalf("runner passed --fork (ForkID): %v", rec[1:])
				}
			}
			warns := strings.Count(errBuf.String(), "context unknown")
			if warns != tc.wantWarn {
				t.Fatalf("warns=%d want %d\n%s", warns, tc.wantWarn, errBuf.String())
			}
			if h := readHandoff(t, loopDir); !strings.Contains(h, tc.handoff) {
				t.Fatalf("handoff missing %q:\n%s", tc.handoff, h)
			}
		})
	}
}

func wrapFakePi(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv")
	real := fakePi(t)
	body := "#!/bin/sh\npwd >> " + shellQuote(logPath) + "\nprintf '%s\\n' \"$@\" >> " + shellQuote(logPath) + "\necho '---' >> " + shellQuote(logPath) + "\nexec " + shellQuote(real) + " \"$@\"\n"
	return writeExec(t, dir, "pi", body), logPath
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func sessionJSONLs(t *testing.T, loopDir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(loopDir, "state", "*", "sessions", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func readHandoff(t *testing.T, loopDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(loopDir, "state", "*", "handoff.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("handoff: %v %v", matches, err)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func headerLine(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return line
}

func gitTop(t *testing.T, loopDir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", loopDir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func piInvocations(t *testing.T, logPath string) [][]string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var inv [][]string
	var cur []string
	for _, line := range strings.Split(string(b), "\n") {
		if line == "---" {
			if len(cur) > 0 {
				inv = append(inv, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return inv
}

func hasArg(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func modeIs(args []string, mode string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--mode" && args[i+1] == mode {
			return true
		}
	}
	return false
}

func argValue(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func samePath(a, b string) bool {
	aa, err1 := filepath.EvalSymlinks(a)
	bb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return aa == bb
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode())
	})
}

func TestApplyControlSetKeepsUnknownMode(t *testing.T) {
	var errBuf bytes.Buffer
	rr := &runner{
		cfg: config.Defaults(),
		r:   ui.New(ui.Options{Out: io.Discard, Err: &errBuf, Quiet: true}),
	}
	rr.cfg.Session = config.SessionShared
	rr.cfg.Compact = config.CompactFail
	rr.sessPolicy.Mode = config.SessionShared

	rr.applyControlSet("LOOP_SESSION", "shaerd")
	if rr.cfg.Session != config.SessionShared || rr.sessPolicy.Mode != config.SessionShared {
		t.Fatalf("session changed: cfg=%q policy=%q", rr.cfg.Session, rr.sessPolicy.Mode)
	}
	rr.applyControlSet("LOOP_COMPACT", "nope")
	if rr.cfg.Compact != config.CompactFail {
		t.Fatalf("compact changed to %q", rr.cfg.Compact)
	}
	got := errBuf.String()
	if !strings.Contains(got, "LOOP_SESSION") || !strings.Contains(got, "shaerd") || !strings.Contains(got, "left unchanged") {
		t.Fatalf("session warn = %q", got)
	}
	if !strings.Contains(got, "LOOP_COMPACT") || !strings.Contains(got, "nope") {
		t.Fatalf("compact warn = %q", got)
	}

	rr.applyControlSet("LOOP_SESSION", "fork")
	if rr.cfg.Session != config.SessionFork || rr.sessPolicy.Mode != config.SessionFork {
		t.Fatalf("legal set did not apply: cfg=%q policy=%q", rr.cfg.Session, rr.sessPolicy.Mode)
	}
}
