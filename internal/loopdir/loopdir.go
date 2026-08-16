// Package loopdir resolves the loop directory loop operates on.
//
// From v0.3, loop operates on .loop/ in the current directory by default.
// -C DIR is the explicit escape hatch (same convention as git -C). There is
// no upward directory search: .loop/ must be in the directory loop is told
// to use.
package loopdir

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultDir is the loop recipe directory name.
const DefaultDir = ".loop"

// Resolve returns the loop directory to use. If explicit is non-empty it
// wins (joined onto cwd if relative, passed through if absolute).
// Otherwise cwd/.loop is used. Resolve does not check existence.
func Resolve(cwd, explicit string) (string, error) {
	if explicit == "" {
		return filepath.Join(cwd, DefaultDir), nil
	}
	if filepath.IsAbs(explicit) {
		return explicit, nil
	}
	return filepath.Join(cwd, explicit), nil
}

// Missing reports whether dir does not look like an initialized loop dir:
// no loop.env, no manifest, and no non-empty prompts/ directory.
func Missing(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "loop.env")); err == nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest")); err == nil {
		return false
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "prompts")); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				return false
			}
		}
	}
	return true
}

// MissingMessage is the error text for the Missing(dir) case. It mentions
// "loop init" and names the directory it looked in.
func MissingMessage(dir string) string {
	return fmt.Sprintf(
		"loop: %s is not a loop directory (no loop.env, manifest, or prompts/ found).\n"+
			"  Run `loop init` to scaffold one, or use -C to point at an existing loop directory.",
		dir)
}
