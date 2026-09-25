# Stories — panel

The panel and dummy's routing: what a running dummy answers, and the frame
every page it serves is drawn in. dummy is the platform's UI reference
implementation, so every page is server-rendered HTML — the whole of a page's
content arrives in the response body, and nothing is assembled afterwards by
JavaScript. The stylesheet a page links is not script: it changes how the
content looks, never what the content is. An nginx gate in front of dummy
authenticates every request and sets `X-User-Id` and `X-User-Email` on the
request it passes upstream; a sibling app calling dummy forwards the ones it
received (`S2`). dummy trusts those two headers absolutely and has no
unauthenticated case, so there is no sign-in page and no signed-out chrome.
Only nginx and the suite's own apps can reach dummy's socket, so a request
that arrives without `X-User-Id` means the gate or a sibling is misconfigured
— a server fault, not a bad request. On a developer's laptop there is no gate,
so the headers are passed by hand, and every request below that needs identity
shows them. The requests go to a dummy the developer serves with
`systemd-socket-activate -l 127.0.0.1:3000 dummy` (`S2`).

The demo resource is widgets. A widget has a `name`, an integer `count`, and
a `status` that is one of `active`, `paused`, or `retired`. The widgets are an
in-memory fixture set, reset every time the process starts and surviving
nothing; at startup it holds exactly three, in this order: `alpha` count 3
status `active`, `beta` count 0 status `paused`, `gamma` count 12 status
`retired`.

Every page dummy serves is drawn in one common frame, the chrome: the mark,
the email address of the caller taken from `X-User-Email`, and a sign-out
button. The mark's visible text is `ikigenba`, and its `data-service`
attribute names the service it fronts,
`<strong class="mark" data-service="dummy">ikigenba</strong>`, which the
stylesheet shows as `ikigenba │ dummy`; the service's name is lowercase
`dummy` everywhere it appears, and every page's `<title>` is `dummy`. Because
the caller's identity is what the chrome is drawn from, a failure on a page
that dummy can name to an identified caller is itself a page in that same
chrome, and only the missing-header fault, where there is no identity to draw
with, is bare text.

The sign-out button signs the caller out of the whole space in one click. It
is a form, not a link, and it follows the email in the chrome's header:

```
<form class="inline" method="post" action="<auth-logout>"><button class="secondary small" type="submit"><svg class="ico" aria-hidden="true" …>…</svg>Sign out</button></form>
```

The `logout` icon is drawn before the text as an inline
`<svg class="ico" aria-hidden="true">`, so the button's accessible text is
`Sign out` alone. `<auth-logout>` is auth's `/logout` on the same space, an
absolute URL, so submitting the form leaves dummy: the browser POSTs to auth,
carrying the space-wide `ikigenba_session` cookie, and what that POST does —
ending the session and sending the browser to auth's sign-in page — is auth's
behaviour, told in auth's own stories. dummy serves no logout route and sets
no cookie. dummy reads the space from the request's own `Host`: a trailing
port is dropped, then a single leading `dummy.` label; what remains is the
space, and `<auth-logout>` is `<scheme>://auth.<space>/logout`. The scheme is
the request's `X-Forwarded-Proto` when that header is exactly `http` or
exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty
value, or no header at all. A `Host` with no `dummy.` label, as on a
developer's `127.0.0.1:3000`, has no space in it, and `<auth-logout>` is then
auth's local origin, `http://localhost:3001/logout`.

Every HTML page dummy sends — the panel and every page in the chrome: the
404, the 405, the 415, and the 422 redraw (`S5`) — links `/assets/theme.css`
as its stylesheet, `<link rel="stylesheet" href="/assets/theme.css">`, and
declares the phone-width viewport,
`<meta name="viewport" content="width=device-width, initial-scale=1">`. Each
such page draws the chrome as a `<header>` at the top of its `<body>`, and
puts everything after that header — the page's own content — inside one
`<main>` element, so the content sits in the same centred column as the
chrome rather than running the full width of the window. The
missing-header 500, being bare text, has neither. The
stylesheet and the fonts it loads are dummy's own, served under `/assets/`
(`S8`); a page makes no request to any third party.

dummy writes one line to stderr for each request it answers with a 5xx, in
the form `S2` fixes, `dummy: request <id>: <reason>` — its only 5xx is the
missing-header 500 below — and nothing for any other answer: a 404, a 405, a 415, or a 422 is the caller's mistake, not
trouble, and a healthy dummy stays silent.

The routes are `GET /`, which sends the caller to the panel; `GET /widgets`,
the panel page; `GET /widgets/table`, the table fragment (`S4`);
`POST /widgets`, which creates a widget (`S5`); and `/assets/<name>`, the
stylesheet, fonts, and licence that give every page its look (`S8`). A
response block shows the status line and the headers the story fixes; a
header it does not show, `Date` say, is not fixed.

## A user opens the panel

The panel is the whole of dummy's interface: the chrome, the table of
widgets, and the form that creates one. The table's rows are in the document
that arrives, so a reader who fetches the page with `curl` has everything
someone looking at a browser has. Each widget's status is a word in its own
column, never a colour or an icon alone, for the same reason: the word sits
inside a status marker that names its value,
`<span class="status" data-status="active">active</span>`, so the stylesheet
can colour it while the word stays the thing a reader reads. The count
column is marked numeric, its header cell `<th class="num">Count</th>` and
each row's count cell `<td class="num">3</td>`, so counts line up. The
table's header row holds exactly three cells, in order, `<th>Name</th>`,
`<th class="num">Count</th>`, and `<th>Status</th>`.

Above the table and the form is the page's heading block, the page title and
a subtitle beneath it:

```
<div class="section-head"><div><h1>Widgets</h1><p>3 widgets · refreshes every 5 seconds</p></div></div>
```

The subtitle reads `<N> widgets · refreshes every 5 seconds`, where `<N>` is
the number of widgets when the page was rendered: `3 widgets` for the fixture
set, `1 widget` when there is exactly one, and `0 widgets` when there are
none. The page's script re-fetches the table every 5 seconds (`S4`), so the
subtitle's second half is true. The subtitle is drawn when the page is rendered
and is not part of the table, so the table's poll leaves it as it was; it catches up when the page is
next loaded.

The table and the form share the page: in a browser window at least 960 pixels
wide they sit side by side, the table first; narrower, as on a phone, they
stack, the form below the table. The form sits in a card whose `<header>`
holds its heading, `<h2 class="text-md">Add widget</h2>`, a heading beneath
the page's `Widgets` heading. The form's button reads `Add widget`, with the
`plus` icon drawn before the text as an inline
`<svg class="ico" aria-hidden="true">`, so the icon is hidden from assistive
technology and the button's accessible text is `Add widget` alone.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `dummy` that links
`/assets/theme.css` as its stylesheet and declares the phone-width viewport.
Its visible text carries the mark's text `ikigenba` — the mark names the
service `dummy` in its `data-service` attribute — the caller's email address
`mg@example.com`, the sign-out button reading `Sign out` in a form that
POSTs to `http://localhost:3001/logout`, the heading `Widgets` with the subtitle
`3 widgets · refreshes every 5 seconds`, and beneath it a table whose header
cells read `Name`, `Count`, and `Status` and whose rows are the
three fixture widgets in fixture order with their counts and their statuses as
words: `alpha` 3 `active`, `beta` 0 `paused`, `gamma` 12 `retired`. The
`Count` header cell and each count cell are marked numeric, and each status
word is inside a status marker naming that status. Beside the table is the
card headed `Add widget` holding the form that creates a widget, with a field
for each of a widget's three fields and a button reading `Add widget` behind
its hidden `plus` icon. Everything after the chrome's header — the heading
block, the table, and the card — is inside the page's one `<main>` element.
The text `Dummy` appears nowhere.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- Nothing has changed.

## A user asks for the service root

Nothing lives at the root; the panel is where a caller who typed the bare
host is meant to land, and the root exists only to send them there.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/
```

Response:

```
HTTP/1.1 303 See Other
Location: /widgets
```

Status 303. The body is empty.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed.

## A user's client asks for the panel's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike,
with no body. The panel is what a monitor or a proxy reaches for when it wants
to know dummy is up without paying for the page.

Request:

```
$ curl -sI -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is empty.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed.

## A user on a space is offered sign-out from that space

The sign-out form addresses auth on the space the request names in its
`Host`, never a fixed host, so one dummy build signs a caller out of whichever
space it is serving. A developer shows the space's headers by hand.

Request:

```
$ curl -si -H 'Host: dummy.sbx.ikigenba.dev:443' -H 'X-Forwarded-Proto: https' -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

```
$ curl -si -H 'Host: dummy.sbx.ikigenba.dev' -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

```
$ curl -si -H 'Host: dummy.sbx.ikigenba.dev' -H 'X-Forwarded-Proto: HTTPS' -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page, and its chrome's sign-out button
reading `Sign out` is in a form whose method is `post` and whose action is
`https://auth.sbx.ikigenba.dev/logout`. With `X-Forwarded-Proto: http` and the
same `Host`, the action is `http://auth.sbx.ikigenba.dev/logout`. No link in
the chrome leads to auth.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed.
- dummy set no cookie.

## A request arrives without the identity headers

In production this cannot happen from outside: the gate sets the headers on
every request it forwards, a sibling app forwards the ones it received, and
nothing but nginx and the suite's apps can reach dummy's socket. So a request
without `X-User-Id` says the gate or a sibling is misconfigured, which is dummy's fault to report, not the caller's to fix — hence a
500 and not a 400 or a 401. There is no identity to draw the chrome from, so
this one answer is bare text. A developer meets it by forgetting the headers,
as here.

Request:

```
$ curl -si http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is one line of plain text saying the identity header is
missing. Every route answers this way, the root and the fragment included; the
identity check runs before dummy looks at the path or the method, so a request
with no headers is never a 303, a 404, or a 405.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The request carries no `X-User-Id` header.

Postconditions:

- Nothing has changed. No widget was read and none was created.
- dummy wrote one line to stderr, `dummy: request -: X-User-Id is missing`,
  naming the request `-` because it carried no `X-Request-Id`.

## A request from nginx arrives without the identity headers

nginx sets `X-Request-Id` on every request it forwards, so when the gate is
misconfigured and forwards a request without `X-User-Id`, the line dummy
writes names the request by the id nginx gave it, and the operator reading the
journal can find the same request in nginx's log. The developer here stands in
for such an nginx by sending the id by hand.

Request:

```
$ curl -si -H 'X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is the same one line of plain text saying the identity
header is missing.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The request carries `X-Request-Id` and no `X-User-Id` header.

Postconditions:

- Nothing has changed. No widget was read and none was created.
- dummy wrote one line to stderr,
  `dummy: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: X-User-Id is missing`.

## A caller asks for a path that does not exist

The caller is identified, so dummy can answer in the chrome and give them the
way back to the panel rather than a dead end.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/nope
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is an HTML document in the same chrome as the panel —
the mark, `mg@example.com`, and the sign-out button, with the same title,
stylesheet link, and viewport as every page (above) — whose visible text,
inside the page's one `<main>` element after the chrome's header,
says the page was not found and carries a link to `/widgets`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed.

## A caller sends the panel a method it does not take

`Allow` names every method `/widgets` takes, whichever story owns it: `GET`
and `HEAD` read the panel, and `POST` creates a widget (`S5`).

Request:

```
$ curl -si -X DELETE -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD, POST
Content-Type: text/html; charset=utf-8
```

Status 405. The body is an HTML document in the same chrome as the panel,
with the same title, stylesheet link, and viewport as every page (above),
whose visible text, inside the page's one `<main>` element after the chrome's
header, says the method is not allowed and carries a link to
`/widgets`. `PUT` and `PATCH` are refused the same way.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed. No widget was created.
