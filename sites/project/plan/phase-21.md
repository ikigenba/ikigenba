# Phase 21 — Create at a chosen visibility; `sync` requires an existing site

*Realizes design Decision 20 (the `create(name, public?)` one-insert birth and the `sync`-requires-existing change). Depends on Phase 16 (the D20 tool table — `created_by` threading, `SiteDir` retargeting, `renderSite`) and Phase 15 (the `Store.Create`/`SetVisibility`/`Layout` seam this extends).*

Give `create` an optional visibility and stop `sync` from bringing sites into
being. This spans the store primitive and its two callers; no schema migration
(the `public` column already exists), no nginx, no landing change.

- **`sites/internal/sites/store.go`** — `Store.Create` gains a `public bool`
  parameter and stores it verbatim in the `INSERT` (replacing the hardcoded
  `0`), returning a `Site` whose `Public` is the value passed. The doc comment
  changes to "inserts a fresh row at the caller-chosen visibility; there is no
  store-side default." The D15/Phase-15 store test (`internal/sites/store_test.go`)
  is updated to the new four-argument signature and gains the `public:true` case
  (this trues up R-QSLO-SAIQ's existing behavior to the new signature; the id
  remains Phase 15's).
- **`sites/internal/mcp/tools.go`** — the `create` tool's input schema gains an
  optional `public` boolean (**not** in `required`); `toolCreate` decodes it and
  calls `store.Create(ctx, name, id.OwnerEmail, a.Public)` then
  `os.MkdirAll(SiteDir(a.Public, name))` — one insert, one directory, born at the
  requested visibility, no intermediate move. The description notes the optional
  `public` flag and that it defaults private.
- **`sites/internal/mcp/sync.go`** — the create-or-reuse branch is removed: on a
  `Get` that returns `ErrNotFound`, `toolSync` returns the `not_found` error
  envelope and creates neither a row nor a directory. An existing site
  reconciles exactly as before. The description is reworded to state the site
  must already exist and that visibility is unchanged (no "publish"/"deploy"
  wording).
- **Tests** — `internal/mcp` handler tests over a temp DB, temp `SITES_ROOT`, and
  a fake mirror client; `internal/sites` store tests over a real migrated SQLite DB.

**Done when:** the sites suite is green (`cd sites && go build ./...`, `go vet
./...`, `gofmt -l .` prints nothing, `go test ./...`, `bin/check-migrations sites`),
AND R-554R-3MBC: a test calling `create(name, public:true)` asserts the returned
site has `public == true` and a `url` ending `public/<name>/`, that the directory
exists at `SiteDir(true, name)` and **not** at `SiteDir(false, name)`; and a test
calling `create(name)` with `public` omitted asserts `public == false`, a `url`
ending `private/<name>/`, and the directory at `SiteDir(false, name)`;
AND R-56CN-HE21: a test calling `sync` for an absent slug asserts a `not_found`
error envelope, that a subsequent `get`/`list` still shows the slug absent, and
that no directory exists under either `SiteDir(true, name)` or `SiteDir(false,
name)`; and a test calling `sync` for a pre-created site asserts its files
reconcile and its `public` flag is unchanged.
