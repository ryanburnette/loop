package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseTurnGateHook(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest", ""+
		"# comment\n"+
		"\n"+
		"turn writer prompts/writer.md model=writer\n"+
		"gate tests gates/tests.sh\n"+
		"hook fmt hooks/fmt.sh\n")

	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Steps) != 3 {
		t.Fatalf("steps: got %d want 3", len(m.Steps))
	}
	if m.Steps[0].Type != Turn || m.Steps[0].Name != "writer" || m.Steps[0].Path != "prompts/writer.md" {
		t.Fatalf("turn: %+v", m.Steps[0])
	}
	if m.Steps[0].Model != "writer" || m.Steps[0].Required != true {
		t.Fatalf("turn keys: %+v", m.Steps[0])
	}
	if m.Steps[1].Type != Gate || m.Steps[1].Name != "tests" || !m.Steps[1].Required {
		t.Fatalf("gate: %+v", m.Steps[1])
	}
	if m.Steps[2].Type != Hook || m.Steps[2].Name != "fmt" {
		t.Fatalf("hook: %+v", m.Steps[2])
	}
}

func TestVerdictConsumesRestOfLine(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest",
		"turn reviewer prompts/reviewer.md model=reviewer verdict=^VERDICT: PASS\n")

	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Steps[0].Verdict != "^VERDICT: PASS" {
		t.Fatalf("verdict: %q", m.Steps[0].Verdict)
	}
	if m.Steps[0].Model != "reviewer" {
		t.Fatalf("model dropped: %+v", m.Steps[0])
	}
}

func TestVerdictDropsTrailingKeys(t *testing.T) {
	// Load-bearing v1 gotcha: verdict= eats the rest of the line.
	dir := t.TempDir()
	p := write(t, dir, "manifest",
		"turn reviewer prompts/r.md verdict=^VERDICT: PASS required=0\n")

	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Steps[0].Verdict != "^VERDICT: PASS required=0" {
		t.Fatalf("verdict should swallow trailing keys, got %q", m.Steps[0].Verdict)
	}
	if !m.Steps[0].Required {
		t.Fatal("required should stay at default 1 when it comes after verdict=")
	}
}

func TestSystemConsumesRestOfLine(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest",
		"turn writer prompts/w.md system=be brief and precise\n")

	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Steps[0].System != "be brief and precise" {
		t.Fatalf("system: %q", m.Steps[0].System)
	}
}

func TestHasObjective(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"gate default required", "turn w p.md\ngate t g.sh\n", true},
		{"gate required 0", "turn w p.md\ngate t g.sh required=0\n", false},
		{"verdict required", "turn r p.md verdict=^VERDICT: PASS\n", true},
		{"verdict not required", "turn r p.md required=0 verdict=^VERDICT: PASS\n", false},
		{"no check", "turn w p.md\nturn c p.md\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := write(t, dir, "manifest", tc.body)
			m, err := ParseFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.HasObjective(); got != tc.want {
				t.Fatalf("HasObjective=%v want %v", got, tc.want)
			}
		})
	}
}

func TestUnknownType(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest", "flarp writer prompts/w.md\n")
	if _, err := ParseFile(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestShortLine(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "manifest", "turn writer\n")
	if _, err := ParseFile(p); err == nil {
		t.Fatal("expected error")
	}
}

func mkfile(t *testing.T, dir string, rel string, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPrefersExplicitManifest(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "manifest", "turn only-this gates/tests.sh\n")
	mkfile(t, dir, "prompts/01-writer.md", "go\n")

	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Steps) != 1 || m.Steps[0].Name != "only-this" {
		t.Fatalf("explicit manifest should win, got %+v", m.Steps)
	}
}

func TestDeriveFromPromptsGatesHooks(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "prompts/02-reviewer.md", "go\n")
	mkfile(t, dir, "prompts/01-writer.md", "go\n")
	mkfile(t, dir, "gates/tests.sh", "#!/bin/sh\nexit 0\n")
	mkfile(t, dir, "hooks/notify.sh", "#!/bin/sh\ntrue\n")

	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Steps) != 4 {
		t.Fatalf("steps: got %d want 4: %+v", len(m.Steps), m.Steps)
	}
	// turns first, in lexical filename order; numeric prefix stripped from Name.
	if m.Steps[0].Type != Turn || m.Steps[0].Name != "writer" || m.Steps[0].Path != "prompts/01-writer.md" {
		t.Fatalf("step0: %+v", m.Steps[0])
	}
	if m.Steps[1].Type != Turn || m.Steps[1].Name != "reviewer" || m.Steps[1].Path != "prompts/02-reviewer.md" {
		t.Fatalf("step1: %+v", m.Steps[1])
	}
	// then gates, required by default.
	if m.Steps[2].Type != Gate || m.Steps[2].Name != "tests" || !m.Steps[2].Required {
		t.Fatalf("step2: %+v", m.Steps[2])
	}
	// then hooks, last.
	if m.Steps[3].Type != Hook || m.Steps[3].Name != "notify" {
		t.Fatalf("step3: %+v", m.Steps[3])
	}
}

func TestDeriveNameStripsLeadingNumericPrefixOnly(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "prompts/writer.md", "go\n") // no numeric prefix

	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Steps[0].Name != "writer" {
		t.Fatalf("Name=%q want writer", m.Steps[0].Name)
	}
}

func TestDeriveEmptyDirErrorsClearly(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error when there is no manifest and no prompts/gates/hooks")
	}
}
