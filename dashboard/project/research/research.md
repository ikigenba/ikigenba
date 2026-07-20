# dashboard — Research

**Non-contractual external ground truth.** This file collects the external
facts the design leans on so design need not re-derive them. The build loop
never reads it. It is rewritten in place to stay a single coherent statement of
what is currently known, not a running log.

## Google OpenID Connect id_token — the identity claims

The dashboard federates identity to two IdPs: Google (OIDC) and GitHub (plain
OAuth; see the GitHub section below). `internal/googleidp` performs the
authorization-code exchange and verifies the returned **id_token** (RS256
against Google's JWKS, `iss`/`aud`/`exp` checked). Everything below is about
that verified id_token; the Google path makes **no** separate `userinfo` call.

- **`iss` (issuer)** — a standard OIDC claim, always present. For Google it is
  `https://accounts.google.com` (Google also accepts the bare
  `accounts.google.com`). Identifies *which* IdP minted the token.
- **`sub` (subject)** — a standard OIDC claim, **always present**, mandated by
  OIDC Core. It is the stable, unique identifier for the user *within that
  issuer* and is the claim OIDC intends you to key a local account on, because
  email and name can change. `sub` is unique only per-issuer, so the globally
  unique identity is the pair **`(iss, sub)`**. Today, single-provider, `sub`
  alone is unambiguous; storing `iss` too is cheap insurance against a future
  second provider silently colliding.
- **`email`** — present when the `email` scope is granted (it is). Mutable: a
  Workspace admin can change it. Therefore an *attribute*, not an identity key.
- **`name` / `picture`** — present when the `profile` scope is granted (it is —
  `internal/googleidp` requests `openid email profile`). In the authorization-
  code flow Google normally includes `name` (composed display name) and
  `picture` in the id_token, but they are **optional**: a given login may omit
  either. Consumers must tolerate absence. If they ever prove unreliable in
  practice, the fallback is a `userinfo` fetch at callback — not needed now.

### The `picture` claim is a public URL, not image data

`picture` is a **URL string** (~100 chars), e.g.
`https://lh3.googleusercontent.com/a/ACg8oc…=s96-c`, not base64 bytes. Key
facts:

- It is served from Google's CDN and is **publicly fetchable with no
  authentication** — no cookie, no bearer token. Anyone with the URL can GET the
  image. (A mild privacy note: the avatar URL is effectively a public link to
  the person's Google profile photo.)
- The trailing `=s96-c` is a **size directive**; rewriting it (`=s256-c`, …)
  requests a different resolution from the same URL.
- The URL can **rotate/expire** over time, so a stored URL is a cache of the
  last login, acceptable for a display avatar and refreshed on every login.

## GitHub user login via the suite's GitHub App — no OIDC, no id_token

The suite already owns one **GitHub App** named `ikigenba` (App ID `4217005`,
owned by the `@ikigenba` org), used by the `github`/`repos` services in
*installation* mode (App JWT → installation token). The same App also carries
**OAuth user-authorization credentials** — a client id (`Iv23…`-form) and a
client secret — which support a user-to-server web login flow. Facts the design
leans on, verified against GitHub's documented v3 API:

- **Endpoints.** Authorize: `https://github.com/login/oauth/authorize`
  (`client_id`, `redirect_uri`, `state`). Token exchange: `POST
  https://github.com/login/oauth/access_token` with `client_id`,
  `client_secret`, `code`, `redirect_uri`; send `Accept: application/json` to
  get a JSON body (`access_token`, `error`, …) instead of the default
  form-encoded one. Errors (bad/expired code, wrong secret) come back **HTTP
  200 with an `error` field**, not a 4xx — the client must check the body.
- **No id_token.** GitHub is not an OIDC provider for user login: the exchange
  yields only an opaque user-to-server access token (`ghu_…` when expiring).
  Identity is then read from the REST API with that token — there is nothing to
  signature-verify locally; authenticity comes from the token exchange having
  been performed directly against `github.com` with the client secret.
- **Scopes are ignored for GitHub Apps.** A GitHub App's user-to-server token's
  abilities come from the App's configured permissions (plus the user's own
  access), not from a `scope` parameter; `scope` on the authorize URL is
  ignored. The App has **Account → Email addresses: Read-only** and
  **Organization → Members: Read-only** configured.
- **Who the user is: `GET https://api.github.com/user`** (header
  `Authorization: Bearer <token>`, `Accept: application/vnd.github+json`).
  Relevant fields: `id` (immutable numeric user id — the stable subject;
  `login` can be changed by the user), `login`, `name` (nullable), `avatar_url`
  (public CDN URL, same display-avatar properties as Google's `picture`),
  `email` (**null unless the user made an email public** — unreliable).
- **The real email: `GET https://api.github.com/user/emails`** — a JSON array
  of `{email, primary, verified, visibility}`. The design uses the entry with
  `primary: true`; its `verified` flag is GitHub's own verification state.
  Requires the Email-addresses account permission above.
- **Org membership: `GET https://api.github.com/user/memberships/orgs/{org}`**
  with the *user's own* token — returns `{state, role, …}` where `state` is
  `active` or `pending`; **404** when the authenticated user has no membership
  at all. Because the App is installed on the org and has Members: Read-only,
  a user-to-server token can read the caller's own membership. (The
  alternative, `GET /orgs/{org}/members/{username}` with an installation
  token, would require the App private key in the dashboard — rejected; the
  key stays confined to the `github` service.)
- **No forced re-auth.** GitHub has no equivalent of OIDC's `prompt=login`; if
  the browser already has a GitHub session, the authorize hop may complete
  silently. Accepted behavior.
- **Token lifecycle.** With "user-to-server token expiration" enabled the token
  expires in 8 hours and a refresh token is issued. Irrelevant here: the
  dashboard uses the token for the three reads above during the callback and
  discards it — nothing is stored.
- **Credentials on disk.** `~/.secrets/IKIGENBA_APP_CLIENT_ID` and
  `~/.secrets/IKIGENBA_APP_CLIENT_SECRET` hold the pair; the org name lives in
  `~/.secrets/IKIGENBA_GITHUB_ORG`. The dashboard consumes them as env vars
  (`GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_ORG`) via `.envrc`
  references, matching the `GOOGLE_*` convention.
- **Callback URLs registered on the App:** `http://localhost:8080/oauth/github/callback`
  (dev) and `https://int.ikigenba.com/oauth/github/callback` (prod).

## One authorization endpoint per AS metadata document

RFC 8414 AS metadata (what MCP clients discover at
`/.well-known/oauth-authorization-server`) carries exactly **one**
`authorization_endpoint`, and MCP clients follow it non-interactively — there
is no client UI for choosing among several. Multi-IdP sites therefore keep one
authorize URL and put the provider choice *inside* the flow as a human-facing
chooser page. This is why the design adds a chooser rather than a second
authorize endpoint.

## HTTP header value constraints

The identity is surfaced to services as response headers on the dashboard's
`auth_request` introspection endpoints, which nginx forwards upstream. Raw HTTP
header values must be **US-ASCII and free of CR/LF**; a stray newline or
non-ASCII byte can corrupt or inject a header. `iss`/`sub` (ASCII) and the
`picture` URL (ASCII) are safe as-is, but **`name` is Unicode** (accents, CJK,
emoji). Any attribute placed in a header is therefore **percent-encoded**
(UTF-8 then `%`-escaped) so the emitted value is guaranteed ASCII and
injection-safe; a consumer percent-decodes to recover the original.

## Existing local id primitive

`internal/ids` already provides `ids.New()` — an opaque, random 128-bit value,
base32-encoded (no padding), carrying **no** embedded timestamp. PATs and OAuth
tokens are already built from it. It has the properties an external identity
handle wants (opaque, collision-free without coordination, URL-safe, leaks
neither user count nor provider), so the identity handle reuses it rather than
introducing a ULID/UUID dependency. The only ULID property forgone is lexical
time-sortability, which an identity handle does not need.
