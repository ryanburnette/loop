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
	if !strings.Contains(res.Text, "hello from fake-pi") {
		t.Fatalf("text: %q", res.Text)
	}
	if res.ContextPercent != 12 {
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
	if !strings.Contains(res.Text, "fixed the loader") {
		t.Fatalf("text: %q", res.Text)
	}
	if res.LastTool != "edit" {
		t.Fatalf("last tool: %q", res.LastTool)
	}
	if res.ContextPercent != 18 {
		t.Fatalf("percent: %d", res.ContextPercent)
	}
}
