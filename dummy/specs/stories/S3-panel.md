# Stories — panel

The panel and dummy's routing: what a running dummy answers, and the frame
every page it serves is drawn in. dummy is the platform's UI reference
implementation, so every page is server-rendered HTML — the whole of a page's
content arrives in the response body, the launcher's list of services
included, and no script adds content a user sees. A page's scripts only act
on what the server sent: the panel's re-fetches the table (`S4`); the
launcher's, `/_appkit/launcher.js`, filters the list of services as the user
types; and the platform's button feedback script, `/_appkit/feedback.js`,
which every page loads with or without a launcher, makes an enabled button
visibly react when the user presses it. The stylesheet a page links is not
script: it changes how the content
looks, never what the content is; nor is the icon a page links, the
platform's favicon, which a browser shows in the page's tab. An nginx gate in front of dummy
authenticates every request and sets `X-User-Id` and `X-User-Email` on the
request it passes upstream; a sibling app calling dummy forwards the ones it
received (`S2`). dummy trusts those two headers absolutely and has no
unauthenticated case, so there is no sign-in page and no signed-out banner.
Only nginx and the suite's own apps can reach dummy's socket, so a request
that arrives without `X-User-Id`, or with it empty, means the gate or a
sibling is misconfigured — a server fault, not a bad request. On a developer's
laptop there is no gate, so the headers are passed by hand, and every request
below that needs identity shows them. The requests go to a running dummy
(`S2`), started, unless a story says otherwise, with a services file whose
one entry is the telemetry service's, which carries no icon and takes every
event (`S2`), so the page has no launcher. Each is
shown as the HTTP request dummy receives, with the headers the story depends
on; a request that shows no `Host` header carries one with no `dummy.` label.

The demo resource is widgets. A widget has a `name`, an integer `count`, and
a `status` that is one of `active`, `paused`, or `retired`. The widgets are
kept in dummy's database, `state/dummy.db` under its working directory, and
every request shares them. A database dummy creates holds no widgets; every
widget created since is kept across restarts and deploys, with the id it was
given. The panel's table lists them in the order they were created (`S4`).
Every widget has an id, which dummy gives it when the widget is created:
`wgt_` followed by 16 lowercase hexadecimal digits, drawn at random, so in
practice no two widgets ever share one. The id is how dummy's trail names a
widget (`S2`), since the trail never carries a widget's name; the MCP tools
show it beside the name (`S9-mcp.md`), and the panel and the table fragment
do not show it.

Every page dummy serves is drawn in one common frame, the banner, the same
banner every app of the platform draws, at the top of the page. It holds the
mark, a profile icon linking to the caller's profile in auth, and a sign-out
button; on a host with a services file it also holds the launcher button
(below). The mark's text is `ikigenba`, and
it names the service it fronts, `dummy`; the service's name is lowercase `dummy` everywhere it
appears, and every page's title, the one a browser shows on its tab, is
`dummy`. Because the caller's identity
is what the banner is drawn from, a failure on a page that dummy can name to
an identified caller is itself a page with that same banner. Of the answers
to `/` and `/widgets`, only two are bare text: the missing-header fault,
where there is no identity to draw with, and the answer dummy gives when it
cannot reach the widgets (below).

Every page with the banner also ends with the footer, the last thing on the
page, whose text is `dummy <display>`: the service's name, one space, and
`<display>`, the string `dummy --version` prints under the environment dummy
was started with (`S1`), exactly as it prints it, so a user can tell which
code is serving the page. No story fixes its value. When that environment sets
neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`, `<display>` is empty and the
footer's text is `dummy` and the one space. The missing-header 500 and the 503
dummy answers when it cannot reach the widgets, being bare text, have no
footer, and neither does the table fragment (`S4`).

The profile link in the banner has no text of its own: it is labelled
`Profile` for assistive technology and titled with the caller's
`X-User-Email` value, exactly as it arrived, which a browser shows as its
tooltip, so hovering the link shows who is signed in. Outside the banner,
nothing on the page shows the email.

The profile link's target is
`<auth-profile>`, auth's root on the same space, an absolute URL derived from
the request as `<auth-logout>` is (below), so following it leaves dummy for
auth; what auth shows there is auth's behaviour, told in auth's own stories.

The sign-out button signs the caller out of the whole space in one click. It
is in a form, not a link: pressing it POSTs to `<auth-logout>`. The button
reads `Sign out`. `<auth-logout>` is auth's `/logout` on the same space, an
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
developer's laptop, has no space in it, and both then name auth's
local origin: `<auth-profile>` is `http://localhost:3001/` and `<auth-logout>`
is `http://localhost:3001/logout`.

The launcher is the banner's way to the platform's other services. It is there
only when the host's services file lists services: dummy takes the file's path
from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S2`), and it
reads the file itself afresh for every page, so a rewrite of the file shows on
the next page without a restart. The file is a JSON object whose `services`
member is an array; each entry is an object with `name`, a non-empty string;
`url`, `description`, and `socket`, strings; `enabled`, `true` or `false`,
`false` for a service switched off; `mcp`, `true` or `false`; and, optionally,
`icon`, a string holding the SVG text of the service's icon. The file lists
every service on the host, but the launcher offers only the entries that carry
an icon: an entry with no `icon`, or one that is not a string, stays out of
the launcher. The array's order is the launcher's order. Members the launcher
does not know are ignored, and an entry that lacks one of the six others, or
holds one of the wrong kind, is left out while the rest are still shown. With
no variable, no readable file, a file that is not such an object, or no usable
entry that carries an icon, the page has no launcher, and is otherwise the
same page; dummy writes nothing about it, since a broken launcher never breaks
a page. When the launcher is there, the banner holds a launcher button
labelled `Services`; pressing it opens the list of the services, which is
closed when the page loads. The list holds a search field labelled `Find a
service`, with the placeholder `Find a service`, and one entry per service
that carries an icon, in the file's order, each showing the service's icon and
then its name. An enabled service's entry is a link to its `url`. A service
switched off keeps its place but is not a working link, and its entry is
titled `<name> is unavailable`, which a browser shows as its tooltip; its
visible text is still its icon and name. dummy's own entry, the one named
`dummy`, is marked as the current page. The list, the search field, and a
hidden no-match line are all in the page as served; the one script the
launcher adds is `/_appkit/launcher.js`, and without a launcher the page loads
no such script. The banner, its launcher and the launcher's script are the
platform's, drawn by the platform's shared page kit for every app; dummy
decides what goes into the banner, and the platform how it is drawn.

Every HTML page dummy sends — the panel and every page with the banner: the
404, the 405, the 415, and the 422 redraw (`S5`) — links `/_appkit/theme.css`
as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads
`/_appkit/feedback.js`, and declares the phone-width
viewport, so a phone shows it at the phone's own width rather than as a
shrunken desktop page. The missing-header 500 and the 503, being bare text,
have none of them. The stylesheet, the fonts it loads, the launcher's script,
the button feedback script, and the favicon are the platform's shared files, served by
dummy under `/_appkit/` (`S8`); a page makes no request to any third party.
dummy serves nothing under `/assets/`: a path there is a path that does not
exist, like any other.

dummy records every request it serves in its trail (`S2`), whatever the route
and whatever the answer, the shared files under `/_appkit/`, `/mcp`, the
missing-header 500 and the 503 included. When the request arrives it records
`request.started`, whose attributes are the request's `method`, as sent, and
its `path`, never its query: `GET /widgets?sort=name` records the `path`
`/widgets`. When the answer is complete it records `request.finished`, whose
attributes are the answer's `status`, a number; `duration_us`, how long dummy
took to answer, in whole microseconds; `request_bytes`, how many bytes of the
request's body dummy read; and `response_bytes`, how many bytes of body its
answer carried. Both carry the request's `X-Request-Id`, or the id dummy gave
a request that came without one (`S2`), and the caller's `X-User-Id`, empty
when there is none; anything dummy records while answering, a widget it
creates (`S5`) or a tool call (`S9-mcp.md`), falls between the two under the
same request id and user. A request with a `request.started` and no
`request.finished` is one dummy never finished answering. No answer earns a
line on stderr: a 404, a 405, a 415, or a 422 is the caller's mistake, and
dummy's two 5xx answers, the missing-header 500 (its only 500) and the 503 it
answers when it cannot reach the widgets, both below, are recorded by their
`request.finished` like every other answer, so a dummy whose telemetry takes
every event writes nothing to stderr at all (`S2`).

The routes are `GET /`, which sends the caller to the panel; `GET /widgets`,
the panel page; `GET /widgets/table`, the table fragment (`S4`); `POST
/widgets`, which creates a widget (`S5`); `/_appkit/<name>`, the stylesheet,
launcher script, button feedback script, favicon, fonts, and licences that every
page shares (`S8`); and
`/mcp`, exactly that path, the MCP endpoint that offers the widgets to MCP
clients, whose answers, to every method, are `S9-mcp.md`'s and never one of
the pages below. A response block shows the status line and the headers the
story fixes; a header it does not show, `Date` say, is not fixed.

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
the number of widgets when the page was rendered: `3 widgets` for the three
below, `1 widget` when there is exactly one, and `0 widgets` when there are
none, as in a database dummy has just created. The page's script re-fetches
the table every 5 seconds (`S4`), so the subtitle's second half is true. The
subtitle is drawn when the page is rendered and is not part of the table, so
the table's poll leaves it as it was; it catches up when the page is next
loaded.

The table and the form share the page: in a browser window at least 960 pixels
wide they sit side by side, the table first; narrower, as on a phone, they
stack, the form below the table. The form sits in a card headed
`Add widget`, a heading one level beneath the page's `Widgets` heading. The
form's button reads `Add widget`, with the `plus` icon drawn before the text
and hidden from assistive technology, so the button's accessible text is
`Add widget` alone.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `dummy` that links
`/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its
icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport. Its
banner holds the mark, whose text is `ikigenba` and which names the service
`dummy`; the profile link, labelled `Profile` and titled `mg@example.com`,
leading to `http://localhost:3001/`; and the sign-out button reading `Sign
out` in a form that POSTs to `http://localhost:3001/logout`. Beneath the
banner is the heading `Widgets` with the subtitle `3 widgets · refreshes every
5 seconds`, and beneath it a table whose header cells read `Name`, `Count`,
and `Status` and whose rows are the three widgets in the order they were
created, with their counts and their statuses as words: `alpha` 3 `active`,
`beta` 0 `paused`, `gamma` 12 `retired`. The `Count` header cell and each
count cell are marked numeric, and each status word is inside a status marker
naming that status. Beside the table is the card headed `Add widget` holding
the form that creates a widget, with a field for each of a widget's three
fields and a button reading `Add widget` behind its hidden `plus` icon; in a
browser, that button, like `Sign out`, visibly reacts as the user presses it.
The banner holds no launcher button, and the page loads no
`/_appkit/launcher.js`, since dummy has no services file. Last on the page is
the footer reading `dummy <display>`, where `<display>` is what
`dummy --version` prints under the same environment. Outside the banner, the address
`mg@example.com` is not in the page's visible text. Outside the banner and the
footer, the text `Dummy` appears nowhere.

Preconditions:

- dummy is serving, started with `IKIGENBA_SERVICES` unset.
- The database holds exactly these widgets, in creation order: `alpha` count 3
  status `active`, `beta` count 0 status `paused`, `gamma` count 12 status
  `retired`.

Postconditions:

- Nothing has changed.

## A user opens the panel before any widget exists

A database dummy has just created holds no widgets, so this is the panel a
user first sees on a new host or a developer's fresh working directory. The
page is the same page with nothing in the table: the header row is still
there, so the reader sees what a widget will show, and the form is there to
create the first one.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`, except
that the subtitle reads `0 widgets · refreshes every 5 seconds` and the
table holds its header row, with cells reading `Name`, `Count`, and `Status`,
and no other row.

Preconditions:

- dummy is serving, started with `IKIGENBA_SERVICES` unset.
- The database holds no widgets: no widget has been created since dummy
  created it.

Postconditions:

- Nothing has changed. The database still holds no widgets.

## A user asks for the service root

Nothing lives at the root; the panel is where a caller who typed the bare
host is meant to land, and the root exists only to send them there.

Request:

```
GET / HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 303 See Other
Location: /widgets
```

Status 303. The body is empty.

Preconditions:

- dummy is serving.

Postconditions:

- Nothing has changed.

## A user's client asks for the panel's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike,
with no body, on every route dummy answers itself; the shared files under
`/_appkit/` answer it as `S8` tells, and `/mcp` as `S9-mcp.md` tells. The
panel is what a monitor or a proxy reaches for when it wants
to know dummy is up without paying for the page.

Request:

```
HEAD /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is empty.

Preconditions:

- dummy is serving.

Postconditions:

- Nothing has changed.

## A user on a space is offered sign-out from that space

The sign-out form and the profile link address auth on the space the request
names in its `Host`, never a fixed host, so one dummy build signs a caller out
of, and sends them to their profile on, whichever space it is serving. A developer shows the space's headers by hand.

Request:

```
GET /widgets HTTP/1.1
Host: dummy.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /widgets HTTP/1.1
Host: dummy.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /widgets HTTP/1.1
Host: dummy.sbx.ikigenba.dev
X-Forwarded-Proto: HTTPS
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page, and its banner's sign-out button
reading `Sign out` is in a form whose method is `post` and whose action is
`https://auth.sbx.ikigenba.dev/logout`, and the banner's profile link, titled
`mg@example.com`, leads to `https://auth.sbx.ikigenba.dev/`. With
`X-Forwarded-Proto: http` and the same `Host`, the action is
`http://auth.sbx.ikigenba.dev/logout` and the profile link leads to
`http://auth.sbx.ikigenba.dev/`. There is no launcher here.

Preconditions:

- dummy is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- dummy set no cookie.

## An operator finds a user's visit to the panel in dummy's trail

An operator who knows a request's id — from nginx's log, say, or from the
trail of another service the request passed through — finds in dummy's trail
what dummy did with it: who asked, for what, how dummy answered, and how
long it took. The request id is the one nginx set, carried unchanged, so the
same id finds the request in every service it touched. The developer here
stands in for nginx by sending the id by hand.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`.

Preconditions:

- dummy is serving, and telemetry takes every event.
- The database holds the widgets of `A user opens the panel`.

Postconditions:

- The widgets are unchanged.
- telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The caller's email is in neither.
- dummy wrote nothing to stderr.

## A developer's request without a request id is recorded under an id dummy gives it

A developer's request reaches dummy without nginx, so it carries no
`X-Request-Id`. dummy gives it one, in nginx's shape, so the request is as
traceable in the trail as one nginx forwarded. Each such request gets an id of
its own; an `X-Request-Id` that is present but empty is treated as absent.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`.

Preconditions:

- dummy is serving, and telemetry takes every event.
- The request carries no `X-Request-Id` header.
- The database holds the widgets of `A user opens the panel`.

Postconditions:

- The widgets are unchanged.
- telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `<request-id>` is 32 lowercase hexadecimal digits, the same in both events,
  and is drawn afresh for each request that came without one.
- dummy wrote nothing to stderr.

## A request arrives without the identity headers

In production this cannot happen from outside: the gate sets the headers on
every request it forwards, a sibling app forwards the ones it received, and
nothing but nginx and the suite's apps can reach dummy's socket. So a request
without `X-User-Id` says the gate or a sibling is misconfigured, which is dummy's fault to report, not the caller's to fix — hence a
500 and not a 400 or a 401. There is no identity to draw the banner from, so
this answer is bare text. A developer meets it by forgetting the headers,
as here.

Request:

```
GET /widgets HTTP/1.1
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`,
ending in a newline. A `HEAD` is answered with the same status and
`Content-Type` and an empty body. An `X-User-Id` header whose value is empty is answered the
same way as no header at all. Every route answers this way, the root, the
fragment, the shared files under `/_appkit/`, and the MCP endpoint `/mcp`
included; the identity check runs before dummy looks at the path or the
method, so a request with no headers is never a 303, a 404, or a 405.

Preconditions:

- dummy is serving.
- The request carries no `X-User-Id` header, or one whose value is empty.

Postconditions:

- Nothing has changed. No widget was read and none was created.
- dummy wrote nothing to stderr about the 500.
- telemetry has received the request's two events, under the id dummy gave
  the request, since it carried no `X-Request-Id` (`S2`), and with an empty
  user, since it carried no `X-User-Id`:

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"GET","path":"/widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

  `<request-id>` is the same 32 lowercase hexadecimal digits in both.

## A request from nginx arrives without the identity headers

nginx sets `X-Request-Id` on every request it forwards, so when the gate is
misconfigured and forwards a request without `X-User-Id`, dummy's trail
records the request under the id nginx gave it, and the operator who finds
the 500 in the trail can find the same request in nginx's log. The developer
here stands in for such an nginx by sending the id by hand.

Request:

```
GET /widgets HTTP/1.1
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the same one line, `identity header missing`,
ending in a newline.

Preconditions:

- dummy is serving.
- The request carries `X-Request-Id` and no `X-User-Id` header.

Postconditions:

- Nothing has changed. No widget was read and none was created.
- dummy wrote nothing to stderr about the 500.
- telemetry has received the request's two events under the id nginx gave
  it, with an empty user:

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A user opens the panel while dummy cannot reach the widgets

The panel shows the widgets, so when dummy cannot read them there is nothing
true to show. It does not draw the panel as if there were no widgets, which
would tell the user their widgets were gone; it says plainly that it cannot
reach them, quoting nothing of the database's own error, and the user may try
again later. The answer is bare text, with no banner and no footer. The
fragment (`S4`) and the form (`S5`) need the widgets too and answer the same
way; the root's redirect, the 404 and the 405 pages, the 415 a form post
that is not form-encoded gets (`S5`), and the shared files under `/_appkit/`
need no widget and are answered as usual.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
```

Status 503. The body is exactly the one line
`cannot reach the widgets; try again later`, ending in a newline. It has no
banner and no footer. A `HEAD` is answered with the same status and headers
and an empty body.

Preconditions:

- dummy is serving, and telemetry takes every event.
- dummy's database cannot be read: `state/dummy.db` has become unreadable
  since dummy opened it, the filesystem holding it failing, say.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr. telemetry has received the request's two
  events, in this order:

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":503}}
  ```

- dummy is still serving.

## A caller asks for a path that does not exist

The caller is identified, so dummy can answer with the banner and give them the
way back to the panel rather than a dead end. dummy serves nothing under
`/assets/`, so a path there, such as `/assets/theme.css`, is answered the same
way; the page's stylesheet is under `/_appkit/` (`S8`). The MCP endpoint is
`/mcp` alone (`S9-mcp.md`): `/mcp/`, or any path beneath it, is a path that
does not exist like any other.

Request:

```
GET /nope HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /assets/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /mcp/ HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is an HTML document with the same banner and footer as
the panel — the mark, the profile link titled `mg@example.com` leading to
`http://localhost:3001/`, and the sign-out button POSTing to
`http://localhost:3001/logout`, with the same title, stylesheet link, icon link,
feedback script, and viewport as every page (above) — which, between the banner and
the footer, says the page was not found and carries a link to `/widgets`.

Preconditions:

- dummy is serving.

Postconditions:

- Nothing has changed.

## A caller sends the panel a method it does not take

`Allow` names every method `/widgets` takes, whichever story owns it: `GET`
and `HEAD` read the panel, and `POST` creates a widget (`S5`). The MCP
endpoint `/mcp` refuses a method it does not take in its own way, not with
this page (`S9-mcp.md`).

Request:

```
DELETE /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD, POST
Content-Type: text/html; charset=utf-8
```

Status 405. The body is an HTML document with the same banner and footer as
the panel — the mark, the profile link titled `mg@example.com` leading to
`http://localhost:3001/`, and the sign-out button POSTing to
`http://localhost:3001/logout` — with the same title, stylesheet link, icon link,
feedback script, and viewport as every page (above), which, between the banner and
the footer, says the method is not allowed and carries a link to `/widgets`.
`PUT` and `PATCH` are refused the same way.

Preconditions:

- dummy is serving.

Postconditions:

- Nothing has changed. No widget was created.

## A user on a host with services opens the launcher

On a host, the services file lists the platform's services, and the launcher
is how a user gets from dummy to any of them without typing an address. A
developer stands in for the host by writing a services file and naming it
when serving dummy. The file here, `/tmp/services.json`, lists four
services: three with an icon, one of them switched off, and `mcp`, the
platform's MCP gateway, which has no icon and so is not in the launcher:

```
{
  "services": [
    {"name": "auth", "url": "https://auth.sbx.ikigenba.dev/", "description": "Sign in to the space", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false, "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><circle cx='12' cy='12' r='9'/></svg>"},
    {"name": "mcp", "url": "https://mcp.sbx.ikigenba.dev/", "description": "The space's MCP gateway", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false},
    {"name": "dummy", "url": "https://dummy.sbx.ikigenba.dev/", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><rect x='4' y='4' width='16' height='16'/></svg>"},
    {"name": "ledger", "url": "https://ledger.sbx.ikigenba.dev/", "description": "Ledger", "socket": "/run/ikigenba/ledger.sock", "enabled": false, "mcp": false, "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><path d='M4 20L20 4'/></svg>"}
  ]
}
```

In a browser, the list is closed when the page loads, and pressing the
launcher button opens it. What the list does as the user types is the
platform's launcher script's, not dummy's, and is meant to go like this:
typing in the search field keeps only the entries
whose name contains the typed text, ignoring case and any spaces around it;
clearing the field shows them all again. When the text matches no entry, the
no-match line appears, reading `No service matches “<text>”.` with the typed
text in quotation marks. Pressing Enter in the search field opens the first
entry still shown that is a working link, and does nothing when there is
none. That filtering is the whole of what `/_appkit/launcher.js` does: every
entry, and the no-match line, arrived with the page.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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
`ledger is unavailable`. There is no entry for `mcp`, which has no icon. The
no-match line is in the page and hidden. The page
loads the script `/_appkit/launcher.js`.

Preconditions:

- dummy is serving, started with `IKIGENBA_SERVICES=/tmp/services.json` in
  its environment.
- `/tmp/services.json` holds the file above and is readable by dummy.
- The database holds the widgets of `A user opens the panel`.

Postconditions:

- Nothing has changed. The services file is as it was.

## A user on a laptop with no services file sees no launcher

A developer's laptop has no services file, and nothing names one:
`IKIGENBA_SERVICES` is unset (`S2`). The banner is then the banner without a
launcher, and the page is otherwise the same page a host serves.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`: the banner
holds the mark, the profile link titled `mg@example.com` leading to
`http://localhost:3001/`, and the sign-out button POSTing to
`http://localhost:3001/logout`, and no launcher button. The page carries no list of services and no `Find a service` field,
and it loads no `/_appkit/launcher.js`.

Preconditions:

- dummy is serving, started with `IKIGENBA_SERVICES` unset.
- The database holds the widgets of `A user opens the panel`.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr about the launcher. With no services file it
  has no telemetry to send to (`S2`), so stderr holds the request's two
  events, `request.started` and `request.finished`, each as a
  `dummy: undelivered event: <event>` line, and nothing else for this
  request.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A
broken launcher never breaks a page, so dummy draws the page without one and
reports nothing: this is not a fault of the request, and dummy's answer is the
same as when no file is named at all. A file that exists but cannot be read,
is not a JSON object with a `services` array, or has no usable entry that
carries an icon is answered the same way.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the panel page of `A user opens the panel`, with no
launcher button, no list of services, and no `/_appkit/launcher.js`.

Preconditions:

- dummy is serving, started with `IKIGENBA_SERVICES=/tmp/services.json` in
  its environment.
- `/tmp/services.json` does not exist.
- The database holds the widgets of `A user opens the panel`.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr about the launcher. With no services file to
  read it has no telemetry to send to (`S2`), so stderr holds the request's
  two events, `request.started` and `request.finished`, each as a
  `dummy: undelivered event: <event>` line, and nothing else for this
  request.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched
on or off, and dummy reads the file afresh for every page, so the next page a
user loads shows the new list without dummy being restarted. Here the host
has switched `ledger` on since dummy started: `/tmp/services.json` is the file
of `A user on a host with services opens the launcher` with `ledger`'s
`enabled` now `true`.

Request:

```
GET /widgets HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

- dummy is serving, started with `IKIGENBA_SERVICES=/tmp/services.json` in
  its environment
  while `/tmp/services.json` listed `ledger` as switched off, and it has not
  been restarted since.
- `/tmp/services.json` now lists `ledger` with `enabled` `true`.
- The database holds the widgets of `A user opens the panel`.

Postconditions:

- Nothing has changed.
