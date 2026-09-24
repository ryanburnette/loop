package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanburnette/loop/internal/config"
)

// Stale LOOP_* from Extra must not survive next to the value this run
// computed. getenv on glibc returns the first match.
func TestBuildEnvEmitsRuntimeKeysOnce(t *testing.T) {
	t.Setenv("LOOP_STATE_DIR", "from-process")
	t.Setenv("LOOP_ITERATION", "9")
	t.Setenv("LOOP_NOT_A_REAL_KEY", "leak")
	t.Setenv("LOOP_ENV_SENTINEL", "kept")

	cfg := config.Defaults()
	cfg.TestCmd = "go test ./..."
	cfg.Extra = map[string]string{
		"LOOP_STATE_DIR":    "stale",
		"LOOP_ITERATION":    "0",
		"LOOP_TEST_CMD":     "stale-cmd",
		"LOOP_FINDINGS":     "FINDINGS.md",
		"LOOP_ENV_SENTINEL": "kept",
	}

	const (
		id       = "20260924T120000Z-1"
		loopDir  = "/loop"
		workroot = "/work"
		stateDir = "/state/real"
		branch   = "loop/20260924T120000Z-1"
		iter     = 3
		phase    = "tests"
	)
	env := buildEnv(cfg, id, loopDir, workroot, stateDir, branch, iter, phase)

	got := map[string][]string{}
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			t.Fatalf("env entry %q has no '='", e)
		}
		got[k] = append(got[k], v)
	}

	want := map[string]string{
		"LOOP_ID":           id,
		"LOOP_ROOT":         loopDir,
		"LOOP_WORKROOT":     workroot,
		"LOOP_STATE_DIR":    stateDir,
		"LOOP_BRANCH_NAME":  branch,
		"LOOP_ITERATION":    "3",
		"LOOP_PHASE":        phase,
		"LOOP_LOG":          filepath.Join(stateDir, "gate-log.md"),
		"LOOP_TEST_CMD":     "go test ./...",
		"LOOP_FINDINGS":     "FINDINGS.md",
		"LOOP_ENV_SENTINEL": "kept",
		"LOOP_MAX_ITER":     "5",
		"LOOP_SESSION":      "none",
		"LOOP_MEND":         filepath.Join(stateDir, "mend.md"),
		"LOOP_BRIEF":        filepath.Join(stateDir, "brief.md"),
		"LOOP_RETURN":       filepath.Join(stateDir, "return.md"),
	}
	for k, v := range want {
		vals := got[k]
		if len(vals) != 1 || vals[0] != v {
			t.Fatalf("%s = %q, want exactly %q", k, vals, v)
		}
	}
	for _, absent := range []string{"LOOP_NOT_A_REAL_KEY"} {
		if _, ok := got[absent]; ok {
			t.Fatalf("%s leaked into the gate environment: %q", absent, got[absent])
		}
	}
	for k, vals := range got {
		if len(vals) != 1 {
			t.Fatalf("%s appears %d times: %q", k, len(vals), vals)
		}
	}
	if _, ok := got["PATH"]; !ok {
		if _, has := os.LookupEnv("PATH"); has {
			t.Fatal("PATH was dropped from the gate environment")
		}
	}
}
