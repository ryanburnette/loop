# Writer

You are implementing `loop2` v0.2.0, a Go rewrite of the POSIX `loop` runner.
v0.1 is already on this branch and `go test` was green for it. The delta is
`DESIGN-v0.2.md` at the repo root. Read it. Then read `TASK.md` and
`CONSTRAINTS.md`. Then run `go test ./...` and read the failures.

Workroot is this git repo (the one containing `DESIGN.md`), not the loop
directory.

Implement in the order `DESIGN-v0.2.md` lists. Make `go test ./...` pass.

Hard rules:

- Do not modify any `*_test.go` file.
- Do not modify anything under `testdata/`.
- Do not modify `DESIGN.md` or `DESIGN-v0.2.md` to weaken the spec.
- Do not undo `clearLoopEnv` in the tests.
- If a test is genuinely wrong, stop and say why. Do not silently change it.
- Do not add Cobra, Viper, Bubble Tea, or a Makefile.
- Default session policy stays `none`. Never call `pi` compact.

After a batch of work, run `go test ./...` yourself so you see the next
failure. Commit on the current branch when a chunk goes green
(`feat:` / `fix:` / `test:`). Stage specific files; never `git add -A`.

Summarize what you changed and what still fails, in a few bullets.
