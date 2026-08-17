// Package run is the iteration loop.
//
// Custom loop.sh mode is deferred; manifest + one-shot flags only in v1.
package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ryanburnette/loop/internal/config"
	"github.com/ryanburnette/loop/internal/control"
	"github.com/ryanburnette/loop/internal/freeze"
	"github.com/ryanburnette/loop/internal/gitinfo"
	"github.com/ryanburnette/loop/internal/manifest"
	"github.com/ryanburnette/loop/internal/pi"
	"github.com/ryanburnette/loop/internal/session"
	"github.com/ryanburnette/loop/internal/ui"
)

// Options is the entry configuration for a run.
type Options struct {
	Dir      string
	Workroot string // override the workroot (used by one-shot, whose dir is in temp)
	Pi       string
	Quiet    bool
	Verbose  bool
	JSON     bool
	Color    *bool
	Compact  string
	MaxIter  int
	Session  string
	ResumeID string
	Out      io.Writer
	Err      io.Writer
}

// Run executes a loop and returns a process exit code.
func Run(opts Options) (int, error) {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.Err
	if errOut == nil {
		errOut = os.Stderr
	}

	loopDir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return 2, err
	}
	if st, err := os.Stat(loopDir); err != nil || !st.IsDir() {
		return 2, fmt.Errorf("loop dir %s: %w", opts.Dir, err)
	}

	var workroot string
	if opts.Workroot != "" {
		workroot = opts.Workroot
	} else {
		w, err := gitWorkroot(loopDir)
		if err != nil {
			return 2, err
		}
		workroot = w
	}

	overlay := config.Overlay{}
	if opts.MaxIter > 0 {
		n := opts.MaxIter
		overlay.MaxIter = &n
	}
	if opts.Session != "" {
		s := config.SessionMode(opts.Session)
		overlay.Session = &s
	}
	if opts.Compact != "" {
		c := config.CompactMode(opts.Compact)
		overlay.Compact = &c
	}
	if opts.Pi != "" {
		p := opts.Pi
		overlay.PiPath = &p
	}

	cfg, err := config.Load(loopDir, overlay)
	if err != nil {
		return 2, err
	}

	man, err := manifest.Load(loopDir)
	if err != nil {
		// Custom loop.sh deferred.
		if _, e2 := os.Stat(filepath.Join(loopDir, "loop.sh")); e2 == nil {
			return 2, fmt.Errorf("custom loop.sh mode is deferred")
		}
		return 2, fmt.Errorf("manifest: %w", err)
	}

	runStart := time.Now()

	// Collect git info for display before any branch setup, so GitBranch
	// reflects the branch we started on, not the loop branch we may create.
	gitInfo := gitinfo.Collect(workroot)

	color := false
	if opts.Color != nil {
		color = *opts.Color
	} else if fileIsTTY(out) && os.Getenv("NO_COLOR") == "" {
		color = true
	}
	r := ui.New(ui.Options{Out: out, Color: color, Quiet: opts.Quiet, Verbose: opts.Verbose, JSON: opts.JSON})

	var (
		id        string
		stateDir  string
		startIter int
	)
	if opts.ResumeID != "" {
		id = opts.ResumeID
		stateDir = filepath.Join(loopDir, "state", id)
		if st, err := os.Stat(stateDir); err != nil || !st.IsDir() {
			return 2, fmt.Errorf("resume state not found: %s", stateDir)
		}
		startIter = readIntFile(filepath.Join(stateDir, "iteration"))
	} else {
		id = newID()
		stateDir = filepath.Join(loopDir, "state", id)
		// Branch setup must run before any state files land in the workroot,
		// otherwise the dirty-tree check sees our own writes.
		if cfg.Branch {
			if err := setupBranch(workroot, cfg.BranchBase, id, loopDir); err != nil {
				return 2, err
			}
		}
		if err := os.MkdirAll(filepath.Join(stateDir, "sessions"), 0o755); err != nil {
			return 2, err
		}
		if err := writeMeta(stateDir, id, cfg, workroot); err != nil {
			return 2, err
		}
		if err := os.WriteFile(filepath.Join(stateDir, "gate-log.md"), []byte("# gate log\n"), 0o644); err != nil {
			return 2, err
		}
		if err := os.WriteFile(filepath.Join(stateDir, "iteration"), []byte("0\n"), 0o644); err != nil {
			return 2, err
		}
		if err := os.MkdirAll(filepath.Join(loopDir, "state"), 0o755); err != nil {
			return 2, err
		}
		_ = os.WriteFile(filepath.Join(loopDir, "state", "CURRENT_ID"), []byte(id+"\n"), 0o644)

		// Freeze only on fresh start, never on resume.
		if err := freeze.Snapshot(workroot, filepath.Join(stateDir, "frozen"), cfg.Freeze); err != nil {
			return 2, err
		}
		startIter = 0
	}

	writeStatus := func(iter, max int, phase string) {
		elapsed := int(time.Since(runStart).Seconds())
		line := fmt.Sprintf("iteration %d/%d · phase: %s · elapsed %ds", iter, max, phase, elapsed)
		_ = os.WriteFile(filepath.Join(stateDir, "status"), []byte(line+"\n"), 0o644)
	}

	branchName := ""
	if cfg.Branch {
		branchName = "loop/" + id
		if b, err := gitCurrentBranch(workroot); err == nil && b != "" {
			branchName = b
		}
	}

	objective := man.HasObjective()

	// SIGINT / SIGTERM is a control stop: finish the current cheap step if
	// we can, write SUCCESS=0, and exit 1. Do not hang in the pause poll.
	stopSig := make(chan os.Signal, 1)
	signal.Notify(stopSig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopSig)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := false
	go func() {
		<-stopSig
		stopped = true
		cancel()
	}()

	r.Header(ui.Header{
		ID:        id,
		Dir:       loopDir,
		WorkRoot:  workroot,
		Branch:    branchName,
		GitRepo:   gitInfo.Repo,
		GitBranch: gitInfo.Branch,
		GitSHA:    gitInfo.ShortSHA,
		GitDirty:  gitInfo.Dirty,
		GitDirtyN: gitInfo.DirtyN,
		Session:   string(cfg.Session),
		MaxIter:   cfg.MaxIter,
		Objective: objective,
	})

	// summary prints the end-of-run footer. result is one of
	// success|fail|stopped|done; iterUsed is the last iteration reached.
	summary := func(result string, iterUsed int) {
		r.Summary(ui.Summary{
			Elapsed:    time.Since(runStart),
			Iterations: iterUsed,
			MaxIter:    cfg.MaxIter,
			Result:     result,
			Branch:     branchName,
		})
	}

	// Session tracking.
	sessPolicy := session.Policy{
		Mode:         cfg.Session,
		SessionTurns: cfg.SessionTurns,
		ForkPercent:  cfg.ForkPercent,
	}
	var (
		sessID           = id
		turnsThisSession int
		lastCtxPercent   int
		lastCompacted    bool
		hasSession       bool
		handoffPath      = filepath.Join(stateDir, "handoff.md")
	)

	gateLogPath := filepath.Join(stateDir, "gate-log.md")
	paused := false

	for iter := startIter + 1; iter <= cfg.MaxIter; iter++ {
		if err := os.WriteFile(filepath.Join(stateDir, "iteration"), []byte(strconv.Itoa(iter)+"\n"), 0o644); err != nil {
			return 2, err
		}
		r.Iteration(iter, cfg.MaxIter)
		writeStatus(iter, cfg.MaxIter, "start")
		iterOK := true
		var (
			lastGateName string
			lastGateOK   bool
			lastGateLog  string
		)

		for _, step := range man.Steps {
			if stopped {
				break
			}
			// Control plane between steps.
			cmds, err := control.Consume(filepath.Join(stateDir, "control"))
			if err != nil {
				return 2, err
			}
			for _, cmd := range cmds {
				switch cmd.Kind {
				case control.Stop:
					appendMeta(stateDir, "SUCCESS=0")
					writeStatus(iter, cfg.MaxIter, "stopped")
					r.Stopped(relState(loopDir, stateDir))
					summary("stopped", iter)
					return 1, nil
				case control.Pause:
					paused = true
				case control.Resume:
					paused = false
				case control.Set:
					applySet(&cfg, cmd.Key, cmd.Value)
					// Keep session policy in sync.
					sessPolicy.Mode = cfg.Session
					sessPolicy.SessionTurns = cfg.SessionTurns
					sessPolicy.ForkPercent = cfg.ForkPercent
				case control.Unknown:
					r.Warn("unknown control: " + cmd.Raw)
				}
			}
			didPause := false
			for paused {
				if !didPause {
					r.Paused()
					didPause = true
				}
				select {
				case <-ctx.Done():
					// Signal stop while paused: do not write a durable
					// "stop" to the control file — that would poison a
					// later resume of this same run.
					appendMeta(stateDir, "SUCCESS=0")
					writeStatus(iter, cfg.MaxIter, "stopped")
					r.Stopped(relState(loopDir, stateDir))
					summary("stopped", iter)
					return 1, nil
				case <-time.After(200 * time.Millisecond):
				}
				cmds, err := control.Consume(filepath.Join(stateDir, "control"))
				if err != nil {
					return 2, err
				}
				for _, cmd := range cmds {
					switch cmd.Kind {
					case control.Resume:
						paused = false
					case control.Stop:
						appendMeta(stateDir, "SUCCESS=0")
						writeStatus(iter, cfg.MaxIter, "stopped")
						r.Stopped(relState(loopDir, stateDir))
						summary("stopped", iter)
						return 1, nil
					case control.Set:
						applySet(&cfg, cmd.Key, cmd.Value)
						sessPolicy.Mode = cfg.Session
						sessPolicy.SessionTurns = cfg.SessionTurns
						sessPolicy.ForkPercent = cfg.ForkPercent
					}
				}
			}
			if didPause {
				r.Resumed()
			}

			env := buildEnv(cfg, id, loopDir, workroot, stateDir, branchName, iter, step.Name)

			switch step.Type {
			case manifest.Turn:
				t0 := time.Now()
				modelID := resolveModel(cfg, step.Model)
				detail := modelID
				if detail == "" {
					detail = "default"
				}
				r.StepStart("turn", step.Name, detail)
				writeStatus(iter, cfg.MaxIter, "turn "+step.Name)

				dec := sessPolicy.Decide(session.State{
					TurnsThisSession: turnsThisSession,
					ContextPercent:   lastCtxPercent,
					Compacted:        lastCompacted,
					HasSession:       hasSession,
				})
				// Consume compaction flag after deciding.
				lastCompacted = false

				req := pi.Request{
					PiPath:         cfg.PiPath,
					Model:          modelID,
					Approve:        cfg.Approve,
					System:         step.System,
					NoContextFiles: cfg.NoContextFiles,
					PromptFile:     resolvePath(loopDir, step.Path),
					Context:        cfg.Context,
					WorkRoot:       workroot,
					Ctx:            ctx,
				}
				// Attach handoff on every iteration after the first.
				if iter > 1 {
					if _, err := os.Stat(handoffPath); err == nil {
						req.Handoff = handoffPath
					}
				}
				switch {
				case !dec.UseSession:
					// none — leave SessionID empty → --no-session
				case dec.Action == session.New:
					sessID = fmt.Sprintf("%s-%d-%s", id, iter, step.Name)
					turnsThisSession = 0
					hasSession = true
					req.SessionID = sessID
					req.SessionDir = filepath.Join(stateDir, "sessions")
				case dec.Action == session.Fork:
					prev := sessID
					sessID = fmt.Sprintf("%s-%d-%s", id, iter, step.Name)
					turnsThisSession = 0
					hasSession = true
					req.SessionID = sessID
					req.SessionDir = filepath.Join(stateDir, "sessions")
					req.ForkID = prev
				default: // Continue
					req.SessionID = sessID
					req.SessionDir = filepath.Join(stateDir, "sessions")
				}

				turnBase := filepath.Join(stateDir, fmt.Sprintf("turn-%d-%s", iter, step.Name))
				req.StdoutFile = turnBase + ".md"
				req.JSONLFile = turnBase + ".jsonl"
				req.StderrFile = turnBase + ".err"

				// Live tool line: stream tool events as they arrive instead of
				// only after the turn ends.
				turnStart := t0
				req.OnEvent = func(ev pi.Event) {
					switch {
					case ev.ToolName != "":
						r.Tool(ev.ToolName, shortToolArg(ev.Raw))
					case ev.ContextPercent > 0 && ev.Type == "session_status":
						r.Context(ev.ContextPercent, int(time.Since(turnStart).Seconds()))
					case ev.TextDelta != "" && opts.Verbose:
						r.Assistant(ev.TextDelta)
					}
				}

				res, err := pi.Run(req)
				elapsed := int(time.Since(t0).Seconds())
				if err != nil {
					r.StepDone(false, "errored", elapsed)
					if step.Required {
						iterOK = false
					}
					break
				}
				if opts.Verbose && res.Text != "" {
					// Full text already streamed via deltas; nothing extra.
				}
				lastCtxPercent = res.ContextPercent
				if dec.UseSession {
					turnsThisSession++
					hasSession = true
				}

				note := "done"
				ok := true
				if res.Compacted {
					lastCompacted = true
					switch cfg.Compact {
					case config.CompactFail:
						ok = false
						note = "compacted"
						if step.Required {
							iterOK = false
						}
					case config.CompactWarn:
						r.Warn("compaction detected; next turn will start a new session")
						// Force new session next time via lastCompacted.
					case config.CompactAllow:
						// do nothing
					}
				}
				r.StepDone(ok, note, elapsed)

				// Verdict check.
				if step.Verdict != "" && ok {
					matched := false
					// Match line-oriented: a verdict like `^VERDICT: PASS`
					// must hit on its own line even when the model writes
					// prose before it. `(?m)` makes ^/$ anchor at line
					// boundaries; it is idempotent if the pattern already
					// carries it. Fall back to a literal contains on a
					// bad pattern.
					if re, err := regexp.Compile("(?m)" + step.Verdict); err == nil {
						matched = re.MatchString(res.Text)
					} else {
						matched = strings.Contains(res.Text, step.Verdict)
					}
					if matched {
						appendLog(gateLogPath, fmt.Sprintf("VERDICT %s: PASS\n", step.Name))
					} else {
						appendLog(gateLogPath, fmt.Sprintf("VERDICT %s: FAIL\n", step.Name))
						if step.Required {
							iterOK = false
						}
					}
				}
				_ = env // exported via gate/hook; turns inherit process+cfg via pi cwd only

			case manifest.Gate:
				t0 := time.Now()
				detail := ""
				if step.Required {
					detail = "required"
				}
				r.StepStart("gate", step.Name, detail)
				writeStatus(iter, cfg.MaxIter, "gate "+step.Name)

				var (
					gateOK  bool
					gateOut string
				)
				if step.Path == "loop:frozen" {
					err := freeze.Check(workroot, filepath.Join(stateDir, "frozen"))
					gateOK = err == nil
					if err != nil {
						gateOut = err.Error()
					} else {
						gateOut = "ok"
					}
				} else {
					script := resolvePath(loopDir, step.Path)
					cmd := exec.CommandContext(ctx, script)
					ownProcessGroup(cmd)
					cmd.Dir = workroot
					cmd.Env = env
					cmd.Stdin = nil
					if f, err := os.Open(os.DevNull); err == nil {
						cmd.Stdin = f
						defer f.Close()
					}
					outb, err := cmd.CombinedOutput()
					gateOut = string(outb)
					gateOK = err == nil
				}
				elapsed := int(time.Since(t0).Seconds())

				// An operator stop (SIGINT/SIGTERM) cancels ctx mid-gate; that is
				// not a gate failure, so log and report it as stopped.
				if ctx.Err() != nil {
					appendLog(gateLogPath, fmt.Sprintf("GATE %s: STOPPED\n\n", step.Name))
					r.StepDone(false, "stopped", elapsed)
					break
				}

				appendLog(gateLogPath, fmt.Sprintf("GATE %s: %s\n%s\n", step.Name, map[bool]string{true: "OK", false: "FAIL"}[gateOK], gateOut))
				lastGateName = step.Name
				lastGateOK = gateOK
				lastGateLog = gateOut

				if gateOK {
					r.StepDone(true, "OK", elapsed)
				} else {
					note := "FAIL"
					if d := strings.TrimSpace(gateOut); d != "" {
						if i := strings.IndexByte(d, '\n'); i >= 0 {
							d = d[:i]
						}
						if len(d) > 80 {
							d = d[:80] + "…"
						}
						note = "FAIL: " + d
					}
					r.StepDone(false, note, elapsed)
					if strings.TrimSpace(gateOut) != "" {
						r.GateDetail(gateOut)
					}
					if step.Required {
						iterOK = false
					}
				}

			case manifest.Hook:
				t0 := time.Now()
				r.StepStart("hook", step.Name, "")
				writeStatus(iter, cfg.MaxIter, "hook "+step.Name)
				script := resolvePath(loopDir, step.Path)
				cmd := exec.CommandContext(ctx, script)
				ownProcessGroup(cmd)
				cmd.Dir = workroot
				cmd.Env = env
				if f, err := os.Open(os.DevNull); err == nil {
					cmd.Stdin = f
					defer f.Close()
				}
				outb, _ := cmd.CombinedOutput()
				appendLog(gateLogPath, fmt.Sprintf("HOOK %s\n%s\n", step.Name, string(outb)))
				r.StepDone(true, "ran", int(time.Since(t0).Seconds()))
			}
		}

		if stopped {
			appendMeta(stateDir, "SUCCESS=0")
			writeStatus(iter, cfg.MaxIter, "stopped")
			r.Stopped(relState(loopDir, stateDir))
			summary("stopped", iter)
			return 1, nil
		}

		// Write handoff at end of iteration.
		frozenStatus := "not configured"
		if len(cfg.Freeze) > 0 {
			if err := freeze.Check(workroot, filepath.Join(stateDir, "frozen")); err != nil {
				frozenStatus = "drift"
			} else {
				frozenStatus = "ok"
			}
		}
		_ = session.WriteHandoff(handoffPath, session.Handoff{
			Goal:           loadGoal(workroot, loopDir, cfg.Context),
			Constraints:    loadConstraints(workroot, loopDir),
			LastGate:       lastGateName,
			LastGateOK:     lastGateOK,
			LastGateLog:    lastGateLog,
			DiffStat:       gitDiffStat(workroot),
			SessionPolicy:  string(cfg.Session),
			TurnsInSession: turnsThisSession,
			ContextPercent: lastCtxPercent,
			Compacted:      lastCompacted,
			Frozen:         frozenStatus,
		})

		if iterOK && objective {
			appendMeta(stateDir, "SUCCESS=1")
			writeStatus(iter, cfg.MaxIter, "success")
			r.Success(iter, relState(loopDir, stateDir))
			summary("success", iter)
			return 0, nil
		}
	}

	appendMeta(stateDir, "SUCCESS=0")
	if objective {
		writeStatus(startIter+1, cfg.MaxIter, "failed")
		r.Fail(relState(loopDir, stateDir))
		summary("fail", cfg.MaxIter)
		return 1, nil
	}
	writeStatus(startIter+1, cfg.MaxIter, "done")
	r.Done(relState(loopDir, stateDir))
	summary("done", cfg.MaxIter)
	return 0, nil
}

func newID() string {
	return time.Now().UTC().Format("20060102T150405Z") + "-" + strconv.Itoa(os.Getpid())
}

func gitWorkroot(loopDir string) (string, error) {
	cmd := exec.Command("git", "-C", loopDir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("workroot: not a git repo: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitCurrentBranch(workroot string) (string, error) {
	cmd := exec.Command("git", "-C", workroot, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitDiffStat(workroot string) string {
	cmd := exec.Command("git", "-C", workroot, "diff", "--stat")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func setupBranch(workroot, base, id, loopDir string) error {
	// Refuse a dirty tree, but tolerate untracked files that are part of the
	// loop's own recipe rather than the user's work-in-progress: anything under
	// the loop dir (.loop/ — loop.env, prompts/, gates/, gitignored state/), and
	// a top-level TASK.md at the workroot root (the handoff goal file — see
	// DESIGN.md / CHECKLIST.md, which put TASK.md at the repo root, not the loop
	// dir). Without this, `loop init && loop run` (the documented first-run
	// flow, with LOOP_BRANCH=1 by default) could never start, because init
	// leaves .loop/ untracked and the skill writes an untracked TASK.md.
	// Modified tracked files and untracked files that are not recipe artifacts
	// still refuse.
	st, err := exec.Command("git", "-C", workroot, "status", "--porcelain").Output()
	if err != nil {
		return err
	}
	loopRel := ""
	realWorkroot, err := filepath.EvalSymlinks(workroot)
	if err != nil || realWorkroot == "" {
		realWorkroot = workroot
	}
	realLoopDir, err := filepath.EvalSymlinks(loopDir)
	if err != nil || realLoopDir == "" {
		realLoopDir = loopDir
	}
	if rel, err := filepath.Rel(realWorkroot, realLoopDir); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "." && !strings.HasPrefix(rel, "../") {
			loopRel = rel
		}
	}
	for _, line := range strings.Split(string(st), "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 {
			continue
		}
		status := line[:2]
		path := strings.TrimRight(line[3:], "/")
		if status == "??" {
			// Untracked recipe artifacts are tolerated.
			if loopRel != "" && (path == loopRel || strings.HasPrefix(path, loopRel+"/")) {
				continue
			}
			// A top-level TASK.md (the goal file) at the workroot root only;
			// a nested subdir/TASK.md is not the recipe goal and still refuses.
			if path == "TASK.md" {
				continue
			}
		}
		return fmt.Errorf("worktree not clean — commit or stash before a branch loop")
	}
	branch := "loop/" + id
	backup := "backup/loop-" + id
	if err := exec.Command("git", "-C", workroot, "rev-parse", "--verify", base).Run(); err != nil {
		return fmt.Errorf("no branch %s — set LOOP_BRANCH_BASE", base)
	}
	_ = exec.Command("git", "-C", workroot, "branch", backup, base).Run()
	if err := exec.Command("git", "-C", workroot, "checkout", "-b", branch, base).Run(); err != nil {
		return fmt.Errorf("create %s failed: %w", branch, err)
	}
	return nil
}

func writeMeta(stateDir, id string, cfg config.Config, workroot string) error {
	base, _ := exec.Command("git", "-C", workroot, "rev-parse", cfg.BranchBase).Output()
	var b strings.Builder
	fmt.Fprintf(&b, "LOOP_ID=%s\n", id)
	if cfg.Branch {
		fmt.Fprintf(&b, "LOOP_BRANCH_NAME=loop/%s\n", id)
	}
	fmt.Fprintf(&b, "STARTED_AT=%s\nBASE=%s\nLOOP_SESSION=%s\nLOOP_MAX_ITER=%d\n",
		time.Now().UTC().Format(time.RFC3339),
		strings.TrimSpace(string(base)), cfg.Session, cfg.MaxIter)
	return os.WriteFile(filepath.Join(stateDir, "meta.env"), []byte(b.String()), 0o644)
}

func appendMeta(stateDir, line string) {
	f, err := os.OpenFile(filepath.Join(stateDir, "meta.env"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\nFINISHED_AT=%s\n", line, time.Now().UTC().Format(time.RFC3339))
}

func appendLog(path, s string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(s)
}

func readIntFile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func resolvePath(loopDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(loopDir, p)
}

func resolveModel(cfg config.Config, role string) string {
	if role == "" {
		return ""
	}
	if m, ok := cfg.Models[strings.ToLower(role)]; ok {
		return m
	}
	// Also try as direct model id if it looks like one.
	if strings.Contains(role, "/") {
		return role
	}
	return ""
}

func buildEnv(cfg config.Config, id, loopDir, workroot, stateDir, branch string, iter int, phase string) []string {
	env := os.Environ()
	// Strip existing LOOP_* then add resolved.
	filtered := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, "LOOP_") {
			continue
		}
		filtered = append(filtered, e)
	}
	env = filtered
	env = append(env, cfg.Environ()...)
	env = append(env,
		"LOOP_ID="+id,
		"LOOP_ROOT="+loopDir,
		"LOOP_WORKROOT="+workroot,
		"LOOP_STATE_DIR="+stateDir,
		"LOOP_BRANCH_NAME="+branch,
		"LOOP_ITERATION="+strconv.Itoa(iter),
		"LOOP_PHASE="+phase,
		"LOOP_LOG="+filepath.Join(stateDir, "gate-log.md"),
	)
	return env
}

func loadGoal(workroot, loopDir, context string) string {
	for _, dir := range []string{workroot, loopDir} {
		if dir == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "TASK.md"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Skip pure headings; use the first body line as the goal.
			if strings.HasPrefix(line, "#") {
				continue
			}
			return line
		}
		// No body line: fall back to first heading text.
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
			if line != "" {
				return line
			}
		}
	}
	return context
}

func loadConstraints(workroot, loopDir string) string {
	for _, dir := range []string{workroot, loopDir} {
		if dir == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "CONSTRAINTS.md"))
		if err != nil {
			continue
		}
		return string(b)
	}
	return ""
}

func applySet(cfg *config.Config, key, val string) {
	switch key {
	case "LOOP_MAX_ITER":
		if n, err := strconv.Atoi(val); err == nil {
			cfg.MaxIter = n
		}
	case "LOOP_SESSION":
		cfg.Session = config.SessionMode(val)
	case "LOOP_SESSION_TURNS":
		if n, err := strconv.Atoi(val); err == nil {
			cfg.SessionTurns = n
		}
	case "LOOP_FORK_PERCENT":
		if n, err := strconv.Atoi(val); err == nil {
			cfg.ForkPercent = n
		}
	case "LOOP_COMPACT":
		cfg.Compact = config.CompactMode(val)
	case "LOOP_CONTEXT":
		cfg.Context = val
	default:
		if role, ok := strings.CutPrefix(key, "LOOP_"); ok {
			if m, ok := strings.CutSuffix(role, "_MODEL"); ok {
				if cfg.Models == nil {
					cfg.Models = map[string]string{}
				}
				cfg.Models[strings.ToLower(m)] = val
			}
		}
	}
}

func relState(loopDir, stateDir string) string {
	rel, err := filepath.Rel(loopDir, stateDir)
	if err != nil {
		return stateDir
	}
	return rel
}

// ownProcessGroup puts cmd in its own process group and arranges for the
// whole group to be killed (not just the leader) when its context is
// cancelled, so orphaned grandchildren (e.g. `sleep` under a shell script)
// do not keep the run hanging on SIGINT/SIGTERM.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return os.ErrProcessDone
	}
	cmd.WaitDelay = 5 * time.Second
}

func fileIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// shortToolArg returns a short, human-readable snippet of a tool's first
// argument (e.g. "read loader.go", "bash go test") for the live tool line.
func shortToolArg(ev map[string]any) string {
	args, _ := ev["args"].(map[string]any)
	if args == nil {
		return ""
	}
	// Common pi tool args: command, path, file_path, pattern.
	for _, k := range []string{"command", "path", "file_path", "file", "pattern", "query"} {
		if v, ok := args[k].(string); ok && v != "" {
			s := strings.ReplaceAll(v, "\n", " ")
			if len(s) > 60 {
				s = s[:60] + "…"
			}
			return s
		}
	}
	return ""
}
