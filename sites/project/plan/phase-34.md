# Phase 34 — Owner-id conversion: rebuild `sites` on `owner_id`, snapshot `owner_email`

*Realizes design Decision 15 (data model / store / migration) and 20 (MCP owner
threading), with in-place test updates to Decisions 19 and 25. Depends on the
suite appkit owner-id flip (appkit's `server.Identity` carries `OwnerID`; the
identity gate keys on `X-Owner-Id`) already being in place.*

The suite owner-id conversion (`docs/owner-id-design.md`), sites' slice. sites
owner-scopes nothing (the slug `name` is the global handle; `list` and the landing
page show every site), so this is a **capture-and-display** conversion, not a
re-scoping one: the stable `owner_id` is stored beside the display `owner_email`,
and neither is read for logic.

**What gets built (observable end state):**

- **One new timestamped migration** (`bin/create-migration sites <name>`; the four
  existing migrations stay frozen) that **drops and recreates** `sites` with the
  D15 shape: `name` PK, `public`, `owner_id TEXT NOT NULL`, `owner_email TEXT NOT
  NULL` (replacing `created_by`), `source_path`, `created_at`, `updated_at`, all
  `STRICT`, with `CREATE INDEX idx_sites_owner ON sites(owner_id)`. Rows are
  dropped.
- **`internal/sites` store** (D15): `Site` drops `CreatedBy`, gains `OwnerID` +
  `OwnerEmail`; `Create(ctx, name, ownerID, ownerEmail, public)`; `Get`/`List`
  select the new columns; `SetVisibility` leaves the owner columns untouched.
- **`internal/mcp`** (D20): `create` threads `id.OwnerID` + `id.OwnerEmail` from the
  request `Identity` (no tool argument); `renderSite` projects `owner_id` +
  `owner_email` in place of `created_by`.
- **`internal/mcp` output schema** (D25): the `create`/`set_visibility`
  `outputSchema` swaps `created_by` for `owner_id` + `owner_email`.
- **`cmd/sites` landing** (D19): the `siteRow.CreatedBy`/`createdBy` value is
  sourced from `Site.OwnerEmail`; rendered output (visible table + JSON island) is
  unchanged (email-only display; no `owner_id` in the page).
- Tests inject `X-Owner-Id` (plus `X-Owner-Email` where a snapshot is asserted).

**New ids (need genuine tests):**

- R-Z3ZN-5BFE — `Store.Create(name, ownerID, ownerEmail, public)` persists
  `owner_id` and `owner_email` verbatim (email a snapshot, not derived); Get/List
  return them; two creates with the **same email but different ownerID** persist
  **distinct `owner_id`**. Real migrated SQLite.
- R-Z57J-J363 — after the full migration set, `pragma table_info(sites)` includes
  `owner_id` + `owner_email` and **excludes** `created_by`/`tier`/`published`/
  `published_at`. Real SQLite.
- R-Z6FF-WUWS — `create` with Identity `X-Owner-Id: id-alice` / `X-Owner-Email:
  alice@example.com` persists `owner_id == "id-alice"` and `owner_email ==
  "alice@example.com"`, both surfaced on the returned site and on `list`; two
  callers with the same email but different `X-Owner-Id` create sites with distinct
  `owner_id`. Real handler+DB.

**Revised ids — update the existing realizing tests in place (behavior unchanged,
call/field surface changed):**

- R-QSLO-SAIQ, R-QTTL-629F (D15) — `Store.Create` now takes `ownerID, ownerEmail`.
- R-CW5E-T20N (D25) — the `create` `structuredContent`/mirrored-text object now
  lists `owner_id` + `owner_email` instead of `created_by`.
- R-CYL7-KLI1 (D25) — the `create`/`set_visibility` emitted object's required keys
  now include `owner_id` + `owner_email` (matching the revised schema).

**Retired ids — delete their tags and tests:** R-QRDS-EIS1, R-QQ5W-0R1C (D15),
R-RFRS-1XLX (D20). Their behaviors are replaced by R-Z3ZN-5BFE / R-Z57J-J363 /
R-Z6FF-WUWS.

**Done when:**

- R-Z3ZN-5BFE, R-Z57J-J363, R-Z6FF-WUWS each covered by a clearly-named,
  genuinely-asserting test carrying its tag (behaviors above).
- The revised realizing tests (R-QSLO-SAIQ, R-QTTL-629F, R-CW5E-T20N, R-CYL7-KLI1)
  updated to the new store signature / owner projection and still pass.
- Retired tags gone from the codebase:
  `grep -rn 'R-QRDS-EIS1\|R-QQ5W-0R1C\|R-RFRS-1XLX' --exclude-dir=project sites/`
  returns empty.
- The suite is green (design Conventions): `cd sites && go build ./...`,
  `cd sites && go vet ./...`, `cd sites && gofmt -l .` (no output), and
  `cd sites && go test ./...` all succeed with zero failures.
