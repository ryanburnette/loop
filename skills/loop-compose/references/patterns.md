# Loop patterns

A loop is only as good as its check. Patterns are ordered weakest check to
strongest. Each maps to a `loop init` template.

## 1. Double-check (weakest gate) — `loop init double-check`

Work, then a second turn that is a hostile self-review. The check is another
model turn, so it is soft. Use it when there is no test to run yet. Treat its
"looks good" with suspicion — it is the least grounded gate.

The template uses a `manifest` so the critic carries a soft verdict
(`required=0 verdict=^VERDICT: PASS\b` — note `required=0` comes *before*
`verdict=`, because `verdict=` consumes the rest of the line): a FAIL does not
stop the loop, only the iteration cap does. There is no hard gate, so the loop
exits 0 after one pass.

## 2. Test-gate / until-green (the workhorse) — `loop init until-green`

The check is your test suite, an exit code the model cannot argue with. This is
the default. Writer turn, then the test gate, iterating until green or the cap
fires. Bounded: stops and exits 1 if it cannot go green in `LOOP_MAX_ITER`
turns.

Convention-derived (no `manifest`): `prompts/01-writer.md` → turn,
`gates/tests.sh` → gate. The gate runs `LOOP_TEST_CMD` (default
`go test ./...`).

Two guardrails are baked in: a hard iteration cap, and a prompt rule not to
edit the tests to force green.

## 3. Build/lint/typecheck gate — `loop init until-green`, retargeted

Same shape as until-green, different sensor. Any command with a meaningful exit
code works as a gate: `go build`, `tsc --noEmit`, `ruff check`,
`terraform validate`. Scaffold `until-green` and change `LOOP_TEST_CMD` (and
rename the gate file if you like). Chain gates cheapest-first: typecheck, then
lint, then tests — cheap gates fail fast and save expensive turns.

## 4. Two-model generate-and-critique — `loop init two-model-critique`

One model writes, a *different* model reviews with a hostile prompt, the writer
addresses the findings, then the test suite is the hard gate. The reviewer's
`VERDICT` is a soft gate; tests are the hard gate. Different model families
have different blind spots, so cross-model critique catches more than either
reviewing itself.

The template uses a `manifest`: `writer` → `reviewer` (verdict) → `fixer`
→ `tests` gate. Set `LOOP_WRITER_MODEL`, `LOOP_REVIEWER_MODEL`, and
`LOOP_FIXER_MODEL` to different models for the cross-family benefit.

The reviewer verdict is soft by default: the template ships `required=0`, so a
`VERDICT: FAIL` does not stop the loop — only the test gate and the iteration
cap do. Drop `required=0` to make the verdict a hard, blocking gate.

## 5. Until-count (discovery work) — `loop init until-count`

Goal is "find N things" (bugs, edge cases, missing test cases), not "make the
tests pass." Each turn hunts for one more and appends it to a findings file. The
loop stops when the model writes `DONE` on its own line, or the cap fires. The
`DONE` rule is soft (the model writes it), so the turn cap is the hard
backstop.

Convention-derived: `prompts/01-hunt.md` → turn, `gates/done.sh` → gate that
greps the findings file for a lone `DONE`.

## Picking one

- Have tests: **until-green (2)**, optionally gated behind a **build/lint (3)**
  first.
- No tests yet: **double-check (1)**, and consider having the loop write tests
  first, then switch to until-green.
- Want a stronger review: **two-model critique (4)** — only when you can also
  name a hard gate, so the soft review is not the only stopping rule.
- Discovery ("find N"): **until-count (5)**, always with the turn cap.

## Running unattended

Any of these can run in the background (`loop run > run.log 2>&1 &`). Rules
that keep this safe:

- Only background loops with a **hard stopping rule** (an iteration cap or a
  gate that must pass). An unbounded background loop burns tokens while you are
  away.
- Point it at disposable ground: `LOOP_BRANCH=1`, a feature branch or scratch
  worktree — never a shared or production path.
- Tail the log; come back to a finished job or a clean failure.
