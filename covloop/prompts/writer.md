# Writer

`go test ./...` has exactly one failing test:
`cmd/loop.TestFreezePrintsHowToCheckIt`. Read it in `cmd/loop/main_test.go`.

The bug: `loop freeze <dir>` (in `cmd/loop/main.go`, `cmdFreeze`) prints where
it wrote the frozen snapshot, but not how to actually use it. A user has to
already know that `frozen?` needs `LOOP_STATE_DIR` and `LOOP_WORKROOT` set to
specific paths. Fix `cmdFreeze` so its success output includes the exact
command to run — something like:

```
froze 1 pattern(s) → <state>
check with: LOOP_STATE_DIR=<state's parent> LOOP_WORKROOT=<workroot> loop frozen?
```

Match whatever the test actually asserts (`LOOP_STATE_DIR=`, `LOOP_WORKROOT=`,
`frozen?` must all appear in stdout) — read the test, don't guess at it.

Hard rules:

- Do not modify `cmd/loop/main_test.go` or any other `*_test.go` file.
- Do not modify anything under `testdata/`.
- Do not modify this loop dir's `loop.env`, `manifest`, or `gates/tests.sh`.
- This is the smallest possible change that makes the test pass. Do not
  refactor unrelated code in the same pass.

Run `go test ./...` yourself before finishing. Summarize the change in one or
two bullets.
