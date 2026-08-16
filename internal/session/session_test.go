package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanburnette/loop/internal/config"
)

func TestNoneAlwaysFresh(t *testing.T) {
	p := Policy{
		Mode:         config.SessionNone,
		SessionTurns: 4,
		ForkPercent:  40,
	}
	d := p.Decide(State{TurnsThisSession: 0, ContextPercent: 10, Compacted: false})
	if d.Action != New || d.UseSession {
		t.Fatalf("none should start fresh with no session: %+v", d)
	}
	d = p.Decide(State{TurnsThisSession: 3, ContextPercent: 10, Compacted: false})
	if d.Action != New || d.UseSession {
		t.Fatalf("none stays fresh: %+v", d)
	}
}

func TestSharedUntilCap(t *testing.T) {
	p := Policy{
		Mode:         config.SessionShared,
		SessionTurns: 4,
		ForkPercent:  40,
	}
	d := p.Decide(State{TurnsThisSession: 0, HasSession: false})
	if d.Action != New || !d.UseSession {
		t.Fatalf("first shared: %+v", d)
	}
	d = p.Decide(State{TurnsThisSession: 2, HasSession: true})
	if d.Action != Continue {
		t.Fatalf("continue: %+v", d)
	}
	d = p.Decide(State{TurnsThisSession: 4, HasSession: true})
	if d.Action != New {
		t.Fatalf("turn cap should open a new session: %+v", d)
	}
}

func TestForkOnPercent(t *testing.T) {
	p := Policy{
		Mode:         config.SessionFork,
		SessionTurns: 8,
		ForkPercent:  40,
	}
	d := p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 41})
	if d.Action != Fork {
		t.Fatalf("want fork at 41%%: %+v", d)
	}
	d = p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 10})
	if d.Action != Continue {
		t.Fatalf("want continue at 10%%: %+v", d)
	}
}

func TestCompactedForcesNew(t *testing.T) {
	for _, mode := range []config.SessionMode{config.SessionShared, config.SessionFork} {
		p := Policy{Mode: mode, SessionTurns: 8, ForkPercent: 90}
		d := p.Decide(State{TurnsThisSession: 1, HasSession: true, Compacted: true})
		if d.Action != New {
			t.Fatalf("%s compacted should start a new session, got %+v", mode, d)
		}
	}
}

func TestWriteHandoff(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "handoff.md")
	err := WriteHandoff(p, Handoff{
		Goal:           "implement loop2",
		Constraints:    "do not edit tests",
		LastGate:       "tests",
		LastGateOK:     false,
		LastGateLog:    "FAIL: TestParse",
		DiffStat:       " internal/manifest/manifest.go | 10 ++++++++++",
		SessionPolicy:  "none",
		TurnsInSession: 1,
		ContextPercent: 12,
		Compacted:      false,
		Frozen:         "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"implement loop2",
		"do not edit tests",
		"FAIL: TestParse",
		"internal/manifest/manifest.go",
		"policy: none",
		"frozen: ok",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("handoff missing %q\n%s", want, s)
		}
	}
}

func TestHandoffTruncatesHugeLog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "handoff.md")
	log := strings.Repeat("x", 80_000)
	if err := WriteHandoff(p, Handoff{LastGateLog: log}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 20_000 {
		t.Fatalf("handoff too large: %d", len(b))
	}
	if !strings.Contains(string(b), "truncated") {
		t.Fatal("expected truncation marker")
	}
}
