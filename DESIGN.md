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
- Manifest steps: `turn | gate | hook`. `verdict=` and `system=` consume the
  rest of the line. `scorecard=` is one path token. `required=` defaults to 1.
- Success is decided once per iteration, not per step. A loop with an objective
  (any required gate, required verdict, or required scorecard) exits 0 on the
  first `ok` iteration and 1 at the cap. A loop with no objective runs
  `MaxIter` times and exits 0.
- Workroot is the containing git repo (`git -C <loop-dir> rev-parse --show-toplevel`).
  No external-workroot flag.
- Config layering: defaults, then `loop.env`, then process env / flags. Flags
  and env win so a one-off does not require editing the file.
- `LOOP_FREEZE` + built-in `loop:frozen` gate. The baseline is in memory.
  Resume does not re-freeze. A missing `frozen/index` fails closed.
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
  compaction events. Every assistant message is kept for the turn file and
  the verdict grep, not only the last one. Messages are joined with a blank
  line, `---`, and a blank line. Streamed deltas are not part of that text.
  Legacy `verdict=` greps that text. New templates do not use it.
  A scorecard is a file the runner scores.
- Session policy is first-class: `none | shared | fork`. See Compaction.
- Every iteration after the first attaches a runner-authored `mend.md`.
  Later turns in that iteration also attach `brief.md`. Session memory is a
  convenience inside one iteration. The mend is the source of truth.
  `handoff.md` is a stub and is not attached.
- A control file (`state/<id>/control`) is read between steps. v1 of the
  control plane is pause / resume / stop / set. No interactive editor yet.
- `loop.env` is `KEY=VALUE`, not a sourced shell script. No `${VAR:-default}`
  expansion. Defaults live in the runner. The runner exports the resolved
  `LOOP_*` values, each key once, so existing gate scripts keep working.
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
   Each turn is a fresh `pi --no-session`. After the first iteration the
   prompt file plus `@mend.md` is the context. Later turns in that iteration
   also get `@brief.md`. There is nothing to compact.
2. **Cap turns per shared session.** `LOOP_SESSION_TURNS` (default 4). After
   that many turns, or at the next iteration, start a new session. Attach
   the mend, and on a later turn the brief. Do not pass `pi --fork`.
   Four turns of coding against a 500k-window model (grok-4.5, GLM-5.2) almost
   never fill the window if tool output is not dumped raw.
3. **Re-feed the check every time.** The mend records the gate exit code, the
   scorecard result, and at most 8 lines of tail. The full gate log is not
   pasted. Do not rely on the model remembering `go test` failed.
4. **Do not pull the world into context.** `LOOP_NO_CONTEXT_FILES=1` passes
   `--no-context-files` so a huge `AGENTS.md` tree is not loaded on every turn.
   Default is off (keep project instructions). The build loop can turn it on
   if needed.
5. **Use the big windows.** The models this stack actually has are 500k–1M.
   Do not design as if the window were 32k.

### Detect

`pi --mode json` emits `compaction_start` / `compaction_end`. It does not
emit context percent. After a `shared` or `fork` turn the runner probes
`get_session_stats` and records that percent. `none` does not probe.

### React

`LOOP_COMPACT` (`fail` | `warn` | `allow`, default `warn`):

- `fail` — a compaction event fails the turn (and so the iteration, if the
  turn is required).
- `warn` — log it and keep going.
- `allow` — no warning, and the turn is not failed for compaction. Exists
  for debugging. Do not default to this.

All three cut the session. The next turn opens a new session instead of
continuing the one pi just summarized, and it is attached the current brief.
A turn that compacts and then exits non-zero still counts: the runner records
the compaction before it handles the error. `allow` only suppresses the
warning and the turn failure.

Never call `pi`'s compact command. Never continue a compacted shared session
as if the summary were the work. Never pass `pi --fork` to copy that summary
into the next session.

### `fork` policy

`LOOP_SESSION=fork` is `shared`, plus one extra cut. The name stays so
existing `loop.env` files parse. The runner does not pass `pi --fork`.
That flag copies the transcript into the next session.

After a `shared` or `fork` turn, the runner reads `contextUsage.percent`
from `get_session_stats` on the jsonl that turn already wrote. The percent
is unknown when that file cannot be resolved, the probe fails, or the
payload has no numeric percent for this session. Unknown is not 0. It does
not cut, and it does not fail the turn. `none` does not probe.

`fork` opens a new empty `--session-id` when any of these is true:

- the previous turn compacted
- turns in this session hit `LOOP_SESSION_TURNS`
- the percent is known, `LOOP_FORK_PERCENT` is greater than 0, and the
  percent is at least that threshold (default 40)

A known 0 does not cut at the default of 40. `LOOP_FORK_PERCENT` of 0 or
less disables the percent cut. It does not mean always cut.

The new session is empty. It gets the same mend a `none` turn would, and
the brief when this is not the first turn of the iteration. History that
still matters has been written down by the runner, not summarized by the
model.

`shared` remains available when an operator wants the in-iteration transcript.
The init templates default to `none`. `double-check` is two turns and does not
share a session. A later turn reads `brief.md`, not the previous pi session.
The iteration boundary drops the session id. The next iteration starts a new
one.

## Architecture

```
cmd/loop/            flag dispatch, usage, version
internal/config/     defaults + loop.env + env + flags
internal/manifest/   parse steps, HasObjective
internal/freeze/     snapshot + compare to an in-memory baseline
internal/mend/       mend.md, brief.md, settled ledger
internal/session/    none|shared|fork
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
Stall          stop | continue         default stop
StallPaths     []string                from LOOP_STALL_PATHS
```

`loop.env` parser: skip blank lines and `#` comments. Accept `KEY=VALUE` and
`KEY="VALUE"` / `KEY='VALUE'`. Reject backticks and `$(...)`. Unknown keys
that start with `LOOP_` are kept and exported (gates use `LOOP_TEST_CMD` and
`LOOP_FINDINGS`). Gates and hooks see each key once. These runtime values
overwrite the recipe and the process environment: `LOOP_ID`, `LOOP_ROOT`,
`LOOP_WORKROOT`, `LOOP_STATE_DIR`, `LOOP_BRANCH_NAME`, `LOOP_ITERATION`,
`LOOP_PHASE`, `LOOP_LOG`.

`LOOP_SESSION` must be exactly `none`, `shared`, or `fork`. `LOOP_COMPACT`
must be exactly `fail`, `warn`, or `allow`. `LOOP_STALL` must be exactly
`stop` or `continue`. Any other value is a load error that names the key and
the legal set, whether it came from `loop.env`, the process environment, or a
flag. A control-file `set` of an illegal value warns and leaves the previous
value in place.

Flag / env overlay uses the same names as v1 (`LOOP_MAX_ITER`, …) plus the
new ones (`LOOP_SESSION_TURNS`, `LOOP_FORK_PERCENT`, `LOOP_COMPACT`,
`LOOP_NO_CONTEXT_FILES`, `LOOP_PI`, `LOOP_STALL`, `LOOP_STALL_PATHS`).

### Manifest

Steps stay `turn | gate | hook`. `verdict=` and `system=` consume the rest of
the line and must be last. `scorecard=` is one path token. It does not consume
the rest of the line, so `required=` may sit on either side of it. A line with
both `verdict=` and `scorecard=` is a parse error. `verdict=` stays for recipes
that already use it.

`HasObjective` is true if any gate has `required != 0` (the default), or any
required turn has a `verdict` or a `scorecard`. A `required=0` scorecard is
advice. It is logged and does not by itself end the loop.

`Derive` does not invent scorecard steps from filenames. A scorecard exists
only when a manifest names it.

A turn with `scorecard=` is a judging turn. It runs with
`--no-extensions --tools read,grep,find,ls,write` and no other tool list.
The runner hashes the loop directory except `state/` before and after that
turn. A mismatch fails the iteration even when `required=0`. An unreadable
filled file fails a required scorecard. On `required=0`, if the recipe did
not change, the runner logs `UNREADABLE` and continues. A pi crash still
aborts the iteration.

### Mend

The runner writes `state/<id>/mend.md` at the end of every iteration,
including `none`. It is the source of truth for the next iteration. The
model does not write it. `handoff.md` in the same directory is a stub that
points at `mend.md`. The runner does not attach the stub, the previous
session, turn files, or the raw gate log.

`mend.md` is attached on every turn after the first iteration. `brief.md`
is the same kind of page for steps that have already finished in the current
iteration. It is attached on later turns of that iteration, including the
turn after a compaction. At iteration end the brief is replaced with
`See mend.md.`

Facts are runner-owned and labeled `source: runner`: the goal and
constraints from the loop dir, gate name and exit code, scorecard rule
result and marks, diff stat, freeze, and the session counters. The goal is
the first non-heading line of the loop dir's `TODO.md` if present, else
`LOOP_CONTEXT`. A `TODO.md` outside the loop dir is not the goal.

The model may propose settled lines as JSON (`LOOP_PROPOSAL_OUT`). The
runner commits them into `ledger.json` after a structural check. A newline,
an empty field, or a non-boolean `do_not_retry` is rejected. The ledger
holds 24 settled lines. The 25th drops the oldest line whose `do_not_retry`
is false, or the oldest line if every one is do-not-retry. Settled lines are
labeled as claims. Facts win.

The diff stat is three parts, capped at 30 lines: `git diff --stat
START...HEAD`, staged, and unstaged. `START` is `git rev-parse HEAD`
recorded in `meta.env` after branch setup, or at once when no branch is
created. Resume does not rewrite it. `BASE` stays the branch base and is
not the diff root. A missing `START` renders `committed: base unknown`.
When this run created `loop/<id>`, `START` equals `BASE`.

Resume reads the iteration file and starts at the next iteration. It
requires both `mend.md` and `ledger.json`. If either is missing or
unreadable, the run stops and tells the operator to start a new run.
Resume does not load a pre-mend handoff, does not re-freeze, and does not
rebuild a transcript.

Shared and fork session ids are abandoned at the iteration boundary. The
next iteration opens a new session. The runner does not pass `pi --fork`.

### Freeze

`LOOP_FREEZE` is a space-separated list of basename globs (`*_test.go`, not
paths). A fresh run snapshots matching files under `state/<id>/frozen/`
(`index`, plus one `N.sum` per pattern). Resume does not snapshot again.

The files on disk are a record. The turn can edit them. After the snapshot,
and on resume after the existing snapshot is read, the runner loads the sums
into memory. That copy is the baseline. Each `loop:frozen` gate hashes the
worktree and compares it to the baseline. It does not use the on-disk `*.sum`
as the expected hash.

A missing `index` fails the check. The error text is `freeze index missing`.
An empty index (no patterns) means nothing is frozen, and the check returns
nil. If `index` or a `*.sum` differs from the loaded copy, the gate fails
with `freeze store modified`.

A pattern that matches no files still writes an empty sum. A file created
later that matches the pattern is drift. Resume reloads the sums once, then
keeps that copy. It does not re-snapshot. Re-snapshotting would bless edits
made before the process died.

The mend line stays `ok`, `drift`, or `not configured`. A missing index
or a modified store is `drift`.

### Stopping rules

Success is the first iteration whose required checks pass. The run exits 0.
A loop with no required check runs to `MaxIter` and exits 0 with `result:
done`. That is not a pass. Spending the cap with a required check still red
exits 1 with `result: fail`. `stop` in the control file, or `SIGINT` /
`SIGTERM`, exits 1 with `result: stopped`. Nothing is merged.

Preflight runs after branch setup and the freeze snapshot, before any pi
turn. It does not run the gates. A failure exits 2, writes `return.md` with
`result: recipe`, and does not start iteration 1. It does not delete
`loop/<id>` when branch setup already created it. The Next paragraph names
that branch.

Preflight checks:

- `MaxIter` is at least 1.
- `exec.LookPath` finds the resolved pi binary.
- Every turn prompt is a regular file.
- Every gate is `loop:frozen` or an executable file. The bit is the check.
- Every `scorecard=` path parses, including a threshold the items cannot reach.
- No turn has both `verdict=` and `scorecard=`. The manifest parser already rejects this.
- `LOOP_SESSION` and `LOOP_COMPACT` are legal. `config.Load` already rejects a bad value.
- Each `LOOP_FREEZE` pattern matches at least one file. An empty sum in the snapshot is a miss.

Assurance `self-graded` or `verdict` warns at startup. It does not fail preflight.

A required gate whose process exits 126 or 127 stops the run on that
iteration. The same stop applies when the process never starts: `ENOENT`
(including a missing shebang interpreter) or `exec.ErrNotFound` is 127, and
`EACCES` is 126. `result: recipe`, exit 2. The Next paragraph names the gate,
the code, and the iteration. A normal failing test exits 1 and the loop
continues. The runner does not look for the text `command not found`. A test
can print that and still be a real red. A `required=0` gate that exits 127
does not stop the run.

`LOOP_STALL` is `stop` (default) or `continue`. `LOOP_STALL_PATHS` is an
optional space-separated list of workroot-relative paths.

At the end of iteration `i >= 2`, when that iteration is not ok, the runner
compares two strings with the previous iteration. `sig` is the required
failures, in step order. For each required gate that failed: the exit code
and the first non-empty output line. For each required scorecard that failed
or was unreadable: the name and the failing item ids, or `unreadable`.
Verdict text and turn errors are not part of `sig`. `tree` is three parts.
`git rev-parse HEAD`, then `git status --porcelain` with the same loop-dir
tolerance as branch setup, so `.loop/` is not progress, then the sha256 of
each `LOOP_STALL_PATHS` entry. A missing file is the token `missing`, not a
hash.

If the previous iteration was also not ok, and `sig` and `tree` both match,
the run stops. `result: stalled`, exit 1. Two identical no-change failures
are enough. An empty signature still matches, so two not-ok iterations that
failed without a required gate or scorecard stall. An ok iteration does not
arm the next empty signature. The Next paragraph names the signature and the
HEAD sha. A new commit changes HEAD, so it does not stall.
Porcelain does not list ignored paths. An ignored file is not progress unless
the recipe names it in `LOOP_STALL_PATHS`. An untracked file outside the loop
dir is porcelain, so a new one is progress. `LOOP_STALL=continue` turns the
stop off and the run reaches the cap.

The signature, the tree, and whether the iteration was not ok are written to
`state/<id>/stall.json` at the end of every finished iteration. Resume reads
that file once, into memory, so it does not grant two fresh iterations before
the same failure can stall. A git status error drops the in-memory snapshot
and removes `stall.json`, so the next comparison does not use a non-adjacent
baseline. The turn can edit `stall.json`. Stall is a cost control, not an
anti-cheat.
Freeze is the anti-cheat.

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
   [--approve]
   [--append-system-prompt <text>]
   [--no-context-files]
   --                     # end option parsing; context is not a flag
   @<prompt> [@<mend>] [@<brief>] [<context>…]
```

Stdin is `/dev/null`. Cwd is workroot. Parse stdout as jsonl. Write:

- `turn-<iter>-<name>.md` — every assistant message, joined with a blank line, `---`, and a blank line (for verdicts and humans)
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
- during a turn: last tool (`read foo.go`, `bash go test`) and elapsed
  time. Context percent is printed after the probe, not during the turn.
- per gate: pass/fail
- footer: success / failed-at-cap / done-no-objective, path to state

`-v` also prints extracted assistant text as it lands. `-q` prints one final
line: `<result>  iteration N/M  assurance <value>  return <path>`. `--json`
prints one machine event per line instead of the human view (runner events,
not pi's).

Do not use a spinner that fights with tool lines. While a pi process is
running, the runner rewrites the `status` file every 30 seconds from its own
clock. The tool name is the latest `tool_execution_start`, and it is omitted
when there has not been one. Event handlers update that name in memory and
do not write the file on each event.

### State

Same layout as v1, plus:

```
state/<id>/
  mend.md            # attached after the first iteration
  brief.md           # later turns of the current iteration
  handoff.md         # stub; not attached
  ledger.json        # settled lines, required to resume
  excerpts/          # gate tails, not inlined into the mend
  control            # optional, user/UI written
  status             # one live line; rewritten every 30s during a pi turn
  return.md          # the page a human reads on return
  stall.json         # previous iteration's failure signature and tree
  turn-*.jsonl
```

`state/CURRENT_RETURN` sits next to `state/CURRENT_ID` and holds the return
path relative to the workroot. `meta.env` keeps the v1 keys and adds
`RESULT`, `ASSURANCE`, and `RETURN` (absolute). `SUCCESS=1` only for
`success`. A no-objective run that finishes the cap exits 0 and stays
`SUCCESS=0` with `RESULT=done`.

The runner rewrites `return.md` at the start (`result: running`), after every
iteration, and on stop. `stalled` and `recipe` are written when those
stopping rules fire. `loop status` prints the heartbeat first, then
`return.md` in full. Exit 0 when `RESULT` is `running`, `success`, or `done`.
Exit 1 when it is `fail`, `stopped`, `stalled`, or `recipe`. Exit 2 when there
is no current run, the loop directory is missing, or the flags are bad. No
current run used to exit 1. A finished failure used to exit 0. Both changes
are deliberate.

## Templates

`loop init` scaffolds four recipes. New ones do not use `verdict=`. A legacy
verdict is a grep of the model's own prose. Do not scaffold a new one. The
check ranking, strongest first, is a test or build exit, an expected-value
script, a cross-model scorecard, a same-model scorecard, then that grep.
The compose skill writes the `.card` before the run. It tells the human to
come back to `loop status` and `return.md`. It does not put `pi` inside a
gate script. `fork` cuts to a new empty session and does not pass `pi --fork`.

- `until-green` (default). A writer turn and a shell gate. No scorecard.
  Convention-derived. The `loop.env` comment points at `return.md`. Enabling
  `loop:frozen` takes a manifest. A missing freeze index does not pass.
- `double-check`. A writer turn, then a read-only critic scorecard,
  `required=0`, `LOOP_MAX_ITER=1`, `LOOP_SESSION=none`. The card is `rule all`.
  The critic writes only the JSON path the runner names. It does not edit.
  There is no required check, so finishing the iteration is `done`, not a pass.
- `two-model-critique`. Writer, reviewer scorecard (`required=0`, `rule all`),
  fixer, tests gate. `LOOP_SESSION=none`. The fixer reads the brief the runner
  attached. Three model pins. The tests gate makes assurance `gated`. Unset
  pins are not cross-model. They are `self-graded` only when there is no
  required gate.
- `until-count`. A hunt turn and a `DONE` script. The script stays. It is the
  model declaring done, not a stronger check than a scorecard. `FINDINGS.md`
  is a normal untracked file, so a new finding changes the stall tree.
  Porcelain does not list ignored files.

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
- Do not treat a missing freeze index as success. Do not grade `loop:frozen`
  against the on-disk sums; those sit in the workroot the turn can edit.
- Do not put the runner on the user's PATH from this repo. They install it.
- Do not keep implementing `loop.sh` custom mode in v1. Manifest + one-shot
  flags are enough. Leave a comment in `run` that custom mode is deferred.
- Do not weaken a test or delete a fixture to get green.

## Implementation order

1. `internal/manifest`, `internal/config`, `internal/freeze`,
   `internal/control` — they have no subprocess and should go green first.
2. `internal/pi` against fake-pi and the jsonl fixtures.
3. `internal/session` (policy decisions). `internal/mend` writes the page
   the next turn actually reads.
4. `internal/ui` (render to a `io.Writer`; assert on plain output with color
   off).
5. `internal/run` + `cmd/loop` until `go test ./...` is green.

Commit as you go, on the branch the runner created.
