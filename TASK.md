Implement loop2 v0.2.0 as specified in DESIGN-v0.2.md until `go test ./...` is green.

v0.1 is already on main and works against fake-pi. This pass fixes the
gaps: ambient-env-safe tests (already isolated — do not undo that),
handoff from the workroot, a live status file, `--json` runner events,
resume-does-not-refreeze, SIGINT/SIGTERM as stop, streaming tool events
during a turn, and lipgloss as a direct module.

Do not modify any `*_test.go` file or anything under `testdata/` to make
the tests pass. Fix the implementation. If a test is genuinely wrong,
stop and say why — do not silently weaken it.
