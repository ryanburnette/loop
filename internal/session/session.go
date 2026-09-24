// Package session decides session policy actions.
package session

import (
	"github.com/ryanburnette/loop/internal/config"
)

// Action is what to do for the next turn's session.
type Action int

const (
	// New opens a fresh empty session (or no session). A percent cut is
	// New: pi --fork would copy the transcript into that session.
	New Action = iota
	// Continue reuses the current session id.
	Continue
)

// Policy is the session decision configuration.
type Policy struct {
	Mode         config.SessionMode
	SessionTurns int
	ForkPercent  int
}

// State is the current session counters/flags before a turn.
type State struct {
	TurnsThisSession int
	ContextPercent   int
	// ContextKnown is false when the last probe did not return a percent.
	// A stored 0 with ContextKnown false is unknown, not an empty window.
	ContextKnown bool
	Compacted    bool
	HasSession   bool
}

// Decision is the outcome of Policy.Decide.
type Decision struct {
	Action     Action
	UseSession bool
}

// Decide picks the next session action.
func (p Policy) Decide(s State) Decision {
	switch p.Mode {
	case config.SessionNone:
		return Decision{Action: New, UseSession: false}
	case config.SessionShared:
		if s.Compacted {
			return Decision{Action: New, UseSession: true}
		}
		if !s.HasSession {
			return Decision{Action: New, UseSession: true}
		}
		if p.SessionTurns > 0 && s.TurnsThisSession >= p.SessionTurns {
			return Decision{Action: New, UseSession: true}
		}
		return Decision{Action: Continue, UseSession: true}
	case config.SessionFork:
		if s.Compacted {
			return Decision{Action: New, UseSession: true}
		}
		if !s.HasSession {
			return Decision{Action: New, UseSession: true}
		}
		if p.SessionTurns > 0 && s.TurnsThisSession >= p.SessionTurns {
			return Decision{Action: New, UseSession: true}
		}
		// Unknown never cuts. A known 0 does not cut at the default of 40.
		// ForkPercent <= 0 disables the cut, including at a known 100.
		if s.ContextKnown && p.ForkPercent > 0 && s.ContextPercent >= p.ForkPercent {
			return Decision{Action: New, UseSession: true}
		}
		return Decision{Action: Continue, UseSession: true}
	default:
		return Decision{Action: New, UseSession: false}
	}
}
