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

A credential reaches either endpoint one of three ways: the session cookie
named by D05 (`ikigenba_session=<session-id>`); an `Authorization: Bearer
ikp_<secret>` header whose whole `ikp_`-prefixed value is the personal access
token secret; or an `Authorization: Basic <base64>` header, the form git sends
over HTTP, where `<base64>` is the standard base64 of `<username>:<secret>`.
The two header forms are both token credentials. In a Basic credential the
password, everything after the first `:`, is the token secret, decided exactly
as the same secret sent as a bearer: the same identity, the same refusals, the
same use recorded. The username is ignored — never compared, stored, or
recorded. A Basic value that is empty, is not standard base64, or decodes to
bytes with no `:` is malformed: it is still a token credential, so it is
refused 403 like a token auth will not honor, never answered 401. Neither
scheme is matched in any other letter case, as git and every other client
send them as written here; an `Authorization` header with any other scheme is
not a credential, and the request is decided by its cookie. auth issues no
challenge: no answer either endpoint gives carries `WWW-Authenticate`. A
client that waits for a challenge before sending Basic, as git does, gets it
from the space's nginx in front of the app it calls, not from auth. This design owns the two identity-header names auth emits on
success and the rule for reading a credential off the request; it references
the session cookie name (D05), the `internal/store` `Identity` type and its
liveness rules (D04), and the touching / non-touching store operations (D04).

The dividing line between the two endpoints is mutation. `/check` counts a
request as use: it resolves identity through the touching store operations
(`TouchSession`, `TouchTokenIdentity`), which update last-use. `/me` never
mutates: it resolves through the read-only operations (`LookupSessionIdentity`,
`LookupTokenIdentity`). The dividing line between 401 and 403 is which
credential failed: a request with no token credential is decided by the
session cookie, and a missing or no-longer-live session is answerable by
signing in again (401); a request that presents a token credential, bearer or
Basic, is decided by that token alone, and a token the store will not honor,
or a malformed Basic credential, is a refusal to pass through (403).
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
(`token` for a bearer token, `basic` for a Basic one, malformed or not, else
`session` for a session cookie, else `none`), following the same precedence
that decides the identity; `method`,
`host`, and `path`, read from the three `X-Original-` headers, the path with
its query cut off at the first `?` because a query can carry data and the
trail carries metadata only; and `token`, the honored token's `tok_` id, only
when a token was honored, whether it came as `token` or `basic`. A header nginx did not send is recorded as the empty
string and never changes the answer — a developer calling `/check` by hand is
decided as nginx's subrequest would be. A refused token carries no `token`
attribute even when its secret matched a stored token, so a refused token's
four causes stay as indistinguishable in the trail as they are at the door.
A refused or malformed Basic credential's events are identical whatever its
username and whatever the cause. The event never carries a secret, a Basic
credential's username or encoded value, a session id, or an email.

No further attribute distinguishes one actor from another, by decision: the
`credential` kind and the honored token's id are enough to separate one user's
concurrent actors after the fact. Each agent holds its own token, so its
requests carry a distinct token id, while all of one user's browser sessions
share `credential=session` and are deliberately not told apart, because a
session id is a secret. An app's own events join to the check by request id.

`/me` is not a check: it records only its request events.

## REQUIREMENTS

- R-J8XR-4IF9: The `internal/server` package MUST export `HeaderUserID` and `HeaderUserEmail` as untyped string constants whose values are `"X-User-Id"` and `"X-User-Email"`, so that each is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration of any type whose underlying type is `string`.
- R-NKMT-RO74: auth's design defines that a request **presents a token credential** when its first `Authorization` value begins `Bearer ` or begins `Basic `, compared byte for byte; its **credential kind** is then `token` for `Bearer ` and `basic` for `Basic `; its **token secret** is, for `Bearer `, everything after `Bearer `, unmodified, and, for `Basic `, when everything after `Basic ` is non-empty, is accepted without error by the standard library's `base64.StdEncoding.DecodeString`, and decodes to bytes holding a `:`, everything in those decoded bytes after their first `:`, unmodified, the bytes before it (the username) being ignored; and a `Basic ` value that fails any of those three conditions is a **malformed Basic credential**, which presents a token credential of kind `basic` but has no token secret. The `/check` and `/me` handlers MUST pass a request's token secret unmodified to the token store operation that hashes it, and MUST read a session credential from the request cookie named `ikigenba_session` (D05), passing its value as the session id; every requirement of auth's design that names these terms MUST denote that.
- R-NN2M-J7OI: When a request presents a token credential (R-NKMT-RO74), the `/check` and `/me` handlers MUST resolve identity solely from its token secret through the token store operation, and MUST NOT consult the `ikigenba_session` cookie, whether or not the token is honored and whether or not the credential is a malformed Basic credential.
- R-NQQB-OIWL: A `GET /check` presenting no token credential (R-NKMT-RO74) and an `ikigenba_session` cookie whose session `TouchSession` (D04) reports live MUST respond 200 with `HeaderUserID` set to the returned `Identity.UserID` and `HeaderUserEmail` set to the returned `Identity.Email`, and MUST leave the session's last-use time updated to the request time (the request counts as use).
- R-F9QA-YVQD: A `GET /check` carrying neither an `Authorization` header nor an `ikigenba_session` cookie MUST respond 401, MUST set neither `HeaderUserID` nor `HeaderUserEmail`, and MUST update no stored session or token.
- R-NT64-G2DZ: A `GET /check` presenting no token credential (R-NKMT-RO74) and an `ikigenba_session` cookie whose session is idle beyond the D04 idle window but still within the D04 cap (testable case: last use 20 minutes ago, login 3 hours ago) MUST respond 401 with neither identity header set, and MUST leave that session's last-use time unchanged (the request does not count as use).
- R-NWTT-LDM2: A `GET /check` presenting no token credential (R-NKMT-RO74) and an `ikigenba_session` cookie whose session is past the D04 cap even though recently used (testable case: login 18 hours 30 minutes ago, last use 1 minute ago) MUST respond 401 with neither identity header set, and MUST leave that session's last-use time unchanged.
- R-FDE0-46YG: A `GET /check` carrying an `Authorization: Bearer` credential that `TouchTokenIdentity` (D04) honors MUST respond 200 with `HeaderUserID` set to the returned `Identity.UserID` and `HeaderUserEmail` set to the returned `Identity.Email`, and MUST leave the token's last-used time updated to the request time.
- R-FFTS-VQFU: A `GET /check` whose `Authorization: Bearer` credential `TouchTokenIdentity` (D04) does not honor — for any of the indistinguishable D04 `ErrNotFound` causes: unknown, disabled, expired, or owner's last Google login older than the D04 login window — MUST respond 403 with neither identity header set and MUST update no stored session or token, the response being byte-for-byte identical across those causes.
- R-FH1P-9I6J: A `GET /check` carrying both a live `ikigenba_session` cookie and an `Authorization: Bearer` credential that `TouchTokenIdentity` honors MUST respond 200 with the identity headers set to the token owner's `Identity` (not the session owner's), MUST leave the token's last-used time updated to the request time, and MUST leave the cookie's session last-use time unchanged (the session is not consulted or counted as use).
- R-FI9L-N9X8: The `/check` responses MUST convey identity only through the `HeaderUserID` and `HeaderUserEmail` headers; the `/check` response body is not part of the contract and tests MUST NOT assert its content.
- R-FJHI-11NX: A `GET /me` carrying an `Authorization: Bearer` credential that `LookupTokenIdentity` (D04) honors MUST respond 200 with `Content-Type: application/json` and a body that is exactly the compact JSON object `{"id":"<user-id>","email":"<email>"}` — keys `id` then `email`, no insignificant whitespace — where `id` is the returned `Identity.UserID` and `email` is the returned `Identity.Email`, and MUST update no stored session or token.
- R-NZ9M-CX3G: A `GET /me` presenting no token credential (R-NKMT-RO74) and an `ikigenba_session` cookie whose session `LookupSessionIdentity` (D04) reports live MUST respond 200 with `Content-Type: application/json` and a body exactly the compact JSON object `{"id":"<user-id>","email":"<email>"}` filled with the session owner's `Identity.UserID` and `Identity.Email`, and MUST update no stored session or token.
- R-O2XB-I8BJ: A `GET /me` presenting no token credential (R-NKMT-RO74) and either no `ikigenba_session` cookie or an `ikigenba_session` cookie whose value names no live session MUST respond 401 with `Content-Type: text/plain; charset=utf-8` and a body that is a single line of plain text and is not a JSON object.
- R-2TME-OFZ2: A `GET /me` whose `Authorization: Bearer` credential `LookupTokenIdentity` (D04) does not honor — for any of the indistinguishable D04 `ErrNotFound` causes: unknown, disabled, expired, or owner's last Google login older than the D04 login window — MUST respond 403 with `Content-Type: text/plain; charset=utf-8` and a body that is a single line of plain text stating the token was refused and is not a JSON object, identical across those causes.
- R-2UUB-27PR: No `GET /me` request, on any path (honored token, live session, no credential, or refused token), MUST update any stored session's last-use time or any stored token's last-used time, nor perform any other store write.
- R-O5D4-9RSX: On `/check` and `/me`, a request presenting no token credential (R-NKMT-RO74) whose session cookie is absent or not live MUST be answered 401, and a request presenting a token credential that is a malformed Basic credential or whose token secret the store will not honor MUST be answered 403; a 401 MUST NOT be derived from a token credential and a 403 MUST NOT be derived from a session-cookie outcome.
- R-O7SX-1BAB: A `GET /check` or `GET /me` presenting a token credential of kind `basic` that has a token secret (R-NKMT-RO74) MUST be answered with the same status, the same `HeaderUserID`, `HeaderUserEmail`, and `Content-Type` values, each absent exactly when it is absent there, and, for `/me`, the same body, and MUST change the same stored session and token state, as the same request with its first `Authorization` value replaced by `Bearer ` followed by that token secret.
- R-OBGM-6MIE: A `GET /check` or `GET /me` presenting a malformed Basic credential (R-NKMT-RO74) MUST be answered with the same status, the same `HeaderUserID`, `HeaderUserEmail`, and `Content-Type` values, each absent exactly when it is absent there, and, for `/me`, the same body, as the same request with its first `Authorization` value replaced by `Bearer ` followed by a value that matches no stored token, and MUST update no stored session or token.
- R-ODWE-Y5ZS: No answer auth gives to a request for `/check` or `/me`, whatever its status, MUST carry a `WWW-Authenticate` header field.
- R-TJWJ-B9P3: For each `GET /check` auth answers, auth MUST record (D05) exactly one event whose name begins `check.` — named `check.allowed` when it answers `200`, `check.refused` when it answers `401` or `403`, and `check.failed` when it answers `500` — and MUST record no event for that request other than that one and its `request.started` and `request.finished` events.
- R-TL4F-P1FS: The envelope request id of the check event R-TJWJ-B9P3 requires MUST be the request's request id (D05); its envelope user MUST be, for `check.allowed`, the `UserID` of the `Identity` the check resolved, the value the response's `HeaderUserID` carries, and, for `check.refused` and `check.failed`, empty.
- R-OGC7-PPH6: The attributes of the check event R-TJWJ-B9P3 requires MUST be exactly the keys `outcome`, `credential`, `method`, `host`, and `path`, each with a `string` value, together with the key `token`, with a `string` value, exactly when the event is `check.allowed` and its `credential` is `token` or `basic`.
- R-TNK8-GKX6: The `outcome` attribute of the check event R-TJWJ-B9P3 requires MUST be `allowed` when auth answers the `GET /check` `200`, `unauthenticated` when it answers `401`, `forbidden` when it answers `403`, and `failed` when it answers `500`.
- R-OIS0-H8YK: The `credential` attribute of the check event R-TJWJ-B9P3 requires MUST be the request's credential kind (R-NKMT-RO74) when the request presents a token credential, a malformed Basic credential included, otherwise `session` when it carries a cookie named `ikigenba_session`, and otherwise `none`, whatever auth answers.
- R-TQ01-84EK: The `method` attribute of the check event R-TJWJ-B9P3 requires MUST be the request's first `X-Original-Method` value, its `host` attribute the request's first `X-Original-Host` value, and its `path` attribute the request's first `X-Original-URI` value with everything from its first `?` on removed (the whole value when it holds no `?`), each otherwise unaltered, and each the empty string when the request carries no such header.
- R-TR7X-LW59: A `GET /check` MUST be answered with the same status and identity headers, and MUST change the same stored session and token state, whether or not it carries `X-Original-Method`, `X-Original-Host`, or `X-Original-URI` and whatever values they hold.
- R-OMFP-MK6N: The `token` attribute of a `check.allowed` event whose `credential` is `token` or `basic` MUST be the `ID` of the token whose secret is the request's token secret (R-NKMT-RO74) — the `TokenID` of the `Identity` that `TouchTokenIdentity` (D04) returned for it.
- R-2XQU-WL0O: For any two `GET /check` requests whose first `X-Request-Id` values are the same non-empty value, that carry the same `X-Original-Method`, `X-Original-Host`, and `X-Original-URI` values, and that carry an `Authorization: Bearer` credential that `TouchTokenIdentity` (D04) does not honor, for any of the D04 `ErrNotFound` causes — unknown, disabled, expired, or owner's last Google login older than the D04 login window — the check events auth records MUST be identical apart from their `Time`.
- R-OOVI-E3O1: For any two `GET /check` requests whose first `X-Request-Id` values are the same non-empty value, that carry the same `X-Original-Method`, `X-Original-Host`, and `X-Original-URI` values, and each of which presents a token credential of kind `basic` (R-NKMT-RO74) that is either a malformed Basic credential or one whose token secret `TouchTokenIdentity` (D04) does not honor, for any of the D04 `ErrNotFound` causes — unknown, disabled, expired, or owner's last Google login older than the D04 login window — whatever their usernames, the check events auth records MUST be identical apart from their `Time`.
- R-TUVM-R7DC: For a `GET /me` request, whatever auth answers, auth MUST record no event other than its `request.started` and `request.finished` events.
