# Phase 16 — owner-id test/identity alignment (no schema, no migration)

*Realizes design Decision — (no ids; test/identity-alignment only, touching D2's
in-process-gate rationale). Depends on the suite's appkit hard flip having
landed (owner-id-design Decision 1 / appkit D13): the chassis `server.Identity`
now carries `OwnerID`/`OwnerName`/`OwnerPicture` beside `OwnerEmail`/`ClientID`,
and `requireIdentityHeaders` refuses `401` when `X-Owner-Id` is empty.*

ledger is the one suite service that holds **live production customer data**, and
it has **no owner columns** — it stores no owner on any row and exposes no owner
in any domain result. This is the suite-wide owner-id conversion's ledger slice
(owner-id-plan.md phase 11): a **code/test alignment only**. It ships **no
migration and touches no schema** — a hard suite rule that protects ledger's
`state/`.

The only substrate that moves is the MCP test harness. Under the flipped appkit
gate, a request must carry `X-Owner-Id` to reach an inner handler, so the shared
`rpc` helper in `internal/mcp/tools_test.go` — which today injects only
`X-Owner-Email` and `X-Client-Id` — must also inject `X-Owner-Id`, or every
domain-tool RPC call (`record`/`reverse`/`reconcile`/`balance`/`register`/`get`/
`describe`, plus the chassis `health`/`reflection`) returns `401` instead of its
asserted result. The display header `X-Owner-Email` stays injected (the health
envelope still asserts `owner_email`), and the health envelope now also carries
`owner_id`, so `TestHealth` follows the widened Identity by additionally
asserting it. No domain behavior, wire result, event payload, or column changes.

ledger's nginx fragment (D4) already forwards all four `X-Owner-*` headers
including `X-Owner-Id` (R-FLV3-9RX8, R-FN2Z-NJNX) and is unchanged by this phase.

**What gets built (test/spec alignment; governed source in `internal/mcp` +
`cmd/ledger` only):**

- `internal/mcp/tools_test.go`: the `rpc` helper injects `X-Owner-Id` on every
  request (retaining `X-Owner-Email` and `X-Client-Id`); `TestHealth`
  additionally asserts the health envelope's `owner_id` equals the injected id,
  keeping its existing `owner_email`/`client_id` assertions.
- No new tags, no new ids: every existing ledger Verification id is already
  realized by a passing, unchanged test; this phase only keeps those tests green
  under the flipped chassis gate.

**Done when** (deterministic exit conditions):

- **No-migration guard (protects live data).** The migrations directory
  `internal/db/migrations/` is **byte-identical / unchanged** — its file-set
  stays exactly the five committed files
  (`001_schema_migrations.sql`, `002_ledger.sql`, `003_outbox.sql`,
  `20260712184029_external_ref.sql`, `20260712184833_outbox_routing.sql`) with
  no file added, removed, or modified. `git status --porcelain
  internal/db/migrations/` prints nothing.
- The harness injects the id: `grep -q 'X-Owner-Id' internal/mcp/tools_test.go`
  succeeds, and `TestHealth` asserts `owner_id` (a `grep -q 'owner_id'
  internal/mcp/tools_test.go` succeeds).
- No design Verification id is unrealized: the ikispec coverage `comm -23` check
  (design ids vs `*_test.go` tags + pending phase files) prints nothing but the
  literal `R-XXXX-XXXX` placeholder.
- The suite is green per design's *Conventions*: `cd ledger && go build ./...`,
  `cd ledger && go vet ./...`, `cd ledger && gofmt -l .` (no output), and
  `cd ledger && go test ./...` all succeed with zero failures.
