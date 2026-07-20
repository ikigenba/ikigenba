# Phase 26 — Owner-id keying: rebuild the `scripts` table and rekey scoping on `owner_id`

*Realizes design Decision 28 (owner-id keying) and the owner-id revisions of
Decision 22 (`suite.mcp` identity by id), 21 (`SUITE_OWNER_ID` runtime env), 19
(`OwnerID` output-schema field), and 17 (`ownsScript` gate keys on `owner_id`).
Depends on appkit's shipped owner-id gate (its D13): `server.Identity` carries
`OwnerID` and `POST /mcp` 401s on empty `X-Owner-Id`. No earlier pending phase
is a dependency.*

scripts' slice of the suite-wide owner-id conversion
(`docs/owner-id-design.md`). `scripts` is the only owner-scoped table (`runs`
and `script_triggers` scope through `script_id`), so rekeying it rekeys the
domain.

Observable end state:

- One new timestamped migration (`bin/create-migration scripts owner_id_keying`;
  frozen `002_scripts.sql` and `20260609135007_add_source_path.sql` untouched)
  **drops and recreates** `scripts` with `owner_id TEXT NOT NULL` (sole scoping
  key) beside `owner_email TEXT NOT NULL` (write-once snapshot), carrying the
  existing columns incl. `source_path`, an index on `owner_id`, and the
  `idx_scripts_source` partial-unique index **rekeyed** to `(owner_id,
  source_path)`. Rows dropped; dependent `runs`/`script_triggers` rows cleared
  so no dangling FK survives (migration-window state, per D17).
- `internal/script` (`store.go`/`service.go`/`model.go`) keys `ListByOwner`,
  the `ownsScript` gate, and the `import` upsert on `owner_id`; `create`/`import`
  snapshot `Identity.OwnerEmail` and never read it for logic. `Script`/
  `ScriptDetail` gain an untagged `OwnerID` field beside `OwnerEmail`.
- `internal/mcp/tools.go`: `create` threads `Identity.OwnerID` (key) and
  `Identity.OwnerEmail` (snapshot); the `update` `outputSchema` declares the
  PascalCase `OwnerID`/`OwnerEmail` keys; `get`/`list`/`update`
  `structuredContent` expose both.
- `internal/runner`: `SUITE_OWNER_ID` (the row's `OwnerID`) is injected beside
  `SUITE_OWNER_EMAIL`; `suite.py`'s `suite.mcp` sends `X-Owner-Id:
  $SUITE_OWNER_ID` (required by the callee gate) plus `X-Owner-Email:
  $SUITE_OWNER_EMAIL` (display).
- Every scripts test that drives `/mcp` injects `X-Owner-Id` (plus
  `X-Owner-Email` where a snapshot/display value is asserted); the module is
  green again after appkit's flip.

**Done when** the suite is green (`cd scripts && go build ./...`,
`go vet ./...`, `gofmt -l .` empty, `go test ./...` all pass) and:

New ids, each covered by a genuinely-asserting tagged test:

- R-Q2LM-XR9W — full embedded migration set over fresh real SQLite (seeded with
  a pre-conversion row) yields a `scripts` table with `owner_id`+`owner_email`
  NOT NULL, `source_path` present, UNIQUE index over `(owner_id, source_path)`,
  an index on `owner_id`, **zero rows**; `002_scripts.sql` and
  `20260609135007_add_source_path.sql` byte-identical to their committed bodies.
- R-Q3TJ-BJ0L — `create`/`import` persists `owner_id == X-Owner-Id` and
  `owner_email == X-Owner-Email` from distinct, unrelated values (email a
  snapshot, not the key).
- R-Q51F-PARA — `list` scopes on `owner_id`, excluding another id's scripts even
  when both callers share one `X-Owner-Email`.
- R-Q69C-32HZ — `get`/`update`/`delete`/`run` on a different id's script return
  `not_found` and mutate nothing (the `ownsScript` gate on `owner_id`) even with
  a shared `X-Owner-Email`; the owner then succeeds.
- R-Q7H8-GU8O — `get`/`list`/`update` `structuredContent` carry both `OwnerID`
  and `OwnerEmail` (PascalCase) equal to the stored values.
- R-Q8P4-ULZD — the `import` upsert keys on `(owner_id, source_path)`: same
  path+id updates one row; same path under a different id sharing the email is a
  distinct row.
- R-Q9X1-8DQ2 — `suite.mcp` happy path sends `X-Owner-Id == $SUITE_OWNER_ID`
  (required), `X-Owner-Email == $SUITE_OWNER_EMAIL`, `X-Client-Id
  scripts:$SUITE_SCRIPT_ID`, no `X-Forwarded-Proto`; returns `structuredContent`
  verbatim; omitted args sent as `{}`.

Revised already-realized ids — update their tests for owner-id semantics
(they stay realized):

- R-HWSL-TII2 (D21) — the child env now also carries `SUITE_OWNER_ID`.
- R-C43Q-0BYO (D19) — the `update` schema declares the PascalCase `OwnerID`/
  `OwnerEmail` keys.
- R-7XEU-W467 (D17) — the trigger `ownsScript` gate keys on `owner_id`.

Retired id — its test is deleted, and it appears in no non-spec source:
`grep -rn 'R-I0GA-YTQ5' --include='*.go' --exclude-dir=project .` returns empty.
