# Clean up loop: teach the concepts, fix the review findings

Rewrite the README into a teaching document and address the open code/doc
concerns from a review. The loop's reviewer (grok-4.6) decides when the work
is acceptable: it is instructed to think hard about whether something *needs*
to happen, not to find nits. If it is on the fence it opens a GitHub issue and
approves. So: do the work that genuinely matters, keep the suite green, and
converge.

## 1. README rewrite (headline)

`README.md` currently reads like a changelog of design prompts — "rewrite of
the POSIX loop", version notes, flag tables. Replace it with a lesson in the
looping material this project is built on, stated clearly: what `loop` is, why
it exists, and how to use it. No version-history language. Lead with what it
is and the problem it solves.

The README must teach these concepts, each with a concrete example implemented
with this `loop` (real `loop.env` / manifest / prompt snippets, not abstractions):

- **The core loop.** Act → check something the model cannot argue with → feed
  the result back → repeat until a stopping rule fires. Why the check must be
  objective (the model is an optimistic narrator of its own work).
- **Objective gates vs. soft verdicts.** A required gate or required verdict is
  the objective; success is decided once per iteration, not per step. A loop
  with an objective exits 0 on the first passing iteration and 1 at the cap; a
  loop with no objective runs `MaxIter` times and exits 0. Show both.
- **The four patterns** as worked examples: `until-green` (test gate),
  `double-check` (soft critic verdict, no hard gate), `two-model-critique`
  (cross-model review + test gate), `until-count` (discovery, soft DONE rule +
  hard cap). Show the actual `loop init` scaffolds and how to adapt them.
- **Compaction: avoid, detect, react.** Why auto-compaction is lossy for loops
  (the narrator writes the history it will read next). How `loop` avoids it
  (prefer `none`, cap turns per shared session, re-feed the check every time via
  the handoff, don't pull the world into context, use big windows), detects it
  (`compaction_start` / `contextUsage` events), and reacts (`LOOP_COMPACT` =
  fail | warn | allow). Session policies `none | shared | fork` and when each
  applies.
- **The handoff is the source of truth, not model memory.** Runner-authored
  `handoff.md`: goal, constraints, last gate result, diffstat, session facts,
  frozen status. Attached as `@handoff.md` on every turn after the first.
- **Freeze / anti-cheat.** `LOOP_FREEZE` + the built-in `loop:frozen` gate:
  snapshot hashes at run start, re-check each iteration, fail on drift. So the
  loop cannot pass by editing its own gate or tests.
- **Disposable branches.** `LOOP_BRANCH=1` makes `loop/<id>` off the base and a
  safety `backup/loop-<id>`, refuses a dirty tree. Keeps the loop off your
  working tree.
- **Bounded retries.** The iteration cap is the hard backstop. Always have one.
- **The control plane.** `state/<id>/control` (`pause` / `resume` / `stop` /
  `set KEY=VALUE`) read between steps, and `SIGINT`/`SIGTERM` as stop. The hook
  for a future interactive UI.

Then a short, current "how to use it" section: `loop init`, `loop run`, `-C`,
`loop status`, `loop freeze` / `frozen?`, `--resume`. Match the actual binary
behavior (verify against `loop help` and the code — do not invent flags).

Preserve the conceptual notes — they are the point. The archived design docs
under `tmp/archive/` (DESIGN-v0.2.md, DESIGN-v0.3.md, etc.) and `DESIGN.md` are
reference material; read them, carry the concepts into the README accessibly,
do not link to archives.

## 2. Doc drift

- The control file `set` directive is `set KEY=VALUE` (see `internal/control`
  and its test), not `set KEY VALUE`. Fix anywhere docs say otherwise (the
  skill under `skills/loop-compose/` included).
- `matchVerdict` does a prefix match: `^VERDICT: PASS` also matches
  `VERDICT: PASSED`. Either anchor the verdict patterns in the scaffold
  templates and skill examples (e.g. `^VERDICT: PASS\b`) or document the
  prefix-match behavior clearly. Pick one and be consistent.

## 3. Code concerns

- `internal/run/run.go`: `Run` is one ~400-line function. Extract
  `runTurn` / `runGate` / `runHook` helpers (each returning a small result
  struct) so the iteration loop reads like the spec. Behavior must not change;
  the suite stays green.
- `internal/run/run.go`: a `pi.Run` error on a turn records nothing to
  `gate-log.md`, so the handoff's "last gate" section carries the previous
  iteration's gate. Log turn errors to `gate-log.md`.
- `cmd/loop/main.go`: flags propagate to config via `os.Setenv` (a global side
  channel). Move `--branch` / `--base` / `--approve` / `--context` / `--model`
  onto `run.Options` and build the overlay in `run.Run` instead of mutating
  process env.
- `internal/freeze/freeze.go`: `matchFiles` walks the entire workroot (only
  `.git` and run-state pruned). Prune `node_modules`, `vendor`, and common
  build-output dirs so a broad freeze pattern does not hash gigabytes.
- `internal/freeze/freeze.go`: `cmdFreeze`'s manual snapshot excludes only
  `.freeze-tmp`, not run-state dirs — inconsistent with the run-time snapshot.
  Align the exclusions.
- Add one-line godoc comments to exported functions that lack them
  (`Defaults`, `Load`, `Scaffold`, `Run`, etc.).

## 4. Product stance: `.loop/` is operator scratch

`.loop/` is gitignored in this repo (see `.gitignore`) — the recipe that drives
a loop is operator-owned scratch, not product. The `loop-compose` skill
currently tells agents to `git add .loop && git commit`. Reconcile the skill
with this stance: advise gitignoring `.loop/` (the recipe is operator scratch),
and note the trade-off honestly. Do not break the skill's other guidance.

## Invariants (do not break)

- `go test ./...` stays green. Do not edit `*_test.go` or `testdata/` to make
  it pass — `LOOP_FREEZE=*_test.go` enforces this.
- `flag.FlagSet` only. No Cobra/Viper/Bubble Tea. No Makefile. lipgloss v1.
- `loop.env` is `KEY=VALUE`, never sourced. Default session is `none`. Never
  call `pi` compact. Workroot is the containing git repo. Resume does not
  re-freeze.
- See `AGENTS.md` and `DESIGN.md` for the full contract.

Commit each logical change on this branch with a Conventional Commit message.
The reviewer inspects the actual diff (`git diff main...HEAD` and `git diff`)
against this task — not your summary.
