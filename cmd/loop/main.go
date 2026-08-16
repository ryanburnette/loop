// Command loop runs agentic loops with pi.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ryanburnette/loop/internal/config"
	"github.com/ryanburnette/loop/internal/freeze"
	"github.com/ryanburnette/loop/internal/run"
)

const version = "0.2.0"

func main() {
	os.Exit(mainErr(os.Args[1:], os.Stdout, os.Stderr))
}

func mainErr(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 2
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version", "-V", "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "run":
		return cmdRun(args[1:], stdout, stderr)
	case "status":
		return cmdStatus(args[1:], stdout, stderr)
	case "freeze":
		return cmdFreeze(args[1:], stdout, stderr)
	case "frozen?":
		return cmdFrozen(stdout, stderr)
	default:
		// v1 compatible: loop <dir> [flags]
		if strings.HasPrefix(args[0], "-") {
			fmt.Fprintf(stderr, "loop: unknown flag %s (pass a dir first, or use run)\n", args[0])
			return 2
		}
		return cmdRun(args, stdout, stderr)
	}
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		maxIter = fs.Int("max-iter", 0, "override LOOP_MAX_ITER")
		session = fs.String("session", "", "none|shared|fork")
		branch  = fs.Bool("branch", false, "create loop/<id> branch")
		base    = fs.String("base", "", "branch base (LOOP_BRANCH_BASE)")
		approve = fs.Bool("approve", true, "pass --approve to pi")
		context = fs.String("context", "", "extra context string")
		compact = fs.String("compact", "", "fail|warn|allow")
		piPath  = fs.String("pi", "", "pi binary (LOOP_PI)")
		resume  = fs.String("resume", "", "resume run id")
		quiet   = fs.Bool("q", false, "quiet")
		verbose = fs.Bool("v", false, "verbose")
		jsonOut = fs.Bool("json", false, "machine events")
		prompt  = fs.String("prompt", "", "one-shot prompt file")
		gate    = fs.String("gate", "", "one-shot gate command/script")
		models  multiFlag
	)
	fs.Var(&models, "model", "role=id (repeatable)")
	_ = approve // applied via env below after Parse detects explicit false

	// The usage line documents `loop run <dir> [flags]`, but flag.FlagSet.Parse
	// stops at the first non-flag argument. Parse in a loop, peeling off one
	// positional each pass, so flags work before and after the dir — and
	// unknown trailing flags actually error instead of being silently dropped.
	positionals, err := parseInterSpersed(fs, args)
	if err != nil {
		return 2
	}
	dir := ""
	if len(positionals) > 0 {
		dir = positionals[0]
	}
	if len(positionals) > 1 {
		fmt.Fprintf(stderr, "loop: unexpected arguments: %s\n", strings.Join(positionals[1:], " "))
		return 2
	}

	// One-shot: build a temp loop dir when --prompt/--gate given without dir.
	if dir == "" && (*prompt != "" || *gate != "") {
		// Keep scratch under ./state/ (gitignored) instead of littering cwd.
		id := time.Now().UTC().Format("20060102T150405Z") + "-" + strconv.Itoa(os.Getpid())
		tmp := filepath.Join(".", "state", "oneshot-"+id)
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			fmt.Fprintf(stderr, "loop: %v\n", err)
			return 2
		}
		dir = tmp
		if err := writeOneShot(dir, *prompt, *gate); err != nil {
			fmt.Fprintf(stderr, "loop: %v\n", err)
			return 2
		}
	}
	if dir == "" {
		fmt.Fprintln(stderr, "loop: run requires a directory (or --prompt/--gate)")
		printUsage(stderr)
		return 2
	}

	opts := run.Options{
		Dir:      dir,
		Pi:       *piPath,
		Quiet:    *quiet,
		Verbose:  *verbose,
		JSON:     *jsonOut,
		Compact:  *compact,
		MaxIter:  *maxIter,
		Session:  *session,
		ResumeID: *resume,
		Out:      stdout,
		Err:      stderr,
	}
	// Apply remaining overrides via env so config.Load sees them.
	if *branch {
		_ = os.Setenv("LOOP_BRANCH", "1")
	}
	if *base != "" {
		_ = os.Setenv("LOOP_BRANCH_BASE", *base)
	}
	if fs.Lookup("approve").Value.String() == "false" {
		_ = os.Setenv("LOOP_APPROVE", "0")
	}
	if *context != "" {
		_ = os.Setenv("LOOP_CONTEXT", *context)
	}
	for _, m := range models {
		role, id, ok := strings.Cut(m, "=")
		if !ok {
			fmt.Fprintf(stderr, "loop: --model wants role=id, got %q\n", m)
			return 2
		}
		_ = os.Setenv("LOOP_"+strings.ToUpper(role)+"_MODEL", id)
	}

	code, err := run.Run(opts)
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		if code == 0 {
			code = 2
		}
	}
	return code
}

func cmdStatus(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "loop status <dir>")
		return 2
	}
	dir, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	idPath := filepath.Join(dir, "state", "CURRENT_ID")
	b, err := os.ReadFile(idPath)
	if err != nil {
		fmt.Fprintf(stderr, "loop: no current run in %s\n", dir)
		return 1
	}
	id := strings.TrimSpace(string(b))
	state := filepath.Join(dir, "state", id)
	meta, _ := os.ReadFile(filepath.Join(state, "meta.env"))
	status, _ := os.ReadFile(filepath.Join(state, "status"))
	iter, _ := os.ReadFile(filepath.Join(state, "iteration"))
	fmt.Fprintf(stdout, "id %s\n", id)
	fmt.Fprintf(stdout, "iteration %s\n", strings.TrimSpace(string(iter)))
	if len(status) > 0 {
		fmt.Fprintf(stdout, "status %s\n", strings.TrimSpace(string(status)))
	}
	if len(meta) > 0 {
		fmt.Fprintln(stdout, "--- meta.env ---")
		fmt.Fprint(stdout, string(meta))
	}
	return 0
}

func cmdFreeze(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "loop freeze <dir>")
		return 2
	}
	dir, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	cfg, err := config.Load(dir, config.Overlay{})
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	if len(cfg.Freeze) == 0 {
		fmt.Fprintln(stdout, "nothing to freeze (LOOP_FREEZE empty)")
		return 0
	}
	workroot, err := gitTop(dir)
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	state := filepath.Join(dir, "state", ".freeze-tmp", "frozen")
	if err := freeze.Snapshot(workroot, state, cfg.Freeze); err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "froze %d pattern(s) → %s\n", len(cfg.Freeze), state)
	fmt.Fprintf(stdout, "check with: LOOP_STATE_DIR=%s LOOP_WORKROOT=%s loop frozen?\n",
		filepath.Dir(state), workroot)
	return 0
}

func cmdFrozen(stdout, stderr io.Writer) int {
	stateDir := os.Getenv("LOOP_STATE_DIR")
	if stateDir == "" {
		fmt.Fprintln(stderr, "loop frozen?: LOOP_STATE_DIR not set")
		return 2
	}
	workroot := os.Getenv("LOOP_WORKROOT")
	if workroot == "" {
		var err error
		workroot, err = gitTop(stateDir)
		if err != nil {
			fmt.Fprintf(stderr, "loop: %v\n", err)
			return 2
		}
	}
	if err := freeze.Check(workroot, filepath.Join(stateDir, "frozen")); err != nil {
		// err.Error() is "freeze drift: <patterns>"; surface it so the
		// operator sees which pattern(s) drifted, matching the gate path.
		fmt.Fprintln(stdout, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

func writeOneShot(dir, prompt, gate string) error {
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "gates"), 0o755); err != nil {
		return err
	}
	var lines []string
	if prompt != "" {
		// Copy or reference prompt.
		dst := filepath.Join(dir, "prompts", "oneshot.md")
		b, err := os.ReadFile(prompt)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		lines = append(lines, "turn writer prompts/oneshot.md")
	}
	if gate != "" {
		dst := filepath.Join(dir, "gates", "check.sh")
		body := "#!/bin/sh\nset -eu\n" + gate + "\n"
		// If gate looks like a path to a script, invoke it.
		if st, err := os.Stat(gate); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(gate)
			body = "#!/bin/sh\nset -eu\nexec \"" + abs + "\"\n"
		}
		if err := os.WriteFile(dst, []byte(body), 0o755); err != nil {
			return err
		}
		lines = append(lines, "gate check gates/check.sh")
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "loop.env"), []byte("LOOP_MAX_ITER=5\nLOOP_SESSION=none\n"), 0o644)
}

func gitTop(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repo: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `loop %s — run agentic loops with pi

Usage:
  loop <dir> [flags]
  loop run <dir> [flags]
  loop run --prompt F --gate C
  loop status <dir>
  loop freeze <dir>
  loop frozen?
  loop help
  loop version

Flags:
  --max-iter N          override LOOP_MAX_ITER
  --session MODE        none|shared|fork (default none)
  --branch              create loop/<id> branch
  --base BRANCH         LOOP_BRANCH_BASE
  --approve             pass --approve (default true)
  --context TEXT        extra context
  --model role=id       repeatable
  --compact MODE        fail|warn|allow
  --pi PATH             pi binary
  --resume ID           resume a run
  --prompt FILE         one-shot prompt file (no dir needed)
  --gate CMD|PATH       one-shot gate command or script
  -v                    verbose
  -q                    quiet
  --json                machine events
  -V, version           print version
`, version)
}

// parseInterSpersed runs fs.Parse repeatedly, peeling off one positional
// argument each pass so flags may appear before and after the dir. This is
// what makes `loop run <dir> -q` and `loop run <dir> --resume <id>` work, and
// it makes unknown flags after the dir error instead of being dropped.
func parseInterSpersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
	return positionals, nil
}

// multiFlag collects repeatable string flags.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
