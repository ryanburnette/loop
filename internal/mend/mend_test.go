package mend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitRejectsNewline(t *testing.T) {
	var ledger Ledger
	raw := []byte(`{"proposals":[` +
		`{"tried":"cache the client\nin init","failed":"TestReload timed out","do_not_retry":false},` +
		`{"tried":"keep the valid one","failed":"still red","do_not_retry":true}` +
		`]}`)
	Commit(&ledger, "writer", 3, raw)
	if len(ledger.Settled) != 1 || ledger.Settled[0].Tried != "keep the valid one" {
		t.Fatalf("valid entry should commit, got %+v", ledger.Settled)
	}
	if len(ledger.Rejected) != 1 || !strings.Contains(ledger.Rejected[0], "newline") {
		t.Fatalf("newline entry rejected as %+v", ledger.Rejected)
	}
	if strings.Contains(ledger.Rejected[0], "cache the client") {
		t.Fatalf("rejected line should not keep the broken sentence: %s", ledger.Rejected[0])
	}
}

func TestCommitDropsOldestNonDoNotRetry(t *testing.T) {
	var ledger Ledger
	for i := 0; i < 24; i++ {
		dnr := i != 1
		raw := fmt.Sprintf(`{"proposals":[{"tried":"t%d","failed":"f%d","do_not_retry":%t}]}`, i, i, dnr)
		Commit(&ledger, "writer", 1, []byte(raw))
	}
	if len(ledger.Settled) != 24 || ledger.Dropped != 0 {
		t.Fatalf("before cap %+v dropped %d", len(ledger.Settled), ledger.Dropped)
	}
	Commit(&ledger, "writer", 2, []byte(`{"proposals":[{"tried":"new","failed":"later","do_not_retry":false}]}`))
	if len(ledger.Settled) != 24 {
		t.Fatalf("len %d", len(ledger.Settled))
	}
	if ledger.Dropped != 1 {
		t.Fatalf("dropped %d", ledger.Dropped)
	}
	for _, s := range ledger.Settled {
		if s.Tried == "t1" {
			t.Fatal("oldest non-do_not_retry entry was kept")
		}
	}
	if !hasDoNotRetry(ledger, "t0") {
		t.Fatal("oldest do-not-retry entry should stay")
	}
	if !hasTried(ledger, "new") {
		t.Fatal("25th entry missing")
	}
}

func TestCommitDropsOldestWhenAllDoNotRetry(t *testing.T) {
	var ledger Ledger
	for i := 0; i < 24; i++ {
		raw := fmt.Sprintf(`{"proposals":[{"tried":"t%d","failed":"f%d","do_not_retry":true}]}`, i, i)
		Commit(&ledger, "writer", 1, []byte(raw))
	}
	Commit(&ledger, "writer", 2, []byte(`{"proposals":[{"tried":"new","failed":"later","do_not_retry":true}]}`))
	if hasTried(ledger, "t0") {
		t.Fatal("oldest do-not-retry should drop when every line is do-not-retry")
	}
	if !hasTried(ledger, "new") || ledger.Dropped != 1 {
		t.Fatalf("ledger %+v dropped %d", ledger.Settled, ledger.Dropped)
	}
}

func TestCommitDedupKeepsEarlierIteration(t *testing.T) {
	var ledger Ledger
	Commit(&ledger, "writer", 2, []byte(`{"proposals":[{"tried":"cache the client in init","failed":"first","do_not_retry":false}]}`))
	Commit(&ledger, "fixer", 5, []byte(`{"proposals":[{"tried":"cache the client in init","failed":"TestReload timed out","do_not_retry":true}]}`))
	if len(ledger.Settled) != 1 {
		t.Fatalf("dedup should not grow the ledger: %+v", ledger.Settled)
	}
	got := ledger.Settled[0]
	if got.Iter != 2 || got.Step != "writer" || got.Failed != "TestReload timed out" || !got.DoNotRetry {
		t.Fatalf("dedup %+v", got)
	}
}

func TestCommitTooMany(t *testing.T) {
	var props []string
	for i := 0; i < 10; i++ {
		props = append(props, fmt.Sprintf(`{"tried":"t%d","failed":"f%d","do_not_retry":false}`, i, i))
	}
	raw := []byte(`{"proposals":[` + strings.Join(props, ",") + `]}`)
	var ledger Ledger
	Commit(&ledger, "writer", 1, raw)
	if len(ledger.Settled) != 8 {
		t.Fatalf("kept %d, want 8", len(ledger.Settled))
	}
	n := 0
	for _, r := range ledger.Rejected {
		if strings.Contains(r, "too many") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("too many rejections %d: %+v", n, ledger.Rejected)
	}
}

func TestFactsOutrankSettled(t *testing.T) {
	facts := Facts{
		RunID:       "run1",
		Iter:        3,
		MaxIter:     8,
		Branch:      "loop/run1",
		Head:        "abc1234",
		Freeze:      "ok",
		Session:     "none",
		Context:     "n/a",
		Goal:        "fix the csv loader",
		Constraints: "- do not edit tests\n",
		Checks: []Check{
			{
				Kind:     "gate",
				Name:     "tests",
				OK:       false,
				Exit:     1,
				Required: true,
				Excerpt:  "/tmp/state/excerpts/3-tests.log",
			},
			{
				Kind:     "scorecard",
				Name:     "reviewer",
				Required: false,
				Render:   "rule all, 1/2 required met",
				Marks: []Mark{
					{ID: "auth", Label: "met", Because: "middleware still calls CheckSession"},
					{ID: "tests", Label: "unmet", Because: "TestLogin was deleted"},
				},
				Card: "/tmp/state/cards/3-reviewer.json",
			},
		},
		Diff: Diff{Committed: "internal/loader.go | 12 ++++----"},
	}
	for i := 0; i < 20; i++ {
		facts.Checks[0].Tail = append(facts.Checks[0].Tail, fmt.Sprintf("line-%d", i))
	}
	// A blob that must not be pasted back. It is not a Fact field.
	facts.Checks[0].Tail = append([]string{strings.Repeat("X", 12_000)}, facts.Checks[0].Tail...)
	ledger := Ledger{
		Settled: []Settled{{
			Iter: 2, Step: "writer", Tried: "cache the client in init",
			Failed: "TestReload timed out", DoNotRetry: true,
		}},
		Rejected: []string{"iter 3, writer: proposal unreadable."},
	}
	path := filepath.Join(t.TempDir(), "mend.md")
	if err := WriteMend(path, facts, ledger); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	factsPart := s
	if i := strings.Index(s, "## Settled"); i >= 0 {
		factsPart = s[:i]
	}
	for _, want := range []string{
		"source: runner",
		"fix the csv loader",
		"do not edit tests",
		"gate tests: FAIL exit 1 (required)",
		"rule all, 1/2 required met",
		"auth met — middleware still calls CheckSession",
		"tests unmet — TestLogin was deleted",
		"internal/loader.go",
	} {
		if !strings.Contains(factsPart, want) {
			t.Fatalf("facts missing %q\n%s", want, factsPart)
		}
	}
	if strings.Count(s, "\n  tail: ") != 8 {
		t.Fatalf("tail lines %d, want 8\n%s", strings.Count(s, "\n  tail: "), s)
	}
	if strings.Contains(s, strings.Repeat("X", 100)) {
		t.Fatal("gate log was inlined")
	}
	if strings.Contains(factsPart, "TestReload timed out") {
		t.Fatal("settled failure was written into Facts")
	}
	settled := s[strings.Index(s, "## Settled"):]
	if !strings.Contains(settled, "claimed by writer at iter 2") || !strings.Contains(settled, "do-not-retry") {
		t.Fatalf("settled label missing\n%s", settled)
	}
	if !strings.Contains(settled, "proposal unreadable.") {
		t.Fatalf("rejection missing\n%s", settled)
	}
}

func TestMissingStartRendersBaseUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mend.md")
	err := WriteMend(path, Facts{
		Iter: 1, MaxIter: 1, Goal: "g", Context: "n/a", Freeze: "not configured", Session: "none",
		Diff: Diff{CommittedUnknown: true, Unstaged: "file.go | 1 +"},
	}, Ledger{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "committed: base unknown") {
		t.Fatalf("missing START should say the base is unknown:\n%s", s)
	}
	if !strings.Contains(s, "file.go") {
		t.Fatal("unstaged diff should still render")
	}
	committed := section(s, "#### Committed")
	if strings.Contains(committed, "file.go") {
		t.Fatalf("unstaged diff must not stand in for committed:\n%s", committed)
	}
}

func TestTruncatesBodyBeforeFacts(t *testing.T) {
	facts := Facts{
		Iter: 1, MaxIter: 1, Goal: "keep-goal", Context: "n/a",
		GoalBody:    strings.Repeat("B", 100_000),
		Constraints: strings.Repeat("C", 100_000),
		Checks: []Check{{
			Kind: "gate", Name: "tests", Exit: 7, Required: true, OK: false,
			Tail: []string{"real tail"},
		}},
	}
	ledger := Ledger{Settled: []Settled{{
		Iter: 1, Step: "writer", Tried: "one", Failed: "two", DoNotRetry: true,
	}}}
	path := filepath.Join(t.TempDir(), "mend.md")
	if err := WriteMend(path, facts, ledger); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxMendBytes {
		t.Fatalf("mend is %d bytes", len(b))
	}
	s := string(b)
	if !strings.Contains(s, "keep-goal") || !strings.Contains(s, "exit 7") {
		t.Fatalf("goal or exit code truncated, %d bytes", len(s))
	}
	if !strings.Contains(s, "do-not-retry") {
		t.Fatal("do-not-retry line dropped ahead of the body")
	}
	if !strings.Contains(s, "… truncated …") {
		t.Fatal("expected a truncation marker")
	}
	if strings.Contains(s, strings.Repeat("B", 5000)) {
		t.Fatal("body was not truncated")
	}
}

func TestFitDropsWholeLinesNotFacts(t *testing.T) {
	facts := Facts{
		Iter: 1, MaxIter: 1, Goal: "KEEP-GOAL", GoalBody: "short body",
		Context: "n/a",
		Checks: []Check{{
			Kind: "gate", Name: "tests", Exit: 7, Required: true,
		}},
	}
	ledger := Ledger{Settled: []Settled{{
		Iter: 1, Step: "writer", Tried: "KEEP-DNR", Failed: "still", DoNotRetry: true,
	}}}
	for i := 0; i < 40; i++ {
		ledger.Settled = append(ledger.Settled, Settled{
			Iter: 1, Step: "writer",
			Tried:      fmt.Sprintf("retry-%d", i),
			Failed:     strings.Repeat("x", 500),
			DoNotRetry: false,
		})
	}
	s := writeMendString(t, facts, ledger)
	if !strings.Contains(s, "KEEP-GOAL") || !strings.Contains(s, "short body") {
		t.Fatal("goal text was clipped when the body was not the overflow")
	}
	if strings.Contains(s, "… truncated …") {
		t.Fatal("truncation marker stamped on text that was not clipped")
	}
	if !strings.Contains(s, "gate tests: FAIL exit 7 (required)") {
		t.Fatal("exit line missing or cut")
	}
	if !strings.Contains(s, "tried KEEP-DNR. failed: still.") {
		t.Fatal("do-not-retry line missing or cut")
	}
	if strings.Contains(s, "retry-0") {
		t.Fatal("oldest retryable line was kept")
	}
	if !strings.Contains(s, "settled dropped:") {
		t.Fatalf("render drops not counted:\n%s", s)
	}
}

func TestFitLeavesOversizedFactsIntact(t *testing.T) {
	blob := strings.Repeat("Z", 20_000)
	facts := Facts{
		Iter: 1, MaxIter: 1, Goal: "KEEP-GOAL", Context: "n/a",
		Checks: []Check{{
			Kind: "gate", Name: "tests", Exit: 7, Required: true,
		}},
	}
	ledger := Ledger{Settled: []Settled{{
		Iter: 1, Step: "writer", Tried: "KEEP-DNR", Failed: blob, DoNotRetry: true,
	}}}
	s := writeMendString(t, facts, ledger)
	if len(s) <= maxMendBytes {
		t.Fatalf("page %d bytes, want it left over budget", len(s))
	}
	if !strings.Contains(s, "exit 7") || !strings.Contains(s, blob) || !strings.Contains(s, "KEEP-DNR") {
		t.Fatal("a Fact was sliced to fit")
	}
}

func writeMendString(t *testing.T, facts Facts, ledger Ledger) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mend.md")
	if err := WriteMend(path, facts, ledger); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProposalRoundTrip(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"proposals": []Proposal{{
			Tried:      "cache the client in init",
			Failed:     "TestReload timed out",
			DoNotRetry: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ledger Ledger
	Commit(&ledger, "writer", 2, raw)
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	if err := SaveLedger(path, ledger); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Settled) != 1 || got.Settled[0].Tried != "cache the client in init" || !got.Settled[0].DoNotRetry {
		t.Fatalf("%+v", got)
	}
}

func hasTried(l Ledger, tried string) bool {
	for _, s := range l.Settled {
		if s.Tried == tried {
			return true
		}
	}
	return false
}

func hasDoNotRetry(l Ledger, tried string) bool {
	for _, s := range l.Settled {
		if s.Tried == tried {
			return s.DoNotRetry
		}
	}
	return false
}

func section(s, heading string) string {
	i := strings.Index(s, heading)
	if i < 0 {
		return ""
	}
	rest := s[i+len(heading):]
	if j := strings.Index(rest, "\n#### "); j >= 0 {
		return rest[:j]
	}
	return rest
}
