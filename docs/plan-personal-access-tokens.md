# Plan — Personal Access Tokens (PATs)

> Implementation plan for `docs/adr-personal-access-tokens.md`. The ADR is the
> canon for *why* and *what*; this is the *how/order*. All work is inside
> `dashboard/`. On any conflict, the ADR wins and this plan is corrected.
>
> **Execution model.** The work is split into **four phases run strictly
> serially** — one subagent per phase, no parallelism. Each implementation phase
> (P1–P3) is sized to fit a single subagent's context and ends at a **green
> checkpoint** (the build compiles and the named tests pass) so the next phase
> starts from a known-good tree. P4 is operator-driven (deploy), not a subagent.
> A phase lists what to **read**, what to **do**, and the **checkpoint** that
> proves it is done.

## Invariants to preserve (all phases)

- The OAuth chain model (`oauth_chains` / `oauth_tokens`) is **untouched**.
- `/internal/authn` stays write-free on the allow path (no DB write per request).
- nginx, services, and `appkit` are **not** modified.
- Migration is timestamped via `bin/new-migration` and, once committed, immutable
  (`docs/adr-migration-timestamps.md`).
- Every phase leaves `go build ./...` green. Implementation phases also leave the
  phase's named tests green before handing off.

## Resolved design choices (decided in review — do not re-litigate)

- **hashString: duplicate, do not extract.** Copy the four-line SHA-256-hex
  helper into `internal/pat` rather than extracting a shared package or exporting
  it from `oauth`. Extraction ripples into `oauth` for a trivial function;
  duplication keeps `pat` decoupled.
- **Show-once render: dedicated named template.** The confirmation is its own
  named template (`pat_created`) parsed into the template set and buffer-rendered
  via `ExecuteTemplate` — **not** an index-shell branch and **not** a
  `NewPATSecret` field bolted onto `indexData`. It is a minimal standalone page
  (the raw token + a Done link), not the full index shell.

---

## Phase P1 — Data layer (migration + `internal/pat` package)

**Goal.** The `personal_tokens` table exists and a fully-tested store mints,
validates, lists, and revokes PATs. No wiring into the app yet — the package
compiles and tests on its own.

**Read first.**
- ADR §D1 (table + prefix), §D6/§D7 (expiry/last_used columns present-but-unwritten).
- `internal/oauth/tokens.go` — `TokenStore`, `NewTokenStore`, `insertToken`,
  `lookupActive`, `ValidateAccess`, `RevokeChain`, `ListChainsByOwner` (the shape
  to mirror).
- `internal/oauth/types.go` — prefix consts + sentinel-error set.
- `internal/oauth/authcodes.go:124` — `hashString` (the function to duplicate).
- `internal/ids/ids.go` — `ids.New()`.
- An existing file under `internal/db/migrations/` for SQL/format reference.

**Do.**
1. `bin/new-migration dashboard add_personal_tokens` → fill the stamped file with
   the `personal_tokens` table + `idx_personal_tokens_owner` index from ADR §D1
   verbatim. Run `bin/check-migrations`.
2. New package `dashboard/internal/pat`:
   - `const Prefix = "ms_pat_"`.
   - `type PAT struct { ID, PublicID, OwnerEmail, Label string; CreatedAt time.Time; LastUsedAt, ExpiresAt, RevokedAt *time.Time }`.
   - `type Store struct { DB *sql.DB; Now func() time.Time }` **plus a
     `func NewStore(db *sql.DB) *Store`** that sets `Now: time.Now` (mirror
     `NewTokenStore`).
   - Local sentinel errors `ErrNotFound, ErrRevoked, ErrExpired, ErrBadPrefix`
     (defined locally — do **not** import the `oauth` set).
   - **Duplicate** `hashString` (SHA-256 hex) into this package.
   - Methods:
     - `Create(ctx, ownerEmail, label string) (plaintext string, pat PAT, err error)`
       — `Prefix + ids.New() + ids.New()`; insert with `hashString(plaintext)`,
       `expires_at = NULL`, `created_at = Now().UTC()`. The **only** place
       plaintext exists.
     - `ValidatePAT(ctx, plaintext string) (PAT, error)` — prefix guard
       (`ErrBadPrefix`) → lookup by `hashString` (`ErrNotFound`) → `ErrRevoked`
       if `revoked_at` set → `ErrExpired` if `expires_at` set and
       `!Now().Before(expires_at)` → else the row. **No writes.**
     - `ListByOwner(ctx, ownerEmail string) ([]PAT, error)` — non-revoked, newest
       first.
     - `GetByPublicID(ctx, publicID string) (PAT, error)`.
     - `Revoke(ctx, id string) error` — set `revoked_at` if NULL (idempotent).
3. Unit tests (`pat_test.go`): create→validate roundtrip; bad prefix; revoked;
   synthetic expired (set `expires_at` in the past via a fixed `Now`); list
   excludes revoked + newest-first; assert the stored row holds the **hash, not
   the plaintext**.

**Checkpoint.** `go test ./internal/pat/...` passes; `bin/check-migrations` clean;
`go build ./...` green.

**Touches.** `internal/db/migrations/<ts>_add_personal_tokens.sql`,
`internal/pat/{pat.go,pat_test.go}`.

---

## Phase P2 — Auth backend (wire store + gate branch + audit events)

**Goal.** A PAT row authenticates cross-service through `/internal/authn`,
emitting the right identity headers, with the store injected through the normal
`Options` seam. After this phase the feature works end-to-end at the gate; only
the UI is missing.

**Read first.**
- ADR §D2–§D5 (gate branch, workspace, rate limit, headers).
- `internal/server/server.go:38-185` — the `Options` struct, the `app` struct,
  and `newApp` (the validation + construction seam). **This is the wiring you
  extend — it is easy to miss.**
- `cmd/dashboard/main.go:147-171` — where collaborators are constructed and
  passed to `server.Register(server.Options{...})`.
- `internal/server/authn.go` — the (a)–(h) pipeline; note (e) is the binding
  check to skip, (f)/(g)/(h) are reused, and (h) currently sets `X-Chain-Id`
  unconditionally.
- `internal/audit/audit.go:22-61` — the `EventType` const block and `Event`
  struct shape.

**Do.**
1. **Wire the store (six edits, all required):**
   - `Options.PATs *pat.Store` field (`server.go`).
   - nil-check in `newApp` (`if opts.PATs == nil { return nil, errors.New("server: PATs is required") }`).
   - `app.pats *pat.Store` field.
   - assign `pats: opts.PATs` in the returned `&app{...}`.
   - construct `pats := pat.NewStore(conn)` in `main.go` next to `oauthTokens`.
   - pass `PATs: pats` in the `server.Register(server.Options{...})` call.
2. **Audit constants** (`audit.go`): add to the const block, **typed as
   `EventType`** —
   `EventPATCreated EventType = "pat.created"` and
   `EventPATRevoked EventType = "pat.revoked"`. (No writes yet — consumed in P3.)
3. **Gate branch** (`authn.go`, after bearer extraction (c)): dispatch on prefix.
   - `strings.HasPrefix(tok, oauth.AccessPrefix)` → existing path, unchanged.
   - `strings.HasPrefix(tok, pat.Prefix)` → `a.pats.ValidatePAT(...)`. On error:
     same `invalid_token` 401 + `prmURL` challenge + `auditAuthnDeny` with
     `Details{"reason": "invalid_pat", "detail": err.Error()}`. On success:
     **skip (e) entirely**; run (f) workspace against `pat.OwnerEmail`; (g) rate
     limit on `pat.ID`; (h) emit headers per ADR §D5 — set `X-Owner-Email`,
     `X-Client-Id = "pat:" + pat.PublicID`, `X-Token-Id = pat.ID`, and **do NOT
     set `X-Chain-Id`** (the OAuth branch sets it; the PAT branch must omit it).
   - Neither prefix → fall through to the existing `ValidateAccess` path (yields
     `ErrBadPrefix` → today's 401), preserving current behavior for malformed
     bearers.
   - Allow audit for a PAT: `Type: EventAuthnAllow`, `OwnerEmail = pat.OwnerEmail`,
     `ClientID = "pat:" + pat.PublicID`, `Details{"token_id": pat.ID, "kind":
     "pat", "resource": boundResource}`, **no `ChainID`**.
4. **Tests** (`authn` handler — the fixture must configure **≥2 resources** to
   prove cross-service): valid PAT against two *different* service URIs both
   allow; revoked PAT → 401; out-of-workspace owner → 401; over-budget → 429;
   identity headers exactly as ADR §D5 (and `X-Chain-Id` absent).

**Checkpoint.** `go test ./internal/server/...` passes; `go build ./...` green.

**Touches.** `internal/server/server.go`, `cmd/dashboard/main.go`,
`internal/audit/audit.go`, `internal/server/authn.go`,
`internal/server/authn_test.go` (or the existing authn test file).

---

## Phase P3 — HTTP + UI slice (handlers, routes, templates, index, CSS)

**Goal.** A signed-in user can create, see, copy-once, and revoke PATs in the
index page. Handlers and templates ship **together** — the create handler renders
the show-once template and the index renders the list partial, so splitting them
would leave a non-building tree.

**Read first.**
- ADR §D8–§D10 (label rules, plain section + POST→303 revoke, show-once render).
- `internal/server/grants.go` — `requireSession`, `sameOrigin`,
  `handleGrantRevoke` (the exact pattern to mirror, including the
  indistinguishable-404 and the sameOrigin-before-session ordering), and the
  reusable `relativeTime` helper.
- `internal/server/index.go` — `indexData` + the `if data.Owner != ""` populate
  block.
- `internal/server/routes.go` — the route table.
- `internal/server/server.go:154` — the `template.ParseFS(...)` **explicit file
  list** the new partials must be added to.
- `ui/html/index.html`, `ui/html/partials/grants_block.tmpl`,
  `ui/static/app.js` (`.copy-btn` handler), `ui/static/app.css` — UI patterns to
  mirror.

**Do.**
1. **Handlers** (`internal/server/pat.go`):
   - `POST /pat` — `sameOrigin` first, then `requireSession` (mirror grant
     ordering). Read + `TrimSpace` label; reject empty or `len > 48` (400, inline
     error). `Create`; write the `pat.created` audit (`OwnerEmail` +
     `Details{"public_id":..., "label":...}`); then **buffer-render the
     `pat_created` template directly** (200, no redirect — ADR §D10): plaintext in
     a copy-able `<pre>` reusing `.copy-btn`, a "shown once" notice, a Done link
     to `/`.
   - `POST /pat/{public_id}/revoke` — `sameOrigin` + `requireSession`;
     `GetByPublicID`; if missing / not owned / already revoked → `http.NotFound`
     (indistinguishable). `Revoke`; write the `pat.revoked` audit
     (`Details{"public_id":...}`); 303 → `/`.
   - View model `patRow{ PublicID, Label, CreatedISO, CreatedRel }` +
     `patRowsFromPATs([]pat.PAT)` using `relativeTime`.
2. **Routes** (`routes.go`): `mux.Handle("POST /pat", a.handlePATCreate())` and
   `mux.Handle("POST /pat/{public_id}/revoke", a.handlePATRevoke())`. No
   stream/fragment routes (ADR §D9).
3. **Template registration** (`server.go`): add
   `"html/partials/pat_block.tmpl"` and `"html/partials/pat_created.tmpl"` to the
   `template.ParseFS(...)` list. **Without this the page panics at startup.**
4. **Index integration** (`index.go` + `index.html`): add `PATs []patRow` to
   `indexData`; populate in the signed-in block (non-fatal on list error, same
   posture as `data.Grants`). Add a "Personal access tokens" `<section>` after
   the grants/install sections: a create form (label input + Create) and the list
   rendered via `{{template "pat_block" .PATs}}`.
5. **Templates** (`ui/html/partials/`): `pat_block.tmpl` (label · created · per-row
   Revoke form, styled like `grants_block`) and `pat_created.tmpl` (the show-once
   page).
6. **CSS** (`ui/static/app.css`): minimal styling matching the grants list. No
   `app.js` change — reuse the existing `.copy-btn` handler.
7. **Tests** (`pat_test.go` in `internal/server`): create (happy, empty label,
   over-48 label, cross-origin→403, unauthenticated→401); revoke (happy,
   not-owned→404, already-revoked→404); the show-once render contains the
   plaintext exactly once.

**Checkpoint.** `go build ./...` and `go test ./...` both green; manual local
smoke (sign in → create → copy → list → revoke) renders correctly.

**Touches.** `internal/server/pat.go`, `internal/server/routes.go`,
`internal/server/index.go`, `internal/server/server.go` (ParseFS list),
`ui/html/index.html`, `ui/html/partials/pat_block.tmpl`,
`ui/html/partials/pat_created.tmpl`, `ui/static/app.css`,
`internal/server/pat_test.go`.

---

## Phase P4 — Ship (operator-driven, not a subagent)

- `bin/bump dashboard minor` (new user-facing feature) → `bin/ship dashboard` →
  `ssh int sudo opsctl stage dashboard v<ver> --artifact /tmp/...` →
  `ssh int sudo opsctl deploy dashboard v<ver>`, per the root `CLAUDE.md` flow.
  No dashboard restart of *other* services required (no new MCP service / no
  manifest change).
- **Manual end-to-end verify** (needs the live box + Claude Code CLI): sign in →
  create a PAT → copy it → `claude mcp add --transport http --header
  "Authorization: Bearer <pat>" ikigenba_crm <url>` → confirm a tool call works
  against **multiple** services with the one token → revoke → confirm 401.

---

## Touch list (whole feature)

| File | Phase | Change |
|---|---|---|
| `internal/db/migrations/<ts>_add_personal_tokens.sql` | P1 | new table + index |
| `internal/pat/{pat.go,pat_test.go}` | P1 | new store + types + `NewStore` + duplicated `hashString` + tests |
| `internal/server/server.go` | P2, P3 | `Options.PATs` + `newApp` nil-check + `app.pats` + assignment (P2); ParseFS list +2 partials (P3) |
| `cmd/dashboard/main.go` | P2 | construct `pat.NewStore(conn)`, pass `PATs:` to `Register` |
| `internal/audit/audit.go` | P2 | two `EventType` constants |
| `internal/server/authn.go` | P2 | prefix-dispatch branch, skip (e), omit `X-Chain-Id` for PATs |
| `internal/server/authn_test.go` | P2 | PAT gate tests (≥2 resources) |
| `internal/server/pat.go` | P3 | create + revoke handlers, view model |
| `internal/server/routes.go` | P3 | two new routes |
| `internal/server/index.go` | P3 | `PATs` on `indexData`, populate when signed in |
| `internal/server/pat_test.go` | P3 | handler tests |
| `ui/html/index.html` | P3 | PAT `<section>` + create form |
| `ui/html/partials/pat_block.tmpl` | P3 | PAT list partial |
| `ui/html/partials/pat_created.tmpl` | P3 | show-once confirmation page |
| `ui/static/app.css` | P3 | PAT list/section styling |
| `dashboard/VERSION` | P4 | minor bump at ship time |
