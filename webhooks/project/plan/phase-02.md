# Phase 2 — Webhook identity & secret lifecycle

*Realizes design Decision 3 (Webhook identity & secret lifecycle). Depends on Phase 1.*

Add the domain layer that mints and verifies webhook identities and secrets. This
phase introduces the `Service`/`Clock` seam that design assigns to D1 but is first
needed here (`internal/webhooks/service.go`): the concrete `Service` over the
Phase 1 `Store` and `*sql.DB`, with an injectable `Clock` (`RealClock` for prod,
deterministic clock under test) and an `Outbox` field left nil until Phase 3.
D1's own verification ids are *not* realized here — only the seam is born; D1 is
proven in Phase 6.

The secret lifecycle lands as design D3 specifies, in
`internal/webhooks/secret.go`: `newName` (`ids.New()`, 26-char base32), `newSecret`
(`ms_wh_` + `ids.New()`), `hashSecret` (SHA-256 hex), `verifySecret`
(`subtle.ConstantTimeCompare` over the hex hashes), and `validateName`
(`^[A-Za-z0-9_-]{1,64}$`). The owning `Service` methods are `Create` (empty name →
generate, else `validateName`; persists only `hashSecret(secret)`; returns the
plaintext show-once; maps duplicate-name to `ErrNameTaken`, invalid to
`ErrInvalidName`) and `Rotate` (fresh secret for the owner's webhook, old one
invalidated, `name`/`owner`/`created_at` unchanged; missing-or-not-owned →
`ErrNotFound`). The sentinels `ErrNameTaken`/`ErrInvalidName`/`ErrNotFound` are
defined here.

End state: `cd webhooks && go build ./... && go vet ./... && go test ./...` green.

**Done when:** design D3's Verification ids are each covered by a genuine test
(unit + real-SQLite) and the suite is green —
- R-37GT-C05G — `Create`'s secret begins `ms_wh_` and the stored `secret_hash`
  equals `sha256hex(secret)` (and ≠ the plaintext);
- R-38OP-PRW5 — `verifySecret` accepts the exact secret and rejects any other,
  including a one-character difference;
- R-39WM-3JMU — after `Rotate` the new secret verifies, the old no longer does,
  and `name`/`owner_email`/`created_at` are unchanged;
- R-3CCE-V348 — neither a `ListByOwner` item nor a `GetByName` `Webhook` exposes
  the secret or `secret_hash`; plaintext comes only from `Create`/`Rotate`;
- R-3DKB-8UUX — an invalid user-supplied name (`/`, `.`, space, empty, 65 chars)
  → `ErrInvalidName` with no row written; a valid name is accepted and persisted;
- R-3ES7-MMLM — an empty name generates a 26-char `[A-Z2-7]` name, and two
  successive empty-name creates yield different names.
