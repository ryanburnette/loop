package config

import (
	"slices"
	"testing"
)

// Process env beating loop.env is the intended layering, but it is otherwise
// invisible — an ambient LOOP_MAX_ITER silently replaces the recipe's cap.
// Load records the conflict so the runner can say so.
func TestOverriddenRecordsEnvBeatingFile(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_MAX_ITER=2\nLOOP_SESSION=none\n")
	t.Setenv("LOOP_MAX_ITER", "9")

	c, err := Load(dir, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 9 {
		t.Fatalf("MaxIter=%d want 9: env must still win", c.MaxIter)
	}
	if !slices.Contains(c.Overridden, "LOOP_MAX_ITER") {
		t.Fatalf("Overridden=%v want LOOP_MAX_ITER", c.Overridden)
	}
	if slices.Contains(c.Overridden, "LOOP_SESSION") {
		t.Fatalf("LOOP_SESSION was not overridden: %v", c.Overridden)
	}
}

// The same value in both places is not a conflict worth reporting.
func TestOverriddenIgnoresEqualValues(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_MAX_ITER=2\n")
	t.Setenv("LOOP_MAX_ITER", "2")

	c, err := Load(dir, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Overridden) != 0 {
		t.Fatalf("Overridden=%v want empty", c.Overridden)
	}
}

// A flag settles the question, so blaming the environment would mislead.
func TestOverriddenIgnoresKeysAFlagSettles(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_MAX_ITER=2\n")
	t.Setenv("LOOP_MAX_ITER", "9")

	n := 3
	c, err := Load(dir, Overlay{MaxIter: &n})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 3 {
		t.Fatalf("MaxIter=%d want 3: the flag wins", c.MaxIter)
	}
	if len(c.Overridden) != 0 {
		t.Fatalf("Overridden=%v want empty when a flag decided", c.Overridden)
	}
}

// Precedence overall: defaults < loop.env < process env < flags.
func TestPrecedenceOrder(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_MAX_ITER=2\nLOOP_BRANCH_BASE=from-file\nLOOP_COMPACT=allow\n")
	t.Setenv("LOOP_BRANCH_BASE", "from-env")

	base := "from-flag"
	c, err := Load(dir, Overlay{BranchBase: &base})
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionTurns != 4 {
		t.Fatalf("SessionTurns=%d want the default 4", c.SessionTurns)
	}
	if c.MaxIter != 2 {
		t.Fatalf("MaxIter=%d want 2 from loop.env", c.MaxIter)
	}
	if c.Compact != CompactAllow {
		t.Fatalf("Compact=%q want allow from loop.env", c.Compact)
	}
	if c.BranchBase != "from-flag" {
		t.Fatalf("BranchBase=%q want from-flag", c.BranchBase)
	}
}
