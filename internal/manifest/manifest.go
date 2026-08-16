// Package manifest parses loop manifest files.
package manifest

import (
	"bufio"
	"fmt"
	"os"
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
		step, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("manifest:%d: %w", lineNo, err)
		}
		m.Steps = append(m.Steps, step)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

func parseLine(line string) (Step, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return Step{}, fmt.Errorf("short line: need type name path")
	}
	typ := fields[0]
	switch typ {
	case Turn, Gate, Hook:
	default:
		return Step{}, fmt.Errorf("unknown step type %q", typ)
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

	for rest != "" {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			break
		}
		switch {
		case strings.HasPrefix(rest, "verdict="):
			s.Verdict = strings.TrimPrefix(rest, "verdict=")
			return s, nil
		case strings.HasPrefix(rest, "system="):
			s.System = strings.TrimPrefix(rest, "system=")
			return s, nil
		default:
			// key=value token
			tok, next, _ := strings.Cut(rest, " ")
			rest = next
			key, val, ok := strings.Cut(tok, "=")
			if !ok {
				return Step{}, fmt.Errorf("bad key token %q", tok)
			}
			switch key {
			case "model":
				s.Model = val
			case "required":
				s.Required = val != "0"
			default:
				// ignore unknown keys (v1 warns; we stay quiet for parse)
			}
		}
	}
	return s, nil
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
