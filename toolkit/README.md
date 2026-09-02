# toolkit

`toolkit` is built **spec-first**: the design documents under `specs/design/`
define the contract, and an automated build loop writes the code, tests it, and
proves it against the spec. Every behavior traces to a requirement id, and every
requirement id to a test. See [how the spec system works](../docs/spec-system.md).

## What toolkit is

A Go library of ready-made tools for
[`agentkit`](../agentkit/README.md) consumers. Six constructors, one package:

| Tool    | What it does                                                    |
|---------|-----------------------------------------------------------------|
| `Bash`  | runs `bash -c` from a starting directory; nonzero exit is data   |
| `Read`  | numbered lines with `offset`/`limit` paging                      |
| `Write` | replaces a file's content, creating parents                      |
| `Edit`  | exact, unique-match string replacement (`replace_all` optional)  |
| `Glob`  | `**` globs, newest file first                                    |
| `Grep`  | RE2 search with ripgrep-style modes and context, in pure Go      |

Every constructor takes an explicit root directory and returns
`(agentkit.Tool, error)`. The five file tools are confined to that root and
symlink-aware; `Bash` merely starts there. Each result is capped at 30,000
characters with an explicit truncation marker. The tools behave like the Claude
Code harness tools of the same names, so a model already knows how to use them.

```go
read, err := toolkit.Read(project)
grep, err := toolkit.Grep(project, toolkit.WithSkip(".git", "node_modules"))
conv := agentkit.Config{Tools: []agentkit.Tool{read, grep}}
```

`Glob` and `Grep` walk the tree honoring `.gitignore` and `.ignore` files; a
consumer adds its own gitignore-syntax exclusions with `WithSkip`. Options are
typed per tool, so passing one to a tool it does not apply to is a compile error.

Later phases add `WebSearch` and `WebFetch` in their own design documents.

## Building it

Requires **Go 1.26+** and `bash` on `PATH`. From this directory:

```sh
make build     # go build ./...
make test      # go test -race ./...
```

The full verification gates (format, build, race tests, `golangci-lint`,
`llm-lint`) are declared in [`AGENTS.md`](AGENTS.md).

## The spec

- `specs/design/` — the design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves, so coverage is a
  `grep`.
- `specs/loops/` — the gather → build → verify prompts the build loop runs.
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions the
  loop verifies against.
