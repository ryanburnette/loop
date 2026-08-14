# loop

`loop` runs agentic loops with `pi`. You describe a loop in a directory —
prompts, gates, hooks — and `loop` runs it: act, check the result against
something more objective than the model's own opinion, feed the result back, and
repeat until a stopping rule fires.

It is the outer loop. The inner loop (think, act, observe) already runs inside
one `pi` turn; `loop` automates the outer one across turns. A loop is only as good
as its check, so the design pushes you toward exit-code gates and away from
self-grading.

This is the generic form of the pattern documented in
[research-loops](https://github.com/ryanburnette/research-loops) and first tried
in the gitaware recovery scripts. Ship four starter templates; write your own
when they don't fit.

## Quickstart

`loop` is one self-contained script. Put it on your PATH:

```sh
ln -s "$PWD/loop" ~/.local/bin/loop   # or: cp loop ~/.local/bin/loop
```

Copy the workhorse template into a repo and run it. The template expects a
`TASK.md` describing the goal.

```sh
cp -r templates/until-green ./myloop
echo 'Fix the CSV loader in internal/loader.go' > myloop/TASK.md
loop myloop
```

That's it. The loop runs inside the git repo that contains `myloop`, creates a
`loop/<id>` branch, calls `pi` to do the work, runs your tests as the gate, and
repeats until the tests pass or it hits the iteration cap. State and logs land in
`myloop/state/<id>/`.

To watch a loop with no objective gate (work, then a self-critique, then stop),
try the weakest template:

```sh
cp -r templates/double-check ./myloop
loop myloop
```

## How-to

### Make a new loop

Start from the closest template and edit three things:

```sh
cp -r templates/until-green ./myloop
```

1. **`loop.env`** — set `LOOP_MAX_ITER`, `LOOP_BRANCH`, and any `LOOP_<ROLE>_MODEL`.
2. **`prompts/*.md`** — the static text sent to `pi` for each turn.
3. **`gates/*.sh`** — executable scripts. Exit 0 means pass; anything else fails
   the iteration. The runner exports `LOOP_*` env vars (see Reference) so your
   scripts can read paths and the iteration number.

The `manifest` file orders the steps. You usually only change it to add or
reorder a gate or a reviewer turn.

### Change what counts as "done"

The gate is the check. Swap the gate script for whatever has a meaningful exit
code in your stack:

```sh
# gates/check.sh
#!/bin/sh
go build ./... && go vet ./... && go test ./...
```

```sh
# gates/check.sh  (a JS project)
#!/bin/sh
npx tsc --noEmit && npm test
```

Chain gates cheapest-first (typecheck, then lint, then tests) so bad turns die
before the expensive check runs. Put each behind its own `gate` line, or combine
them in one script with `&&`.

### Add a reviewer (two-model critique)

One model writes; a *different* model reviews with a hostile prompt; the writer
addresses the findings; tests are the hard gate. Different model families have
different blind spots, so cross-model critique catches more than self-review.

```sh
cp -r templates/two-model-critique ./myloop
```

Set the two models in `loop.env`:

```sh
LOOP_WRITER_MODEL=synthetic/hf:zai-org/GLM-5.2
LOOP_REVIEWER_MODEL=xai/grok-4.5
```

The reviewer's `VERDICT: PASS` is a soft gate (the model's own judgment); the
test gate is the hard one. Both must pass for an iteration to succeed. Keep the
verdict soft — it's a signal, not proof.

### Stop the loop from cheating the gate

A test-gate loop can "pass" by editing the tests. Freeze the files you don't want
touched, then add the built-in `loop:frozen` gate:

```sh
# loop.env
LOOP_FREEZE='*_test.go gates/check.sh'
```

```sh
# manifest, after the tests gate
gate frozen loop:frozen
```

At setup the runner hashes every file matching each `LOOP_FREEZE` pattern
(basename `find -name` match, excluding `.git` and the state dir). The `loop:frozen`
gate re-hashes and exits 1 on any change. A loop that "goes green" by weakening a
test now fails the frozen gate instead. Pair this with the prompt instruction "do
not modify the tests to make them pass" — the templates already include it.

### Run a loop in the background

Any loop with a hard stopping rule (an iteration cap, or a gate that must pass)
is safe to detach. Point it at a branch and log the output:

```sh
nohup loop myloop > ~/loops/myloop.log 2>&1 &
```

When you come back, read the diff and the gate log, not the model's summary. The
model is an optimistic narrator of its own work; the gate output is the truth.

### Resume a run

Iterations are atomic. Continue a stopped run from the next iteration:

```sh
loop myloop --resume <id>      # find the id in myloop/state/<id>/meta.env
```

Resume does not re-freeze (the baseline from the original run is the anti-cheat
point) and does not re-create the branch. This lets you inspect drift by resuming
after a model edits a frozen file.

### Go fully custom with `loop.sh`

When the per-iteration logic is too irregular for a manifest (conditional steps,
the gitaware-style commit-and-push-per-turn dance), drop a `loop.sh` in the loop
dir instead of `manifest`. The runner sources it and calls:

- `loop_setup` (optional) — once, before the loop
- `loop_iteration` (required) — each iteration; exit 0 = success, non-zero = keep going

It still applies `LOOP_MAX_ITER`, creates the branch and freezes if configured,
and exports the `LOOP_*` env vars. From `loop_iteration` you can call the runner's
own helpers (`fn_run_turn`, `fn_setup_branch`, `fn_freeze`, `fn_builtin_frozen`)
to run turns and gates.

```sh
# loop.sh (minimal custom loop)
loop_iteration() {
	fn_run_turn writer prompts/writer.md writer "" 1 ""   # name path model verdict required system
	go test ./... || return 1
	return 0
}
```

## Reference

### Commands

```
loop <dir> [--resume <id>]   run (or resume) a loop
loop freeze <dir>            (re)freeze LOOP_FREEZE patterns for a loop dir
loop frozen?                 check frozen hashes (uses LOOP_STATE_DIR; run as a gate)
loop help                    this message
```

### loop.env

All optional; defaults shown. Environment variables set on the command line
(`LOOP_MAX_ITER=2 loop myloop`) override `loop.env`, so one-off runs don't
require editing the file.

```sh
LOOP_MAX_ITER=5            # hard cap (always terminates the loop). Counts
                           # ITERATIONS (passes through the manifest), not turns.
LOOP_SESSION=shared        # shared (one session, all turns) | none (stateless)
LOOP_BRANCH=0              # 1 = create loop/<id> off LOOP_BRANCH_BASE before running
LOOP_BRANCH_BASE=main
LOOP_APPROVE=1             # pass --approve to pi (needed for headless runs)
LOOP_FREEZE=               # space list of find -name patterns to hash at setup
LOOP_CONTEXT=             # text appended to every turn (expanded once at startup)
LOOP_WRITER_MODEL=         # model for manifest role "writer" (empty = pi default)
LOOP_REVIEWER_MODEL=       # model for manifest role "reviewer"
```

Any `LOOP_<ROLE>_MODEL` var is the model for the manifest role `<role>`. Roles
are just names; `model=writer` in the manifest resolves to `LOOP_WRITER_MODEL`,
`model=critic` to `LOOP_CRITIC_MODEL`, and so on.

### The manifest

Line-based. Fields are whitespace-separated; `#` starts a comment line. Three
step types:

```
# type   name       path                  key=value ...
turn     writer     prompts/writer.md     model=writer
gate     precheck   gates/check.sh        required=0
hook     fmt        hooks/fmt.sh
turn     reviewer   prompts/reviewer.md   model=reviewer verdict=^VERDICT: PASS
gate     tests      gates/tests.sh
gate     frozen     loop:frozen
```

Step types:

- **`turn`** — a `pi -p` call with the prompt file (as `@<abs-path>`) and the
  context appended. Keys: `model=<role>`, `verdict=<regex>` (grep on the turn's
  stdout; match = PASS), `required=0|1` (default 1; a failed required verdict
  fails the iteration), `system=<text>` (appended to pi's system prompt).
- **`gate`** — run a script; exit 0 = pass. Key: `required=0|1` (default 1). A
  gate not required still runs and logs but can't fail the loop — use this for an
  informational pre-check. Path `loop:frozen` is a built-in gate.
- **`hook`** — run a script for side effects (gofmt, git commit); exit code
  ignored.

`verdict=` and `system=` may contain spaces — they consume the rest of the line,
so put them last on their line.

Paths in `path` resolve relative to the loop dir. Absolute paths pass through.
`loop:frozen` is the one built-in path (not a file).

### What counts as success

An iteration succeeds when every **required** gate exits 0 *and* every
**required** verdict matched. Then:

- A loop **with** an objective check (any required gate or required verdict) can
  succeed early and exit 0, or fail to converge and exit 1 at the cap.
- A loop **without** an objective check (the `double-check` pattern) just runs
  `LOOP_MAX_ITER` iterations of the manifest and exits 0. There's nothing to
  check, so there's nothing to fail.

This is the load-bearing rule from the lessons: a loop earns its keep only when
the check is grounded in something the model cannot talk its way around.

### Env exported to gates and hooks

```
LOOP_ID              unique run id
LOOP_ITERATION       1-based iteration number
LOOP_ROOT            the loop dir (absolute)
LOOP_WORKROOT        the git repo top-level (absolute)
LOOP_STATE_DIR       state/<id> (absolute)
LOOP_LOG             path to the shared gate-log.md
LOOP_PHASE           name of the step currently running
LOOP_LAST_TURN       stdout file of the most recent turn (for verdict-less checks)
LOOP_BRANCH_NAME     loop/<id> (when LOOP_BRANCH=1)
LOOP_BRANCH_BASE     the base branch
```

Gates append their own detail to `$LOOP_LOG`. `LOOP_LAST_TURN` lets a gate read
what the last turn actually produced instead of trusting a summary.

### State layout

```
state/<id>/
  meta.env             LOOP_ID, LOOP_BRANCH_NAME, STARTED_AT, BASE, SUCCESS, FINISHED_AT
  iteration            current iteration number
  gate-log.md          every gate and verdict result, appended
  turn-<iter>-<phase>.md       stdout of each turn
  turn-<iter>-<phase>.md.err   stderr of each turn
  frozen/              anti-cheat baselines (index, <n>.sum)
  sessions/            pi session files (shared mode)
state/CURRENT_ID       the most recent run's id
```

## Explanation

### The mental model: two loops

A single `pi` turn is already a small loop. The model thinks, calls a tool (read
a file, run a command), looks at the output, thinks again, and keeps going until
it decides it is done. That inner loop is called ReAct (reason + act), and the
harness runs it for you inside one turn.

`loop` is the same cycle pulled up one level. Instead of the model deciding when
a turn is done, *you* decide when the whole task is done, and you run the agent
as many turns as that takes. So there are two loops stacked: the inner one the
harness owns, the outer one `loop` owns.

When you "do a turn then double-check, then run it again," you are hand-running
the outer loop. `loop` moves that runtime out of your head and into a script.

### Why the check matters more than the prompt

A single long prompt ("do X, then verify, then fix, then run tests") asks the
model to *simulate* the loop in one turn. It has no hard checkpoint: if the model
convinces itself the tests pass, nothing contradicts it. A real outer loop puts a
non-negotiable gate between turns, where `go test` either exits 0 or it does not,
and the model does not get a vote.

That is the whole difference. Same model, same tools. The loop adds a gate the
model cannot argue with. This is why the runner ranks checks strongest to
weakest — exit code, then diff, then a different model, then self-grading — and
pushes you up that list.

### Why branch hygiene is on by default for mutating loops

A loop that changes source should run where a wrong answer costs nothing to
discard. `LOOP_BRANCH=1` makes a `loop/<id>` branch off your base branch and a
`backup/loop-<id>` safety branch before the first turn, so the loop proposes and
you dispose: you review the branch, then merge or throw it away. Never point a
mutating loop at a shared or production branch.

### When to use `loop.sh` instead of a manifest

The manifest is a pure ordered list. It handles the common shapes — a turn, a
gate, a hook, repeated — and it's inspectable at a glance. Reach for `loop.sh`
when the per-iteration logic needs conditionals, loops over a changing set, or
side effects interleaved with turns in a way a flat list can't express. The
gitaware recovery loop (commit-snapshot-per-turn, shared `CURRENT_ID` pointer,
run the same gate before and after the writer with different semantics) is the
canonical `loop.sh` case.

### Mapping to the research-loops lessons

| Lesson (research-loops)             | Here                              |
|-------------------------------------|-----------------------------------|
| outer loop you own, in shell         | `loop` runner + manifest          |
| two-model critique (pattern #4)     | `templates/two-model-critique/`   |
| soft rule + hard cap (guardrail #1) | verdict + `LOOP_MAX_ITER`         |
| exit-code gates (guardrail #2)       | `gate` steps                     |
| anti-cheat (guardrail #3)            | `LOOP_FREEZE` + `loop:frozen`     |
| background agent (guardrail #7)     | `nohup loop <dir>`                |
| read the diff, not the summary       | `state/<id>/` + `gate-log.md`     |
