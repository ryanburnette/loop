# loop2 v0.3.0

v0.1 built the runner. v0.2 made it watchable and correct under a parent
loop's environment. v0.3 changes how a human actually uses it day to day,
and adds a skill so an agent can set it up on your behalf. Implement this
file. `DESIGN.md` and `DESIGN-v0.2.md` are still true except where this file
overrides them.

## The reframe: `loop` operates on the current directory

Through v0.2, `loop run <dir>` (or `loop <dir>`) took a path to a loop
directory that could live anywhere. That flow is gone as the primary
interface. From v0.3:

- `loop` is a single binary on `PATH`, same as `git` or `pi`.
- A project keeps its loop recipe in `.loop/` at the project root.
- You `cd` into the project and run `loop run`, `loop status`, etc. with no
  positional directory argument — they operate on `.loop/` in the current
  directory.
- `-C DIR` is the explicit escape hatch (same convention as `git -C`): it
  overrides both "current directory" and "`.loop/` inside it" — `loop run
  -C /path/to/proj` runs `/path/to/proj/.loop`. Scripts, tests, and anyone
  who doesn't want to `cd` use this. It is not the primary interface.

No upward directory search (unlike `.git`). `.loop/` must be in the
directory `loop` is told to use (cwd, or `-C`'s argument). If it is not
there, fail with a message that says so and suggests `loop init`. Searching
upward is a deliberate non-goal here — it adds ambiguity (which `.loop/` did
I just run?) for a case `-C` already covers explicitly.

New package `internal/loopdir` owns this resolution. Suggested shape:

```go
package loopdir

const DefaultDir = ".loop"

// Resolve returns the loop directory to use. If explicit is non-empty it
// wins (joined onto cwd if relative, passed through if absolute).
// Otherwise cwd/.loop is used. Resolve does not check existence.
func Resolve(cwd, explicit string) (string, error)

// Missing reports whether dir does not look like an initialized loop dir:
// no loop.env, no manifest, and no non-empty prompts/ directory.
func Missing(dir string) bool

// MissingMessage is the error text for the Missing(dir) case. It must
// mention "loop init".
func MissingMessage(dir string) string
```

`cmd/loop` resolves the directory once (via `loopdir.Resolve`, honoring a
new `-C` flag shared by `run`/`status`/`freeze`/`init`) and passes the
resulting path into `run.Options.Dir` exactly as before. Do not change
`internal/run`'s contract with its directory — only how that directory gets
chosen.

## `.loop/` layout

```
.loop/
  loop.env
  manifest            # optional now — see below
  prompts/
    01-writer.md
    02-reviewer.md
  gates/
    tests.sh
  hooks/
    notify.sh
  state/              # created at runtime, not authored
```

`state/` still lives under the loop dir (`.loop/state/`), same as v0.1/v0.2.
Ship a `.gitignore` note in the skill/docs telling users to gitignore
`.loop/state/` (not `.loop/` itself — the recipe belongs in version
control).

## Convention-derived manifest (the point of numbered files)

`manifest` is now optional. If `.loop/manifest` doesn't exist, derive one
from directory contents:

- `prompts/*.md`, sorted lexically by filename, each becomes a required
  `turn` step in that order.
- `gates/*` (any regular file, any name), sorted lexically, each becomes a
  required `gate` step, run after all turns.
- `hooks/*`, sorted lexically, each becomes a `hook` step, run last.
- A step's `Name` (used for `LOOP_<NAME>_MODEL` lookup and for display) is
  the filename with its extension removed and a leading `NN-` numeric
  prefix stripped: `01-writer.md` → `writer`, `writer.md` → `writer`,
  `02-reviewer.md` → `reviewer`. Its `Path` keeps the real filename
  (`prompts/01-writer.md`), because that's what has to be opened.
- If there is no `manifest` and no files under `prompts/`, `gates/`, or
  `hooks/`, fail with a clear error (nothing to run).

This gives the everyday case — drop numbered prompt files and a gate script
in folders, run `loop run`, done — with zero DSL to learn. `manifest` stays
available, unchanged, for anyone who wants explicit interleaving (turn,
gate, turn, gate), soft verdicts, `system=`, or step names that don't match
the derivation rule.

Suggested shape in `internal/manifest`:

```go
// Load reads dir/manifest if present, else derives one by convention
// from dir/prompts, dir/gates, dir/hooks.
func Load(dir string) (*Manifest, error)

func Derive(dir string) (*Manifest, error)
```

`internal/run` should call `manifest.Load(dir)` instead of
`manifest.ParseFile(filepath.Join(dir, "manifest"))` directly, so both paths
go through one entry point.

## `loop init [template]`

Scaffolds `.loop/` in the target directory (cwd, or `-C`'s argument).
Refuses to overwrite an existing `.loop/` (fail with a clear message; no
`--force` in this version — deliberately, so it can't clobber a recipe by
accident).

Templates, ported from `~/git/ryanburnette/loop/templates/` and adapted to
the `.loop/` + numbered-prompt convention:

- `until-green` (default if no template named): writer turn + test gate.
- `double-check`: writer turn + critic turn, soft verdict, no hard gate.
- `two-model-critique`: writer, reviewer (verdict), fixer, test gate.
- `until-count`: hunt turn + counting gate.

`loop init` with no argument uses `until-green`. `loop init two-model-critique`
scaffolds that one. Each template's prompt files carry real, useful starter
content (not lorem-ipsum placeholders) — same spirit as the POSIX repo's
templates, reworded for the numbered-file convention.

## UI overhaul

Still lipgloss, still no alt-screen, no Bubble Tea, no web UI — an
append-safe, styled scrolling log, so it stays pipeable and diffable in a
saved run log. What changes is how much it tells you and how legible that
is.

Required elements:

1. **Header panel** at the start of a run: loop id, session mode,
   iteration cap, and now git info — current branch, short HEAD, a dirty/
   clean indicator. If `LOOP_BRANCH=1`, show the branch the run is working
   on (`LOOP_BRANCH_NAME`) distinctly from the git branch you started on.
2. **Step lines**, visually distinct by type (turn vs. gate vs. hook) —
   color and/or a short label/glyph, not color alone (must still be legible
   with `NO_COLOR=1`, where it degrades to a plain text label).
3. **Live status while a turn runs**: elapsed time, last tool name, context
   percent when known — this exists from v0.2 (`OnEvent`); make it part of
   the same visual language as the rest (same styling conventions), don't
   leave it as a bolted-on plain line.
4. **Gate verdicts**, clearly pass/fail, with the first few lines of
   failure output shown (not just "FAIL" — a human reading the terminal
   should see *why* without opening `gate-log.md`).
5. **Summary footer** on exit: elapsed, iterations used, success/fail, and
   the branch name if one was created.

New package `internal/gitinfo`, used only for display — a failure here must
never affect run behavior or exit codes:

```go
package gitinfo

type Info struct {
	Repo     bool   // false if dir is not inside a git repo
	Branch   string // "" if detached HEAD or unknown
	ShortSHA string
	Dirty    bool
	DirtyN   int
}

// Collect gathers git info for dir. It never returns an error; any
// failure (not a repo, git missing, etc.) yields a zero Info with
// Repo=false so callers render a blank/dash instead of failing the run.
func Collect(dir string) Info
```

Hard requirement, testable: with `NO_COLOR=1` set (or output not a TTY —
lipgloss should already handle the TTY case; `NO_COLOR` needs an explicit
check), the human renderer must emit **zero ESC bytes** (`\x1b`). Verify
this yourself before calling the UI work done — it's exactly the kind of
thing that's easy to get "mostly" right and wrong in one code path.

## The skill

Ship a pi skill so a user can describe what they want in conversation and
the agent composes `.loop/` for them, instead of hand-writing `loop.env`
and prompt files.

Location: `skills/loop-compose/SKILL.md` in this repo (source of truth,
versioned with the tool). Follow the Agent Skills format pi implements —
YAML frontmatter with `name: loop-compose` and a specific `description:`
(what it does and when to use it — "the user wants to set up or modify a
`.loop/` directory to run pi in a loop" territory, not "helps with loops").
Add `skills/loop-compose/references/` for the material that shouldn't be
inline in `SKILL.md` (progressive disclosure — only the description is
always in context):

- `references/patterns.md` — condensed from
  `~/git/ryanburnette/research-loops/03-patterns.md`: the five patterns
  (double-check, until-green, build/lint gate, two-model critique,
  until-count) and the "picking one" decision guide, rewritten against the
  actual v0.3 `.loop/` format (numbered prompts, `loop.env` keys, `loop
  init` templates) instead of the POSIX repo's `loop.env`/`manifest` shape.
- `references/guardrails.md` — condensed from
  `~/git/ryanburnette/research-loops/04-guardrails.md`: a check must be
  more objective than the model, bound the retries, freeze what proves
  success, keep loops on disposable ground (`LOOP_BRANCH=1`). The skill
  must push back on "loop until it looks right" with no gate — it should
  steer the user toward an objective check or an explicit soft-check +
  hard-cap combo, not just do whatever's asked.
- `references/loop-env.md` — the `loop.env` key reference (`LOOP_MAX_ITER`,
  `LOOP_SESSION`, `LOOP_BRANCH`, `LOOP_BRANCH_BASE`, `LOOP_FREEZE`,
  `LOOP_<ROLE>_MODEL`, `LOOP_APPROVE`, `LOOP_CONTEXT`, `LOOP_COMPACT`,
  `LOOP_TEST_CMD`) with what each does and a sane default.

`SKILL.md` itself should be short: what the skill is for, the questions to
ask the user (what's the check? tests, a script, a reviewer, a count?), the
decision of which pattern/template fits, then concretely: run `loop init
<template>`, edit the numbered prompt files and `loop.env` for the actual
task, tell the user the exact `loop run` command. It should point at the
reference files rather than inline everything.

This is prose/instructional content, not code — write it last, after the
CLI and directory format are real, so it describes the actual tool and not
an aspiration. Verify it by using it: compose a `.loop/` directory by
following the skill's own instructions for at least two different
scenarios (e.g. "make tests pass" and "review my PR with a second model"),
then actually run `loop run` against the result (against `fake-pi` is fine)
and confirm it works. Document that you did this in your summary — don't
just assert the skill is good, show the transcript of using it.

## Hard rules (same as before, restated because they matter here)

- Do not edit `*_test.go` or anything under `testdata/`.
- Do not touch `loop.env`, `manifest`, `gates/tests.sh`, `CHECKLIST-v0.3.md`,
  or this file inside the loop dir running this work — they're frozen.
- `loop.env` stays `KEY=VALUE` only; no `$()` or backticks (already
  enforced by `internal/config`, don't weaken it while touching that code).
- Never call `pi`'s own compact command from the runner.
- No Cobra, Viper, Bubble Tea, web UI, external-workroot flag, `loop.sh`.

## Implementation order

1. `internal/loopdir` (resolve + missing-dir message) and the `-C` flag
   wired into `run`/`status`/`freeze`/`init` in `cmd/loop`. Drop the old
   `loop <dir>` / `loop run <dir>` positional-dir flow — replace with cwd
   default + `-C`.
2. `manifest.Load` / `manifest.Derive`, wired into `internal/run` in place
   of the direct `ParseFile` call.
3. `loop init` and its four templates.
4. `internal/gitinfo`, then the UI overhaul using it (header, step lines,
   gate verdict detail, summary footer, `NO_COLOR` correctness).
5. The skill, written and hands-on verified against the real CLI from steps
   1–4.

Keep `clearLoopEnv`-style isolation in any new or touched test that loads
config or runs the CLI — this bit a previous round (`covloop`) and the
pattern must be applied to every new test file that touches `LOOP_*`, not
just the packages that already had it.
