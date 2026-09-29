# Stories — sign-in

The browser sign-in flow: the sign-in page and profile at `/`, the start of a
Google sign-in at `/login/google`, the callback at `/login/google/callback`,
and sign-out at `/logout`. Every request here runs against auth a developer
serves with `systemd-socket-activate -l 127.0.0.1:3001 auth` (`S2-serve.md`),
at `http://localhost:3001`; on a space nginx proxies auth's hostname to auth's
socket instead. A request whose `Host` is `localhost:3001` is a local one and
takes the fixed development forms stated below; every other request is on a
space. The facts that depend on the space's own
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
cross-site defense and an `Origin` check is the second. For `/logout` that check
is that the origin is on this space, so any app on the space can sign its user
out: on a space the accepted origins are exactly the hosts the session cookie
reaches, over `https` — `https://<space>` and `https://<host>.<space>` for a
subdomain at any depth (any host ending in `.<space>`), auth's own
`https://auth.<space>` among them. Run locally, where
the cookie has no `Domain` and every app on `localhost` shares it whatever its
port, the accepted origins are `http://localhost` and `http://localhost:<port>`
for any port, auth's own `http://localhost:3001` among them.

A page fixes its visible text and the markup the stylesheet keys on: an
element or class is quoted where the stylesheet hooks in, the visible text is
stated as fact in the story's status line, and no body is quoted whole. Every
HTML page auth serves is titled `auth`, links `/assets/theme.css` as its
stylesheet, `<link rel="stylesheet" href="/assets/theme.css">`, and declares
the phone-width viewport,
`<meta name="viewport" content="width=device-width, initial-scale=1">`. The
stylesheet and the fonts it loads are auth's own, served under `/assets/`; a
page makes no request to any other host. An icon is a Tabler outline icon
drawn inline before a button's text as `<svg class="ico" aria-hidden="true">`,
so the button's accessible text is its word alone. The workspace a page names
is `WORKSPACE_DOMAIN`, here `michaelgreenly.dev`. A page names the apex, which
auth reads from the request's own `Host`: a trailing port is dropped, and the
apex is the last two dot-separated labels of what remains —
`auth.sbx.ikigenba.dev` gives `ikigenba.dev`, and the local `localhost:3001`
gives `localhost`.

auth draws its pages in one of two frames. A page for a visitor who is not
signed in is a sign-in card: it has no banner, and its `<body>` holds
`<main class="auth-page">`, which holds one `<section class="card">`; the card
begins with the bare mark, `<span class="mark">ikigenba</span>`, and its
heading is `<h1>` reading `Sign in to <apex>`. Its way forward is a link styled
as a button, `<a class="button secondary large google">`, that starts a Google
sign-in. A page for a signed-in user — the profile here, and the token-created
page and the rejected-create page (`S5-tokens.md`) — is drawn with the
banner: the `<header>` at the top of its `<body>`, holding the mark
`<a class="mark" data-service="auth" href="/">ikigenba</a>`, which the
stylesheet shows as `ikigenba │ auth`, then the user's email address, then the
sign-out form. On auth's own pages the email is plain text, not a link: the
profile it would lead to is auth's `/`, where the mark already goes. The
banner is followed by one `<main>` element holding everything else on the
page. The sign-out form is

```
<form class="inline" method="post" action="/logout"><button class="secondary small" type="submit"><svg class="ico" aria-hidden="true" …>…</svg>Sign out</button></form>
```

with the `logout` icon before the text. auth's failures that are not pages —
the 400, the 502s, and the 403 sign-out refusals below — stay one line of
plain text in neither frame, because the visitor may not be signed in. A
response block shows the status line and only the headers the story fixes; a
header it does not show is not fixed.

## A visitor asks for the sign-in page

With no live session the index is the sign-in page: a sign-in card whose only
way forward is the link that starts a Google sign-in. It tells the visitor
which space they are signing in to and that one sign-in covers every service
there.

Request:

```
$ curl -si http://localhost:3001/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `auth`, with the stylesheet
link and viewport every page has, drawn as a sign-in card. Its visible text
is the mark `ikigenba`, the heading `Sign in to localhost`, the sentence
`Access is limited to Google accounts in the michaelgreenly.dev workspace.`,
the link `Continue with Google` whose target is `/login/google`, and the
card's `<footer>` reading
`You're signing in at localhost:3001. One sign-in covers every service in this space.`
The footer names the `Host` as sent, port included.

Preconditions:

- auth is serving on `127.0.0.1:3001` with `GOOGLE_CLIENT_ID`,
  `GOOGLE_CLIENT_SECRET`, and `WORKSPACE_DOMAIN=michaelgreenly.dev` set.
- The request carries no `ikigenba_session` cookie, or one that names no live
  session.

Postconditions:

- Nothing has changed.

## A visitor on a space asks for the sign-in page

On a space the page names the space's apex and auth's own host, both read
from the request's `Host`, never fixed. A developer shows the space's `Host`
by hand.

Request:

```
$ curl -si -H 'Host: auth.sbx.ikigenba.dev' http://localhost:3001/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the sign-in page of the story above, with the heading
`Sign in to ikigenba.dev` and the footer reading
`You're signing in at auth.sbx.ikigenba.dev. One sign-in covers every service in this space.`
The workspace sentence and the `Continue with Google` link to `/login/google`
are unchanged.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- The request carries no `ikigenba_session` cookie, or one that names no live
  session.

Postconditions:

- Nothing has changed.

## A visitor arrives at the sign-in page with a return URL

An app on the space sends a visitor who is not signed in to auth with
`?return=<url>`, the page they were after. The sign-in page names where the
visitor is headed, and carries the return URL to the login start in its link;
it does not store it.

Request:

```
$ curl -si -H 'Host: auth.sbx.ikigenba.dev' 'http://localhost:3001/?return=https%3A%2F%2Fdummy.sbx.ikigenba.dev%2Fwidgets'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `auth`, with the stylesheet
link and viewport every page has, drawn as a sign-in card. Its visible text
is the mark `ikigenba`, the heading `Sign in to ikigenba.dev`, the sentence
`Sign in to continue to dummy.sbx.ikigenba.dev.` naming the return URL's
host exactly as the URL writes it, a port included, the link `Continue with Google` whose target is
`/login/google?return=https%3A%2F%2Fdummy.sbx.ikigenba.dev%2Fwidgets` — the
return URL, URL-encoded — and the card's `<footer>` reading
`Access is limited to Google accounts in the michaelgreenly.dev workspace.`
Run locally, a request to `http://localhost:3001/` with
`?return=http%3A%2F%2Flocalhost%3A3000%2Fwidgets` is the same page with the
heading `Sign in to localhost` and the sentence
`Sign in to continue to localhost:3000.`

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- The request carries no `ikigenba_session` cookie, or one that names no live
  session.
- The return URL's host is the space or a subdomain of it, by the rule the
  callback uses to honor a return URL.

Postconditions:

- Nothing has changed. The return URL is not persisted; it travels in the
  link to the login start.

## A visitor arrives at the sign-in page with a return URL outside the space

A return URL whose host is neither the space nor a subdomain of it — by the
rule the callback uses to honor a return URL — or that cannot be parsed as a
URL, is never named on the page: the visitor sees the plain sign-in card. The
link still carries the return URL to the login start as written, where the
callback later discards it (`Google returns a member with a return URL
outside the space`).

Request:

```
$ curl -si -H 'Host: auth.sbx.ikigenba.dev' 'http://localhost:3001/?return=https%3A%2F%2Fevil.example%2F'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the sign-in page of `A visitor on a space asks for
the sign-in page` — the heading `Sign in to ikigenba.dev`, the sentence
`Access is limited to Google accounts in the michaelgreenly.dev workspace.`,
and the footer reading
`You're signing in at auth.sbx.ikigenba.dev. One sign-in covers every service in this space.`
— except that the link `Continue with Google` has the target
`/login/google?return=https%3A%2F%2Fevil.example%2F`. No sentence beginning
`Sign in to continue to` appears. A `?return=` that is not a parseable URL
gives the same page, its value carried in the link the same way.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- The request carries no `ikigenba_session` cookie, or one that names no live
  session.

Postconditions:

- Nothing has changed. The return URL is not persisted.

## A visitor starts a Google sign-in

Following the sign-in link records a new login state and redirects the browser
to Google. The state is a random value that also carries the PKCE code verifier
and any return URL the sign-in page passed along; the same value defends the
callback against a forged request.

Request:

```
$ curl -si http://localhost:3001/login/google
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

- auth is serving on `127.0.0.1:3001` with its Google settings.

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
$ curl -si http://localhost:3001/login/google
```

Response:

```
HTTP/1.1 502 Bad Gateway
Content-Type: text/plain; charset=utf-8
```

Status 502. The body is one line of plain text saying the sign-in provider
could not be reached. auth writes one line to stderr,
`auth: request <id>: <reason>`, where `<reason>` is the underlying error — the
unreachable host or Google's failure to answer — and `<id>` is the request's
`X-Request-Id`, or `-` when it carries none, as here (`S2-serve.md`).

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
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
$ curl -si 'http://localhost:3001/login/google/callback?code=<code>&state=<state>'
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

- auth is serving on `127.0.0.1:3001` with its Google settings.
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
$ curl -si 'http://localhost:3001/login/google/callback?code=<code>&state=<state>'
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

- auth is serving on `127.0.0.1:3001` with its Google settings.
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
$ curl -si 'http://localhost:3001/login/google/callback?code=<code>&state=<state>'
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

- auth is serving on `127.0.0.1:3001` with its Google settings.
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
$ curl -si 'http://localhost:3001/login/google/callback?code=<code>&state=<state>'
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

- auth is serving on `127.0.0.1:3001` with its Google settings.
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
$ curl -si 'http://localhost:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8
```

Status 400. The body is one line of plain text saying the sign-in could not be
verified. A callback with no `state` at all is refused the same way.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
- No in-flight login state matches `<state>`.

Postconditions:

- No user row, no session, and no cookie are created. Nothing has changed.

## A visitor cancels at Google

The visitor declined at Google, so the callback carries `error=access_denied`
instead of a code. auth answers with the sign-in card again, saying the
sign-in was cancelled and offering to start over.

Request:

```
$ curl -si 'http://localhost:3001/login/google/callback?error=access_denied&state=<state>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `auth`, with the stylesheet
link and viewport every page has, drawn as a sign-in card. Its visible text
is the mark `ikigenba`, the heading `Sign in to localhost`, a warning,
`<div class="alert" data-kind="warn" role="status">`, titled
`Sign-in cancelled` and reading
`Google didn't grant access, so you weren't signed in. You can try again.`,
and the link `Continue with Google` whose target is `/login/google`. No
`Set-Cookie` is sent.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
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
$ curl -si 'http://localhost:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/html; charset=utf-8
```

Status 403. The body is an HTML document titled `auth`, with the stylesheet
link and viewport every page has, drawn as a sign-in card. Its visible text
is the mark `ikigenba`, the heading `Sign in to localhost`, an error,
`<div class="alert" data-kind="err" role="alert">`, titled
`Workspace membership required` and reading
`ada@example.com isn't a verified account in the michaelgreenly.dev workspace. Sign in with your @michaelgreenly.dev account instead.`
— the email is the one the ID token carried — the link `Try another account`
whose target is `/login/google`, and the card's `<footer>` reading
`Think you should have access? Ask your michaelgreenly.dev workspace admin to add you.`
No `Set-Cookie` is sent.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
- An in-flight login state exists named by `<state>`; the code exchanges
  successfully, but the account's `hd` is not `michaelgreenly.dev` or its email
  is not verified. The ID token's email is `ada@example.com`.

Postconditions:

- No user row is created; no session and no cookie exist. The in-flight login
  state is consumed. Nothing else has changed.

## Google does not answer

The login state matched, but the token exchange with Google fails or Google is
unreachable, so auth cannot complete the sign-in.

Request:

```
$ curl -si 'http://localhost:3001/login/google/callback?code=<code>&state=<state>'
```

Response:

```
HTTP/1.1 502 Bad Gateway
Content-Type: text/plain; charset=utf-8
```

Status 502. The body is one line of plain text saying the sign-in provider
could not be reached. auth writes one line to stderr,
`auth: request <id>: <reason>`, where `<reason>` is the underlying error — the
failed token exchange or the unreachable host — and `<id>` is the request's
`X-Request-Id`, or `-` when it carries none, as here (`S2-serve.md`).

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
- An in-flight login state exists named by `<state>`; the token exchange with
  Google fails or Google is unreachable.

Postconditions:

- No user row, no session, and no cookie are created.

## A user asks for the profile

With a live session the index is the profile, drawn with the banner:
who the user is, the tokens they hold, and the form that creates another. A
`?return=<url>` is ignored because the visitor is already signed in. On a
space, a browser that has just completed sign-in sends the received cookie
automatically on this HTTPS path and sees this profile; the session is not
limited to the login callback's path.

The page's heading is `<h1>` reading `Your account`, with the subtitle
`You're signed in to <apex>.` beneath it. Three cards follow, each a
`<section class="card">` whose heading is an `<h2>` in the card's `<header>`.
The `Account` card holds `<dl class="kv">` pairing `Email` with the user's
email, `Workspace` with `WORKSPACE_DOMAIN`, and `Signed in via` with `Google`.
The `API tokens` card is `<section class="card flush">`; its header also
reads `Personal access tokens let scripts and tools act as you. Send one as a bearer token.`
It holds the user's tokens as a table inside `<div class="table-scroll">`,
whose header cells read `Name`, `Created`, `Last used`, `Expires`, and
`Status`, then one empty cell over the row actions. What each row shows, the
order of the rows, and what the card holds instead of the table when the user
has no tokens are `S5-tokens.md`'s. No token's secret appears anywhere on the
profile. The third card is the `Create a token` card that `S5-tokens.md`
defines, whose form POSTs to `/tokens` with fields `name` and `expires`.

Request:

```
$ curl -si --cookie 'ikigenba_session=<opaque>' http://localhost:3001/
```

```
$ curl -si --cookie 'ikigenba_session=<opaque>' 'http://localhost:3001/?return=<url>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `auth`, with the stylesheet
link and viewport every page has, drawn with the banner: the mark
naming the service `auth`, the email `ada@michaelgreenly.dev`, and the
`Sign out` button in the form that POSTs to `/logout`. Inside the page's one
`<main>` its visible text is the heading `Your account` with the subtitle
`You're signed in to localhost.`; the `Account` card reading `Email`
`ada@michaelgreenly.dev`, `Workspace` `michaelgreenly.dev`, and
`Signed in via` `Google`; the `API tokens` card with its explanation and the
user's tokens (`S5-tokens.md`); and the `Create a token` card, whose form
POSTs to `/tokens` with fields `name` and `expires` (`S5-tokens.md`).
Both forms return the same page; the `?return=<url>` is ignored.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- The request carries an `ikigenba_session` cookie naming a live session for a
  provisioned user whose email is `ada@michaelgreenly.dev`; that user holds
  zero or more tokens.

Postconditions:

- Nothing has changed. Serving the profile does not touch the session; the touch
  happens at `/check` (`S4-check.md`).

## A user signs out

Sign-out changes state, so it is a `POST` from a form. Same-origin here: the
request's `Origin` is auth's own origin.

Request:

```
$ curl -si -X POST -H 'Origin: http://localhost:3001' --cookie 'ikigenba_session=<opaque>' http://localhost:3001/logout
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

- auth is serving on `127.0.0.1:3001` with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session, and
  its `Origin` is auth's own origin (locally `http://localhost:3001`, on a space
  `https://auth.<space>`).

Postconditions:

- The session is deleted server-side; the cookie no longer names any session.
- The browser removes the cookie for that domain and `Path=/`; it no longer
  sends that cookie to auth or the space's other apps. Following the redirect
  shows the sign-in page.
- The user row and the user's tokens are untouched.

## A user signs out from an app on the space

An app on the space offers sign-out in its own banner — dummy's, say, at
`https://dummy.<space>`, or `http://localhost:3000` run locally — as a form
that POSTs to auth's `/logout`. The app is same-site with auth, so the browser
sends the `SameSite=Lax` session cookie with the `POST`, and its `Origin` is the
app's own, which is on this space. One click signs the user out of the space,
exactly as signing out from auth's own profile does.

Request:

```
$ curl -si -X POST -H 'Origin: http://localhost:3000' --cookie 'ikigenba_session=<opaque>' http://localhost:3001/logout
```

Response:

```
HTTP/1.1 302 Found
Location: /
Set-Cookie: ikigenba_session=; Path=/; Max-Age=0; Secure; HttpOnly; SameSite=Lax
```

Status 302. The response is the same as for a sign-out from auth's own origin:
`Location: /` is auth's own `/`, so the browser lands on auth's sign-in page,
not back on the app. The `Set-Cookie` clears `ikigenba_session` exactly as in
the same-origin story; on a space it also carries `Domain=<space>`, run locally
it has none. On a space the request is the same with an `Origin` such as
`https://dummy.<space>`, `https://<space>`, or `https://a.b.<space>`: a
subdomain at any depth is accepted, because the session cookie reaches it.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session, and
  its `Origin` is on this space but is not auth's own origin (locally
  `http://localhost:3000`, or `http://localhost` or `http://localhost:<port>`
  for any other port; on a space `https://<space>`, or `https://<host>.<space>`
  for any subdomain at any depth other than `auth`).

Postconditions:

- The session is deleted server-side; the cookie no longer names any session.
- The browser removes the cookie for that domain and `Path=/`; it no longer
  sends that cookie to auth, the app the user signed out from, or the space's
  other apps. Following the redirect shows auth's sign-in page.
- The user row and the user's tokens are untouched.

## A user signs out from another site

A cross-site `POST` to `/logout`: its `Origin` is not on this space. The
`Origin` check is the second line of defense after `SameSite=Lax`, and it
refuses the request. On a space the same refusal meets an origin that names the
space's hosts over `http` rather than `https`: `http://<space>` or
`http://<host>.<space>`.

Request:

```
$ curl -si -X POST -H 'Origin: https://evil.example' --cookie 'ikigenba_session=<opaque>' http://localhost:3001/logout
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text. No `Set-Cookie` is sent.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session, and
  its `Origin` is not on this space (locally, not `http://localhost` or
  `http://localhost:<port>`; on a space, not `https://<space>` or
  `https://<host>.<space>` for a subdomain at any depth).

Postconditions:

- The session still exists; nothing has changed.

## A user signs out with no Origin

A `POST` to `/logout` that carries no `Origin` header cannot show it comes from
this space, so it is refused as a cross-site one is.

Request:

```
$ curl -si -X POST --cookie 'ikigenba_session=<opaque>' http://localhost:3001/logout
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text. No `Set-Cookie` is sent.

Preconditions:

- auth is serving on `127.0.0.1:3001` with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session, and
  no `Origin` header.

Postconditions:

- The session still exists; nothing has changed.
