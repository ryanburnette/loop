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
2. **Check.** Run a gate — a script whose exit code is the verdict. The model
   gets no vote on whether the gate passed.
3. **Feed back.** Write the gate result, the diff, and the goal into a handoff
   file and attach it to the next turn.
4. **Repeat** until a stopping rule fires: the gate passes, or the iteration
   cap is reached.

The check has to be objective because the model's own summary is not. "I ran
the tests and they pass" is a claim the model makes confidently either way;
`go test ./...; echo $?` is a number. The loop's job is to turn the number
into the next prompt.

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

## Objective gates vs. soft verdicts

Success is decided **once per iteration**, not per step. A loop has an
*objective* if any gate is required (the default) or any turn carries a
required verdict. A loop with an objective exits `0` on the first passing
iteration and `1` if the cap is spent without passing. A loop with no
objective runs `MaxIter` times and exits `0`.

This distinction is the whole shape of a loop, so see both.

**With an objective** (`until-green`): the `tests` gate is required. The loop
runs the writer, runs the tests, and if the tests fail the iteration is marked
failed. It tries again, handing the test output back to the writer. When the
tests pass, the loop exits `0`. If they never pass within `LOOP_MAX_ITER`, it
exits `1`.

**Without an objective** (`double-check` with only a soft critic): the critic
turn carries `verdict=^VERDICT: PASS\b` but `required=0`, and there is no gate.
A failed verdict does not stop the loop — nothing does, except the cap. The
loop runs once (`LOOP_MAX_ITER=1`) and exits `0` regardless of the verdict.
The verdict is recorded in `gate-log.md` as advice, not as a stopping rule.

One thing is decided per *step* rather than per iteration: if a turn's `pi`
call errors outright — the process dies, or exits nonzero — the runner logs it
to `gate-log.md`, abandons the rest of that iteration's steps, and marks the
iteration failed. A gate cannot vouch for a turn that never ran, so it is not
given the chance to.

A verdict is a regex matched line-anchored (the runner prepends `(?m)`), so
`^VERDICT: PASS\b` matches a line starting with `VERDICT: PASS` and the `\b`
stops `VERDICT: PASSED` from satisfying it. Make a verdict `required` (drop
`required=0`) and it becomes a hard, blocking check; leave it `required=0` and
it is a soft signal that does not stop anything.

In a manifest, `verdict=` and `system=` consume the rest of the line, so
`required=0` must come *before* `verdict=`:

```
turn critic prompts/02-critic.md model=critic required=0 verdict=^VERDICT: PASS\b
```

## The four patterns

`loop init <template>` scaffolds one of four. They are ordered weakest check
to strongest.

### until-green — `loop init until-green` (default)

The workhorse. Writer turn, then the test gate, iterating until green or the
cap. The check is your test suite — an exit code the model cannot argue with.
Convention-derived (no manifest): `prompts/01-writer.md` → turn,
`gates/tests.sh` → gate. Change `LOOP_TEST_CMD` to retarget it to any command
with a meaningful exit code: `npm test`, `pytest -q`, `cargo test`,
`tsc --noEmit`, `terraform validate`.

### double-check — `loop init double-check`

The weakest gate. Two turns: the writer does the work, then a hostile critic
reviews it on a fresh turn. The critic's verdict is soft (`required=0`), there
is no hard gate, and the loop exits `0` after one pass. Treat its "looks good"
with suspicion; use it when there is no test to run yet, and graduate to
`until-green` when you can name an objective check.

```
turn writer   prompts/01-writer.md   model=writer
turn critic   prompts/02-critic.md   model=critic required=0 verdict=^VERDICT: PASS\b
```

### two-model-critique — `loop init two-model-critique`

One model writes, a *different* model reviews with a hostile prompt, the
writer addresses the findings, then the test suite is the hard gate. The
reviewer's verdict is soft; the tests are the objective. Different model
families have different blind spots, so cross-model critique catches more than
either reviewing itself. Pin two models with `LOOP_WRITER_MODEL` and
`LOOP_REVIEWER_MODEL`.

```
turn writer     prompts/01-writer.md    model=writer
turn reviewer   prompts/02-reviewer.md  model=reviewer required=0 verdict=^VERDICT: PASS\b
turn fixer      prompts/03-fixer.md     model=fixer
gate tests      gates/tests.sh
```

### until-count — `loop init until-count`

Discovery work: the goal is "find N things" (bugs, edge cases, missing test
cases), not "make the tests pass." Each turn hunts for one more and appends it
to a findings file. The loop succeeds when the model writes `DONE` on its own
line (a soft rule the model decides), and the iteration cap is the hard
backstop. The gate greps the findings file for a lone `DONE`:

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
  Each turn is a fresh `pi --no-session`. The prompt plus the handoff is the
  whole context. There is nothing to compact.
- *Cap turns per shared session.* `LOOP_SESSION_TURNS` (default 4). After that
  many turns in one session, start a new session and attach the handoff.
- *Re-feed the check every time.* The last gate log is copied into the handoff
  (truncated if huge). The model does not have to remember `go test` failed;
  it is told.
- *Do not pull the world into context.* `LOOP_NO_CONTEXT_FILES=1` passes
  `--no-context-files` so a large instruction tree is not loaded on every turn.
- *Use the big windows.* The models this stack has are 500k–1M tokens. Do not
  design as if the window were 32k.

**Detect.** `pi --mode json` emits `compaction_start` / `compaction_end` and
`contextUsage` events. The runner records context percent on the live status
line and notes when a compaction event fires.

**React.** `LOOP_COMPACT` (`fail` | `warn` | `allow`, default `warn`):

- `fail` — a compaction event fails the turn. The next iteration starts a new
  session with a full handoff.
- `warn` — log it, force the next turn onto a new session, keep going.
- `allow` — do nothing. Exists for debugging; do not default to it.

The runner never calls `pi`'s compact command and never continues a compacted
shared session as if the summary were the work.

### Session policies: `none` | `shared` | `fork`

- `none` (default): each turn is a fresh `--no-session`. Continuity comes from
  the handoff and git history, not conversation memory. Right for almost every
  gate-driven loop.
- `shared`: turns share one `--session-id`. After `LOOP_SESSION_TURNS` turns,
  or if a compaction is detected, the runner starts a new session. Fine for
  short loops (`double-check` is two turns); wrong for anything that might run
  to the cap.
- `fork`: like `shared`, but the runner `--fork`s onto a new session when
  context usage crosses `LOOP_FORK_PERCENT` (default 40), and starts a fresh
  session at the turn cap or on compaction. History that still matters has
  been written down by the runner, not summarized by the model.

## The handoff is the source of truth, not model memory

At the end of each iteration the runner writes `state/<id>/handoff.md` and
attaches it as `@handoff.md` on every turn after the first. Session memory is a
convenience; the handoff is what the next turn actually relies on. It carries,
in order:

1. The goal — the first non-heading line of `.loop/TODO.md`, else
   `LOOP_CONTEXT`.
2. Constraints copied from `.loop/CONSTRAINTS.md`, if present.
3. The last gate's name, status, and the tail of its log.
4. `git diff --stat` of the workroot.
5. Session facts: policy, turns this session, last context percent, whether a
   compaction fired.
6. Frozen status: `ok`, `drift`, or `not configured`.

The runner writes this file; the model is never asked to. That is what makes
`LOOP_SESSION=none` safe: a fresh-session turn still receives the goal, the
last failure, and the diff in writing.

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
weakens a test to pass, the frozen gate catches it.

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

Every `loop init` template sets `LOOP_BRANCH=1`, and so does a one-shot run —
`--approve` defaults on, so a loop is an auto-approved agent with write access
to the repo, and that belongs on disposable ground. `--branch=false` overrides
it when you mean to run against the current tree. A one-shot also bases its
branch on the current commit (`HEAD`) rather than the default `main`, so
`loop run --prompt F --gate C` works on a repo whose trunk is `master` or
`develop` without a `--base` flag; a regular loop still defaults to `main`.

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
-q                   quiet (final line only)
--json               machine events, one JSON object per line
-V, version          print version
```

While a run is in progress, `loop status` prints the run id, iteration, status
line, and `meta.env`. Resume a stopped or failed run by id:

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
