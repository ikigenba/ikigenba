# Phase 38 — The name/slug split in the domain: schema, validator, store

*Realizes design Decision 15 (data model: slug + display name).*

`internal/sites` and `internal/db` take on the split identity: a new immutable
migration (created with `bin/create-migration sites <name>`) rebuilds the
`sites` table with a `slug` TEXT primary key and a required free-form `name`
display-label column, carrying no rows across (no production data — D15's exact
SQL). The package gains `ValidateName` (+ the `ErrInvalidName` sentinel), the
`Site` struct carries `Slug` and `Name`, `Create` takes `(slug, name, ownerID,
ownerEmail, v)`, `SetVisibility` renames the **slug** (never the name), and the
new `Rename` mutates only the display name. `Get`/`List`/`Delete`/
`SetSourcePath` re-key on `slug`.

The suite must stay green, so this phase also **mechanically adapts every
caller** (`internal/mcp`, the landing handler, their tests) to the new store
signatures **without changing any wire behavior yet**: where a caller only has
today's single identifier, pass it as both slug and name. The MCP tool schemas,
the landing template, and their behavioral contracts are rewritten by Phases
39/40 — not here. Tests tagged with the retired D15 ids (R-H0LI-XQXI,
R-H1TF-BIO7, R-Z3ZN-5BFE, R-H31B-PAEW, R-H498-325L) are replaced by the new
ids' tests; no retired-id tag survives in `internal/sites`.

**Done when:** each of R-Z9L9-PCXQ (create persists slug+name+visibility
verbatim), R-ZAT6-34OF (SetVisibility re-slugs, never renames the label),
R-ZC12-GWF4 (Rename changes only the name), R-ZD8Y-UO5T (owner id/email
snapshot), R-ZEGV-8FWI (migrated schema shape + CHECK + NOT NULL name), and
R-ZFOR-M7N7 (ValidateName rules) is covered by a clearly-named test tagged with
its id, and the suite is green per design Conventions.
