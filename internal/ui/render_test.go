package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// esc is the ESC byte used to start an ANSI escape sequence.
var esc = []byte{0x1b}

// assertNoEsc fails if buf contains any ANSI escape bytes. The package doc
// comment promises that color-off output emits zero ESC bytes; this checks
// that promise across every rendering method, not just one.
func assertNoEsc(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	if buf == nil {
		return
	}
	if bytes.Contains(buf.Bytes(), esc) {
		t.Fatalf("color-off output must contain zero ESC bytes, got:\n%q", buf.String())
	}
}

func TestNewDefaultsNilOut(t *testing.T) {
	// New must not panic when Out is nil; it falls back to os.Stdout. We do
	// not write through the default writer here (that would spam os.Stdout);
	// constructing the renderer exercises the nil-Out branch in New.
	r := New(Options{Color: false})
	if r == nil {
		t.Fatal("New returned nil")
	}
}

func TestHeaderColorAndNoColor(t *testing.T) {
	cases := []struct {
		name  string
		color bool
	}{
		{"plain", false},
		{"color", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := New(Options{Out: &buf, Color: c.color})
			r.Header(Header{
				ID:        "RUNID",
				Dir:       "/d",
				WorkRoot:  "/w",
				Branch:    "loop/RUNID",
				GitRepo:   true,
				GitBranch: "main",
				GitSHA:    "abc123",
				GitDirty:  true,
				GitDirtyN: 3,
				Session:   "none",
				MaxIter:   5,
				Objective: true,
			})
			s := buf.String()
			for _, want := range []string{"RUNID", "/d", "/w", "loop/RUNID", "main", "abc123", "dirty(3)", "none", "objective yes"} {
				if !strings.Contains(s, want) {
					t.Fatalf("missing %q in %s mode:\n%s", want, c.name, s)
				}
			}
			if !c.color {
				assertNoEsc(t, &buf)
			}
		})
	}
}

func TestHeaderGitCleanAndNoRepo(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false})
	r.Header(Header{ID: "x", GitRepo: true, GitBranch: "main", GitSHA: "deadbeef", GitDirty: false, GitDirtyN: 0})
	s := buf.String()
	if !strings.Contains(s, "clean") {
		t.Fatalf("clean git should print clean:\n%s", s)
	}
	if strings.Contains(s, "dirty") {
		t.Fatalf("clean git should not mention dirty:\n%s", s)
	}

	// No SHA → renders a dash.
	buf.Reset()
	r.Header(Header{ID: "y", GitRepo: true, GitBranch: "dev", GitSHA: "", GitDirty: false})
	if !strings.Contains(buf.String(), " - ") && !strings.Contains(buf.String(), "- ") {
		t.Fatalf("empty sha should render dash:\n%s", buf.String())
	}

	// Not a git repo → "-" git line.
	buf.Reset()
	r.Header(Header{ID: "z", GitRepo: false})
	if !strings.Contains(buf.String(), "git") {
		t.Fatalf("non-repo should still print a git line:\n%s", buf.String())
	}
}

func TestHeaderQuiet(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false, Quiet: true})
	r.Header(Header{ID: "x", GitRepo: true})
	if buf.String() != "" {
		t.Fatalf("quiet header should print nothing, got %q", buf.String())
	}
}

func TestHeaderJSON(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false, JSON: true})
	r.Header(Header{ID: "h1", Dir: "/d", WorkRoot: "/w", Branch: "loop/b", GitRepo: true, GitBranch: "main", GitSHA: "sha", GitDirty: true, GitDirtyN: 2, Session: "none", MaxIter: 3, Objective: true})
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("header json invalid: %v\n%s", err, buf.String())
	}
	if m["type"] != "header" || m["id"] != "h1" || m["gitDirty"] != true || m["gitBranch"] != "main" {
		t.Fatalf("header json fields wrong: %v", m)
	}
}

func TestIterationModes(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Iteration(2, 9)
		if !strings.Contains(buf.String(), "iteration 2/9") {
			t.Fatalf("iter: %s", buf.String())
		}
		assertNoEsc(t, &buf)
	})
	t.Run("quiet", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true})
		r.Iteration(1, 1)
		if buf.String() != "" {
			t.Fatalf("quiet iter should be empty, got %q", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Iteration(3, 4)
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "iteration" || m["i"].(float64) != 3 || m["n"].(float64) != 4 {
			t.Fatalf("iter json: %v", m)
		}
	})
	t.Run("color", func(t *testing.T) {
		// Color:true exercises the lipgloss style assignments in New and the
		// s.Render branch in style. lipgloss strips escapes when the writer is
		// not a TTY, so we assert on content, not on ESC bytes (the zero-ESC
		// promise is the color-off path, checked everywhere else).
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: true})
		r.Iteration(1, 1)
		if !strings.Contains(buf.String(), "iteration 1/1") {
			t.Fatalf("color iter content: %s", buf.String())
		}
	})
}

func TestStepStartAllKinds(t *testing.T) {
	kinds := []string{"turn", "gate", "hook", "mystery"}
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false})
	for _, k := range kinds {
		r.StepStart(k, k+"-step", "detail-"+k)
	}
	s := buf.String()
	for _, want := range []string{"▶", "turn", "▣", "gate", "⚙", "hook", "→", "mystery", "detail-turn"} {
		if !strings.Contains(s, want) {
			t.Fatalf("stepStart missing %q:\n%s", want, s)
		}
	}
	assertNoEsc(t, &buf)
}

func TestStepStartNoDetail(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false})
	r.StepStart("turn", "writer", "")
	s := buf.String()
	if !strings.Contains(s, "turn writer") {
		t.Fatalf("stepStart: %s", s)
	}
	// No detail suffix when empty: the line ends after the name.
	if strings.Contains(s, "  \n") {
		t.Fatalf("stepStart should not trail with detail when empty: %q", s)
	}
}

func TestStepStartQuietAndJSON(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false, Quiet: true})
	r.StepStart("turn", "x", "d")
	if buf.String() != "" {
		t.Fatalf("quiet stepStart should be empty: %q", buf.String())
	}
	buf.Reset()
	r = New(Options{Out: &buf, Color: false, JSON: true})
	r.StepStart("gate", "tests", "required")
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["kind"] != "gate" || m["name"] != "tests" {
		t.Fatalf("stepStart json: %v", m)
	}
}

func TestStepDoneOkAndFail(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false})
	r.StepDone(true, "passed", 7)
	r.StepDone(false, "FAIL", 1)
	s := buf.String()
	if !strings.Contains(s, "✓ passed (7s)") {
		t.Fatalf("stepDone ok missing: %s", s)
	}
	if !strings.Contains(s, "✗ FAIL (1s)") {
		t.Fatalf("stepDone fail missing: %s", s)
	}
	assertNoEsc(t, &buf)
}

func TestStepDoneQuietAndJSON(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false, Quiet: true})
	r.StepDone(true, "x", 1)
	if buf.String() != "" {
		t.Fatalf("quiet stepDone empty: %q", buf.String())
	}
	buf.Reset()
	r = New(Options{Out: &buf, Color: false, JSON: true})
	r.StepDone(false, "boom", 2)
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != false || m["note"] != "boom" || m["elapsed"].(float64) != 2 {
		t.Fatalf("stepDone json: %v", m)
	}
}

func TestTool(t *testing.T) {
	t.Run("withDetail", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Tool("bash", "ls /tmp")
		if !strings.Contains(buf.String(), "tool bash ls /tmp") {
			t.Fatalf("tool: %s", buf.String())
		}
		assertNoEsc(t, &buf)
	})
	t.Run("noDetail", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Tool("bash", "")
		if !strings.Contains(buf.String(), "tool bash") {
			t.Fatalf("tool no detail: %s", buf.String())
		}
		// No bare trailing detail area.
		if strings.Contains(buf.String(), "tool bash \n") {
			t.Fatalf("tool no detail should not trail: %q", buf.String())
		}
	})
	t.Run("quiet", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true})
		r.Tool("bash", "x")
		if buf.String() != "" {
			t.Fatalf("quiet tool: %q", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Tool("bash", "ls")
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "tool" || m["name"] != "bash" {
			t.Fatalf("tool json: %v", m)
		}
	})
}

func TestContext(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Context(42, 13)
		s := buf.String()
		if !strings.Contains(s, "42%") || !strings.Contains(s, "13s") {
			t.Fatalf("context: %s", s)
		}
		assertNoEsc(t, &buf)
	})
	t.Run("quiet", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true})
		r.Context(1, 1)
		if buf.String() != "" {
			t.Fatalf("quiet context: %q", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Context(80, 9)
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["percent"].(float64) != 80 || m["elapsed"].(float64) != 9 {
			t.Fatalf("context json: %v", m)
		}
	})
}

func TestVerdict(t *testing.T) {
	t.Run("softFailPrints", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Verdict("critic", false, false) // matched=false required=false → soft fail
		s := buf.String()
		if !strings.Contains(s, "verdict: FAIL") || !strings.Contains(s, "soft") {
			t.Fatalf("soft fail should print:\n%s", s)
		}
		assertNoEsc(t, &buf)
	})
	t.Run("requiredFailSilent", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Verdict("critic", false, true) // required fail → handled by step_done
		if buf.String() != "" {
			t.Fatalf("required fail should be silent in human mode (step_done reports it): %q", buf.String())
		}
	})
	t.Run("passSilent", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Verdict("critic", true, false)
		if buf.String() != "" {
			t.Fatalf("passing verdict should be silent in human mode: %q", buf.String())
		}
	})
	t.Run("quietSilent", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true})
		r.Verdict("critic", false, false)
		if buf.String() != "" {
			t.Fatalf("quiet verdict: %q", buf.String())
		}
	})
	t.Run("jsonAlwaysEmits", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Verdict("critic", true, true)
		r.Verdict("critic", false, false)
		r.Verdict("critic", false, true)
		lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
		if len(lines) != 3 {
			t.Fatalf("expected 3 verdict events, got %d: %s", len(lines), buf.String())
		}
		for _, l := range lines {
			var m map[string]any
			if err := json.Unmarshal(l, &m); err != nil {
				t.Fatal(err)
			}
			if m["type"] != "verdict" || m["name"] != "critic" {
				t.Fatalf("verdict json: %v", m)
			}
		}
	})
}

func TestGateDetail(t *testing.T) {
	t.Run("short", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.GateDetail("line1\nline2\n")
		s := buf.String()
		if !strings.Contains(s, "line1") || !strings.Contains(s, "line2") {
			t.Fatalf("gateDetail short: %s", s)
		}
		if strings.Contains(s, "…") {
			t.Fatalf("short gateDetail should not truncate: %s", s)
		}
		assertNoEsc(t, &buf)
	})
	t.Run("longTruncates", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		in := strings.Repeat("L\n", 20)
		r.GateDetail(in)
		s := buf.String()
		if !strings.Contains(s, "…") {
			t.Fatalf("long gateDetail should truncate with ellipsis: %s", s)
		}
		// Only the first 6 lines + ellipsis appear.
		if got := strings.Count(s, "L"); got > 7 {
			t.Fatalf("too many lines kept: %d\n%s", got, s)
		}
	})
	t.Run("quiet", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true})
		r.GateDetail("x\n")
		if buf.String() != "" {
			t.Fatalf("quiet gateDetail: %q", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.GateDetail("out\n")
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["out"] != "out\n" {
			t.Fatalf("gateDetail json: %v", m)
		}
	})
}

func TestAssistant(t *testing.T) {
	t.Run("verbose", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Verbose: true})
		r.Assistant("here is some model text")
		if !strings.Contains(buf.String(), "here is some model text") {
			t.Fatalf("assistant verbose: %s", buf.String())
		}
		assertNoEsc(t, &buf)
	})
	t.Run("notVerbose", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Assistant("hidden")
		if buf.String() != "" {
			t.Fatalf("assistant non-verbose should be empty: %q", buf.String())
		}
	})
	t.Run("quiet", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true, Verbose: true})
		r.Assistant("x")
		if buf.String() != "" {
			t.Fatalf("quiet assistant: %q", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Assistant("hi")
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["text"] != "hi" {
			t.Fatalf("assistant json: %v", m)
		}
	})
}

func TestPausedResumed(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Paused()
		r.Resumed()
		s := buf.String()
		if !strings.Contains(s, "PAUSED") || !strings.Contains(s, "resume to continue") {
			t.Fatalf("paused: %s", s)
		}
		if !strings.Contains(s, "resumed") {
			t.Fatalf("resumed: %s", s)
		}
		assertNoEsc(t, &buf)
	})
	t.Run("quiet", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true})
		r.Paused()
		r.Resumed()
		if buf.String() != "" {
			t.Fatalf("quiet paused/resumed: %q", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Paused()
		r.Resumed()
		lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
		if len(lines) != 2 {
			t.Fatalf("expected 2 events: %s", buf.String())
		}
		var p, rs map[string]any
		json.Unmarshal(lines[0], &p)
		json.Unmarshal(lines[1], &rs)
		if p["type"] != "paused" || rs["type"] != "resumed" {
			t.Fatalf("paused/resumed json: %v %v", p, rs)
		}
	})
}

func TestWarn(t *testing.T) {
	t.Run("humanToErr", func(t *testing.T) {
		var out, errb bytes.Buffer
		r := New(Options{Out: &out, Err: &errb, Color: false})
		r.Warn("something fishy")
		if out.String() != "" {
			t.Fatalf("warn should not go to stdout in human mode: %q", out.String())
		}
		if !strings.Contains(errb.String(), "warn") || !strings.Contains(errb.String(), "something fishy") {
			t.Fatalf("warn to stderr: %q", errb.String())
		}
		assertNoEsc(t, &errb)
	})
	t.Run("humanNilErrFallsToOut", func(t *testing.T) {
		var out bytes.Buffer
		r := New(Options{Out: &out, Color: false})
		r.Warn("no err writer")
		// Falls back to out when err is nil.
		if !strings.Contains(out.String(), "no err writer") {
			t.Fatalf("warn nil-err should fall back to out: %q", out.String())
		}
	})
	t.Run("quietStillWarns", func(t *testing.T) {
		var out, errb bytes.Buffer
		r := New(Options{Out: &out, Err: &errb, Color: false, Quiet: true})
		r.Warn("still surfaces in quiet")
		if !strings.Contains(errb.String(), "still surfaces in quiet") {
			t.Fatalf("warn should fire even in quiet: %q", errb.String())
		}
	})
	t.Run("jsonToOut", func(t *testing.T) {
		var out, errb bytes.Buffer
		r := New(Options{Out: &out, Err: &errb, Color: false, JSON: true})
		r.Warn("inline event")
		if errb.String() != "" {
			t.Fatalf("json warn should not touch stderr: %q", errb.String())
		}
		var m map[string]any
		if err := json.Unmarshal(out.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "warn" || m["msg"] != "inline event" {
			t.Fatalf("warn json: %v", m)
		}
	})
}

func TestSummary(t *testing.T) {
	results := []string{"success", "fail", "stopped", "done"}
	for _, res := range results {
		t.Run(res, func(t *testing.T) {
			var buf bytes.Buffer
			r := New(Options{Out: &buf, Color: false})
			r.Summary(Summary{Elapsed: 0, Iterations: 2, MaxIter: 5, Result: res, Branch: "loop/x"})
			s := buf.String()
			if !strings.Contains(s, strings.ToUpper(res)) {
				t.Fatalf("summary %s missing label: %s", res, s)
			}
			if !strings.Contains(s, "loop/x") {
				t.Fatalf("summary %s missing branch: %s", res, s)
			}
			if !strings.Contains(s, "2/5") {
				t.Fatalf("summary %s missing iteration ratio: %s", res, s)
			}
			assertNoEsc(t, &buf)
		})
	}
	t.Run("noBranch", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Summary(Summary{Result: "done", Iterations: 1, MaxIter: 1})
		if strings.Contains(buf.String(), "branch") {
			t.Fatalf("summary should omit branch when empty: %s", buf.String())
		}
	})
	t.Run("quiet", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, Quiet: true})
		r.Summary(Summary{Result: "success"})
		if buf.String() != "" {
			t.Fatalf("quiet summary: %q", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Summary(Summary{Elapsed: 0, Iterations: 1, MaxIter: 3, Result: "fail", Branch: "loop/b"})
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["result"] != "fail" || m["iterations"].(float64) != 1 || m["branch"] != "loop/b" {
			t.Fatalf("summary json: %v", m)
		}
	})
}

func TestFinalLines(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Success(3, "state/x")
		s := buf.String()
		if !strings.Contains(s, "SUCCESS") || !strings.Contains(s, "pass 3") || !strings.Contains(s, "state/x") {
			t.Fatalf("success: %s", s)
		}
		assertNoEsc(t, &buf)
	})
	t.Run("stopped", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Stopped("state/y")
		if !strings.Contains(buf.String(), "STOPPED") || !strings.Contains(buf.String(), "state/y") {
			t.Fatalf("stopped: %s", buf.String())
		}
		assertNoEsc(t, &buf)
	})
	t.Run("fail", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Fail("state/z")
		if !strings.Contains(buf.String(), "FAILED") || !strings.Contains(buf.String(), "state/z") {
			t.Fatalf("fail: %s", buf.String())
		}
		assertNoEsc(t, &buf)
	})
	t.Run("done", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false})
		r.Done("state/d")
		if !strings.Contains(buf.String(), "DONE") || !strings.Contains(buf.String(), "state/d") {
			t.Fatalf("done: %s", buf.String())
		}
		assertNoEsc(t, &buf)
	})
	t.Run("jsonEach", func(t *testing.T) {
		var buf bytes.Buffer
		r := New(Options{Out: &buf, Color: false, JSON: true})
		r.Success(1, "s")
		r.Stopped("s2")
		r.Fail("s3")
		r.Done("s4")
		lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
		if len(lines) != 4 {
			t.Fatalf("expected 4 final events: %s", buf.String())
		}
		want := []string{"success", "stopped", "fail", "done"}
		for i, l := range lines {
			var m map[string]any
			if err := json.Unmarshal(l, &m); err != nil {
				t.Fatal(err)
			}
			if m["type"] != want[i] {
				t.Fatalf("final[%d] json type %v want %s", i, m, want[i])
			}
		}
	})
}

func TestFinalLinesQuietStillEmit(t *testing.T) {
	// Success/Stoed/Fail/Done print even in quiet mode (they are the result,
	// not progress). Only Summary is quiet-suppressed.
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false, Quiet: true})
	r.Success(1, "state/x")
	if !strings.Contains(buf.String(), "SUCCESS") {
		t.Fatalf("quiet should still print final result: %q", buf.String())
	}
}

func TestEmitMarshalErrorSwallowed(t *testing.T) {
	// emit must not panic on an unmarshallable value (e.g. a chan). It
	// silently returns, since this is a best-effort progress stream.
	r := New(Options{Out: &bytes.Buffer{}, Color: false, JSON: true})
	r.emit(make(chan int)) // must not panic
}
