# Stories — sign-in

The browser sign-in flow: the sign-in page and profile at `/`, the start of a
Google sign-in at `/login/google`, the callback at `/login/google/callback`,
and sign-out at `/logout`. Every request here runs against auth on the loopback
address `http://127.0.0.1:3001`; the facts that depend on the space's own
hostname — the callback `redirect_uri`, the session cookie's `Domain`, and
which return URLs count as being under the space — are stated in prose, because
a real space carries them and `S7-on-a-space.md` proves the whole path there.

The session cookie is named `ikigenba_session`; it carries `Path=/`, `Secure`,
`HttpOnly`, and `SameSite=Lax`, and on a space it also carries `Domain=<space>`
(the space host and every subdomain). Its path covers every path on those hosts,
including auth's `/`, `/tokens`, and `/logout`, and other apps' paths. Run
locally the cookie has no `Domain` and is still
`Secure`, so the response blocks below show it without one. A login records the
user's last-Google-login time, which `S4-check.md` reads when it decides a
token.

Users are keyed by `(issuer, subject)` from the ID token; the email is a copy
refreshed on every login. The return URL and the CSRF protection are the same
thing: a random login state, recorded when a sign-in starts and carried through
the Google round trip, that also holds the PKCE verifier and any return URL.
Every state-changing request is a `POST`; `SameSite=Lax` is the first line of
cross-site defense and an `Origin` check is the second. Pages are described only
by their observable structure — the links they contain and their targets, the
forms they contain with their method, action, and field names — never by any
wording, label, or heading. A response block shows the status line and only the
headers the story fixes; a header it does not show is not fixed.

## A visitor asks for the sign-in page

With no live session the index is the sign-in page: an HTML page whose only way
forward is the link that starts a Google sign-in. It accepts a `?return=<url>`,
which is not stored here; it is carried to the login start when the visitor
follows the link.

Request:

```
$ curl -si http://127.0.0.1:3001/
```

```
$ curl -si 'http://127.0.0.1:3001/?return=<url>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page containing a link to `/login/google`. Both
forms return the same page; when `?return=<url>` is present the return URL is
carried to the login start, not persisted.

Preconditions:

- auth is running with `PORT=3001` and `GOOGLE_CLIENT_ID`,
  `GOOGLE_CLIENT_SECRET`, and `WORKSPACE_DOMAIN` set.
- The request carries no `ikigenba_session` cookie, or one that names no live
  session.

Postconditions:

- Nothing has changed.

## A visitor starts a Google sign-in

Following the sign-in link records a new login state and redirects the browser
to Google. The state is a random value that also carries the PKCE code verifier
and any return URL the sign-in page passed along; the same value defends the
callback against a forged request.

Request:

```
$ curl -si http://127.0.0.1:3001/login/google
```

Response:

```
HTTP/1.1 302 Found
Location: https://accounts.google.com/o/oauth2/v2/auth?...
```

Status 302. The `Location` is Google's OAuth 2.0 authorization endpoint; its
query carries the OAuth client id from `GOOGLE_CLIENT_ID`, `hd=michaelgreenly.dev`
as the Workspace hint, a `redirect_uri`, a `state` parameter, and a PKCE
`code_challenge` with `code_challenge_method=S256`. The rest of the query is not
fixed here. On a space the `redirect_uri` is
`https://auth.<space>/login/google/callback`; run locally it is
`http://localhost:3001/login/google/callback`, the callback registered on the
same OAuth client for development.

Preconditions:

- auth is running with `PORT=3001` and its Google settings.

Postconditions:

- An in-flight login state has been recorded, named by the `state` value in the
  `Location`, carrying the PKCE verifier and, if the sign-in page passed one,
  the return URL. No user and no session exist yet.

## Google does not answer at sign-in start

Starting a sign-in depends on Google: auth must reach Google before it can
redirect the browser. When Google is unreachable it cannot, so it reports that
the sign-in provider could not be reached and records no login state. This is
the start-time twin of the callback's `Google does not answer`, in the same
shape.

Request:

```
$ curl -si http://127.0.0.1:3001/login/google
```

Response:

```
HTTP/1.1 502 Bad Gateway
Content-Type: text/plain; charset=utf-8
```

Status 502. The body is one line of plain text saying the sign-in provider
could not be reached. The underlying error — the unreachable host or Google's
failure to answer — is written to auth's stderr.

Preconditions:

- auth is running with `PORT=3001` and its Google settings.
- Google is unreachable, so auth cannot reach it to start the sign-in.

Postconditions:

- No login state is recorded. No user, no session, and no cookie are created.
  Nothing has changed.

## Google returns a member for the first time

The callback matches a recorded login state, so auth exchanges the code for
tokens using the PKCE verifier and verifies the ID token: `hd` equals
`michaelgreenly.dev` and `email_verified` is true. The account has never signed
in before, so it is provisioned.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=<opaque>; Path=/; Secure; HttpOnly; SameSite=Lax
```

Status 302. Redirects to `/`. On a space the cookie also carries
`Domain=<space>` (the space host and every subdomain); run locally it has no
`Domain` and is still `Secure`.

Preconditions:

- auth is running with its Google settings.
- An in-flight login state exists named by `<state>`, and `<code>` is a valid
  Google authorization code for that login.
- The account is in the `michaelgreenly.dev` Workspace and has never signed in
  before: no user row exists for its `(issuer, subject)`.

Postconditions:

- A new user row exists, keyed by `(issuer, subject)`, with a freshly minted
  opaque `X-User-Id` and the account's email.
- A session exists server-side, named by the `ikigenba_session` cookie.
- On a space, the browser stores the cookie and sends it when following the
  redirect to `/` over HTTPS, so the user sees the profile without signing in
  again.
- The user's last-Google-login time is set.
- The in-flight login state is consumed.

## Google returns a member who has signed in before

The same flow, but a user row already exists for this `(issuer, subject)`. The
account is not provisioned again; its email is refreshed and a fresh session is
made.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=<opaque>; Path=/; Secure; HttpOnly; SameSite=Lax
```

Status 302. Redirects to `/`. The cookie carries the same attributes as in the
first-login story.

Preconditions:

- auth is running with its Google settings.
- An in-flight login state exists named by `<state>`; the account is a
  `michaelgreenly.dev` member and already has a user row for its
  `(issuer, subject)`.

Postconditions:

- No new user row is created and the `X-User-Id` is unchanged; the existing
  user's email is refreshed to the ID token's value.
- A new session exists server-side, named by the cookie.
- On a space, the browser stores the new cookie and sends it when following the
  redirect to `/` over HTTPS, so the user sees the profile.
- The user's last-Google-login time is updated.
- The in-flight login state is consumed. No duplicate user exists.

## Google returns a member with a return URL waiting

The login state carried a return URL whose host is the space or a subdomain of
it, so a successful sign-in ends at that URL instead of `/`.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 302 Found
Location: <return>
Set-Cookie: ikigenba_session=<opaque>; Path=/; Secure; HttpOnly; SameSite=Lax
```

Status 302. Redirects to the return URL `<return>` the login state carried. Its
host is the space or a subdomain of it, the space being auth's own hostname
without its leading `auth.` label; run locally the allowed hosts derive from
auth's own hostname the same way. The cookie carries the attributes described in
the first-login story.

Preconditions:

- auth is running with its Google settings.
- An in-flight login state exists named by `<state>`, carrying a return URL
  `<return>` whose host is the space or a subdomain of it.
- The account is a `michaelgreenly.dev` member.

Postconditions:

- The user is provisioned or refreshed as in the member stories, a session
  exists, and the user's last-Google-login time is updated.
- For an HTTPS return URL on a space, the browser sends the new cookie to the
  returned path. When the URL belongs to another app routed through `/check`
  as in `S7-on-a-space.md`, that app's request authenticates with the session
  without another sign-in.
- The in-flight login state is consumed.

## Google returns a member with a return URL outside the space

The login state carried a return URL whose host is neither the space nor a
subdomain of it. Such a URL is never honored; sign-in still succeeds and falls
back to `/`.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=<opaque>; Path=/; Secure; HttpOnly; SameSite=Lax
```

Status 302. The return URL's host is not the space nor a subdomain of it, so it
is discarded and the redirect falls back to `/`. The cookie is set as in the
first-login story.

Preconditions:

- auth is running with its Google settings.
- An in-flight login state exists named by `<state>`, carrying a return URL
  `<return>` whose host is neither the space nor a subdomain of it.
- The account is a `michaelgreenly.dev` member.

Postconditions:

- The user is provisioned or refreshed as in the member stories, a session
  exists, and the user's last-Google-login time is updated.
- The in-flight login state is consumed. The return URL was not used.
- On a space, the browser sends the new cookie when following the fallback
  redirect to `/` over HTTPS, so the user sees the profile.

## Google returns a callback with an unknown state

The callback's `state` matches no recorded login state — missing, unknown, or
forged — so the request is rejected before any token exchange.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8
```

Status 400. The body is one line of plain text saying the sign-in could not be
verified. A callback with no `state` at all is refused the same way.

Preconditions:

- auth is running with its Google settings.
- No in-flight login state matches `<state>`.

Postconditions:

- No user row, no session, and no cookie are created. Nothing has changed.

## A visitor cancels at Google

The visitor declined at Google, so the callback carries `error=access_denied`
instead of a code. auth returns the sign-in page again.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?error=access_denied&state=<state>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page containing a link to `/login/google` — the
sign-in page again. No `Set-Cookie` is sent.

Preconditions:

- auth is running with its Google settings.
- The callback carries `error=access_denied`. An in-flight login state named by
  `<state>` may exist from the start of the sign-in.

Postconditions:

- No user row is created or changed; no session and no cookie exist. The
  in-flight login state, if any, is consumed.

## Google returns an account outside the Workspace

The code exchange succeeds, but the ID token's `hd` is not `michaelgreenly.dev`,
or its `email_verified` is false. The account is not a Workspace member, so no
account is provisioned.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/html; charset=utf-8
```

Status 403. The body is an HTML page. No `Set-Cookie` is sent.

Preconditions:

- auth is running with its Google settings.
- An in-flight login state exists named by `<state>`; the code exchanges
  successfully, but the account's `hd` is not `michaelgreenly.dev` or its email
  is not verified.

Postconditions:

- No user row is created; no session and no cookie exist. The in-flight login
  state is consumed. Nothing else has changed.

## Google does not answer

The login state matched, but the token exchange with Google fails or Google is
unreachable, so auth cannot complete the sign-in.

Request:

```
$ curl -si 'http://127.0.0.1:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 502 Bad Gateway
Content-Type: text/plain; charset=utf-8
```

Status 502. The body is one line of plain text saying the sign-in provider
could not be reached. The underlying error — the failed token exchange or the
unreachable host — is written to auth's stderr.

Preconditions:

- auth is running with its Google settings.
- An in-flight login state exists named by `<state>`; the token exchange with
  Google fails or Google is unreachable.

Postconditions:

- No user row, no session, and no cookie are created.

## A user asks for the profile

With a live session the index is the profile, described here only by the forms
and links it contains. A `?return=<url>` is ignored because the visitor is
already signed in. On a space, a browser that has just completed sign-in sends
the received cookie automatically on this HTTPS path and sees this profile;
the session is not limited to the login callback's path.

Request:

```
$ curl -si --cookie 'ikigenba_session=<opaque>' http://127.0.0.1:3001/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page containing: a form that POSTs to
`/logout`; for each of the user's tokens, a form that POSTs to that token's
enable or disable URL (`/tokens/<id>/enable`, `/tokens/<id>/disable`) and a form
that POSTs to its delete URL (`/tokens/<id>/delete`); and a form that POSTs to
`/tokens` with fields `name` and `expires`. A `?return=<url>` on this request is
ignored.

Preconditions:

- auth is running with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session for a
  provisioned user; that user holds zero or more tokens.

Postconditions:

- Nothing has changed. Serving the profile does not touch the session; the touch
  happens at `/check` (`S4-check.md`).

## A user signs out

Sign-out changes state, so it is a `POST` from a form. Same-origin here: the
request's `Origin` is auth's own origin.

Request:

```
$ curl -si -X POST -H 'Origin: http://127.0.0.1:3001' --cookie 'ikigenba_session=<opaque>' http://127.0.0.1:3001/logout
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=; Path=/; Max-Age=0; Secure; HttpOnly; SameSite=Lax
```

Status 302. Redirects to `/`. The `Set-Cookie` clears `ikigenba_session` (empty
value, `Max-Age=0`) with the same `Path=/` and domain scope as the login cookie.
On a space the clearing cookie also carries `Domain=<space>`; run locally it
has none.

Preconditions:

- auth is running with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session, and
  its `Origin` is auth's own origin (locally `http://127.0.0.1:3001`, on a space
  `https://auth.<space>`).

Postconditions:

- The session is deleted server-side; the cookie no longer names any session.
- The browser removes the cookie for that domain and `Path=/`; it no longer
  sends that cookie to auth or the space's other apps. Following the redirect
  shows the sign-in page.
- The user row and the user's tokens are untouched.

## A user signs out from another site

A cross-site `POST` to `/logout`: its `Origin` is not auth's own origin. The
`Origin` check is the second line of defense after `SameSite=Lax`, and it
refuses the request.

Request:

```
$ curl -si -X POST -H 'Origin: https://evil.example' --cookie 'ikigenba_session=<opaque>' http://127.0.0.1:3001/logout
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text. No `Set-Cookie` is sent.

Preconditions:

- auth is running with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session, and
  its `Origin` is not auth's own origin.

Postconditions:

- The session still exists; nothing has changed.
