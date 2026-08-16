# Writer

You are implementing `loop2` v0.3.0. v0.1 and v0.2 are already on this
branch and `go test` is green for them. Read, in order: `DESIGN-v0.3.md`
(the spec for this round), `TASK.md`, `CONSTRAINTS.md`, and
`CHECKLIST-v0.3.md` (what "usable" means for this round — read it now, not
just when the reviewer complains about it).

Workroot is this git repo (the one containing `DESIGN.md`), not the loop
directory.

Then read `gate-log.md` in this run's state dir (`$LOOP_LOG`). It has every
gate and reviewer verdict from every prior iteration. Do not redo work
already done; do not reintroduce something a past round already fixed.

This is a bigger round than v0.2: a real reframe of how the CLI is used
(current directory + `.loop/`, not a directory argument), a UI overhaul, and
a new pi skill. `DESIGN-v0.3.md`'s "Implementation order" section is the
sequence — follow it. Some test files were already updated for the new
contract (`cmd/loop/main_test.go` no longer uses a positional loop
directory; `internal/loopdir` and `internal/gitinfo` are new packages with
only test files; `internal/manifest`'s test file has new cases for
`Load`/`Derive`). Read those test files closely before writing code against
them — they are the actual spec for what each function must do.

`go test ./...` must be green before you start on the checklist. Then work
`CHECKLIST-v0.3.md` proactively — build the binary, run it, fix what you
find — instead of waiting for the reviewer to hand you the same items back.

The skill (`skills/loop-compose/SKILL.md` and its `references/`) is prose,
not code, but it's still a required deliverable, and it needs to describe
the CLI you actually built, so write it after the CLI/UI/init work is real,
not before. Use it yourself once, for one realistic scenario, before you
consider the turn done — if following your own skill's instructions doesn't
produce a `.loop/` that actually runs, the skill is wrong, not the test.

Hard rules:

- Do not modify any `*_test.go` file or anything under `testdata/`.
- Do not modify `DESIGN.md`, `DESIGN-v0.2.md`, `DESIGN-v0.3.md`,
  `CHECKLIST.md`, `CHECKLIST-v0.3.md`, `CONSTRAINTS.md`, `manifest`,
  `loop.env`, or any `gates/*.sh` in this loop dir. They define the goal and
  the gate; changing them to fit your work is exactly the failure mode this
  checklist exists to catch.
- Do not undo `clearLoopEnv` in the tests, and add it to any new test file
  you write that touches config loading or the CLI — a past round
  (`covloop`) got this wrong once already.
- If a test is genuinely wrong, stop and say why. Do not silently change it.
- Do not add Cobra, Viper, Bubble Tea, a web UI, or a Makefile.
- Default session policy stays `none`. Never call `pi` compact.
- Build scratch/manual test artifacts outside this repo (`mktemp -d`), not
  under `./tmp` here — a stray file named `fake-pi` or `manifest` inside
  this repo will falsely trip the freeze gate.

After a batch of work, run `go test ./...` and, once it is green, actually
run the built binary against a scratch fixture yourself. Commit on the
current branch when a chunk goes green (`feat:` / `fix:` / `test:`). Stage
specific files; never `git add -A`.

Summarize what you changed, what still fails, and which checklist items you
verified yourself, in a few bullets.
