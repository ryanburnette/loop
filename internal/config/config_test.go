package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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

// clearLoopEnv drops ambient LOOP_* so file-only tests stay honest when a
// parent loop (or the user's shell) has those vars set.
func clearLoopEnv(t *testing.T) {
	t.Helper()
	for _, e := range os.Environ() {
		k, _, ok := splitKV(e)
		if !ok || len(k) < 5 || k[:5] != "LOOP_" {
			continue
		}
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
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
	clearLoopEnv(t)
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
	clearLoopEnv(t)
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
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_CONTEXT=$(whoami)\n")
	if _, err := Load(dir, Overlay{}); err == nil {
		t.Fatal("expected reject of $()")
	}
}

func TestRejectBackticks(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_CONTEXT=`whoami`\n")
	if _, err := Load(dir, Overlay{}); err == nil {
		t.Fatal("expected reject of backticks")
	}
}

func TestQuotedValue(t *testing.T) {
	clearLoopEnv(t)
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

func TestProcessEnvBeatsFileOverlayBeatsEnv(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_MAX_ITER=9\n")
	t.Setenv("LOOP_MAX_ITER", "4")

	c, err := Load(dir, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 4 {
		t.Fatalf("process env should beat file: MaxIter=%d", c.MaxIter)
	}

	n := 2
	c, err = Load(dir, Overlay{MaxIter: &n})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 2 {
		t.Fatalf("overlay should beat process env: MaxIter=%d", c.MaxIter)
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

// TestOverlayTaggedFields is the guard against the overlayKeys maintenance
// trap: every pointer field on Overlay must carry a `loop:"LOOP_..."` tag so
// overlayKeys (derived by reflection) learns about it automatically. A new
// pointer field added without a tag would silently drop out of the settled set
// and produce false "env overrides loop.env" warnings for keys a flag actually
// decided. Models is exempt (it expands to LOOP_<ROLE>_MODEL by name).
func TestOverlayTaggedFields(t *testing.T) {
	tt := reflect.TypeOf(Overlay{})
	for i := 0; i < tt.NumField(); i++ {
		f := tt.Field(i)
		if f.Name == "Models" {
			continue
		}
		if f.Type.Kind() != reflect.Pointer {
			t.Fatalf("Overlay field %s is %s, not a pointer; only pointer fields are optional overlays", f.Name, f.Type)
		}
		if tag, ok := f.Tag.Lookup("loop"); !ok || tag == "" {
			t.Fatalf("Overlay field %s has no `loop:` tag; add one so overlayKeys learns the LOOP_* key it settles", f.Name)
		}
	}
}

// TestOverlayKeysSettled asserts overlayKeys reports exactly the keys whose
// pointer fields are set, plus the per-role model keys, and nothing more.
func TestOverlayKeysSettled(t *testing.T) {
	mi := 5
	m := SessionShared
	got := overlayKeys(Overlay{MaxIter: &mi, Session: &m, Models: map[string]string{"writer": "w", "reviewer": "r"}})
	want := map[string]bool{
		"LOOP_MAX_ITER":       true,
		"LOOP_SESSION":        true,
		"LOOP_WRITER_MODEL":   true,
		"LOOP_REVIEWER_MODEL": true,
	}
	if len(got) != len(want) {
		t.Fatalf("overlayKeys=%v want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("overlayKeys missing %s: %v", k, got)
		}
	}
	// Nothing set → nothing settled.
	if len(overlayKeys(Overlay{})) != 0 {
		t.Fatalf("empty overlay should settle nothing")
	}
}

// TestLoadAllKeys drives every applyMap case with a real loop.env and asserts
// the parsed Config, so each LOOP_* key branch is exercised on its actual
// effect (not just that it does not panic). Covers the integer, bool, string,
// list, and role-model branches, plus the unknown-key path.
func TestLoadAllKeys(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, strings.Join([]string{
		"LOOP_MAX_ITER=8",
		"LOOP_SESSION=fork",
		"LOOP_SESSION_TURNS=7",
		"LOOP_FORK_PERCENT=55",
		"LOOP_COMPACT=allow",
		"LOOP_BRANCH=1",
		"LOOP_BRANCH_BASE=dev",
		"LOOP_APPROVE=false",
		"LOOP_FREEZE=*_test.go *.sh",
		"LOOP_CONTEXT=extra ctx",
		"LOOP_NO_CONTEXT_FILES=true",
		"LOOP_TEST_CMD=pytest -q",
		"LOOP_PI=/usr/local/bin/pi",
		"LOOP_WRITER_MODEL=alpha",
		"LOOP_UNKNOWN_THING=ignored",
	}, "\n")+"\n")
	c, err := Load(dir, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 8 || c.Session != SessionFork || c.SessionTurns != 7 || c.ForkPercent != 55 {
		t.Fatalf("numeric/session fields: %+v", c)
	}
	if c.Compact != CompactAllow || c.Branch != true || c.BranchBase != "dev" || c.Approve != false {
		t.Fatalf("bool/string fields: %+v", c)
	}
	if !reflect.DeepEqual(c.Freeze, []string{"*_test.go", "*.sh"}) {
		t.Fatalf("Freeze=%v", c.Freeze)
	}
	if c.Context != "extra ctx" || c.NoContextFiles != true {
		t.Fatalf("context fields: %+v", c)
	}
	if c.TestCmd != "pytest -q" || c.PiPath != "/usr/local/bin/pi" {
		t.Fatalf("cmd/pi fields: %+v", c)
	}
	if c.Models["writer"] != "alpha" {
		t.Fatalf("role model not parsed: %v", c.Models)
	}
	if !slices.Contains(c.Unknown, "LOOP_UNKNOWN_THING") {
		t.Fatalf("unknown key not recorded: %v", c.Unknown)
	}
	if c.Extra["LOOP_TEST_CMD"] != "pytest -q" || c.Extra["LOOP_UNKNOWN_THING"] != "ignored" {
		t.Fatalf("Extra map: %v", c.Extra)
	}
}

// TestLoadBoolKeyVariants covers the true/false parsing shapes for LOOP_BRANCH,
// LOOP_APPROVE, and LOOP_NO_CONTEXT_FILES (1, true, TRUE, 0, anything-else).
func TestLoadBoolKeyVariants(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true}, {"true", true}, {"TRUE", true}, {"True", true},
		{"0", false}, {"false", false}, {"nope", false}, {"", false},
	}
	for _, key := range []string{"LOOP_BRANCH", "LOOP_APPROVE", "LOOP_NO_CONTEXT_FILES"} {
		for _, c := range cases {
			t.Run(key+"="+c.val, func(t *testing.T) {
				clearLoopEnv(t)
				dir := t.TempDir()
				writeEnv(t, dir, key+"="+c.val+"\n")
				cfg, err := Load(dir, Overlay{})
				if err != nil {
					t.Fatal(err)
				}
				got := false
				switch key {
				case "LOOP_BRANCH":
					got = cfg.Branch
				case "LOOP_APPROVE":
					got = cfg.Approve
				case "LOOP_NO_CONTEXT_FILES":
					got = cfg.NoContextFiles
				}
				if got != c.want {
					t.Fatalf("%s=%q → %v want %v", key, c.val, got, c.want)
				}
			})
		}
	}
}

// TestLoadIntegerParseErrors covers the three Atoi error returns in applyMap.
func TestLoadIntegerParseErrors(t *testing.T) {
	for _, key := range []string{"LOOP_MAX_ITER", "LOOP_SESSION_TURNS", "LOOP_FORK_PERCENT"} {
		t.Run(key, func(t *testing.T) {
			clearLoopEnv(t)
			dir := t.TempDir()
			writeEnv(t, dir, key+"=not-a-number\n")
			_, err := Load(dir, Overlay{})
			if err == nil {
				t.Fatalf("%s with a non-integer should error", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("error should name %s: %v", key, err)
			}
		})
	}
}

// TestApplyOverlayAllFields sets every Overlay pointer field and asserts each
// wins over the loop.env/default value, covering every applyOverlay branch.
func TestApplyOverlayAllFields(t *testing.T) {
	clearLoopEnv(t)
	dir := t.TempDir()
	writeEnv(t, dir, "LOOP_MAX_ITER=2\nLOOP_BRANCH=0\nLOOP_APPROVE=1\n")
	mi := 9
	st := SessionFork
	stt := 6
	fp := 50
	cp := CompactAllow
	br := true
	bb := "dev"
	ap := false
	fz := []string{"a_test.go", "b_test.go"}
	ctx := "flag-ctx"
	ncf := true
	tc := "make test"
	pp := "/flag/pi"
	o := Overlay{
		MaxIter:        &mi,
		Session:        &st,
		SessionTurns:   &stt,
		ForkPercent:    &fp,
		Compact:        &cp,
		Branch:         &br,
		BranchBase:     &bb,
		Approve:        &ap,
		Freeze:         &fz,
		Context:        &ctx,
		NoContextFiles: &ncf,
		Models:         map[string]string{"writer": "flag-w"},
		TestCmd:        &tc,
		PiPath:         &pp,
	}
	c, err := Load(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIter != 9 || c.Session != SessionFork || c.SessionTurns != 6 || c.ForkPercent != 50 {
		t.Fatalf("overlay numerics: %+v", c)
	}
	if c.Compact != CompactAllow || c.Branch != true || c.BranchBase != "dev" || c.Approve != false {
		t.Fatalf("overlay bools/strings: %+v", c)
	}
	if !reflect.DeepEqual(c.Freeze, []string{"a_test.go", "b_test.go"}) {
		t.Fatalf("overlay Freeze=%v", c.Freeze)
	}
	if c.Context != "flag-ctx" || c.NoContextFiles != true || c.TestCmd != "make test" || c.PiPath != "/flag/pi" {
		t.Fatalf("overlay strings: %+v", c)
	}
	if c.Models["writer"] != "flag-w" {
		t.Fatalf("overlay models: %v", c.Models)
	}
	// Freeze overlay must be a copy, not alias the caller's slice.
	fz[0] = "mutated"
	if c.Freeze[0] == "mutated" {
		t.Fatal("Freeze overlay must copy the slice, not alias it")
	}
}
