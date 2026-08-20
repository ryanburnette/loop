// Package scaffold writes loop recipe templates via loop init.
//
// Templates are embedded as Go string constants (not //go:embed files) so
// that scaffolding never creates files named writer.md, loop.env, manifest,
// tests.sh, etc. inside this source repo — those basenames are freeze
// patterns in the dogfood loop and stray copies would falsely trip its gate.
package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Template is a named set of files (relative path → content).
type Template struct {
	Name  string
	Files map[string]string
}

// Templates is the registry keyed by template name.
var Templates = map[string]Template{}

// Default is the template used when loop init is called with no argument.
const Default = "until-green"

// DefaultOr returns name, or the default template name when name is empty.
func DefaultOr(name string) string {
	if name == "" {
		return Default
	}
	return name
}

// topDir returns the first path component of p (the top-level directory under
// the loop dir). For "gates/tests.sh" it returns "gates"; for "loop.env" it
// returns "loop.env".
func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

func register(t Template) {
	Templates[t.Name] = t
}

// Names returns the sorted template names.
func Names() []string {
	out := make([]string, 0, len(Templates))
	for k := range Templates {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Scaffold writes the named template into dir. dir must not already exist.
// If name is empty, the default template is used.
func Scaffold(dir, name string) error {
	if dir == "" {
		return fmt.Errorf("loop directory path is empty")
	}
	if name == "" {
		name = Default
	}
	t, ok := Templates[name]
	if !ok {
		return fmt.Errorf("unknown template %q (available: %v)", name, Names())
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("loop directory already exists: %s (refusing to overwrite; remove it first)", dir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	// Deterministic order for reproducible scaffolding.
	paths := make([]string, 0, len(t.Files))
	for p := range t.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(full), err)
		}
		mode := fs.FileMode(0o644)
		// Anything under gates/ or hooks/ is meant to be executed by the
		// runner, so make it executable. Match the top-level directory
		// component (not just the immediate parent) so a future template
		// that nests a script under gates/sub/ is still executable.
		if topDir(p) == "gates" || topDir(p) == "hooks" {
			mode = 0o755
		}
		if err := os.WriteFile(full, []byte(t.Files[p]), mode); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
	}
	// Write a .gitignore that hides the whole recipe, not just state/. Git
	// reads a .gitignore even when that file is itself ignored, so `*` makes
	// the loop dir vanish from `git status` with no edit to the project's own
	// .gitignore — everything about a loop, including the fact that it is not
	// part of the project, stays inside the one directory.
	//
	// Ignoring only state/ left the dir untracked-but-visible, which is the
	// state that trips a clean-tree check: `loop run` tolerates its own recipe
	// dir, but a one-shot run (whose loop dir lives in temp) has no way to know
	// that a stray .loop/ is not the user's work in progress.
	//
	// To share a recipe with a team instead, replace this file's contents or
	// `git add -f` the directory.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Join(dir, ".gitignore"), err)
	}
	return nil
}

func init() {
	register(untilGreen)
	register(doubleCheck)
	register(twoModelCritique)
	register(untilCount)
}
