# ADR — Personal Access Tokens (PATs)

> **Status: ACCEPTED (design converged, 2026-06-08).** Durable decision record
> for adding owner-minted, cross-service bearer tokens ("Personal Access
> Tokens") to the dashboard. Written in the house *context → decision →
> consequences* shape of `docs/event-plane-decisions.md` and
> `docs/adr-deployment-redesign.md`.
>
> **Scope.** Entirely inside `dashboard/`. It adds a token kind that
> authenticates against the existing nginx → `/internal/authn` → service path
> **without** changing the nginx auth contract, `appkit`, or any service's
> domain surface. It explicitly does **not** add RBAC (per-service or
> per-tool authorization) — that is deferred (see *Non-goals*).
>
> The implementation task list lives in `docs/plan-personal-access-tokens.md`.

---

## Context — the problem we are solving

Today every MCP grant is an OAuth *chain* (`oauth_chains`) bound to exactly one
client, one owner, and **one `resource`** (one service). The browser-less CLI
flow is therefore per-service and two-step:

- `claude mcp add …` registers an MCP server but **does not authenticate** it.
  Claude Code has no `claude mcp auth` command; OAuth login is deferred to the
  interactive `/mcp` slash-command inside a session. (Confirmed against Claude
  Code 2.1.169: `claude mcp` exposes only `add`, `add-from-claude-desktop`,
  `add-json`, `get`, `list`, `remove`, `reset-project-choices`, `serve`.)
- Codex, by contrast, performs the OAuth login as part of `codex mcp add`, so it
  comes out "added **and** authed" from the command line.

The asymmetry the user hit: Claude Code servers land **unauthenticated**, and
each service must be authed separately through the browser dance. There is no
single credential a user can install once and have every suite service work.

Claude Code *does* support one fully-CLI path: `claude mcp add --transport http
--header "Authorization: Bearer <token>" …` is added-and-authed in one shot. The
missing piece is a **bearer token the user can mint** — long-lived, owner-scoped,
valid across all services — to put in that header.

## Decision — an owner-minted, cross-service bearer token

Add a **Personal Access Token**: a long-lived opaque bearer, minted by a
signed-in dashboard user, that authenticates as that user against **every**
MCP service on the box. It is a sibling of the OAuth access token at the auth
gate, not a replacement — OAuth chains are unchanged.

### D1 — Dedicated `personal_tokens` table, separate from the OAuth model

A PAT is **not** an `oauth_chains` row. The chain model is intrinsically
single-resource (its one invariant), is the backing store for the
"Connected MCP clients" grants list (joined to `dcr_clients` for a display
name), and shares the grant revoke route. Bending it with a NULL/sentinel
"wildcard resource" would corrupt that invariant and bleed PATs into the grants
list. A dedicated table keeps the OAuth model pristine and gives the separate
UI section, list query, audit events, and revoke route for free.

```sql
CREATE TABLE personal_tokens (
    id           TEXT PRIMARY KEY,        -- internal PK (audit + rate-limit key)
    public_id    TEXT NOT NULL UNIQUE,    -- user-facing id for the revoke URL
    owner_email  TEXT NOT NULL,
    label        TEXT NOT NULL,           -- required; identifies where it's used
    token_hash   TEXT NOT NULL UNIQUE,    -- SHA-256 hex; plaintext never stored
    created_at   TEXT NOT NULL,
    last_used_at TEXT NULL,               -- present but NOT written in v1 (see D7)
    expires_at   TEXT NULL,               -- present but always NULL in v1 (see D6)
    revoked_at   TEXT NULL
);
CREATE INDEX idx_personal_tokens_owner ON personal_tokens(owner_email);
```

Migration is created with `bin/new-migration dashboard add_personal_tokens`
(timestamped, per `docs/adr-migration-timestamps.md`).

New plaintext prefix **`ms_pat_`**, alongside `ms_oat_` (access) / `ms_ort_`
(refresh). Plaintext shape mirrors OAuth: `ms_pat_` + `ids.New()` + `ids.New()`.
Hashing reuses the existing `hashString` (SHA-256 hex).

### D2 — Auth gate: prefix-dispatch, skip only the resource-binding check

`/internal/authn` (`dashboard/internal/server/authn.go`) runs a strict ordered
pipeline: (a) loopback → (b) resolve bound resource → (c) extract bearer →
(d) validate token → (e) **resource binding** → (f) workspace → (g) rate limit →
(h) emit identity headers.

The bearer's prefix forks step (d):

- **`ms_oat_…`** → today's `ValidateAccess` → OAuth path, **unchanged** (d–h).
- **`ms_pat_…`** → new `ValidatePAT` → **skip (e) entirely** (a PAT is
  cross-service by definition; there is no single resource to bind against) →
  then run (f), (g), (h) exactly as OAuth does.

Step (b) remains **unconditional**: even for a PAT, an *invalid* bearer must
return a 401 whose `WWW-Authenticate` carries the correct `resource_metadata`
URL for whichever service was addressed. Only (e) is conditional on token kind.

`ValidatePAT(ctx, plaintext)` returns active iff the row exists, is not revoked,
and is not expired (the latter is a no-op while `expires_at` is NULL).

### D3 — Workspace check is retained for PATs

Step (f) — `ownerInWorkspace(ownerEmail, a.workspaceDomain)` — runs on **every**
PAT-authenticated request, against the PAT row's `owner_email`. This is a
per-request domain-membership check (a cheap string check that the email's
domain matches the configured Google Workspace domain; it is **not** a live
Google account re-validation). Keeping it means a PAT is no weaker than an OAuth
token on this axis: re-pointing the box at a different workspace domain instantly
invalidates every existing PAT for the old domain.

### D4 — Rate limiting is retained for PATs

Step (g) — `a.rateLimiter.Decide(id)` — runs for PATs keyed on the PAT's internal
`id`, identical in shape to the OAuth `vt.Token.ID` key.

### D5 — Identity headers emitted for a PAT

On allow (h) the gate emits:

| Header          | Value for a PAT                                   |
|-----------------|---------------------------------------------------|
| `X-Owner-Email` | the PAT's `owner_email` (the creator)             |
| `X-Client-Id`   | **`pat:<public_id>`** (synthetic, stable per PAT) |
| `X-Token-Id`    | the PAT's internal `id`                           |
| `X-Chain-Id`    | unset (there is no chain)                         |

`X-Client-Id = pat:<public_id>` is **safe**: an audit of every `X-Client-Id` /
`Identity.ClientID` use across the suite found that **only the dashboard** ever
resolves a client id (in the DCR `authorize`/`token` flow, which PATs never
touch). All nine downstream services treat it as fully opaque — read into
`Identity.ClientID`, at most echoed in the `health` tool's `env` map; no table
lookup, no prefix assumption, no FK. There is already precedent: `prompts` mints
synthetic `prompts:<promptID>` client ids for its self-chaining peer calls and
every peer accepts them silently. The `pat:` prefix can never collide with a
DCR-issued client id, and makes PAT traffic distinguishable in logs.

### D6 — PATs do not expire in v1 (column present, enforced if ever set)

The motivation is "install once and forget", which a short TTL defeats. v1 mints
every PAT with `expires_at = NULL` (never expires) and exposes **no** expiry
picker. The column exists and the gate honors it (`!now.Before(expires_at) →
expired`) so adding an optional expiry later is a pure UI change — no migration,
no gate change. **Revocation, not expiry, is the v1 safety mechanism.**

### D7 — `last_used_at` is tracked structurally but not written in v1

A PAT is validated on the hottest loopback path (every MCP request). Unlike the
grants list — which gets "last used" for free as `MAX(oauth_tokens.issued_at)`,
a byproduct of the refresh lifecycle — a PAT has no such byproduct; populating
`last_used_at` would require an `UPDATE` on every authenticated request (write
amplification + SQLite write-lock contention with the services) purely for a
cosmetic label. v1 keeps the column (nullable, free) but does **not** write it.
The list shows **"Created &lt;relative&gt;"** instead, which together with the
required label is enough to disambiguate ("which one did I make for Codex?"). A
throttled/coalesced write is a possible v2.

### D8 — Multiple PATs per user; explicit label required

A user may mint **many** PATs (e.g. one in Claude Code, one in Codex) and revoke
any individually. The **label is required** (`TrimSpace`d, **≤48 characters**,
rejected if empty or over-length; no charset restriction, rendered through
`html/template` auto-escaping) because it is the only field distinguishing one
PAT from another in the list.

### D9 — UI: plain server-rendered section, POST→redirect→re-render

A PAT row only ever changes through an explicit action the signed-in user takes
in the page (create / revoke); there is **no** out-of-band mutation. So PATs do
**not** reuse the grants block's SSE live-stream machinery. The list is rendered
inline in `handleIndex` (`data.PATs` alongside `data.Grants`); revoke is
POST→303→`/` (the same pattern `handleGrantRevoke` already uses). State-changing
routes enforce same-origin + session, exactly like the grant routes.

A new "Personal access tokens" `<section>` on the logged-in index carries: a
create form (one **label** input + **Create** button) and a list of the user's
PATs (label · created · per-row **Revoke** button), styled like `grants_block`.

### D10 — Show-once creation (direct render, no PRG)

`POST /pat` (same-origin + session) mints the token, stores the hash, and renders
a dedicated confirmation view **directly in the 200 response**: the full
`ms_pat_…` plaintext in a copy-able `<pre>` (reusing the existing `.copy-btn`
handler), a "this is the only time you'll see this — copy it now" notice, and a
**Done** link to `/`. Thereafter only the hash exists; the plaintext is never
recoverable or shown again.

This deliberately forgoes Post/Redirect/Get: a redirect to `/` would discard the
one-time plaintext. The accepted cost is that refreshing the confirmation page
re-POSTs (browser "resubmit?" prompt); a confirmed resubmit mints a **second,
different** PAT — visible in the list and one-click revocable, never a re-show of
the same secret. The confirmation page shows **only the raw token** — no
PAT-embedded `mcp add` commands (those would have to embed a secret and are out
of scope for v1; the public `/install/*` scripts are untouched).

### D11 — Nothing changes outside `dashboard/`

nginx already forwards the `Authorization: Bearer` header to `/internal/authn`
and trusts whatever identity headers come back, oblivious to token kind — **no
nginx change**. Services and `appkit` already treat `X-Owner-Email` /
`X-Client-Id` as opaque trusted headers — **no service or appkit change**. The
entire feature lands inside `dashboard/`.

## Non-goals (explicitly deferred)

- **RBAC.** No per-service allow/deny and no intra-service ("read-only",
  per-tool) authorization. A v1 PAT authorizes **every** service the owner could
  reach, exactly as a full set of OAuth grants would. The natural home for a
  future coarse (per-service) RBAC layer is the same gate branch in D2 (an
  allowed-service set checked in place of the skipped resource binding);
  intra-service levels would require extending the trusted-header contract
  (e.g. `X-Scopes`) through `appkit` and every service — a larger, separate
  effort.
- **Expiry UI** (D6) and **live `last_used_at`** (D7).
- **PAT-embedded install commands / Codex static-header support** (D10).

## Consequences

- **One credential, all services, added-and-authed from the CLI.** A user mints
  one PAT and uses it in `claude mcp add --header "Authorization: Bearer …"` for
  every suite service — closing the Claude-Code auth gap that motivated this.
- **A leaked PAT is valid against every service on the box** until revoked. This
  is the intended trade of dropping RBAC. Mitigations: the retained workspace
  check (D3), required-label + per-row revocation for fast targeted response,
  and `pat:`-tagged audit rows for attribution. PATs never expire in v1 (D6), so
  revocation is the *only* kill switch — it must be reliable and obvious in the
  UI.
- **Contained blast radius.** One migration, one new `internal/pat` package, one
  prefix-dispatch branch in the auth gate, one UI section, two new audit event
  types. No change to nginx, services, appkit, or the OAuth chain model.
- **Forward-compatible with RBAC.** The gate branch in D2 is exactly where a
  per-service allow-list would later slot in, without disturbing the OAuth path.
