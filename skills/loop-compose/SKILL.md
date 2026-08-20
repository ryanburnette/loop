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

Everything needed to set up a loop lives in one directory. You drop it into a
project, gitignore it, and call `loop` from your PATH.

```
.loop/
  loop.env            # KEY=VALUE config (never sourced as a shell script)
  manifest            # OPTIONAL — omit it to derive steps from file names
  TODO.md             # the goal; first non-heading line leads every handoff
  CONSTRAINTS.md      # OPTIONAL — standing rules copied into every handoff
  prompts/
    01-writer.md      # numbered prompt files → turn steps, in lexical order
    02-reviewer.md
  gates/
    tests.sh          # any executable → a gate step, run after all turns
  hooks/
    notify.sh         # any executable → a hook step, run last
  state/              # created at runtime
```

If there is no `manifest`, the runner derives one: `prompts/*.md` become turn
steps, `gates/*` become gate steps, `hooks/*` become hook steps — all required,
sorted lexically by filename. A step's role name is the filename with its
extension and a leading `NN-` numeric prefix stripped (`01-writer.md` →
`writer`). Use numbered files for the common case; write a `manifest` only when
you need interleaving (turn, gate, turn), soft verdicts, or `system=`.

`loop` operates on `.loop/` in the current directory. `loop run` with no
arguments runs `.loop/`. `loop run -C /path/to/project` runs the `.loop/` in
that project (you may also name the loop dir directly, e.g.
`-C /path/to/project/.loop`). There is no upward search.

## Before you scaffold: ask the user these questions

You cannot pick a pattern without knowing the check. Ask, in this order:

1. **What is the goal, in one sentence?** (This becomes the first line of
   `.loop/TODO.md` — the loop reads it as the handoff goal.)
2. **What is the check — how will we know the loop succeeded?** This is the
   load-bearing question. The options, strongest to weakest:
   - A test suite / build / lint command with an exit code (best).
   - A script that compares output against an expected value.
   - A second, different model reviewing with a hostile prompt (soft).
   - The same model grading itself (weakest — push back, see below).
3. **How many turns should it be allowed before it gives up?** (The iteration
   cap. Always have one.)
4. **Should it work on a throwaway branch?** (Almost always yes — `LOOP_BRANCH=1`
   keeps the loop off your working tree.)

## Pick the pattern

`loop init <template>` scaffolds one of four. Match the user's check to one:

| The user's check is… | Template | Why |
|---|---|---|
| a test/build/lint command | `until-green` (default) | writer turn + test gate, iterates until green or the cap fires |
| a second model's hostile review, no test yet | `double-check` | writer + critic with a soft verdict, no hard gate |
| a second model review *and* a test command | `two-model-critique` | write → review (verdict) → fix → tests |
| "find N things" (bugs, edge cases) | `until-count` | hunt turn + a gate that looks for a `DONE` line |

Decision guide and the full pattern catalog: see `references/patterns.md`.

### Hard vs. soft reviewer verdict

The `two-model-critique` and `double-check` templates ship the reviewer/critic
verdict as **soft** (`required=0`): a `VERDICT: FAIL` does not stop the loop —
only the test gate (in `two-model-critique`) or the iteration cap (in
`double-check`) does. The runner still logs `VERDICT <name>: FAIL` to
`gate-log.md` and prints a non-fatal marker, but the run continues.

Drop `required=0` (so the verdict line reads just `verdict=^VERDICT: PASS\b`) to
make the reviewer's **FAIL a hard, blocking gate** — the iteration is marked
failed and, if the cap is spent, the run fails. (A failed required step does
not abort the remaining steps in the iteration; it only sets the iteration's
outcome, so the run fails once the cap is exhausted.) Do this when the user's phrasing is "review it
**before it counts as done**" / "don't accept it unless the reviewer passes" —
i.e. the review *is* the acceptance signal, not just advice. Keep `required=0`
when the review is a second opinion alongside a real test gate, or when you want
the loop to keep iterating toward the cap regardless of the reviewer's mood.

Remember `required=0` must come *before* `verdict=` on the manifest line,
because `verdict=` consumes the rest of the line.

## Push back on "loop with no check"

If the user gives a vague goal ("make it better", "refactor until it's clean")
with **no objective way to tell success from failure**, do not just scaffold a
loop and call it done. Either:

- Propose an objective check (a test, a lint, a script) and confirm it with
  them, or
- Propose the soft-check + hard-cap combo: a hostile reviewer turn (soft
  verdict) bounded by a low `LOOP_MAX_ITER` (2–3), so the loop runs a couple of
  passes and stops for the human. Make clear this is a *review aid*, not a
  correctness guarantee.

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
     literal strings.** See `references/loop-env.md`.
   - `.loop/prompts/*.md`: rewrite the starter content for the actual goal.
     Point the writer at `.loop/TODO.md`. Keep the "do not modify the tests
     to make them pass" rule if there is a test gate.
   - `.loop/gates/*.sh`: if the template has a gate, confirm it runs the right
     command. Gates run in the workroot with `LOOP_*` env exported.
4. `loop init` writes `.loop/.gitignore` with `state/`, so run state won't
   dirty the tree on its own. Treat `.loop/` itself as **operator scratch**:
   gitignore it (add `.loop/` to the project's `.gitignore`) rather than
   committing the recipe. A loop recipe is personal automation — the model
   pins, the caps, the prompt wording are yours, not the project's — and
   committing it into a shared repo forces every contributor to carry your
   loop config. `loop run` works either way (the branch check tolerates an
   untracked `.loop/`). The trade-off is honest: a gitignored recipe is easy
   to lose and is not shared across machines, so if the loop is meant to be
   part of the project's workflow (everyone should run `loop run` the same
   way), commit `.loop/` instead and skip the gitignore. Suggest a freeze
   pattern (`LOOP_FREEZE=*_test.go`) if the user wants the loop caught
   editing its own gate — see `references/guardrails.md`.
5. Fill in `.loop/TODO.md` with the one-sentence goal (the first non-heading
   line is what the loop uses as the handoff goal), plus whatever detail the
   model would otherwise have to guess. `loop init` scaffolds a stub. It sits
   inside `.loop/` with the rest of the recipe, so gitignoring `.loop/` keeps
   today's objective out of a shared repo along with everything else.

## Tell the user how to run it

Give the exact command. From the project root:

```
loop run                 # runs .loop/ in the current directory
```

or from elsewhere:

```
loop run -C /path/to/project
```

Watch the first iteration. If the gate fails for a systemic reason (wrong
command, missing dependency), stop and fix the recipe — don't let it burn the
cap. `loop status` shows the current run; `gate-log.md` under `.loop/state/<id>/`
has every gate verdict.

## Verify before you finish

Before declaring the recipe done, run `loop run` once yourself (against a fake
or cheap model is fine) and confirm it actually starts, runs the steps in the
intended order, and stops on the gate. This works straight after `loop init` —
no commit is required first. A recipe that reads well but fails to run is wrong
— fix the recipe, don't hand the user prose.

## References

- `references/patterns.md` — the five patterns and how to pick one. They map
  onto four `loop init` templates: the build/lint pattern is `until-green`
  retargeted at a different command.
- `references/guardrails.md` — the rules that keep a loop useful: objective
  checks, bounded retries, freeze what proves success, disposable branches.
- `references/loop-env.md` — every `loop.env` key, what it does, sane defaults.
