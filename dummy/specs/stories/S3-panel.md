# Stories — panel

The panel and dummy's routing: what a running dummy answers, and the frame
every page it serves is drawn in. dummy is the platform's UI reference
implementation, so every page is server-rendered HTML — the whole of a page's
content arrives in the response body, and nothing is assembled afterwards by
JavaScript. An nginx gate in front of dummy authenticates every request and
sets `X-User-Id` and `X-User-Email` on the request it passes upstream; dummy
trusts those two headers absolutely and has no unauthenticated case, so there
is no sign-in page and no signed-out chrome. Only the gate can reach dummy's
socket, so a request that arrives without `X-User-Id` means the gate is
misconfigured — a server fault, not a bad request. On a developer's laptop
there is no gate, so the headers are passed by hand, and every request below
that needs identity shows them.

The demo resource is widgets. A widget has a `name`, an integer `count`, and
a `status` that is one of `active`, `paused`, or `retired`. The widgets are an
in-memory fixture set, reset every time the process starts and surviving
nothing; at startup it holds exactly three, in this order: `alpha` count 3
status `active`, `beta` count 0 status `paused`, `gamma` count 12 status
`retired`.

Every page dummy serves is drawn in one common frame, the chrome: the service
name, the email address of the caller taken from `X-User-Email`, and a link to
sign out addressed to auth on its own host — an absolute URL, so following it
leaves dummy. Because the caller's identity is what the chrome is drawn from,
a failure on a page that dummy can name to an identified caller is itself a
page in that same chrome, and only the missing-header fault, where there is no
identity to draw with, is bare text.

The routes are `GET /`, which sends the caller to the panel; `GET /widgets`,
the panel page; `GET /widgets/table`, the table fragment (`S4`); and
`POST /widgets`, which creates a widget (`S5`). A response block shows the
status line and the headers the story fixes; a header it does not show, `Date`
say, is not fixed.

## A user opens the panel

The panel is the whole of dummy's interface: the chrome, the table of
widgets, and the form that creates one. The table's rows are in the document
that arrives, so a reader who fetches the page with `curl` has everything
someone looking at a browser has. Each widget's status is a word in its own
column, never a colour or an icon alone, for the same reason.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document whose visible text carries the
service name, the caller's email address `mg@example.com`, the sign-out link,
and the three fixture widgets in fixture order with their counts and their
statuses as words: `alpha` 3 `active`, `beta` 0 `paused`, `gamma` 12
`retired`. Below the table is the form that creates a widget, with a field for
each of a widget's three fields.

Preconditions:

- dummy is running with `PORT=3000`.
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

- dummy is running with `PORT=3000`.

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

- dummy is running with `PORT=3000`.

Postconditions:

- Nothing has changed.

## A request arrives without the identity headers

In production this cannot happen from outside: the gate sets the headers on
every request it forwards, and nothing but the gate can reach dummy's socket.
So a request without `X-User-Id` says the gate is misconfigured or has been
bypassed, which is dummy's fault to report, not the caller's to fix — hence a
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

- dummy is running with `PORT=3000`.
- The request carries no `X-User-Id` header.

Postconditions:

- Nothing has changed. No widget was read and none was created.

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
the service name, `mg@example.com`, and the sign-out link — whose visible text
says the page was not found and carries a link to `/widgets`.

Preconditions:

- dummy is running with `PORT=3000`.

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

Status 405. The body is an HTML document in the same chrome as the panel
whose visible text says the method is not allowed and carries a link to
`/widgets`. `PUT` and `PATCH` are refused the same way.

Preconditions:

- dummy is running with `PORT=3000`.

Postconditions:

- Nothing has changed. No widget was created.
