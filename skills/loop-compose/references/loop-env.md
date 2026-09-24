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

Do not set `LOOP_MEND`, `LOOP_BRIEF`, `LOOP_RETURN`, `LOOP_SCORECARD_OUT`, or
`LOOP_PROPOSAL_OUT` here. The runner sets the first three on gates and hooks,
and the last two on the pi process. A recipe that sets them is an unknown key.
On the gate side the runtime value wins.

## Keys

### `LOOP_MAX_ITER` — iteration cap (required)
The hard backstop. The loop runs at most this many iterations. Always set it,
even alongside a gate, so a loop that never converges still terminates.
Default: `5`. Sensible range: 2–6 for fix-loops, up to ~10 for discovery.
Preflight rejects a value below 1.

### `LOOP_SESSION` — session policy
How pi sessions carry across turns within an iteration. The next iteration
does not keep the transcript. It gets `mend.md`.
- `none` (default): each turn is a fresh `--no-session` invocation. Continuity
  comes from `mend.md` and git history. Best when each turn should re-read
  the spec files instead of relying on conversation memory. The templates
  use this.
- `shared`: turns inside one iteration share one session id (`--session-id`).
  The iteration boundary opens a new id. Opt in when you want that transcript.
- `fork`: the name stays so old files parse. It is `shared`, plus a cut to a
  new empty session when the probed context percent is known,
  `LOOP_FORK_PERCENT` is greater than 0, and the percent is at least that
  threshold (default 40). The runner does not pass `pi --fork`. Unknown
  percent does not cut. A known 0 does not cut. `LOOP_FORK_PERCENT` of 0 or
  less turns the percent cut off. A turn cap or a compaction also opens a
  new empty session.

`none` is the safe default.

### `LOOP_SESSION_TURNS` — turns before a new session id
Default: `4`. Counts turns inside one session, not iterations. After this
many, `shared` and `fork` open a new empty id and the runner attaches the
current brief. `none` does not use it.

### `LOOP_FORK_PERCENT` — percent cut for `fork`
Default: `40`. `fork` cuts when context percent is at least this number.
`0` or below disables the percent cut. It does not mean "always cut."
`none` does not consult it.

### `LOOP_BRANCH` — work on a throwaway branch
`1` makes the runner create `loop/<run-id>` off `LOOP_BRANCH_BASE` and refuse a
dirty tree, so the loop never touches your working branch. Strongly recommended
for any unattended loop. `0` (default in the runner) works in place.
`until-green` and `two-model-critique` set `1`. `double-check` and
`until-count` set `0` because they look at the tree you already have. Set `1`
before leaving those unattended.

### `LOOP_BRANCH_BASE` — base for the loop branch
The branch `loop/<id>` is created from. Default: `main`. Set to your trunk if it
has another name.

### `LOOP_FREEZE` — anti-cheat file patterns
Space-separated basename globs (e.g. `*_test.go`). At run start the runner
hashes every matching file in the workroot; the built-in `loop:frozen` gate
re-hashes each iteration and fails on drift. Add `gate frozen loop:frozen` to
the manifest (after the tests gate) to enforce it. A missing `frozen/index`
does not pass. The check fails closed. A pattern that matches nothing is a
preflight error. Freeze the tests, the fixtures, and the files that define
"done" so the loop cannot pass by editing its own goal or gate. Empty
(default) = nothing frozen.

### `LOOP_<ROLE>_MODEL` — pin a model to a role
Maps a manifest step's `model=<role>` to a model id. Example:
`LOOP_WRITER_MODEL=synthetic/hf:zai-org/GLM-5.2` makes every `model=writer`
step use that model. Empty = pi's default, which the runner does not look up.
An empty string is not a distinct model. For `two-model-critique`, the three
pins are `LOOP_WRITER_MODEL`, `LOOP_REVIEWER_MODEL`, and `LOOP_FIXER_MODEL`.
The shipped template stays `gated` because the tests gate is required.
`self-graded` and `cross-model` need no required gate and at least one
required scorecard. A soft card with that gate removed is `none`.
`cross-model` also needs writer, reviewer, and fixer all non-empty, with the
reviewer different from both acting turns.

### Scorecards, and the legacy `verdict=` gotcha

A judging turn names a card the operator wrote before the run:

```
turn critic prompts/02-critic.md model=critic required=0 scorecard=scorecards/critic.card
```

`scorecard=` is one path token. It does not consume the rest of the line, so
`required=0` may sit on either side. A line with both `verdict=` and
`scorecard=` does not parse. New templates use a scorecard. The judging prompt
says to write only the JSON path the runner names. The runner applies the
rule. There is no `passed` field in the model's JSON.

`required=0` makes a failed rule advice. It is logged and written into the
brief and the mend. It does not make a turn failure-proof. If the turn's `pi`
call errors outright, the runner abandons the rest of that iteration and marks
it failed, even when `required=0`.

`verdict=` stays for recipes that already have it. Do not scaffold a new one.
It is a regex over the model's own prose, matched line-anchored (the runner
prepends `(?m)`). In a manifest, `verdict=VALUE` and `system=VALUE` swallow
everything after them on that line. So `required=0` must come **before**
`verdict=`, or it is eaten and `required` stays at its default `true`.

```
turn critic prompts/02-critic.md model=critic required=0 verdict=^VERDICT: PASS\b
```

Wrong (this is a *hard* verdict, because `required=0` is swallowed):

```
turn critic prompts/02-critic.md model=critic verdict=^VERDICT: PASS required=0
```

The `\b` after `PASS` stops `VERDICT: PASSED` from satisfying `VERDICT: PASS`.

### `LOOP_APPROVE` — pass `--approve` to pi
`1` (default) passes `--approve` so pi auto-approves tool calls (a loop cannot
prompt). Set `0` only for very constrained setups. The `--approve` flag on
`loop run` overrides this.

### `LOOP_CONTEXT` — extra context string
Appended to every pi turn as a positional argument, after `--`, so a value of
`--no-session` is text and not a flag. Use for a short, stable reminder.
Usually empty — prefer `.loop/TODO.md` and `.loop/CONSTRAINTS.md`, which the
runner copies into `mend.md`.

### `LOOP_NO_CONTEXT_FILES` — skip project instruction files
`1` passes `--no-context-files` so a large instruction tree is not loaded on
every turn. Default `0` keeps project instructions.

### `LOOP_COMPACT` — what to do when pi compacts
The runner detects compaction events; it never triggers them. This sets the
policy when one is detected mid-run. All three still cut to a new empty
session. `allow` does not mean "keep reading the summary."
- `fail` (strict): fail the turn that compacted.
- `warn` (default): note it; the next turn starts a new session.
- `allow`: no warning, and the turn is not failed for compaction.

### `LOOP_STALL` — stop when nothing is changing
`stop` (default) or `continue`. After iteration 2, if the iteration is not ok
and the required-failure signature and the tree both match the previous
iteration, the run stops with `result: stalled`. `continue` turns that off.
The tree is `HEAD`, porcelain status (`.loop/` itself is not progress), and
the hashes in `LOOP_STALL_PATHS`. A new commit changes `HEAD`, so it does not
stall. The model can edit `stall.json`. Stall is a cost control. Freeze is
the anti-cheat.

### `LOOP_STALL_PATHS` — extra paths in the stall tree
Space-separated workroot-relative paths. Content-hashed even when gitignored.
Empty by default. Porcelain does not list ignored files, so an ignored path
is not progress unless it is named here. Porcelain sees `FINDINGS.md` appear
(`?? FINDINGS.md`). A later append does not change porcelain. `until-count`
sets `LOOP_STALL_PATHS=FINDINGS.md` so the hash changes. To treat an append
as progress, name the file here, or set `LOOP_STALL=continue`.

### `LOOP_TEST_CMD` — the test command
The command the `tests` gate runs (the scaffolded `gates/tests.sh` evals this).
Default: `go test ./...`. Change to match your stack: `npm test`, `pytest -q`,
`cargo test`. Exported to gates as an env var, so custom gates can use it too.
Recipe-owned keys the runner does not interpret, such as `LOOP_FINDINGS`, are
still passed through, and the runner warns that the key is unknown. That
warning is expected.

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
