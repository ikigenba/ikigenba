# wiki

The `wiki` service for the ikigenba suite: a knowledge-base domain service under
`/srv/wiki/` that ingests source text, extracts and compiles subject pages, and
answers questions with cited RAG (`ingest` / `search` / `ask`). It is an appkit
binary over SQLite and an event-plane consumer (it ingests the dropbox feed). It
serves a bearer-gated MCP surface for agents alongside a session-gated read
surface (home, ask results, subject pages) for humans, with no token logic
(nginx is the sole trust boundary). Module path: `wiki`.

## How changes are made

Changes go through the spec under `project/`, not direct edits — settle the
spec, then let the build loop realize it. The spec itself is direction-gated:
`project/**` is written only inside an operator-invoked move (the `$open-spec`
→ `$grill-me` → `$seal-spec` arc, or the build loop's completion mutations).
In any other session `project/` is read-only reference — a stale or wrong spec
is a finding to report, not a license to edit, and a settled discussion is not
direction: say what should change and wait. Edit code directly only on
explicit operator instruction. See the `$ikispec` skill for the `project/`
spec contracts and `$ralph` for the unattended build workflow.

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

- Default gate: `go test ./...` from `wiki/` (also runnable via `make test`).
- Layers present: **hermetic**, **composed**, and **live**.
- Environmental preconditions beyond the Go toolchain: **none**. The
  `autotune/` scorer executables are committed in-tree and run under the same Go
  toolchain.
- GOWORK mode: **workspace**, using the repository-root `go.work`; production
  builds force `GOWORK=off` through `bin/ship wiki`.
- Live invocation: `go test -tags live ./...` from `wiki/`. It discovers the
  running loopback prompts service through the registry and requires
  `OPENAI_API_KEY` for the real autotune judge model.
- Run the live layer at deploy verification, and whenever a change touches the
  `internal/llm` prompts client, an `autotune/` judge prompt, or a scorer.

The composed layer is the `cmd/wiki` boot and module-wiring coverage plus the
`internal/db` migration run over the real binary's embedded migrations. The
hermetic layer is everything else in the default gate, including scorer paths
run with `SCORE_SKIP_JUDGE=1` over committed fixtures.

## Versioning

The committed `wiki/VERSION` file is the single source of truth (v-prefixed SemVer,
currently `v0.13.1`). Advance it with `bin/bump wiki <major|minor|patch>`; ship with
`bin/ship wiki`. Git tags are not the version mechanism.
