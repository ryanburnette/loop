package session

import (
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
	d = p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 100, ContextKnown: true})
	if d.Action != Continue {
		t.Fatalf("shared ignores percent: %+v", d)
	}
}

func TestForkOnPercent(t *testing.T) {
	p := Policy{
		Mode:         config.SessionFork,
		SessionTurns: 8,
		ForkPercent:  40,
	}
	// Known 41 cuts to a new empty session. The runner must not set ForkID
	// or pass pi --fork; that would copy the transcript.
	d := p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 41, ContextKnown: true})
	if d.Action != New || !d.UseSession {
		t.Fatalf("known 41 should cut to a new session: %+v", d)
	}
	d = p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 10, ContextKnown: true})
	if d.Action != Continue {
		t.Fatalf("known 10 should continue: %+v", d)
	}
	d = p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 0, ContextKnown: false})
	if d.Action != Continue {
		t.Fatalf("unknown should continue: %+v", d)
	}
	d = p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 0, ContextKnown: true})
	if d.Action != Continue {
		t.Fatalf("known 0 at default 40 should continue: %+v", d)
	}
	p.ForkPercent = 0
	d = p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 100, ContextKnown: true})
	if d.Action != Continue {
		t.Fatalf("ForkPercent 0 should continue at known 100: %+v", d)
	}
	p.ForkPercent = -1
	d = p.Decide(State{TurnsThisSession: 1, HasSession: true, ContextPercent: 100, ContextKnown: true})
	if d.Action != Continue {
		t.Fatalf("ForkPercent < 0 should continue at known 100: %+v", d)
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
