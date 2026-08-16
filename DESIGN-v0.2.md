# loop2 v0.2.0

v0.1 (what is on `main` now) implements the runner: manifest, config, freeze,
session policy, `pi --mode json`, the iteration loop, a lipgloss banner, and
the CLI. It is enough to go green against fake-pi.

v0.2 makes it a runner you can watch and interrupt, and a suite that stays
green when a parent loop exports `LOOP_*`. Implement this file. Do not invent
a different product. `DESIGN.md` is still the v0.1 spec; this file is the
delta.

## Bugs to fix

### 1. Tests must ignore ambient `LOOP_*`

`config.Load` correctly applies process env. The tests must not. A parent
loop (this one) exports `LOOP_MAX_ITER`, `LOOP_BRANCH`, `LOOP_FREEZE`. If
the suite reads those, `TestLoadLoopEnv` and `TestUntilGreenSucceeds` fail
for reasons that have nothing to do with the code under test.

The existing tests now call `clearLoopEnv`. Keep that. Do not "fix" the
tests by stopping `Load` from reading process env — that is the product.

`TestProcessEnvBeatsFileOverlayBeatsEnv` is the contract: file < env < overlay.

### 2. Handoff reads TASK.md / CONSTRAINTS.md from the workroot

v0.1 loads them from the loop dir. They live next to the code
(`DESIGN.md`, `TASK.md`, `CONSTRAINTS.md` at the repo root). A loop dir
is just the recipe. `loadGoal` and `loadConstraints` must prefer
`$LOOP_WORKROOT`, then the loop dir, then `LOOP_CONTEXT`.

`TestHandoffReadsGoalFromWorkroot` covers this.

### 3. Write `state/<id>/status`

v0.1's `loop status` reads a file the Go runner never writes. Write it
on every step change, same shape as the POSIX runner:

```
iteration 1/8 · phase: turn writer · elapsed 12s
```

and `phase: success` / `phase: failed` / `phase: done` at the end.

`TestWritesStatusFile` covers this.

### 4. `--json` emits runner events

v0.1 swallows output when `JSON` is set. Emit one JSON object per line:

```
{"type":"header","id":"...","session":"none","maxIter":5}
{"type":"iteration","i":1,"n":5}
{"type":"step_start","kind":"turn","name":"writer"}
{"type":"step_done","ok":true,"note":"done","elapsed":3}
{"type":"success","iter":1,"state":"state/<id>"}
```

Human renderer stays the default. `--json` is for scripts.

`TestJSONEmitsRunnerEvents` covers this.

### 5. Resume does not re-freeze

Already specified. `TestResumeDoesNotRefreeze` now exists: start with
`LOOP_FREEZE`, edit a frozen file, `--resume`, the `loop:frozen` gate
must fail. If resume re-hashes, the test fails.

### 6. SIGINT / SIGTERM is stop

On `SIGINT` or `SIGTERM`, treat it as a control `stop`: finish the
current cheap step if you can, write `SUCCESS=0`, exit 1. Do not leave
the process hanging in the pause poll. A test is optional (signals in
`go test` are messy); wire it and say so in the commit.

### 7. `lipgloss` is a direct dependency

`go.mod` currently lists it `// indirect`. `go get github.com/charmbracelet/lipgloss@v1.1.0`
and keep it in the `require` block without `indirect`.

## Live turn output (the reason for Go)

v0.1 prints the last tool *after* the turn ends. v0.2 streams.

`internal/pi.Run` should accept an optional `OnEvent func(Event)` (or
equivalent) and call it as jsonl lines arrive. The runner uses that to
print, during the turn:

- last tool name and a short arg (`read loader.go`, `bash go test`)
- context percent when `session_status` lands
- elapsed

On a TTY this can be one updating status line (`\r`). Off a TTY
(background, log) print a new timestamped line per tool, same as v0.1's
non-tty habit. Do not add Bubble Tea.

`-v` still prints extracted assistant text as it lands (text deltas).

No new test required for the live line itself (hard to assert a `\r`).
The `OnEvent` hook should be unit-tested: parse a fixture, count tool
events delivered.

## Control plane, still file-based

Keep `state/<id>/control`. Between steps, and inside the pause wait,
keep consuming it. v0.2 does not add an interactive editor. It does
make `pause` wake on SIGINT as `stop` rather than spinning until
someone writes `resume`.

## CLI hygiene

- `--json` works as above.
- `loop status <dir>` prints the live `status` file (now that it exists)
  plus `SUCCESS` / `FINISHED_AT` from `meta.env`.
- One-shot `--prompt` / `--gate` still works. Prefer writing the temp
  loop dir under `./state/oneshot-<id>/` rather than a random
  `loop-oneshot-*` in cwd, so it is gitignored.
- `help` still prints version then usage.

## What not to do

Same as `DESIGN.md`. Still no Cobra, Viper, Bubble Tea, web UI,
external-workroot flag, `loop.sh`, or calling `pi` compact.

Do not edit `*_test.go` or `testdata/` to go green. Isolation helpers
already in the tests stay.

## Implementation order

1. Keep tests isolated (`clearLoopEnv` already landed). Confirm
   `go test ./...` under a parent `LOOP_MAX_ITER=8 LOOP_BRANCH=1` is
   green for the v0.1 tests, red only for the new ones.
2. Handoff from workroot. Status file. `--json` events. Resume freeze.
   Signal handling. Direct lipgloss require.
3. `OnEvent` + live tool line.
4. One-shot dir under `./state/`. Status command polish.

Commit as you go on the branch the runner created.
