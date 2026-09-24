package mend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryanburnette/loop/internal/manifest"
	"github.com/ryanburnette/loop/internal/scaffold"
)

func TestFormatElapsed(t *testing.T) {
	if got := FormatElapsed(2*time.Hour + 14*time.Minute); got != "2h14m" {
		t.Fatalf("elapsed: %s", got)
	}
	if got := FormatElapsed(90 * time.Second); got != "1m30s" {
		t.Fatalf("elapsed: %s", got)
	}
	if got := FormatElapsed(45 * time.Second); got != "45s" {
		t.Fatalf("elapsed: %s", got)
	}
}

func TestFailSampleStillRedAndPassed(t *testing.T) {
	page := RenderReturn(Page{
		Result:     ResultFail,
		Assurance:  AssuranceGated,
		Gloss:      GatedGloss,
		Iter:       4,
		MaxIter:    8,
		Elapsed:    2*time.Hour + 14*time.Minute,
		Branch:     "loop/20260924T120000Z-12345",
		BranchText: "branch loop/20260924T120000Z-12345",
		Workroot:   "/Users/ryan/Developer/foo",
		Head:       "abc1234",
		Checks: []Check{
			{
				Kind: "gate", Name: "tests", OK: false, Exit: 1, Required: true,
				Tail: []string{"--- FAIL: TestParse (internal/manifest/manifest_test.go)"},
			},
			{
				Kind: "scorecard", Name: "reviewer", OK: false, Required: false,
				Render: "rule all, 2/5 required met",
				Marks:  []Mark{{ID: "tests", Label: "unmet", Because: "TestLogin was deleted"}},
			},
		},
		Mend:     "state/id/mend.md",
		GateLog:  "state/id/gate-log.md",
		LastTurn: "state/id/turn-4-writer.md",
	})
	if strings.Contains(page, "exited 0") {
		t.Fatalf("gated page must not say a gate exited 0:\n%s", page)
	}
	wantAssurance := "assurance: gated — " + GatedGloss
	if !strings.Contains(page, wantAssurance) {
		t.Fatalf("assurance line:\n%s", page)
	}
	red := pageSection(page, "Still red")
	passed := pageSection(page, "Passed")
	if !strings.Contains(red, "- gate tests: FAIL exit 1 — --- FAIL: TestParse (internal/manifest/manifest_test.go)") {
		t.Fatalf("failing gate not under Still red:\n%s", red)
	}
	if !strings.Contains(red, "- scorecard reviewer: FAIL rule all, 2/5 required met (soft) — tests unmet: TestLogin was deleted") {
		t.Fatalf("failing scorecard not under Still red:\n%s", red)
	}
	if strings.TrimSpace(passed) != "(none)" {
		t.Fatalf("Passed want (none), got %q", passed)
	}
	next := pageSection(page, "Next")
	wantNext := "The cap was spent with a required check still red. Nothing was merged.\nThe work is on branch loop/20260924T120000Z-12345. Read the diff, not the model's summary."
	if strings.TrimSpace(next) != wantNext {
		t.Fatalf("fail Next:\n%q", next)
	}
}

func TestNextParagraphs(t *testing.T) {
	cases := []struct {
		name string
		page Page
		want string
	}{
		{
			name: "running",
			page: Page{Result: ResultRunning, Iter: 3, MaxIter: 8},
			want: "The run is in progress at iteration 3 of 8. The phase is the status line. Nothing has been merged.",
		},
		{
			name: "success-branch",
			page: Page{Result: ResultSuccess, Iter: 2, BranchText: "branch loop/id"},
			want: "Required checks passed on iteration 2. Nothing was merged. The work is on branch loop/id. Read the diff, not the model's summary.",
		},
		{
			name: "success-current",
			page: Page{Result: ResultSuccess, Iter: 1},
			want: "Required checks passed on iteration 1. Nothing was merged. The work is on the current branch. Read the diff, not the model's summary.",
		},
		{
			name: "fail-current",
			page: Page{Result: ResultFail},
			want: "The cap was spent with a required check still red. Nothing was merged.\nThe work is on the current branch. Read the diff, not the model's summary.",
		},
		{
			name: "stopped",
			page: Page{Result: ResultStopped, Iter: 2, BranchText: "branch loop/id"},
			want: "The operator stopped the run during iteration 2 (control file or signal). Nothing was merged. The work is on branch loop/id if one was created.",
		},
		{
			name: "stopped-no-branch",
			page: Page{Result: ResultStopped, Iter: 1},
			want: "The operator stopped the run during iteration 1 (control file or signal). Nothing was merged. The work is on the current branch if one was created.",
		},
		{
			name: "done",
			page: Page{Result: ResultDone},
			want: "The cap was reached. This loop had no required check, so finishing the cap is not a pass. Nothing was merged.",
		},
		{
			name: "stalled",
			page: Page{Result: ResultStalled, Iter: 4, StallSignature: "gate tests exit 1", StallHead: "abc1234"},
			want: "Stopped after iteration 4 because the failure signature and the tree matched iteration 3. The signature is gate tests exit 1. HEAD abc1234. Nothing was merged. LOOP_STALL=continue is how this recipe keeps going.",
		},
		{
			name: "recipe-no-turn",
			page: Page{Result: ResultRecipe, RecipeNoTurn: true, RecipeBranch: "loop/id"},
			want: "The recipe failed. No pi turn ran. Branch setup created loop/id. Preflight runs after branch setup, and a preflight failure does not delete that branch.",
		},
		{
			name: "recipe-gate",
			page: Page{Result: ResultRecipe, RecipeGate: "tests", RecipeCode: 127, RecipeIter: 1},
			want: "The recipe failed. Required gate tests exited 127 on iteration 1.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Next(tc.page); got != tc.want {
				t.Fatalf("Next:\n got %q\nwant %q", got, tc.want)
			}
			// Every result renders, including the ones the runner does not stop on yet.
			body := RenderReturn(tc.page)
			if !strings.Contains(body, "result: "+tc.page.Result) || !strings.Contains(body, tc.want) {
				t.Fatalf("render missing result or Next:\n%s", body)
			}
		})
	}
}

func TestPassedGateNotStillRed(t *testing.T) {
	body := RenderReturn(Page{
		Result: ResultSuccess,
		Checks: []Check{
			{Kind: "gate", Name: "tests", OK: true, Exit: 0, Required: true},
			{Kind: "gate", Name: "lint", OK: false, Exit: 1, Required: true, Tail: []string{"boom"}},
		},
	})
	red := pageSection(body, "Still red")
	passed := pageSection(body, "Passed")
	if strings.Contains(red, "tests") || !strings.Contains(red, "- gate lint: FAIL exit 1 — boom") {
		t.Fatalf("Still red:\n%s", red)
	}
	if strings.TrimSpace(passed) != "- gate tests: OK exit 0" {
		t.Fatalf("Passed:\n%s", passed)
	}
}

func TestAssurance(t *testing.T) {
	t.Run("gated freeze-only", func(t *testing.T) {
		man := loadManifest(t, "gate frozen loop:frozen\n")
		got, gloss := DecideAssurance(man, func(string) string { return "" })
		if got != AssuranceGated {
			t.Fatalf("assurance %s", got)
		}
		if gloss != GatedGloss || strings.Contains(gloss, "exited 0") {
			t.Fatalf("gloss %q", gloss)
		}
	})
	t.Run("gated until-count", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "loop")
		if err := scaffold.Scaffold(dir, "until-count"); err != nil {
			t.Fatal(err)
		}
		man, err := manifest.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		// The hunt role is unset, so the resolved model is empty. The DONE
		// gate is still a required gate, so this is gated, not self-graded.
		got, gloss := DecideAssurance(man, func(string) string { return "" })
		if got != AssuranceGated {
			t.Fatalf("until-count assurance %s, steps %+v", got, man.Steps)
		}
		if gloss != GatedGloss || strings.Contains(gloss, "exited 0") {
			t.Fatalf("gloss %q", gloss)
		}
	})
	t.Run("gated beats empty model", func(t *testing.T) {
		man := loadManifest(t, ""+
			"turn writer prompts/w.md model=writer\n"+
			"turn judge prompts/j.md model=judge scorecard=scorecards/r.card\n"+
			"gate frozen loop:frozen\n")
		got, _ := DecideAssurance(man, func(role string) string {
			if role == "judge" {
				return "other/model"
			}
			return ""
		})
		if got != AssuranceGated {
			t.Fatalf("empty actor must not override a required gate, got %s", got)
		}
	})
	t.Run("cross-model", func(t *testing.T) {
		man := loadManifest(t, ""+
			"turn writer prompts/w.md model=writer\n"+
			"turn judge prompts/j.md model=judge scorecard=scorecards/r.card\n")
		got, gloss := DecideAssurance(man, func(role string) string {
			switch role {
			case "writer":
				return "alpha/one"
			case "judge":
				return "beta/two"
			}
			return ""
		})
		if got != AssuranceCrossModel || gloss != "" {
			t.Fatalf("got %s gloss %q", got, gloss)
		}
	})
	t.Run("self-graded empty actor", func(t *testing.T) {
		man := loadManifest(t, ""+
			"turn writer prompts/w.md model=writer\n"+
			"turn judge prompts/j.md model=judge scorecard=scorecards/r.card\n")
		got, gloss := DecideAssurance(man, func(role string) string {
			if role == "judge" {
				return "other/model"
			}
			return ""
		})
		if got != AssuranceSelfGraded {
			t.Fatalf("empty actor want self-graded, got %s", got)
		}
		if gloss != SelfGradedGloss {
			t.Fatalf("gloss %q", gloss)
		}
	})
	t.Run("self-graded two empty", func(t *testing.T) {
		man := loadManifest(t, ""+
			"turn writer prompts/w.md model=writer\n"+
			"turn judge prompts/j.md model=judge scorecard=scorecards/r.card\n")
		got, _ := DecideAssurance(man, func(string) string { return "" })
		if got != AssuranceSelfGraded {
			t.Fatalf("two empty strings want self-graded, got %s", got)
		}
	})
	t.Run("verdict", func(t *testing.T) {
		man := loadManifest(t, "turn critic prompts/c.md model=critic verdict=^VERDICT: PASS\n")
		got, gloss := DecideAssurance(man, func(string) string { return "some/model" })
		if got != AssuranceVerdict || gloss != VerdictGloss {
			t.Fatalf("got %s gloss %q", got, gloss)
		}
	})
	t.Run("none", func(t *testing.T) {
		man := loadManifest(t, "turn writer prompts/w.md model=writer\n")
		got, gloss := DecideAssurance(man, func(string) string { return "" })
		if got != AssuranceNone || gloss != "" {
			t.Fatalf("got %s gloss %q", got, gloss)
		}
	})
}

func loadManifest(t *testing.T, body string) *manifest.Manifest {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	man, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return man
}

func pageSection(page, name string) string {
	marker := "## " + name + "\n"
	i := strings.Index(page, marker)
	if i < 0 {
		return ""
	}
	rest := strings.TrimPrefix(page[i+len(marker):], "\n")
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimRight(rest, "\n")
}
