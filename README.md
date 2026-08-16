# loop

`loop` runs agentic loops with `pi`. You describe a loop in a directory —
`loop.env`, a `manifest`, and `prompts/` + `gates/` + `hooks/` — and `loop`
runs it: act, check the result against something more objective than the
model's own opinion, feed the result back, and repeat until a stopping rule
fires.

This is the Go rewrite of the POSIX `loop` (sibling `../loop` repo). The spec
is [DESIGN.md](DESIGN.md).

## Build

```sh
go build -o ./tmp/loop ./cmd/loop
```

There is no install target. Put the binary wherever you like.

## Usage

```
loop <dir> [flags]
loop run <dir> [flags]
loop run --prompt F --gate C
loop status <dir>
loop freeze <dir>
loop frozen?
loop help
loop version
```

Flags go before or after the directory: `loop run myloop -q` and
`loop run -q myloop` are equivalent.

```
--max-iter N          override LOOP_MAX_ITER
--session MODE        none|shared|fork (default none)
--branch              create loop/<id> branch
--base BRANCH         LOOP_BRANCH_BASE
--approve             pass --approve (default true)
--context TEXT        extra context
--model role=id       repeatable
--compact MODE        fail|warn|allow
--pi PATH             pi binary
--resume ID           resume a run
--prompt FILE         one-shot prompt file (no dir needed)
--gate CMD|PATH       one-shot gate command or script
-v                    verbose
-q                    quiet (final line only)
--json                machine events, one JSON object per line
-V, version           print version
```

## A loop directory

```
myloop/
  loop.env        KEY=VALUE config (LOOP_MAX_ITER, LOOP_SESSION, LOOP_FREEZE, ...)
  manifest        one step per line: turn | gate | hook
  prompts/        prompt files referenced by turns
  gates/          gate scripts referenced by gates
  hooks/          hook scripts referenced by hooks
```

Manifest steps:

```
turn writer   prompts/writer.md   model=writer
gate tests    gates/tests.sh
hook fmt      hooks/fmt.sh
```

`turn` accepts `model=`, `system=` (rest of line), `verdict=` (rest of line),
and `required=` (default 1). `gate` accepts `required=` (default 1). A loop
with an objective — any required gate or required verdict — exits 0 on the
first passing iteration and 1 at the cap. A loop with no objective runs
`MaxIter` times and exits 0.

## Status and resume

While a run is in progress, `loop status <dir>` prints the current run id,
iteration, status line, and `meta.env`. Each run also writes a liveness file
to `state/<id>/status` on every step change.

Resume a stopped or failed run by id:

```sh
loop run myloop --resume 20260816T211458Z-58884
```

Resume does not re-freeze: it compares against the snapshot taken when the run
started.

## Signals

`SIGINT` / `SIGTERM` stops the run cleanly: the current step finishes if it
can, the run writes `SUCCESS=0`, and the final line is `STOPPED` (not
`FAILED`). Mid-run control is also possible via the `state/<id>/control` file
(`pause`, `resume`, `stop`, `set KEY VALUE`).

## Tests

`go test ./...` is the gate. It uses `testdata/fake-pi`, never a real model.
