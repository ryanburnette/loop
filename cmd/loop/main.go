// Command loop runs agentic loops with pi.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ryanburnette/loop/internal/config"
	"github.com/ryanburnette/loop/internal/freeze"
	"github.com/ryanburnette/loop/internal/loopdir"
	"github.com/ryanburnette/loop/internal/run"
	"github.com/ryanburnette/loop/internal/scaffold"
)

const version = "0.3.0"

func main() {
	os.Exit(mainErr(os.Args[1:], os.Stdout, os.Stderr))
}

func mainErr(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
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
	case "init":
		return cmdInit(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "loop: unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// resolveLoopDir computes the loop directory from -C (or cwd/.loop by default)
// without existence checks. Used by `init`, whose -C names the directory to
// create.
func resolveLoopDir(cFlag string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return loopdir.Resolve(cwd, cFlag)
}

// resolveExistingLoopDir resolves the loop directory for commands that operate
// on an existing loop (run/status/freeze). -C may name either the loop dir
// itself (.../proj/.loop) or the project directory that contains it
// (.../proj); in the latter case <dir>/.loop is used when it is a loop dir.
// The default (no -C) already resolves to cwd/.loop, so it is left alone.
// There is no upward search.
func resolveExistingLoopDir(cFlag string) (string, error) {
	dir, err := resolveLoopDir(cFlag)
	if err != nil {
		return "", err
	}
	if cFlag != "" && loopdir.Missing(dir) {
		if dotLoop := filepath.Join(dir, loopdir.DefaultDir); !loopdir.Missing(dotLoop) {
			return dotLoop, nil
		}
	}
	return dir, nil
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		cFlag   = fs.String("C", "", "project or loop directory (default: ./.loop)")
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

	positionals, err := parseInterSpersed(fs, args)
	if err != nil {
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "loop: unexpected arguments: %s\n", strings.Join(positionals, " "))
		return 2
	}

	dir, err := resolveExistingLoopDir(*cFlag)
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}

	// One-shot: --prompt/--gate build a scratch loop dir in a temp directory
	// so the user's workroot is never dirtied by the recipe or the run state.
	// The workroot is the git repo containing the -C target (or the cwd); -C
	// is honored as the project to run against, never silently dropped. The
	// scratch dir is removed when the process exits. One-shot wins over an
	// existing .loop/ because the user named explicit files; the flags are
	// never silently dropped.
	var oneShotWorkroot string
	if *prompt != "" || *gate != "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "loop: %v\n", err)
			return 2
		}
		projectDir := cwd
		if *cFlag != "" {
			if filepath.IsAbs(*cFlag) {
				projectDir = *cFlag
			} else {
				projectDir = filepath.Join(cwd, *cFlag)
			}
		}
		workroot, err := gitTop(projectDir)
		if err != nil {
			fmt.Fprintf(stderr, "loop: workroot: %v\n", err)
			return 2
		}
		scratch, err := os.MkdirTemp("", "loop-oneshot-")
		if err != nil {
			fmt.Fprintf(stderr, "loop: %v\n", err)
			return 2
		}
		// Keep the scratch dir: the run's state (gate-log.md, turn-*.md,
		// handoff.md, status) lives under scratch/state/<id> and the summary
		// prints that absolute path so the user can inspect a one-shot run
		// afterward. Removing it (as a prior fix did) left the printed
		// state/<id> as a dangling reference. The recipe files are tiny and
		// live in the OS temp dir, so retention is cheap.
		if err := writeOneShot(scratch, *prompt, *gate); err != nil {
			fmt.Fprintf(stderr, "loop: %v\n", err)
			return 2
		}
		dir = scratch
		oneShotWorkroot = workroot
	} else if loopdir.Missing(dir) {
		fmt.Fprintln(stderr, loopdir.MissingMessage(dir))
		return 2
	}

	opts := run.Options{
		Dir:      dir,
		Workroot: oneShotWorkroot,
		OneShot:  oneShotWorkroot != "",
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
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cFlag := fs.String("C", "", "project or loop directory (default: ./.loop)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintf(stderr, "loop: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	dir, err := resolveExistingLoopDir(*cFlag)
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	if loopdir.Missing(dir) {
		fmt.Fprintln(stderr, loopdir.MissingMessage(dir))
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
	fs := flag.NewFlagSet("freeze", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cFlag := fs.String("C", "", "project or loop directory (default: ./.loop)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintf(stderr, "loop: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	dir, err := resolveExistingLoopDir(*cFlag)
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	if loopdir.Missing(dir) {
		fmt.Fprintln(stderr, loopdir.MissingMessage(dir))
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
		fmt.Fprintln(stdout, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

func cmdInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cFlag := fs.String("C", "", "loop directory to create (default: ./.loop)")
	rest, err := parseInterSpersed(fs, args)
	if err != nil {
		return 2
	}
	tmpl := ""
	if len(rest) > 1 {
		fmt.Fprintf(stderr, "loop: unexpected arguments: %s\n", strings.Join(rest[1:], " "))
		return 2
	}
	if len(rest) == 1 {
		tmpl = rest[0]
	}
	dir, err := resolveLoopDir(*cFlag)
	if err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	// -C DIR may name the loop dir itself (e.g. .../proj/.loop) or the
	// project dir that contains it (e.g. .../proj). In the second case
	// scaffold DIR/.loop; only when DIR is already the loop dir path do we
	// scaffold DIR directly. The discriminator is the .loop basename
	// convention: a path ending in .loop is the loop dir, anything else is a
	// project dir.
	if filepath.Base(dir) != loopdir.DefaultDir {
		dir = filepath.Join(dir, loopdir.DefaultDir)
	}
	if err := scaffold.Scaffold(dir, tmpl); err != nil {
		fmt.Fprintf(stderr, "loop: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "scaffolded %s in %s\n", scaffold.DefaultOr(tmpl), dir)
	fmt.Fprintln(stdout, "edit the prompt files and loop.env, then run: loop run")
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
  loop run [flags]              run the .loop/ in the current directory
  loop run -C DIR [flags]       run a specific loop directory
  loop run --prompt F --gate C  one-shot, no .loop/ needed
  loop status [-C DIR]
  loop freeze [-C DIR]
  loop frozen?                  check a freeze snapshot (env-driven)
  loop init [template] [-C DIR] scaffold .loop/
  loop help
  loop version

loop operates on .loop/ in the current directory. Use -C DIR to target a
different project from elsewhere: DIR may be the loop dir itself (e.g.
.../proj/.loop) or the project directory that contains .loop/ (e.g. .../proj);
in the second case DIR/.loop is used. There is no upward search for .loop/.

Templates: until-green (default), double-check, two-model-critique, until-count

Flags (run):
  -C DIR               project or loop directory (default ./.loop)
  --max-iter N         override LOOP_MAX_ITER
  --session MODE       none|shared|fork (default none)
  --branch             create loop/<id> branch
  --base BRANCH        LOOP_BRANCH_BASE
  --approve            pass --approve (default true)
  --context TEXT       extra context
  --model role=id      repeatable
  --compact MODE       fail|warn|allow
  --pi PATH            pi binary
  --resume ID          resume a run
  --prompt FILE        one-shot prompt file (no dir needed)
  --gate CMD|PATH      one-shot gate command or script
  -v                   verbose
  -q                   quiet
  --json               machine events
  -V, version          print version
`, version)
}

// parseInterSpersed runs fs.Parse repeatedly, peeling off one positional
// argument each pass so flags may appear before and after one another. This
// makes `loop run -q -C dir` and `loop run -C dir -q` both work, and makes
// unknown flags error instead of being silently dropped.
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
