package manifest

import (
	"strings"
	"testing"
)

// Key tokens may be separated by tabs as well as spaces. Cutting on a literal
// " " swallowed a tab-delimited line whole: model= took the rest of the line
// and the verdict vanished, silently turning an objective loop into one with
// no stopping rule but the cap.
func TestKeyTokensAcceptTabs(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest",
		"turn\treviewer\tprompts/r.md\tmodel=reviewer\trequired=0\tverdict=^VERDICT: PASS\\b\n")

	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := m.Steps[0]
	if s.Model != "reviewer" {
		t.Fatalf("model: %q", s.Model)
	}
	if s.Required {
		t.Fatal("required=0 was swallowed")
	}
	if s.Verdict != `^VERDICT: PASS\b` {
		t.Fatalf("verdict: %q", s.Verdict)
	}
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}
}

// An unrecognized key parses fine but changes what the loop does, so it is
// reported rather than dropped.
func TestUnknownKeyWarns(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest", "turn writer prompts/w.md requried=0\n")

	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Steps[0].Required {
		t.Fatal("a misspelled required= must not take effect")
	}
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "requried") {
		t.Fatalf("want a warning naming the bad key, got %v", m.Warnings)
	}
	if !strings.HasPrefix(m.Warnings[0], "manifest:1:") {
		t.Fatalf("warning should carry the line number: %q", m.Warnings[0])
	}
}

func TestKnownKeysDoNotWarn(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest", "turn writer prompts/w.md model=x required=0\ngate g g.sh\n")
	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}
}
