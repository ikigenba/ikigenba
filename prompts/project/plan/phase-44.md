# Phase 44 — Owner-id keying conversion: rebuild `prompts`/`runs`, rekey every owner path on `owner_id`

*Realizes design Decision 34 (schema rebuild + store/service rekey), 35 (MCP
tool surface — id scoping, email snapshot, `owner_id` in results), 6 (suite
discovery — the peer-identity slice), and the runner threading in 7. Depends on
no pending phase.*

The suite-wide owner-id conversion (`docs/owner-id-design.md`), prompts' slice
(suite phase 2). appkit's id-keyed chassis (`server.Identity{OwnerID,
OwnerEmail, OwnerName, OwnerPicture, ClientID}`, gate on `X-Owner-Id`; appkit
D13) is already the existing codebase; prompts is red against it until this
phase completes. The suite plan sizes each service's conversion as one phase, so
this single phase spans `internal/db`, `internal/prompt`, `internal/mcp`,
`internal/suite`, and `internal/runner`. End state:

- **Schema** (`internal/db/migrations`): one NEW migration minted with
  `bin/create-migration prompts <name>` (never hand-numbered; frozen migrations
  untouched) drops and recreates `prompts` and `runs` per D36's shapes —
  `owner_id TEXT NOT NULL` (scoping key, `idx_prompts_owner`/`idx_runs_owner`)
  beside write-once `owner_email TEXT NOT NULL`; the import-upsert unique index
  moves to `(owner_id, source_path)`; the run indexes on prompt/status are
  preserved; rows dropped.
- **Store/Service** (`internal/prompt`): `Prompt` and `Run` carry `OwnerID` +
  `OwnerEmail`; every scoping read/write/filter keys on `owner_id`;
  `Create(ownerID, ownerEmail, …)` and `Import(ownerID, ownerEmail, …)` snapshot
  the email; `SpawnRun`/`SpawnTriggeredRun` denormalize the prompt's `owner_id`
  and `owner_email` onto the run (D36).
- **MCP tools** (`internal/mcp`): every owner-scoped verb threads `id.OwnerID`;
  `create`/`import` read `id.OwnerEmail` only for the row snapshot; the
  `Prompt`/`PromptDetail`/`Run` results and their `outputSchema`s (and
  `promptSchema`/`detailSchema`/`runSchema`) keep `owner_email` and add
  `owner_id` (D37).
- **Suite discovery** (`internal/suite`): `Discover(ctx, manifestRoot, ownerID,
  ownerEmail, promptID)` forwards `X-Owner-Id` (the gating header peers now
  require) plus `X-Owner-Email` (display) plus `X-Client-Id` on every outbound
  peer call (D6).
- **Runner** (`internal/runner`): threads `run.OwnerID` and `run.OwnerEmail`
  into `r.discover(…)` (D7).
- **Tests**: every test that crosses the identity gate (the assembled `/mcp`
  handler, the discovery peer calls) injects `X-Owner-Id`, plus `X-Owner-Email`
  where a snapshot or display value is asserted; owner-scoping tests inject
  distinct `X-Owner-Id`s (same email where the discriminator demands it). The
  existing owner-scoping test behind D27's R-B8EC-2AOM (`get`/`run_get` of a
  foreign-owned id → `not_found`) has its foreign-owner set up via a distinct
  `X-Owner-Id`; the id and its behavior are unchanged, only the identity
  injection.

No ids are retired by this phase: prompts' prompt/run owner-scoping was
previously unversioned (legacy migrations `002`/`006`, no minted ids), so this
phase **adds** coverage rather than replacing behavior statements. D6's and D7's
existing ids are unchanged; their design text was touched only for the renamed
`Discover` signature and the runner call.

**Done when:**

- Each new-behavior id is covered by a clearly-named tagged test:
  - R-E59O-RJC7 — the full migration set rebuilds `prompts` and `runs` with
    NOT NULL `owner_id`/`owner_email` and zero rows.
  - R-E6HL-5B2W — prompt store scoping keys on `owner_id`, distinct under a
    shared `owner_email` (list/get/delete).
  - R-E7PH-J2TL — run store/service scoping keys on the run's `owner_id`,
    distinct under a shared email.
  - R-E8XD-WUKA — `Create` persists `owner_id`/`owner_email` verbatim,
    `owner_email` is write-once across `Update`, and a run denormalizes the
    prompt's `owner_id`.
  - R-EBD6-OE1O — same-email/different-id callers are distinct owners at the
    MCP tool layer (list/get/delete/run_cancel).
  - R-ECL3-25SD — `create`/`import` snapshot `owner_id` from `X-Owner-Id` and
    `owner_email` from `X-Owner-Email`; ownership is by id.
  - R-EDSZ-FXJ2 — `get`/`list`/`run_get`/`run_list` results and their
    `outputSchema`s carry `owner_id` beside `owner_email`.
  - R-EF0V-TP9R — suite discovery forwards `X-Owner-Id`/`X-Owner-Email` on
    outbound peer calls (recording peer).
- No ids are retired by this phase, so no retired-id grep applies; the
  design-only coverage difference (the ikispec `comm -23` check, glob
  `*_test.go`, `--exclude-dir=project`) is empty.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  and `go test ./...` all exit 0 in `prompts/`.
