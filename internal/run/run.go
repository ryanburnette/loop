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
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
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
	OneShot  bool   // true when Dir is a throwaway scratch loop dir (print absolute state path, keep it)
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

	// Flag overrides built into the config overlay instead of mutating process
	// env. Pointer types distinguish "unset" from an explicit zero value so a
	// --branch=false can override LOOP_BRANCH=1 from loop.env.
	Branch     *bool
	BranchBase string
	Approve    *bool
	Context    string
	Models     map[string]string
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
	if opts.Branch != nil {
		b := *opts.Branch
		overlay.Branch = &b
	}
	if opts.BranchBase != "" {
		b := opts.BranchBase
		overlay.BranchBase = &b
	}
	if opts.Approve != nil {
		a := *opts.Approve
		overlay.Approve = &a
	}
	if opts.Context != "" {
		c := opts.Context
		overlay.Context = &c
	}
	if len(opts.Models) > 0 {
		overlay.Models = opts.Models
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
	r := ui.New(ui.Options{Out: out, Err: errOut, Color: color, Quiet: opts.Quiet, Verbose: opts.Verbose, JSON: opts.JSON})

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
		// Point CURRENT_ID at the run being resumed, so `loop status` reports
		// this run and not whichever one happened to start most recently.
		_ = os.WriteFile(filepath.Join(loopDir, "state", "CURRENT_ID"), []byte(id+"\n"), 0o644)
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

	branchName := ""
	if cfg.Branch {
		branchName = "loop/" + id
		if b, err := gitCurrentBranch(workroot); err == nil && b != "" {
			branchName = b
		}
	}

	// stateDisplay is what the final result line prints as the run's state
	// location. For a normal loop it is relative to the loop dir (state/<id>);
	// for a one-shot run the loop dir is a throwaway temp dir, so the relative
	// path is useless and we print the absolute state dir instead.
	stateDisplay := relState(loopDir, stateDir)
	if opts.OneShot {
		stateDisplay = stateDir
	}

	objective := man.HasObjective()

	// SIGINT / SIGTERM is a control stop: finish the current cheap step if
	// we can, write SUCCESS=0, and exit 1. Do not hang in the pause poll.
	stopSig := make(chan os.Signal, 1)
	signal.Notify(stopSig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopSig)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// stopped is written by the signal goroutine and read by the step loop, so
	// it has to be atomic — a plain bool here is a data race.
	var stopped atomic.Bool
	go func() {
		select {
		case <-stopSig:
			stopped.Store(true)
			cancel()
		case <-ctx.Done():
			// Run returned; nothing left to stop.
		}
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

	// Warn about unknown LOOP_* keys in loop.env (sorted for stable output)
	// so a typo like LOOP_MAX_ITERATIONS is not silently accepted as a runner
	// setting. These keys are not used by the runner, but they are still
	// exported to gates/hooks (via config.Extra), so say so honestly rather
	// than claiming they were ignored.
	if len(cfg.Unknown) > 0 {
		uks := append([]string(nil), cfg.Unknown...)
		sort.Strings(uks)
		for _, k := range uks {
			r.Warn("unknown loop.env key: " + k + " (not a runner setting; passed through to gates/hooks)")
		}
	}

	// Warn when the process environment silently overrode a key the recipe
	// set. An ambient LOOP_MAX_ITER in the operator's shell beating the cap in
	// loop.env is intended layering (DESIGN.md: flags and env win) but it is
	// invisible otherwise, and it has already bitten this repo's own tests.
	if len(cfg.Overridden) > 0 {
		oks := append([]string(nil), cfg.Overridden...)
		sort.Strings(oks)
		for _, k := range oks {
			r.Warn("process env overrides loop.env: " + k + " (env wins; unset it to use the recipe's value)")
		}
	}

	// Non-fatal manifest complaints: a mistyped key parses fine but changes
	// what the loop does, so say so rather than dropping it.
	for _, w := range man.Warnings {
		r.Warn(w)
	}

	// Per-run state shared across steps. The step bodies (runTurn/runGate/
	// runHook) are methods on runner so the iteration loop below reads as the
	// spec: act, check, feed the result back, repeat. cfg is owned by the
	// runner from here on; control `set` and the handoff read rr.cfg.
	rr := &runner{
		cfg:         cfg,
		sessPolicy:  session.Policy{Mode: cfg.Session, SessionTurns: cfg.SessionTurns, ForkPercent: cfg.ForkPercent},
		r:           r,
		opts:        opts,
		ctx:         ctx,
		id:          id,
		loopDir:     loopDir,
		workroot:    workroot,
		stateDir:    stateDir,
		branchName:  branchName,
		gateLogPath: filepath.Join(stateDir, "gate-log.md"),
		handoffPath: filepath.Join(stateDir, "handoff.md"),
		runStart:    runStart,
		sessID:      id,
	}
	paused := false

	// summary prints the end-of-run footer. result is one of
	// success|fail|stopped|done; iterUsed is the last iteration reached.
	summary := func(result string, iterUsed int) {
		r.Summary(ui.Summary{
			Elapsed:    time.Since(runStart),
			Iterations: iterUsed,
			MaxIter:    rr.cfg.MaxIter,
			Result:     result,
			Branch:     branchName,
		})
	}

	// lastIter is the iteration the run actually reached, for the final status
	// file and summary. It differs from MaxIter when the loop body never ran.
	lastIter := startIter
	for iter := startIter + 1; iter <= rr.cfg.MaxIter; iter++ {
		lastIter = iter
		if err := os.WriteFile(filepath.Join(stateDir, "iteration"), []byte(strconv.Itoa(iter)+"\n"), 0o644); err != nil {
			return 2, err
		}
		r.Iteration(iter, rr.cfg.MaxIter)
		rr.writeStatus(iter, "start")
		iterOK := true
		var (
			lastGateName string
			lastGateOK   bool
			lastGateLog  string
		)

		// Labeled so a step that must abort the rest of the iteration (a turn
		// that errored, a gate cut short by an operator stop) can break the
		// step loop. A bare `break` inside the type switch below only leaves
		// the switch, which silently ran the remaining steps — a gate could
		// then pass and report SUCCESS for an iteration whose model never ran.
	stepLoop:
		for _, step := range man.Steps {
			if stopped.Load() {
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
					rr.writeStatus(iter, "stopped")
					r.Stopped(stateDisplay)
					summary("stopped", iter)
					return 1, nil
				case control.Pause:
					paused = true
				case control.Resume:
					paused = false
				case control.Set:
					applySet(&rr.cfg, cmd.Key, cmd.Value)
					// Keep session policy in sync.
					rr.sessPolicy.Mode = rr.cfg.Session
					rr.sessPolicy.SessionTurns = rr.cfg.SessionTurns
					rr.sessPolicy.ForkPercent = rr.cfg.ForkPercent
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
					rr.writeStatus(iter, "stopped")
					r.Stopped(stateDisplay)
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
						rr.writeStatus(iter, "stopped")
						r.Stopped(stateDisplay)
						summary("stopped", iter)
						return 1, nil
					case control.Set:
						applySet(&rr.cfg, cmd.Key, cmd.Value)
						rr.sessPolicy.Mode = rr.cfg.Session
						rr.sessPolicy.SessionTurns = rr.cfg.SessionTurns
						rr.sessPolicy.ForkPercent = rr.cfg.ForkPercent
					}
				}
			}
			if didPause {
				r.Resumed()
			}

			env := buildEnv(rr.cfg, id, loopDir, workroot, stateDir, branchName, iter, step.Name)

			switch step.Type {
			case manifest.Turn:
				tr := rr.runTurn(step, iter)
				if tr.failed {
					iterOK = false
				}
				if tr.broke {
					// The iteration did not run to completion, so it cannot
					// have demonstrated its objective — even if the turn was
					// required=0. Without this, a soft turn whose pi call
					// errored would abort the steps and still be scored ok.
					iterOK = false
					break stepLoop
				}
			case manifest.Gate:
				gr := rr.runGate(step, iter, env)
				if gr.name != "" {
					lastGateName = gr.name
					lastGateOK = gr.ok
					lastGateLog = gr.log
				}
				if gr.failed {
					iterOK = false
				}
				if gr.broke {
					iterOK = false
					break stepLoop
				}
			case manifest.Hook:
				rr.runHook(step, iter, env)
			}
		}

		if stopped.Load() {
			appendMeta(stateDir, "SUCCESS=0")
			rr.writeStatus(iter, "stopped")
			r.Stopped(stateDisplay)
			summary("stopped", iter)
			return 1, nil
		}

		// Write handoff at end of iteration.
		frozenStatus := "not configured"
		if len(rr.cfg.Freeze) > 0 {
			if err := freeze.Check(workroot, filepath.Join(stateDir, "frozen")); err != nil {
				frozenStatus = "drift"
			} else {
				frozenStatus = "ok"
			}
		}
		_ = session.WriteHandoff(rr.handoffPath, session.Handoff{
			Goal:           loadGoal(workroot, loopDir, rr.cfg.Context),
			Constraints:    loadConstraints(workroot, loopDir),
			LastGate:       lastGateName,
			LastGateOK:     lastGateOK,
			LastGateLog:    lastGateLog,
			DiffStat:       gitDiffStat(workroot),
			SessionPolicy:  string(rr.cfg.Session),
			TurnsInSession: rr.turnsThisSession,
			ContextPercent: rr.lastCtxPercent,
			Compacted:      rr.lastCompacted,
			Frozen:         frozenStatus,
		})

		if iterOK && objective {
			appendMeta(stateDir, "SUCCESS=1")
			rr.writeStatus(iter, "success")
			r.Success(iter, stateDisplay)
			summary("success", iter)
			return 0, nil
		}
	}

	appendMeta(stateDir, "SUCCESS=0")
	if objective {
		rr.writeStatus(lastIter, "failed")
		r.Fail(stateDisplay)
		summary("fail", lastIter)
		return 1, nil
	}
	rr.writeStatus(lastIter, "done")
	r.Done(stateDisplay)
	summary("done", lastIter)
	return 0, nil
}

// runner holds the state shared across the steps of an iteration. The step
// helpers runTurn/runGate/runHook are methods on it so Run's loop body stays
// small and reads like the spec.
type runner struct {
	cfg        config.Config
	sessPolicy session.Policy
	r          *ui.Renderer
	opts       Options
	ctx        context.Context

	id         string
	loopDir    string
	workroot   string
	stateDir   string
	branchName string

	gateLogPath string
	handoffPath string

	runStart time.Time

	// Session state, carried across turns within and across iterations.
	sessID           string
	turnsThisSession int
	lastCtxPercent   int
	lastCompacted    bool
	hasSession       bool
}

// turnResult is the outcome of a turn step.
type turnResult struct {
	failed bool // a required turn failed → iteration not ok
	broke  bool // the turn errored → abort the rest of the iteration
}

// gateResult is the outcome of a gate step.
type gateResult struct {
	name   string // empty when the gate was stopped (no last-gate update)
	ok     bool
	log    string
	failed bool // a required gate failed → iteration not ok
	broke  bool // the gate was stopped (ctx cancelled) → abort the iteration
}

// writeStatus writes the one-line liveness file read by `loop status`.
func (rr *runner) writeStatus(iter int, phase string) {
	elapsed := int(time.Since(rr.runStart).Seconds())
	line := fmt.Sprintf("iteration %d/%d · phase: %s · elapsed %ds", iter, rr.cfg.MaxIter, phase, elapsed)
	_ = os.WriteFile(filepath.Join(rr.stateDir, "status"), []byte(line+"\n"), 0o644)
}

// runTurn executes one pi turn: resolve the session decision, build the
// request, stream live events, react to compaction, and evaluate a soft
// verdict. It mutates the runner's session state and returns whether the
// iteration is still ok and whether the step loop should break (turn error).
func (rr *runner) runTurn(step manifest.Step, iter int) turnResult {
	t0 := time.Now()
	modelID := resolveModel(rr.cfg, step.Model)
	detail := modelID
	if detail == "" {
		detail = "default"
	}
	rr.r.StepStart("turn", step.Name, detail)
	rr.writeStatus(iter, "turn "+step.Name)

	dec := rr.sessPolicy.Decide(session.State{
		TurnsThisSession: rr.turnsThisSession,
		ContextPercent:   rr.lastCtxPercent,
		Compacted:        rr.lastCompacted,
		HasSession:       rr.hasSession,
	})
	// Consume compaction flag after deciding.
	rr.lastCompacted = false

	req := pi.Request{
		PiPath:         rr.cfg.PiPath,
		Model:          modelID,
		Approve:        rr.cfg.Approve,
		System:         step.System,
		NoContextFiles: rr.cfg.NoContextFiles,
		PromptFile:     resolvePath(rr.loopDir, step.Path),
		Context:        rr.cfg.Context,
		WorkRoot:       rr.workroot,
		Ctx:            rr.ctx,
	}
	// Attach handoff on every iteration after the first.
	if iter > 1 {
		if _, err := os.Stat(rr.handoffPath); err == nil {
			req.Handoff = rr.handoffPath
		}
	}
	switch {
	case !dec.UseSession:
		// none — leave SessionID empty → --no-session
	case dec.Action == session.New:
		rr.sessID = fmt.Sprintf("%s-%d-%s", rr.id, iter, step.Name)
		rr.turnsThisSession = 0
		rr.hasSession = true
		req.SessionID = rr.sessID
		req.SessionDir = filepath.Join(rr.stateDir, "sessions")
	case dec.Action == session.Fork:
		prev := rr.sessID
		rr.sessID = fmt.Sprintf("%s-%d-%s", rr.id, iter, step.Name)
		rr.turnsThisSession = 0
		rr.hasSession = true
		req.SessionID = rr.sessID
		req.SessionDir = filepath.Join(rr.stateDir, "sessions")
		req.ForkID = prev
	default: // Continue
		req.SessionID = rr.sessID
		req.SessionDir = filepath.Join(rr.stateDir, "sessions")
	}

	turnBase := filepath.Join(rr.stateDir, fmt.Sprintf("turn-%d-%s", iter, step.Name))
	req.StdoutFile = turnBase + ".md"
	req.JSONLFile = turnBase + ".jsonl"
	req.StderrFile = turnBase + ".err"

	// Live tool line: stream tool events as they arrive instead of only after
	// the turn ends.
	turnStart := t0
	req.OnEvent = func(ev pi.Event) {
		switch {
		case ev.ToolName != "":
			rr.r.Tool(ev.ToolName, shortToolArg(ev.Raw))
		case ev.ContextPercent > 0 && ev.Type == "session_status":
			rr.r.Context(ev.ContextPercent, int(time.Since(turnStart).Seconds()))
		case ev.TextDelta != "" && rr.opts.Verbose:
			rr.r.Assistant(ev.TextDelta)
		}
	}

	res, err := pi.Run(req)
	elapsed := int(time.Since(t0).Seconds())
	if err != nil {
		rr.r.StepDone(false, "errored", elapsed)
		appendLog(rr.gateLogPath, fmt.Sprintf("TURN %s: ERROR\n%s\n\n", step.Name, err.Error()))
		if step.Required {
			return turnResult{failed: true, broke: true}
		}
		return turnResult{broke: true}
	}
	// Assistant text was already streamed to the UI via OnEvent text deltas
	// when -v is on; res.Text is kept for the verdict match and the turn file.
	rr.lastCtxPercent = res.ContextPercent
	if dec.UseSession {
		rr.turnsThisSession++
		rr.hasSession = true
	}

	note := "done"
	ok := true
	if res.Compacted {
		rr.lastCompacted = true
		switch rr.cfg.Compact {
		case config.CompactFail:
			ok = false
			note = "compacted"
		case config.CompactWarn:
			rr.r.Warn("compaction detected; next turn will start a new session")
			// Force new session next time via lastCompacted.
		case config.CompactAllow:
			// do nothing
		}
	}

	// Verdict check runs BEFORE StepDone so a failed required verdict renders
	// as a failing step (not a passing one) and reaches the UI/reporter.
	verdictMatched := true
	verdictEvaluated := false
	if step.Verdict != "" && ok {
		verdictEvaluated = true
		verdictMatched = matchVerdict(step.Verdict, res.Text)
		appendLog(rr.gateLogPath, fmt.Sprintf("VERDICT %s: %s\n",
			step.Name, map[bool]string{true: "PASS", false: "FAIL"}[verdictMatched]))
		if !verdictMatched && step.Required {
			ok = false
			note = fmt.Sprintf("FAIL: verdict (%s not found)", step.Verdict)
		}
	}
	rr.r.StepDone(ok, note, elapsed)
	// Emit the verdict event / soft marker only when the verdict was actually
	// evaluated. A turn that compacted (ok=false before the verdict check)
	// skips the verdict; reporting matched=true for it would mislead JSON
	// consumers.
	if verdictEvaluated {
		rr.r.Verdict(step.Name, verdictMatched, step.Required)
	}
	if step.Required && !ok {
		return turnResult{failed: true}
	}
	return turnResult{}
}

// runGate executes one gate step: the built-in loop:frozen check, or an
// executable script. It logs the outcome to gate-log.md and returns the
// result for the iteration loop (which tracks the last gate for the handoff).
func (rr *runner) runGate(step manifest.Step, iter int, env []string) gateResult {
	t0 := time.Now()
	detail := ""
	if step.Required {
		detail = "required"
	}
	rr.r.StepStart("gate", step.Name, detail)
	rr.writeStatus(iter, "gate "+step.Name)

	var (
		gateOK  bool
		gateOut string
	)
	if step.Path == "loop:frozen" {
		err := freeze.Check(rr.workroot, filepath.Join(rr.stateDir, "frozen"))
		gateOK = err == nil
		if err != nil {
			gateOut = err.Error()
		} else {
			gateOut = "ok"
		}
	} else {
		script := resolvePath(rr.loopDir, step.Path)
		cmd := exec.CommandContext(rr.ctx, script)
		ownProcessGroup(cmd)
		cmd.Dir = rr.workroot
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

	// An operator stop (SIGINT/SIGTERM) cancels ctx mid-gate; that is not a
	// gate failure, so log and report it as stopped.
	if rr.ctx.Err() != nil {
		appendLog(rr.gateLogPath, fmt.Sprintf("GATE %s: STOPPED\n\n", step.Name))
		rr.r.StepDone(false, "stopped", elapsed)
		return gateResult{broke: true}
	}

	appendLog(rr.gateLogPath, fmt.Sprintf("GATE %s: %s\n%s\n", step.Name, map[bool]string{true: "OK", false: "FAIL"}[gateOK], gateOut))

	if gateOK {
		rr.r.StepDone(true, "OK", elapsed)
		return gateResult{name: step.Name, ok: true, log: gateOut}
	}
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
	rr.r.StepDone(false, note, elapsed)
	if strings.TrimSpace(gateOut) != "" {
		rr.r.GateDetail(gateOut)
	}
	return gateResult{name: step.Name, ok: false, log: gateOut, failed: step.Required}
}

// runHook executes one hook step. Hooks are fire-and-forget: their output is
// logged but they never fail the iteration.
func (rr *runner) runHook(step manifest.Step, iter int, env []string) {
	t0 := time.Now()
	rr.r.StepStart("hook", step.Name, "")
	rr.writeStatus(iter, "hook "+step.Name)
	script := resolvePath(rr.loopDir, step.Path)
	cmd := exec.CommandContext(rr.ctx, script)
	ownProcessGroup(cmd)
	cmd.Dir = rr.workroot
	cmd.Env = env
	if f, err := os.Open(os.DevNull); err == nil {
		cmd.Stdin = f
		defer f.Close()
	}
	outb, _ := cmd.CombinedOutput()
	appendLog(rr.gateLogPath, fmt.Sprintf("HOOK %s\n%s\n", step.Name, string(outb)))
	rr.r.StepDone(true, "ran", int(time.Since(t0).Seconds()))
}

func newID() string {
	return time.Now().UTC().Format("20060102T150405Z") + "-" + strconv.Itoa(os.Getpid())
}

func gitWorkroot(loopDir string) (string, error) {
	cmd := exec.Command("git", "-C", loopDir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("workroot: %s is not inside a git repo (run `git init` first)", loopDir)
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
		return fmt.Errorf("worktree not clean — commit or stash before a branch loop, or pass --branch=false to run on the current tree: %s", path)
	}
	branch := "loop/" + id
	backup := "backup/loop-" + id
	if err := exec.Command("git", "-C", workroot, "rev-parse", "--verify", base).Run(); err != nil {
		return fmt.Errorf("no branch %s — set LOOP_BRANCH_BASE or pass --base BRANCH", base)
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

// matchVerdict reports whether the model's turn output contains the verdict
// pattern on its own line. A verdict like `^VERDICT: PASS` must hit on its
// own line even when the model writes prose before it: `(?m)` makes ^/$
// anchor at line boundaries (idempotent if the pattern already carries it).
// A bad pattern falls back to a literal substring contains.
func matchVerdict(pattern, text string) bool {
	if re, err := regexp.Compile("(?m)" + pattern); err == nil {
		return re.MatchString(text)
	}
	return strings.Contains(text, pattern)
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
