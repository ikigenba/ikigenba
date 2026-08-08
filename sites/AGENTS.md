# sites

The `sites` service for the ikigenba suite: a loopback-only static-website host
under `/srv/sites/`. Agents create named sites and write files into them over MCP,
and the same process serves those files back to browsers. Each site is a slug with a
public/private visibility flag; files live on disk under a per-visibility tree. An
appkit binary with no token logic (nginx is the sole trust boundary). It is neither
an event-plane producer nor consumer (no `/feed`). Module path: `sites`.

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

- `cmd/sites`: the composition root (the `appkit.Spec`, landing + MCP wiring).
- `internal/`: `sites` (slug/visibility domain + Dropbox sync), `files` (confined
  filesystem ops), `serve` (the static server for `/public/` and `/private/`),
  `web` + `share/www` (landing page), `mcp`, `db` (embedded migrations).
- `etc/`: `manifest.env` and the nginx location fragment.
- `project/`: the spec the build loop works from.

## Tests

The full green bar, run from the repository root, is:

```sh
cd sites && go build ./...
cd sites && go vet ./...
cd sites && gofmt -l . # must print nothing
cd sites && go test ./...
```

The default gate has exactly two test layers: **hermetic** and **composed**.
There is no live layer and no manual layer in this tree.

Beyond the Go toolchain, the suite has two environmental preconditions. A
`google-chrome` binary must be on `PATH` for the hermetic browser-wiring test;
its absence is a hard failure, never a skip. The `go` binary must also be on
`PATH` in the test process's environment, with the module cache already
resolving sites' `replace` siblings; its absence is likewise a hard failure,
never a skip.

The gate runs in **workspace** GOWORK mode through the repository-root
`go.work`. The production build's `GOWORK=off` mode is not part of the gate.

## Versioning

The committed `sites/VERSION` file is the single source of truth (v-prefixed SemVer,
currently `v0.17.1`). Advance it with `bin/bump sites <major|minor|patch>`; ship with
`bin/ship sites`. Git tags are not the version mechanism.
