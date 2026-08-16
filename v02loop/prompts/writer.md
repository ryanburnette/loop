# Writer

You are implementing `loop2` v0.2.0, a Go rewrite of the POSIX `loop` runner.
v0.1 is already on this branch and `go test` was green for it. Read, in
order: `DESIGN-v0.2.md` (the technical delta), `TASK.md`, `CONSTRAINTS.md`,
and `CHECKLIST.md` (what "usable" means — a lot of this is not something
`go test` can grade, so read it now, not just when the reviewer complains
about it).

Workroot is this git repo (the one containing `DESIGN.md`), not the loop
directory.

Then read `gate-log.md` in this run's state dir (`$LOOP_LOG`). It has every
gate and reviewer verdict from every prior iteration. Do not redo work
already done; do not reintroduce something a past round already fixed.

Implement `DESIGN-v0.2.md`'s order first (`go test ./...` must be green).
Then work the checklist proactively — build the binary, run it, fix what you
find — instead of waiting for the reviewer to hand you the same items back.
That costs fewer iterations for both of us.

Hard rules:

- Do not modify any `*_test.go` file or anything under `testdata/`.
- Do not modify `DESIGN.md`, `DESIGN-v0.2.md`, `CHECKLIST.md`, `CONSTRAINTS.md`,
  `manifest`, `loop.env`, or any `gates/*.sh` in this loop dir. They define
  the goal and the gate; changing them to fit your work is exactly the
  failure mode this checklist exists to catch.
- Do not undo `clearLoopEnv` in the tests.
- If a test is genuinely wrong, stop and say why. Do not silently change it.
- Do not add Cobra, Viper, Bubble Tea, or a Makefile.
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
