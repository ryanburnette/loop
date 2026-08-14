# loop

A small POSIX shell runner for agentic loops with `pi`. You describe a loop in a
directory — prompts, gates, hooks — and `loop` runs it: act, check against
something more objective than the model's own opinion, feed back, repeat until a
stopping rule fires.

It is the outer loop from the [research-loops](../research-loops) lessons, made
generic. The inner loop (think/act/observe) already runs inside one `pi` turn;
this tool automates the outer one across turns. A loop is only as good as its
check, so the design pushes you toward exit-code gates and away from
self-grading.

## Install

`loop` is one self-contained script. Put it on your PATH:

```sh
ln -s "$PWD/loop" ~/.local/bin/loop   # or cp loop ~/.local/bin/loop
```

## Quick start

Copy a template into a repo and run it:

```sh
cp -r templates/until-green ./myloop
cd myloop
echo 'Fix the CSV loader in internal/loader.go' > TASK.md
loop .                         # runs until tests green or the cap fires
```

Templates, weakest check to strongest:

- `templates/double-check/` — work, then a hostile self-review. No hard gate.
- `templates/until-green/` — writer turn, then the test suite is the gate. The workhorse.
- `templates/two-model-critique/` — one model writes, a *different* model reviews, writer fixes, tests gate.
- `templates/until-count/` — find N things; stops when the model writes `DONE` or the cap fires.

## A loop directory

```
myloop/
  loop.env     config (models, caps, session, branch, freeze, context)
  manifest     ordered steps: one per line of  turn | gate | hook
  loop.sh       optional: full-custom mode (sourced; defines setup/iteration/success)
  prompts/      *.md prompt files (static text)
  gates/        executable scripts; exit 0 = pass
  hooks/        executable scripts; side effects, exit ignored
```

A loop runs **inside the git repo that contains its directory** (the workroot is
that repo's top-level). Loops that mutate source should set `LOOP_BRANCH=1` so the
runner creates a `loop/<id>` branch off `LOOP_BRANCH_BASE` (default `main`) and a
`backup/loop-<id>` safety branch first.

## loop.env

All optional; defaults shown.

```sh
LOOP_MAX_ITER=5            # hard cap (always terminates the loop)
LOOP_SESSION=shared        # shared (one session, all turns) | none (stateless turns)
LOOP_BRANCH=0              # 1 = create loop/<id> off LOOP_BRANCH_BASE before running
LOOP_BRANCH_BASE=main
LOOP_APPROVE=1             # pass --approve to pi (needed for headless runs)
LOOP_FREEZE=               # space list of find -name patterns to hash at setup (anti-cheat)
LOOP_CONTEXT=             # text appended to every turn (expanded once at startup)
LOOP_WRITER_MODEL=         # model for role "writer" (empty = pi default)
LOOP_REVIEWER_MODEL=       # model for role "reviewer"
```

Environment variables set on the command line (e.g. `LOOP_MAX_ITER=2 loop .`)
override `loop.env`, so one-off runs don't require editing the file. Any
`LOOP_<ROLE>_MODEL` var is the model for the manifest role `<role>`.

## The manifest

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

- **`turn`** — a `pi -p` call with the prompt file (as `@<abs-path>`) and the context appended. Keys: `model=<role>`, `verdict=<regex>` (grep on the turn's stdout; match = PASS), `required=0|1` (default 1; a failed required verdict fails the iteration), `system=<text>` (appended to pi's system prompt).
- **`gate`** — run a script; exit 0 = pass. Key: `required=0|1` (default 1). A gate not required still runs and logs but can't fail the loop — use this for an informational pre-check. Path `loop:frozen` is a built-in gate (see anti-cheat).
- **`hook`** — run a script for side effects (gofmt, git commit); exit code ignored.

`verdict=` and `system=` may contain spaces — they consume the rest of the line,
so put them last on their line.

### What counts as success

An iteration succeeds when every **required** gate exits 0 *and* every
**required** verdict matched. If a loop has no required gate and no required
verdict (the `double-check` pattern), there is no objective check — the runner
just runs all `LOOP_MAX_ITER` turns and exits 0. Only loops with an objective
check can "succeed early" or "fail to converge" (exit 1 at the cap).

This is the load-bearing rule from the lessons: a loop earns its keep only when
the check is grounded in something the model cannot talk its way around.

## Env exported to gates and hooks

```
LOOP_ID LOOP_ITERATION LOOP_ROOT LOOP_WORKROOT LOOP_STATE_DIR LOOP_LOG
LOOP_PHASE LOOP_LAST_TURN LOOP_BRANCH_NAME LOOP_BRANCH_BASE
```

`LOOP_LAST_TURN` is the stdout file of the most recent turn (for verdict-less
checks). `LOOP_LOG` is the shared `gate-log.md` (append your own detail there).

## Anti-cheat (the `loop:frozen` gate)

A test-gate loop has an obvious exploit: edit the tests until they pass. The
runner can hash files at setup and detect drift:

```sh
# loop.env
LOOP_FREEZE=*_test.go

# manifest, after the tests gate
gate frozen loop:frozen
```

At setup the runner `sha256`s every file matching each `LOOP_FREEZE` pattern
(basename `find -name` match, excluding `.git` and the state dir) into
`state/<id>/frozen/`. The `loop:frozen` gate re-hashes and exits 1 on any
change. A loop that "goes green" by weakening a test now fails the frozen gate
instead. Pair this with the prompt instruction "do not modify the tests to make
them pass" (the templates already include it).

## Custom mode: `loop.sh`

If a loop's per-iteration logic is too irregular for a manifest, drop a `loop.sh`
in the dir instead of `manifest`. The runner sources it and calls:

- `loop_setup`     — once, before the loop (optional)
- `loop_iteration` — each iteration; exit 0 = success, non-zero = keep going

It still applies `LOOP_MAX_ITER`, creates the branch and freezes if configured,
and exports all the `LOOP_*` env vars. `loop_iteration` can call the runner's
own helpers (`fn_run_turn`, `fn_setup_branch`, `fn_freeze`, `fn_builtin_frozen`)
for turn/gate execution.

## Resume

Iterations are atomic. Resume from the next iteration:

```sh
loop . --resume <id>        # id is in state/<id>/meta.env, or state/CURRENT_ID
```

Resume does **not** re-freeze (the baseline from the original run is the
anti-cheat point) and does **not** re-create the branch.

## Background agent

Any loop with a hard stopping rule is safe to detach (Hashimoto's "always have an
agent running"):

```sh
nohup loop . > ~/loops/myloop.log 2>&1 &
```

Read the diff and the gate log when you come back, not the model's summary.

## State

```
state/<id>/
  meta.env  iteration  gate-log.md
  turn-<iter>-<phase>.md  turn-<iter>-<phase>.md.err
  frozen/         # anti-cheat baselines
  sessions/       # pi session files (shared mode)
state/CURRENT_ID
```

## Mapping to the lessons

| Lesson (research-loops)            | Here                              |
|------------------------------------|-----------------------------------|
| outer loop you own, in shell       | `loop` runner + manifest          |
| two-model critique (pattern #4)    | `templates/two-model-critique/`   |
| soft rule + hard cap (guardrail #1)| verdict + `LOOP_MAX_ITER`          |
| exit-code gates (guardrail #2)     | `gate` steps                      |
| anti-cheat (guardrail #3)          | `LOOP_FREEZE` + `loop:frozen`     |
| background agent (guardrail #7)     | `nohup loop .`                    |
| read the diff, not the summary      | `state/<id>/` + `gate-log.md`     |
