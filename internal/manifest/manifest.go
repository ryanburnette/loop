// Package manifest parses loop manifest files.
package manifest

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Step types.
const (
	Turn = "turn"
	Gate = "gate"
	Hook = "hook"
)

// Step is one manifest line.
type Step struct {
	Type     string
	Name     string
	Path     string
	Model    string
	Verdict  string
	System   string
	Required bool
}

// Manifest is an ordered list of steps.
type Manifest struct {
	Steps []Step
	// Warnings lists non-fatal parse complaints (an unrecognized key=value
	// token, say). A typo like `requried=0` is not a syntax error — the line
	// still parses — but it silently changes what the loop does, so the
	// runner surfaces these rather than dropping them.
	Warnings []string
}

// ParseFile reads and parses a manifest file.
func ParseFile(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &Manifest{}
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		step, warns, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("manifest:%d: %w", lineNo, err)
		}
		for _, w := range warns {
			m.Warnings = append(m.Warnings, fmt.Sprintf("manifest:%d: %s", lineNo, w))
		}
		m.Steps = append(m.Steps, step)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// parseLine parses one manifest line into a Step. It also returns non-fatal
// warnings for tokens that parsed but probably did not mean what was written.
func parseLine(line string) (Step, []string, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return Step{}, nil, fmt.Errorf("short line: need type name path")
	}
	typ := fields[0]
	switch typ {
	case Turn, Gate, Hook:
	default:
		return Step{}, nil, fmt.Errorf("unknown step type %q", typ)
	}

	s := Step{
		Type:     typ,
		Name:     fields[1],
		Path:     fields[2],
		Required: true,
	}

	// Re-scan keys from the raw line so verdict=/system= can consume the rest.
	rest := line
	// Skip type, name, path tokens (whitespace-separated).
	for i := 0; i < 3; i++ {
		rest = strings.TrimLeft(rest, " \t")
		if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
			rest = rest[sp:]
		} else {
			rest = ""
		}
	}
	rest = strings.TrimLeft(rest, " \t")

	var warns []string
	for rest != "" {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			break
		}
		switch {
		case strings.HasPrefix(rest, "verdict="):
			s.Verdict = strings.TrimPrefix(rest, "verdict=")
			return s, warns, nil
		case strings.HasPrefix(rest, "system="):
			s.System = strings.TrimPrefix(rest, "system=")
			return s, warns, nil
		default:
			// key=value token, delimited by any run of whitespace. Cutting on
			// a literal " " would swallow a tab-separated line whole, so
			// `model=x<TAB>verdict=…` would silently set Model to
			// "x\tverdict=…" and drop the verdict.
			tok, next := cutField(rest)
			rest = next
			key, val, ok := strings.Cut(tok, "=")
			if !ok {
				return Step{}, warns, fmt.Errorf("bad key token %q", tok)
			}
			switch key {
			case "model":
				s.Model = val
			case "required":
				s.Required = val != "0"
			default:
				warns = append(warns, fmt.Sprintf("unknown key %q (ignored)", key))
			}
		}
	}
	return s, warns, nil
}

// cutField splits off the first whitespace-delimited token of s and returns it
// with the remainder.
func cutField(s string) (tok, rest string) {
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

// Load reads dir/manifest if present, else derives one by convention from
// dir/prompts, dir/gates, dir/hooks.
func Load(dir string) (*Manifest, error) {
	manPath := filepath.Join(dir, "manifest")
	if _, err := os.Stat(manPath); err == nil {
		return ParseFile(manPath)
	}
	return Derive(dir)
}

// Derive builds a manifest by convention from dir/prompts, dir/gates,
// dir/hooks. Prompts/*.md become required turn steps (sorted lexically by
// filename), gates/* become required gate steps, hooks/* become hook steps.
// A step's Name strips the file extension and a leading NN- numeric prefix.
// Returns an error if there is nothing to run.
func Derive(dir string) (*Manifest, error) {
	m := &Manifest{}

	for _, name := range deriveList(dir, "prompts", true) {
		// A derived turn step resolves its model from LOOP_<ROLE>_MODEL via
		// its Name (the role), matching DESIGN-v0.3.md. Set Model to the
		// role name so resolveModel can look it up; an explicit manifest
		// step keeps its own model= semantics (empty unless set).
		role := deriveName(name)
		m.Steps = append(m.Steps, Step{
			Type:     Turn,
			Name:     role,
			Path:     filepath.Join("prompts", name),
			Model:    role,
			Required: true,
		})
	}
	for _, name := range deriveList(dir, "gates", false) {
		m.Steps = append(m.Steps, Step{
			Type:     Gate,
			Name:     deriveName(name),
			Path:     filepath.Join("gates", name),
			Required: true,
		})
	}
	for _, name := range deriveList(dir, "hooks", false) {
		m.Steps = append(m.Steps, Step{
			Type:     Hook,
			Name:     deriveName(name),
			Path:     filepath.Join("hooks", name),
			Required: true,
		})
	}

	if len(m.Steps) == 0 {
		return nil, fmt.Errorf("no manifest and no prompts/gates/hooks files in %s: nothing to run", dir)
	}
	return m, nil
}

// deriveList returns the regular files in dir/sub, sorted lexically by
// filename. When mdOnly is true, only *.md files are returned. A missing
// sub directory yields nil.
func deriveList(dir, sub string, mdOnly bool) []string {
	entries, err := os.ReadDir(filepath.Join(dir, sub))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if mdOnly && !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// deriveName strips the file extension and a leading NN- numeric prefix:
// 01-writer.md → writer, writer.md → writer, tests.sh → tests.
func deriveName(filename string) string {
	name := filename
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "-"); i > 0 {
		if _, err := strconv.Atoi(name[:i]); err == nil {
			name = name[i+1:]
		}
	}
	return name
}

// HasObjective reports whether the loop has a required gate or required verdict.
func (m *Manifest) HasObjective() bool {
	if m == nil {
		return false
	}
	for _, s := range m.Steps {
		if !s.Required {
			continue
		}
		switch s.Type {
		case Gate:
			return true
		case Turn:
			if s.Verdict != "" {
				return true
			}
		}
	}
	return false
}
