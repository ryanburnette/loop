# AGENTS.md

Guidance for any agent working in this repo. Read this first, then read
`LOCAL.md` if present (gitignored, machine/operator-specific).

## What this is

`loop` is a single POSIX shell script that runs agentic loops with `pi`. A loop
is a directory (`loop.env` + `manifest` + `prompts/` + `gates/` + `hooks/`).
The runner acts, checks, feeds back, and repeats until a stopping rule fires.
Four starter templates live in `templates/`.

There is no build step and no dependencies beyond POSIX `sh`, `git`, `find`,
`xargs`, `shasum`, `grep`, `awk`, and `pi` on PATH.

## Skills to load

- `shell-scripting` — this repo is POSIX sh. Follow its conventions exactly:
  `#!/bin/sh`, `set -eu`, `test` over `[`, the `g_`/`b_`/`a_` variable prefixes,
  `fn_` helpers. Lint every change with `shellcheck` and `shfmt -i 3 -sr -ci -s`.
- `git-workflow` — for commits, branches, and PRs. Note the override below.
- `prose` — for README, AGENTS.md, prompt files, and commit messages.

## Architecture

### One file, two modes

`loop` is one self-contained script. It has two execution modes:

- **Manifest mode** (default): parse `manifest` as an ordered list of
  `turn | gate | hook` steps and run them each iteration.
- **Custom mode**: if `loop.sh` exists in the loop dir, source it and call
  `loop_setup` (optional) then `loop_iteration` (required) each iteration. Ignore
  the manifest entirely. `loop_iteration` can call the runner's own helper
  functions (`fn_run_turn`, `fn_setup_branch`, `fn_freeze`, `fn_builtin_frozen`).

A loop dir must have exactly one of `manifest` or `loop.sh`. Both is an error.

### The run loop (manifest mode)

Per iteration, the runner walks the manifest lines in order. Each line is parsed
by `set -- $mline` (whitespace-split), then keys after the path are consumed.
`verdict=` and `system=` eat the rest of the line so they can contain spaces —
they must be the last key on their line. `b_ok` starts at 1 each iteration; a
required gate or required verdict failing sets it to 0.

Success is decided at the end of the iteration, once, not per step:

- If the loop has an objective check (any required gate or required verdict),
  an iteration with `b_ok=1` succeeds and the runner exits 0. Otherwise it
  continues to the next iteration. Hitting `LOOP_MAX_ITER` without success exits 1.
- If the loop has no objective check, the runner just runs `LOOP_MAX_ITER`
  iterations and exits 0. There is nothing to check, so nothing to fail.

`g_objective` is computed once at startup by scanning the manifest for any
required gate or required verdict.

### Config layering

Defaults are applied first, then `loop.env` is sourced, then env overrides are
reapplied. This means a one-off `LOOP_MAX_ITER=2 loop ./x` wins over a `5` in
`loop.env` without editing the file. The override snapshot is captured *before*
defaults and restored *after* sourcing, so it survives `loop.env` redefining the
var. Do not reorder this; it's load-bearing.

### Workroot is always the containing git repo

`LOOP_WORKROOT` is `git -C <loop-dir> rev-parse --show-toplevel`. There is no
flag to point a loop at an external repo by design — a loop runs inside the repo
it lives in. `LOOP_BRANCH=1` creates `loop/<id>` off `LOOP_BRANCH_BASE` (default
`main`) and a `backup/loop-<id>` safety branch, refusing to start if the tree is
dirty.

### Paths

`fn_resolve` resolves manifest `path` values: absolute paths pass through,
`loop:*` builtins pass through, everything else is made absolute relative to the
loop dir. `@<abs-path>` is passed to `pi` for prompt files so `pi` finds them
regardless of `LOOP_WORKROOT`.

### Anti-cheat

`LOOP_FREEZE` is a space list of `find -name` patterns. At setup the runner
hashes matching files (excluding `.git` and the state dir) into
`state/<id>/frozen/`. The built-in `loop:frozen` gate re-hashes and exits 1 on
drift. Resume does not re-freeze — the original baseline is the anti-cheat point.

## Gotchas

- **`LOOP_MAX_ITER` counts iterations, not turns.** An iteration is one pass
  through the manifest. `double-check` has two `turn` steps, so `LOOP_MAX_ITER=1`
  runs two turns. Setting it to `2` runs four. The template sets `1` for this
  reason.
- **`verdict=` and `system=` consume the rest of the line.** They must be the
  last key on their manifest line. A `verdict=^VERDICT: PASS` followed by
  `required=0` would drop the `required=0`. Put multi-word keys last.
- **`LOOP_BRANCH` is the 0/1 flag; `LOOP_BRANCH_NAME` is the branch.** The runner
  exports `LOOP_BRANCH_NAME` (not `LOOP_BRANCH`) to gates/hooks. Reusing the
  flag name for the branch was a real bug; don't reintroduce it.
- **Custom mode runs setup hooks in a fixed order.** The runner calls
  `fn_setup_branch` then `fn_freeze` then writes `CURRENT_ID` *before*
  `loop_setup`. If your `loop_setup` needs the branch or frozen baseline, read
  them from state, not from a hook you expected to run first.
- **Resume skips branch creation and freezing on purpose.** Do not add re-freeze
  to resume. The whole point of resume is to compare against the original
  baseline, so a model editing a frozen file is caught by resuming.
- **`set -eu` is on.** Unset variables fail. The override-snapshot loop and
  `loop.env`/`loop.sh` sourcing toggle `set +u` temporarily. Keep that.
- **`pi` is called with `</dev/null`.** Turns are non-interactive; `pi` gets no
  stdin. A prompt that tells the model to ask questions will hang silently unless
  you handle it in the prompt or gate.
- **Gates and hooks run with `LOOP_WORKROOT` as cwd.** Use `$LOOP_ROOT` to
  address loop-dir files, not the current directory.

## Config schema

### loop.env keys

| Key                 | Default | Notes                                              |
|---------------------|---------|----------------------------------------------------|
| `LOOP_MAX_ITER`     | `5`     | iterations (manifest passes), not turns            |
| `LOOP_SESSION`      | `shared`| `shared` (one session) or `none` (stateless)       |
| `LOOP_BRANCH`       | `0`     | `1` = create `loop/<id>` off `LOOP_BRANCH_BASE`     |
| `LOOP_BRANCH_BASE`  | `main`  | base branch for `LOOP_BRANCH=1`                   |
| `LOOP_APPROVE`      | `1`     | pass `--approve` to `pi` (headless runs)           |
| `LOOP_FREEZE`       | (empty) | space list of `find -name` patterns                |
| `LOOP_CONTEXT`      | (empty) | text appended to every turn, expanded once         |
| `LOOP_<ROLE>_MODEL` | (empty) | model for manifest role `<role>`; empty = default  |

### manifest step types

| Type | Keys                                      | Effect                              |
|------|-------------------------------------------|-------------------------------------|
| `turn` | `model=`, `verdict=`, `required=`, `system=` | run `pi -p` with the prompt file  |
| `gate` | `required=`                               | run script; exit 0 = pass           |
| `hook` | (none)                                    | run script; exit ignored            |

Built-in gate path: `loop:frozen`. `path` resolves relative to the loop dir;
absolute paths pass through.

### Env exported to gates and hooks

`LOOP_ID`, `LOOP_ITERATION`, `LOOP_ROOT`, `LOOP_WORKROOT`, `LOOP_STATE_DIR`,
`LOOP_LOG`, `LOOP_PHASE`, `LOOP_LAST_TURN`, `LOOP_BRANCH_NAME`,
`LOOP_BRANCH_BASE`.

## Pre-commit checklist

```sh
shellcheck loop
shfmt -i 3 -sr -ci -s -d loop
shfmt -i 3 -sr -ci -s -w loop   # apply if the -d above shows a diff

# lint every gate/hook script in templates too
find templates -name '*.sh' -print0 | xargs -0 shellcheck
find templates -name '*.sh' -print0 | xargs -0 shfmt -i 3 -sr -ci -s -d
```

Both must be clean before a commit. The runner is the product; a script that
fails `shellcheck` or `shfmt` is a regression.

If you change behavior, verify the exact `pi` flags you rely on against
`pi --help` before committing. The runner depends on: `--print`, `--model`,
`--session-id`, `--session-dir`, `--no-session`, `--approve`,
`--append-system-prompt`, and `@<file>` prompt syntax.

## Git workflow override

The user has approved committing and pushing directly to `main` in **this repo
only**, to avoid losing work between turns. This overrides the `git-workflow`
skill's default "never push to main" rule for this repo. Do not apply branch
protection here (it would block the direct pushes the user wants). Still:

- Use Conventional Commits (`feat:`, `fix:`, `docs:`).
- Stage specific files; never `git add -A`.
- Write multi-line commit messages to `./tmp/commit-msg.txt` and use `git commit -F`.
- Inspect `git status` and `git diff` before committing.

## Testing changes

There is no automated test suite yet. Validate by running a template against a
scratch repo with a real `pi` model (it's fine to burn tokens for this). The
common smoke tests:

- A failing test → `until-green` should fix it and exit 0, or hit the cap and
  exit 1.
- `LOOP_FREEZE` a test file, then edit it during a run and `--resume` → the
  `loop:frozen` gate should fail.
- `double-check` at `LOOP_MAX_ITER=1` should run exactly two turns (writer +
  critic), not four.
- `LOOP_BRANCH=1` should leave you on `loop/<id>` with a `backup/loop-<id>` branch.

## What not to do

- Don't add a build step or runtime dependency. `loop` stays one POSIX script.
- Don't add an external-workroot flag. Loops run inside their containing repo.
- Don't make `loop.env` templated. Prompts stay static files; `LOOP_CONTEXT`
  carries the per-run text.
- Don't re-freeze on resume.
- Don't push `loop` onto the user's PATH. They install it themselves.
