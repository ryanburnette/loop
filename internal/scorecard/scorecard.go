// Package scorecard parses an operator card and scores the judge's JSON.
// It does not run pi or look at git. A model mistake is an Outcome, not a Go error.
package scorecard

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Card is an operator-authored scorecard.
type Card struct {
	Rule  string // "all" or "threshold"
	Need  int
	Items []Item
}

// Item is one row. Scale 0 means binary; otherwise 2, 3, or 5.
type Item struct {
	ID       string
	Required bool
	Scale    int
	Text     string
}

// Mark is one scored row. Max is 1 for a binary item.
type Mark struct {
	ID      string
	Value   int
	Max     int
	Because string
}

// Outcome is the rule result. Readable is false when the filled file cannot
// be scored. Error is a model mistake, not an operator mistake.
// Render is the one line the mend prints. It is empty when the file is unreadable.
type Outcome struct {
	Readable bool
	Passed   bool
	Met      int
	Need     int
	Marks    []Mark
	Error    string
	Render   string
}

// itemID is the operator's row id. One letter, then up to 31 more of the same set.
var itemID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)

const maxBody = 2048

// ParseCard reads a .card file. An error here is an operator mistake.
func ParseCard(path string) (Card, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Card{}, err
	}
	return parseCard(string(b))
}

type itemBuild struct {
	item  Item
	lines []string
}

func parseCard(text string) (Card, error) {
	if !utf8.ValidString(text) {
		return Card{}, fmt.Errorf("scorecard: not utf-8")
	}
	var (
		card     Card
		cur      *itemBuild
		seenRule bool
		ids      = map[string]bool{}
	)
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	fail := func(format string, args ...any) (Card, error) {
		return Card{}, fmt.Errorf("scorecard:%d: "+format, append([]any{lineNo}, args...)...)
	}
	finish := func() error {
		if cur == nil {
			return nil
		}
		body := strings.Join(cur.lines, "\n")
		if strings.TrimSpace(body) == "" {
			return fmt.Errorf("scorecard: item %s: empty body", cur.item.ID)
		}
		if len(body) > maxBody {
			return fmt.Errorf("scorecard: item %s: body exceeds 2KB", cur.item.ID)
		}
		cur.item.Text = body
		card.Items = append(card.Items, cur.item)
		cur = nil
		return nil
	}
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")
		trim := strings.TrimSpace(line)
		if cur != nil {
			if trim == "" {
				if err := finish(); err != nil {
					return Card{}, err
				}
			} else if !strings.HasPrefix(trim, "#") {
				cur.lines = append(cur.lines, line)
			}
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		fields := strings.Fields(trim)
		switch fields[0] {
		case "rule":
			if seenRule {
				return fail("duplicate rule")
			}
			seenRule = true
			rule, need, err := parseRule(fields)
			if err != nil {
				return fail("%s", err.Error())
			}
			card.Rule = rule
			card.Need = need
		case "item":
			if len(card.Items) >= 24 {
				return fail("at most 24 items")
			}
			it, err := parseItemHead(fields)
			if err != nil {
				return fail("%s", err.Error())
			}
			if ids[it.ID] {
				return fail("duplicate id %s", it.ID)
			}
			ids[it.ID] = true
			cur = &itemBuild{item: it}
		default:
			return fail("unexpected line %q", trim)
		}
	}
	if err := sc.Err(); err != nil {
		return Card{}, err
	}
	if err := finish(); err != nil {
		return Card{}, err
	}
	if len(card.Items) == 0 {
		return Card{}, fmt.Errorf("scorecard: at least one item")
	}
	if card.Rule == "" {
		card.Rule = "all"
	}
	sum, required := 0, 0
	for _, it := range card.Items {
		sum += itemMax(it)
		if it.Required {
			required++
		}
	}
	if card.Rule == "all" {
		if required == 0 {
			return Card{}, fmt.Errorf("scorecard: rule all needs a required item")
		}
		card.Need = required
	}
	if card.Rule == "threshold" && card.Need > sum {
		return Card{}, fmt.Errorf("scorecard: threshold %d exceeds maximum %d", card.Need, sum)
	}
	return card, nil
}

func parseRule(fields []string) (string, int, error) {
	if len(fields) == 2 && fields[1] == "all" {
		return "all", 0, nil
	}
	if len(fields) == 3 && fields[1] == "threshold" {
		n, err := strconv.Atoi(fields[2])
		if err != nil || n < 1 || strconv.Itoa(n) != fields[2] {
			return "", 0, fmt.Errorf("threshold needs a positive integer")
		}
		return "threshold", n, nil
	}
	return "", 0, fmt.Errorf("rule must be \"all\" or \"threshold N\"")
}

func parseItemHead(fields []string) (Item, error) {
	if len(fields) < 2 {
		return Item{}, fmt.Errorf("item missing id")
	}
	id := fields[1]
	if !itemID.MatchString(id) {
		return Item{}, fmt.Errorf("bad item id %q", id)
	}
	it := Item{ID: id, Required: true}
	for i := 2; i < len(fields); i++ {
		switch fields[i] {
		case "optional":
			if !it.Required {
				return Item{}, fmt.Errorf("duplicate optional")
			}
			it.Required = false
		case "scale":
			if it.Scale != 0 {
				return Item{}, fmt.Errorf("duplicate scale")
			}
			i++
			if i >= len(fields) {
				return Item{}, fmt.Errorf("scale missing max")
			}
			n, err := strconv.Atoi(fields[i])
			if err != nil || (n != 2 && n != 3 && n != 5) {
				return Item{}, fmt.Errorf("scale must be 2, 3, or 5")
			}
			it.Scale = n
		default:
			return Item{}, fmt.Errorf("bad item token %q", fields[i])
		}
	}
	return it, nil
}

func itemMax(it Item) int {
	if it.Scale == 0 {
		return 1
	}
	return it.Scale
}

// Score applies the card's rule to the judge's JSON. Unreadable input returns
// Readable false and Passed false. It does not return a Go error.
func Score(card Card, filledJSON []byte) Outcome {
	fail := func(msg string) Outcome {
		return Outcome{Passed: false, Error: msg}
	}
	if len(bytes.TrimSpace(filledJSON)) == 0 {
		return fail("empty")
	}
	dec := json.NewDecoder(bytes.NewReader(filledJSON))
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil {
		return fail("not valid JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fail("trailing junk")
	}
	if _, ok := top["passed"]; ok {
		return fail("passed field")
	}
	for k := range top {
		if k != "items" {
			return fail("unknown field")
		}
	}
	rawItems, ok := top["items"]
	if !ok {
		return fail("missing items")
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(rawItems, &items); err != nil {
		return fail("not valid JSON")
	}

	byID := make(map[string]Item, len(card.Items))
	for _, it := range card.Items {
		byID[it.ID] = it
	}
	filled := make(map[string]map[string]json.RawMessage, len(items))
	for _, obj := range items {
		for k := range obj {
			switch k {
			case "id", "mark", "because":
			default:
				return fail("unknown field")
			}
		}
		rawID, ok := obj["id"]
		if !ok {
			return fail("missing item")
		}
		var id string
		if err := json.Unmarshal(rawID, &id); err != nil || id == "" {
			return fail("not valid JSON")
		}
		if _, dup := filled[id]; dup {
			return fail("duplicate id")
		}
		filled[id] = obj
	}
	if len(items) != len(card.Items) {
		if len(items) > len(card.Items) {
			return fail("extra item")
		}
		return fail("missing item")
	}
	for id := range filled {
		if _, ok := byID[id]; !ok {
			return fail("extra item")
		}
	}
	for _, it := range card.Items {
		if _, ok := filled[it.ID]; !ok {
			return fail("missing item")
		}
	}

	marks := make([]Mark, 0, len(card.Items))
	sum, met, need := 0, 0, 0
	for _, it := range card.Items {
		obj := filled[it.ID]
		rawMark, ok := obj["mark"]
		if !ok {
			return fail("bad mark")
		}
		val, err := parseMark(rawMark, it.Scale)
		if err != nil {
			return fail("bad mark")
		}
		because, err := parseBecause(obj)
		if err != nil {
			return fail(err.Error())
		}
		max := itemMax(it)
		marks = append(marks, Mark{ID: it.ID, Value: val, Max: max, Because: because})
		sum += val
		if it.Required {
			need++
			if val == max {
				met++
			}
		}
	}

	out := Outcome{Readable: true, Marks: marks}
	switch card.Rule {
	case "threshold":
		out.Met = sum
		out.Need = card.Need
		out.Passed = sum >= card.Need
	default:
		out.Met = met
		out.Need = need
		out.Passed = need > 0 && met == need
	}
	out.Render = renderLine(card.Rule, out.Met, out.Need)
	return out
}

func renderLine(rule string, met, need int) string {
	if rule == "threshold" {
		return fmt.Sprintf("sum %d / need %d", met, need)
	}
	return fmt.Sprintf("rule all, %d/%d required met", met, need)
}

func parseMark(raw json.RawMessage, scale int) (int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return 0, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return 0, fmt.Errorf("trailing junk")
	}
	switch n := v.(type) {
	case string:
		max := scale
		if max == 0 {
			max = 1
		}
		switch n {
		case "met":
			return max, nil
		case "unmet":
			return 0, nil
		default:
			return 0, fmt.Errorf("bad mark")
		}
	case json.Number:
		// Binary rows accept only the strings met and unmet.
		if scale == 0 {
			return 0, fmt.Errorf("bad mark")
		}
		i, err := n.Int64()
		if err != nil || i < 0 || int(i) > scale {
			return 0, fmt.Errorf("bad mark")
		}
		return int(i), nil
	default:
		return 0, fmt.Errorf("bad mark")
	}
}

func parseBecause(obj map[string]json.RawMessage) (string, error) {
	raw, ok := obj["because"]
	if !ok {
		return "", fmt.Errorf("because missing")
	}
	var because string
	if err := json.Unmarshal(raw, &because); err != nil {
		return "", fmt.Errorf("because")
	}
	if because == "" {
		return "", fmt.Errorf("because empty")
	}
	if strings.ContainsAny(because, "\r\n") {
		return "", fmt.Errorf("because multiline")
	}
	if utf8.RuneCountInString(because) > 200 {
		return "", fmt.Errorf("because too long")
	}
	return because, nil
}
