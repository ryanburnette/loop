package scorecard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCard(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "review.card")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustCard(t *testing.T, body string) Card {
	t.Helper()
	c, err := ParseCard(writeCard(t, body))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRenderStrings(t *testing.T) {
	// Four required rows, two at their maximum. The optional row is met and
	// must not move either count.
	all := mustCard(t, ""+
		"rule all\n\n"+
		"item a\nbody\n\n"+
		"item b\nbody\n\n"+
		"item c\nbody\n\n"+
		"item d\nbody\n\n"+
		"item extra optional\nbody\n")
	allJSON := []byte(`{"items":[` +
		`{"id":"a","mark":"met","because":"holds"},` +
		`{"id":"b","mark":"met","because":"holds"},` +
		`{"id":"c","mark":"unmet","because":"missing"},` +
		`{"id":"d","mark":"unmet","because":"missing"},` +
		`{"id":"extra","mark":"met","because":"bonus"}` +
		`]}`)
	// Scale row worth 2 plus one binary met is 3. Need stays the threshold.
	threshold := mustCard(t, ""+
		"rule threshold 5\n\n"+
		"item depth scale 5\nbody\n\n"+
		"item tests\nbody\n")
	thresholdJSON := []byte(`{"items":[` +
		`{"id":"depth","mark":2,"because":"partial"},` +
		`{"id":"tests","mark":"met","because":"present"}` +
		`]}`)

	cases := []struct {
		name string
		card Card
		raw  []byte
		want string
	}{
		{"rule all", all, allJSON, "rule all, 2/4 required met"},
		{"rule threshold", threshold, thresholdJSON, "sum 3 / need 5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(tc.card, tc.raw)
			if !got.Readable {
				t.Fatalf("unreadable: %s", got.Error)
			}
			if got.Render != tc.want {
				t.Fatalf("render %q want %q", got.Render, tc.want)
			}
		})
	}
}

func TestRuleAll(t *testing.T) {
	card := mustCard(t, ""+
		"rule all\n\n"+
		"item auth\nThe check holds.\n\n"+
		"item depth scale 2\n0 none.\n1 some.\n2 all.\n\n"+
		"item style optional\nMatch the neighbors.\n")
	pass := Score(card, []byte(`{"items":[`+
		`{"id":"auth","mark":"met","because":"middleware still checks"},`+
		`{"id":"depth","mark":2,"because":"both cases covered"},`+
		`{"id":"style","mark":"unmet","because":"naming drifted"}`+
		`]}`))
	if !pass.Readable || !pass.Passed {
		t.Fatalf("optional miss must not fail rule all: %+v", pass)
	}
	if pass.Render != "rule all, 2/2 required met" {
		t.Fatalf("render %q", pass.Render)
	}
	if pass.Met != 2 || pass.Need != 2 {
		t.Fatalf("met/need %d/%d", pass.Met, pass.Need)
	}

	short := Score(card, []byte(`{"items":[`+
		`{"id":"auth","mark":"met","because":"middleware still checks"},`+
		`{"id":"depth","mark":1,"because":"timeout skipped"},`+
		`{"id":"style","mark":"met","because":"matches"}`+
		`]}`))
	if !short.Readable || short.Passed {
		t.Fatalf("scale below max must fail rule all: %+v", short)
	}
	if short.Render != "rule all, 1/2 required met" {
		t.Fatalf("render %q", short.Render)
	}

	if _, err := ParseCard(writeCard(t, "rule all\n\nitem style optional\nNice.\n")); err == nil {
		t.Fatal("rule all with no required item must fail to parse")
	}
}

func TestRuleThreshold(t *testing.T) {
	card := mustCard(t, ""+
		"rule threshold 2\n\n"+
		"item auth\nThe check holds.\n\n"+
		"item style optional scale 2\n0 no.\n1 some.\n2 yes.\n")
	// The required row is unmet. The optional row is the whole sum.
	got := Score(card, []byte(`{"items":[`+
		`{"id":"auth","mark":"unmet","because":"session check removed"},`+
		`{"id":"style","mark":"met","because":"matches neighbors"}`+
		`]}`))
	if !got.Readable || !got.Passed {
		t.Fatalf("threshold ignores the required bit and counts optional points: %+v", got)
	}
	if got.Render != "sum 2 / need 2" {
		t.Fatalf("render %q", got.Render)
	}
	if strings.Contains(got.Render, "required met") {
		t.Fatalf("threshold render must not say required met: %q", got.Render)
	}

	low := Score(card, []byte(`{"items":[`+
		`{"id":"auth","mark":"unmet","because":"session check removed"},`+
		`{"id":"style","mark":1,"because":"partial"}`+
		`]}`))
	if !low.Readable || low.Passed {
		t.Fatalf("sum below need must fail: %+v", low)
	}
	if low.Met != 1 || low.Need != 2 || low.Render != "sum 1 / need 2" {
		t.Fatalf("outcome %+v", low)
	}

	if _, err := ParseCard(writeCard(t, "rule threshold 3\n\nitem a\nbody\n\nitem b\nbody\n")); err == nil {
		t.Fatal("threshold above the maximum sum must fail to parse")
	}
}

func TestExtraItem(t *testing.T) {
	card := mustCard(t, "item auth\nbody\n")
	got := Score(card, []byte(`{"items":[`+
		`{"id":"auth","mark":"met","because":"holds"},`+
		`{"id":"extra","mark":"met","because":"invented"}`+
		`]}`))
	if got.Readable || got.Passed || !strings.Contains(got.Error, "extra item") {
		t.Fatalf("outcome %+v", got)
	}
}

func TestMissingItem(t *testing.T) {
	card := mustCard(t, "item auth\nbody\n\nitem tests\nbody\n")
	got := Score(card, []byte(`{"items":[{"id":"auth","mark":"met","because":"holds"}]}`))
	if got.Readable || got.Passed || !strings.Contains(got.Error, "missing item") {
		t.Fatalf("outcome %+v", got)
	}
}

func TestBadMark(t *testing.T) {
	card := mustCard(t, "item auth\nbody\n\nitem depth scale 2\n0 none.\n1 some.\n2 all.\n")
	cases := []struct {
		name string
		raw  string
	}{
		{"bool", `{"items":[{"id":"auth","mark":true,"because":"holds"},{"id":"depth","mark":2,"because":"all"}]}`},
		{"binary integer", `{"items":[{"id":"auth","mark":1,"because":"holds"},{"id":"depth","mark":2,"because":"all"}]}`},
		{"float", `{"items":[{"id":"auth","mark":"met","because":"holds"},{"id":"depth","mark":1.5,"because":"partial"}]}`},
		{"negative", `{"items":[{"id":"auth","mark":"met","because":"holds"},{"id":"depth","mark":-1,"because":"no"}]}`},
		{"word", `{"items":[{"id":"auth","mark":"yes","because":"holds"},{"id":"depth","mark":2,"because":"all"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(card, []byte(tc.raw))
			if got.Readable || got.Passed || !strings.Contains(got.Error, "bad mark") {
				t.Fatalf("outcome %+v", got)
			}
		})
	}
}

func TestScaleRange(t *testing.T) {
	for _, scale := range []string{"0", "1", "4", "6"} {
		body := "item depth scale " + scale + "\nbody\n"
		if _, err := ParseCard(writeCard(t, body)); err == nil {
			t.Fatalf("scale %s must fail to parse", scale)
		}
	}
	for _, scale := range []string{"2", "3", "5"} {
		body := "item depth scale " + scale + "\nbody\n"
		if _, err := ParseCard(writeCard(t, body)); err != nil {
			t.Fatalf("scale %s: %v", scale, err)
		}
	}
	card := mustCard(t, "item depth scale 5\nbody\n")
	over := Score(card, []byte(`{"items":[{"id":"depth","mark":6,"because":"too high"}]}`))
	if over.Readable || !strings.Contains(over.Error, "bad mark") {
		t.Fatalf("mark above scale: %+v", over)
	}
	top := Score(card, []byte(`{"items":[{"id":"depth","mark":"met","because":"full"}]}`))
	if !top.Readable || !top.Passed || top.Marks[0].Value != 5 {
		t.Fatalf("met on a scale means the max: %+v", top)
	}
}

func TestParseExampleAndRejects(t *testing.T) {
	ex := "" +
		"# scorecards/review.card\n" +
		"# rule all is the default: every required item must be met.\n" +
		"rule all\n" +
		"\n" +
		"item auth\n" +
		"The change does not weaken authentication or session checks.\n" +
		"\n" +
		"item tests\n" +
		"Existing tests still describe the behavior named in TODO.md.\n" +
		"The diff must not delete a test to go green.\n" +
		"\n" +
		"item style optional\n" +
		"The diff matches the surrounding style. Optional: recorded, not required\n" +
		"under rule all.\n" +
		"\n" +
		"item depth scale 2\n" +
		"0 the edge cases in TODO.md are ignored.\n" +
		"1 some are covered.\n" +
		"2 the ones named in TODO.md are covered.\n"
	card := mustCard(t, ex)
	if card.Rule != "all" || len(card.Items) != 4 {
		t.Fatalf("card %+v", card)
	}
	if !card.Items[0].Required || card.Items[0].Scale != 0 || card.Items[0].ID != "auth" {
		t.Fatalf("auth %+v", card.Items[0])
	}
	if card.Items[2].Required || card.Items[2].ID != "style" {
		t.Fatalf("style %+v", card.Items[2])
	}
	if card.Items[3].Scale != 2 || !card.Items[3].Required {
		t.Fatalf("depth %+v", card.Items[3])
	}

	var many strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&many, "item r%d\nbody\n\n", i)
	}
	rejects := []string{
		"item 1bad\nbody\n",
		"item auth\n\n",
		"item auth\nbody\n\nitem auth\nbody\n",
		"item auth extra\nbody\n",
		"rule threshold 0\n\nitem a\nbody\n",
		"rule all\n\nrule threshold 1\n\nitem a\nbody\n",
		many.String(),
	}
	for _, body := range rejects {
		if _, err := ParseCard(writeCard(t, body)); err == nil {
			t.Fatalf("expected parse error for %q", body)
		}
	}
	var big strings.Builder
	big.WriteString("item a\n")
	big.WriteString(strings.Repeat("x", maxBody+1))
	big.WriteString("\n")
	if _, err := ParseCard(writeCard(t, big.String())); err == nil {
		t.Fatal("body over 2KB must fail")
	}
}

func TestUnreadableFile(t *testing.T) {
	card := mustCard(t, "item auth\nbody\n")
	long := strings.Repeat("é", 201)
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", "empty"},
		{"junk", "not json", "not valid JSON"},
		{"trailing", `{"items":[{"id":"auth","mark":"met","because":"holds"}]} trailing`, "trailing junk"},
		{"passed", `{"passed":true,"items":[{"id":"auth","mark":"met","because":"holds"}]}`, "passed field"},
		{"extra key", `{"items":[{"id":"auth","mark":"met","because":"holds","note":"x"}]}`, "unknown field"},
		{"because newline", "{\"items\":[{\"id\":\"auth\",\"mark\":\"met\",\"because\":\"a\\nb\"}]}", "because multiline"},
		{"because long", `{"items":[{"id":"auth","mark":"met","because":"` + long + `"}]}`, "because too long"},
		{"duplicate", `{"items":[{"id":"auth","mark":"met","because":"one"},{"id":"auth","mark":"unmet","because":"two"}]}`, "duplicate id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(card, []byte(tc.raw))
			if got.Readable || got.Passed || got.Render != "" {
				t.Fatalf("outcome %+v", got)
			}
			if !strings.Contains(got.Error, tc.want) {
				t.Fatalf("error %q want %q", got.Error, tc.want)
			}
		})
	}
}
