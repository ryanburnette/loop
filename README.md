# loop

`loop` runs agentic loops with `pi`. You describe an outer loop — a prompt to
act on, something objective to check the result against, a way to feed the
result back — and `loop` repeats act → check → feedback until a stopping rule
fires. It exists because a model working alone is an optimistic narrator of
its own work: it reports success in the same voice whether or not it succeeded.
The fix is not a smarter model; it is a check the model cannot talk around,
applied every iteration, with the result handed back in writing.

A loop is a directory. Drop prompt files, a gate script, and a `loop.env` into
`.loop/` at the project root and run `loop run`. The runner calls `pi`, runs
the gate, writes down what happened, and loops. This document teaches the
concepts the runner is built on, each with a concrete example you can run.

## Build

```sh
go build -o ./bin/loop ./cmd/loop
```

There is no install target. Put the binary wherever you like.

## The core loop

One iteration of a loop is:

1. **Act.** Run a prompt through `pi`. The model edits code, runs tools, writes
   its summary.
2. **Check.** Run a gate: a script whose exit code the model does not vote on.
   When no script can decide, a judging turn fills a scorecard written before
   the run, and the runner applies the rule.
3. **Feed back.** The runner writes `mend.md` — the goal, the check, a short
   diff — and attaches it to the next iteration. It does not paste the
   transcript.
4. **Repeat** until a stopping rule fires: the required checks pass, or the
   iteration cap is reached.

The check has to be objective because the model's own summary is not. "I ran
the tests and they pass" is a claim the model makes confidently either way;
`go test ./...` exiting 0 is a number. When you come back, `loop status`
prints `state/<id>/return.md`. That page says whether it passed. The runner
does not merge.

The smallest loop is `until-green`, and `loop init` scaffolds it:

```
.loop/
  loop.env                  # config
  TODO.md                   # the goal
  prompts/01-writer.md      # the act
  gates/tests.sh            # the check
  state/                    # created at runtime
```

Everything a loop needs is in that one directory — config, goal, prompts,
gates, run state — and it stays out of the project's way on its own.
`loop init` writes a `.loop/.gitignore` of `*`, so the recipe hides itself
from `git status` without you editing the project's `.gitignore`. Commit it
deliberately (`git add -f .loop`) if a loop is meant to be shared.

No `manifest` is needed: the runner derives one by convention. Files in
`prompts/*.md` become turn steps (lexical order), files in `gates/` become
gate steps run after the turns, files in `hooks/` run last. A step's role
name is the filename with its extension and a leading `NN-` prefix stripped
(`01-writer.md` → `writer`).

```sh
loop init                  # scaffolds until-green in ./.loop
loop run                   # runs it
```

`loop.env` for `until-green`:

```
LOOP_MAX_ITER=5
LOOP_SESSION=none
LOOP_BRANCH=1
LOOP_BRANCH_BASE=main
LOOP_TEST_CMD=go test ./...
```

The gate is a shell script that exits nonzero on failure — the model cannot
argue with an exit code:

```sh
#!/bin/sh
set -eu
eval "${LOOP_TEST_CMD:-go test ./...}"
```

## Objective checks

Success is decided **once per iteration**, not per step. A loop has an
*objective* if any gate is required (the default), or any required turn has a
scorecard or a legacy verdict. A loop with an objective exits `0` on the first
passing iteration and `1` if the cap is spent without passing. A loop with no
objective runs `MaxIter` times and exits `0` with `result: done`. That is not
a pass. `SUCCESS=1` is written only for `success`.

New loops do not use `verdict=`. When a script cannot decide, write a
scorecard before the run and point a turn at it. The judge fills a JSON file
at the path the runner names. The runner applies the rule. `required=0` makes
a failed rule advice: it is logged, and it does not by itself end the loop.

**With an objective** (`until-green`): the `tests` gate is required. There is
no scorecard. The loop runs the writer, runs the tests, and if the tests fail
the iteration is marked failed. It tries again with the result in `mend.md`.
When the tests pass, the loop exits `0`. If they never pass within
`LOOP_MAX_ITER`, it exits `1`.

**Without an objective** (`double-check`): the critic scorecard is `required=0`
and there is no gate. A failed rule does not stop the loop. The loop runs once
(`LOOP_MAX_ITER=1`) and exits `0` with `result: done`, whatever the marks say.
Read `return.md`. Do not read that exit code as a pass.

One thing is decided per *step* rather than per iteration: if a turn's `pi`
call errors outright — the process dies, or exits nonzero — the runner logs it
to `gate-log.md`, abandons the rest of that iteration's steps, and marks the
iteration failed. That includes a `required=0` turn. A gate cannot vouch for
a turn that never ran.

`scorecard=` is one path token, so `required=0` may sit on either side:

```
turn critic prompts/02-critic.md model=critic required=0 scorecard=scorecards/critic.card
```

A line with both `verdict=` and `scorecard=` is a parse error.

`verdict=` remains for recipes that already have it. It is a regex matched
line-anchored (the runner prepends `(?m)`), so `^VERDICT: PASS\b` matches a
line starting with `VERDICT: PASS` and the `\b` stops `VERDICT: PASSED` from
satisfying it. It is a grep of the model's own prose. Do not scaffold a new
one. `verdict=` and `system=` consume the rest of the line, so `required=0`
must come *before* `verdict=`.

## The four patterns

`loop init <template>` scaffolds one of four. The check ranking, strongest
first, is a test or build exit, an expected-value script, a cross-model
scorecard, a same-model scorecard, then a legacy verdict grep. `until-count`
is the model declaring done. It is not a stronger check than a scorecard.

### until-green — `loop init until-green` (default)

The workhorse. Writer turn, then the test gate, iterating until green or the
cap. No scorecard. The check is your test suite, an exit code the model cannot
argue with. Convention-derived (no manifest): `prompts/01-writer.md` → turn,
`gates/tests.sh` → gate. Change `LOOP_TEST_CMD` to retarget it to any command
with a meaningful exit code: `npm test`, `pytest -q`, `cargo test`,
`tsc --noEmit`, `terraform validate`. Come back to `loop status` and
`return.md`. The template comment points at that page.

A missing freeze index does not pass. `LOOP_FREEZE` only records hashes until
a manifest adds `gate frozen loop:frozen`. The check fails closed.

### double-check — `loop init double-check`

A review aid. The writer does the work, then a read-only critic fills a
`rule all` scorecard. `required=0`, `LOOP_MAX_ITER=1`, `LOOP_SESSION=none`.
The critic writes only the JSON path the runner names. It does not edit.
There is no shell gate, so finishing the iteration is `done`, not a pass.
Use it when there is no test yet, and graduate to `until-green` when you can
name one. If you drop `required=0` and the model pins are unset or the same,
the runner labels the loop `self-graded`.

```
turn writer   prompts/01-writer.md   model=writer
turn critic   prompts/02-critic.md   model=critic required=0 scorecard=scorecards/critic.card
```

### two-model-critique — `loop init two-model-critique`

One model writes, a reviewer fills a scorecard and does not edit, the fixer
reads the brief the runner attached, then the test suite is the hard gate.
`LOOP_SESSION=none`. The reviewer's scorecard is soft (`required=0`). The
tests are the objective, so assurance is `gated` even when the three model
pins are unset. Unset pins are not cross-model. Set `LOOP_WRITER_MODEL` and
`LOOP_REVIEWER_MODEL` to different non-empty ids before anyone reads a
scorecard-only variant as `cross-model`. `LOOP_FIXER_MODEL` is the third pin.

```
turn writer     prompts/01-writer.md    model=writer
turn reviewer   prompts/02-reviewer.md  model=reviewer required=0 scorecard=scorecards/review.card
turn fixer      prompts/03-fixer.md     model=fixer
gate tests      gates/tests.sh
```

### until-count — `loop init until-count`

Discovery work: the goal is "find N things" (bugs, edge cases, missing test
cases), not "make the tests pass." Each turn hunts for one more and appends it
to a findings file. The gate greps for a lone `DONE` line. That line is the
model declaring done. The cap is the backstop. Do not rank this pattern above
a scorecard. Assurance is `gated` because the script is a required gate, and
that does not mean the findings were tested.

`FINDINGS.md` is a normal untracked file, so a new finding changes the stall
tree. Porcelain does not list ignored files. A hunt that changes nothing, and
whose `DONE` gate keeps failing the same way, does stall.

```sh
#!/bin/sh
set -eu
f="${LOOP_FINDINGS:-FINDINGS.md}"
grep -qx DONE "$f"
```

## Compaction: avoid, detect, react

`pi` can auto-compact a session when the context window fills, replacing the
conversation with a model-written summary. That is lossy for a loop. A loop
depends on a constraint, a failed gate, or the original goal staying in
context. A compacted session is the optimistic narrator writing the history it
will read next: it will summarize "the tests were failing, I am fixing the
loader" into something that loses the exact failure, and then narrate success
against the summary. So the runner's job is to **avoid needing compaction**,
and to **refuse to pretend a compacted session is fine**.

**Avoid.**

- *Prefer `none` for gate-driven loops.* `until-green` does not need history.
  Each turn is a fresh `pi --no-session`. After the first iteration the prompt
  plus `mend.md` is the context. Later turns in that iteration also get
  `brief.md`. There is nothing to compact.
- *Cap turns per shared session.* `LOOP_SESSION_TURNS` (default 4). After that
  many turns, start a new empty session and attach the mend, and on a later
  turn the brief. Do not pass `pi --fork`.
- *Re-feed the check every time.* The mend records the gate exit, the scorecard
  result, and a short tail. The model does not have to remember `go test`
  failed; it is told.
- *Do not pull the world into context.* `LOOP_NO_CONTEXT_FILES=1` passes
  `--no-context-files` so a large instruction tree is not loaded on every turn.
- *Use the big windows.* The models this stack has are 500k–1M tokens. Do not
  design as if the window were 32k.

**Detect.** `pi --mode json` emits `compaction_start` / `compaction_end` and
`contextUsage` events. The runner records context percent on the live status
line and notes when a compaction event fires.

**React.** `LOOP_COMPACT` (`fail` | `warn` | `allow`, default `warn`):

- `fail` — a compaction event fails the turn. The next turn opens a new empty
  session with the mend attached.
- `warn` — log it, and still cut to a new session.
- `allow` — no warning, and the turn is not failed for compaction. The session
  still cuts. Exists for debugging; do not default to it.

The runner never calls `pi`'s compact command and never continues a compacted
shared session as if the summary were the work. It does not pass `pi --fork`
to copy that summary.

### Session policies: `none` | `shared` | `fork`

- `none` (default): each turn is a fresh `--no-session`. Continuity comes from
  `mend.md` and git history, not conversation memory. Right for almost every
  gate-driven loop. The init templates use this.
- `shared`: turns inside one iteration share one `--session-id`. After
  `LOOP_SESSION_TURNS` turns, or if a compaction is detected, the runner
  starts a new empty session. Opt in when you want that transcript. The
  iteration boundary drops the id.
- `fork`: like `shared`, plus a cut to a new empty session when context
  percent reaches `LOOP_FORK_PERCENT` (default 40). The runner does not pass
  `pi --fork`. That flag would copy the transcript. The same kind of cut
  happens at the turn cap or on compaction. The new session gets `mend.md`,
  and the brief when this is not the first turn of the iteration.

## The mend is the source of truth, not model memory

At the end of each iteration the runner writes `state/<id>/mend.md` and
attaches it on every turn after the first. Later turns in the same iteration
also get `brief.md`, so a fixer sees the scorecard without sharing a pi
session. `handoff.md` in that directory is a stub. The runner does not attach
it, the previous session, or the raw gate log.

The runner writes the mend. The model does not. That is what makes
`LOOP_SESSION=none` safe: a fresh turn still receives the goal, the last
check, and the diff in writing. Settled lines are claims the runner committed.
Facts win. The page you open when you come back is `return.md`, which
`loop status` prints.

`TODO.md` lives in `.loop/` with the rest of the recipe, and like the rest of
it, it is operator scratch — today's objective, your wording, your priorities.
Gitignore `.loop/` and the whole setup goes with it. The runner reads the goal
off disk and never expects it to be tracked.

## Freeze / anti-cheat

A test-gate loop has an obvious exploit: edit the tests until they pass.
Models do this. `LOOP_FREEZE` plus the built-in `loop:frozen` gate close it.

`LOOP_FREEZE` is a space-separated list of basename globs (`*_test.go`, not
paths — matching is against the file's base name). At run start the runner
hashes every matching file in the workroot and stores the hashes. The
`loop:frozen` gate re-hashes each iteration and fails on drift, so if the loop
weakens a test to pass, the frozen gate catches it. A missing `frozen/index`
does not pass. The check fails closed.

```
LOOP_FREEZE=*_test.go
```

Because `until-green` is convention-derived (no manifest), enforcing the
frozen gate means writing a manifest that carries the derived steps then the
frozen gate:

```
turn writer prompts/01-writer.md model=writer
gate tests  gates/tests.sh
gate frozen loop:frozen
```

Freeze the tests, the fixtures, and anything that defines "done." Snapshot is
taken once at run start; resume does not re-freeze, it compares against the
original snapshot.

## Disposable branches

`LOOP_BRANCH=1` keeps the loop off your working tree. The runner creates
`loop/<id>` off `LOOP_BRANCH_BASE` (default `main`) and a safety
`backup/loop-<id>` branch, and refuses a dirty tree. An untracked `.loop/` is
never what it refuses on — that is the recipe, not your work — and this holds
for a one-shot too, whose own recipe lives in a temp directory but which still
ignores a `.loop/` it finds in the project. Review the branch and merge, or
throw it away. The loop proposes; you dispose.

`until-green` and `two-model-critique` set `LOOP_BRANCH=1`. `double-check` and
`until-count` set `0` so they look at the tree you already have. Set
`LOOP_BRANCH=1` before you leave one of those unattended. A one-shot defaults
to `LOOP_BRANCH=1` too. `--approve` defaults on, so a loop is an auto-approved
agent with write access to the repo, and that belongs on disposable ground.
`--branch=false` overrides it when you mean to run against the current tree.
A one-shot also bases its branch on the current commit (`HEAD`) rather than
the default `main`, so `loop run --prompt F --gate C` works on a repo whose
trunk is `master` or `develop` without a `--base` flag; a regular loop still
defaults to `main`.

## Bounded retries

The iteration cap (`LOOP_MAX_ITER`, default 5) is the hard backstop. Always
have one, even alongside a gate, so a loop that never converges still
terminates. If one action keeps failing the same way, more iterations rarely
fix it — the failure usually means something systemic the turns cannot touch.
Keep the cap low (2–5 for fix-loops, up to ~10 for discovery).

## The control plane

Between steps, if `state/<id>/control` exists, the runner reads it, truncates
it, and applies it:

```
pause               # block until resume
resume
stop                # fail the run, exit 1
set KEY=VALUE       # overlay config: LOOP_MAX_ITER, LOOP_SESSION, models, …
```

`set` overlays the resolved config for the rest of the run — bump the cap,
swap a model, switch session policy. `SIGINT` / `SIGTERM` are treated as
`stop`: the in-flight subprocess is killed, the run writes `SUCCESS=0`, and
the final line is `STOPPED`. This file is the hook a future interactive UI
writes to; v1 ships the file interface, not the UI.

## How to use it

`loop` operates on `.loop/` in the current directory, like `git` or `pi`. Use
`-C DIR` to target a different project from elsewhere: `DIR` may be the loop
dir itself (`.../proj/.loop`) or the project directory that contains it
(`.../proj`); in the second case `DIR/.loop` is used. There is no upward
search for `.loop/`. Flags may appear before or after `-C`.

```
loop init [template] [-C DIR]  scaffold .loop/ (until-green is default)
loop run [flags]              run .loop/ in the current directory
loop run -C DIR [flags]       run a specific project's .loop/
loop run --prompt F --gate C  one-shot, no .loop/ needed
loop status [-C DIR]          show the current run
loop freeze [-C DIR]          snapshot frozen files for manual inspection
loop frozen?                  check a freeze snapshot (env-driven)
loop help
loop version
```

Run flags:

```
-C DIR               project or loop directory (default ./.loop)
--max-iter N         override LOOP_MAX_ITER
--session MODE       none|shared|fork (default none)
--branch             create loop/<id> branch (--branch=false overrides loop.env)
--base BRANCH        LOOP_BRANCH_BASE
--approve            pass --approve to pi (default true)
--context TEXT       extra context string
--model role=id      pin a model to a role (repeatable)
--compact MODE       fail|warn|allow
--pi PATH            pi binary
--resume ID          resume a run
--prompt FILE        one-shot prompt file (no dir needed)
--gate CMD|PATH      one-shot gate command or script
-v                   verbose (stream assistant text)
-q                   quiet (one final line, including the return path)
--json               machine events, one JSON object per line
-V, version          print version
```

While a run is in progress, and after it finishes, `loop status` prints the
heartbeat line from `state/<id>/status` first, then `state/<id>/return.md` in
full, then the run id, the iteration, and `meta.env`. The heartbeat is
rewritten every 30 seconds while a pi process is running, from the runner's
clock, not from the next pi event. The tool name is the latest
`tool_execution_start` and is omitted when there has not been one.

`return.md` is the page to open when you come back. `result` is one of
`running`, `success`, `fail`, `stopped`, `done`, `stalled`, or `recipe`.
`meta.env` keeps the older keys and adds `RESULT`, `ASSURANCE`, and `RETURN`
(the absolute path). `SUCCESS=1` is written only for `success`. A loop that
reaches the cap with no required check still exits 0 and stays `SUCCESS=0`,
with `RESULT=done`. Do not read `SUCCESS` as the assurance label.

`loop status` exit codes changed in 0.4.0. Both changes are deliberate; neither
is the old behavior:

- No current run now exits 2. It used to exit 1. A missing loop directory and
  bad flags were already 2 and still are.
- A finished run whose `RESULT` is `fail`, `stopped`, `stalled`, or `recipe`
  now exits 1. It used to exit 0 along with every other finished run.
  `running`, `success`, and `done` exit 0. A run that is still in progress
  stays `RESULT=running` and exits 0 even when the last finished iteration
  was red.

`-q` prints one final line on stdout. The line names the result, the iteration,
the assurance, and the return path:

```text
fail  iteration 4/8  assurance gated  return .loop/state/<id>/return.md
```

Resume a stopped or failed run by id:

```sh
loop run --resume 20260816T211458Z-58884
```

Resume does not re-freeze: it compares against the snapshot taken when the run
started. A one-shot `loop run --prompt F --gate C` builds a scratch loop dir
in a temp directory so your workroot is never dirtied; the summary prints the
absolute state path so you can inspect it afterward.

Settings resolve in one order: built-in defaults, then `loop.env`, then the
process environment, then flags. Env beating the file is deliberate — a
one-off should not require editing the recipe — but it is easy to forget an
exported `LOOP_MAX_ITER`, so the runner warns on startup whenever the two
disagree and names the key. Unknown `loop.env` keys and unrecognized manifest
keys are warned about too: they are still passed through to gates and hooks,
but they are not runner settings, and a typo would otherwise be silent.

## Tests

`go test ./...` is the gate. It uses `testdata/fake-pi`, never a real model.

## License

MIT. See `LICENSE`.
