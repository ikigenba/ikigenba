# Stories — sign-in

The browser sign-in flow: the sign-in page and profile at `/`, the start of a
Google sign-in at `/login/google`, the callback at `/login/google/callback`,
and sign-out at `/logout`. The requests go to a running auth (`S2-serve.md`),
started with its Google settings and no services file unless a story says
otherwise; on a space they reach it through nginx. Each request is shown as the
HTTP request auth receives, with the headers the story depends on. A request
whose `Host` is `localhost:3001` is a local one and takes the fixed development
forms stated below; every other request is on a space. The facts that depend
on the space's own hostname — the callback `redirect_uri`, the session
cookie's `Domain`, and which return URLs count as being under the space — are
stated in prose, because a real space carries them and `S7-on-a-space.md`
proves the whole path there.

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
HTML page auth serves is titled `auth`, links `/_appkit/theme.css` as its
stylesheet, and declares the phone-width viewport, so a phone shows it at the
phone's own width rather than as a shrunken desktop page. The stylesheet, the
fonts it loads, and the launcher's script are the platform's shared files,
served by auth under `/_appkit/` (`S8-assets.md`); a page makes no request to
any other host. auth serves nothing under `/assets/`: a path there is a path
that does not exist, like any other. An icon is a Tabler outline icon
drawn inline before a button's text as `<svg class="ico" aria-hidden="true">`,
so the button's accessible text is its word alone. The workspace a page names
is `WORKSPACE_DOMAIN`, here `michaelgreenly.dev`. A page names the apex, which
auth reads from the request's own `Host`: a trailing port is dropped, and the
apex is the last two dot-separated labels of what remains —
`auth.sbx.ikigenba.dev` gives `ikigenba.dev`, and the local `localhost:3001`
gives `localhost`.

auth draws its pages in one of two frames. A page for a visitor who is not
signed in is a sign-in card: it has no banner, no launcher, and no page
footer, and its `<body>` holds `<main class="auth-page">`, which holds one
`<section class="card">`; the card begins with the bare mark,
`<span class="mark">ikigenba</span>`, and its heading is `<h1>` reading
`Sign in to <apex>`. Its way forward is a link styled as a button,
`<a class="button secondary large google">`, that starts a Google sign-in. A
page for a signed-in user — the profile here, and the token-created page and
the rejected-create page (`S5-tokens.md`) — is drawn with the banner, the same
banner every app of the platform draws, at the top of the page. It holds the
mark, the profile icon, and a sign-out button; on a host with a services file
it also holds the launcher button (below). The mark's text is `ikigenba`, and
it names the service it fronts, `auth`, which a browser shows as
`ikigenba │ auth`; the mark is not a link. The profile icon is a link to `/`,
auth's own profile: `<a class="profile">`, labelled `Profile` for assistive
technology and titled with the user's email address, which a browser shows as
its tooltip, so hovering it shows who is signed in. It shows the `user-circle`
icon and no text; the email is not part of the page's visible text there. The
banner is followed by one `<main>` element holding the page's content, and the
page ends with the page footer, a `<footer>` that is the last thing in the
body, reading `auth <version>`: the service's name, one space, and auth's
version exactly as `auth --version` prints it (`S1-bootstrap.md`). The
version is data; no story fixes its value.

The sign-out button signs the user out of the whole space in one click. It
follows the profile icon in the banner, and it is a form, not a link:
pressing it POSTs to `/logout` (`A user signs out`). The `logout` icon is drawn before
the text and hidden from assistive technology, so the button's accessible
text is `Sign out` alone.

The launcher is the banner's way to the platform's other services. It is
there only when the host's services file lists services: auth takes the
file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts
(`S2-serve.md`), and it reads the file itself afresh for every page, so a
rewrite of the file shows on the next page without a restart. The file is a
JSON object whose `services` member is an array; each entry is an object with
`name`, a non-empty string; `url`, a string; `icon`, a string holding the SVG
text of the service's icon; and `enabled`, `true` or `false`, `false` for a
service switched off. The array's order is the launcher's order. Members the
launcher does not know are ignored, and an entry that lacks one of the four,
or holds one of the wrong kind, is left out while the rest are still shown.
With no variable, no readable file, a file that is not such an object, or no
usable entry, the page has no launcher, and is otherwise the same page; auth
writes nothing about it, since a broken launcher never breaks a page. When
the launcher is there, the banner holds a launcher button labelled
`Services`; pressing it opens the list of the services, which is closed when
the page loads. The list holds a search field labelled `Find a service`, with
the placeholder `Find a service`, and one entry per service in the file's
order, each showing the service's icon and then its name. An enabled
service's entry is a link to its `url`. A service switched off keeps its place
but is not a working link, and its entry is titled `<name> is unavailable`,
which a browser shows as its tooltip; its visible text is still its icon and
name. auth's own entry, the one named `auth`, is marked as the current page.
The list, the search field, and a hidden no-match line are all in the page as
served; the one script the launcher adds is `/_appkit/launcher.js`, and
without a launcher the page loads no such script. A sign-in card never has a
launcher, whatever the services file holds.

auth's failures that are not pages — the 400, the 502s, and the 403 sign-out
refusals below — stay one line of plain text in neither frame, because the
visitor may not be signed in. A response block shows the status line and
only the headers the story fixes; a header it does not show is not fixed.

## A visitor asks for the sign-in page

With no live session the index is the sign-in page: a sign-in card whose only
way forward is the link that starts a Google sign-in. It tells the visitor
which space they are signing in to and that one sign-in covers every service
there.

Request:

```
GET / HTTP/1.1
Host: localhost:3001
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
The footer names the `Host` as sent, port included. The page has no banner
and no launcher, and loads no `/_appkit/launcher.js`.

Preconditions:

- auth is serving, with `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev` set.
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
GET / HTTP/1.1
Host: auth.sbx.ikigenba.dev
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

- auth is serving, with its Google settings and
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
GET /?return=https%3A%2F%2Fdummy.sbx.ikigenba.dev%2Fwidgets HTTP/1.1
Host: auth.sbx.ikigenba.dev
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
Run locally, a request for `/` with `Host: localhost:3001` and
`?return=http%3A%2F%2Flocalhost%3A3000%2Fwidgets` is the same page with the
heading `Sign in to localhost` and the sentence
`Sign in to continue to localhost:3000.`

Preconditions:

- auth is serving, with its Google settings and
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
GET /?return=https%3A%2F%2Fevil.example%2F HTTP/1.1
Host: auth.sbx.ikigenba.dev
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

- auth is serving, with its Google settings and
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
GET /login/google HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.

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
GET /login/google HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET /login/google/callback?code=<code>&state=<state> HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET /login/google/callback?code=<code>&state=<state> HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET /login/google/callback?code=<code>&state=<state> HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET /login/google/callback?code=<code>&state=<state> HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET /login/google/callback?code=<code>&state=<state> HTTP/1.1
Host: localhost:3001
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8
```

Status 400. The body is one line of plain text saying the sign-in could not be
verified. A callback with no `state` at all is refused the same way.

Preconditions:

- auth is serving, with its Google settings.
- No in-flight login state matches `<state>`.

Postconditions:

- No user row, no session, and no cookie are created. Nothing has changed.

## A visitor cancels at Google

The visitor declined at Google, so the callback carries `error=access_denied`
instead of a code. auth answers with the sign-in card again, saying the
sign-in was cancelled and offering to start over.

Request:

```
GET /login/google/callback?error=access_denied&state=<state> HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET /login/google/callback?code=<code>&state=<state> HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET /login/google/callback?code=<code>&state=<state> HTTP/1.1
Host: localhost:3001
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

- auth is serving, with its Google settings.
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
GET / HTTP/1.1
Host: localhost:3001
Cookie: ikigenba_session=<opaque>
```

```
GET /?return=<url> HTTP/1.1
Host: localhost:3001
Cookie: ikigenba_session=<opaque>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `auth`, with the stylesheet
link and viewport every page has, drawn with the banner: the mark's text
`ikigenba`, naming the service `auth` and not a link, the profile icon
labelled `Profile` and titled `ada@michaelgreenly.dev` as a link to `/`, and
the `Sign out` button in the form that POSTs to `/logout`. The banner holds no
launcher button, and the page loads no `/_appkit/launcher.js`, since auth has
no services file here. Inside the page's one
`<main>` its visible text is the heading `Your account` with the subtitle
`You're signed in to localhost.`; the `Account` card reading `Email`
`ada@michaelgreenly.dev`, `Workspace` `michaelgreenly.dev`, and
`Signed in via` `Google`; the `API tokens` card with its explanation and the
user's tokens (`S5-tokens.md`); and the `Create a token` card, whose form
POSTs to `/tokens` with fields `name` and `expires` (`S5-tokens.md`). After
the `<main>`, the page ends with the page footer reading `auth <version>`,
where `<version>` is what `auth --version` prints.
Both forms return the same page; the `?return=<url>` is ignored.

Preconditions:

- auth is serving, with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`, started with `IKIGENBA_SERVICES`
  unset.
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
POST /logout HTTP/1.1
Host: localhost:3001
Origin: http://localhost:3001
Cookie: ikigenba_session=<opaque>
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

- auth is serving, with its Google settings.
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
POST /logout HTTP/1.1
Host: localhost:3001
Origin: http://localhost:3000
Cookie: ikigenba_session=<opaque>
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

- auth is serving, with its Google settings.
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
POST /logout HTTP/1.1
Host: localhost:3001
Origin: https://evil.example
Cookie: ikigenba_session=<opaque>
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text. No `Set-Cookie` is sent.

Preconditions:

- auth is serving, with its Google settings.
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
POST /logout HTTP/1.1
Host: localhost:3001
Cookie: ikigenba_session=<opaque>
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text. No `Set-Cookie` is sent.

Preconditions:

- auth is serving, with its Google settings.
- The request carries an `ikigenba_session` cookie naming a live session, and
  no `Origin` header.

Postconditions:

- The session still exists; nothing has changed.

## A user on a host with services opens the launcher

On a host, the services file lists the platform's services, and the launcher
is how a user gets from their profile to any of them without typing an
address. A developer stands in for the host by writing a services file and
naming it when serving auth. The file here, `/tmp/services.json`, lists three
services, one of them switched off:

```
{
  "services": [
    {"name": "auth", "url": "https://auth.sbx.ikigenba.dev/", "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><circle cx='12' cy='12' r='9'/></svg>", "enabled": true},
    {"name": "dummy", "url": "https://dummy.sbx.ikigenba.dev/", "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><rect x='4' y='4' width='16' height='16'/></svg>", "enabled": true},
    {"name": "ledger", "url": "https://ledger.sbx.ikigenba.dev/", "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><path d='M4 20L20 4'/></svg>", "enabled": false}
  ]
}
```

In a browser, the list is closed when the page loads, and pressing the
launcher button opens it. Typing in the search field keeps only the entries
whose name contains the typed text, ignoring case and any spaces around it;
clearing the field shows them all again. When the text matches no entry, the
no-match line appears, reading `No service matches “<text>”.` with the typed
text in quotation marks. Pressing Enter in the search field opens the first
entry still shown that is a working link, and does nothing when there is
none. That filtering is the whole of what `/_appkit/launcher.js` does: every
entry, and the no-match line, arrived with the page.

Request:

```
GET / HTTP/1.1
Host: localhost:3001
Cookie: ikigenba_session=<opaque>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page of `A user asks for the profile`,
with the same banner, and the banner also holds the launcher button labelled
`Services`. The page carries the list of services labelled `Services`,
holding the search field labelled `Find a service` with the placeholder
`Find a service` and three entries in the file's order: `auth`, showing its
icon and then its name, a link to `https://auth.sbx.ikigenba.dev/`, marked as
the current page; `dummy`, showing its icon and then its name, a link to
`https://dummy.sbx.ikigenba.dev/`; and `ledger`, showing its icon and then its
name, not a working link, titled `ledger is unavailable`. The no-match line
is in the page and hidden. The page loads the script `/_appkit/launcher.js`.

Preconditions:

- auth is serving, with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`, started with
  `IKIGENBA_SERVICES=/tmp/services.json` in its environment.
- `/tmp/services.json` holds the file above and is readable by auth.
- The request carries an `ikigenba_session` cookie naming a live session for a
  provisioned user whose email is `ada@michaelgreenly.dev`.

Postconditions:

- Nothing has changed. The services file is as it was.

## A user on a laptop with no services file sees no launcher

A developer's laptop has no services file, and nothing names one:
`IKIGENBA_SERVICES` is unset (`S2-serve.md`). The banner is then the banner
without a launcher, and the page is otherwise the same page a host serves.

Request:

```
GET / HTTP/1.1
Host: localhost:3001
Cookie: ikigenba_session=<opaque>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page of `A user asks for the profile`:
the banner holds the mark, the profile icon titled `ada@michaelgreenly.dev`
linking to `/`, and the sign-out button POSTing to `/logout`, and no launcher
button. The page
carries no list of services, no `Find a service` field, and no no-match line,
and it loads no `/_appkit/launcher.js`.

Preconditions:

- auth is serving, with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`, started with `IKIGENBA_SERVICES`
  unset.
- The request carries an `ikigenba_session` cookie naming a live session for a
  provisioned user whose email is `ada@michaelgreenly.dev`.

Postconditions:

- Nothing has changed.
- auth wrote nothing to stderr.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A
broken launcher never breaks a page, so auth draws the page without one and
reports nothing: this is not a fault of the request, and auth's answer is the
same as when no file is named at all. A file that exists but cannot be read,
is not a JSON object with a `services` array, or has no usable entry is
answered the same way.

Request:

```
GET / HTTP/1.1
Host: localhost:3001
Cookie: ikigenba_session=<opaque>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page of `A user asks for the profile`,
with no launcher button, no list of services, and no `/_appkit/launcher.js`.

Preconditions:

- auth is serving, with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`, started with
  `IKIGENBA_SERVICES=/tmp/services.json` in its environment.
- `/tmp/services.json` does not exist.
- The request carries an `ikigenba_session` cookie naming a live session for a
  provisioned user whose email is `ada@michaelgreenly.dev`.

Postconditions:

- Nothing has changed.
- auth wrote nothing to stderr.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched
on or off, and auth reads the file afresh for every page, so the next page a
user loads shows the new list without auth being restarted. Here the host
has switched `ledger` on since auth started: `/tmp/services.json` is the file
of `A user on a host with services opens the launcher` with `ledger`'s
`enabled` now `true`.

Request:

```
GET / HTTP/1.1
Host: localhost:3001
Cookie: ikigenba_session=<opaque>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page with the launcher, as in
`A user on a host with services opens the launcher`, except that the `ledger`
entry is a link to `https://ledger.sbx.ikigenba.dev/` and is no longer titled
as unavailable.

Preconditions:

- auth is serving, with its Google settings and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`, started with
  `IKIGENBA_SERVICES=/tmp/services.json` in its environment while
  `/tmp/services.json` listed `ledger` as switched off, and it has not
  been restarted since.
- `/tmp/services.json` now lists `ledger` with `enabled` `true`.
- The request carries an `ikigenba_session` cookie naming a live session for a
  provisioned user whose email is `ada@michaelgreenly.dev`.

Postconditions:

- Nothing has changed.
