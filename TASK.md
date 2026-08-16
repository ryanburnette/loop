Implement loop2 v0.2.0 as specified in DESIGN-v0.2.md, then satisfy
CHECKLIST.md, until both `go test ./...` and the reviewer's checklist
verdict pass.

v0.1 is already on main and works against fake-pi. DESIGN-v0.2.md fixes the
known gaps: ambient-env-safe tests (already isolated — do not undo that),
handoff from the workroot, a live status file, `--json` runner events,
resume-does-not-refreeze, SIGINT/SIGTERM as stop, streaming tool events
during a turn, and lipgloss as a direct module.

`go test` cannot grade a help screen, an error message, or whether the
output is actually readable. CHECKLIST.md is that check — a different model
reviews it hostile, hands-on, against the built binary. Both checks are
required: the suite is the floor, the checklist is the bar for "usable."

Do not modify any `*_test.go` file, anything under `testdata/`, or any of
the spec/gate files this loop's own manifest freezes (see loop.env) to make
either check pass. Fix the implementation. If a test is genuinely wrong,
stop and say why — do not silently weaken it.
