package pi

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakePI(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	// internal/pi -> repo root testdata/fake-pi
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	p := filepath.Join(root, "testdata", "fake-pi")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fake-pi missing at %s: %v", p, err)
	}
	return p
}

func TestArgvPrintJSON(t *testing.T) {
	args := Argv(Request{
		PiPath:     "pi",
		Model:      "xai/grok-4.5",
		SessionID:  "abc",
		SessionDir: "/tmp/sess",
		Approve:    true,
		PromptFile: "/abs/prompt.md",
		Handoff:    "/abs/handoff.md",
		Context:    "fix it",
	})
	got := strings.Join(args, " ")
	for _, want := range []string{
		"pi", "-p", "--mode json", "--model xai/grok-4.5",
		"--session-id abc", "--session-dir /tmp/sess",
		"--approve", "@/abs/prompt.md", "@/abs/handoff.md", "fix it",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("argv missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "--no-session") {
		t.Fatal("shared session should not pass --no-session")
	}
}

func TestArgvContextAfterEndOfFlags(t *testing.T) {
	args := Argv(Request{
		PiPath:     "pi",
		SessionID:  "abc",
		SessionDir: "/tmp/sess",
		PromptFile: "/abs/prompt.md",
		Handoff:    "/abs/handoff.md",
		Context:    "--no-session",
	})
	dash := -1
	for i, a := range args {
		if a == "--" {
			dash = i
			break
		}
	}
	if dash < 0 {
		t.Fatalf("argv missing --: %q", args)
	}
	sessionBefore := false
	before, after := 0, 0
	for i, a := range args {
		if a == "--session-id" && i < dash {
			sessionBefore = true
		}
		if a == "--no-session" {
			if i < dash {
				before++
			} else if i > dash {
				after++
			}
		}
	}
	if !sessionBefore {
		t.Fatalf("--session-id not before --: %q", args)
	}
	if before != 0 || after != 1 {
		t.Fatalf("--no-session before=%d after=%d, want 0 and 1: %q", before, after, args)
	}
	for _, want := range []string{"@/abs/prompt.md", "@/abs/handoff.md"} {
		at := -1
		for i, a := range args {
			if a == want {
				at = i
				break
			}
		}
		if at <= dash {
			t.Fatalf("%s not after --: %q", want, args)
		}
	}
}

func TestArgvNoSession(t *testing.T) {
	args := Argv(Request{PiPath: "pi", PromptFile: "/p.md"})
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--no-session") {
		t.Fatalf("none policy should pass --no-session: %s", got)
	}
}

func TestRunExtractsTextAndUsage(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "turn.md")
	res, err := Run(Request{
		PiPath:     fakePI(t),
		PromptFile: "/p.md",
		WorkRoot:   dir,
		StdoutFile: out,
		JSONLFile:  out + ".jsonl",
		StderrFile: out + ".err",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello from fake-pi" {
		t.Fatalf("text: %q", res.Text)
	}
	if len(res.Messages) != 1 || res.Messages[0] != "hello from fake-pi" {
		t.Fatalf("messages: %#v", res.Messages)
	}
	// fake-pi still emits session_status. The json stream must not believe it.
	if res.ContextPercent != 0 {
		t.Fatalf("context percent: %d", res.ContextPercent)
	}
	if res.LastTool != "bash" {
		t.Fatalf("last tool: %q", res.LastTool)
	}
	if res.Compacted {
		t.Fatal("should not be compacted")
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "hello from fake-pi") {
		t.Fatalf("turn file: %s", b)
	}
}

func TestRunDetectsCompaction(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "turn.md")
	t.Setenv("FAKE_PI_COMPACT", "1")
	res, err := Run(Request{
		PiPath:     fakePI(t),
		PromptFile: "/p.md",
		WorkRoot:   dir,
		StdoutFile: out,
		JSONLFile:  out + ".jsonl",
		StderrFile: out + ".err",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compacted {
		t.Fatal("expected compacted")
	}
}

func TestParseFixture(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "events", "turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseJSONL(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "fixed the loader" {
		t.Fatalf("text: %q", res.Text)
	}
	if len(res.Messages) != 1 || res.Messages[0] != "fixed the loader" {
		t.Fatalf("messages: %#v", res.Messages)
	}
	if res.LastTool != "edit" {
		t.Fatalf("last tool: %q", res.LastTool)
	}
	// The fixture still emits session_status. That event must not set percent.
	if res.ContextPercent != 0 {
		t.Fatalf("percent: %d", res.ContextPercent)
	}
}

func TestParseMultiMessageKeepsEveryAssistantBlock(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "events", "turn-multimessage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	res, err := parseStream(strings.NewReader(string(raw)), func(ev Event) {
		if ev.TextDelta != "" {
			deltas = append(deltas, ev.TextDelta)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 || deltas[0] != "VERDICT: PASS" {
		t.Fatalf("onEvent deltas: %#v", deltas)
	}
	first := "VERDICT: PASS\nchecked the loader"
	second := "edited loader.go"
	if len(res.Messages) != 2 || res.Messages[0] != first || res.Messages[1] != second {
		t.Fatalf("messages: %#v", res.Messages)
	}
	want := first + "\n\n---\n\n" + second
	if res.Text != want {
		t.Fatalf("text: %q", res.Text)
	}
	// The delta is a prefix of the first message. Twice means it was concatenated.
	if strings.Count(res.Text, "VERDICT: PASS") != 1 {
		t.Fatalf("verdict count: %q", res.Text)
	}
	if res.ContextPercent != 0 {
		t.Fatalf("percent: %d", res.ContextPercent)
	}
}
