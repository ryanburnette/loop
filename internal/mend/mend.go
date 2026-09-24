// Package mend renders the runner-owned context for the next turn.
// The model may propose settled lines. It does not write mend.md or brief.md.
package mend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxSettled   = 24
	maxProposals = 8
	maxRunes     = 200
	maxTailLines = 8
	maxMendBytes = 16 * 1024
)

// Proposal is one settled-line suggestion from a turn.
// Structural checks only: the runner does not decide whether the sentence is true.
type Proposal struct {
	Tried      string `json:"tried"`
	Failed     string `json:"failed"`
	DoNotRetry bool   `json:"do_not_retry"`
}

// Settled is a proposal the runner committed.
type Settled struct {
	Iter       int    `json:"iter"`
	Step       string `json:"step"`
	Tried      string `json:"tried"`
	Failed     string `json:"failed"`
	DoNotRetry bool   `json:"do_not_retry"`
}

// Ledger is the committed settled lines plus rejections, persisted as ledger.json.
type Ledger struct {
	Settled  []Settled `json:"settled"`
	Rejected []string  `json:"rejected"`
	Dropped  int       `json:"dropped"`
}

// Facts are runner-owned. Nothing in Commit writes these from JSON.
type Facts struct {
	RunID       string
	Iter        int
	MaxIter     int
	Branch      string
	Head        string
	Freeze      string
	Session     string
	Compacted   bool
	Context     string
	Goal        string
	GoalBody    string
	Constraints string
	Checks      []Check
	Diff        Diff
}

// Check is one gate or scorecard result from the iteration just recorded.
type Check struct {
	Kind       string // "gate" or "scorecard"
	Name       string
	OK         bool
	Exit       int
	Required   bool
	Tail       []string
	Excerpt    string
	Render     string
	Marks      []Mark
	Card       string
	Unreadable bool
	Error      string
}

// Mark is one scorecard row, already labeled by the runner.
type Mark struct {
	ID      string
	Label   string
	Because string
}

// Diff is the three-command diff stat. CommittedUnknown means START was
// missing or the committed command failed; the renderer must not pretend
// an empty worktree diff is the whole change.
type Diff struct {
	CommittedUnknown bool
	Committed        string
	Staged           string
	Unstaged         string
	Omitted          int
}

// HandoffStub is what handoff.md contains. It is not attached.
const HandoffStub = "# Handoff\n\nSuperseded by mend.md in this directory. The runner attaches mend.md.\n"

const briefStub = "See mend.md.\n"

// BriefStub is the single line written over brief.md at iteration end.
func BriefStub() string { return briefStub }

// Commit applies proposal JSON to the ledger.
// A malformed file becomes one rejected line and does not fail the caller.
// Missing proposals means no new settled lines.
func Commit(ledger *Ledger, step string, iter int, raw []byte) {
	if ledger == nil {
		return
	}
	reject := func(reason string) {
		ledger.Rejected = append(ledger.Rejected, fmt.Sprintf("iter %d, %s: %s", iter, step, reason))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		reject("proposal unreadable.")
		return
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		reject("proposal unreadable.")
		return
	}
	rawProps, ok := top["proposals"]
	if !ok || string(bytes.TrimSpace(rawProps)) == "null" {
		return
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawProps, &entries); err != nil {
		reject("proposal unreadable.")
		return
	}
	for i, ent := range entries {
		if i >= maxProposals {
			reject("too many")
			continue
		}
		if err := acceptOne(ledger, step, iter, ent); err != nil {
			reject(err.Error())
		}
	}
}

func acceptOne(ledger *Ledger, step string, iter int, ent json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(ent, &obj); err != nil {
		return errors.New("proposal unreadable.")
	}
	tried, err := jsonString(obj["tried"])
	if err != nil {
		return errors.New("bad field")
	}
	failed, err := jsonString(obj["failed"])
	if err != nil {
		return errors.New("bad field")
	}
	// A newline is its own rejection. Check it before emptiness so a line
	// break inside a sentence is not reported as an empty field.
	if strings.ContainsAny(tried, "\r\n") || strings.ContainsAny(failed, "\r\n") {
		return errors.New("newline")
	}
	if tried == "" || failed == "" {
		return errors.New("empty field")
	}
	if utf8.RuneCountInString(tried) > maxRunes || utf8.RuneCountInString(failed) > maxRunes {
		return errors.New("too long")
	}
	dnr, err := jsonBool(obj["do_not_retry"])
	if err != nil {
		return err
	}
	ledger.add(Settled{
		Iter:       iter,
		Step:       step,
		Tried:      tried,
		Failed:     failed,
		DoNotRetry: dnr,
	})
	return nil
}

func jsonString(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

// jsonBool accepts only JSON true or false. A missing field is false.
func jsonBool(raw json.RawMessage) (bool, error) {
	s := string(bytes.TrimSpace(raw))
	if s == "" || s == "null" {
		return false, nil
	}
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("do_not_retry not a boolean")
	}
}

func (l *Ledger) add(s Settled) {
	for i := range l.Settled {
		if l.Settled[i].Tried == s.Tried {
			// Same dead end: keep the earlier iteration, replace the outcome.
			l.Settled[i].Failed = s.Failed
			l.Settled[i].DoNotRetry = s.DoNotRetry
			return
		}
	}
	if len(l.Settled) >= maxSettled {
		drop := oldestDrop(l.Settled)
		l.Settled = append(l.Settled[:drop], l.Settled[drop+1:]...)
		l.Dropped++
	}
	l.Settled = append(l.Settled, s)
}

// oldestDrop prefers the oldest retryable line. Do-not-retry is the memory
// worth keeping; drop one of those only when every line says not to retry.
func oldestDrop(ss []Settled) int {
	for i := range ss {
		if !ss[i].DoNotRetry {
			return i
		}
	}
	return 0
}

// SaveLedger writes ledger.json. Nil slices are stored as empty arrays.
func SaveLedger(path string, ledger Ledger) error {
	if ledger.Settled == nil {
		ledger.Settled = []Settled{}
	}
	if ledger.Rejected == nil {
		ledger.Rejected = []string{}
	}
	b, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFile(path, b)
}

// LoadLedger reads ledger.json. A missing or invalid file is an error.
// Callers must not invent an empty ledger from mend.md.
func LoadLedger(path string) (Ledger, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Ledger{}, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return Ledger{}, fmt.Errorf("empty ledger")
	}
	var ledger Ledger
	if err := json.Unmarshal(b, &ledger); err != nil {
		return Ledger{}, err
	}
	return ledger, nil
}

// RenderMend returns the page the runner attaches. The runner keeps these
// bytes and rewrites the file from them before the next turn.
func RenderMend(facts Facts, ledger Ledger) string {
	return fit(facts, ledger, false)
}

// WriteMend renders mend.md. Bodies are truncated before Facts' exit codes
// or do-not-retry lines when the page would exceed 16KB.
func WriteMend(path string, facts Facts, ledger Ledger) error {
	return writeFile(path, []byte(RenderMend(facts, ledger)))
}

// WriteBrief renders brief.md for later turns of the current iteration.
func WriteBrief(path string, facts Facts, ledger Ledger) error {
	return writeFile(path, []byte(fit(facts, ledger, true)))
}

// WriteHandoffStub replaces handoff.md so an old reader does not see a log paste.
func WriteHandoffStub(path string) error {
	return writeFile(path, []byte(HandoffStub))
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func fit(facts Facts, ledger Ledger, brief bool) string {
	if s := render(facts, ledger, brief); len(s) <= maxMendBytes {
		return s
	}
	facts = clipBodies(facts, 1024)
	if s := render(facts, ledger, brief); len(s) <= maxMendBytes {
		return s
	}
	facts = clearTails(facts)
	if s := render(facts, ledger, brief); len(s) <= maxMendBytes {
		return s
	}
	// Drop whole rejected and retryable lines. Do not cut a Fact in the
	// middle. Do-not-retry lines and exit codes stay even if the page is
	// still over 16KB; the runner warns instead of slicing them.
	view := ledger
	view.Settled = append([]Settled(nil), ledger.Settled...)
	view.Rejected = append([]string(nil), ledger.Rejected...)
	s := render(facts, view, brief)
	for len(s) > maxMendBytes && len(view.Rejected) > 0 {
		view.Rejected = view.Rejected[1:]
		s = render(facts, view, brief)
	}
	for len(s) > maxMendBytes {
		idx := oldestRetryable(view.Settled)
		if idx < 0 {
			break
		}
		view.Settled = append(view.Settled[:idx], view.Settled[idx+1:]...)
		view.Dropped++
		s = render(facts, view, brief)
	}
	return s
}

func oldestRetryable(ss []Settled) int {
	for i := range ss {
		if !ss[i].DoNotRetry {
			return i
		}
	}
	return -1
}

func clipBodies(f Facts, n int) Facts {
	f.GoalBody = clipText(f.GoalBody, n)
	f.Constraints = clipText(f.Constraints, n)
	f.Checks = append([]Check(nil), f.Checks...)
	return f
}

func clearTails(f Facts) Facts {
	f.Checks = append([]Check(nil), f.Checks...)
	for i := range f.Checks {
		if len(f.Checks[i].Tail) > 0 {
			// The marker sits on the tail that was removed, not on the goal.
			f.Checks[i].Tail = []string{"… truncated …"}
		}
	}
	return f
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 0 {
		return "… truncated …\n"
	}
	cut := s[:n]
	for cut != "" && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n… truncated …\n"
}

func render(facts Facts, ledger Ledger, brief bool) string {
	var b strings.Builder
	if brief {
		b.WriteString("# Brief\n\n")
		b.WriteString("This iteration so far. Facts outrank settled claims.\n")
		b.WriteString("Do not edit this file. The runner rewrites it after each step.\n\n")
	} else {
		b.WriteString("# Mend\n\n")
		fmt.Fprintf(&b, "Written by the runner at the end of iteration %d. Facts outrank settled claims.\n", facts.Iter)
		b.WriteString("Do not edit this file. The next turn is a new pi process; this is the history.\n\n")
	}

	b.WriteString("## Facts\n\n")
	b.WriteString("source: runner\n\n")
	fmt.Fprintf(&b, "- run: %s\n", facts.RunID)
	if brief {
		fmt.Fprintf(&b, "- iteration: %d of %d\n", facts.Iter, facts.MaxIter)
	} else {
		fmt.Fprintf(&b, "- iteration just finished: %d of %d\n", facts.Iter, facts.MaxIter)
	}
	fmt.Fprintf(&b, "- branch: %s\n", emptyDash(facts.Branch))
	fmt.Fprintf(&b, "- head: %s\n", emptyDash(facts.Head))
	fmt.Fprintf(&b, "- freeze: %s\n", emptyDash(facts.Freeze))
	fmt.Fprintf(&b, "- session: %s\n", emptyDash(facts.Session))
	fmt.Fprintf(&b, "- compaction this iteration: %s\n", yesNo(facts.Compacted))
	fmt.Fprintf(&b, "- context percent: %s\n", emptyDash(facts.Context))
	if ledger.Dropped > 0 {
		fmt.Fprintf(&b, "- settled dropped: %d\n", ledger.Dropped)
	}
	b.WriteString("\n### Goal\n\n")
	if facts.Goal != "" {
		b.WriteString(facts.Goal)
		b.WriteString("\n")
	} else {
		b.WriteString("(none)\n")
	}
	if facts.GoalBody != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(facts.GoalBody, "\n"))
		b.WriteString("\n")
	}
	b.WriteString("\n### Constraints\n\n")
	if strings.TrimSpace(facts.Constraints) != "" {
		b.WriteString(strings.TrimRight(facts.Constraints, "\n"))
		b.WriteString("\n")
	} else {
		b.WriteString("(none)\n")
	}

	b.WriteString("\n### Checks\n\n")
	if len(facts.Checks) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, c := range facts.Checks {
			writeCheck(&b, c)
		}
	}

	b.WriteString("\n### Diff stat\n\n")
	b.WriteString("#### Committed\n\n")
	if facts.Diff.CommittedUnknown {
		b.WriteString("committed: base unknown\n")
	} else {
		writeStat(&b, facts.Diff.Committed)
	}
	b.WriteString("\n#### Staged\n\n")
	writeStat(&b, facts.Diff.Staged)
	b.WriteString("\n#### Unstaged\n\n")
	writeStat(&b, facts.Diff.Unstaged)
	if facts.Diff.Omitted > 0 {
		fmt.Fprintf(&b, "\nomitted lines: %d\n", facts.Diff.Omitted)
	}

	b.WriteString("\n## Settled\n\n")
	if len(ledger.Settled) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, s := range ledger.Settled {
			flag := ""
			if s.DoNotRetry {
				flag = "do-not-retry, "
			}
			fmt.Fprintf(&b, "- iter %d, %s, %sclaimed by %s at iter %d: tried %s. failed: %s.\n",
				s.Iter, s.Step, flag, s.Step, s.Iter, s.Tried, s.Failed)
		}
	}

	b.WriteString("\n## Rejected proposals\n\n")
	if len(ledger.Rejected) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, r := range ledger.Rejected {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}

	if !brief {
		b.WriteString("\n## Discarded\n\n")
		b.WriteString("The previous pi transcript was not attached. turn-*.md under this state\n")
		b.WriteString("directory is for the operator, not for the next turn.\n")
	}
	return b.String()
}

func writeCheck(b *strings.Builder, c Check) {
	req := "soft"
	if c.Required {
		req = "required"
	}
	switch c.Kind {
	case "scorecard":
		status := "PASS"
		switch {
		case c.Unreadable:
			status = "UNREADABLE"
		case !c.OK:
			status = "FAIL"
		}
		fmt.Fprintf(b, "- scorecard %s: %s", c.Name, status)
		if c.Render != "" {
			b.WriteString(" ")
			b.WriteString(c.Render)
		}
		fmt.Fprintf(b, " (%s)\n", req)
		for _, m := range c.Marks {
			fmt.Fprintf(b, "  - %s %s — %s\n", m.ID, m.Label, m.Because)
		}
		if c.Unreadable && c.Error != "" {
			fmt.Fprintf(b, "  error: %s\n", c.Error)
		}
		if c.Card != "" {
			fmt.Fprintf(b, "  card: %s\n", c.Card)
		}
	default:
		status := "OK"
		if !c.OK {
			status = "FAIL"
		}
		fmt.Fprintf(b, "- gate %s: %s exit %d (%s)\n", c.Name, status, c.Exit, req)
		for _, line := range showTail(c.Tail) {
			fmt.Fprintf(b, "  tail: %s\n", line)
		}
		if c.Excerpt != "" {
			fmt.Fprintf(b, "  excerpt: %s\n", c.Excerpt)
		}
	}
}

func showTail(lines []string) []string {
	if len(lines) > maxTailLines {
		return lines[len(lines)-maxTailLines:]
	}
	return lines
}

func writeStat(b *strings.Builder, stat string) {
	stat = strings.TrimRight(stat, "\n")
	if stat == "" {
		b.WriteString("(none)\n")
		return
	}
	for _, line := range strings.Split(stat, "\n") {
		fmt.Fprintf(b, "    %s\n", line)
	}
}

func emptyDash(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
