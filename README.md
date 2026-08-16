# loop

`loop` runs agentic loops with `pi`. You describe a loop in a directory —
prompts, gates, hooks — and `loop` runs it: act, check the result against
something more objective than the model's own opinion, feed the result back, and
repeat until a stopping rule fires.

This directory is the Go rewrite. The spec is [DESIGN.md](DESIGN.md). The POSIX
runner that is writing this lives in the sibling `../loop` repo.

Not installable yet. `go test ./...` is the gate.
