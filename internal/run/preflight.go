package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ryanburnette/loop/internal/manifest"
	"github.com/ryanburnette/loop/internal/scorecard"
)

// preflight runs after branch setup and the freeze snapshot, before any pi
// turn. It does not execute gates. The caller must not delete loop/<id>:
// branch setup may already have created it, and the recipe page names it.
// Session and compact enums are rejected by config.Load. Assurance
// self-graded or verdict warns at startup and is not a failure here.
func (rr *runner) preflight(man *manifest.Manifest) error {
	if rr.cfg.MaxIter < 1 {
		return fmt.Errorf("LOOP_MAX_ITER=%d: must be >= 1", rr.cfg.MaxIter)
	}
	if _, err := exec.LookPath(rr.cfg.PiPath); err != nil {
		return fmt.Errorf("pi: %w", err)
	}
	if man == nil {
		return fmt.Errorf("manifest: missing")
	}
	for _, step := range man.Steps {
		switch step.Type {
		case manifest.Turn:
			if step.Verdict != "" && step.Scorecard != "" {
				return fmt.Errorf("turn %s: verdict= and scorecard= cannot share a turn", step.Name)
			}
			p := resolvePath(rr.loopDir, step.Path)
			st, err := os.Stat(p)
			if err != nil {
				return fmt.Errorf("turn %s: prompt: %w", step.Name, err)
			}
			if !st.Mode().IsRegular() {
				return fmt.Errorf("turn %s: prompt %s is not a regular file", step.Name, step.Path)
			}
			if step.Scorecard != "" {
				if _, err := scorecard.ParseCard(resolvePath(rr.loopDir, step.Scorecard)); err != nil {
					return fmt.Errorf("turn %s: %w", step.Name, err)
				}
			}
		case manifest.Gate:
			if step.Path == "loop:frozen" {
				continue
			}
			p := resolvePath(rr.loopDir, step.Path)
			st, err := os.Stat(p)
			if err != nil {
				return fmt.Errorf("gate %s: %w", step.Name, err)
			}
			if !st.Mode().IsRegular() {
				return fmt.Errorf("gate %s: %s is not a regular file", step.Name, step.Path)
			}
			// The executable bit is the check. Running the suite here would spend
			// the work preflight exists to avoid, and a non-executable script is
			// what would otherwise exit 126 on iteration 1.
			if st.Mode()&0o111 == 0 {
				return fmt.Errorf("gate %s: %s is not executable", step.Name, step.Path)
			}
		}
	}
	return freezePatternsMatched(filepath.Join(rr.stateDir, "frozen"), rr.cfg.Freeze)
}

// freezePatternsMatched reads the sums Snapshot just wrote. An empty sum is a
// pattern that matched nothing. A file created later would be drift, after
// the run had already spent turns finding that out.
func freezePatternsMatched(frozenDir string, patterns []string) error {
	for i, pat := range patterns {
		b, err := os.ReadFile(filepath.Join(frozenDir, fmt.Sprintf("%d.sum", i+1)))
		if err != nil {
			return fmt.Errorf("LOOP_FREEZE pattern %q: %w", pat, err)
		}
		if strings.TrimSpace(string(b)) == "" {
			return fmt.Errorf("LOOP_FREEZE pattern %q matches no files", pat)
		}
	}
	return nil
}
