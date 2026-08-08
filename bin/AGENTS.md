# bin

`bin/` contains the repository-root operator scripts and the `bintest` proof
tier. It is governed by the specifications under `bin/project/` and is not
independently versioned.

## Tests

Run the default gate from the repository root with
`go test ./bin/bintest/...`.

The test module is the hermetic layer. The bash orchestration tier is the
manual layer. There is no composed layer and no live layer.

There is no environmental precondition beyond the Go toolchain. The `GOWORK`
mode is `workspace`; the test module resolves its sibling modules through the
repository workspace.

The repository-root aggregate gate remains in the root module because it fans
out across modules and would recurse from within `bintest`.
