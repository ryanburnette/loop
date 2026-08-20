package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePi(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pi")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

const oneTextEvent = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"partial"}]}}`

// A nonzero exit is a failed turn even when pi emitted text first. Treating
// it as clean lets the loop score an iteration on work that did not finish.
func TestNonZeroExitIsAnError(t *testing.T) {
	state := t.TempDir()
	bin := writePi(t, "#!/bin/sh\nprintf '%s\\n' '"+oneTextEvent+"'\nexit 3\n")

	res, err := Run(Request{
		PiPath:     bin,
		StdoutFile: filepath.Join(state, "turn.md"),
		StderrFile: filepath.Join(state, "turn.err"),
	})
	if err == nil {
		t.Fatal("want an error for a nonzero pi exit")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("error should name the exit code: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode=%d want 3", res.ExitCode)
	}
	if res.Text != "partial" {
		t.Fatalf("text should still be parsed for the turn file: %q", res.Text)
	}
}

func TestZeroExitWithTextIsFine(t *testing.T) {
	state := t.TempDir()
	bin := writePi(t, "#!/bin/sh\nprintf '%s\\n' '"+oneTextEvent+"'\nexit 0\n")

	res, err := Run(Request{
		PiPath:     bin,
		StdoutFile: filepath.Join(state, "turn.md"),
		StderrFile: filepath.Join(state, "turn.err"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "partial" {
		t.Fatalf("text=%q", res.Text)
	}
}
