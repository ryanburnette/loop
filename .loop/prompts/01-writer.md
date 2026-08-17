# Writer

This is `loop2`'s own repo, running its own binary on itself for the first
time with a real reviewer model (not `fake-pi`). The task:

`internal/scaffold/scaffold_test.go` was just added — 15 tests covering
`Scaffold()` and the four `loop init` templates (`until-green`,
`double-check`, `two-model-critique`, `until-count`), all passing. Do a
genuinely critical pass over `internal/scaffold/scaffold.go` and
`internal/scaffold/templates.go`:

- Read every template's `loop.env`, `manifest` (where present), and prompt
  files for real problems: wrong claims about the runner's actual behavior
  (check against `DESIGN-v0.3.md` and the real code in `internal/run`,
  `internal/manifest`, `cmd/loop`), inconsistent tone or structure between
  templates, anything that would mislead a user following `loop init`.
- Read `Scaffold()` itself for real edge cases: what happens with a
  zero-length template name that isn't literally `""`? A `dir` argument
  that's relative vs absolute? Deterministic file mode assignment — is
  `gates/`/`hooks/` really the only directory that needs `0o755`, or could a
  future template put an executable somewhere else?
- Fix anything real you find. Add a test for it if it's the kind of thing a
  test can catch (append to `scaffold_test.go`... except you can't: it's
  frozen for this run — note what you'd add in your summary instead, and fix
  the underlying code/content issue directly).

If you genuinely find nothing wrong after a real critical pass, say so
explicitly and explain what you checked — do not manufacture busywork.

Hard rules:

- Do not modify `*_test.go` or `testdata/`.
- Do not modify `.loop/loop.env`, `.loop/manifest`, `.loop/gates/tests.sh`,
  or any `.loop/prompts/*.md`.
- Run `go test ./...` before finishing.

Summarize what you found and changed (or why you found nothing) in a few
bullets.
