# CHECKLIST-v0.3.md — what "usable" means for loop2 v0.3

`go test` cannot grade whether a header panel is legible, whether the skill's
guidance actually produces a working `.loop/` directory, or whether `loop
init`'s starter prompts are real content or lorem ipsum. This file is that
check. `CHECKLIST.md` (v0.2) still applies underneath this — build, vet,
fmt, test, docs-match-binary — this file is the v0.3 delta on top of it.

Reviewer: do not grade from reading the diff. Build the binary and run every
item for real. Do all hands-on testing **outside this repo** (`mktemp -d` in
`/tmp`, not `./tmp`) — a scratch `.loop/` built inside this repo can contain
a file that trips `loop:frozen` on the next check.

If `gates/tests.sh` already failed earlier this iteration (check
`gate-log.md`), the code is not usable by definition — say so, cite it,
`VERDICT: FAIL`, skip the rest. Read `gate-log.md` before you start; don't
re-discover what a past iteration of you already found.

Every item needs a one-line result: the command you ran and what you saw.

## A. cwd + `.loop/` resolution

- [ ] In a scratch git repo with `.loop/` at its root, `cd` into it and run
      `./tmp/loop run` with no arguments — it finds and runs `.loop/`. Prove
      it with `pwd` in your transcript, not just "it worked."
- [ ] From a different cwd, `./tmp/loop run -C /path/to/that/repo` runs the
      same `.loop/` without cd'ing there.
- [ ] Running `./tmp/loop run` in a directory with no `.loop/` fails fast,
      names the directory it looked in, and suggests `loop init`. No stack
      trace, no bare "exit status 1".
- [ ] The old v0.1/v0.2 `loop <dir>` / `loop run <dir>` positional-directory
      form is gone. Confirm `./tmp/loop run /some/dir` either errors clearly
      (treats it as an unexpected argument) or is rejected — it must not
      silently run a directory that isn't `.loop`.

## B. `manifest` optional, convention-derived fallback

- [ ] Build a `.loop/` with **no `manifest` file**: `prompts/01-writer.md`,
      `prompts/02-reviewer.md`, `gates/tests.sh`, wired to fake-pi. Run it.
      Confirm from `gate-log.md`/turn files that the steps ran in the order
      the numbers implied (writer, then reviewer, then the gate) — not
      filesystem order, not reversed.
- [ ] A `.loop/` with an explicit `manifest` still uses it verbatim even if
      `prompts/`/`gates/` also have files that would derive something
      different. Explicit wins.
- [ ] A `.loop/` with neither a `manifest` nor any files under
      `prompts/`/`gates/`/`hooks/` fails with a message that says there's
      nothing to run — not a panic, not a silent no-op success.

## C. `loop init`

- [ ] `loop init` with no argument scaffolds `.loop/` with real, useful
      starter content in the prompt file(s) — read them. Placeholder text
      like "TODO: write your prompt here" with nothing else is a fail.
- [ ] `loop init until-green`, `loop init double-check`,
      `loop init two-model-critique`, `loop init until-count` each scaffold
      something distinct from the others — diff two of them and confirm the
      `loop.env` and prompt content actually differ in a way that matches
      the pattern's name (e.g. two-model-critique's `loop.env` mentions a
      second model role).
- [ ] Running `loop init` a second time where `.loop/` already exists
      refuses and does not touch the existing files — prove it by editing
      `.loop/loop.env` first, re-running `init`, and confirming your edit
      survived.
- [ ] A scaffolded template actually runs: `loop init until-green` in a
      scratch repo, point it at fake-pi, `loop run`, confirm it completes
      (success or a clean expected failure against the fake gate — either
      is fine, a crash is not).

## D. UI — read the actual output, don't summarize it

Paste real terminal output into your review for each of these, not a
description of it.

- [ ] Default output includes a header with: loop id, session mode,
      iteration cap, and git info (current branch, short SHA, dirty/clean).
      Run it once in a clean scratch repo and once with an uncommitted
      change present — the dirty indicator must differ between the two.
- [ ] Turn steps, gate steps, and hook steps are visually distinguishable
      from each other in the default output — not just by reading the text
      carefully, but at a glance (color, label, or glyph).
- [ ] A failing gate's output shows *why* in the default terminal output —
      not just "FAIL" with the detail buried only in `gate-log.md`.
- [ ] The run ends with a one-line-or-small-block summary: elapsed time,
      iterations used, pass/fail, and the branch name if `LOOP_BRANCH=1`.
- [ ] `NO_COLOR=1 ./tmp/loop run ...` produces output with **zero** ESC
      bytes. Check it directly: `NO_COLOR=1 ./tmp/loop run ... | cat -v |
      grep -c '\^\['` must print `0`. This is not optional — it's the one
      objectively checkable piece of "beautiful and informative," so verify
      it exactly, don't eyeball it.
- [ ] `-q` output is still just the final result line — the header/step
      panels do not leak through quiet mode.
- [ ] `--json` output is unaffected by all of the above: still one JSON
      object per line, still parses with `jq -c .`.

## E. The skill

- [ ] `skills/loop-compose/SKILL.md` exists, has valid frontmatter (`name:`
      matches the slug rules — lowercase, digits, hyphens, no leading/
      trailing/doubled hyphen — and a real `description:`), and is not
      just a restatement of this checklist.
- [ ] **Actually use it.** Pick two different scenarios (e.g. "I have a
      failing test suite, make it pass" and "I want a second model to
      review my writer's output before it counts as done"). For each,
      follow the skill's own instructions — as if you were the agent a user
      handed this task to — to compose a real `.loop/` directory. Then
      `loop run` it (against fake-pi is fine). Report what you did and
      whether it worked; a skill that reads well but produces a `.loop/`
      that fails to run is a fail, no matter how good the prose is.
- [ ] The skill pushes back on "loop with no check" — if you feed it a
      vague goal with no objective way to tell success from failure in your
      test scenario, it should ask for one or propose one, not just
      scaffold `loop init` and call it done.
- [ ] `references/patterns.md`, `references/guardrails.md`, and
      `references/loop-env.md` (or equivalent) exist and describe the
      *actual* v0.3 `.loop/` format and `loop.env` keys — not the POSIX
      repo's `loop.env`/`manifest`-only shape. Spot-check one claim in each
      file against the real behavior.

## F. Regression safety

- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` (empty output),
      `go test ./... -count=1` are all clean.
- [ ] `go.mod` still has no Cobra, Viper, Bubble Tea. No `Makefile`.
- [ ] Everything in `CHECKLIST.md` (v0.2) that is still applicable still
      holds — spot check at least the `--resume`/freeze and SIGTERM items,
      since the directory-resolution change touches the same code paths.

## Verdict

`VERDICT: PASS` only if every box above is checked with real evidence.
Prefer `FAIL` when unsure — a false PASS here is worse than one more
iteration.
