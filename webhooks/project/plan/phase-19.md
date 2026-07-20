# Phase 19 — Owner-id keying conversion: rebuild the webhooks table, rekey every owner path on `owner_id`

*Realizes design Decision 2 (data model — the owner-id slice), 3 (secret
lifecycle — the create-snapshot slice), 4 (ingress — the identity-header
slice), 5 (event production — the owner-pair slice), and 6 (MCP tools — the
id-scoping slice).*

The suite-wide owner-id conversion (`docs/owner-id-design.md`) lands in
webhooks. appkit's id-keyed chassis (`server.Identity{OwnerID, OwnerEmail,
OwnerName, OwnerPicture, ClientID}`, gate on `X-Owner-Id`) is already the
existing codebase; the module is red against it until this phase completes.
End state:

- **Schema**: one NEW migration minted with `bin/create-migration webhooks
  <name>` (never hand-numbered; committed migrations untouched) drops and
  recreates `webhooks` per D2's current shape — `owner_id TEXT NOT NULL`
  (scoping key, indexed) beside write-once `owner_email`, keeping the D17
  `verification`/`secret` columns; rows dropped.
- **Store/Service** (`internal/db`, `internal/webhooks`): `Webhook` carries
  `OwnerID` + `OwnerEmail`; `ListByOwner`/`Delete`/`UpdateSecret` key on
  `owner_id` only; `Create(ownerID, ownerEmail, name)` snapshots the email;
  `Rotate(ownerID, name)`.
- **Ingress** (`internal/webhooks/ingress.go`): the defense-in-depth check
  rejects `X-Owner-Id` alongside `X-Owner-Email`/`X-Client-Id` (D4).
- **Event payload** (`internal/webhooks/events.go`): `owner` is replaced by
  `owner_id` + `owner_email`, both from the stored row (D5); registry Sample
  follows.
- **MCP tools** (`internal/mcp`): verbs scope on `id.OwnerID`; `create` reads
  `id.OwnerEmail` only for the row snapshot (D6/D12).
- **Tests**: all suite tests inject `X-Owner-Id` (plus `X-Owner-Email` where a
  snapshot or display value is asserted). Tests tagged with the revised
  in-place ids are updated to their current design statements: R-SZ8I-R4EY,
  R-39WM-3JMU, R-GTUZ-AIGW, R-5Z8J-Y0YP, R-6445-H3XH, R-GBFM-CG9S,
  R-A3FB-J3ZK (payload key list). Tests tagged with the retired ids
  R-T0GF-4W5N, R-T1OB-INWC, R-7L8J-RITT, R-GV2V-OA7L are deleted or re-tagged
  to their replacement ids.

**Done when:**

- The new-behavior ids are each covered by a clearly-named tagged test:
  - R-L3TX-A4Q3 — full migration set rebuilds `webhooks` with NOT NULL
    `owner_id`/`owner_email` (+ D17 columns) and zero rows.
  - R-L51T-NWGS — `ListByOwner` keys on `owner_id`, distinct even under a
    shared `owner_email`.
  - R-L69Q-1O7H — `Delete`/`UpdateSecret` scope on `owner_id`, inert
    cross-owner even under a shared email.
  - R-L7HM-FFY6 — `Create` persists `owner_id`/`owner_email` verbatim as
    passed.
  - R-L8PI-T7OV — ingress 404s on any of the three identity headers,
    tolerates `X-Forwarded-Proto`.
  - R-L9XF-6ZFK — payload `owner_id`/`owner_email` come from the stored row;
    no `owner` field remains.
  - R-LB5B-KR69 — same-email/different-id callers are distinct owners at the
    tool layer.
- Retired ids are gone from the tree:
  `grep -rn 'R-T0GF-4W5N\|R-T1OB-INWC\|R-7L8J-RITT\|R-GV2V-OA7L' --exclude-dir=project .`
  (run from `webhooks/`) returns empty.
- The suite is green per design Conventions: `go build ./...`,
  `go vet ./...`, `go test ./...` all exit 0 in `webhooks/`.
