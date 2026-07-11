# Phase 20 — the identity model: the `identities` table and its store

*Realizes design Decision 17 (identity model / `identities` store). Depends on
no earlier phase (new self-contained package + migration).*

Adds the durable identity substrate. A new forward-only migration
(`bin/create-migration dashboard add_identities`) creates the `identities` table
— `id` (opaque `ids.New()` handle, PK), `iss`, `sub`, `email`, nullable `name`/
`picture`, timestamps, `UNIQUE (iss, sub)`. A new `internal/identity` package
provides a `Store` (over `*sql.DB`, injectable `Now` and `New` id source) with
`ResolveOrCreate(ctx, Claims{Iss,Sub,Email,Name,Picture}) (id, err)` — a single
`INSERT … ON CONFLICT (iss, sub) DO UPDATE` upsert returning the surviving
handle — and `Lookup(ctx, id) (Identity, error)` with a distinguishable
not-found. No callback, header, or HTTP behavior changes in this phase; the store
stands alone and is exercised directly against a real temp DB.

**Done when:** the suite is green (per design *Conventions* — `go build ./...`,
`go vet ./...`, `gofmt -l .` empty, `go test ./...`, `bin/check-migrations
dashboard`) and each id below is covered by a clearly-named, genuinely-asserting
test in `internal/identity`, run against a real temp `modernc.org/sqlite`
migrated by the appkit runner:

- R-VJMO-6CN9 — `ResolveOrCreate` on an unseen `(iss, sub)` inserts one row and
  returns its handle; `Lookup` of that handle round-trips the stored fields.
- R-VKUK-K4DY — the returned handle comes from the injected `ids.New` source
  (asserted via a stub) and is neither the email nor a sequential integer.
- R-VM2G-XW4N — a repeat `(iss, sub)` returns the same handle and leaves exactly
  one row (no duplicate), even with different email/name/picture.
- R-VNAD-BNVC — a repeat `(iss, sub)` with changed attributes updates
  email/name/picture (last-login-wins) while `id`/`created_at` are unchanged.
- R-VOI9-PFM1 — `Lookup` of an unknown handle returns the distinguishable
  not-found result, not a zero-value identity.
