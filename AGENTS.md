# AGENTS.md

Guidance for any agent working in this repo. Read this first, then read
`LOCAL.md` if present (gitignored, machine/operator-specific). Then read
`DESIGN.md` — that is the product spec.

## What this is

`loop` is a Go CLI that runs agentic loops with `pi`. A loop is a directory
(`loop.env` + `manifest` + `prompts/` + `gates/` + `hooks/`). The runner acts,
checks, feeds back, and repeats until a stopping rule fires.

This is the rewrite of the POSIX `loop` in the sibling `../loop` repo. Behavior
that is not specified here is specified in `DESIGN.md`.

## Skills to load

- `go-develop` — mainstream packages only. `flag.FlagSet`, no Cobra/Viper.
  lipgloss v1 for terminal styling. No Makefile.
- `prose` — README, AGENTS.md, DESIGN.md, commit messages, user-facing text.
- `git-workflow` — commits and branches. Note the override below.

## Invariants

- Workroot is `git -C <loop-dir> rev-parse --show-toplevel`. No external-workroot flag.
- Default session policy is `none`. Shared sessions are opt-in and capped.
- Never call `pi` compact. Detect compaction; do not trigger it.
- Resume does not re-freeze.
- `loop.env` is `KEY=VALUE`, never sourced as a shell script.
- Do not edit `*_test.go` or `testdata/` to make tests pass.
- Custom `loop.sh` mode is deferred.

## Pre-commit

```sh
go fmt ./...
goimports -w .
go build ./...
go test ./...
go vet ./...
```

All five must be clean. If you change a `pi` flag the runner relies on, verify
it against `pi --help` first. Current flags: `-p`, `--mode json`, `--model`,
`--session-id`, `--session-dir`, `--no-session`, `--fork`, `--approve`,
`--append-system-prompt`, `--no-context-files`, `@<file>`.

## Git workflow override

The user has approved committing and pushing directly to `main` in **this repo
only**, once the suite is green and they ask. Until then, stay on the branch
the dogfood loop created. Still:

- Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`).
- Stage specific files; never `git add -A`.
- Multi-line messages go to `./tmp/commit-msg.txt` and `git commit -F`.
- Inspect `git status` and `git diff` before committing.

## Testing

`go test ./...` is the gate. It must not talk to a real model. `LOOP_PI` /
`testdata/fake-pi` is the contract for `internal/pi`.

## What not to do

- Don't add Cobra, Viper, Bubble Tea, or a web UI.
- Don't add an external-workroot flag.
- Don't source `loop.env`.
- Don't call `pi` compact.
- Don't re-freeze on resume.
- Don't put the binary on the user's PATH from this repo.
