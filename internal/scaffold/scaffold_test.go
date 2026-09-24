package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ryanburnette/loop/internal/manifest"
	"github.com/ryanburnette/loop/internal/scorecard"
)

func TestNamesReturnsAllFourSorted(t *testing.T) {
	got := Names()
	want := []string{"double-check", "two-model-critique", "until-count", "until-green"}
	if len(got) != len(want) {
		t.Fatalf("Names()=%v want %v", got, want)
	}
	for i, n := range want {
		if got[i] != n {
			t.Fatalf("Names()[%d]=%q want %q (got %v)", i, got[i], n, got)
		}
	}
}

func TestDefaultOrEmptyReturnsUntilGreen(t *testing.T) {
	if got := DefaultOr(""); got != "until-green" {
		t.Fatalf("DefaultOr(\"\")=%q want until-green", got)
	}
	if got := DefaultOr("double-check"); got != "double-check" {
		t.Fatalf("DefaultOr(double-check)=%q want double-check (passthrough)", got)
	}
}

func TestScaffoldUnknownTemplateErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".loop")
	err := Scaffold(dir, "not-a-real-template")
	if err == nil {
		t.Fatal("expected an error for an unknown template")
	}
	if !strings.Contains(err.Error(), "not-a-real-template") {
		t.Fatalf("error should name the bad template, got %q", err)
	}
}

func TestScaffoldRefusesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".loop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Scaffold(dir, "until-green"); err == nil {
		t.Fatal("expected Scaffold to refuse an existing dir")
	}
}

func TestScaffoldEmptyNameUsesDefault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".loop")
	if err := Scaffold(dir, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "01-writer.md")); err != nil {
		t.Fatalf("empty name should scaffold until-green: %v", err)
	}
}

// The scaffolded recipe must hide itself from git without the project having
// to edit its own .gitignore. Asserting on the file's contents would only
// restate the implementation, so this drives real git and checks the property
// that matters: after `loop init`, `git status` is clean.
func TestScaffoldedLoopDirIsInvisibleToGit(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", "README")
	git("commit", "-qm", "init")

	if err := Scaffold(filepath.Join(root, ".loop"), "until-green"); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("a scaffolded .loop/ must leave the tree clean, git reported:\n%s", out)
	}

	// The recipe is hidden, not missing.
	for _, rel := range []string{"loop.env", "TODO.md", "prompts/01-writer.md", "gates/tests.sh"} {
		if _, err := os.Stat(filepath.Join(root, ".loop", rel)); err != nil {
			t.Fatalf("scaffolded file %s should exist: %v", rel, err)
		}
	}
}

func TestScaffoldGateAndHookFilesAreExecutable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".loop")
	if err := Scaffold(dir, "until-green"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "gates", "tests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("gates/tests.sh should be executable, mode=%s", fi.Mode())
	}
}

func TestAllTemplatesScaffoldWithRealContent(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".loop")
			if err := Scaffold(dir, name); err != nil {
				t.Fatal(err)
			}
			entries := 0
			err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || strings.HasSuffix(p, ".gitignore") {
					return nil
				}
				entries++
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				s := string(b)
				if strings.TrimSpace(s) == "" {
					t.Errorf("%s: empty file", p)
				}
				lower := strings.ToLower(s)
				if strings.Contains(lower, "todo: write") || strings.Contains(lower, "lorem ipsum") {
					t.Errorf("%s: looks like placeholder content: %q", p, s[:min(80, len(s))])
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if entries == 0 {
				t.Fatal("template scaffolded zero files")
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestTemplatesAreDistinctFromEachOther(t *testing.T) {
	seen := map[string]string{} // loop.env content -> template name
	for _, name := range Names() {
		tpl := Templates[name]
		env := tpl.Files["loop.env"]
		if prior, ok := seen[env]; ok {
			t.Fatalf("%s and %s have identical loop.env content", name, prior)
		}
		seen[env] = name
	}
}

func TestTwoModelCritiqueMentionsReviewerModel(t *testing.T) {
	env := Templates["two-model-critique"].Files["loop.env"]
	if !strings.Contains(env, "LOOP_REVIEWER_MODEL") {
		t.Fatalf("two-model-critique's loop.env should mention LOOP_REVIEWER_MODEL, got:\n%s", env)
	}
}

// Parse the scaffolded manifest and the rule-all card. This does not run a
// template against a model.
func TestScorecardTemplatesParse(t *testing.T) {
	cases := []struct {
		name      string
		judge     string
		wantSteps int
		wantGate  bool
	}{
		{name: "double-check", judge: "critic", wantSteps: 2, wantGate: false},
		{name: "two-model-critique", judge: "reviewer", wantSteps: 4, wantGate: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".loop")
			if err := Scaffold(dir, tc.name); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "manifest"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "verdict=") {
				t.Fatalf("new template must not use verdict=:\n%s", raw)
			}
			if !strings.Contains(string(raw), "scorecard=") {
				t.Fatalf("manifest missing scorecard=:\n%s", raw)
			}
			m, err := manifest.ParseFile(filepath.Join(dir, "manifest"))
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Warnings) != 0 {
				t.Fatalf("warnings: %v", m.Warnings)
			}
			if len(m.Steps) != tc.wantSteps {
				t.Fatalf("steps=%d want %d: %+v", len(m.Steps), tc.wantSteps, m.Steps)
			}
			sawCard := false
			sawGate := false
			for _, s := range m.Steps {
				if s.Verdict != "" {
					t.Fatalf("parsed verdict: %+v", s)
				}
				if s.Type == manifest.Gate {
					sawGate = true
					if !s.Required {
						t.Fatalf("gate should be required: %+v", s)
					}
				}
				if s.Scorecard == "" {
					continue
				}
				if s.Type != manifest.Turn || s.Name != tc.judge {
					t.Fatalf("scorecard step: %+v", s)
				}
				if s.Required {
					t.Fatalf("scorecard should be required=0: %+v", s)
				}
				sawCard = true
				card, err := scorecard.ParseCard(filepath.Join(dir, s.Scorecard))
				if err != nil {
					t.Fatal(err)
				}
				if card.Rule != "all" || card.Need < 1 {
					t.Fatalf("card rule=%q need=%d", card.Rule, card.Need)
				}
			}
			if !sawCard {
				t.Fatal("no scorecard step parsed")
			}
			if sawGate != tc.wantGate {
				t.Fatalf("gate present=%v want %v", sawGate, tc.wantGate)
			}
			env, err := os.ReadFile(filepath.Join(dir, "loop.env"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(env), "LOOP_SESSION=none") {
				t.Fatalf("session:\n%s", env)
			}
			if strings.Contains(string(env), "LOOP_SESSION=shared") {
				t.Fatalf("template must not default to shared:\n%s", env)
			}
		})
	}
}

func TestDoubleCheckIsOneSoftPass(t *testing.T) {
	env := Templates["double-check"].Files["loop.env"]
	if !strings.Contains(env, "LOOP_MAX_ITER=1") || !strings.Contains(env, "LOOP_SESSION=none") {
		t.Fatalf("double-check caps and session:\n%s", env)
	}
	prompt := Templates["double-check"].Files["prompts/02-critic.md"]
	if !strings.Contains(prompt, "Write only the JSON path the runner names") {
		t.Fatalf("critic prompt should name the runner JSON path:\n%s", prompt)
	}
	if strings.Contains(prompt, "fix the things") || strings.Contains(prompt, "VERDICT:") {
		t.Fatalf("critic prompt still tells the model to fix or emit a verdict:\n%s", prompt)
	}
}

func TestTwoModelFixerReadsTheBrief(t *testing.T) {
	fixer := Templates["two-model-critique"].Files["prompts/03-fixer.md"]
	if !strings.Contains(fixer, "Read the brief the runner attached") {
		t.Fatalf("fixer prompt:\n%s", fixer)
	}
	if strings.Contains(fixer, "in this session") {
		t.Fatalf("fixer prompt still points at the session:\n%s", fixer)
	}
	reviewer := Templates["two-model-critique"].Files["prompts/02-reviewer.md"]
	if !strings.Contains(reviewer, "Write only the JSON path the runner names") {
		t.Fatalf("reviewer prompt:\n%s", reviewer)
	}
	if strings.Contains(reviewer, "fix the things") || strings.Contains(reviewer, "VERDICT:") {
		t.Fatalf("reviewer prompt still tells the model to fix or emit a verdict:\n%s", reviewer)
	}
	env := Templates["two-model-critique"].Files["loop.env"]
	for _, pin := range []string{"LOOP_WRITER_MODEL", "LOOP_REVIEWER_MODEL", "LOOP_FIXER_MODEL"} {
		if !strings.Contains(env, pin) {
			t.Fatalf("missing %s:\n%s", pin, env)
		}
	}
	if !strings.Contains(env, "assurance is gated") {
		t.Fatalf("template should say the tests gate makes assurance gated:\n%s", env)
	}
}

func TestUntilGreenHasNoScorecard(t *testing.T) {
	var blob strings.Builder
	for _, body := range Templates["until-green"].Files {
		blob.WriteString(body)
	}
	text := blob.String()
	if strings.Contains(text, "scorecard=") || strings.Contains(text, "verdict=") {
		t.Fatal("until-green stays a writer turn plus a shell gate")
	}
	if !strings.Contains(Templates["until-green"].Files["loop.env"], "return.md") {
		t.Fatal("until-green comment should point at return.md")
	}
	if strings.Contains(strings.ToLower(text), "index passes") {
		t.Fatal("until-green must not say a missing freeze index passes")
	}
}

func TestUntilCountKeepsDoneScript(t *testing.T) {
	gate := Templates["until-count"].Files["gates/done.sh"]
	if !strings.Contains(gate, "grep -qx DONE") {
		t.Fatalf("done gate:\n%s", gate)
	}
	env := Templates["until-count"].Files["loop.env"]
	if !strings.Contains(env, "not a stronger check than a scorecard") {
		t.Fatalf("until-count must not be ranked above a scorecard:\n%s", env)
	}
	if !strings.Contains(env, "LOOP_STALL_PATHS=FINDINGS.md") {
		t.Fatalf("until-count should hash findings so an append is progress:\n%s", env)
	}
	if !strings.Contains(env, "?? FINDINGS.md") {
		t.Fatalf("comment should say porcelain only sees the file appear:\n%s", env)
	}
	if _, ok := Templates["until-count"].Files["manifest"]; ok {
		t.Fatal("until-count stays convention-derived")
	}
}

func TestUntilGreenIsConventionDerivedNoManifest(t *testing.T) {
	if _, ok := Templates["until-green"].Files["manifest"]; ok {
		t.Fatal("until-green should be convention-derived (no manifest file), to exercise that v0.3 feature")
	}
}

func TestUntilCountIsConventionDerivedNoManifest(t *testing.T) {
	if _, ok := Templates["until-count"].Files["manifest"]; ok {
		t.Fatal("until-count should be convention-derived (no manifest file)")
	}
}

func TestScaffoldDeterministicFileModes(t *testing.T) {
	// Non-gate/hook files must not be executable.
	dir := filepath.Join(t.TempDir(), ".loop")
	if err := Scaffold(dir, "until-green"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "loop.env"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 != 0 {
		t.Fatalf("loop.env should not be executable, mode=%s", fi.Mode())
	}
}

func TestScaffoldStatErrorOtherThanNotExistIsReported(t *testing.T) {
	// A dir path that can never be created (parent is a file, not a dir)
	// must surface a real error, not silently succeed or panic.
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(blocker, ".loop") // blocker is a file, not a dir
	if err := Scaffold(dir, "until-green"); err == nil {
		t.Fatal("expected an error when the parent path is not a directory")
	}
}

func init() {
	// Guard against a future template being added without a Names() entry.
	if len(Templates) != len(Names()) {
		panic("Templates/Names count mismatch: " + strconv.Itoa(len(Templates)) + " vs " + strconv.Itoa(len(Names())))
	}
}
