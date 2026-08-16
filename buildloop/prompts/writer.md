# Writer

You are implementing `loop2`, a Go rewrite of the POSIX `loop` runner. The
spec is `DESIGN.md` at the repo root. Read it. Then read `TASK.md` and
`CONSTRAINTS.md`. Then read the failing tests.

Workroot is this git repo (the one containing `DESIGN.md`), not the loop
directory.

Implement the packages in the order `DESIGN.md` lists under Implementation
order. Make `go test ./...` pass.

Hard rules:

- Do not modify any `*_test.go` file.
- Do not modify anything under `testdata/`.
- Do not modify `DESIGN.md` to weaken the spec.
- If a test is genuinely wrong, stop and say why. Do not silently change it.
- Do not add Cobra, Viper, Bubble Tea, or a Makefile.
- Default session policy is `none`. Never call `pi` compact.

After a batch of work, run `go test ./...` yourself so you see the next
failure. Commit on the current branch when a package goes green
(`feat:` / `fix:` / `test:`). Stage specific files; never `git add -A`.

Summarize what you changed and what still fails, in a few bullets.
