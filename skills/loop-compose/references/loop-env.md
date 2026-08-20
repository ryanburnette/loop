# loop.env key reference

`loop.env` lives at `.loop/loop.env`. It is `KEY=VALUE`, one per line, parsed by
the runner — **never sourced as a shell script.** That means no `$()`, no
backticks, and no `${VAR:-default}`: those would be literal strings, not
expansions. Comments start with `#`. Values may be quoted (`"..."` or `'...'`).

Resolution order is defaults, then `loop.env`, then process env
(`LOOP_PI=...`), then flags (`loop run --max-iter 3`). So a `LOOP_*` variable
exported in the shell beats the value written here, and a flag beats both. The
runner warns at startup when the environment and `loop.env` disagree about a
key, so an ambient `LOOP_MAX_ITER` does not silently replace the recipe's cap.

## Keys

### `LOOP_MAX_ITER` — iteration cap (required)
The hard backstop. The loop runs at most this many iterations. Always set it,
even alongside a gate, so a loop that never converges still terminates.
Default: `5`. Sensible range: 2–6 for fix-loops, up to ~10 for discovery.

### `LOOP_SESSION` — session policy
How pi sessions carry across turns within an iteration.
- `none` (default): each turn is a fresh `--no-session` invocation. Continuity
  comes from `handoff.md` and git history. Best when each turn should re-read
  the spec files instead of relying on conversation memory.
- `shared`: turns share one session id (`--session-id`).
- `fork`: each turn forks the previous session (`--fork`).

`none` is the safe default. Use `shared`/`fork` only when conversation context
genuinely helps and the spec is stable.

### `LOOP_BRANCH` — work on a throwaway branch
`1` makes the runner create `loop/<run-id>` off `LOOP_BRANCH_BASE` and refuse a
dirty tree, so the loop never touches your working branch. Strongly recommended
for any unattended loop. `0` (default) works in place.

### `LOOP_BRANCH_BASE` — base for the loop branch
The branch `loop/<id>` is created from. Default: `main`. Set to your trunk if it
has another name.

### `LOOP_FREEZE` — anti-cheat file patterns
Space-separated basename globs (e.g. `*_test.go`). At run start the runner
hashes every matching file in the workroot; the built-in `loop:frozen` gate
re-hashes each iteration and fails on drift. Add `gate frozen loop:frozen` to
the manifest (after the tests gate) to enforce it. Freeze the tests, the
fixtures, and the files that define "done" so the loop cannot pass by editing
its own goal or gate. Empty (default) = nothing frozen.

### `LOOP_<ROLE>_MODEL` — pin a model to a role
Maps a manifest step's `model=<role>` to a model id. Example:
`LOOP_WRITER_MODEL=synthetic/hf:zai-org/GLM-5.2` makes every `model=writer`
step use that model. Empty = pi's default. For `two-model-critique`, set
`LOOP_WRITER_MODEL`, `LOOP_REVIEWER_MODEL`, and `LOOP_FIXER_MODEL` to different
models for the cross-family benefit.

### Manifest key gotcha: `verdict=` and `system=` consume the rest of the line, and verdicts are regex
In a manifest, `verdict=VALUE` and `system=VALUE` swallow everything after them
on that line — they are not single tokens. So `required=0` must come **before**
`verdict=`, or it gets eaten into the verdict string and `required` stays at its
default `true`. Correct soft-verdict line:

```
turn critic prompts/02-critic.md model=critic required=0 verdict=^VERDICT: PASS\b
```

Wrong (this is a *hard* verdict, because `required=0` is swallowed):

```
turn critic prompts/02-critic.md model=critic verdict=^VERDICT: PASS required=0
```

The `\b` after `PASS` anchors the match so `VERDICT: PASSED` does not also
satisfy `VERDICT: PASS` — verdict patterns are regex, matched line-anchored
(`(?m)` is prepended), so word boundaries and `$` are meaningful. Omit the
boundary only if you genuinely want a prefix match.

### `LOOP_APPROVE` — pass `--approve` to pi
`1` (default) passes `--approve` so pi auto-approves tool calls (a loop cannot
prompt). Set `0` only for very constrained setups. The `--approve` flag on
`loop run` overrides this.

### `LOOP_CONTEXT` — extra context string
Appended to every pi turn as a positional argument. Use for a short, stable
reminder the model should always see. Usually empty — prefer `TASK.md` and
`CONSTRAINTS.md` in the workroot, which the runner feeds via `handoff.md`.

### `LOOP_COMPACT` — what to do when pi compacts
The runner detects compaction events; it never triggers them. This sets the
policy when one is detected mid-run:
- `fail` (strict): fail the turn that compacted.
- `warn` (default): note it; the next turn starts a new session.
- `allow`: ignore it.

### `LOOP_TEST_CMD` — the test command
The command the `tests` gate runs (the scaffolded `gates/tests.sh` evals this).
Default: `go test ./...`. Change to match your stack: `npm test`, `pytest -q`,
`cargo test`. Exported to gates as an env var, so custom gates can use it too.

### `LOOP_PI` — path to the pi binary
Default: `pi` (looked up on PATH). Override in tests or unusual installs. The
`--pi` flag on `loop run` overrides this.

## Minimal example (until-green)

```
LOOP_MAX_ITER=5
LOOP_SESSION=none
LOOP_BRANCH=1
LOOP_BRANCH_BASE=main
LOOP_TEST_CMD=go test ./...
# LOOP_WRITER_MODEL=synthetic/hf:zai-org/GLM-5.2
# LOOP_FREEZE=*_test.go
```
