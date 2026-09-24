package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loopEnv(max int, extra string) string {
	return fmt.Sprintf("LOOP_MAX_ITER=%d\nLOOP_SESSION=none\nLOOP_BRANCH=0\n%s", max, extra)
}

func piMarker(t *testing.T) (string, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "pi-ran")
	pi := writeExec(t, t.TempDir(), "pi", fmt.Sprintf("#!/bin/sh\ntouch '%s'\nexit 0\n", marker))
	return pi, marker
}

func assertPiNotRun(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("pi was invoked")
	}
}

func tickCount(t *testing.T, loopDir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(loopDir, "ticks"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "x")
}

const redGate = "#!/bin/sh\nprintf 'x\\n' >> \"$LOOP_ROOT/ticks\"\necho 'command not found'\nexit 1\n"

func TestPreflightNonExecutableGate(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"turn writer prompts/w.md\ngate g gates/late.sh\n",
		map[string]string{
			"loop.env":     "LOOP_MAX_ITER=3\nLOOP_SESSION=none\nLOOP_BRANCH=1\nLOOP_BRANCH_BASE=HEAD\n",
			"prompts/w.md": "go\n",
		})
	gate := filepath.Join(loopDir, "gates", "late.sh")
	if err := os.MkdirAll(filepath.Dir(gate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gate, 0o644); err != nil {
		t.Fatal(err)
	}
	pi, marker := piMarker(t)
	code, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true})
	if err == nil || code != 2 {
		t.Fatalf("exit %d err %v, want 2 and a preflight error", code, err)
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("error %v", err)
	}
	assertPiNotRun(t, marker)
	if iter := strings.TrimSpace(stateText(t, loopDir, "iteration")); iter != "0" {
		t.Fatalf("iteration %q, want 0", iter)
	}
	if strings.Contains(stateText(t, loopDir, "gate-log.md"), "GATE ") {
		t.Fatal("preflight ran the gate")
	}
	id := strings.TrimSpace(string(mustRead(t, filepath.Join(loopDir, "state", "CURRENT_ID"))))
	page := stateText(t, loopDir, "return.md")
	if !strings.Contains(page, "result: recipe") {
		t.Fatalf("page:\n%s", page)
	}
	want := "The recipe failed. No pi turn ran. Branch setup created loop/" + id + ". Preflight runs after branch setup, and a preflight failure does not delete that branch."
	if !strings.Contains(page, want) {
		t.Fatalf("Next:\n%s", page)
	}
	if head := gitOut(t, root, "rev-parse", "--abbrev-ref", "HEAD"); head != "loop/"+id {
		t.Fatalf("HEAD %q, branch should still exist", head)
	}
	if !strings.Contains(gitOut(t, root, "branch", "--list", "loop/"+id), "loop/"+id) {
		t.Fatal("preflight deleted loop/<id>")
	}
	meta := stateText(t, loopDir, "meta.env")
	if !strings.Contains(meta, "RESULT=recipe") || !strings.Contains(meta, "SUCCESS=0") {
		t.Fatalf("meta:\n%s", meta)
	}
}

func TestPreflightBadScorecardAndFreeze(t *testing.T) {
	t.Run("threshold", func(t *testing.T) {
		_, loopDir := scratchLoop(t,
			"turn reviewer prompts/r.md scorecard=scorecards/review.card\n",
			map[string]string{
				"prompts/r.md":           "review\n",
				"scorecards/review.card": "rule threshold 9\n\nitem auth\nThe check holds.\n",
			})
		pi, marker := piMarker(t)
		code, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true})
		if err == nil || code != 2 {
			t.Fatalf("exit %d err %v", code, err)
		}
		if !strings.Contains(err.Error(), "threshold") {
			t.Fatalf("error %v", err)
		}
		assertPiNotRun(t, marker)
		if !strings.Contains(stateText(t, loopDir, "return.md"), "result: recipe") {
			t.Fatal(stateText(t, loopDir, "return.md"))
		}
	})
	t.Run("freeze", func(t *testing.T) {
		_, loopDir := scratchLoop(t, "turn writer prompts/w.md\n", map[string]string{
			"loop.env":     loopEnv(2, "LOOP_FREEZE=no_such_*.zz\n"),
			"prompts/w.md": "go\n",
		})
		pi, marker := piMarker(t)
		code, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true})
		if err == nil || code != 2 {
			t.Fatalf("exit %d err %v", code, err)
		}
		if !strings.Contains(err.Error(), "matches no files") {
			t.Fatalf("error %v", err)
		}
		assertPiNotRun(t, marker)
	})
	t.Run("maxiter", func(t *testing.T) {
		_, loopDir := scratchLoop(t, "turn writer prompts/w.md\n", map[string]string{
			"loop.env":     loopEnv(0, ""),
			"prompts/w.md": "go\n",
		})
		pi, marker := piMarker(t)
		code, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true})
		if err == nil || code != 2 {
			t.Fatalf("exit %d err %v", code, err)
		}
		if !strings.Contains(err.Error(), "LOOP_MAX_ITER") {
			t.Fatalf("error %v", err)
		}
		assertPiNotRun(t, marker)
	})
}

func TestGateExit127StopsBeforeIteration2(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"turn writer prompts/w.md\ngate boom gates/boom.sh\ngate later gates/later.sh\n",
		map[string]string{
			"loop.env":       loopEnv(4, ""),
			"prompts/w.md":   "go\n",
			"gates/boom.sh":  "#!/bin/sh\nprintf 'x\\n' >> \"$LOOP_ROOT/ticks\"\necho 'command not found'\nexit 127\n",
			"gates/later.sh": "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
		})
	logPath := filepath.Join(t.TempDir(), "pi-log")
	pi := writeExec(t, t.TempDir(), "pi", fmt.Sprintf("#!/bin/sh\nprintf 'x\\n' >> '%s'\nexec '%s' \"$@\"\n", logPath, fakePi(t)))
	code, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true})
	if err != nil || code != 2 {
		t.Fatalf("exit %d err %v", code, err)
	}
	if _, err := os.Stat(filepath.Join(root, "MARKER")); err == nil {
		t.Fatal("later gate ran after exit 127")
	}
	if tickCount(t, loopDir) != 1 {
		t.Fatalf("gate ran %d times", tickCount(t, loopDir))
	}
	if iter := strings.TrimSpace(stateText(t, loopDir, "iteration")); iter != "1" {
		t.Fatalf("iteration %q", iter)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(log), "x") != 1 {
		t.Fatalf("pi ran %d times:\n%s", strings.Count(string(log), "x"), log)
	}
	page := stateText(t, loopDir, "return.md")
	if !strings.Contains(page, "result: recipe") {
		t.Fatalf("page:\n%s", page)
	}
	if !strings.Contains(page, "Required gate boom exited 127 on iteration 1.") {
		t.Fatalf("Next:\n%s", page)
	}
	if strings.Contains(page, "No pi turn ran.") {
		t.Fatalf("a turn ran:\n%s", page)
	}
	meta := stateText(t, loopDir, "meta.env")
	if !strings.Contains(meta, "RESULT=recipe") || !strings.Contains(meta, "SUCCESS=0") {
		t.Fatalf("meta:\n%s", meta)
	}
}

func TestTwoNoDiffFailuresStall(t *testing.T) {
	root, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
		"loop.env":   loopEnv(5, ""),
		"gates/g.sh": redGate,
	})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil || code != 1 {
		t.Fatalf("exit %d err %v", code, err)
	}
	if tickCount(t, loopDir) != 2 {
		t.Fatalf("gate ran %d times, want 2", tickCount(t, loopDir))
	}
	page := stateText(t, loopDir, "return.md")
	head := gitOut(t, root, "rev-parse", "HEAD")
	want := "Stopped after iteration 2 because the failure signature and the tree matched iteration 1. The signature is gate g exit 1: command not found. HEAD " + head + "."
	if !strings.Contains(page, "result: stalled") || !strings.Contains(page, want) {
		t.Fatalf("page:\n%s", page)
	}
	if metaField(stateText(t, loopDir, "meta.env"), "RESULT") != "stalled" {
		t.Fatalf("meta:\n%s", stateText(t, loopDir, "meta.env"))
	}
	if !strings.Contains(stateText(t, loopDir, "stall.json"), "command not found") {
		t.Fatalf("stall.json:\n%s", stateText(t, loopDir, "stall.json"))
	}
}

func TestCommitPreventsStall(t *testing.T) {
	_, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
		"loop.env": loopEnv(3, ""),
		"gates/g.sh": "#!/bin/sh\n" +
			"printf 'x\\n' >> \"$LOOP_ROOT/ticks\"\n" +
			"printf '%s\\n' \"$LOOP_ITERATION\" > \"$LOOP_WORKROOT/progress.txt\"\n" +
			"git -C \"$LOOP_WORKROOT\" add -- progress.txt\n" +
			"git -C \"$LOOP_WORKROOT\" -c user.email=t@t -c user.name=t commit -qm \"progress $LOOP_ITERATION\"\n" +
			"echo 'still red'\n" +
			"exit 1\n",
	})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil || code != 1 {
		t.Fatalf("exit %d err %v", code, err)
	}
	if tickCount(t, loopDir) != 3 {
		t.Fatalf("gate ran %d times, want 3", tickCount(t, loopDir))
	}
	if got := metaField(stateText(t, loopDir, "meta.env"), "RESULT"); got != "fail" {
		t.Fatalf("RESULT=%s, a new commit must not stall", got)
	}
}

func TestUntrackedFileOutsideLoopPreventsStall(t *testing.T) {
	root, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
		"loop.env": loopEnv(2, ""),
		"gates/g.sh": "#!/bin/sh\n" +
			"printf 'x\\n' >> \"$LOOP_ROOT/ticks\"\n" +
			"if [ \"$LOOP_ITERATION\" -ge 2 ]; then\n" +
			"  printf '%s\\n' \"$LOOP_ITERATION\" > \"$LOOP_WORKROOT/wip.txt\"\n" +
			"fi\n" +
			"echo 'still red'\n" +
			"exit 1\n",
	})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil || code != 1 {
		t.Fatalf("exit %d err %v", code, err)
	}
	if _, err := os.Stat(filepath.Join(root, "wip.txt")); err != nil {
		t.Fatal("iteration 2 did not write the untracked file")
	}
	if tickCount(t, loopDir) != 2 {
		t.Fatalf("gate ran %d times", tickCount(t, loopDir))
	}
	if got := metaField(stateText(t, loopDir, "meta.env"), "RESULT"); got != "fail" {
		t.Fatalf("RESULT=%s, porcelain should see wip.txt", got)
	}
}

func TestIgnoredFileStallsUnlessListed(t *testing.T) {
	gate := "#!/bin/sh\n" +
		"printf 'x\\n' >> \"$LOOP_ROOT/ticks\"\n" +
		"printf '%s\\n' \"$LOOP_ITERATION\" > \"$LOOP_WORKROOT/ignored.txt\"\n" +
		"echo 'still red'\n" +
		"exit 1\n"
	t.Run("ignored", func(t *testing.T) {
		root, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
			"loop.env":   loopEnv(4, ""),
			"gates/g.sh": gate,
		})
		ignoreIgnored(t, root)
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
		if err != nil || code != 1 {
			t.Fatalf("exit %d err %v", code, err)
		}
		if tickCount(t, loopDir) != 2 {
			t.Fatalf("gate ran %d times, ignored content must not count", tickCount(t, loopDir))
		}
		if got := metaField(stateText(t, loopDir, "meta.env"), "RESULT"); got != "stalled" {
			t.Fatalf("RESULT=%s", got)
		}
	})
	t.Run("listed", func(t *testing.T) {
		root, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
			"loop.env":   loopEnv(3, "LOOP_STALL_PATHS=ignored.txt\n"),
			"gates/g.sh": gate,
		})
		ignoreIgnored(t, root)
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
		if err != nil || code != 1 {
			t.Fatalf("exit %d err %v", code, err)
		}
		if tickCount(t, loopDir) != 3 {
			t.Fatalf("gate ran %d times, listed path should be progress", tickCount(t, loopDir))
		}
		if got := metaField(stateText(t, loopDir, "meta.env"), "RESULT"); got != "fail" {
			t.Fatalf("RESULT=%s", got)
		}
	})
}

func ignoreIgnored(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, root, "add", "--", ".gitignore")
	gitOut(t, root, "commit", "-qm", "ignore")
}

func TestStallContinueReachesCap(t *testing.T) {
	_, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
		"loop.env":   loopEnv(3, "LOOP_STALL=continue\n"),
		"gates/g.sh": redGate,
	})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil || code != 1 {
		t.Fatalf("exit %d err %v", code, err)
	}
	if tickCount(t, loopDir) != 3 {
		t.Fatalf("gate ran %d times, want the cap", tickCount(t, loopDir))
	}
	if got := metaField(stateText(t, loopDir, "meta.env"), "RESULT"); got != "fail" {
		t.Fatalf("RESULT=%s", got)
	}
}

func TestResumeStallDoesNotGrantTwoIterations(t *testing.T) {
	_, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
		"loop.env":   loopEnv(1, ""),
		"gates/g.sh": redGate,
	})
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil || code != 1 {
		t.Fatalf("first exit %d err %v", code, err)
	}
	if tickCount(t, loopDir) != 1 {
		t.Fatalf("first run ticks %d", tickCount(t, loopDir))
	}
	if got := metaField(stateText(t, loopDir, "meta.env"), "RESULT"); got != "fail" {
		t.Fatalf("first RESULT=%s", got)
	}
	id := strings.TrimSpace(string(mustRead(t, filepath.Join(loopDir, "state", "CURRENT_ID"))))
	code, err = Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, MaxIter: 5, ResumeID: id})
	if err != nil || code != 1 {
		t.Fatalf("resume exit %d err %v", code, err)
	}
	if tickCount(t, loopDir) != 2 {
		t.Fatalf("resume ran until %d failures; stall.json should stop on the next one", tickCount(t, loopDir))
	}
	if got := metaField(stateText(t, loopDir, "meta.env"), "RESULT"); got != "stalled" {
		t.Fatalf("resume RESULT=%s", got)
	}
}
