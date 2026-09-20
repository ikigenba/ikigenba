# D06-check

The two identity endpoints auth serves on its loopback port, `GET /check` and
`GET /me`, both attached in `internal/server`. `/check` is the subrequest nginx
issues for every routed app: nginx forwards the original request's `Cookie` and
`Authorization` headers with no body and acts on the status auth returns — 200
means copy the identity headers onto the upstream request, 401 means redirect
the browser to sign in, 403 means pass the refusal through. `/me` is the public
"who am I" endpoint an agent or a signed-in user calls directly.

A credential reaches either endpoint one of two ways: the session cookie named
by D05 (`ikigenba_session=<session-id>`), or an `Authorization: Bearer
ikp_<secret>` header whose whole `ikp_`-prefixed value is the personal access
token secret. This design owns the two identity-header names auth emits on
success and the rule for reading a credential off the request; it references
the session cookie name (D05), the `internal/store` `Identity` type and its
liveness rules (D04), and the touching / non-touching store operations (D04).

The dividing line between the two endpoints is mutation. `/check` counts a
request as use: it resolves identity through the touching store operations
(`TouchSession`, `TouchTokenIdentity`), which update last-use. `/me` never
mutates: it resolves through the read-only operations (`LookupSessionIdentity`,
`LookupTokenIdentity`). The dividing line between 401 and 403 is which
credential failed: a request with no bearer token is decided by the session
cookie, and a missing or no-longer-live session is answerable by signing in
again (401); a request that presents a bearer token is decided by that token
alone, and a token the store will not honor is a refusal to pass through (403).
A token, when present, wins outright — the session cookie is never consulted —
so the two never disagree. Unknown, disabled, expired, and stale-owner tokens
are one indistinguishable `ErrNotFound` from D04, so all four refuse
identically with no hint of which applied.

## REQUIREMENTS

- R-F4UP-FSRL: The `internal/server` package MUST export the identity-header name constants `HeaderUserID = "X-User-Id"` and `HeaderUserEmail = "X-User-Email"`.
- R-F62L-TKIA: The `/check` and `/me` handlers MUST read a bearer credential from an `Authorization` header bearing the `Bearer ` scheme, passing the entire token value that follows `Bearer ` (the `ikp_`-prefixed secret) unmodified to the store operation that hashes it; and MUST read a session credential from the request cookie named `ikigenba_session` (D05), passing its value as the session id.
- R-F7AI-7C8Z: When a request carries an `Authorization` header with the `Bearer ` scheme, the `/check` and `/me` handlers MUST resolve identity solely through the token store operation and MUST NOT consult the `ikigenba_session` cookie, whether or not the token is honored.
- R-F8IE-L3ZO: A `GET /check` carrying no bearer credential and an `ikigenba_session` cookie whose session `TouchSession` (D04) reports live MUST respond 200 with `HeaderUserID` set to the returned `Identity.UserID` and `HeaderUserEmail` set to the returned `Identity.Email`, and MUST leave the session's last-use time updated to the request time (the request counts as use).
- R-F9QA-YVQD: A `GET /check` carrying neither an `Authorization` header nor an `ikigenba_session` cookie MUST respond 401, MUST set neither `HeaderUserID` nor `HeaderUserEmail`, and MUST update no stored session or token.
- R-FAY7-CNH2: A `GET /check` carrying no bearer credential and an `ikigenba_session` cookie whose session is idle beyond the D04 idle window but still within the D04 cap (testable case: last use 20 minutes ago, login 3 hours ago) MUST respond 401 with neither identity header set, and MUST leave that session's last-use time unchanged (the request does not count as use).
- R-FC63-QF7R: A `GET /check` carrying no bearer credential and an `ikigenba_session` cookie whose session is past the D04 cap even though recently used (testable case: login 18 hours 30 minutes ago, last use 1 minute ago) MUST respond 401 with neither identity header set, and MUST leave that session's last-use time unchanged.
- R-FDE0-46YG: A `GET /check` carrying an `Authorization: Bearer` credential that `TouchTokenIdentity` (D04) honors MUST respond 200 with `HeaderUserID` set to the returned `Identity.UserID` and `HeaderUserEmail` set to the returned `Identity.Email`, and MUST leave the token's last-used time updated to the request time.
- R-FFTS-VQFU: A `GET /check` whose `Authorization: Bearer` credential `TouchTokenIdentity` (D04) does not honor — for any of the indistinguishable D04 `ErrNotFound` causes: unknown, disabled, expired, or owner's last Google login older than the D04 login window — MUST respond 403 with neither identity header set and MUST update no stored session or token, the response being byte-for-byte identical across those causes.
- R-FH1P-9I6J: A `GET /check` carrying both a live `ikigenba_session` cookie and an `Authorization: Bearer` credential that `TouchTokenIdentity` honors MUST respond 200 with the identity headers set to the token owner's `Identity` (not the session owner's), MUST leave the token's last-used time updated to the request time, and MUST leave the cookie's session last-use time unchanged (the session is not consulted or counted as use).
- R-FI9L-N9X8: The `/check` responses MUST convey identity only through the `HeaderUserID` and `HeaderUserEmail` headers; the `/check` response body is not part of the contract and tests MUST NOT assert its content.
- R-FJHI-11NX: A `GET /me` carrying an `Authorization: Bearer` credential that `LookupTokenIdentity` (D04) honors MUST respond 200 with `Content-Type: application/json` and a body that is exactly the compact JSON object `{"id":"<user-id>","email":"<email>"}` — keys `id` then `email`, no insignificant whitespace — where `id` is the returned `Identity.UserID` and `email` is the returned `Identity.Email`, and MUST update no stored session or token.
- R-FKPE-ETEM: A `GET /me` carrying no bearer credential and an `ikigenba_session` cookie whose session `LookupSessionIdentity` (D04) reports live MUST respond 200 with `Content-Type: application/json` and a body exactly the compact JSON object `{"id":"<user-id>","email":"<email>"}` filled with the session owner's `Identity.UserID` and `Identity.Email`, and MUST update no stored session or token.
- R-GAHD-7316: A `GET /me` carrying no bearer credential and either no `ikigenba_session` cookie or an `ikigenba_session` cookie whose value names no live session MUST respond 401 with `Content-Type: text/plain; charset=utf-8` and a body that is a single line of plain text and is not a JSON object.
- R-2TME-OFZ2: A `GET /me` whose `Authorization: Bearer` credential `LookupTokenIdentity` (D04) does not honor — for any of the indistinguishable D04 `ErrNotFound` causes: unknown, disabled, expired, or owner's last Google login older than the D04 login window — MUST respond 403 with `Content-Type: text/plain; charset=utf-8` and a body that is a single line of plain text stating the token was refused and is not a JSON object, identical across those causes.
- R-2UUB-27PR: No `GET /me` request, on any path (honored token, live session, no credential, or refused token), MUST update any stored session's last-use time or any stored token's last-used time, nor perform any other store write.
- R-2W27-FZGG: On `/check` and `/me`, a request presenting no bearer credential whose session cookie is absent or not live MUST be answered 401, and a request presenting a bearer credential the store will not honor MUST be answered 403; a 401 MUST NOT be derived from a refused bearer token and a 403 MUST NOT be derived from a session-cookie outcome.
