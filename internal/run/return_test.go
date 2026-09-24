package run

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryanburnette/loop/internal/config"
	"github.com/ryanburnette/loop/internal/mend"
	"github.com/ryanburnette/loop/internal/pi"
	"github.com/ryanburnette/loop/internal/ui"
)

func TestReturnPageOnSuccessFailDoneAndStop(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		_, loopDir := scratchLoop(t, ""+
			"turn writer prompts/w.md model=writer\n"+
			"gate tests gates/tests.sh\n"+
			"hook peek hooks/peek.sh\n",
			map[string]string{
				"prompts/w.md":   "go\n",
				"gates/tests.sh": "#!/bin/sh\necho ok\nexit 0\n",
				"hooks/peek.sh":  "#!/bin/sh\ncp \"$LOOP_RETURN\" \"$LOOP_WORKROOT/during-return.md\"\n",
			})
		var out, errb bytes.Buffer
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, Out: &out, Err: &errb})
		if err != nil || code != 0 {
			t.Fatalf("exit %d err %v stderr %s", code, err, errb.String())
		}
		during, err := os.ReadFile(filepath.Join(filepath.Dir(loopDir), "during-return.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(during), "result: running") {
			t.Fatalf("start page should be running:\n%s", during)
		}
		if !strings.Contains(string(during), "The run is in progress at iteration 1 of 1. The phase is the status line. Nothing has been merged.") {
			t.Fatalf("running Next:\n%s", during)
		}
		page := stateText(t, loopDir, "return.md")
		if !strings.Contains(page, "result: success") {
			t.Fatalf("final page:\n%s", page)
		}
		if !strings.Contains(page, mend.GatedGloss) || strings.Contains(page, "exited 0") {
			t.Fatalf("gated gloss:\n%s", page)
		}
		if !strings.Contains(page, "Required checks passed on iteration 1. Nothing was merged. The work is on the current branch. Read the diff, not the model's summary.") {
			t.Fatalf("success Next:\n%s", page)
		}
		if !strings.Contains(page, "- gate tests: OK exit 0") {
			t.Fatalf("passed gate missing:\n%s", page)
		}
		id := strings.TrimSpace(string(mustRead(t, filepath.Join(loopDir, "state", "CURRENT_ID"))))
		rel := ".loop/state/" + id + "/return.md"
		if got := strings.TrimSpace(string(mustRead(t, filepath.Join(loopDir, "state", "CURRENT_RETURN")))); got != rel {
			t.Fatalf("CURRENT_RETURN %q", got)
		}
		meta := stateText(t, loopDir, "meta.env")
		if strings.Count(meta, "RESULT=") != 1 {
			t.Fatalf("RESULT should appear once:\n%s", meta)
		}
		if !strings.Contains(meta, "RESULT=success") || !strings.Contains(meta, "ASSURANCE=gated") || !strings.Contains(meta, "SUCCESS=1") {
			t.Fatalf("meta:\n%s", meta)
		}
		if strings.Contains(meta, "SUCCESS=0") {
			t.Fatalf("success must not also record SUCCESS=0:\n%s", meta)
		}
		abs := metaField(meta, "RETURN")
		if abs != filepath.Join(loopDir, "state", id, "return.md") {
			t.Fatalf("RETURN %q", abs)
		}
		wantLine := "success  iteration 1/1  assurance gated  return " + rel + "\n"
		if out.String() != wantLine {
			t.Fatalf("quiet line:\n got %q\nwant %q", out.String(), wantLine)
		}
	})

	t.Run("fail", func(t *testing.T) {
		_, loopDir := scratchLoop(t, "gate tests gates/tests.sh\n", map[string]string{
			"gates/tests.sh": "#!/bin/sh\necho '--- FAIL: TestParse (internal/manifest/manifest_test.go)'\nexit 1\n",
		})
		var out bytes.Buffer
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, Out: &out, Err: io.Discard})
		if err != nil || code != 1 {
			t.Fatalf("exit %d err %v", code, err)
		}
		page := stateText(t, loopDir, "return.md")
		red := strings.Trim(between(page, "## Still red", "## Passed"), "\n")
		passed := strings.Trim(between(page, "## Passed", "## Diff"), "\n")
		if !strings.Contains(red, "- gate tests: FAIL exit 1 — --- FAIL: TestParse (internal/manifest/manifest_test.go)") {
			t.Fatalf("Still red:\n%s", red)
		}
		if strings.TrimSpace(passed) != "(none)" {
			t.Fatalf("Passed want (none), got %q", passed)
		}
		if !strings.Contains(page, "The cap was spent with a required check still red. Nothing was merged.\nThe work is on the current branch.") {
			t.Fatalf("fail Next:\n%s", page)
		}
		meta := stateText(t, loopDir, "meta.env")
		if !strings.Contains(meta, "RESULT=fail") || !strings.Contains(meta, "SUCCESS=0") || strings.Contains(meta, "SUCCESS=1") {
			t.Fatalf("meta:\n%s", meta)
		}
		if !strings.HasPrefix(out.String(), "fail  iteration 1/1  assurance gated  return ") {
			t.Fatalf("quiet line %q", out.String())
		}
	})

	t.Run("done", func(t *testing.T) {
		_, loopDir := scratchLoop(t, "turn writer prompts/w.md\n", map[string]string{
			"prompts/w.md": "go\n",
		})
		var out bytes.Buffer
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, Out: &out, Err: io.Discard})
		if err != nil || code != 0 {
			t.Fatalf("exit %d err %v", code, err)
		}
		page := stateText(t, loopDir, "return.md")
		if !strings.Contains(page, "result: done") || !strings.Contains(page, "assurance: none") {
			t.Fatalf("page:\n%s", page)
		}
		if !strings.Contains(page, "The cap was reached. This loop had no required check, so finishing the cap is not a pass. Nothing was merged.") {
			t.Fatalf("done Next:\n%s", page)
		}
		meta := stateText(t, loopDir, "meta.env")
		if !strings.Contains(meta, "RESULT=done") || !strings.Contains(meta, "SUCCESS=0") || strings.Contains(meta, "SUCCESS=1") {
			t.Fatalf("done must stay SUCCESS=0:\n%s", meta)
		}
		if !strings.HasPrefix(out.String(), "done  iteration 1/1  assurance none  return ") {
			t.Fatalf("quiet line %q", out.String())
		}
	})

	t.Run("stopped", func(t *testing.T) {
		root, loopDir := scratchLoop(t, ""+
			"gate first gates/first.sh\n"+
			"gate second gates/second.sh\n",
			map[string]string{
				"gates/first.sh":  "#!/bin/sh\nprintf 'stop\\n' > \"$LOOP_STATE_DIR/control\"\nexit 0\n",
				"gates/second.sh": "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
			})
		var out bytes.Buffer
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, Out: &out, Err: io.Discard})
		if err != nil || code != 1 {
			t.Fatalf("exit %d err %v", code, err)
		}
		if _, err := os.Stat(filepath.Join(root, "MARKER")); err == nil {
			t.Fatal("second gate ran after stop")
		}
		page := stateText(t, loopDir, "return.md")
		if !strings.Contains(page, "result: stopped") {
			t.Fatalf("page:\n%s", page)
		}
		if !strings.Contains(page, "The operator stopped the run during iteration 1 (control file or signal). Nothing was merged. The work is on the current branch if one was created.") {
			t.Fatalf("stopped Next:\n%s", page)
		}
		meta := stateText(t, loopDir, "meta.env")
		if !strings.Contains(meta, "RESULT=stopped") || !strings.Contains(meta, "SUCCESS=0") {
			t.Fatalf("meta:\n%s", meta)
		}
		if !strings.HasPrefix(out.String(), "stopped  iteration 1/1  assurance gated  return ") {
			t.Fatalf("quiet line %q", out.String())
		}
	})
}

func TestSelfGradedWarnsWhenActorModelEmpty(t *testing.T) {
	_, loopDir := scratchLoop(t, ""+
		"turn writer prompts/w.md model=writer\n"+
		"turn judge prompts/j.md model=judge scorecard=scorecards/review.card\n",
		map[string]string{
			"loop.env":               "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=0\nLOOP_JUDGE_MODEL=other/model\n",
			"prompts/w.md":           "go\n",
			"prompts/j.md":           "judge\n",
			"scorecards/review.card": "rule all\n\nitem auth\nThe check holds.\n",
		})
	t.Setenv("FAKE_PI_CARD", "met")
	var out, errb bytes.Buffer
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, Out: &out, Err: &errb})
	if err != nil || code != 0 {
		t.Fatalf("exit %d err %v stderr %s", code, err, errb.String())
	}
	if !strings.Contains(errb.String(), mend.SelfCheckWarn) {
		t.Fatalf("stderr missing self-check warning:\n%s", errb.String())
	}
	meta := stateText(t, loopDir, "meta.env")
	if !strings.Contains(meta, "ASSURANCE=self-graded") || !strings.Contains(meta, "RESULT=success") {
		t.Fatalf("meta:\n%s", meta)
	}
	page := stateText(t, loopDir, "return.md")
	if !strings.Contains(page, "assurance: self-graded — "+mend.SelfGradedGloss) {
		t.Fatalf("page:\n%s", page)
	}
	if !strings.HasPrefix(out.String(), "success  iteration 1/1  assurance self-graded  return ") {
		t.Fatalf("quiet line %q", out.String())
	}
}

func TestStatusTimerRewritesWithoutEvents(t *testing.T) {
	oldInterval := statusInterval
	oldNow := statusNow
	t.Cleanup(func() {
		statusInterval = oldInterval
		statusNow = oldNow
	})
	statusInterval = 20 * time.Millisecond
	base := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	statusNow = func() time.Time { return base }

	dir := t.TempDir()
	rr := &runner{
		cfg:      config.Config{MaxIter: 8},
		stateDir: dir,
		runStart: base.Add(-5 * time.Second),
		r:        ui.New(ui.Options{Out: io.Discard}),
	}
	rr.writeStatus(3, "turn writer")
	before, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "tool") {
		t.Fatalf("tool omitted until tool_execution_start: %s", before)
	}
	ev := pi.Event{
		Type:     "tool_execution_start",
		ToolName: "bash",
		Raw:      map[string]any{"args": map[string]any{"command": "go test"}},
	}
	for i := 0; i < 8; i++ {
		rr.onTurnEvent(ev, base)
	}
	mid, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		t.Fatal(err)
	}
	if string(mid) != string(before) {
		t.Fatalf("event handler must not write status:\n before %s\n after  %s", before, mid)
	}
	rr.onTurnEvent(pi.Event{
		Type:     "tool_execution_start",
		ToolName: "read",
		Raw:      map[string]any{"args": map[string]any{"path": "loader.go"}},
	}, base)

	statusNow = func() time.Time { return base.Add(40 * time.Second) }
	stop := rr.watchStatus(3, "turn writer")
	defer stop()
	deadline := time.Now().Add(time.Second)
	var got []byte
	for {
		got, _ = os.ReadFile(filepath.Join(dir, "status"))
		if strings.Contains(string(got), "· tool read loader.go") && strings.Contains(string(got), "elapsed 45s") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timer did not rewrite status without a new event: %s", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
