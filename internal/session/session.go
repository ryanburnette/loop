// Package session decides session policy actions and writes handoff files.
package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// Handoff is the runner-authored context for the next turn.
type Handoff struct {
	Goal           string
	Constraints    string
	LastGate       string
	LastGateOK     bool
	LastGateLog    string
	DiffStat       string
	SessionPolicy  string
	TurnsInSession int
	ContextPercent int
	ContextKnown   bool
	Compacted      bool
	Frozen         string
}

const (
	maxLogBytes  = 12_000
	headLogBytes = 6_000
	tailLogBytes = 4_000
)

// WriteHandoff writes handoff.md.
func WriteHandoff(path string, h Handoff) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Handoff\n\n")

	b.WriteString("## Goal\n\n")
	if h.Goal != "" {
		b.WriteString(h.Goal)
		b.WriteString("\n\n")
	} else {
		b.WriteString("(none)\n\n")
	}

	if h.Constraints != "" {
		b.WriteString("## Constraints\n\n")
		b.WriteString(h.Constraints)
		if !strings.HasSuffix(h.Constraints, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Last gate\n\n")
	if h.LastGate != "" {
		status := "FAIL"
		if h.LastGateOK {
			status = "OK"
		}
		fmt.Fprintf(&b, "- name: %s\n- status: %s\n\n", h.LastGate, status)
	}
	if h.LastGateLog != "" {
		b.WriteString("```\n")
		b.WriteString(truncateLog(h.LastGateLog))
		if !strings.HasSuffix(h.LastGateLog, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	}

	b.WriteString("## Diff stat\n\n")
	if h.DiffStat != "" {
		b.WriteString("```\n")
		b.WriteString(h.DiffStat)
		if !strings.HasSuffix(h.DiffStat, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	} else {
		b.WriteString("(clean)\n\n")
	}

	b.WriteString("## Session\n\n")
	fmt.Fprintf(&b, "- policy: %s\n", h.SessionPolicy)
	fmt.Fprintf(&b, "- turns this session: %d\n", h.TurnsInSession)
	fmt.Fprintf(&b, "- context percent: %s\n", contextPercentText(h))
	fmt.Fprintf(&b, "- compacted: %v\n\n", h.Compacted)

	b.WriteString("## Frozen\n\n")
	if h.Frozen != "" {
		fmt.Fprintf(&b, "frozen: %s\n", h.Frozen)
	} else {
		b.WriteString("frozen: not configured\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// contextPercentText is n/a when there is no session to probe, unknown when
// a probe did not return a number, and the number only when it is known.
// Unknown must not render as 0.
func contextPercentText(h Handoff) string {
	if h.SessionPolicy == "" || h.SessionPolicy == string(config.SessionNone) {
		return "n/a"
	}
	if !h.ContextKnown {
		return "unknown"
	}
	return strconv.Itoa(h.ContextPercent)
}

func truncateLog(s string) string {
	if len(s) <= maxLogBytes {
		return s
	}
	head := s[:headLogBytes]
	tail := s[len(s)-tailLogBytes:]
	return head + "\n\n… truncated …\n\n" + tail
}
