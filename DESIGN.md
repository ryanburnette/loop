# loop2

A Go rewrite of `loop`. Same idea: you describe an outer loop (prompts, gates,
hooks), the runner acts, checks something the model cannot talk around, feeds
the result back, and repeats until a stopping rule fires.

This file is the spec. Implement this. Do not invent a different product.

## Why rewrite

The POSIX runner works. It is one file, it has no dependencies, and it has run
real loops. What it cannot do well:

- Show what is happening inside a `pi` turn (tools, tokens, cost).
- Enforce a session/compaction policy. It either shares one session forever or
  shares nothing.
- Accept a mid-run change (pause, bump the cap, swap a model) without a kill.

Those three are the reason for Go, lipgloss, and a control file. Everything else
stays as close to v1 as it can.

## What stays

- A loop is a directory: `loop.env` + exactly one of `manifest` or (later)
  `loop.sh` + `prompts/` + `gates/` + `hooks/`.
- Manifest steps: `turn | gate | hook`. Same line format, same `verdict=` /
  `system=` "rest of line" rule, same `required=` default of 1.
- Success is decided once per iteration, not per step. A loop with an objective
  (any required gate or required verdict) exits 0 on the first `ok` iteration
  and 1 at the cap. A loop with no objective runs `MaxIter` times and exits 0.
- Workroot is the containing git repo (`git -C <loop-dir> rev-parse --show-toplevel`).
  No external-workroot flag.
- Config layering: defaults, then `loop.env`, then process env / flags. Flags
  and env win so a one-off does not require editing the file.
- `LOOP_FREEZE` + built-in `loop:frozen` gate. Resume does not re-freeze.
- `LOOP_BRANCH=1` creates `loop/<id>` off `LOOP_BRANCH_BASE` and a
  `backup/loop-<id>` safety branch, and refuses a dirty tree.
- Turns call `pi -p` with `@<abs-path>` prompts, `--approve` when configured,
  stdin from `/dev/null`.
- No build step for *using* a loop. The runner is a Go binary the user installs;
  a loop dir is still just files and scripts.
- Gates and hooks run with workroot as cwd and the `LOOP_*` env vars exported.

## What changes

- The runner is Go. Packages under `internal/`. `flag.FlagSet` only — no Cobra,
  no Viper, no Bubble Tea in v1.
- Terminal output is lipgloss (`github.com/charmbracelet/lipgloss`, the v1
  module gitaware already uses). Not a full TUI. Styled lines, a live status
  block, tool/token lines from `pi --mode json`.
- `pi` is invoked with `--mode json` so the runner can see tools, usage, and
  compaction events. The model's text is extracted from those events for the
  turn file and the verdict grep.
- Session policy is first-class: `none | shared | fork`. See Compaction.
- Every iteration after the first attaches a runner-authored `handoff.md`.
  Session memory is a convenience. The handoff and the last gate log are the
  source of truth.
- A control file (`state/<id>/control`) is read between steps. v1 of the
  control plane is pause / resume / stop / set. No interactive editor yet.
- `loop.env` is `KEY=VALUE`, not a sourced shell script. No `${VAR:-default}`
  expansion. Defaults live in the runner. The runner exports the resolved
  `LOOP_*` values so existing gate scripts keep working.
- A loop can be started from flags alone (`loop run --prompt … --gate …`)
  without a directory. That path builds a scratch loop dir in the OS temp
  directory — recipe and state both — so the user's workroot is never dirtied
  by the run. The summary prints the absolute state path for inspection
  afterward. Like the `loop init` templates, a one-shot defaults to
  `LOOP_BRANCH=1`; `--branch=false` runs it against the current tree.

## Compaction

Pi's auto-compaction is lossy. A loop that depends on it will forget a
constraint, a failed gate, or the original goal, and then narrate success. The
research notes already say the model is an optimistic narrator of its own work.
A compacted session is that narrator writing the history it will read next.

So the runner's job is to **avoid needing compaction**, and to **refuse to
pretend a compacted session is fine**.

### Avoid

1. **Prefer `none` for gate-driven loops.** until-green does not need history.
   Each turn is a fresh `pi --no-session`. The prompt file plus `@handoff.md`
   (last gate output, diffstat, goal, constraints) is the whole context. There
   is nothing to compact.
2. **Cap turns per shared session.** `LOOP_SESSION_TURNS` (default 4). After
   that many turns in one session, start a new session and attach the handoff.
   Four turns of coding against a 500k-window model (grok-4.5, GLM-5.2) almost
   never fill the window if tool output is not dumped raw.
3. **Re-feed the check every time.** The last gate log is copied into the
   handoff, truncated if huge (keep head and tail, note the cut). Do not rely
   on the model remembering `go test` failed.
4. **Do not pull the world into context.** `LOOP_NO_CONTEXT_FILES=1` passes
   `--no-context-files` so a huge `AGENTS.md` tree is not loaded on every turn.
   Default is off (keep project instructions). The build loop can turn it on
   if needed.
5. **Use the big windows.** The models this stack actually has are 500k–1M.
   Do not design as if the window were 32k.

### Detect

`pi --mode json` emits `compaction_start` / `compaction_end` and
`contextUsage`. The runner records both on the live status line.

### React

`LOOP_COMPACT` (`fail` | `warn` | `allow`, default `warn`):

- `fail` — a compaction event fails the turn (and so the iteration, if the
  turn is required). Next iteration starts a new session with a full handoff.
- `warn` — log it, force-fork the next turn onto a new session, keep going.
- `allow` — do nothing. Exists for debugging. Do not default to this.

Never call `pi`'s compact command. Never continue a compacted shared session
as if the summary were the work.

### `fork` policy

`LOOP_SESSION=fork` starts shared, then `--fork`s (or opens a new
`--session-id`) when either:

- turns in this session hit `LOOP_SESSION_TURNS`, or
- `contextUsage.percent` is at least `LOOP_FORK_PERCENT` (default 40).

The new session gets the same handoff a `none` turn would. History that still
matters has been written down by the runner, not summarized by the model.

`shared` is still valid for short loops (double-check is two turns). It is the
wrong default for anything that might run to the cap.

## Architecture

```
cmd/loop/            flag dispatch, usage, version
internal/config/     defaults + loop.env + env + flags
internal/manifest/   parse steps, HasObjective
internal/freeze/     snapshot + compare
internal/session/    none|shared|fork, handoff file
internal/pi/         build argv, run, parse jsonl events
internal/control/    read/truncate state/<id>/control
internal/run/        the iteration loop
internal/ui/         lipgloss renderer
```

`internal/run` is the only package that knows the full iteration. Everyone
else is a library with tests. `cmd/loop` parses flags and calls `run.Run`.

No `internal/app` god package. No Makefile.

### Config

Resolved struct, not a bag of globals:

```
MaxIter        default 5
Session        none | shared | fork     default none
SessionTurns   default 4
ForkPercent    default 40
Compact        fail | warn | allow      default warn
Branch         default false
BranchBase     default main
Approve        default true
Freeze         []string
Context        string
NoContextFiles default false
Models         map[role]string          from LOOP_<ROLE>_MODEL
TestCmd        default "go test ./..."
PiPath         default "pi"             from LOOP_PI or PATH
```

`loop.env` parser: skip blank lines and `#` comments. Accept `KEY=VALUE` and
`KEY="VALUE"` / `KEY='VALUE'`. Reject backticks and `$(...)`. Unknown keys
that start with `LOOP_` are kept and exported (gates use `LOOP_TEST_CMD` and
`LOOP_FINDINGS`).

Flag / env overlay uses the same names as v1 (`LOOP_MAX_ITER`, …) plus the
new ones (`LOOP_SESSION_TURNS`, `LOOP_FORK_PERCENT`, `LOOP_COMPACT`,
`LOOP_NO_CONTEXT_FILES`, `LOOP_PI`).

### Manifest

Same grammar as v1. `verdict=` and `system=` consume the rest of the line and
must be last. `HasObjective` is true if any gate has `required != 0` (the
default) or any turn has a verdict and `required != 0`.

### Handoff

Written by the runner to `state/<id>/handoff.md` at the end of each
iteration, and attached as `@<abs>` on the next turn. Contents, in order:

1. Goal (first non-empty line of `TASK.md` if present, else `LOOP_CONTEXT`)
2. Constraints copied from the loop dir's `CONSTRAINTS.md` if present
3. Last gate name + exit + tail of its log
4. `git -C workroot diff --stat`
5. Session facts: policy, turns this session, last context percent, whether
   a compaction event fired
6. Frozen: ok / drift / not configured

Do not ask the model to write this file.

### Control file

Between steps, if `state/<id>/control` exists, read it, truncate it, apply:

```
pause                  # block until resume or ctx cancel
resume
stop                   # fail the run, exit 1
set KEY=VALUE          # overlay config (MaxIter, models, Compact, Session, …)
```

Unknown lines are warnings, not fatal. This is the hook a future interactive
UI writes to. v1 does not ship that UI.

Also honor `SIGINT` / `SIGTERM` as `stop` (finish the current step if cheap,
then exit 1 and write `SUCCESS=0`).

### `pi` invocation

```
pi -p --mode json
   [--model <id>]
   [--session-id <id> --session-dir <dir> | --no-session]
   [--fork <id>]          # only when policy says so
   [--approve]
   [--append-system-prompt <text>]
   [--no-context-files]
   @<prompt> [@<handoff>] [<context>…]
```

Stdin is `/dev/null`. Cwd is workroot. Parse stdout as jsonl. Write:

- `turn-<iter>-<name>.md` — extracted assistant text (for verdicts and humans)
- `turn-<iter>-<name>.jsonl` — raw events
- `turn-<iter>-<name>.err` — stderr

A turn with empty extracted text and a non-empty `.err` is an error. A
`compaction_start` event is handled per `LOOP_COMPACT`.

`LOOP_PI` overrides the binary so tests can point at `testdata/fake-pi`.

### UI

lipgloss on stdout when stdout is a TTY and `NO_COLOR` is unset. Otherwise
the same information as timestamped plain lines (background / log safe).

Always show:

- run header: id, dir, workroot, branch, session policy, max iter, objective
- per iteration: `iteration i/n`
- per step: kind, name, model or required, elapsed
- during a turn: last tool (`read foo.go`, `bash go test`), context percent,
  running elapsed
- per gate: pass/fail
- footer: success / failed-at-cap / done-no-objective, path to state

`-v` also prints extracted assistant text as it lands. `-q` prints only the
final result line. `--json` prints one machine event per line instead of the
human view (runner events, not pi's).

Do not use a spinner that fights with tool lines. A single status line that
updates in place on a TTY is enough.

### State

Same layout as v1, plus:

```
state/<id>/
  handoff.md
  control            # optional, user/UI written
  status             # one live line, as v1
  turn-*.jsonl
```

`meta.env` keeps the v1 keys so `loop status` stays useful.

## CLI

```
loop <dir> [flags]              run (dir as first arg, v1 compatible)
loop run <dir> [flags]          same
loop run --prompt F --gate C    one-shot, no dir
loop status <dir>
loop freeze <dir>
loop frozen?                    uses LOOP_STATE_DIR, same as v1
loop help
loop version
```

Reserved flags (go-develop): `-v` verbose, `-V` / `version` version, `help`
prints version + usage. `-q` quiet. `--resume <id>`. `--json`.

Overrides: `--max-iter`, `--session`, `--branch`, `--base`, `--approve`,
`--context`, `--model role=id` (repeatable), `--compact`, `--pi`.

## Tests first

The packages above have table tests. `internal/run` has an integration test
that points `LOOP_PI` at `testdata/fake-pi` and runs the fixture loop in
`testdata/loops/until-green`. Do not talk to a real model in `go test`.

`testdata/fake-pi` is a small program or script that reads its argv, writes a
jsonl stream (including an optional `compaction_start` when
`FAKE_PI_COMPACT=1`), and exits 0. It is the contract for `internal/pi`.

Do not edit `*_test.go` to make the suite pass. If a test is genuinely wrong,
stop and say why.

## What not to do

- Do not add Cobra, Viper, Bubble Tea, or a web UI.
- Do not add an external-workroot flag.
- Do not source `loop.env` with a shell.
- Do not call `pi` compact, and do not enable pi auto-compaction from here.
- Do not re-freeze on resume.
- Do not put the runner on the user's PATH from this repo. They install it.
- Do not keep implementing `loop.sh` custom mode in v1. Manifest + one-shot
  flags are enough. Leave a comment in `run` that custom mode is deferred.
- Do not weaken a test or delete a fixture to get green.

## Implementation order

1. `internal/manifest`, `internal/config`, `internal/freeze`,
   `internal/control` — they have no subprocess and should go green first.
2. `internal/pi` against fake-pi and the jsonl fixtures.
3. `internal/session` (policy decisions + handoff file).
4. `internal/ui` (render to a `io.Writer`; assert on plain output with color
   off).
5. `internal/run` + `cmd/loop` until `go test ./...` is green.

Commit as you go, on the branch the runner created.
