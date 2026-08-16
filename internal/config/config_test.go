package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "loop.env")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaults(t *testing.T) {
	c := Defaults()
	if c.MaxIter != 5 {
		t.Fatalf("MaxIter=%d", c.MaxIter)
	}
	if c.Session != SessionNone {
		t.Fatalf("Session=%q want none (avoid compaction)", c.Session)
	}
	if c.SessionTurns != 4 {
		t.Fatalf("SessionTurns=%d", c.SessionTurns)
	}
	if c.ForkPercent != 40 {
		t.Fatalf("ForkPercent=%d", c.ForkPercent)
	}
	if c.Compact != CompactWarn {
		t.Fatalf("Compact=%q", c.Compact)
	}
	if c.Branch {
		t.Fatal("Branch should default false")
	}
	if !c.Approve {
		t.Fatal("Approve should default true")
	}
	if c.PiPath != "pi" {
		t.Fatalf("PiPath=%q", c.PiPath)
	}
}

func TestLoadLoopEnv(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, ""+
		"# comment\n"+
		"LOOP_MAX_ITER=3\n"+
		"LOOP_SESSION=shared\n"+
		"LOOP_BRANCH=1\n"+
		"LOOP_BRANCH_BASE=develop\n"+
		"LOOP_WRITER_MODEL=xai/grok-4.5\n"+
		"LOOP_TEST_CMD=go test ./internal/...\n"+
		"LOOP_FREEZE='*_test.go gates/check.sh'\n")

	c, err := Load(dir, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 3 || c.Session != SessionShared || !c.Branch || c.BranchBase != "develop" {
		t.Fatalf("basic: %+v", c)
	}
	if c.Models["writer"] != "xai/grok-4.5" {
		t.Fatalf("models: %+v", c.Models)
	}
	if c.Extra["LOOP_TEST_CMD"] != "go test ./internal/..." {
		t.Fatalf("extra: %+v", c.Extra)
	}
	if len(c.Freeze) != 2 || c.Freeze[0] != "*_test.go" || c.Freeze[1] != "gates/check.sh" {
		t.Fatalf("freeze: %#v", c.Freeze)
	}
}

func TestOverlayBeatsFile(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_MAX_ITER=9\nLOOP_SESSION=shared\n")

	max := 2
	sess := SessionNone
	c, err := Load(dir, Overlay{MaxIter: &max, Session: &sess})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 2 {
		t.Fatalf("MaxIter=%d want 2 (overlay)", c.MaxIter)
	}
	if c.Session != SessionNone {
		t.Fatalf("Session=%q want none", c.Session)
	}
}

func TestRejectCommandSubst(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_CONTEXT=$(whoami)\n")
	if _, err := Load(dir, Overlay{}); err == nil {
		t.Fatal("expected reject of $()")
	}
}

func TestRejectBackticks(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_CONTEXT=`whoami`\n")
	if _, err := Load(dir, Overlay{}); err == nil {
		t.Fatal("expected reject of backticks")
	}
}

func TestQuotedValue(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_CONTEXT=\"fix the loader\"\n")
	c, err := Load(dir, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Context != "fix the loader" {
		t.Fatalf("Context=%q", c.Context)
	}
}

func TestExport(t *testing.T) {
	c := Defaults()
	c.MaxIter = 3
	c.Models = map[string]string{"writer": "xai/grok-4.5"}
	c.Extra = map[string]string{"LOOP_TEST_CMD": "go test ./..."}
	env := c.Environ()
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := splitKV(kv)
		got[k] = v
	}
	if got["LOOP_MAX_ITER"] != "3" {
		t.Fatalf("LOOP_MAX_ITER=%q", got["LOOP_MAX_ITER"])
	}
	if got["LOOP_WRITER_MODEL"] != "xai/grok-4.5" {
		t.Fatalf("LOOP_WRITER_MODEL=%q", got["LOOP_WRITER_MODEL"])
	}
	if got["LOOP_TEST_CMD"] != "go test ./..." {
		t.Fatalf("LOOP_TEST_CMD=%q", got["LOOP_TEST_CMD"])
	}
	if got["LOOP_SESSION"] != "none" {
		t.Fatalf("LOOP_SESSION=%q", got["LOOP_SESSION"])
	}
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
