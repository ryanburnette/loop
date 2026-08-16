# loop

`loop` runs agentic loops with `pi`. A project keeps its loop recipe in a
`.loop/` directory — `loop.env`, an optional `manifest`, and `prompts/` +
`gates/` + `hooks/` — and `loop` runs it: act, check the result against
something more objective than the model's own opinion, feed the result back,
and repeat until a stopping rule fires.

This is the Go rewrite of the POSIX `loop` (sibling `../loop` repo). The spec
is [DESIGN.md](DESIGN.md), evolved by [DESIGN-v0.2.md](DESIGN-v0.2.md) and
[DESIGN-v0.3.md](DESIGN-v0.3.md).

## Build

```sh
go build -o ./tmp/loop ./cmd/loop
```

There is no install target. Put the binary wherever you like.

## Usage

`loop` operates on `.loop/` in the current directory, like `git` or `pi`:

```
loop run [flags]              run .loop/ in the current directory
loop run -C DIR [flags]      run a specific project's .loop/
loop run --prompt F --gate C  one-shot, no .loop/ needed
loop status [-C DIR]          show the current run
loop freeze [-C DIR]         snapshot frozen files for manual inspection
loop frozen?                 check a freeze snapshot (env-driven)
loop init [template] [-C DIR] scaffold .loop/
loop help
loop version
```

`-C DIR` may name either the loop directory itself (`.../proj/.loop`) or the
project directory that contains `.loop/` (`.../proj`); in the second case
`DIR/.loop` is used. There is no upward search for `.loop/`.

Flags may appear before or after `-C`: `loop run -q -C proj` and
`loop run -C proj -q` are equivalent. The old `loop <dir>` /
`loop run <dir>` positional form is gone — extra arguments are rejected.

Templates for `loop init`: `until-green` (default), `double-check`,
`two-model-critique`, `until-count`.

```
--max-iter N          override LOOP_MAX_ITER
--session MODE        none|shared|fork (default none)
--branch              create loop/<id> branch
--base BRANCH         LOOP_BRANCH_BASE
--approve            pass --approve (default true)
--context TEXT        extra context
--model role=id       repeatable
--compact MODE        fail|warn|allow
--pi PATH             pi binary
--resume ID           resume a run
--prompt FILE         one-shot prompt file (no dir needed)
--gate CMD|PATH       one-shot gate command or script
-C DIR                project or loop directory (default ./.loop)
-v                    verbose
-q                    quiet (final line only)
--json                machine events, one JSON object per line
-V, version           print version
```

## A .loop/ directory

```
.loop/
  loop.env        KEY=VALUE config (LOOP_MAX_ITER, LOOP_SESSION, LOOP_FREEZE, ...)
  manifest        OPTIONAL — omit it to derive steps from file names
  prompts/        prompt files → turn steps (lexical order)
  gates/          gate scripts → gate steps
  hooks/          hook scripts → hook steps
  state/          created at runtime — gitignore this
```

If there is no `manifest`, the runner derives one: `prompts/*.md` become turn
steps, `gates/*` become gate steps, `hooks/*` become hook steps — all
required, sorted lexically by filename. A step's role name is the filename
with its extension and a leading `NN-` numeric prefix stripped
(`01-writer.md` → `writer`).

A written `manifest` is one step per line:

```
turn writer   prompts/01-writer.md   model=writer
gate tests    gates/tests.sh
hook fmt      hooks/fmt.sh
```

`turn` accepts `model=`, `system=` (rest of line), `verdict=` (rest of line),
and `required=` (default 1). `gate` accepts `required=` (default 1). A loop
with an objective — any required gate or required verdict — exits 0 on the
first passing iteration and 1 at the cap. A loop with no objective runs
`MaxIter` times and exits 0.

## Status and resume

While a run is in progress, `loop status` prints the current run id,
iteration, status line, and `meta.env`. Each run also writes a liveness file
to `.loop/state/<id>/status` on every step change.

Resume a stopped or failed run by id:

```sh
loop run --resume 20260816T211458Z-58884
```

Resume does not re-freeze: it compares against the snapshot taken when the run
started.

## Signals

`SIGINT` / `SIGTERM` stops the run promptly: an in-flight turn or gate
subprocess is terminated (the process group is killed), the run writes
`SUCCESS=0`, and the final line is `STOPPED` (not `FAILED`). Mid-run control
is also possible via the `.loop/state/<id>/control` file (`pause`, `resume`,
`stop`, `set KEY VALUE`).

## Tests

`go test ./...` is the gate. It uses `testdata/fake-pi`, never a real model.
