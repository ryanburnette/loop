// Package control reads and truncates the run control file.
package control

import (
	"bufio"
	"os"
	"strings"
)

// Kind is a control command kind.
type Kind int

const (
	Unknown Kind = iota
	Pause
	Resume
	Stop
	Set
)

// Command is one control directive.
type Command struct {
	Kind  Kind
	Key   string
	Value string
	Raw   string
}

// Consume reads path, parses commands, and truncates the file.
// A missing file is not an error; it returns an empty slice.
func Consume(path string) ([]Command, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// Truncate immediately so a crash after apply does not re-run.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return nil, err
	}

	var cmds []Command
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cmds = append(cmds, parseLine(line))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return cmds, nil
}

func parseLine(line string) Command {
	switch {
	case line == "pause":
		return Command{Kind: Pause, Raw: line}
	case line == "resume":
		return Command{Kind: Resume, Raw: line}
	case line == "stop":
		return Command{Kind: Stop, Raw: line}
	case strings.HasPrefix(line, "set "):
		rest := strings.TrimSpace(strings.TrimPrefix(line, "set "))
		key, val, ok := strings.Cut(rest, "=")
		if !ok {
			return Command{Kind: Unknown, Raw: line}
		}
		return Command{Kind: Set, Key: strings.TrimSpace(key), Value: strings.TrimSpace(val), Raw: line}
	default:
		return Command{Kind: Unknown, Raw: line}
	}
}
