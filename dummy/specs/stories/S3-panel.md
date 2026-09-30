# Stories — panel

The panel and dummy's routing: what a running dummy answers, and the frame
every page it serves is drawn in. dummy is the platform's UI reference
implementation, so every page is server-rendered HTML — the whole of a page's
content arrives in the response body, the launcher's list of services
included, and script never adds content of its own. A page's scripts only
act on what the server sent: the panel's re-fetches the table (`S4`), and the
launcher's, `/_appkit/launcher.js`, filters the list of services as the user
types. The stylesheet a page links is not script: it changes how the
content looks, never what the content is. An nginx gate in front of dummy
authenticates every request and sets `X-User-Id` and `X-User-Email` on the
request it passes upstream; a sibling app calling dummy forwards the ones it
received (`S2`). dummy trusts those two headers absolutely and has no
unauthenticated case, so there is no sign-in page and no signed-out banner.
Only nginx and the suite's own apps can reach dummy's socket, so a request
that arrives without `X-User-Id` means the gate or a sibling is misconfigured
— a server fault, not a bad request. On a developer's laptop there is no gate,
so the headers are passed by hand, and every request below that needs identity
shows them. The requests go to a dummy the developer serves with
`systemd-socket-activate -l 127.0.0.1:3000 dummy` (`S2`), with no services
file unless a story says otherwise.

The demo resource is widgets. A widget has a `name`, an integer `count`, and
a `status` that is one of `active`, `paused`, or `retired`. The widgets are an
in-memory fixture set, reset every time the process starts and surviving
nothing; at startup it holds exactly three, in this order: `alpha` count 3
status `active`, `beta` count 0 status `paused`, `gamma` count 12 status
`retired`.

Every page dummy serves is drawn in one common frame, the banner, the same
banner every app of the platform draws, at the top of the page. It holds the
mark, the email address of the caller taken from `X-User-Email` as a link to
their profile in auth, and a sign-out button; on a host with a services file
it also holds the launcher button (below). The mark's text is `ikigenba`, and
it names the service it fronts, `dummy`, which a browser shows as
`ikigenba │ dummy`; the service's name is lowercase `dummy` everywhere it
appears, and every page's title, the one a browser shows on its tab, is
`dummy`. Because the caller's identity
is what the banner is drawn from, a failure on a page that dummy can name to
an identified caller is itself a page with that same banner, and only the
missing-header fault, where there is no identity to draw with, is bare text.

The caller's email address in the banner is a link whose text is the
`X-User-Email` value, written as escaped HTML text, and whose target is
`<auth-profile>`, auth's root on the same space, an absolute URL derived from
the request as `<auth-logout>` is (below), so following it leaves dummy for
auth; what auth shows there is auth's behaviour, told in auth's own stories.
Apart from the launcher, which may list auth among the platform's services,
this link is the banner's one link to auth.

The sign-out button signs the caller out of the whole space in one click. It
follows the email in the banner, and it is a form, not a link: pressing it
POSTs to `<auth-logout>`. The `logout` icon is drawn before the text and
hidden from assistive technology, so the button's accessible text is
`Sign out` alone. `<auth-logout>` is auth's `/logout` on the same space, an
absolute URL, so submitting the form leaves dummy: the browser POSTs to auth,
carrying the space-wide `ikigenba_session` cookie, and what that POST does —
ending the session and sending the browser to auth's sign-in page — is auth's
behaviour, told in auth's own stories. dummy serves no logout route and sets
no cookie. dummy reads the space from the request's own `Host`: a trailing
port is dropped, then a single leading `dummy.` label; what remains is the
space, `<auth-profile>` is `<scheme>://auth.<space>/`, and `<auth-logout>` is
`<scheme>://auth.<space>/logout`. The scheme is
the request's `X-Forwarded-Proto` when that header is exactly `http` or
exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty
value, or no header at all. A `Host` with no `dummy.` label, as on a
developer's `127.0.0.1:3000`, has no space in it, and both then name auth's
local origin: `<auth-profile>` is `http://localhost:3001/` and `<auth-logout>`
is `http://localhost:3001/logout`.

The launcher is the banner's way to the platform's other services. It is
there only when the host's services file lists services: dummy takes the
file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts
(`S2`), and it reads the file itself afresh for every page, so a rewrite of
the file shows on the next page without a restart. The file is a JSON object
whose `services` member is an array; each entry is an object with `name`, a
non-empty string; `url`, a string; `icon`, a string holding the SVG text of
the service's icon; and `enabled`, `true` or `false`, `false` for a service
switched off. The array's order is the launcher's order. Members the launcher
does not know are ignored, and an entry that lacks one of the four, or holds
one of the wrong kind, is left out while the rest are still shown. With no
variable, no readable file, a file that is not such an object, or no usable
entry, the page has no launcher, and is otherwise the same page; dummy writes
nothing about it, since a broken launcher never breaks a page. When the
launcher is there, the banner holds a launcher button labelled `Services`;
pressing it opens the list of the services, which is closed when the page
loads. The list holds a search field labelled `Find a service`, with the
placeholder `Find a service`, and one entry per service in the file's order,
each showing the service's icon and then its name. An enabled service's entry
is a link to its `url`. A service switched off keeps its place but is not a
working link, and its entry is titled `<name> is unavailable`, which a
browser shows as its tooltip; its visible text is still its icon and name. dummy's own entry,
the one named `dummy`, is marked as the current page. The list, the search
field, and a hidden no-match line are all in the page as served; the one
script the launcher adds is `/_appkit/launcher.js`, and without a launcher the
page loads no such script.

Every HTML page dummy sends — the panel and every page with the banner: the
404, the 405, the 415, and the 422 redraw (`S5`) — links
`/_appkit/theme.css` as its stylesheet and declares the phone-width viewport, so a phone shows it at the phone's own
width rather than as a shrunken desktop page. The
missing-header 500, being bare text, has neither. The stylesheet, the fonts
it loads, and the launcher's script are the platform's shared files, served by
dummy under `/_appkit/` (`S8`); a page makes no request to any third party.
dummy serves nothing under `/assets/`: a path there is a path that does not
exist, like any other.

dummy writes one line to stderr for each request it answers with a 5xx, in
the form `S2` fixes, `dummy: request <id>: <reason>` — its only 5xx is the
missing-header 500 below — and nothing for any other answer: a 404, a 405, a 415, or a 422 is the caller's mistake, not
trouble, and a healthy dummy stays silent.

The routes are `GET /`, which sends the caller to the panel; `GET /widgets`,
the panel page; `GET /widgets/table`, the table fragment (`S4`);
`POST /widgets`, which creates a widget (`S5`); and `/_appkit/<name>`, the
stylesheet, launcher script, fonts, and licences that every page shares
(`S8`). A response block shows the status line and the headers the story
fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the panel

The panel is the whole of dummy's interface: the banner, the table of
widgets, and the form that creates one. The table's rows are in the document
that arrives, so a reader who fetches the page with `curl` has everything
someone looking at a browser has. Each widget's status is a word in its own
column, never a colour or an icon alone, for the same reason: the word is
marked with the status it names, so a browser can colour it while the word
stays the thing a reader reads. The count column is marked numeric, its
header cell and every count cell alike, so counts line up. The table's header
row holds exactly three cells, reading, in order, `Name`, `Count`, and
`Status`.

Above the table and the form is the page's heading, `Widgets`, the page's
top-level heading, with a subtitle beneath it reading
`3 widgets · refreshes every 5 seconds`.

The subtitle reads `<N> widgets · refreshes every 5 seconds`, where `<N>` is
the number of widgets when the page was rendered: `3 widgets` for the fixture
set, `1 widget` when there is exactly one, and `0 widgets` when there are
none. The page's script re-fetches the table every 5 seconds (`S4`), so the
subtitle's second half is true. The subtitle is drawn when the page is rendered
and is not part of the table, so the table's poll leaves it as it was; it catches up when the page is
next loaded.

The table and the form share the page: in a browser window at least 960 pixels
wide they sit side by side, the table first; narrower, as on a phone, they
stack, the form below the table. The form sits in a card headed
`Add widget`, a heading one level beneath the page's `Widgets` heading. The
form's button reads `Add widget`, with the `plus` icon drawn before the text
and hidden from assistive technology, so the button's accessible text is
`Add widget` alone.

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
`/_appkit/theme.css` as its stylesheet and declares the phone-width viewport.
Its visible text carries the mark's text `ikigenba` — the mark names the
service `dummy` — the caller's email address
`mg@example.com` as a link to `http://localhost:3001/`, the sign-out button reading `Sign out` in a form that
POSTs to `http://localhost:3001/logout`, the heading `Widgets` with the subtitle
`3 widgets · refreshes every 5 seconds`, and beneath it a table whose header
cells read `Name`, `Count`, and `Status` and whose rows are the
three fixture widgets in fixture order with their counts and their statuses as
words: `alpha` 3 `active`, `beta` 0 `paused`, `gamma` 12 `retired`. The
`Count` header cell and each count cell are marked numeric, and each status
word is inside a status marker naming that status. Beside the table is the
card headed `Add widget` holding the form that creates a widget, with a field
for each of a widget's three fields and a button reading `Add widget` behind
its hidden `plus` icon. The banner holds no launcher button, and the page
loads no `/_appkit/launcher.js`, since dummy has no services file. The text
`Dummy` appears nowhere.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with `IKIGENBA_SERVICES`
  unset.
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

The sign-out form and the email's link address auth on the space the request
names in its `Host`, never a fixed host, so one dummy build signs a caller out
of, and sends them to their profile on, whichever space it is serving. A developer shows the space's headers by hand.

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

Status 200. The body is the panel page, and its banner's sign-out button
reading `Sign out` is in a form whose method is `post` and whose action is
`https://auth.sbx.ikigenba.dev/logout`, and the caller's email address
`mg@example.com` in the banner links to `https://auth.sbx.ikigenba.dev/`. With
`X-Forwarded-Proto: http` and the same `Host`, the action is
`http://auth.sbx.ikigenba.dev/logout` and the email links to
`http://auth.sbx.ikigenba.dev/`. There is no launcher here, so the email is
the banner's only link to auth.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with `IKIGENBA_SERVICES`
  unset.

Postconditions:

- Nothing has changed.
- dummy set no cookie.

## A request arrives without the identity headers

In production this cannot happen from outside: the gate sets the headers on
every request it forwards, a sibling app forwards the ones it received, and
nothing but nginx and the suite's apps can reach dummy's socket. So a request
without `X-User-Id` says the gate or a sibling is misconfigured, which is dummy's fault to report, not the caller's to fix — hence a
500 and not a 400 or a 401. There is no identity to draw the banner from, so
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
missing. Every route answers this way, the root, the fragment, and the shared
files under `/_appkit/` included; the identity check runs before dummy looks at the path or the method, so a request
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

The caller is identified, so dummy can answer with the banner and give them the
way back to the panel rather than a dead end. dummy serves nothing under
`/assets/`, so a path there, such as `/assets/theme.css`, is answered the same
way; the page's stylesheet is under `/_appkit/` (`S8`).

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/nope
```

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/theme.css
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is an HTML document with the same banner as the panel —
the mark, `mg@example.com` linking to `http://localhost:3001/`, and the
sign-out button POSTing to `http://localhost:3001/logout`, with the same title,
stylesheet link, and viewport as every page (above) — whose visible text,
after the banner, says the page was not found and carries a link to `/widgets`.

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

Status 405. The body is an HTML document with the same banner as the panel —
the mark, `mg@example.com` linking to `http://localhost:3001/`, and the
sign-out button POSTing to `http://localhost:3001/logout` — with the same
title, stylesheet link, and viewport as every page (above), whose visible
text, after the banner, says the method is not allowed and carries a link to
`/widgets`. `PUT` and `PATCH` are refused the same way.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed. No widget was created.

## A user on a host with services opens the launcher

On a host, the services file lists the platform's services, and the launcher
is how a user gets from dummy to any of them without typing an address. A
developer stands in for the host by writing a services file and naming it
when serving dummy. The file here, `/tmp/services.json`, lists three
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
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`, with the
same banner, and the banner also holds the launcher button labelled
`Services`. The page carries the list of services labelled `Services`,
holding the search field labelled `Find a service` with the placeholder
`Find a service` and three entries in the file's order: `auth`, showing its
icon and then its name, a link to `https://auth.sbx.ikigenba.dev/`; `dummy`,
showing its icon and then its name, a link to
`https://dummy.sbx.ikigenba.dev/`, marked as the current page; and `ledger`,
showing its icon and then its name, not a working link, titled
`ledger is unavailable`. The no-match line is in the page and hidden. The page
loads the script `/_appkit/launcher.js`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with
  `systemd-socket-activate -E IKIGENBA_SERVICES=/tmp/services.json -l 127.0.0.1:3000 dummy`.
- `/tmp/services.json` holds the file above and is readable by dummy.
- The widgets are the fixture set as the process started it.

Postconditions:

- Nothing has changed. The services file is as it was.

## A user on a laptop with no services file sees no launcher

A developer's laptop has no services file, and nothing names one:
`IKIGENBA_SERVICES` is unset (`S2`). The banner is then the banner without a
launcher, and the page is otherwise the same page a host serves.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`: the banner
holds the mark, `mg@example.com` linking to `http://localhost:3001/`, and the
sign-out button POSTing to `http://localhost:3001/logout`, and no launcher
button. The page carries no list of services, no `Find a service` field, and
no no-match line, and it loads no `/_appkit/launcher.js`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with
  `systemd-socket-activate -l 127.0.0.1:3000 dummy`, so `IKIGENBA_SERVICES` is
  unset.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A
broken launcher never breaks a page, so dummy draws the page without one and
reports nothing: this is not a fault of the request, and dummy's answer is the
same as when no file is named at all. A file that exists but cannot be read,
is not a JSON object with a `services` array, or has no usable entry is
answered the same way.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`, with no
launcher button, no list of services, and no `/_appkit/launcher.js`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with
  `systemd-socket-activate -E IKIGENBA_SERVICES=/tmp/services.json -l 127.0.0.1:3000 dummy`.
- `/tmp/services.json` does not exist.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched
on or off, and dummy reads the file afresh for every page, so the next page a
user loads shows the new list without dummy being restarted. Here the host
has switched `ledger` on since dummy started: `/tmp/services.json` is the file
of `A user on a host with services opens the launcher` with `ledger`'s
`enabled` now `true`.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page with the launcher, as in
`A user on a host with services opens the launcher`, except that the `ledger`
entry is a link to `https://ledger.sbx.ikigenba.dev/` and is no longer titled as
unavailable.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with
  `systemd-socket-activate -E IKIGENBA_SERVICES=/tmp/services.json -l 127.0.0.1:3000 dummy`
  while `/tmp/services.json` listed `ledger` as switched off, and it has not
  been restarted since.
- `/tmp/services.json` now lists `ledger` with `enabled` `true`.

Postconditions:

- Nothing has changed.
