# Stories — check

The two endpoints auth serves for deciding who a request belongs to.
`GET /check` is the subrequest endpoint nginx calls for every routed app: nginx
forwards the original request's `Cookie` and `Authorization` headers with no
body and with its own `X-Request-Id` for the request, written `<request-id>`
below, and names the request it is deciding in three headers of its own
making: `X-Original-Method`, its method; `X-Original-Host`, its host name; and
`X-Original-URI`, its path and query exactly as the client sent them. nginx
acts on the status auth returns — 200 means copy the identity headers onto the
upstream request, 401 means redirect the browser to sign in, 403 means pass
the refusal through.
`GET /me` is the public "who am I" endpoint an agent or a signed-in user can
call directly. The requests, standing in for nginx or for the caller, go to a
running auth (`S2-serve.md`), started with its Google settings; they reach it
through nginx on a space. Each request is shown as the HTTP request auth
receives, with the headers the story depends on. Every request is on a space;
a request that shows no `Host` header carries `Host: auth.sbx.ikigenba.dev`,
on the space `sbx.ikigenba.dev` (`S3-sign-in.md`).

A credential reaches these endpoints one of two ways: the session cookie
`ikigenba_session=<session-id>`, or `Authorization: Bearer ikp_<token>`. Both
name the same kind of user; apps cannot tell a cookie login from a token login.
On success the identity is two values: `X-User-Id`, an opaque id auth minted for
the user (16 random bytes in Crockford base32, 26 characters — never Google's
subject, never the email; written here as `<user-id>`), and `X-User-Email`, the
user's email. A session ends at the earlier of 18 hours after login and 15
minutes after its last use, and every request through `/check` counts as use. A
token is honored only while its owner has logged in through Google within the
last 30 days. Besides its secret, a token has an id, `tok_` followed by 26
Crockford base32 characters (`S5-tokens.md`), written `<token-id>` below; the
id is not the secret and authenticates nothing. The last-use time of a session
and the last-used time of a token are updated in `/check` and nowhere else;
`/me` never mutates anything.

Every `GET /check` records exactly one check event in the trail, beside the
request events every request records (`S2-serve.md`), because a request
`/check` refuses never reaches an app and the check is the only place that
sees it. The event is `check.allowed` when auth answers 200, `check.refused`
when it answers 401 or 403, and `check.failed` when it answers 500. It carries
the request id `<request-id>`, and the user the check resolved to when it is
`check.allowed`; a refused or failed check names no user. Its attributes are
exactly these strings: `outcome` — `allowed` for 200, `unauthenticated` for
401, `forbidden` for 403, `failed` for 500; `credential` — `token` when the
request presented `Authorization: Bearer`, otherwise `session` when it
presented an `ikigenba_session` cookie, otherwise `none`; `method`, `host`, and
`path`, from `X-Original-Method`, `X-Original-Host`, and `X-Original-URI`, the
path being the URI with everything from its first `?` removed, so no query
string enters the trail; and `token`, the honored token's `<token-id>`, only
when the credential is a token auth honored. A header nginx did not send is
recorded as the empty string and never fails the check. The event never
carries a token's secret, a session id, or an email, and a refused token's
causes stay as indistinguishable in the trail as they are to nginx. `/me` is
not a check: it records no check event, only the request events.

`/check` is only meant to be called by nginx as its internal subrequest; it is not
reachable from the public side of a space, and that unreachability (a request to
`https://<space>/check` gets 404) is proven in `S7-on-a-space.md`, not here.

## nginx checks a request with a live session

nginx makes this subrequest for every routed request that carries the session
cookie. auth answers 200 and hands back the identity headers, and it counts the
request as use of the session.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Cookie: ikigenba_session=<session-id>
```

Response:

```
HTTP/1.1 200 OK
X-User-Id: <user-id>
X-User-Email: <email>
```

Status 200. The response fixes no body; nginx reads only `X-User-Id` and
`X-User-Email` and copies them onto the upstream request.

Preconditions:

- A session exists, named by `ikigenba_session=<session-id>`, whose last use was
  5 minutes ago (within the 15-minute idle window) and whose login was 2 hours
  ago (within the 18-hour cap).
- That session belongs to the user with id `<user-id>` and email `<email>`.

Postconditions:

- The session's last-use time is updated to now (the session is touched).
- auth records `check.allowed` with `outcome=allowed`, `credential=session`,
  `method=GET`, `host=dummy.sbx.ikigenba.dev`, and `path=/widgets`, under
  request id `<request-id>` and user `<user-id>`. The query `?page=2` is not in
  the trail.
- Nothing else has changed.

## nginx checks a request with no credential

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
```

Response:

```
HTTP/1.1 401 Unauthorized
```

Status 401. The response sets no `X-User-Id` or `X-User-Email`. The body is not
fixed; nginx maps the 401 to a login redirect and does not read it.

Preconditions:

- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- auth records `check.refused` with `outcome=unauthenticated`,
  `credential=none`, `method=GET`, `host=dummy.sbx.ikigenba.dev`, and
  `path=/widgets`, under request id `<request-id>` and no user.
- Nothing else has changed.

## nginx checks a request with a session idle too long

A session that has not been used within the last 15 minutes is expired, even
though the 18-hour cap has not been reached.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Cookie: ikigenba_session=<session-id>
```

Response:

```
HTTP/1.1 401 Unauthorized
```

Status 401. The response sets no identity headers, exactly as for no credential;
from nginx's side the two are the same.

Preconditions:

- A session exists, named by `ikigenba_session=<session-id>`, whose last use was
  20 minutes ago (more than 15 minutes) and whose login was 3 hours ago (within
  the 18-hour cap).

Postconditions:

- The session is expired. Its last-use time is not updated; the request does not
  count as use.
- auth records `check.refused` with `outcome=unauthenticated`,
  `credential=session`, `method=GET`, `host=dummy.sbx.ikigenba.dev`, and
  `path=/widgets`, under request id `<request-id>` and no user.

## nginx checks a request with a session past its cap

A session reaches its 18-hour cap counting from login, no matter how recently it
was used. Recent use cannot extend it past the cap.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Cookie: ikigenba_session=<session-id>
```

Response:

```
HTTP/1.1 401 Unauthorized
```

Status 401. The response sets no identity headers.

Preconditions:

- A session exists, named by `ikigenba_session=<session-id>`, whose login was 18
  hours and 30 minutes ago (past the 18-hour cap) but whose last use was 1 minute
  ago (well within the 15-minute idle window).

Postconditions:

- The session is expired. Its last-use time is not updated.
- auth records `check.refused` with `outcome=unauthenticated`,
  `credential=session`, `method=GET`, `host=dummy.sbx.ikigenba.dev`, and
  `path=/widgets`, under request id `<request-id>` and no user.

## nginx checks a request with a token

A request may carry a personal access token instead of a cookie. auth answers
200 with the token owner's identity and records the token's use.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Authorization: Bearer ikp_<token>
```

Response:

```
HTTP/1.1 200 OK
X-User-Id: <user-id>
X-User-Email: <email>
```

Status 200. The response fixes no body; nginx reads only the two identity
headers.

Preconditions:

- A token exists whose value is `ikp_<token>` and whose id is `<token-id>`: it
  is enabled, and it is either unexpired or has no expiry.
- The token's owner has id `<user-id>` and email `<email>`, and that owner's
  most recent Google login was 3 days ago (within the last 30 days).
- The request carries no `ikigenba_session` cookie.

Postconditions:

- The token's last-used time is updated to now.
- auth records `check.allowed` with `outcome=allowed`, `credential=token`,
  `method=GET`, `host=dummy.sbx.ikigenba.dev`, `path=/widgets`, and
  `token=<token-id>`, under request id `<request-id>` and user `<user-id>`.
  Neither the secret nor the email is in the trail.
- Nothing else has changed.

## nginx checks a request with a token that is unknown, disabled, or expired

A bearer token that cannot be honored yields 403, and nginx passes the 403
through rather than redirecting to sign in. An unknown token, a disabled token,
and an expired token are indistinguishable from outside — the same status, no
identity headers, and no hint of which case applied.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Authorization: Bearer ikp_<token>
```

Response:

```
HTTP/1.1 403 Forbidden
```

Status 403. The response sets no `X-User-Id` or `X-User-Email`.

Preconditions:

- The request carries `Authorization: Bearer ikp_<token>`, where `ikp_<token>`
  is one of: a value matching no stored token; a stored token that is disabled;
  or a stored token whose expiry is in the past. All three are handled the same
  way.

Postconditions:

- No token's last-used time is updated.
- auth records `check.refused` with `outcome=forbidden`, `credential=token`,
  `method=GET`, `host=dummy.sbx.ikigenba.dev`, and `path=/widgets`, under
  request id `<request-id>` and no user. It carries no `token` attribute, even
  when the secret matched a stored token, so the three cases are as
  indistinguishable in the trail as at the door.
- Nothing else has changed.

## nginx checks a request with a token whose owner has not signed in for 30 days

A token is honored only while its owner keeps signing in through Google. Once the
owner's last Google login is older than 30 days, every one of that owner's tokens
is refused with 403 until they sign in again, even though the token itself is
enabled and unexpired.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Authorization: Bearer ikp_<token>
```

Response:

```
HTTP/1.1 403 Forbidden
```

Status 403. The response sets no identity headers, exactly as for a bad token;
the two cases are indistinguishable from outside.

Preconditions:

- A token exists whose value is `ikp_<token>`: it is enabled and unexpired.
- The token's owner's most recent Google login was 31 days ago (older than 30
  days).

Postconditions:

- The token's last-used time is not updated.
- auth records `check.refused` with `outcome=forbidden`, `credential=token`,
  `method=GET`, `host=dummy.sbx.ikigenba.dev`, and `path=/widgets`, under
  request id `<request-id>` and no user, with no `token` attribute, exactly as
  for a bad token.
- Nothing else has changed.

## nginx checks a request carrying both a cookie and a token

When a request carries both a valid session cookie and a valid bearer token, the
bearer token decides who the request belongs to. The identity is the token
owner's, and the cookie's session is left alone — not touched, not counted as
use.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Cookie: ikigenba_session=<session-id>
Authorization: Bearer ikp_<token>
```

Response:

```
HTTP/1.1 200 OK
X-User-Id: <token-user-id>
X-User-Email: <token-email>
```

Status 200. The identity headers are the token owner's, `<token-user-id>` and
`<token-email>`, not the session owner's.

Preconditions:

- A session exists, named by `ikigenba_session=<session-id>`, that is live
  (within both the idle window and the cap) and belongs to the user with id
  `<session-user-id>` and email `<session-email>`.
- A token exists whose value is `ikp_<token>` and whose id is `<token-id>`:
  enabled, unexpired, with an owner (id `<token-user-id>`, email
  `<token-email>`) whose most recent Google login was within the last 30 days.

Postconditions:

- The token's last-used time is updated to now.
- The session is untouched: its last-use time is unchanged, and the request does
  not count as use of it.
- auth records `check.allowed` with `outcome=allowed`, `credential=token`,
  `method=GET`, `host=dummy.sbx.ikigenba.dev`, `path=/widgets`, and
  `token=<token-id>`, under request id `<request-id>` and user
  `<token-user-id>`: the bearer token decides the credential as it decides the
  identity.

## nginx checks a request without naming the original request

A subrequest that does not carry `X-Original-Method`, `X-Original-Host`, or
`X-Original-URI` — a developer calling `/check` by hand, say — is decided
exactly as it would be with them. The headers only describe the request in the
trail; a missing one is recorded as the empty string.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
Cookie: ikigenba_session=<session-id>
```

Response:

```
HTTP/1.1 200 OK
X-User-Id: <user-id>
X-User-Email: <email>
```

Status 200, exactly as in `nginx checks a request with a live session`.

Preconditions:

- A session exists, named by `ikigenba_session=<session-id>`, that is live
  (within both the idle window and the cap) and belongs to the user with id
  `<user-id>` and email `<email>`.
- The request carries none of the three `X-Original-` headers.

Postconditions:

- The session's last-use time is updated to now.
- auth records `check.allowed` with `outcome=allowed`, `credential=session`,
  `method=""`, `host=""`, and `path=""`, under request id `<request-id>` and
  user `<user-id>`.
- Nothing else has changed.

## An agent asks who it is

An agent holding a token asks auth directly for the identity that token carries,
without going through a routed app. `/me` reports it and changes nothing.

Request:

```
GET /me HTTP/1.1
Authorization: Bearer ikp_<token>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is the JSON object `{"id":"<user-id>","email":"<email>"}`,
where `<user-id>` is the same opaque id `/check` would return as `X-User-Id` for
this token and `<email>` is the owner's email.

Preconditions:

- A token exists whose value is `ikp_<token>`: enabled, unexpired, with an owner
  (id `<user-id>`, email `<email>`) whose most recent Google login was within the
  last 30 days.

Postconditions:

- Nothing has changed. `/me` does not touch the token's last-used time.
- auth records no check event; the request is in the trail only through its
  request events (`S2-serve.md`).

## A user asks who they are

A signed-in user's browser (or any client carrying the session cookie) asks the
same question and gets the same shape of answer.

Request:

```
GET /me HTTP/1.1
Cookie: ikigenba_session=<session-id>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is the JSON object `{"id":"<user-id>","email":"<email>"}`,
the same fields as for a token, filled with the session owner's id and email.

Preconditions:

- A session exists, named by `ikigenba_session=<session-id>`, that is live
  (within both the idle window and the cap) and belongs to the user with id
  `<user-id>` and email `<email>`.

Postconditions:

- Nothing has changed. `/me` does not count as use, so the session's last-use
  time is not updated.
- auth records no check event; the request is in the trail only through its
  request events (`S2-serve.md`).

## An agent asks who it is with a bad token

`/me` refuses a request it cannot identify. With no credential it answers 401;
with a bearer token it cannot honor it answers 403. Either way the body is one
line of plain text saying why, mirroring how the service answers a missing page
— never a JSON error envelope.

Request:

```
GET /me HTTP/1.1
```

```
GET /me HTTP/1.1
Authorization: Bearer ikp_<token>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain; charset=utf-8
```

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 401 for the first form: the body is one line of plain text saying no
credential was given. Status 403 for the second: the body is one line of plain
text saying the token was refused. Neither body is JSON.

Preconditions:

- For the 401 form, the request carries no `ikigenba_session` cookie and no
  `Authorization` header.
- For the 403 form, the request carries `Authorization: Bearer ikp_<token>`
  where the token is unknown, disabled, expired, or its owner's Google login is
  older than 30 days — all indistinguishable from outside.

Postconditions:

- Nothing has changed.
- auth records no check event; the request is in the trail only through its
  request events (`S2-serve.md`), whose `request.finished` carries the 401 or
  403.

## nginx checks a request while auth cannot use its database

auth decides every request from its database, so when the database fails the
read or write a request needs, auth cannot decide it and answers 500: the
fault is auth's, not the caller's. nginx treats any answer from `/check` other
than 200, 401, and 403 as its own failure, so the visitor sees an error and
the app is never reached. The failure is a handled one, so it is in the trail,
not on stderr: auth records `check.failed`, so the method, host, and path of a
request that never reached an app are still on record, and the request's
`request.finished` carries the 500 (`S2-serve.md`). `/me`, and every route of
`S3-sign-in.md` and `S5-tokens.md`, answers a database failure with the same
500 and writes nothing to stderr either; being no check, it records no check
event, and its `request.finished` carries the 500.

Request:

```
GET /check HTTP/1.1
X-Request-Id: <request-id>
X-Original-Method: GET
X-Original-Host: dummy.sbx.ikigenba.dev
X-Original-URI: /widgets?page=2
Cookie: ikigenba_session=<session-id>
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The response sets no `X-User-Id` or `X-User-Email`. The body is
one line of plain text saying the server failed.

Preconditions:

- auth is serving.
- The request carries a credential, here a session cookie.
- auth's database fails the read the request needs.

Postconditions:

- No session or token was touched.
- auth records `check.failed` with `outcome=failed`, `credential=session`,
  `method=GET`, `host=dummy.sbx.ikigenba.dev`, and `path=/widgets`, under
  request id `<request-id>` and no user.
- auth wrote nothing to stderr.
- Nothing else has changed.
