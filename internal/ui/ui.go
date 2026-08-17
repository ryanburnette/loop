// Package ui renders loop progress to a writer.
//
// The output is an append-safe, styled scrolling log: no alt-screen, no
// Bubble Tea, no web UI. It stays pipeable and diffable. When color is
// disabled (NO_COLOR or not a TTY) it emits zero ESC bytes; step types are
// still distinguishable via glyphs and text labels, never color alone.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Options configures the renderer.
type Options struct {
	Out     io.Writer
	Err     io.Writer
	Color   bool
	Quiet   bool
	Verbose bool
	JSON    bool
}

// Header is the run banner.
type Header struct {
	ID        string
	Dir       string
	WorkRoot  string
	Branch    string // loop branch (LOOP_BRANCH_NAME), "" if none
	GitRepo   bool
	GitBranch string // git branch at run start
	GitSHA    string
	GitDirty  bool
	GitDirtyN int
	Session   string
	MaxIter   int
	Objective bool
}

// Summary is the end-of-run footer.
type Summary struct {
	Elapsed    time.Duration
	Iterations int // iterations used
	MaxIter    int
	Result     string // success|fail|stopped|done
	Branch     string // loop branch name, "" if none
}

// Renderer writes human (or quiet) progress lines.
type Renderer struct {
	out     io.Writer
	err     io.Writer
	color   bool
	quiet   bool
	verbose bool
	json    bool

	bold   lipgloss.Style
	dim    lipgloss.Style
	green  lipgloss.Style
	red    lipgloss.Style
	cyan   lipgloss.Style
	yellow lipgloss.Style
	blue   lipgloss.Style
}

// New builds a Renderer.
func New(opts Options) *Renderer {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	r := &Renderer{
		out:     out,
		err:     opts.Err,
		color:   opts.Color,
		quiet:   opts.Quiet,
		verbose: opts.Verbose,
		json:    opts.JSON,
	}
	if opts.Color {
		r.bold = lipgloss.NewStyle().Bold(true)
		r.dim = lipgloss.NewStyle().Faint(true)
		r.green = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		r.red = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
		r.cyan = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
		r.yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		r.blue = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	}
	return r
}

func (r *Renderer) style(s lipgloss.Style, text string) string {
	if !r.color {
		return text
	}
	return s.Render(text)
}

// emit writes one JSON object per line in --json mode.
func (r *Renderer) emit(v any) {
	if !r.json {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = r.out.Write(b)
}

// Header prints the run banner.
func (r *Renderer) Header(h Header) {
	if r.json {
		r.emit(map[string]any{
			"type":      "header",
			"id":        h.ID,
			"dir":       h.Dir,
			"workroot":  h.WorkRoot,
			"branch":    h.Branch,
			"gitRepo":   h.GitRepo,
			"gitBranch": h.GitBranch,
			"gitSha":    h.GitSHA,
			"gitDirty":  h.GitDirty,
			"session":   h.Session,
			"maxIter":   h.MaxIter,
			"objective": h.Objective,
		})
		return
	}
	if r.quiet {
		return
	}
	obj := "no"
	if h.Objective {
		obj = "yes"
	}
	fmt.Fprintf(r.out, "%s  %s\n", r.style(r.bold, "loop"), time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(r.out, "  %s        %s\n", r.style(r.dim, "id"), h.ID)
	fmt.Fprintf(r.out, "  %s       %s\n", r.style(r.dim, "dir"), h.Dir)
	fmt.Fprintf(r.out, "  %s  %s\n", r.style(r.dim, "workroot"), h.WorkRoot)
	// Git info line.
	if h.GitRepo {
		dirty := "clean"
		dStyle := r.green
		if h.GitDirty {
			dirty = fmt.Sprintf("dirty(%d)", h.GitDirtyN)
			dStyle = r.yellow
		}
		sha := h.GitSHA
		if sha == "" {
			sha = "-"
		}
		fmt.Fprintf(r.out, "  %s   %s @ %s  %s\n",
			r.style(r.dim, "git"), r.style(r.cyan, h.GitBranch), sha, r.style(dStyle, dirty))
	} else {
		fmt.Fprintf(r.out, "  %s   -\n", r.style(r.dim, "git"))
	}
	// Loop branch (working branch), distinct from the git branch we started on.
	if h.Branch != "" {
		fmt.Fprintf(r.out, "  %s  %s\n", r.style(r.blue, "working"), h.Branch)
	}
	fmt.Fprintf(r.out, "  %s   %s  max %d  objective %s\n",
		r.style(r.dim, "session"), h.Session, h.MaxIter, obj)
}

// Iteration prints the iteration banner.
func (r *Renderer) Iteration(i, n int) {
	if r.json {
		r.emit(map[string]any{"type": "iteration", "i": i, "n": n})
		return
	}
	if r.quiet {
		return
	}
	fmt.Fprintf(r.out, "\n%s\n", r.style(r.cyan, fmt.Sprintf("── iteration %d/%d ──", i, n)))
}

// StepStart prints the beginning of a step. Each type gets a distinct glyph
// and a colored label, so turn/gate/hook are distinguishable at a glance and
// remain so (by glyph + label) when color is off.
func (r *Renderer) StepStart(kind, name, detail string) {
	if r.json {
		r.emit(map[string]any{"type": "step_start", "kind": kind, "name": name, "detail": detail})
		return
	}
	if r.quiet {
		return
	}
	glyph, label, st := stepStyle(r, kind)
	line := fmt.Sprintf("%s %s %s", r.style(st, glyph), r.style(st, label), name)
	if detail != "" {
		line += " " + r.style(r.dim, detail)
	}
	fmt.Fprintln(r.out, line)
}

func stepStyle(r *Renderer, kind string) (glyph, label string, st lipgloss.Style) {
	switch kind {
	case "turn":
		glyph = "▶"
		label = "turn"
		st = r.cyan
	case "gate":
		glyph = "▣"
		label = "gate"
		st = r.yellow
	case "hook":
		glyph = "⚙"
		label = "hook"
		st = r.blue
	default:
		glyph = "→"
		label = kind
		st = r.dim
	}
	return
}

// StepDone prints the step result.
func (r *Renderer) StepDone(ok bool, note string, elapsedSec int) {
	if r.json {
		r.emit(map[string]any{"type": "step_done", "ok": ok, "note": note, "elapsed": elapsedSec})
		return
	}
	if r.quiet {
		return
	}
	mark := "✓"
	st := r.green
	if !ok {
		mark = "✗"
		st = r.red
	}
	fmt.Fprintf(r.out, "  %s %s (%ds)\n", r.style(st, mark), note, elapsedSec)
}

// Tool updates the live tool line.
func (r *Renderer) Tool(name, detail string) {
	if r.json {
		r.emit(map[string]any{"type": "tool", "name": name, "detail": detail})
		return
	}
	if r.quiet {
		return
	}
	if detail != "" {
		fmt.Fprintf(r.out, "  %s %s %s\n", r.style(r.dim, "tool"), name, detail)
		return
	}
	fmt.Fprintf(r.out, "  %s %s\n", r.style(r.dim, "tool"), name)
}

// Context prints a live context/elapsed line during a turn.
func (r *Renderer) Context(percent, elapsedSec int) {
	if r.json {
		r.emit(map[string]any{"type": "context", "percent": percent, "elapsed": elapsedSec})
		return
	}
	if r.quiet {
		return
	}
	fmt.Fprintf(r.out, "  %s %d%%  %ds\n", r.style(r.dim, "ctx"), percent, elapsedSec)
}

// Verdict reports a turn verdict result. In --json it always emits a verdict
// event so a machine consumer can see reviewer/critic outcomes (a failing
// required verdict is also reported via step_done ok=false). In the terminal
// a passing verdict is silent (it is in gate-log.md); a failing *required*
// verdict is reported by the failing step_done note; a failing *soft*
// verdict prints a non-fatal marker so the rejection is visible even though
// it does not block.
func (r *Renderer) Verdict(name string, matched, required bool) {
	if r.json {
		r.emit(map[string]any{"type": "verdict", "name": name, "matched": matched, "required": required})
		return
	}
	if r.quiet {
		return
	}
	if matched || required {
		return
	}
	fmt.Fprintf(r.out, "  %s verdict: FAIL (soft; required=0, not blocking)\n", r.style(r.yellow, "⚠"))
}

// GateDetail prints a gate's output (indented, dim) on failure. Only the
// first few lines are shown so the terminal shows why without burying detail
// solely in gate-log.md.
func (r *Renderer) GateDetail(out string) {
	if r.json {
		r.emit(map[string]any{"type": "gate_detail", "out": out})
		return
	}
	if r.quiet {
		return
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 6 {
		lines = append(lines[:6], "…")
	}
	for _, line := range lines {
		fmt.Fprintf(r.out, "    %s\n", r.style(r.dim, line))
	}
}

// Assistant prints extracted text when verbose.
func (r *Renderer) Assistant(text string) {
	if r.json {
		r.emit(map[string]any{"type": "assistant", "text": text})
		return
	}
	if r.quiet || !r.verbose {
		return
	}
	fmt.Fprintln(r.out, text)
}

// Paused prints a line when the run enters a paused wait.
func (r *Renderer) Paused() {
	if r.json {
		r.emit(map[string]any{"type": "paused"})
		return
	}
	if r.quiet {
		return
	}
	fmt.Fprintf(r.out, "%s  control: resume to continue\n", r.style(r.yellow, "PAUSED"))
}

// Resumed prints a line when the run leaves a paused wait.
func (r *Renderer) Resumed() {
	if r.json {
		r.emit(map[string]any{"type": "resumed"})
		return
	}
	if r.quiet {
		return
	}
	fmt.Fprintf(r.out, "%s\n", r.style(r.dim, "resumed"))
}

// Warn prints a warning line. Warnings are diagnostics, so in human mode
// they go to stderr (the err writer) — not stdout — and they are emitted
// even in quiet mode, so a typo like LOOP_MAX_ITERATIONS is still surfaced
// when -q suppresses all progress. In --json mode the warn event stays on
// the stdout JSON stream so a machine consumer sees it inline.
func (r *Renderer) Warn(msg string) {
	if r.json {
		r.emit(map[string]any{"type": "warn", "msg": msg})
		return
	}
	w := r.err
	if w == nil {
		w = r.out
	}
	fmt.Fprintf(w, "%s %s\n", r.style(r.dim, "warn"), msg)
}

// Summary prints the end-of-run footer (non-quiet, non-json only).
func (r *Renderer) Summary(s Summary) {
	if r.json {
		r.emit(map[string]any{
			"type":       "summary",
			"elapsed":    s.Elapsed.String(),
			"iterations": s.Iterations,
			"result":     s.Result,
			"branch":     s.Branch,
		})
		return
	}
	if r.quiet {
		return
	}
	result := strings.ToUpper(s.Result)
	st := r.dim
	switch s.Result {
	case "success":
		st = r.green
	case "fail":
		st = r.red
	case "stopped":
		st = r.yellow
	}
	fmt.Fprintln(r.out, r.style(r.dim, "── summary ──"))
	fmt.Fprintf(r.out, "  %s  %s  iterations %d/%d",
		r.style(st, result), s.Elapsed.Round(time.Second), s.Iterations, s.MaxIter)
	if s.Branch != "" {
		fmt.Fprintf(r.out, "  branch %s", s.Branch)
	}
	fmt.Fprintln(r.out)
}

// Success prints the final success line.
func (r *Renderer) Success(iter int, statePath string) {
	if r.json {
		r.emit(map[string]any{"type": "success", "iter": iter, "state": statePath})
		return
	}
	fmt.Fprintf(r.out, "%s on pass %d  %s\n",
		r.style(r.green, "SUCCESS"), iter, statePath)
}

// Stopped prints the operator-stop final line.
func (r *Renderer) Stopped(statePath string) {
	if r.json {
		r.emit(map[string]any{"type": "stopped", "state": statePath})
		return
	}
	fmt.Fprintf(r.out, "%s  %s\n", r.style(r.yellow, "STOPPED"), statePath)
}

// Fail prints the failed-at-cap line.
func (r *Renderer) Fail(statePath string) {
	if r.json {
		r.emit(map[string]any{"type": "fail", "state": statePath})
		return
	}
	fmt.Fprintf(r.out, "%s  %s\n", r.style(r.red, "FAILED"), statePath)
}

// Done prints the no-objective completion line.
func (r *Renderer) Done(statePath string) {
	if r.json {
		r.emit(map[string]any{"type": "done", "state": statePath})
		return
	}
	fmt.Fprintf(r.out, "%s  %s\n", r.style(r.dim, "DONE"), statePath)
}
