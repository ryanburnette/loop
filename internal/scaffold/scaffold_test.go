package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

func TestTwoModelCritiqueManifestHasVerdict(t *testing.T) {
	m := Templates["two-model-critique"].Files["manifest"]
	if !strings.Contains(m, "verdict=") {
		t.Fatalf("two-model-critique's manifest should carry a verdict= key, got:\n%s", m)
	}
}

func TestDoubleCheckManifestVerdictIsSoft(t *testing.T) {
	m := Templates["double-check"].Files["manifest"]
	if !strings.Contains(m, "required=0") {
		t.Fatalf("double-check's reviewer verdict should be soft (required=0), got:\n%s", m)
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
