# Stories — check

The two endpoints auth serves on its loopback port for deciding who a request
belongs to. `GET /check` is the subrequest endpoint nginx calls for every
routed app: nginx forwards the original request's `Cookie` and `Authorization`
headers with no body, and acts on the status auth returns — 200 means copy the
identity headers onto the upstream request, 401 means redirect the browser to
sign in, 403 means pass the refusal through. `GET /me` is the public "who am I"
endpoint an agent or a signed-in user can call directly. Every story here is a
`curl` request against `http://127.0.0.1:3001`, the loopback address auth
listens on.

A credential reaches these endpoints one of two ways: the session cookie
`ikigenba_session=<session-id>`, or `Authorization: Bearer ikp_<token>`. Both
name the same kind of user; apps cannot tell a cookie login from a token login.
On success the identity is two values: `X-User-Id`, an opaque id auth minted for
the user (16 random bytes in Crockford base32, 26 characters — never Google's
subject, never the email; written here as `<user-id>`), and `X-User-Email`, the
user's email. A session ends at the earlier of 18 hours after login and 15
minutes after its last use, and every request through `/check` counts as use. A
token is honored only while its owner has logged in through Google within the
last 30 days. The last-use time of a session and the last-used time of a token
are updated in `/check` and nowhere else; `/me` never mutates anything.

`/check` is only meant to be called by nginx as a loopback subrequest; it is not
reachable from the public side of a space, and that unreachability (a request to
`https://<space>/check` gets 404) is proven in `S7-on-a-space.md`, not here.

## nginx checks a request with a live session

nginx makes this subrequest for every routed request that carries the session
cookie. auth answers 200 and hands back the identity headers, and it counts the
request as use of the session.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session-id>' http://127.0.0.1:3001/check
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
- Nothing else has changed.

## nginx checks a request with no credential

Request:

```
$ curl -si http://127.0.0.1:3001/check
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

- Nothing has changed.

## nginx checks a request with a session idle too long

A session that has not been used within the last 15 minutes is expired, even
though the 18-hour cap has not been reached.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session-id>' http://127.0.0.1:3001/check
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

## nginx checks a request with a session past its cap

A session reaches its 18-hour cap counting from login, no matter how recently it
was used. Recent use cannot extend it past the cap.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session-id>' http://127.0.0.1:3001/check
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

## nginx checks a request with a token

A request may carry a personal access token instead of a cookie. auth answers
200 with the token owner's identity and records the token's use.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://127.0.0.1:3001/check
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

- A token exists whose value is `ikp_<token>`: it is enabled, and it is either
  unexpired or has no expiry.
- The token's owner has id `<user-id>` and email `<email>`, and that owner's
  most recent Google login was 3 days ago (within the last 30 days).
- The request carries no `ikigenba_session` cookie.

Postconditions:

- The token's last-used time is updated to now.
- Nothing else has changed.

## nginx checks a request with a token that is unknown, disabled, or expired

A bearer token that cannot be honored yields 403, and nginx passes the 403
through rather than redirecting to sign in. An unknown token, a disabled token,
and an expired token are indistinguishable from outside — the same status, no
identity headers, and no hint of which case applied.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://127.0.0.1:3001/check
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

- Nothing has changed. No token's last-used time is updated.

## nginx checks a request with a token whose owner has not signed in for 30 days

A token is honored only while its owner keeps signing in through Google. Once the
owner's last Google login is older than 30 days, every one of that owner's tokens
is refused with 403 until they sign in again, even though the token itself is
enabled and unexpired.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://127.0.0.1:3001/check
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

- Nothing has changed. The token's last-used time is not updated.

## nginx checks a request carrying both a cookie and a token

When a request carries both a valid session cookie and a valid bearer token, the
bearer token decides who the request belongs to. The identity is the token
owner's, and the cookie's session is left alone — not touched, not counted as
use.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session-id>' -H 'Authorization: Bearer ikp_<token>' http://127.0.0.1:3001/check
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
- A token exists whose value is `ikp_<token>`: enabled, unexpired, with an owner
  (id `<token-user-id>`, email `<token-email>`) whose most recent Google login
  was within the last 30 days.

Postconditions:

- The token's last-used time is updated to now.
- The session is untouched: its last-use time is unchanged, and the request does
  not count as use of it.

## An agent asks who it is

An agent holding a token asks auth directly for the identity that token carries,
without going through a routed app. `/me` reports it and changes nothing.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://127.0.0.1:3001/me
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

## A user asks who they are

A signed-in user's browser (or any client carrying the session cookie) asks the
same question and gets the same shape of answer.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session-id>' http://127.0.0.1:3001/me
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

## An agent asks who it is with a bad token

`/me` refuses a request it cannot identify. With no credential it answers 401;
with a bearer token it cannot honor it answers 403. Either way the body is one
line of plain text saying why, mirroring how the service answers a missing page
— never a JSON error envelope.

Request:

```
$ curl -si http://127.0.0.1:3001/me
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://127.0.0.1:3001/me
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
