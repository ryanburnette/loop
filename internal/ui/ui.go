// Package ui renders loop progress to a writer.
package ui

import (
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

// Header prints the run banner.
func (r *Renderer) Header(h Header) {
	if r.quiet || r.json {
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
	if r.quiet || r.json {
		return
	}
	fmt.Fprintf(r.out, "\n%s\n", r.style(r.cyan, fmt.Sprintf("── iteration %d/%d ──", i, n)))
}

// StepStart prints the beginning of a step.
func (r *Renderer) StepStart(kind, name, detail string) {
	if r.quiet || r.json {
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
	if r.quiet || r.json {
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
	if r.quiet || r.json {
		return
	}
	if detail != "" {
		fmt.Fprintf(r.out, "  %s %s %s\n", r.style(r.dim, "tool"), name, detail)
		return
	}
	fmt.Fprintf(r.out, "  %s %s\n", r.style(r.dim, "tool"), name)
}

// Assistant prints extracted text when verbose.
func (r *Renderer) Assistant(text string) {
	if r.quiet || !r.verbose || r.json {
		return
	}
	fmt.Fprintln(r.out, text)
}

// Warn prints a warning line.
func (r *Renderer) Warn(msg string) {
	if r.quiet {
		return
	}
	fmt.Fprintf(r.out, "%s %s\n", r.style(r.dim, "warn"), msg)
}

// Success prints the final success line.
func (r *Renderer) Success(iter int, statePath string) {
	if r.json {
		return
	}
	// Avoid the word "iteration" so quiet-mode tests can detect progress leaks.
	fmt.Fprintf(r.out, "%s on pass %d  %s\n",
		r.style(r.green, "SUCCESS"), iter, statePath)
}

// Fail prints the failed-at-cap line.
func (r *Renderer) Fail(statePath string) {
	if r.json {
		return
	}
	fmt.Fprintf(r.out, "%s  %s\n", r.style(r.red, "FAILED"), statePath)
}

// Done prints the no-objective completion line.
func (r *Renderer) Done(statePath string) {
	if r.json {
		return
	}
	fmt.Fprintf(r.out, "%s  %s\n", r.style(r.dim, "DONE"), statePath)
}
