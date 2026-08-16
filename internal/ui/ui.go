// Package ui renders loop progress to a writer.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Options configures the renderer.
type Options struct {
	Out     io.Writer
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
	Branch    string
	Session   string
	MaxIter   int
	Objective bool
}

// Renderer writes human (or quiet) progress lines.
type Renderer struct {
	out     io.Writer
	color   bool
	quiet   bool
	verbose bool
	json    bool

	bold  lipgloss.Style
	dim   lipgloss.Style
	green lipgloss.Style
	red   lipgloss.Style
	cyan  lipgloss.Style
}

// New builds a Renderer.
func New(opts Options) *Renderer {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	r := &Renderer{
		out:     out,
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
	if h.Branch != "" {
		fmt.Fprintf(r.out, "  %s    %s\n", r.style(r.dim, "branch"), h.Branch)
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

// StepStart prints the beginning of a step.
func (r *Renderer) StepStart(kind, name, detail string) {
	if r.json {
		r.emit(map[string]any{"type": "step_start", "kind": kind, "name": name, "detail": detail})
		return
	}
	if r.quiet {
		return
	}
	line := fmt.Sprintf("→ %s %s", kind, name)
	if detail != "" {
		line += " " + r.style(r.dim, detail)
	}
	fmt.Fprintln(r.out, line)
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

// Tool updates the live tool line (best-effort plain print).
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

// Warn prints a warning line.
func (r *Renderer) Warn(msg string) {
	if r.json {
		r.emit(map[string]any{"type": "warn", "msg": msg})
		return
	}
	if r.quiet {
		return
	}
	fmt.Fprintf(r.out, "%s %s\n", r.style(r.dim, "warn"), msg)
}

// Success prints the final success line.
func (r *Renderer) Success(iter int, statePath string) {
	if r.json {
		r.emit(map[string]any{"type": "success", "iter": iter, "state": statePath})
		return
	}
	// Avoid the word "iteration" so quiet-mode tests can detect progress leaks.
	fmt.Fprintf(r.out, "%s on pass %d  %s\n",
		r.style(r.green, "SUCCESS"), iter, statePath)
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
