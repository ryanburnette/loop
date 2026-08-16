# CHECKLIST.md — what "usable" means for loop2 v0.2

`go test` cannot grade a help screen, an error message, or whether output is
readable. This file is that check instead. It stands in the same place a test
suite does: something the reviewer holds the work against that is not the
model's own opinion of its own work.

Reviewer: do not grade from reading the diff. Build the binary and run every
item for real. "The model is an optimistic narrator of its own work" applies to
you too when you are reviewing — a summary that sounds right is not evidence.
Evidence is the actual output of the actual command.

Do all hands-on testing **outside this repo** (`mktemp -d` in `/tmp`, not
`./tmp`). A scratch loop dir you build inside this repo can contain a file
named `fake-pi` or `manifest`, which will trip the `loop:frozen` gate on the
next check — that is a false positive you caused, not a real drift.

If `gates/tests.sh` already failed earlier this iteration (check
`gate-log.md` / `$LOOP_LOG`), the code is not usable by definition. Say so,
cite it, `VERDICT: FAIL`, and skip the rest — do not spend time hand-testing
code that does not build.

Read `gate-log.md` before you start. It has every gate and verdict from every
prior iteration of this run. Do not re-discover what a past round of you
already found; check whether it was fixed.

Every item below needs a one-line result: the command you ran and what you
saw, not "looks good."

## A. Build

- [ ] `go build -o ./tmp/loop ./cmd/loop` succeeds from a clean tree.
- [ ] `./tmp/loop version` prints a non-empty version string.
- [ ] `./tmp/loop help` exits 0 and lists every subcommand that actually
      exists in `cmd/loop/main.go` (`run`, `status`, `freeze`, `frozen?`,
      `help`, `version`) and every flag `run` actually accepts. A flag in
      the code that is missing from `help`, or vice versa, is a fail.

## B. A real run against fake-pi (scratch dir, outside this repo)

Build a throwaway git repo with a minimal loop dir (mirror
`testdata/loops/until-green`), point `LOOP_PI` or `--pi` at this repo's
`testdata/fake-pi`, and run the real binary against it.

- [ ] `LOOP_BRANCH=1` on a clean scratch repo creates `loop/<id>` and
      `backup/loop-<id>`. `git branch` shows both.
- [ ] `LOOP_BRANCH=1` on a **dirty** scratch repo refuses to start, names the
      problem, and creates no branch.
- [ ] `state/<id>/turn-1-<name>.md` contains the extracted assistant text,
      not a raw jsonl dump.
- [ ] `state/<id>/status` exists during the run and its content changes as
      the run progresses (iteration number, phase).
- [ ] Put a `TASK.md` in the scratch repo's root (not the loop dir) before
      running. `state/<id>/handoff.md` after iteration 1 contains that
      goal. If it says "(none)" while a workroot `TASK.md` exists, fail this
      item and say so.
- [ ] `./tmp/loop status <scratch-loop-dir>` after a run prints the run id,
      iteration, and success state without a crash or a stack trace.
- [ ] Freeze a file (`LOOP_FREEZE`), let a turn edit it (or edit it
      yourself to simulate one), run with `gate frozen loop:frozen` in the
      manifest: the gate fails and the failure output names the pattern
      that drifted, not just "error."
- [ ] `--resume <id>` continues from the correct next iteration (check
      `state/<id>/iteration` before and after), does not create a second
      branch, and does not re-run freeze setup (drift a frozen file between
      the first run and the resume; resume must still catch it).
- [ ] Send the process `SIGTERM` mid-run (e.g. `kill %1` a few seconds in).
      It exits non-zero, does not hang, and `meta.env` is not left
      half-written garbage.

## C. Output quality — the part a unit test cannot grade

- [ ] Default (human) output on a run against fake-pi has: a header block
      naming the run, one line per iteration boundary, one line per step
      with a pass/fail mark, and one unambiguous final line (success,
      failed, or done). Paste the actual output into your review; do not
      describe it from memory.
- [ ] `-q` output is the final result line and nothing else — count the
      lines.
- [ ] `--json` output is one JSON object per line for the whole run. Pipe it
      through `jq -c .` (or equivalent) and confirm every line parses and at
      least header / iteration / step / result event types are present.
- [ ] Trigger three real error cases: a loop dir that does not exist, a
      loop dir with no `manifest` and no `loop.sh`, and a `loop.env` with a
      broken line (e.g. `LOOP_CONTEXT=$(whoami)`). Each error names the
      actual problem. "exit status 1" with nothing else is a fail.

## D. Docs match the binary

- [ ] Every command shown in `README.md` runs as written against
      `./tmp/loop`. A stale v1-only instruction (sourcing `loop.env`,
      shell-only flags) is a fail.
- [ ] Every command in `AGENTS.md`'s pre-commit checklist succeeds:
      `go fmt ./...`, `goimports -w .`, `go build ./...`, `go test ./...`,
      `go vet ./...`.
- [ ] `go.mod` has no Cobra, Viper, or Bubble Tea. No `Makefile` at the repo
      root. (`grep -i` for the first three, `ls` for the fourth.)

## E. Regression safety (cheap, but still worth restating here)

- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` (empty output),
      `go test ./... -count=1` are all clean.

## Verdict

`VERDICT: PASS` only if every box above is checked with real evidence.
Prefer `FAIL` when unsure — a false PASS here is worse than one more
iteration, because nobody else is checking this list before the branch gets
reviewed by a human.
