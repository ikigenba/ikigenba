# dashboard — Research

**Non-contractual external ground truth.** This file collects the external
facts the design leans on so design need not re-derive them. The build loop
never reads it. It is rewritten in place to stay a single coherent statement of
what is currently known, not a running log.

## Google OpenID Connect id_token — the identity claims

The dashboard federates identity to Google only. `internal/googleidp` performs
the authorization-code exchange and verifies the returned **id_token** (RS256
against Google's JWKS, `iss`/`aud`/`exp` checked). Everything below is about
that verified id_token; the dashboard makes **no** separate `userinfo` call.

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
