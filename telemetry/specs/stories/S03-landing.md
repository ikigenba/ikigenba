# Stories — landing

The landing page, the about screen, and telemetry's routing outside MCP and ingest: what a running telemetry answers at every path but `/mcp` and `/ingest`, and the frame its two pages are drawn in. The landing page, at `/`, tells a person what telemetry is and names the four tools an agent reaches it with; the about screen, at `/about`, shows its name, its version, and its description. Both are server-rendered HTML, and the whole of each page's content arrives in the response body, the launcher's list included; no script adds content a user sees. A page's scripts are the platform's and only act on what the server sent: the launcher's, `/_appkit/launcher.js`, filters the launcher's list as the user types; and the platform's button feedback script, `/_appkit/feedback.js`, which every page loads with or without a launcher, makes an enabled button visibly react when the user presses it. An nginx gate in front of telemetry authenticates every request and sets `X-User-Id` and `X-User-Email` on the request it passes upstream, with the request's id in `X-Request-Id`. telemetry trusts those headers absolutely and has no unauthenticated case, so there is no sign-in page and no signed-out banner. Only nginx and the suite's own apps can reach telemetry's socket, so a request that arrives without `X-User-Id`, or with it empty, means the gate or a sibling is misconfigured — a server fault, not a bad request. The requests go to a running telemetry (`S02`), each shown as the HTTP request telemetry receives, with the headers the story depends on. A developer stands in for the gate by passing those headers by hand. telemetry is started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, and that file, the host's services file, holds the suite's services file unless a story says otherwise:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "https://dummy.sbx.ikigenba.dev", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true },
    { "name": "mcp", "url": "https://mcp.sbx.ikigenba.dev", "description": "Connect AI assistants to your services", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false },
    { "name": "telemetry", "url": "https://telemetry.sbx.ikigenba.dev", "description": "The suite's trail of events", "socket": "/run/ikigenba/telemetry.sock", "enabled": true, "mcp": true }
  ]
}
```

telemetry takes the file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows on the next page without a restart. The file is a JSON object whose `services` member is an array; each entry is an object with `name`, a non-empty string; `url`, `description`, and `socket`, strings; `enabled`, `true` or `false`, `false` for a service switched off; `mcp`, `true` or `false`; and, optionally, `icon`, a string holding the SVG text of the service's icon. Members telemetry does not know are ignored, and an entry that lacks one of the six others, or holds one of the wrong kind, is left out while the rest are still used. With no variable, no readable file, or a file that is not such an object, telemetry treats the file as listing no services and writes nothing about it, since a broken services file never breaks a page.

Both pages are drawn in the banner every app of the platform draws, at the top of the page. It holds the mark, whose text is `ikigenba` and which names the service it fronts, `telemetry`, so a browser shows `ikigenba │ telemetry`; a profile link; and a sign-out button; and, when the services file lists services that carry an icon, the launcher button (below). The service's name is lowercase `telemetry` everywhere it appears. The landing page's title, the one a browser shows on its tab, is `telemetry`, and the about screen's is `About telemetry`. The profile link is the `user-circle` icon with no text of its own, labelled `Profile` for assistive technology and titled with the caller's `X-User-Email` value, exactly as it arrived, so hovering it shows who is signed in; the email is in no visible text on the page. The sign-out button is a form, not a link: pressing it POSTs to `<auth-logout>`, and its `logout` icon is drawn before the text and hidden from assistive technology, so its accessible text is `Sign out` alone. Following the profile link or submitting the form leaves telemetry for auth; what auth does there is told in auth's own stories. telemetry serves no logout route and sets no cookie.

`<auth-profile>`, where the banner's profile link leads, is auth's profile page: the `url` of the services file's entry named `auth`, followed by `/`, so `https://auth.sbx.ikigenba.dev/` for the suite's services file; `<auth-logout>` is that same `url` followed by `/logout`. When the file has no entry named `auth`, or that entry's `url` is empty, telemetry reads the space from the request's own `Host`: a trailing port is dropped, then a single leading `telemetry.` label; what remains is the space, `<auth-profile>` is `<scheme>://auth.<space>/`, and `<auth-logout>` is `<scheme>://auth.<space>/logout`. The scheme is the request's `X-Forwarded-Proto` when that header is exactly `http` or exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty value, or no header at all. A `Host` with no `telemetry.` label is the space whole. There is no fixed fallback address.

The launcher is the banner's way to the platform's other services, and it is the same launcher every app draws. It offers only the services file's entries that carry an icon: an entry with no `icon`, or one that is not a string, stays out of it, and with no such entry the page has no launcher and is otherwise the same page. When the launcher is there, the banner holds a launcher button labelled `Services`; pressing it opens the list, which is closed when the page loads. The list holds a search field labelled `Find a service`, with the placeholder `Find a service`, and one entry per service that carries an icon, in the file's order, each showing the service's icon and then its name. An enabled service's entry is a link to its `url`; a service switched off keeps its place but is not a working link, and is titled `<name> is unavailable`. telemetry's own entry, the one named `telemetry`, is marked as the current page. The list, the search field, and a hidden no-match line are all in the page as served; a page loads `/_appkit/launcher.js` only when the launcher is there.

Each page links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport, so a browser shows the suite's favicon on the page's tab and a phone shows the page at the phone's own width. The stylesheet, its fonts, the favicon, the launcher's script, and the button feedback script are the platform's shared files, which telemetry serves under `/_appkit/` (`S04`); a page makes no request to any third party. Last on each page is the footer, whose text is `telemetry <display>`: the service's name, one space, and `<display>`, the string `telemetry --version` prints under the environment telemetry was started with (`S01`), exactly as it prints it, so a user can tell which code is serving the page. No story fixes its value. When that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`, `<display>` is empty and the footer's text is `telemetry` and the one space.

The landing page and the about screen are telemetry's only HTML. Every other answer in this group is bare: the missing-header 500 and the 404 are one line of plain text, and the 405 has an empty body; none has a banner or a footer. telemetry writes nothing to stderr for any answer in this group, the missing-header 500 included: every request it answers here adds exactly two events to its own trail (`S02`), `request.started` with its method and path as it arrives and `request.finished` with the status telemetry answered once the answer is complete, and serving a page reads the trail for nothing and contacts no sibling.

The routes are `/`, the landing page, and `/about`, the about screen, each taking `GET` and `HEAD`; `/_appkit/<name>`, the platform's shared files (`S04`); `/mcp`, exactly that path, the MCP endpoint, whose answers, to every method, are `S05`'s and never one of the answers below; and `/ingest`, exactly that path, where siblings post their events, whose answers, to every method, are `S06`'s and never one of the answers below. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the landing page

A user who lands on telemetry's name, from the launcher or by typing it, learns what the service is and how an agent uses it. Everything a person looking at a browser sees is in the document that arrives, so a reader who fetches the page with `curl` has it all. The page shows no records: the trail is read through the tools, by an agent, and the page says which tools those are.

Request:

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `telemetry` that links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport. Its banner holds the mark, whose text is `ikigenba` and which names the service `telemetry`; the profile link, labelled `Profile` and titled `mg@example.com`, leading to `https://auth.sbx.ikigenba.dev/`; and the sign-out button reading `Sign out` in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`; in a browser, that button visibly reacts as the user presses it. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`, since no entry in the file carries an icon. Beneath the banner is the page's top-level heading, `telemetry`, then the summary `The suite's trail of events. Every service on this host records what it does here: each request it serves, each call it makes to a sibling, each MCP tool it runs, and the events of its own domain.` Below that is the section headed `MCP tools`, reading `Agents search the trail with these tools, through the MCP gateway's call.`, and a list of exactly four tools, in this order, each named and followed by its description: `catalog`, `The services, the events each records, and the attribute keys each carries.`; `search`, `The records that match a filter, newest first.`; `count`, `How many records match a filter, grouped by a field or a time bucket.`; and `trace`, `Every record of one request, from every service it touched, oldest first.` After the section is the link `About telemetry`, leading to `/about`. Last on the page is the footer reading `telemetry <display>`, where `<display>` is what `telemetry --version` prints under the same environment. The address `mg@example.com` is not in the page's visible text. The text `Telemetry` appears nowhere.

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file and is readable by telemetry.

Postconditions:

- Nothing has changed but the trail. The services file is as it was, and no record of the trail was read.
- telemetry wrote nothing to stderr and set no cookie.
- The trail holds two events for the request, both under user `u_7f3a9c21` and a request id telemetry made up for it, 32 lowercase hexadecimal characters, since the request carried no `X-Request-Id`:

  ```
  request.started method=GET path=/
  request.finished status=200
  ```

## A user's client asks for the landing page's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body. The page is what a monitor or a proxy reaches for when it wants to know telemetry is up without paying for the page. `/about` answers `HEAD` the same way.

Request:

```
HEAD / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Forwarded-Proto: https
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

- telemetry is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed but the trail.

## A user opens the about screen

The about screen is where a user checks which code is serving and what the service calls itself. Its three facts are telemetry's own: the name, `<display>`, the string `telemetry --version` prints under the environment telemetry was started with, and the description the manifest declares (`S01`), the same line the host publishes in its services file. The screen reads none of them from the services file, so it says the same with or without one.

Request:

```
GET /about HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `About telemetry`, with the same stylesheet link, icon link, feedback script, viewport, banner, and footer as the landing page of `A user opens the landing page`. Beneath the banner is the page's top-level heading, `About telemetry`, then a list of exactly three facts, in this order, each a label and its value: `Name`, `telemetry`; `Version`, `<display>`, where `<display>` is what `telemetry --version` prints under the same environment and the footer shows, empty when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; and `Description`, `The suite's trail of events`, the manifest's description. After the list is the link `Back to telemetry`, leading to `/`.

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr and set no cookie.
- The trail holds two events for the request, under user `u_7f3a9c21` and a request id telemetry made up for it:

  ```
  request.started method=GET path=/about
  request.finished status=200
  ```

## A user on a host with services opens the launcher

On a host, the services file carries the icon of every service whose package ships one, and the launcher is how a user gets from telemetry to any of them without typing an address. Here `/run/ikigenba/services.json` is the suite's services file with an `icon` on each of its four entries, `auth`, `dummy`, `mcp`, and `telemetry`, each holding the SVG text of that service's icon; telemetry's own is the one its package ships, `share/icon.svg` (`S13`). The launcher lists every service that carries an icon, whether or not it is an MCP service.

In a browser, the list is closed when the page loads, and pressing the launcher button opens it; like `Sign out`, the button visibly reacts as the user presses it. Typing in the search field keeps only the entries whose name contains the typed text, ignoring case and any spaces around it; clearing the field shows them all again. When the text matches no entry, the no-match line appears, reading `No service matches “<text>”.` with the typed text in quotation marks. Pressing Enter in the search field opens the first entry still shown that is a working link, and does nothing when there is none. That filtering is the whole of what `/_appkit/launcher.js` does: every entry, and the no-match line, arrived with the page.

Request:

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, with the same banner, and the banner also holds the launcher button labelled `Services`. The page carries the list of services labelled `Services`, holding the search field labelled `Find a service` with the placeholder `Find a service` and four entries in the file's order, each showing its icon and then its name: `auth`, a link to `https://auth.sbx.ikigenba.dev`; `dummy`, a link to `https://dummy.sbx.ikigenba.dev`; `mcp`, a link to `https://mcp.sbx.ikigenba.dev`; and `telemetry`, a link to `https://telemetry.sbx.ikigenba.dev`, marked as the current page. The no-match line is in the page and hidden. The page loads the script `/_appkit/launcher.js`. The about screen carries the same launcher.

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file with an `icon` string on each entry.

Postconditions:

- Nothing has changed but the trail. The services file is as it was.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A broken services file never breaks a page, so telemetry draws the page without a launcher and reports nothing: this is not a fault of the request, and the answer is the same as when no file is named at all. A file that exists but cannot be read, is not a JSON object with a `services` array, or has no usable entry that carries an icon is answered the same way. With no file there is no `auth` entry either, so auth's links are read from the request's `Host`, which here names the same space.

Request:

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`: the profile link leading to `https://auth.sbx.ikigenba.dev/`, the sign-out button POSTing to `https://auth.sbx.ikigenba.dev/logout`, no launcher button, no list of services, no `Find a service` field, no no-match line, and no `/_appkit/launcher.js`.

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` does not exist.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr about the services file. Its own events need no services file (`S02`), so the trail holds the request's two events as it holds every page's, `request.started` with `method=GET path=/` and `request.finished` with `status=200`, under user `u_7f3a9c21` and a request id telemetry made up for it.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched on or off, and telemetry reads the file afresh for every page, so the next page a user loads shows the new list without telemetry being restarted. Here the host has switched `dummy` off since telemetry started: `/run/ikigenba/services.json` is the file of `A user on a host with services opens the launcher` with `dummy`'s `enabled` now `false`.

Request:

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page with the launcher, as in `A user on a host with services opens the launcher`, except that the `dummy` entry is not a working link and is titled `dummy is unavailable`; it keeps its place and still shows its icon and name.

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment while `/run/ikigenba/services.json` held the file of `A user on a host with services opens the launcher`, with `dummy` switched on, and it has not been restarted since.
- `/run/ikigenba/services.json` now lists `dummy` with `enabled` `false`.

Postconditions:

- Nothing has changed but the trail.

## A user on a host whose services file names no auth still gets auth's links

The banner's profile link and sign-out form address auth on the space the request names in its `Host` when the services file has no `auth` entry to take them from, so one telemetry build serves whichever space it is installed on. Here `/run/ikigenba/services.json` is the suite's services file without the `auth` entry.

Request:

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Forwarded-Proto: HTTPS
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, and for every form above its banner's profile link, titled `mg@example.com`, leads to `https://auth.sbx.ikigenba.dev/`, and the sign-out button reading `Sign out` is in a form whose method is `post` and whose action is `https://auth.sbx.ikigenba.dev/logout`. With `X-Forwarded-Proto: http` and `Host: telemetry.sbx.ikigenba.dev`, the link leads to `http://auth.sbx.ikigenba.dev/` and the form's action is `http://auth.sbx.ikigenba.dev/logout`. The about screen's banner is built the same way.

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file without the entry named `auth`.

Postconditions:

- Nothing has changed but the trail.
- telemetry set no cookie.

## A request arrives without the identity headers

In production this cannot happen from outside: the gate sets the headers on every request it forwards, a sibling forwards the ones it received, and nothing but nginx and the suite's apps can reach telemetry's socket. So a request without `X-User-Id` says the gate or a sibling is misconfigured, which is telemetry's fault to report, not the caller's to fix — hence a 500 and not a 400 or a 401. A developer meets it by forgetting the headers, as here. The one route that takes no identity is `/ingest` (`S06`), where a sibling's writer, not a caller, posts.

Request:

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`, ending in a newline. A `HEAD` is answered with the same status and headers and an empty body. An `X-User-Id` header whose value is empty is answered the same way as no header at all. Every path but `/ingest` answers this way — `/`, `/about`, the shared files under `/_appkit/` (`S04`), the MCP endpoint `/mcp` (`S05`), and every path that does not exist; the identity check runs before telemetry looks at the path or the method, so a request with no identity is never a 404 or a 405.

Preconditions:

- telemetry is serving.
- The request carries no `X-User-Id` header, or one whose value is empty.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr. The 500 is a handled failure, so it is in the trail and not the journal: the trail holds two events for the request, with an empty user and a request id telemetry made up for it, 32 lowercase hexadecimal characters, because it carried no `X-Request-Id`:

  ```
  request.started method=GET path=/
  request.finished status=500
  ```

## A request from nginx arrives without the identity headers

nginx sets `X-Request-Id` on every request it forwards, so when the gate is misconfigured and forwards a request without `X-User-Id`, telemetry records the 500 under the id nginx gave the request, and the operator who finds the 500 in the trail can find the same request in nginx's log. The developer here stands in for such an nginx by sending the id by hand.

Request:

```
GET / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the same one line, `identity header missing`, ending in a newline.

Preconditions:

- telemetry is serving.
- The request carries `X-Request-Id` and no `X-User-Id` header.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr. The trail holds two events for the request, under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user, so `trace` with that id finds them (`S11`):

  ```
  request.started method=GET path=/
  request.finished status=500
  ```

## A caller asks for a path that does not exist

telemetry serves the landing page, the about screen, the shared files, the MCP endpoint, and ingest, and nothing else; it has no page of its own to send a lost caller to, so a path it does not serve is answered with one line of plain text, whatever the method. The MCP endpoint is `/mcp` alone (`S05`) and ingest is `/ingest` alone (`S06`): `/mcp/`, `/ingest/`, or any path beneath either is a path that does not exist like any other. telemetry serves nothing under `/assets/`; the pages' stylesheet is under `/_appkit/` (`S04`).

Request:

```
GET /nope HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /assets/theme.css HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /mcp/ HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
POST /nope HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is exactly the one line `not found`, ending in a newline. It has no banner and no footer.

Preconditions:

- telemetry is serving.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr. The trail holds the request's two events, the second with `status=404`.

## A caller sends a page a method it does not take

The pages only read, so `/` and `/about` take `GET` and `HEAD` and nothing else, and `Allow` names those two. No page is sent. The MCP endpoint and ingest refuse a method they do not take in their own way, each with its own `Allow` (`S05`, `S06`).

Request:

```
POST / HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /about HTTP/1.1
Host: telemetry.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. `PUT`, `PATCH`, and every other method but `GET` and `HEAD` are refused the same way, on both pages.

Preconditions:

- telemetry is serving.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr. The trail holds the request's two events, the second with `status=405`.
