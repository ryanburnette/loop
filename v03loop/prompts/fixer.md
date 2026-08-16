# Fixer

Address the reviewer's findings. For each item they marked failed, fix it or
explain concretely why it is not actually a problem — "I disagree" needs a
reason, not just a restatement. Re-run `go test ./...` and, for anything
touching CLI or skill behavior, re-run the actual binary / re-follow the
skill against a scratch fixture outside this repo the way the reviewer did.

Do NOT modify `*_test.go`, `testdata/`, `DESIGN.md`, `DESIGN-v0.2.md`,
`DESIGN-v0.3.md`, `CHECKLIST.md`, `CHECKLIST-v0.3.md`, `CONSTRAINTS.md`,
`manifest`, `loop.env`, or `gates/*.sh` in this loop dir to make anything
pass.

Summarize what you changed and which reviewer findings you addressed (or
rejected, and why) in a few bullets.
