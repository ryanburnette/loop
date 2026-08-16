// Package gitinfo gathers git status for display only.
//
// A failure here must never affect run behavior or exit codes: Collect never
// returns an error. Any failure (not a repo, git missing, bad path) yields a
// zero Info with Repo=false so callers render a blank/dash instead of failing
// the run.
package gitinfo

import (
	"os/exec"
	"strings"
)

// Info is the git state of a directory, for display.
type Info struct {
	Repo     bool   // false if dir is not inside a git repo
	Branch   string // "" if detached HEAD or unknown
	ShortSHA string
	Dirty    bool
	DirtyN   int
}

// Collect gathers git info for dir. It never returns an error; any failure
// (not a repo, git missing, etc.) yields a zero Info with Repo=false.
func Collect(dir string) Info {
	var info Info
	if err := runGit(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return info
	}
	info.Repo = true

	if out, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		b := strings.TrimSpace(out)
		// "HEAD" means detached; leave Branch empty in that case.
		if b != "" && b != "HEAD" {
			info.Branch = b
		}
	}
	if out, err := gitOutput(dir, "rev-parse", "--short", "HEAD"); err == nil {
		info.ShortSHA = strings.TrimSpace(out)
	}
	if out, err := gitOutput(dir, "status", "--porcelain"); err == nil {
		n := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				n++
			}
		}
		info.DirtyN = n
		info.Dirty = n > 0
	}
	return info
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	return cmd.Run()
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
