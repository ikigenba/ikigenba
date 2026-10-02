# D06-check

The two identity endpoints auth serves, `GET /check` and `GET /me`, both
attached in `internal/server`. `/check` is the subrequest nginx issues for
every routed app, an HTTP request for `/check` like any other: nginx forwards
the original request's `Cookie` and `Authorization` headers with no body and
with its own `X-Request-Id` for the request, names the request it is
deciding in three headers of its own making — `X-Original-Method`, its method;
`X-Original-Host`, its host name; and `X-Original-URI`, its path and query as
the client sent them — and acts on the status auth returns — 200 means copy the identity headers onto the upstream request, 401
means redirect the browser to sign in, 403 means pass the refusal through.
`/me` is the public "who am I" endpoint an agent or a signed-in user calls
directly.

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

Both endpoints decide every request from the database, so when the database
fails the read or write a request needs, auth cannot decide it: that is auth's
fault, not the caller's, and D03 answers it `500` with no identity header.
It is a handled failure, so it is in the trail rather than on stderr: the
request's `request.finished` carries the `500`, and the check records
`check.failed`. nginx treats any `/check` answer other than 200, 401 and 403
as its own failure, so the visitor sees an error and the app is never
reached. No answer this design states writes anything to stderr.

## What the check records

Every `GET /check` records exactly one check event, beside the request events
every request records (D03), because a request `/check` refuses never reaches
an app and the check is the only place that sees it. The event is
`check.allowed` for a 200, `check.refused` for a 401 or 403, and
`check.failed` for a 500 — the last so that the method, host, and path of a
request that never reached an app are still on record when auth's database
fails. It carries the request's id, and, only when the check allowed it, the
user the check resolved to: nginx sends no `X-User-Id` on the subrequest, so
the middleware's context names no user, and the check supplies the resolved
one itself, keeping the request id. A refused or failed check names no user.

Its attributes are all strings: `outcome` (`allowed`, `unauthenticated`,
`forbidden`, or `failed`, one per status); `credential`, the kind presented
(`token` for a bearer token, else `session` for a session cookie, else
`none`), following the same precedence that decides the identity; `method`,
`host`, and `path`, read from the three `X-Original-` headers, the path with
its query cut off at the first `?` because a query can carry data and the
trail carries metadata only; and `token`, the honored token's `tok_` id, only
when a token was honored. A header nginx did not send is recorded as the empty
string and never changes the answer — a developer calling `/check` by hand is
decided as nginx's subrequest would be. A refused token carries no `token`
attribute even when its secret matched a stored token, so a refused token's
four causes stay as indistinguishable in the trail as they are at the door.
The event never carries a secret, a session id, or an email.

No further attribute distinguishes one actor from another, by decision: the
`credential` kind and the honored token's id are enough to separate one user's
concurrent actors after the fact. Each agent holds its own token, so its
requests carry a distinct token id, while all of one user's browser sessions
share `credential=session` and are deliberately not told apart, because a
session id is a secret. An app's own events join to the check by request id.

`/me` is not a check: it records only its request events.

## REQUIREMENTS

- R-J8XR-4IF9: The `internal/server` package MUST export `HeaderUserID` and `HeaderUserEmail` as untyped string constants whose values are `"X-User-Id"` and `"X-User-Email"`, so that each is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration of any type whose underlying type is `string`.
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
- R-TJWJ-B9P3: For each `GET /check` auth answers, auth MUST record (D05) exactly one event whose name begins `check.` — named `check.allowed` when it answers `200`, `check.refused` when it answers `401` or `403`, and `check.failed` when it answers `500` — and MUST record no event for that request other than that one and its `request.started` and `request.finished` events.
- R-TL4F-P1FS: The envelope request id of the check event R-TJWJ-B9P3 requires MUST be the request's request id (D05); its envelope user MUST be, for `check.allowed`, the `UserID` of the `Identity` the check resolved, the value the response's `HeaderUserID` carries, and, for `check.refused` and `check.failed`, empty.
- R-TMCC-2T6H: The attributes of the check event R-TJWJ-B9P3 requires MUST be exactly the keys `outcome`, `credential`, `method`, `host`, and `path`, each with a `string` value, together with the key `token`, with a `string` value, exactly when the event is `check.allowed` and its `credential` is `token`.
- R-TNK8-GKX6: The `outcome` attribute of the check event R-TJWJ-B9P3 requires MUST be `allowed` when auth answers the `GET /check` `200`, `unauthenticated` when it answers `401`, `forbidden` when it answers `403`, and `failed` when it answers `500`.
- R-TOS4-UCNV: The `credential` attribute of the check event R-TJWJ-B9P3 requires MUST be `token` when the request carries an `Authorization` header with the `Bearer ` scheme (R-F62L-TKIA), otherwise `session` when it carries a cookie named `ikigenba_session`, and otherwise `none`, whatever auth answers.
- R-TQ01-84EK: The `method` attribute of the check event R-TJWJ-B9P3 requires MUST be the request's first `X-Original-Method` value, its `host` attribute the request's first `X-Original-Host` value, and its `path` attribute the request's first `X-Original-URI` value with everything from its first `?` on removed (the whole value when it holds no `?`), each otherwise unaltered, and each the empty string when the request carries no such header.
- R-TR7X-LW59: A `GET /check` MUST be answered with the same status and identity headers, and MUST change the same stored session and token state, whether or not it carries `X-Original-Method`, `X-Original-Host`, or `X-Original-URI` and whatever values they hold.
- R-TSFT-ZNVY: The `token` attribute of a `check.allowed` event whose `credential` is `token` MUST be the `ID` of the token whose secret is the request's bearer credential — the `TokenID` of the `Identity` that `TouchTokenIdentity` (D04) returned for it.
- R-TTNQ-DFMN: For any two `GET /check` requests that carry the same `X-Request-Id`, `X-Original-Method`, `X-Original-Host`, and `X-Original-URI` values and an `Authorization: Bearer` credential that `TouchTokenIdentity` (D04) does not honor, for any of the D04 `ErrNotFound` causes — unknown, disabled, expired, or owner's last Google login older than the D04 login window — the check events auth records MUST be identical apart from their `Time`.
- R-TUVM-R7DC: For a `GET /me` request, whatever auth answers, auth MUST record no event other than its `request.started` and `request.finished` events.
