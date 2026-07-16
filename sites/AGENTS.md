# sites

The `sites` service for the ikigenba suite: a loopback-only static-website host
under `/srv/sites/`. Agents create named sites and write files into them over MCP,
and the same process serves those files back to browsers. Each site is a slug with a
public/private visibility flag; files live on disk under a per-visibility tree. An
appkit binary with no token logic (nginx is the sole trust boundary). It is neither
an event-plane producer nor consumer (no `/feed`). Module path: `sites`.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/sites`: the composition root (the `appkit.Spec`, landing + MCP wiring).
- `internal/`: `sites` (slug/visibility domain + Dropbox sync), `files` (confined
  filesystem ops), `serve` (the static server for `/public/` and `/private/`),
  `web` + `share/www` (landing page), `mcp`, `db` (embedded migrations).
- `etc/`: `manifest.env` and the nginx location fragment.
- `project/`: the spec the build loop works from.

## Tests

- `go test ./...` from `sites/`. Green hard-requires a `google-chrome` binary on
  `PATH` (the browser-wiring test is never skipped).
- Green also means clean `go build ./...`, `go vet ./...`, and `gofmt -l .`. The
  prod build forces `GOWORK=off`.

## Versioning

The committed `sites/VERSION` file is the single source of truth (v-prefixed SemVer,
currently `v0.17.1`). Advance it with `bin/bump sites <major|minor|patch>`; ship with
`bin/ship sites`. Git tags are not the version mechanism.
