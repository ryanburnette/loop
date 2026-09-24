# Guardrails

A loop is a machine that repeats. Anything wrong with one turn, it will repeat
too: a vague goal, a soft check, an unbounded run. These rules keep a loop
useful instead of expensive. Read this before you background anything.

## 1. A loop needs a stopping rule, and "the model thinks it is done" is not one

The model is optimistic about its own completion. Every loop needs at least one
stopping rule it cannot talk around:

- A **gate that passes**: tests green, build exits 0. Best rule when you have
  it. (A `gates/*.sh` that exits 0.)
- An **iteration cap** (`LOOP_MAX_ITER`). Always include this, even alongside a
  gate, so a loop that never converges still terminates.
- A **human checkpoint**: the loop stops and hands back for review
  (`LOOP_MAX_ITER=1`, or pause via the control file).

If a loop has only a soft rule (the model writes `DONE`, a critic scorecard
with `required=0`), add a hard cap next to it. Soft plus hard is fine. Soft
alone runs away. `until-count`'s `DONE` line is the model declaring done. Do
not rank that pattern above a scorecard.

## 2. The check must be more objective than the model

This is where loops fail quietly. Ranked strongest to weakest:

1. Exit code of a test, build, typecheck, or lint. The model has no vote.
2. A script that compares output to an expected value. Still a gate.
3. A scorecard whose resolved model differs from every acting turn, and none
   of those strings is empty.
4. A scorecard judged by the same model that acted, or by a model the runner
   cannot tell from pi's default.
5. A legacy `verdict=` grep. Weakest. Do not scaffold a new one.

Push every loop up this list as far as it will go. If the only check is level
4, you have automated the model agreeing with itself. Push back. If the human
insists, set a low cap, put `Assurance: self-graded` in `TODO.md`, and call
it a review aid. Prefer adding a shell gate.

The agent writes the `.card` before the run. Items are about the goal, not
the model's feelings. Prefer `rule all`. Use `rule threshold` only when the
operator asked for "N of M."

Do not put `pi` inside a gate script. A gate is an executable the runner
trusts. A model call inside that script is a recipe bug, and the exit code
still wins.

`until-count` is not level 1. The script greps for a line the model wrote.

## 3. Watch for the loop cheating the gate

A test-gate loop has an obvious exploit: edit the tests until they pass. Models
do this. Defenses:

- Tell it not to, in the prompt: "do not modify tests to make them pass." (The
  `until-green` and `two-model-critique` templates ship this rule.)
- Use `LOOP_FREEZE` to freeze the test files at run start. The built-in
  `loop:frozen` gate re-hashes them each iteration and fails on drift — so if
  the loop edits a frozen file to pass, the frozen gate catches it. Add
  `gate frozen loop:frozen` to the manifest after the tests gate.
  Example: `LOOP_FREEZE=*_test.go`. A missing freeze index does not pass.
  The check fails closed. Uncommenting `LOOP_FREEZE` without the gate only
  records hashes.

Same idea for any gate: a lint gate can be silenced with an ignore comment, a
typecheck with `any`. Grep the diff for the escape hatches.

## 4. Bound the retries per failure

If one action keeps failing, more retries rarely fix it; the failure usually
means something systemic the extra turns cannot touch. Keep `LOOP_MAX_ITER` low
(2–5). The default `LOOP_STALL=stop` ends the run after two not-ok iterations
with the same required failure and the same tree. `LOOP_STALL=continue` is
how a recipe keeps going.

`until-count` does not need `LOOP_STALL_PATHS` for `FINDINGS.md`. That file
is a normal untracked file, so porcelain changes when the hunt appends.
Porcelain does not list ignored paths. An ignored file is not progress unless
the recipe names it.

## 5. Cost is real and compounds

Every turn spends tokens. Controls:

- Iteration caps double as cost caps.
- Cheapest gate first (typecheck before full tests) so bad turns die early.
- Use `LOOP_SESSION=none` (the default) when continuity across turns should
  come from git history and `mend.md` rather than conversation memory — each
  turn re-reads the spec instead of paying to reread its own noise. Use
  `shared` only when the operator explicitly wants the in-iteration transcript.
  `fork` cuts to a new empty session. It does not pass `pi --fork`. That flag
  would copy the transcript.
- The runner detects compaction events (never triggers them). Set
  `LOOP_COMPACT=fail` to fail a turn that compacted, `warn` to note it, or
  `allow` to ignore the warning. All three still cut the session.

The mend keeps settled lines the runner committed. It does not keep the
argument. Do not tell the human the session will remember the dead ends.

## 6. Agent psychosis: a loop that succeeds at the wrong thing

The sharpest failure is not a crash; it is a loop that *succeeds convincingly at
the wrong thing*. It optimizes hard against the metric it was given and produces
a result that looks like a triumph to anyone who cannot check the domain.

Two working rules:

- **Loop on things you can check.** If you cannot judge the output (unfamiliar
  domain, no test that captures what "good" means), do not hand it to an
  unattended loop. Use the loop to learn the domain with you in the seat first.
- **Read the diff, not the summary.** The model's turn-end summary is its own
  optimistic narration. The diff, the gate exit, and `return.md` are what the
  human reads. `loop status` prints that page. The runner writes turn output
  and `gate-log.md` under `.loop/state/<id>/` for the operator, not as the
  next turn's context.

## 7. Keep loops on disposable ground

Background and unattended loops should run where a wrong answer costs nothing to
discard: `LOOP_BRANCH=1` makes the runner create `loop/<id>` off
`LOOP_BRANCH_BASE` (default `main`) and refuse a dirty tree, so the loop works
on a throwaway branch. Review the branch and merge, or throw it away. The loop
proposes; you dispose. Nothing in the runner merges.
