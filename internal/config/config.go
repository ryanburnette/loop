// Package config loads runner defaults, loop.env, and overlays.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// Session modes.
type SessionMode string

const (
	SessionNone   SessionMode = "none"
	SessionShared SessionMode = "shared"
	SessionFork   SessionMode = "fork"
)

// Compact modes.
type CompactMode string

const (
	CompactFail  CompactMode = "fail"
	CompactWarn  CompactMode = "warn"
	CompactAllow CompactMode = "allow"
)

// Config is the fully resolved runner configuration.
type Config struct {
	MaxIter        int
	Session        SessionMode
	SessionTurns   int
	ForkPercent    int
	Compact        CompactMode
	Branch         bool
	BranchBase     string
	Approve        bool
	Freeze         []string
	Context        string
	NoContextFiles bool
	Models         map[string]string
	TestCmd        string
	PiPath         string
	Extra          map[string]string
	// Unknown lists LOOP_* keys found in loop.env that no case recognized.
	// The loader warns on these so a typo like LOOP_MAX_ITERATIONS does not
	// get silently ignored (and silently fall back to the default cap).
	Unknown []string
	// Overridden lists keys set in loop.env to one value and in the process
	// environment to a different one, where no flag settled the question.
	// Env winning over the file is the intended layering, but it is otherwise
	// invisible: an ambient LOOP_MAX_ITER quietly replaces the recipe's cap.
	Overridden []string
}

// Overlay holds optional flag/env overrides. Nil pointer = unset.
type Overlay struct {
	MaxIter        *int
	Session        *SessionMode
	SessionTurns   *int
	ForkPercent    *int
	Compact        *CompactMode
	Branch         *bool
	BranchBase     *string
	Approve        *bool
	Freeze         *[]string
	Context        *string
	NoContextFiles *bool
	Models         map[string]string
	TestCmd        *string
	PiPath         *string
}

// Defaults returns the built-in defaults.
func Defaults() Config {
	return Config{
		MaxIter:        5,
		Session:        SessionNone,
		SessionTurns:   4,
		ForkPercent:    40,
		Compact:        CompactWarn,
		Branch:         false,
		BranchBase:     "main",
		Approve:        true,
		Freeze:         nil,
		Context:        "",
		NoContextFiles: false,
		Models:         map[string]string{},
		TestCmd:        "go test ./...",
		PiPath:         "pi",
		Extra:          map[string]string{},
	}
}

// Load reads loop.env from dir (if present), applies Overlay, and returns Config.
// Precedence is defaults, then loop.env, then process env, then the overlay.
func Load(dir string, o Overlay) (Config, error) {
	c := Defaults()
	var fileKV map[string]string
	envPath := filepath.Join(dir, "loop.env")
	if _, err := os.Stat(envPath); err == nil {
		kv, err := parseEnvFile(envPath)
		if err != nil {
			return Config{}, err
		}
		fileKV = kv
		if err := applyMap(&c, kv, &c.Unknown); err != nil {
			return Config{}, err
		}
	} else if !os.IsNotExist(err) {
		return Config{}, err
	}
	envKV := loopEnviron()
	c.Overridden = overriddenKeys(fileKV, envKV, o)
	applyOverlay(&c, o)
	// Process env can also set LOOP_PI etc.; overlay already covers flags.
	// Honor process env for keys not set via overlay when present.
	_ = applyMap(&c, envKV, nil)
	// Overlay wins over process env.
	applyOverlay(&c, o)
	return c, nil
}

// loopEnviron returns the LOOP_* keys of the process environment.
func loopEnviron() map[string]string {
	kv := map[string]string{}
	for _, e := range os.Environ() {
		k, v, ok := strings.Cut(e, "=")
		if !ok || !strings.HasPrefix(k, "LOOP_") {
			continue
		}
		kv[k] = v
	}
	return kv
}

// overriddenKeys returns the keys loop.env and the process environment
// disagree about and no flag decided. Equal values are not a conflict, and a
// key the overlay sets is settled by the flag, so neither is reported.
func overriddenKeys(fileKV, envKV map[string]string, o Overlay) []string {
	if len(fileKV) == 0 || len(envKV) == 0 {
		return nil
	}
	byFlag := overlayKeys(o)
	var out []string
	for k, fileVal := range fileKV {
		envVal, ok := envKV[k]
		if !ok || envVal == fileVal || byFlag[k] {
			continue
		}
		out = append(out, k)
	}
	return out
}

// overlayKeys is the set of loop.env keys the overlay (flags) settles.
func overlayKeys(o Overlay) map[string]bool {
	keys := map[string]bool{}
	set := func(k string, on bool) {
		if on {
			keys[k] = true
		}
	}
	set("LOOP_MAX_ITER", o.MaxIter != nil)
	set("LOOP_SESSION", o.Session != nil)
	set("LOOP_SESSION_TURNS", o.SessionTurns != nil)
	set("LOOP_FORK_PERCENT", o.ForkPercent != nil)
	set("LOOP_COMPACT", o.Compact != nil)
	set("LOOP_BRANCH", o.Branch != nil)
	set("LOOP_BRANCH_BASE", o.BranchBase != nil)
	set("LOOP_APPROVE", o.Approve != nil)
	set("LOOP_FREEZE", o.Freeze != nil)
	set("LOOP_CONTEXT", o.Context != nil)
	set("LOOP_NO_CONTEXT_FILES", o.NoContextFiles != nil)
	set("LOOP_TEST_CMD", o.TestCmd != nil)
	set("LOOP_PI", o.PiPath != nil)
	for role := range o.Models {
		keys["LOOP_"+strings.ToUpper(role)+"_MODEL"] = true
	}
	return keys
}

func applyMap(c *Config, kv map[string]string, unknown *[]string) error {
	for k, v := range kv {
		switch k {
		case "LOOP_MAX_ITER":
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			c.MaxIter = n
		case "LOOP_SESSION":
			c.Session = SessionMode(v)
		case "LOOP_SESSION_TURNS":
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			c.SessionTurns = n
		case "LOOP_FORK_PERCENT":
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			c.ForkPercent = n
		case "LOOP_COMPACT":
			c.Compact = CompactMode(v)
		case "LOOP_BRANCH":
			c.Branch = v == "1" || strings.EqualFold(v, "true")
		case "LOOP_BRANCH_BASE":
			c.BranchBase = v
		case "LOOP_APPROVE":
			c.Approve = v == "1" || strings.EqualFold(v, "true")
		case "LOOP_FREEZE":
			c.Freeze = strings.Fields(v)
		case "LOOP_CONTEXT":
			c.Context = v
		case "LOOP_NO_CONTEXT_FILES":
			c.NoContextFiles = v == "1" || strings.EqualFold(v, "true")
		case "LOOP_TEST_CMD":
			c.TestCmd = v
			if c.Extra == nil {
				c.Extra = map[string]string{}
			}
			c.Extra[k] = v
		case "LOOP_PI":
			c.PiPath = v
		default:
			if role, ok := strings.CutPrefix(k, "LOOP_"); ok {
				if m, ok := strings.CutSuffix(role, "_MODEL"); ok && m != "" {
					if c.Models == nil {
						c.Models = map[string]string{}
					}
					c.Models[strings.ToLower(m)] = v
					continue
				}
			}
			if strings.HasPrefix(k, "LOOP_") {
				if c.Extra == nil {
					c.Extra = map[string]string{}
				}
				c.Extra[k] = v
				if unknown != nil {
					*unknown = append(*unknown, k)
				}
			}
		}
	}
	return nil
}

func applyOverlay(c *Config, o Overlay) {
	if o.MaxIter != nil {
		c.MaxIter = *o.MaxIter
	}
	if o.Session != nil {
		c.Session = *o.Session
	}
	if o.SessionTurns != nil {
		c.SessionTurns = *o.SessionTurns
	}
	if o.ForkPercent != nil {
		c.ForkPercent = *o.ForkPercent
	}
	if o.Compact != nil {
		c.Compact = *o.Compact
	}
	if o.Branch != nil {
		c.Branch = *o.Branch
	}
	if o.BranchBase != nil {
		c.BranchBase = *o.BranchBase
	}
	if o.Approve != nil {
		c.Approve = *o.Approve
	}
	if o.Freeze != nil {
		c.Freeze = append([]string{}, (*o.Freeze)...)
	}
	if o.Context != nil {
		c.Context = *o.Context
	}
	if o.NoContextFiles != nil {
		c.NoContextFiles = *o.NoContextFiles
	}
	if o.TestCmd != nil {
		c.TestCmd = *o.TestCmd
	}
	if o.PiPath != nil {
		c.PiPath = *o.PiPath
	}
	for k, v := range o.Models {
		if c.Models == nil {
			c.Models = map[string]string{}
		}
		c.Models[k] = v
	}
}

// Environ returns KEY=VALUE pairs for exporting to gates/hooks.
func (c Config) Environ() []string {
	out := []string{
		fmt.Sprintf("LOOP_MAX_ITER=%d", c.MaxIter),
		fmt.Sprintf("LOOP_SESSION=%s", c.Session),
		fmt.Sprintf("LOOP_SESSION_TURNS=%d", c.SessionTurns),
		fmt.Sprintf("LOOP_FORK_PERCENT=%d", c.ForkPercent),
		fmt.Sprintf("LOOP_COMPACT=%s", c.Compact),
		fmt.Sprintf("LOOP_BRANCH=%s", bool01(c.Branch)),
		fmt.Sprintf("LOOP_BRANCH_BASE=%s", c.BranchBase),
		fmt.Sprintf("LOOP_APPROVE=%s", bool01(c.Approve)),
		fmt.Sprintf("LOOP_CONTEXT=%s", c.Context),
		fmt.Sprintf("LOOP_NO_CONTEXT_FILES=%s", bool01(c.NoContextFiles)),
		fmt.Sprintf("LOOP_TEST_CMD=%s", c.TestCmd),
		fmt.Sprintf("LOOP_PI=%s", c.PiPath),
	}
	if len(c.Freeze) > 0 {
		out = append(out, "LOOP_FREEZE="+strings.Join(c.Freeze, " "))
	}
	for role, model := range c.Models {
		key := "LOOP_" + strings.ToUpper(role) + "_MODEL"
		out = append(out, key+"="+model)
	}
	for k, v := range c.Extra {
		// Avoid duplicating keys already emitted.
		if strings.HasPrefix(k, "LOOP_") && !containsKey(out, k) {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func containsKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func bool01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, err := parseEnvLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		out[key] = val
	}
	return out, sc.Err()
}

func parseEnvLine(line string) (string, string, error) {
	key, raw, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", fmt.Errorf("expected KEY=VALUE")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("empty key")
	}
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "$(") || strings.Contains(raw, "`") {
		return "", "", fmt.Errorf("command substitution not allowed in loop.env")
	}
	val, err := unquote(raw)
	if err != nil {
		return "", "", err
	}
	// Reject substitution forms even inside quotes.
	if strings.Contains(val, "$(") || strings.Contains(val, "`") {
		return "", "", fmt.Errorf("command substitution not allowed in loop.env")
	}
	return key, val, nil
}

func unquote(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	switch s[0] {
	case '"', '\'':
		q := s[0]
		if len(s) < 2 || s[len(s)-1] != q {
			return "", fmt.Errorf("unbalanced quotes")
		}
		inner := s[1 : len(s)-1]
		if q == '"' {
			// Minimal escape handling for \" and \\.
			var b strings.Builder
			for i := 0; i < len(inner); i++ {
				if inner[i] == '\\' && i+1 < len(inner) {
					b.WriteByte(inner[i+1])
					i++
					continue
				}
				b.WriteByte(inner[i])
			}
			return b.String(), nil
		}
		return inner, nil
	default:
		// Strip trailing inline comment if space-#.
		if i := strings.Index(s, " #"); i >= 0 {
			s = strings.TrimRightFunc(s[:i], unicode.IsSpace)
		}
		return s, nil
	}
}
