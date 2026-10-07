# Stories — in a sandbox

auth reached through a sandbox: the local runner a developer brings up from a
worktree, which serves every app of the checkout in plain HTTP behind its own
nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The
stories share one sandbox: `wip`, on port `7400`, holding `auth` and `dummy`,
both active, with auth at `http://auth.wip.localhost:7400` and dummy at
`http://dummy.wip.localhost:7400`. Browsers and curl resolve every name under
`localhost` to the loopback address. The sandbox gives auth the environment of
a host — its Google settings, `WORKSPACE_DOMAIN=michaelgreenly.dev`,
`DRAIN_SECONDS`, and `IKIGENBA_SERVICES` (`S02-serve.md`) — and variables a
host never sets, of which auth reads two:
`IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`, auth's own public origin,
and `IKIGENBA_CALLBACK_URL=http://localhost:7400`, the origin Google sends a
browser back to (it does not read `IKIGENBA_SANDBOX`). Google accepts a sign-in redirect to
`http://localhost:<port>` but never to a name under `localhost`, so the
sandbox's nginx answers every request to bare `http://localhost:7400`,
whatever its path, with a 302 to the same path and query at
`http://auth.wip.localhost:7400`. Everything else is auth as `S03-sign-in.md`,
`S05-tokens.md`, and `S10-mcp-clients.md` tell it, on the space auth reads
from the request's `Host`: here `wip.localhost:7400`, whose host name is
`wip.localhost`, which is also the apex the pages name. The space's MCP
gateway is auth's own origin with its `auth.` label swapped for `mcp.`, so
here it is `http://mcp.wip.localhost:7400`, and an MCP client's token is bound
to the host `mcp.wip.localhost` (`S10-mcp-clients.md`). These stories fix only
what the two variables change; on a host neither is set and every earlier group holds as written. The
sandbox's nginx and its bounce are the sandbox's own (a separate
sub-project), named here only by their observable effect, as
`S07-on-a-space.md` names the space's nginx. Every request below is a
`$ curl -si` line to the sandbox, standing in for the browser's own request;
responses pass through the sandbox's nginx over HTTP/1.1.

auth records its trail in a sandbox as on a host (`S02-serve.md`), delivering
each event to the entry named `telemetry` in the services file
`IKIGENBA_SERVICES` names. That file lists every app of the sandbox, so when
the sandbox also holds the `telemetry` app auth's events reach its trail;
a sandbox without it gives auth nowhere to deliver them, and auth writes each
to stderr as an undelivered event (`S02-serve.md`), where `sandbox logs auth`
shows it. The sandbox's routing through `/check` names the request it decides
on the subrequest as a space's does (`S04-check.md`): `X-Original-Method`, its
method; `X-Original-Host`, its host name without the port, such as
`dummy.wip.localhost`; and `X-Original-URI`, its path and query. So auth's
check events in a sandbox record `host=dummy.wip.localhost` for a request to
`http://dummy.wip.localhost:7400`.

## A visitor reaches auth in a sandbox

A visitor with no session opens auth's own name in the sandbox and gets the
sign-in page. The page names the apex and auth's host as the request's `Host`
gives them, exactly as on a space.

Request:

```
$ curl -si http://auth.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the sign-in page (`S03-sign-in.md`), drawn as a sign-in
card, whose visible text is the mark `ikigenba`, the heading
`Sign in to wip.localhost`, the sentence
`Access is limited to Google accounts in the michaelgreenly.dev workspace.`,
the link `Continue with Google` whose target is `/login/google`, and the
card's `<footer>` reading
`You're signing in at auth.wip.localhost:7400. One sign-in covers every service in this space.`

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400` and
  `IKIGENBA_CALLBACK_URL=http://localhost:7400`.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- Nothing has changed.

## A visitor starts a Google sign-in in a sandbox

Following the sign-in link records a login state and sends the browser to
Google, as on a space, but the `redirect_uri` auth asks Google to return to is
the callback path at `IKIGENBA_CALLBACK_URL`, not at the request's `Host`:
`http://localhost:7400/login/google/callback`, the one address in the sandbox
Google will accept.

Request:

```
$ curl -si http://auth.wip.localhost:7400/login/google
```

Response:

```
HTTP/1.1 302 Found
Location: https://accounts.google.com/o/oauth2/v2/auth?...
```

Status 302. The `Location` is Google's authorization endpoint with the query
`S03-sign-in.md` fixes — the client id from `GOOGLE_CLIENT_ID`,
`hd=michaelgreenly.dev`, a `state`, and a PKCE `code_challenge` with
`code_challenge_method=S256` — except that its `redirect_uri` is
`http://localhost:7400/login/google/callback`, `IKIGENBA_CALLBACK_URL`
followed by `/login/google/callback`. It is the same whatever `Host` the
request names.

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_CALLBACK_URL=http://localhost:7400`.

Postconditions:

- An in-flight login state has been recorded, named by the `state` value in
  the `Location`, as in `S03-sign-in.md`. No user and no session exist yet.

## Google sends a visitor in a sandbox back through the bare localhost address

Google ends the sign-in by sending the browser to the `redirect_uri` auth gave
it. That address is bare `localhost`, which no app answers, so the sandbox's
nginx sends the browser on to the same path and query at auth's own name.
auth does not see this request.

Request:

```
$ curl -si 'http://localhost:7400/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/login/google/callback?code=<code>&state=<state>
```

Status 302. The path and query are carried unchanged to auth's own name. The
body is not fixed.

Preconditions:

- The sandbox above is up.
- The visitor started the sign-in as in `A visitor starts a Google sign-in in a
  sandbox`, and Google answered with `<code>` for the login state `<state>`.

Postconditions:

- Nothing has changed. auth received nothing; the browser's next request goes
  to auth.

## Google returns a member to auth in a sandbox

The bounced callback reaches auth at its own name. auth matches the login
state and exchanges the code with Google naming the same `redirect_uri` it
sent at the start, `http://localhost:7400/login/google/callback`, not one made
from this request's `Host`; Google honors the code only for the address it
was issued to. The member is provisioned or refreshed as in `S03-sign-in.md`
and gets a session.

Request:

```
$ curl -si 'http://auth.wip.localhost:7400/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=<opaque>; Domain=wip.localhost; Path=/; Secure; HttpOnly; SameSite=Lax
```

Status 302. Redirects to auth's own `/`. The cookie has the attributes every
session cookie has (`S03-sign-in.md`); its `Domain` is the space with its port
removed, `wip.localhost`, so it reaches auth, dummy, and every other name under
`wip.localhost`. It is `Secure` here as on a host: browsers treat a page at
`http://<name>.localhost` as a secure context, so they keep a `Secure` cookie
the sandbox sets over plain HTTP and send it back to these names.

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400` and
  `IKIGENBA_CALLBACK_URL=http://localhost:7400`.
- The Google OAuth client named by `GOOGLE_CLIENT_ID` accepts
  `http://localhost:7400/login/google/callback` as a redirect URI, as a
  Desktop client does for every `http://localhost` port.
- An in-flight login state exists named by `<state>`, carrying no return URL,
  and `<code>` is the code Google issued for it after the bounce in
  `Google sends a visitor in a sandbox back through the bare localhost address`.
- The account is a `michaelgreenly.dev` member.

Postconditions:

- The user is provisioned or refreshed as in `S03-sign-in.md`, a session exists
  server-side, named by the cookie, and the user's last-Google-login time is
  set.
- The in-flight login state is consumed.
- A browser stores the cookie and sends it when following the redirect to
  `http://auth.wip.localhost:7400/`, so the user sees the profile, and with
  every later request to `http://dummy.wip.localhost:7400`, which the
  sandbox's routing through `/check` lets in without another sign-in.

## Google returns a member in a sandbox to the app they were after

A visitor sent to sign in from dummy carries dummy's URL as the return URL
(the sandbox sends the browser to
`http://auth.wip.localhost:7400/?return=http://dummy.wip.localhost:7400/widgets?page=2`).
An `http` URL with a port is in-space when its host is under the space's host
name, so a successful sign-in ends back at dummy.

Request:

```
$ curl -si 'http://auth.wip.localhost:7400/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 302 Found
Location: http://dummy.wip.localhost:7400/widgets?page=2
Set-Cookie: ikigenba_session=<opaque>; Domain=wip.localhost; Path=/; Secure; HttpOnly; SameSite=Lax
```

Status 302. Redirects to the return URL the login state carried. The cookie
is the one of `Google returns a member to auth in a sandbox`.

Preconditions:

- As in `Google returns a member to auth in a sandbox`, except that the login
  state named by `<state>` carries the return URL
  `http://dummy.wip.localhost:7400/widgets?page=2`.

Postconditions:

- The user is provisioned or refreshed, a session exists, and the user's
  last-Google-login time is updated. The in-flight login state is consumed.
- A browser following the redirect sends the new cookie to dummy, whose
  request authenticates with the session without another sign-in.

## A user signs out from auth in a sandbox

Sign-out from auth's own profile in a sandbox: the `POST`'s `Origin` is auth's
own origin, `http://auth.wip.localhost:7400`, scheme and port included.

Request:

```
$ curl -si -X POST -H 'Origin: http://auth.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' http://auth.wip.localhost:7400/logout
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=; Domain=wip.localhost; Path=/; Max-Age=0; Secure; HttpOnly; SameSite=Lax
```

Status 302. Redirects to auth's own `/`. The `Set-Cookie` clears
`ikigenba_session` with the same `Domain=wip.localhost` and `Path=/` as the
login cookie.

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`.
- The request carries an `ikigenba_session` cookie naming a live session.

Postconditions:

- The session is deleted server-side; the cookie no longer names any session.
- A browser removes the cookie and no longer sends it to auth or dummy;
  following the redirect shows the sign-in page.
- The user row and the user's tokens are untouched.

## A user signs out from dummy's banner in a sandbox

dummy's banner carries the same sign-out button every app's does, a form that
POSTs to auth's `/logout`. The `Origin` is dummy's own,
`http://dummy.wip.localhost:7400`. In a sandbox an origin is on this space
when it has the scheme and the port of `IKIGENBA_PUBLIC_URL` — `http` and
`7400` — and its host is the space's host name, `wip.localhost`, or any name
under it, never one with an empty label or a trailing `.`. One click signs the
user out of the whole sandbox.

Request:

```
$ curl -si -X POST -H 'Origin: http://dummy.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' http://auth.wip.localhost:7400/logout
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=; Domain=wip.localhost; Path=/; Max-Age=0; Secure; HttpOnly; SameSite=Lax
```

Status 302. The response is the same as for a sign-out from auth's own origin:
`Location: /` is auth's own `/`, so the browser lands on auth's sign-in page.
The request is answered the same with an `Origin` of
`http://wip.localhost:7400` or `http://a.b.wip.localhost:7400`.

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`.
- The request carries an `ikigenba_session` cookie naming a live session.

Postconditions:

- The session is deleted server-side; the cookie no longer names any session.
- A browser removes the cookie and no longer sends it to auth, dummy, or any
  other name under `wip.localhost`.
- The user row and the user's tokens are untouched.

## A sign-out in a sandbox from an origin with the wrong scheme or port is refused

An `Origin` naming the sandbox's hosts is on this space only with the scheme
and port of `IKIGENBA_PUBLIC_URL`. One that differs in either is refused as a
cross-site `POST` is (`S03-sign-in.md`): `https://dummy.wip.localhost:7400`
(the scheme a host would use), `http://dummy.wip.localhost:7401` (another
port, such as another sandbox's), and `http://dummy.wip.localhost` (no port).
So is an origin whose host is not under `wip.localhost`, such as
`http://localhost:7400`.

Request:

```
$ curl -si -X POST -H 'Origin: https://dummy.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' http://auth.wip.localhost:7400/logout
```

```
$ curl -si -X POST -H 'Origin: http://dummy.wip.localhost:7401' --cookie 'ikigenba_session=<opaque>' http://auth.wip.localhost:7400/logout
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text. No `Set-Cookie` is sent.

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`.
- The request carries an `ikigenba_session` cookie naming a live session.

Postconditions:

- The session still exists; nothing has changed.

## A user creates a token in a sandbox

A token is created from auth's profile in the sandbox, as a human does to hand
one to an agent working in the worktree. The token actions accept only auth's
own origin, which in a sandbox is `IKIGENBA_PUBLIC_URL`,
`http://auth.wip.localhost:7400`. Everything else about creating a token is
`S05-tokens.md`'s.

Request:

```
$ curl -si -X POST -H 'Origin: http://auth.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' --data 'name=wip-agent&expires=30d' http://auth.wip.localhost:7400/tokens
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the `Token created` page of `S05-tokens.md`, holding
the new token's plaintext secret, `ikp_` then 52 Crockford base32 characters,
exactly once.

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`.
- The request carries an `ikigenba_session` cookie naming a live session.

Postconditions:

- One token record named `wip-agent`, enabled and expiring in 30 days, now
  belongs to the user; only a hash of its secret is stored. The token
  authenticates requests to dummy through the sandbox's routing through
  `/check` (`S04-check.md`).

## A user manages a token in a sandbox

Disabling, enabling, and deleting a token from the profile in a sandbox take
the same own origin, `http://auth.wip.localhost:7400`, and otherwise behave as
`S05-tokens.md` tells.

Request:

```
$ curl -si -X POST -H 'Origin: http://auth.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' http://auth.wip.localhost:7400/tokens/<token-id>/disable
```

```
$ curl -si -X POST -H 'Origin: http://auth.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' http://auth.wip.localhost:7400/tokens/<token-id>/enable
```

```
$ curl -si -X POST -H 'Origin: http://auth.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' http://auth.wip.localhost:7400/tokens/<token-id>/delete
```

Response (each):

```
HTTP/1.1 302 Found
Location: /
```

Status 302. Each redirects to `/` (the profile).

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`.
- The request carries an `ikigenba_session` cookie naming a live session.
- `<token-id>` is the id of a token the user owns: `tok_` followed by 26
  Crockford base32 characters (`S05-tokens.md`), never its secret.

Postconditions:

- After the disable request the token is disabled, after the enable request it
  is enabled again, and after the delete request it is gone, as in
  `S05-tokens.md`.

## A token request in a sandbox from another origin is refused

In a sandbox auth's own origin is exactly `IKIGENBA_PUBLIC_URL`, so a token
`POST` from any other origin is refused as `S05-tokens.md`'s cross-site one is:
the origin a host would use, `https://auth.wip.localhost:7400`; another app in
the same sandbox, `http://dummy.wip.localhost:7400`; and auth's name on
another port, `http://auth.wip.localhost:7401`. Every token action URL
(`/tokens/<token-id>/enable`, `/disable`, `/delete`) is refused the same way.

Request:

```
$ curl -si -X POST -H 'Origin: https://auth.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' --data 'name=wip-agent&expires=30d' http://auth.wip.localhost:7400/tokens
```

```
$ curl -si -X POST -H 'Origin: http://dummy.wip.localhost:7400' --cookie 'ikigenba_session=<opaque>' --data 'name=wip-agent&expires=30d' http://auth.wip.localhost:7400/tokens
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text.

Preconditions:

- The sandbox above is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`.
- The request carries an `ikigenba_session` cookie naming a live session.

Postconditions:

- Nothing has changed. No token was created.
