# Loop patterns

A loop is only as good as its check. Rank the check before you pick a
template. Strongest first:

1. Exit code of a test, build, typecheck, or lint. The model has no vote.
2. A script that compares output to an expected value. Still a gate.
3. A scorecard whose resolved model differs from every acting turn, and none
   of those strings is empty.
4. A scorecard judged by the same model that acted, or by a model the runner
   cannot tell from pi's default.
5. A legacy `verdict=` grep. Do not scaffold a new one.

Write the `.card` before `loop run`. Items are about the goal. Prefer
`rule all`. Use `rule threshold` only when the operator asked for "N of M."
Do not put `pi` inside a gate script.

`until-count` is not above a scorecard on this list. Its `DONE` line is the
model declaring done, and the cap is the backstop.

## 1. Test-gate / until-green (the workhorse) — `loop init until-green`

The check is your test suite, an exit code the model cannot argue with. This is
the default. Writer turn, then the test gate, iterating until green or the cap
fires. No scorecard. Bounded: stops and exits 1 if it cannot go green in
`LOOP_MAX_ITER` iterations.

Convention-derived (no `manifest`): `prompts/01-writer.md` → turn,
`gates/tests.sh` → gate. The gate runs `LOOP_TEST_CMD` (default
`go test ./...`).

Two guardrails are baked in: a hard iteration cap, and a prompt rule not to
edit the tests to force green. The `loop.env` comment points at `return.md`.
Come back to `loop status` and that page.

## 2. Build/lint/typecheck gate — `loop init until-green`, retargeted

Same shape as until-green, different sensor. Any command with a meaningful exit
code works as a gate: `go build`, `tsc --noEmit`, `ruff check`,
`terraform validate`. Scaffold `until-green` and change `LOOP_TEST_CMD` (and
rename the gate file if you like). Chain gates cheapest-first: typecheck, then
lint, then tests — cheap gates fail fast and save expensive turns.

## 3. Two-model generate-and-critique — `loop init two-model-critique`

One model writes, a reviewer fills a scorecard and does not edit, the fixer
reads the brief the runner attached, then the test suite is the hard gate.
`LOOP_SESSION=none`. The reviewer's scorecard is soft (`required=0`); tests
are the objective. Different model families have different blind spots, so a
cross-model review catches more than one model reviewing itself, but only as
advice beside the gate.

The template uses a `manifest`: `writer` → `reviewer` (`scorecard=`, rule
all) → `fixer` → `tests` gate. The card is `scorecards/review.card`. Three
pins: `LOOP_WRITER_MODEL`, `LOOP_REVIEWER_MODEL`, and `LOOP_FIXER_MODEL`.

This template stays `gated` because the tests gate is required. `self-graded`
and `cross-model` need no required gate and at least one required scorecard.
A soft card with the gate removed is `none`. `cross-model` also needs the
writer, reviewer, and fixer pins all non-empty, with the reviewer different
from both acting turns. The stock template keeps `required=0` and the tests
gate.

The fixer prompt says to read the brief. It does not say to read the review
above in a shared session.

## 4. Double-check — `loop init double-check`

Work, then a read-only critic scorecard. Use it when there is no test to run
yet. Treat it as a review aid. The card is `rule all`, `required=0`,
`LOOP_MAX_ITER=1`, `LOOP_SESSION=none`. The critic prompt says to write only
the JSON path the runner names. It does not tell the critic to fix the code.

There is no shell gate, so the loop has no required check. It runs once and
exits 0 with `result: done`. That is not a pass. `return.md` says so.

Drop `required=0` only when the review is the acceptance signal. If the pins
are unset or the same, the runner labels that `self-graded`. Prefer graduating
to until-green.

## 5. Until-count (discovery work) — `loop init until-count`

Goal is "find N things" (bugs, edge cases, missing test cases), not "make the
tests pass." Each turn hunts for one more and appends it to a findings file.
The loop stops when the model writes `DONE` on its own line, or the cap fires.
The `DONE` rule is the model declaring done. Do not call this a stronger
check than a scorecard. The cap is the backstop. Assurance is `gated` because
the script is a required gate, and that label does not mean the findings were
tested.

Convention-derived: `prompts/01-hunt.md` → turn, `gates/done.sh` → gate that
greps the findings file for a lone `DONE`.

Porcelain sees `FINDINGS.md` appear (`?? FINDINGS.md`). A later append does
not change porcelain or HEAD. The template sets `LOOP_STALL_PATHS=FINDINGS.md`
so another finding is progress. Without that path, set `LOOP_STALL=continue`
or the default stall stops at iteration 2. A hunt that changes nothing, and
whose `DONE` gate keeps failing the same way, still stalls.

## Picking one

- Have tests: **until-green (1)**, optionally gated behind a **build/lint (2)**
  first.
- No tests yet, and a person will read the review: **double-check (4)**, and
  consider having the loop write tests first, then switch to until-green.
- Want a second model and a real gate: **two-model critique (3)**. The tests
  are the stopping rule. The scorecard is advice.
- Discovery ("find N"): **until-count (5)**, always with the cap. Not above
  a scorecard.

## Running unattended

Any of these can run in the background (`loop run > run.log 2>&1 &`). Rules
that keep this safe:

- Only background loops with a **hard stopping rule** (an iteration cap, and
  a gate when you can name one). An unbounded background loop burns tokens
  while you are away.
- Point it at disposable ground: `LOOP_BRANCH=1`. Do not leave `double-check`
  or `until-count` on `LOOP_BRANCH=0` if nobody is watching the worktree.
- Come back to `loop status` and `state/<id>/return.md`. Do not come back
  expecting the session to remember the dead ends. The mend kept the settled
  ones. Read the diff, not the model's summary.
