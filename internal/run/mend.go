package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ryanburnette/loop/internal/config"
	"github.com/ryanburnette/loop/internal/manifest"
	"github.com/ryanburnette/loop/internal/mend"
	"github.com/ryanburnette/loop/internal/scorecard"
)

const diffLineCap = 30

func (rr *runner) finishIteration(iter int) error {
	facts := rr.facts(iter)
	if err := mend.SaveLedger(rr.ledgerPath, rr.ledger); err != nil {
		return err
	}
	// Keep the rendered bytes. The next attach rewrites the file from this
	// copy so a turn cannot hand the following pi process an edited mend.
	rendered := mend.RenderMend(facts, rr.ledger)
	rr.mendBytes = []byte(rendered)
	if err := os.WriteFile(rr.mendPath, rr.mendBytes, 0o644); err != nil {
		return err
	}
	if err := mend.WriteHandoffStub(rr.handoffPath); err != nil {
		return err
	}
	// The brief's contents are in the mend. Leave a stub so the next
	// iteration's first turn does not attach this iteration's page.
	if err := os.WriteFile(rr.briefPath, []byte(mend.BriefStub()), 0o644); err != nil {
		return err
	}
	rr.warnIfTruncated(rr.mendPath)
	// The run is still in progress. The terminal result overwrites this page.
	return rr.writeReturn(mend.ResultRunning, iter)
}

func (rr *runner) writeBrief(iter int) {
	if err := mend.WriteBrief(rr.briefPath, rr.facts(iter), rr.briefLedger(iter)); err != nil {
		return
	}
	rr.warnIfTruncated(rr.briefPath)
}

func (rr *runner) warnIfTruncated(path string) {
	if rr.warnedTrunc {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// A clipped body or tail carries the marker. A page that is still over
	// 16KB kept its exit codes and do-not-retry lines whole; warn anyway.
	over := len(b) > 16*1024
	clipped := strings.Contains(string(b), "… truncated …")
	if !over && !clipped {
		return
	}
	rr.warnedTrunc = true
	if over {
		rr.r.Warn("mend still exceeds 16KB; exit codes and do-not-retry lines were kept")
		return
	}
	rr.r.Warn("mend truncated to stay under 16KB")
}

// restoreMend writes the last runner-rendered mend over whatever a turn
// left on disk. False when this process has not rendered or loaded one.
func (rr *runner) restoreMend() bool {
	if len(rr.mendBytes) == 0 {
		return false
	}
	if err := os.WriteFile(rr.mendPath, rr.mendBytes, 0o644); err != nil {
		return false
	}
	return true
}

func (rr *runner) snapSettled() {
	rr.snap = map[string]mend.Settled{}
	for _, s := range rr.ledger.Settled {
		rr.snap[s.Tried] = s
	}
}

func (rr *runner) briefLedger(iter int) mend.Ledger {
	out := mend.Ledger{Dropped: rr.ledger.Dropped}
	prefix := fmt.Sprintf("iter %d, ", iter)
	for _, s := range rr.ledger.Settled {
		prev, ok := rr.snap[s.Tried]
		changed := !ok || prev.Failed != s.Failed || prev.DoNotRetry != s.DoNotRetry
		if s.Iter == iter || changed {
			out.Settled = append(out.Settled, s)
		}
	}
	for _, r := range rr.ledger.Rejected {
		if strings.HasPrefix(r, prefix) {
			out.Rejected = append(out.Rejected, r)
		}
	}
	return out
}

func (rr *runner) commitProposal(iter int, step string) {
	p := filepath.Join(rr.stateDir, "proposals", fmt.Sprintf("%d-%s.json", iter, step))
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	mend.Commit(&rr.ledger, step, iter, b)
}

func (rr *runner) facts(iter int) mend.Facts {
	goal := loadGoal(rr.loopDir, rr.cfg.Context)
	body := loadTodoBody(rr.loopDir)
	if strings.TrimSpace(body) == strings.TrimSpace(goal) {
		body = ""
	}
	branch := rr.branchName
	if branch == "" {
		if b, err := gitCurrentBranch(rr.workroot); err == nil && b != "" && b != "HEAD" {
			branch = b
		} else {
			branch = "(none)"
		}
	}
	ctx := "n/a"
	if rr.cfg.Session != config.SessionNone {
		// A json-stream 0 is not a measured percent. Only a successful probe is.
		if rr.lastCtxKnown {
			ctx = strconv.Itoa(rr.lastCtxPercent)
		} else {
			ctx = "unknown"
		}
	}
	return mend.Facts{
		RunID:       rr.id,
		Iter:        iter,
		MaxIter:     rr.cfg.MaxIter,
		Branch:      branch,
		Head:        gitShort(rr.workroot),
		Freeze:      rr.freezeStatus(),
		Session:     string(rr.cfg.Session),
		Compacted:   rr.sawCompact,
		Context:     ctx,
		Goal:        goal,
		GoalBody:    body,
		Constraints: loadConstraints(rr.loopDir),
		Checks:      append([]mend.Check(nil), rr.checks...),
		Diff:        collectDiff(rr.workroot, rr.startSHA),
	}
}

func (rr *runner) freezeStatus() string {
	if len(rr.cfg.Freeze) == 0 {
		return "not configured"
	}
	if err := rr.freezeBase.Check(rr.workroot, filepath.Join(rr.stateDir, "frozen")); err != nil {
		return "drift"
	}
	return "ok"
}

func (rr *runner) noteGate(step manifest.Step, iter int, ok bool, exit int, log string) {
	dir := filepath.Join(rr.stateDir, "excerpts")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("%d-%s.log", iter, step.Name))
	_ = os.WriteFile(path, []byte(excerptBody(log)), 0o644)
	rr.checks = append(rr.checks, mend.Check{
		Kind:     "gate",
		Name:     step.Name,
		OK:       ok,
		Exit:     exit,
		Required: step.Required,
		Tail:     tailLines(log, 8),
		Excerpt:  path,
	})
}

func (rr *runner) noteScore(step manifest.Step, outPath string, outcome scorecard.Outcome) {
	c := mend.Check{
		Kind:       "scorecard",
		Name:       step.Name,
		OK:         outcome.Readable && outcome.Passed,
		Required:   step.Required,
		Render:     outcome.Render,
		Card:       outPath,
		Unreadable: !outcome.Readable,
		Error:      outcome.Error,
	}
	for _, m := range outcome.Marks {
		c.Marks = append(c.Marks, mend.Mark{ID: m.ID, Label: markLabel(m), Because: m.Because})
	}
	rr.checks = append(rr.checks, c)
}

func markLabel(m scorecard.Mark) string {
	if m.Max <= 1 {
		if m.Max > 0 && m.Value >= m.Max {
			return "met"
		}
		return "unmet"
	}
	return strconv.Itoa(m.Value)
}

func (rr *runner) noteUnreadable(step manifest.Step, errText string) {
	rr.checks = append(rr.checks, mend.Check{
		Kind:       "scorecard",
		Name:       step.Name,
		Required:   step.Required,
		Unreadable: true,
		Error:      errText,
	})
}

func loadTodoBody(loopDir string) string {
	b, err := os.ReadFile(filepath.Join(loopDir, goalFile))
	if err != nil {
		return ""
	}
	return string(b)
}

func metaValue(stateDir, key string) string {
	b, err := os.ReadFile(filepath.Join(stateDir, "meta.env"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

func gitHEAD(workroot string) string {
	out, err := gitArgs(workroot, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func gitShort(workroot string) string {
	out, err := exec.Command("git", "-C", workroot, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "unknown"
	}
	return s
}

func collectDiff(workroot, start string) mend.Diff {
	var d mend.Diff
	staged, _ := gitArgs(workroot, "diff", "--stat", "--cached")
	unstaged, _ := gitArgs(workroot, "diff", "--stat")
	committed := ""
	if start == "" {
		d.CommittedUnknown = true
	} else {
		out, err := gitArgs(workroot, "diff", "--stat", start+"...HEAD")
		if err != nil {
			d.CommittedUnknown = true
		} else {
			committed = out
		}
	}
	d.Committed, d.Staged, d.Unstaged, d.Omitted = capThree(committed, staged, unstaged, diffLineCap)
	if d.CommittedUnknown {
		d.Committed = ""
	}
	return d
}

func gitArgs(workroot string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", workroot}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func capThree(committed, staged, unstaged string, limit int) (c, s, u string, omitted int) {
	c, n, o := capSection(committed, limit)
	omitted += o
	limit -= n
	s, n, o = capSection(staged, limit)
	omitted += o
	limit -= n
	u, _, o = capSection(unstaged, limit)
	omitted += o
	return c, s, u, omitted
}

// capSection keeps lines that fit. A non-empty section that does not fit at
// all is "(N lines omitted)", not an empty string. Empty is what writeStat
// renders as "(none)", which would hide a real staged or unstaged diff.
func capSection(text string, limit int) (kept string, used, omitted int) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return "", 0, 0
	}
	lines := strings.Split(text, "\n")
	if limit <= 0 {
		return fmt.Sprintf("(%d lines omitted)", len(lines)), 0, len(lines)
	}
	if len(lines) <= limit {
		return strings.Join(lines, "\n"), len(lines), 0
	}
	return strings.Join(lines[:limit], "\n"), limit, len(lines) - limit
}

func tailLines(s string, n int) []string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func excerptBody(s string) string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= 80 {
		return s + "\n"
	}
	var b strings.Builder
	for _, ln := range lines[:40] {
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	b.WriteString("…\n")
	for _, ln := range lines[len(lines)-40:] {
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}

func briefAttachable(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(b))
	return s != "" && s != strings.TrimSpace(mend.BriefStub())
}
