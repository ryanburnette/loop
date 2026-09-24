---
name: loop-compose
description: Compose or modify a .loop/ directory so the `loop` CLI can run pi in an agentic loop. Use when the user wants to set up an automated write→check→repeat loop over a codebase — e.g. "make the tests pass", "have a second model review my changes", "hunt for bugs until done".
---

# loop-compose

You are composing a `.loop/` recipe for the `loop` CLI (a Go binary on PATH,
same shape as `git` or `pi`). `loop` runs pi in a write→check→feedback→repeat
cycle until a stopping rule fires. Your job is to pick the right pattern and
scaffold it, not to hand-write `loop.env` and prompt files from scratch.

## What a .loop/ directory is

Everything needed to set up a loop lives in one directory. `loop init` writes
`.loop/.gitignore` containing `*`, so the recipe hides itself. Call `loop`
from your PATH.

```
.loop/
  loop.env            # KEY=VALUE config (never sourced as a shell script)
  manifest            # OPTIONAL — omit it to derive steps from file names
  TODO.md             # the goal; first non-heading line is copied into mend.md
  CONSTRAINTS.md      # OPTIONAL — standing rules copied into the mend
  prompts/
    01-writer.md      # numbered prompt files → turn steps, in lexical order
    02-reviewer.md
  gates/
    tests.sh          # any executable → a gate step, run after all turns
  hooks/
    notify.sh         # any executable → a hook step, run last
  scorecards/
    review.card       # items written before the run; a judging turn fills JSON
  state/              # created at runtime; return.md is the page to open
```

If there is no `manifest`, the runner derives one: `prompts/*.md` become turn
steps, `gates/*` become gate steps, `hooks/*` become hook steps — all required,
sorted lexically by filename. A step's role name is the filename with its
extension and a leading `NN-` numeric prefix stripped (`01-writer.md` →
`writer`). Use numbered files for the common case; write a `manifest` only when
you need interleaving (turn, gate, turn), a scorecard, or `system=`. A
scorecard exists only when a manifest names it. The runner does not invent one
from a filename.

`loop` operates on `.loop/` in the current directory. `loop run` with no
arguments runs `.loop/`. `loop run -C /path/to/project` runs the `.loop/` in
that project (you may also name the loop dir directly, e.g.
`-C /path/to/project/.loop`). There is no upward search.

## Before you scaffold: ask the user these questions

You cannot pick a pattern without knowing the check. Ask, in this order:

1. **What is the goal, in one sentence?** (This becomes the first non-heading
   line of `.loop/TODO.md`. The runner copies it into `mend.md`.)
2. **What is the check — how will we know the loop succeeded?** This is the
   load-bearing question. The options, strongest to weakest:
   - Exit code of a test, build, typecheck, or lint. The model has no vote.
   - A script that compares output to an expected value. Still a gate.
   - A scorecard whose resolved model differs from every acting turn, and
     none of those strings is empty. Not a `VERDICT:` line.
   - A scorecard judged by the same model that acted, or by a model the
     runner cannot tell from pi's default.
   - A legacy `verdict=` grep of the model's own prose. Weakest. Do not
     scaffold a new one.
3. **How many iterations should it be allowed before it gives up?** (Always
   have a cap.)
4. **Should it work on a throwaway branch?** (Almost always yes —
   `LOOP_BRANCH=1` keeps an unattended loop off the working tree.)

`until-count` is not on that list as a stronger check. Its `DONE` line is the
model declaring done. The cap is the backstop.

## Pick the pattern

`loop init <template>` scaffolds one of four. Match the user's check to one:

| The user's check is… | Template | Why |
|---|---|---|
| a test/build/lint command | `until-green` (default) | writer turn + shell gate, no scorecard, until green or the cap |
| a second model's review, no test yet | `double-check` | writer, then a read-only critic scorecard, `required=0`, one iteration, `LOOP_SESSION=none` |
| a second model review *and* a test command | `two-model-critique` | writer, reviewer scorecard (`required=0`), fixer reads the brief, tests gate, `LOOP_SESSION=none` |
| "find N things" (bugs, edge cases) | `until-count` | hunt turn + a `DONE` script. Not a stronger check than a scorecard |

Decision guide and the full pattern catalog: see `references/patterns.md`.

### Hard vs. soft scorecard

The `two-model-critique` and `double-check` templates ship the scorecard as
**soft** (`required=0`). A failed rule does not stop the loop. The test gate
(in `two-model-critique`) or the iteration cap (in `double-check`) does.
The runner still records the marks in the brief and the mend.

`scorecard=` is one path token. It does not consume the rest of the line, so
`required=0` may sit on either side. Do not put `verdict=` on the same turn.
That is a parse error.

Drop `required=0` when the user's phrasing is "review it **before it counts
as done**" / "don't accept it unless the reviewer passes" — the review *is*
the acceptance signal. Keep `required=0` when the review is a second opinion
beside a real test gate.

The shipped `two-model-critique` stays `gated` because the tests gate is
required. `self-graded` and `cross-model` need no required gate and at least
one required scorecard. A soft card with the gate removed is `none`.
`cross-model` also needs the writer, reviewer, and fixer pins all non-empty,
with the reviewer different from both acting turns. Keep `required=0` and the
tests gate unless the review itself is the acceptance signal.

`double-check` has no required check, so finishing its one iteration is
`result: done`, not a pass.

A pi process that errors still abandons the rest of that iteration, even
when the turn is `required=0`. `required` is about the check, not a dead
process.

## Push back on "loop with no check"

If the user gives a vague goal ("make it better", "refactor until it's clean")
with **no objective way to tell success from failure**, do not just scaffold a
loop and call it done. Either:

- Propose an objective check (a test, a lint, a script that compares to an
  expected value) and confirm it with them, or
- Say so when the only check would be the same model grading itself. If the
  human insists, drop `required=0` so the scorecard is required, set a low
  `LOOP_MAX_ITER` (1 or 2), put `Assurance: self-graded` in `TODO.md`, and
  say this is a review aid. Empty or identical pins are then `self-graded`.
  A soft card left at `required=0`, with no required gate, is `none`. Stock
  `double-check` is that soft shape.

Prefer a shell gate. A scorecard does not replace one.

A loop with neither an objective gate nor a hard cap runs until the cap doing
nothing measurable. That is the failure mode to refuse. See
`references/guardrails.md`.

## Scaffold and wire it up

Once you know the pattern:

1. `cd` into the project root (or plan to use `-C`). The project should be a git
   repo — `loop` resolves the workroot as the containing git repo.
2. Run `loop init <template>` (or `loop init` for `until-green`). It refuses to
   overwrite an existing `.loop/`.
3. Edit the scaffolded files for the actual task:
   - `.loop/loop.env`: set `LOOP_TEST_CMD` to the real check command; uncomment
     and set `LOOP_<ROLE>_MODEL` for each role the template uses. Set
     `LOOP_MAX_ITER` to the agreed cap. Keep `LOOP_BRANCH=1` unless the user
     said otherwise. **`loop.env` is `KEY=VALUE` only — no `$()`, no backticks,
     no `${VAR:-default}`. It is never sourced by a shell, so those would be
     literal strings.** See `references/loop-env.md`. `fork` cuts to a new
     empty session. It does not pass `pi --fork`.
   - `.loop/prompts/*.md`: rewrite the starter content for the actual goal.
     Point the writer at `.loop/TODO.md`. Keep the "do not modify the tests
     to make them pass" rule if there is a test gate. A judging prompt says
     to write only the JSON path the runner names. It does not say to fix
     the code. A later fixer reads the brief the runner attached.
   - `.loop/scorecards/*.card`: write the items before `loop run`, about the
     goal, not about the model's feelings. Prefer `rule all`. Use
     `rule threshold` only when the operator asked for "N of M." Do not leave
     the items for the judging model to invent.
   - `.loop/gates/*.sh`: if the template has a gate, confirm it runs the right
     command. Gates run in the workroot with `LOOP_*` env exported. Do not
     put `pi` inside a gate script. The runner will trust the exit code
     anyway, and a model inside the gate is not a check.
4. Nothing to do about gitignoring: `loop init` writes `.loop/.gitignore`
   containing `*`, so the recipe hides itself. Git reads a `.gitignore` even
   when that file is itself ignored, which means `.loop/` never shows up in
   `git status` and the project's own `.gitignore` stays untouched. A loop
   recipe is personal automation — the model pins, the caps, the prompt
   wording are yours, not the project's — and this keeps it that way by
   default without asking the user to remember a step.

   To share a recipe with a team instead, replace that file's contents (or
   `git add -f .loop`) and commit it. That is the deliberate choice; the
   trade-off is that every contributor then carries your model pins and caps.

   Suggest a freeze pattern (`LOOP_FREEZE=*_test.go`) if the user wants the
   loop caught editing its own tests — see `references/guardrails.md`. A
   missing freeze index does not pass. The check fails closed, and the gate
   has to be in the manifest or nothing enforces the hashes.
5. Fill in `.loop/TODO.md` with the one-sentence goal (the first non-heading
   line is what the mend uses), plus whatever detail the model would otherwise
   have to guess. `loop init` scaffolds a stub. It sits inside `.loop/` with
   the rest of the recipe, so gitignoring `.loop/` keeps today's objective out
   of a shared repo along with everything else.

## Tell the user how to run it

Give the exact command and the page to open when they come back. From the
project root:

```
loop run                 # runs .loop/ in the current directory
```

or from elsewhere:

```
loop run -C /path/to/project
```

Then they leave. Hours are the steady state. They come back to `loop status`
and `state/<id>/return.md`. Do not tell them to watch iteration 1 if preflight
passed. Do tell them a preflight failure means the recipe is wrong: the run
stops with `result: recipe` and does not spend the cap. Do not tell them the
session will remember the dead ends. The mend does, and only the settled ones.

`LOOP_BRANCH=1` is the recommendation for anything unattended.

## Verify before you hand it over

Confirm the files, then stop. Do not run the recipe against a real model to
prove the template.

- `loop.env` is `KEY=VALUE`. No command substitution.
- A scorecard manifest line parses, the `.card` is `rule all` unless they
  asked for a threshold, and no new turn uses `verdict=`.
- No gate script calls `pi`.
- The judging prompt says to write only the JSON path the runner names.
- You told the human the command, `loop status`, and `return.md`.

## References

- `references/patterns.md` — the five patterns and how to pick one. They map
  onto four `loop init` templates: the build/lint pattern is `until-green`
  retargeted at a different command.
- `references/guardrails.md` — the rules that keep a loop useful: objective
  checks, bounded retries, freeze what proves success, disposable branches.
- `references/loop-env.md` — every `loop.env` key, what it does, sane defaults.
