package mend

import (
	"fmt"
	"strings"
	"time"

	"github.com/ryanburnette/loop/internal/manifest"
)

// Result values written to return.md and meta.env RESULT=.
// stalled and recipe are renderable so a later stop can use the same page.
// This package does not decide when those stops fire.
const (
	ResultRunning = "running"
	ResultSuccess = "success"
	ResultFail    = "fail"
	ResultStopped = "stopped"
	ResultDone    = "done"
	ResultStalled = "stalled"
	ResultRecipe  = "recipe"
)

// Assurance is decided once at startup from the manifest. It is not SUCCESS.
const (
	AssuranceGated      = "gated"
	AssuranceCrossModel = "cross-model"
	AssuranceSelfGraded = "self-graded"
	AssuranceVerdict    = "verdict"
	AssuranceNone       = "none"
)

// GatedGloss is the startup sentence for a required gate, including loop:frozen.
// It must not be read as "a gate passed" or "a gate exited 0".
const GatedGloss = "This loop has a required gate. The runner does not inspect what that gate checks. Still red and Passed are the exit codes."

// SelfGradedGloss and VerdictGloss are what the return says when the only
// check is the model agreeing with itself.
const (
	SelfGradedGloss = "This loop's only check is the model agreeing with itself."
	VerdictGloss    = "This loop's only check is the model agreeing with itself. It is a grep of the model's own prose."
)

// SelfCheckWarn is the startup warning for self-graded and verdict.
const SelfCheckWarn = "this loop's only check is the model agreeing with itself, and the return will say so"

// Page is one rendering of return.md. BranchText is "branch loop/<id>" when
// the run creates a branch, and "the current branch" when it does not.
// The header Branch field is the name to print, which may be a git branch.
type Page struct {
	Result     string
	Assurance  string
	Gloss      string
	Iter       int
	MaxIter    int
	Elapsed    time.Duration
	Branch     string
	BranchText string
	Workroot   string
	Head       string
	Checks     []Check
	Diff       Diff
	Mend       string
	GateLog    string
	LastTurn   string

	// Stall and recipe fields are named in the Next paragraph. They are
	// empty for the results the runner writes today.
	StallSignature string
	StallHead      string
	RecipeNoTurn   bool
	RecipeGate     string
	RecipeCode     int
	RecipeIter     int
	RecipeBranch   string
}

// DecideAssurance classifies the manifest. resolve returns the model string
// the runner will pass to pi. An empty string is unknown: the runner does
// not look up pi's default. An acting turn is any turn without scorecard=.
// A required gate, including loop:frozen, is gated even when a model string
// is empty.
func DecideAssurance(man *manifest.Manifest, resolve func(role string) string) (string, string) {
	if resolve == nil {
		resolve = func(string) string { return "" }
	}
	if man == nil {
		return AssuranceNone, ""
	}
	var actors []string
	var judges []string
	requiredGate := false
	requiredVerdict := false
	for _, s := range man.Steps {
		switch s.Type {
		case manifest.Gate:
			if s.Required {
				requiredGate = true
			}
		case manifest.Turn:
			if s.Scorecard != "" {
				if s.Required {
					judges = append(judges, resolve(s.Model))
				}
				continue
			}
			actors = append(actors, resolve(s.Model))
			if s.Verdict != "" && s.Required {
				requiredVerdict = true
			}
		}
	}
	if requiredGate {
		return AssuranceGated, GatedGloss
	}
	if len(judges) > 0 {
		if crossModel(actors, judges) {
			return AssuranceCrossModel, ""
		}
		return AssuranceSelfGraded, SelfGradedGloss
	}
	if requiredVerdict {
		return AssuranceVerdict, VerdictGloss
	}
	return AssuranceNone, ""
}

// crossModel is the level-3 check: at least one acting turn, every compared
// model string non-empty, and each required scorecard different from every
// acting turn. Empty is not a distinct model.
func crossModel(actors, judges []string) bool {
	if len(actors) == 0 || len(judges) == 0 {
		return false
	}
	for _, m := range actors {
		if m == "" {
			return false
		}
	}
	for _, j := range judges {
		if j == "" {
			return false
		}
		for _, a := range actors {
			if j == a {
				return false
			}
		}
	}
	return true
}

// RenderReturn writes one screen. No gate log is pasted in.
func RenderReturn(p Page) string {
	var b strings.Builder
	b.WriteString("# Return\n\n")
	fmt.Fprintf(&b, "result: %s\n", p.Result)
	b.WriteString(assuranceLine(p))
	b.WriteByte('\n')
	fmt.Fprintf(&b, "iteration: %d/%d\n", p.Iter, p.MaxIter)
	fmt.Fprintf(&b, "elapsed: %s\n", FormatElapsed(p.Elapsed))
	branch := p.Branch
	if branch == "" {
		branch = "(none)"
	}
	fmt.Fprintf(&b, "branch: %s\n", branch)
	fmt.Fprintf(&b, "workroot: %s\n", p.Workroot)
	head := p.Head
	if head == "" {
		head = "unknown"
	}
	fmt.Fprintf(&b, "head: %s\n", head)

	b.WriteString("\n## Still red\n\n")
	writeReturnChecks(&b, p.Checks, false)
	b.WriteString("\n## Passed\n\n")
	writeReturnChecks(&b, p.Checks, true)
	b.WriteString("\n## Diff\n\n")
	writeReturnDiff(&b, p.Diff)
	b.WriteString("\n## Read this\n\n")
	fmt.Fprintf(&b, "- mend: %s\n", emptyDash(p.Mend))
	fmt.Fprintf(&b, "- gate log: %s\n", emptyDash(p.GateLog))
	if p.LastTurn != "" {
		fmt.Fprintf(&b, "- last turn: %s\n", p.LastTurn)
	}
	b.WriteString("\n## Next\n\n")
	b.WriteString(Next(p))
	b.WriteByte('\n')
	return b.String()
}

func assuranceLine(p Page) string {
	if p.Gloss == "" {
		return "assurance: " + p.Assurance
	}
	return "assurance: " + p.Assurance + " — " + p.Gloss
}

// Next is the closing paragraph. The words are the design table; branch text
// is "branch loop/<id>" or "the current branch". Stall names the signature
// and the HEAD. Recipe names the condition that failed.
func Next(p Page) string {
	where := p.BranchText
	if where == "" {
		where = "the current branch"
	}
	switch p.Result {
	case ResultRunning:
		return fmt.Sprintf("The run is in progress at iteration %d of %d. The phase is the status line. Nothing has been merged.", p.Iter, p.MaxIter)
	case ResultSuccess:
		return fmt.Sprintf("Required checks passed on iteration %d. Nothing was merged. The work is on %s. Read the diff, not the model's summary.", p.Iter, where)
	case ResultFail:
		return fmt.Sprintf("The cap was spent with a required check still red. Nothing was merged.\nThe work is on %s. Read the diff, not the model's summary.", where)
	case ResultStopped:
		return fmt.Sprintf("The operator stopped the run during iteration %d (control file or signal). Nothing was merged. The work is on %s if one was created.", p.Iter, where)
	case ResultDone:
		return "The cap was reached. This loop had no required check, so finishing the cap is not a pass. Nothing was merged."
	case ResultStalled:
		prev := p.Iter - 1
		if prev < 0 {
			prev = 0
		}
		sig := p.StallSignature
		if sig == "" {
			sig = "(none)"
		}
		head := p.StallHead
		if head == "" {
			head = "unknown"
		}
		return fmt.Sprintf("Stopped after iteration %d because the failure signature and the tree matched iteration %d. The signature is %s. HEAD %s. Nothing was merged. LOOP_STALL=continue is how this recipe keeps going.", p.Iter, prev, sig, head)
	case ResultRecipe:
		var b strings.Builder
		b.WriteString("The recipe failed.")
		if p.RecipeNoTurn {
			b.WriteString(" No pi turn ran.")
		}
		if p.RecipeGate != "" {
			fmt.Fprintf(&b, " Required gate %s exited %d on iteration %d.", p.RecipeGate, p.RecipeCode, p.RecipeIter)
		}
		if p.RecipeBranch != "" {
			fmt.Fprintf(&b, " Branch setup created %s. Preflight runs after branch setup, and a preflight failure does not delete that branch.", p.RecipeBranch)
		}
		return b.String()
	default:
		return ""
	}
}

// FormatElapsed matches the return page: 2h14m, 14m3s, or 45s.
func FormatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

func writeReturnChecks(b *strings.Builder, checks []Check, passed bool) {
	n := 0
	for _, c := range checks {
		ok := c.OK && !c.Unreadable
		if ok != passed {
			continue
		}
		fmt.Fprintf(b, "%s\n", returnCheckLine(c))
		n++
	}
	if n == 0 {
		b.WriteString("(none)\n")
	}
}

func returnCheckLine(c Check) string {
	if c.Kind == "scorecard" {
		req := "soft"
		if c.Required {
			req = "required"
		}
		status := "PASS"
		switch {
		case c.Unreadable:
			status = "UNREADABLE"
		case !c.OK:
			status = "FAIL"
		}
		s := fmt.Sprintf("- scorecard %s: %s", c.Name, status)
		if c.Render != "" {
			s += " " + c.Render
		}
		s += " (" + req + ")"
		if !c.OK {
			if ev := scoreEvidence(c); ev != "" {
				s += " — " + ev
			}
		}
		return s
	}
	status := "OK"
	if !c.OK {
		status = "FAIL"
	}
	s := fmt.Sprintf("- gate %s: %s exit %d", c.Name, status, c.Exit)
	if !c.OK {
		if ev := firstTail(c.Tail); ev != "" {
			s += " — " + ev
		}
	}
	return s
}

func scoreEvidence(c Check) string {
	if c.Unreadable && c.Error != "" {
		return c.Error
	}
	var parts []string
	for _, m := range c.Marks {
		if m.Label == "met" {
			continue
		}
		if m.Because != "" {
			parts = append(parts, fmt.Sprintf("%s %s: %s", m.ID, m.Label, m.Because))
		} else if m.Label != "" {
			parts = append(parts, fmt.Sprintf("%s %s", m.ID, m.Label))
		} else {
			parts = append(parts, m.ID)
		}
	}
	return strings.Join(parts, "; ")
}

func firstTail(lines []string) string {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func writeReturnDiff(b *strings.Builder, d Diff) {
	writeDiffPart(b, "committed", d.Committed, d.CommittedUnknown)
	b.WriteByte('\n')
	writeDiffPart(b, "staged", d.Staged, false)
	b.WriteByte('\n')
	writeDiffPart(b, "unstaged", d.Unstaged, false)
	if d.Omitted > 0 {
		fmt.Fprintf(b, "\nomitted lines: %d\n", d.Omitted)
	}
}

func writeDiffPart(b *strings.Builder, name, stat string, unknown bool) {
	if unknown {
		// Same words as the mend. An empty worktree diff is not the whole change.
		fmt.Fprintf(b, "%s: base unknown\n", name)
		return
	}
	stat = strings.TrimRight(stat, "\n")
	if stat == "" {
		fmt.Fprintf(b, "%s:\n(none)\n", name)
		return
	}
	fmt.Fprintf(b, "%s:\n", name)
	for _, line := range strings.Split(stat, "\n") {
		fmt.Fprintf(b, "    %s\n", line)
	}
}
