// Package pi builds argv, runs pi, and parses --mode json events.
package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Request is one pi invocation.
type Request struct {
	PiPath         string
	Model          string
	SessionID      string
	SessionDir     string
	ForkID         string
	Approve        bool
	System         string
	NoContextFiles bool
	PromptFile     string
	Handoff        string
	Context        string
	WorkRoot       string
	StdoutFile     string // extracted assistant text
	JSONLFile      string // raw events
	StderrFile     string
	// OnEvent, if non-nil, is called for each parsed jsonl line as it
	// arrives, enabling live tool/context streaming during a turn.
	OnEvent func(Event)
	// Ctx, if non-nil, is used to start pi; cancelling it kills pi.
	Ctx context.Context
}

// Event is one parsed pi --mode json line, delivered to OnEvent in stream order.
type Event struct {
	Type           string
	ToolName       string
	ContextPercent int
	TextDelta      string
	Compacted      bool
	Raw            map[string]any
}

// Result is the parsed outcome of a turn.
type Result struct {
	Text           string
	ContextPercent int
	LastTool       string
	Compacted      bool
	ExitCode       int
}

// Argv builds the pi command line (including the binary as args[0]).
func Argv(req Request) []string {
	pi := req.PiPath
	if pi == "" {
		pi = "pi"
	}
	args := []string{pi, "-p", "--mode", "json"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--session-id", req.SessionID)
		if req.SessionDir != "" {
			args = append(args, "--session-dir", req.SessionDir)
		}
		if req.ForkID != "" {
			args = append(args, "--fork", req.ForkID)
		}
	} else {
		args = append(args, "--no-session")
	}
	if req.Approve {
		args = append(args, "--approve")
	}
	if req.System != "" {
		args = append(args, "--append-system-prompt", req.System)
	}
	if req.NoContextFiles {
		args = append(args, "--no-context-files")
	}
	if req.PromptFile != "" {
		args = append(args, "@"+req.PromptFile)
	}
	if req.Handoff != "" {
		args = append(args, "@"+req.Handoff)
	}
	if req.Context != "" {
		args = append(args, req.Context)
	}
	return args
}

// Run executes pi and returns the parsed result. Writes text/jsonl/err files when set.
func Run(req Request) (Result, error) {
	args := Argv(req)
	ctx := req.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	// Kill the whole process group on cancel so pi's child processes do not
	// outlive it and keep pipes open.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return os.ErrProcessDone
	}
	cmd.WaitDelay = 5 * time.Second
	if req.WorkRoot != "" {
		cmd.Dir = req.WorkRoot
	}
	cmd.Stdin = nil // closed stdin; Go passes /dev/null equivalent
	// Explicitly attach /dev/null for clarity on Unix.
	devNull, err := os.Open(os.DevNull)
	if err == nil {
		cmd.Stdin = devNull
		defer devNull.Close()
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderrFile *os.File
	if req.StderrFile != "" {
		stderrFile, err = os.Create(req.StderrFile)
		if err != nil {
			return Result{}, err
		}
		defer stderrFile.Close()
		cmd.Stderr = stderrFile
	} else {
		cmd.Stderr = io.Discard
	}

	var jsonlFile *os.File
	if req.JSONLFile != "" {
		jsonlFile, err = os.Create(req.JSONLFile)
		if err != nil {
			return Result{}, err
		}
		defer jsonlFile.Close()
	}

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start pi: %w", err)
	}

	var reader io.Reader = stdout
	if jsonlFile != nil {
		reader = io.TeeReader(stdout, jsonlFile)
	}
	res, parseErr := parseStream(reader, req.OnEvent)
	waitErr := cmd.Wait()
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			return res, waitErr
		}
	}
	if parseErr != nil {
		return res, parseErr
	}

	if req.StdoutFile != "" {
		if err := os.WriteFile(req.StdoutFile, []byte(res.Text), 0o644); err != nil {
			return res, err
		}
	}

	// Empty text + non-empty stderr is an error.
	if strings.TrimSpace(res.Text) == "" && req.StderrFile != "" {
		if st, err := os.Stat(req.StderrFile); err == nil && st.Size() > 0 {
			return res, fmt.Errorf("pi produced no text (see %s)", req.StderrFile)
		}
	}
	return res, nil
}

// ParseJSONL reads a pi --mode json event stream.
func ParseJSONL(r io.Reader) (Result, error) {
	return parseStream(r, nil)
}

func parseStream(r io.Reader, onEvent func(Event)) (Result, error) {
	var res Result
	var textParts []string
	sc := bufio.NewScanner(r)
	// Allow long lines (tool output).
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Skip malformed lines rather than failing the turn.
			continue
		}
		typ, _ := ev["type"].(string)
		evt := Event{Type: typ, Raw: ev}
		switch typ {
		case "compaction_start":
			res.Compacted = true
			evt.Compacted = true
		case "tool_execution_start":
			if name, ok := ev["toolName"].(string); ok && name != "" {
				res.LastTool = name
				evt.ToolName = name
			}
		case "message_update":
			// Streamed text deltas.
			if ame, ok := ev["assistantMessageEvent"].(map[string]any); ok {
				if ame["type"] == "text_delta" {
					if d, ok := ame["delta"].(string); ok {
						textParts = append(textParts, d)
						evt.TextDelta = d
					}
				}
			}
		case "message_end", "turn_end":
			if msg, ok := ev["message"].(map[string]any); ok {
				if role, _ := msg["role"].(string); role == "assistant" {
					if t := extractText(msg); t != "" {
						// Prefer full message text over accumulated deltas.
						textParts = []string{t}
					}
				}
			}
		case "session_status":
			if cu, ok := ev["contextUsage"].(map[string]any); ok {
				res.ContextPercent = asInt(cu["percent"])
				evt.ContextPercent = res.ContextPercent
			}
		}
		if onEvent != nil {
			onEvent(evt)
		}
	}
	if err := sc.Err(); err != nil {
		return res, err
	}
	res.Text = strings.Join(textParts, "")
	return res, nil
}

func extractText(msg map[string]any) string {
	content, ok := msg["content"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, c := range content {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == "text" {
			if t, ok := m["text"].(string); ok {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "")
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		var i int
		fmt.Sscanf(n, "%d", &i)
		return i
	default:
		return 0
	}
}
