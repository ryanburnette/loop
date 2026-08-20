package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchLoop builds a git repo containing a loop dir with the given manifest
// and files, and returns (workroot, loopDir). files maps a path relative to
// the loop dir to its content; anything under gates/ or hooks/ is made
// executable. loop.env gets MaxIter/session/branch defaults unless supplied.
func scratchLoop(t *testing.T, manifest string, files map[string]string) (string, string) {
	t.Helper()
	clearLoopEnv(t)
	root := t.TempDir()
	loopDir := filepath.Join(root, ".loop")

	if _, ok := files["loop.env"]; !ok {
		files["loop.env"] = "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=0\n"
	}
	if manifest != "" {
		files["manifest"] = manifest
	}
	for rel, body := range files {
		full := filepath.Join(loopDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(rel, "gates/") || strings.HasPrefix(rel, "hooks/") {
			mode = 0o755
		}
		if err := os.WriteFile(full, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	return root, loopDir
}

// failingPi is a stand-in for a pi that dies without producing a turn.
const failingPi = "#!/bin/sh\necho 'boom: model unavailable' >&2\nexit 1\n"

func writeExec(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakePi(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "fake-pi")
}

// A turn whose pi call errors must abort the rest of the iteration. Before
// this was fixed the `break` landed on the type switch, so the remaining
// gates ran anyway — and a passing gate scored the iteration ok, reporting
// SUCCESS for work the model never did.
func TestTurnErrorAbortsRemainingSteps(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"turn writer prompts/01-writer.md required=0\ngate marker gates/marker.sh\n",
		map[string]string{
			"prompts/01-writer.md": "go\n",
			"gates/marker.sh":      "#!/bin/sh\ntouch \"$LOOP_WORKROOT/GATE_RAN\"\nexit 0\n",
		})
	pi := writeExec(t, t.TempDir(), "failing-pi", failingPi)

	code, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("a turn that never ran must not be scored a success")
	}
	if _, err := os.Stat(filepath.Join(root, "GATE_RAN")); err == nil {
		t.Fatal("gate ran after the turn errored; the step loop did not break")
	}
}

// A turn error belongs in gate-log.md, so the next iteration's handoff does
// not carry a stale gate result as if it were current.
func TestTurnErrorIsLogged(t *testing.T) {
	_, loopDir := scratchLoop(t, "turn writer prompts/01-writer.md\n",
		map[string]string{"prompts/01-writer.md": "go\n"})
	pi := writeExec(t, t.TempDir(), "failing-pi", failingPi)

	if _, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true}); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(loopDir, "state", "*", "gate-log.md"))
	if len(matches) == 0 {
		t.Fatal("no gate-log.md")
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "TURN writer: ERROR") {
		t.Fatalf("turn error missing from gate-log.md:\n%s", b)
	}
}

// Success is decided once per iteration: an objective loop exits 0 on the
// first passing iteration and 1 at the cap; a loop with no objective runs
// MaxIter times and exits 0 regardless.
func TestExitCodeMatrix(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		gate     string
		want     int
	}{
		{"objective passes", "turn w prompts/01-writer.md\ngate g gates/g.sh\n", "#!/bin/sh\nexit 0\n", 0},
		{"objective never passes", "turn w prompts/01-writer.md\ngate g gates/g.sh\n", "#!/bin/sh\nexit 1\n", 1},
		{"soft gate only", "turn w prompts/01-writer.md\ngate g gates/g.sh required=0\n", "#!/bin/sh\nexit 1\n", 0},
		{"no objective", "turn w prompts/01-writer.md\n", "#!/bin/sh\nexit 1\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, loopDir := scratchLoop(t, tc.manifest, map[string]string{
				"loop.env":             "LOOP_MAX_ITER=2\nLOOP_SESSION=none\nLOOP_BRANCH=0\n",
				"prompts/01-writer.md": "go\n",
				"gates/g.sh":           tc.gate,
			})
			code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
			if err != nil {
				t.Fatal(err)
			}
			if code != tc.want {
				t.Fatalf("exit %d want %d", code, tc.want)
			}
		})
	}
}

// A required verdict that does not match is a blocking check.
func TestRequiredVerdictBlocks(t *testing.T) {
	_, loopDir := scratchLoop(t,
		"turn writer prompts/01-writer.md verdict=^VERDICT: PASS\n",
		map[string]string{"prompts/01-writer.md": "go\n"})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit %d want 1: fake-pi never writes VERDICT: PASS", code)
	}
}

// `stop` in the control file ends the run before the next step.
func TestControlStopEndsRun(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"gate stopper gates/01-stopper.sh\ngate marker gates/02-marker.sh\n",
		map[string]string{
			"gates/01-stopper.sh": "#!/bin/sh\nprintf 'stop\\n' > \"$LOOP_STATE_DIR/control\"\nexit 0\n",
			"gates/02-marker.sh":  "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
		})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit %d want 1 after control stop", code)
	}
	if _, err := os.Stat(filepath.Join(root, "MARKER")); err == nil {
		t.Fatal("second gate ran after stop")
	}
}

// `set KEY=VALUE` overlays config for the rest of the run.
func TestControlSetOverlaysConfig(t *testing.T) {
	root, loopDir := scratchLoop(t, "gate bump gates/bump.sh\n", map[string]string{
		"loop.env": "LOOP_MAX_ITER=5\nLOOP_SESSION=none\nLOOP_BRANCH=0\n",
		// Fails every iteration so the loop runs to the cap, and lowers the
		// cap to 2 on the way past. Counts its own invocations.
		"gates/bump.sh": "#!/bin/sh\n" +
			"printf 'x\\n' >> \"$LOOP_WORKROOT/COUNT\"\n" +
			"printf 'set LOOP_MAX_ITER=2\\n' > \"$LOOP_STATE_DIR/control\"\n" +
			"exit 1\n",
	})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit %d want 1", code)
	}
	b, err := os.ReadFile(filepath.Join(root, "COUNT"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "x"); n != 2 {
		t.Fatalf("gate ran %d times, want 2 (control `set LOOP_MAX_ITER=2` ignored?)", n)
	}
}

// SIGINT is a stop: the in-flight step is killed and the run exits 1. The
// gate signals the test binary, which is the runner's process here.
func TestSignalStopsRun(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"gate signaler gates/01-signaler.sh\ngate marker gates/02-marker.sh\n",
		map[string]string{
			"gates/01-signaler.sh": "#!/bin/sh\nkill -INT \"$PPID\"\nsleep 30\n",
			"gates/02-marker.sh":   "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
		})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit %d want 1 after SIGINT", code)
	}
	if _, err := os.Stat(filepath.Join(root, "MARKER")); err == nil {
		t.Fatal("second gate ran after the signal stop")
	}
	matches, _ := filepath.Glob(filepath.Join(loopDir, "state", "*", "meta.env"))
	if len(matches) == 0 {
		t.Fatal("no meta.env")
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "SUCCESS=0") {
		t.Fatalf("meta.env should record SUCCESS=0 on a signal stop:\n%s", b)
	}
}

// LOOP_BRANCH=1 moves the run onto loop/<id> and leaves a backup branch.
func TestBranchLoopCreatesBranchAndBackup(t *testing.T) {
	root, loopDir := scratchLoop(t, "turn w prompts/01-writer.md\n", map[string]string{
		"loop.env":             "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=1\nLOOP_BRANCH_BASE=HEAD\n",
		"prompts/01-writer.md": "go\n",
	})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	head := gitOut(t, root, "rev-parse", "--abbrev-ref", "HEAD")
	if !strings.HasPrefix(head, "loop/") {
		t.Fatalf("HEAD is %q, want a loop/<id> branch", head)
	}
	branches := gitOut(t, root, "branch", "--list", "backup/loop-*")
	if strings.TrimSpace(branches) == "" {
		t.Fatal("no backup/loop-<id> safety branch")
	}
}

// A branch loop refuses a dirty tree, and says how to proceed anyway.
func TestBranchLoopRefusesDirtyTree(t *testing.T) {
	root, loopDir := scratchLoop(t, "turn w prompts/01-writer.md\n", map[string]string{
		"loop.env":             "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=1\nLOOP_BRANCH_BASE=HEAD\n",
		"prompts/01-writer.md": "go\n",
	})
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err == nil {
		t.Fatal("expected a dirty-tree refusal")
	}
	if !strings.Contains(err.Error(), "--branch=false") {
		t.Fatalf("refusal should name the escape hatch, got: %v", err)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// A one-shot run builds its recipe in a temp dir, so the loop dir contributes
// nothing to the dirty-tree tolerance. An untracked .loop/ left in the project
// by a previous `loop init` must still not block it — the run does not even
// use that directory.
func TestOneShotNotBlockedByUntrackedLoopDir(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	// A .loop/ the project never gitignored, as `loop init` used to leave it.
	if err := os.MkdirAll(filepath.Join(root, ".loop", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".loop", "loop.env"), []byte("LOOP_MAX_ITER=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The one-shot's own recipe, in a scratch dir outside the workroot.
	scratch := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scratch, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "prompts", "oneshot.md"), []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "manifest"), []byte("turn writer prompts/oneshot.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "loop.env"),
		[]byte("LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=1\nLOOP_BRANCH_BASE=HEAD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, err := Run(Options{Dir: scratch, Workroot: root, OneShot: true, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatalf("an untracked .loop/ must not block a one-shot: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit %d want 0", code)
	}
	if head := gitOut(t, root, "rev-parse", "--abbrev-ref", "HEAD"); !strings.HasPrefix(head, "loop/") {
		t.Fatalf("HEAD is %q, want a loop/<id> branch", head)
	}
}

// Untracked work outside the recipe still refuses, so the tolerance above did
// not turn the clean-tree check into a no-op.
func TestUntrackedWorkOutsideRecipeStillRefuses(t *testing.T) {
	root, loopDir := scratchLoop(t, "turn w prompts/01-writer.md\n", map[string]string{
		"loop.env":             "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=1\nLOOP_BRANCH_BASE=HEAD\n",
		"prompts/01-writer.md": "go\n",
	})
	if err := os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true}); err == nil {
		t.Fatal("untracked work outside the loop dir must still refuse")
	}
}
