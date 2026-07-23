# wiki

The `wiki` service for the ikigenba suite: a knowledge-base domain service under
`/srv/wiki/` that ingests source text, extracts and compiles subject pages, and
answers questions with cited RAG (`ingest` / `search` / `ask`). It is an appkit
binary over SQLite and an event-plane consumer (it ingests the dropbox feed). It
serves a bearer-gated MCP surface for agents alongside a session-gated read
surface (home, ask results, subject pages) for humans, with no token logic
(nginx is the sole trust boundary). Module path: `wiki`.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/wiki`: the composition root.
- `internal/`: the domain packages: `extract`, `compile`, `retrieve`, `ask`,
  `page`, `markdown`, `llm`, `worker` (ingest queue), `web`, `mcp`, `db`
  (migrations), `ids`.
- `autotune/`: committed tune-folder data and scorer workspace.
- `etc/`: `manifest.env` and the nginx location fragment.
- `share/www`: Carbon assets and page templates for the read surface.
- `project/`: the spec the build loop works from.

## Tests

- `go test ./...` from `wiki/` (also runnable via `make test`).

## Versioning

The committed `wiki/VERSION` file is the single source of truth (v-prefixed SemVer,
currently `v0.13.1`). Advance it with `bin/bump wiki <major|minor|patch>`; ship with
`bin/ship wiki`. Git tags are not the version mechanism.
