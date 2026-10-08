# Stories — landing

The landing page, the about screen, and the frame events' pages are drawn in: what a running events answers at `/` and `/about`, and the rules every route of events shares. The landing page, at `/`, tells a signed-in user what events is, lists its subscribers with where each is in the log and how far behind, and names the five tools an agent inspects the bus and unsticks subscribers with; the about screen, at `/about`, shows events' name, its version, and its description. Both are server-rendered HTML, and the whole of each page's content arrives in the response body, the launcher's list included; neither page carries a script of its own, and no script adds content a user sees: the launcher's, `/_appkit/launcher.js`, which a page loads only with the launcher, only filters the launcher's list as the user types; and the platform's button feedback script, `/_appkit/feedback.js`, which every page loads with or without a launcher, makes an enabled button visibly react when the user presses it. Every page of events is for a signed-in user, and events serves nothing to guests: its manifest declares no `guests`, so on a host with an authenticator nginx sends a visitor with no credential to auth's sign-in before the request reaches events. Every signed-in user sees the same pages: events shows each user the whole bus. nginx sets `X-User-Id` and `X-User-Email` on every request it passes upstream, the caller auth authenticated, with the request's id in `X-Request-Id`, and events trusts those headers (`S02`). So a request that arrives without `X-User-Id`, or with it empty, means nginx or a sibling is misconfigured — a server fault, not a bad request — and every route of events but `/emit` answers it the same way, before it looks at the path or the method (below). The requests go to a running events (`S02`), each shown as the HTTP request events receives, with the headers the story depends on; a developer stands in for nginx by passing those headers by hand. events is started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, and that file, the host's services file, holds the suite's services file unless a story says otherwise:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "https://dummy.sbx.ikigenba.dev", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true },
    { "name": "events", "url": "https://events.sbx.ikigenba.dev", "description": "The suite's internal event bus", "socket": "/run/ikigenba/events.sock", "enabled": true, "mcp": true },
    { "name": "mcp", "url": "https://mcp.sbx.ikigenba.dev", "description": "Connect AI assistants to your services", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false },
    { "name": "repos", "url": "https://repos.sbx.ikigenba.dev", "description": "Git repositories for the suite's content", "socket": "/run/ikigenba/repos.sock", "enabled": true, "mcp": true },
    { "name": "scripts", "url": "https://scripts.sbx.ikigenba.dev", "description": "Python scripts run from the suite's repositories", "socket": "/run/ikigenba/scripts.sock", "enabled": true, "mcp": true },
    { "name": "sites", "url": "https://sites.sbx.ikigenba.dev", "description": "Static sites from the suite's repositories", "socket": "/run/ikigenba/sites.sock", "enabled": true, "mcp": true },
    { "name": "telemetry", "url": "https://telemetry.sbx.ikigenba.dev", "description": "The suite's trail of events", "socket": "/run/ikigenba/telemetry.sock", "enabled": true, "mcp": true }
  ]
}
```

events takes the file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows on the next page without a restart. The file is a JSON object whose `services` member is an array; each entry is an object with `name`, a non-empty string; `url`, `description`, and `socket`, strings; `enabled`, `true` or `false`, `false` for a service switched off; `mcp`, `true` or `false`; and, optionally, `icon`, a string holding the SVG text of the service's icon. Members events does not know are ignored, and an entry that lacks one of the six others, or holds one of the wrong kind, is left out while the rest are still used. With no variable, no readable file, or a file that is not such an object, events treats the file as listing no services and writes nothing about it, since a broken services file never breaks a page. The same file names the telemetry service events sends its trail to (`S02`); in this group telemetry takes every event unless a story says otherwise.

Both pages are drawn in the banner every app of the platform draws, at the top of the page. It holds the mark, the launcher button, a profile link, and a sign-out icon button, in that order; the launcher button is there only when the services file lists services that carry an icon (below), immediately after the mark. The mark shows the platform's favicon, then the product name `Ikigenba`, then the service it fronts, `events`: events' own icon and then its name when the services file lists `events` with an icon, and the name alone otherwise. The favicon in the mark is decoration and has no text of its own. The service's name is lowercase `events` everywhere it appears. The landing page's title, the one a browser shows on its tab, is `events`, and the about screen's is `About events`. Neither page has a breadcrumb. The profile link is the `user-circle` icon with no text of its own, labelled `Profile` for assistive technology and titled with the caller's `X-User-Email` value, exactly as it arrived, so hovering it shows who is signed in; the email is in no visible text on the page. The sign-out button is a form, not a link: pressing it POSTs to `<auth-logout>`. Like the profile link, it shows the `logout` icon and no text of its own: it is labelled `Sign out` for assistive technology and titled `Sign out`, which a browser shows as its tooltip. Following the profile link or submitting the form leaves events for auth; what auth does there is auth's own. events serves no logout route, and no page of events sets a cookie.

`<auth-profile>`, where the banner's profile link leads, is auth's profile page: the `url` of the services file's entry named `auth`, followed by `/`, so `https://auth.sbx.ikigenba.dev/` for the suite's services file; `<auth-logout>` is that same `url` followed by `/logout`. When the file has no entry named `auth`, or that entry's `url` is empty, events reads the space from the request's own `Host`: a trailing port is dropped, then a single leading `events.` label; what remains is the space, `<auth-profile>` is `<scheme>://auth.<space>/`, and `<auth-logout>` is `<scheme>://auth.<space>/logout`. The scheme is the request's `X-Forwarded-Proto` when that header is exactly `http` or exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty value, or no header at all. A `Host` with no `events.` label is the space whole. There is no fixed fallback address.

The launcher is the banner's way to the platform's other services, and it is the same launcher every app draws. It offers only the services file's entries that carry an icon: an entry with no `icon`, or one that is not a string, stays out of it, and with no such entry the page has no launcher and is otherwise the same page. When the launcher is there, the banner holds a launcher button labelled `Services`; pressing it opens the list, which is closed when the page loads. The list holds a search field labelled `Find a service`, with the placeholder `Find a service`, and one entry per service that carries an icon, in the file's order, each showing the service's icon and then its name. An enabled service's entry is a link to its `url`; a service switched off keeps its place but is not a working link, and is titled `<name> is unavailable`. events' own entry, the one named `events`, is marked as the current page. The list, the search field, and a hidden no-match line are all in the page as served; a page loads `/_appkit/launcher.js` only when the launcher is there.

Each page links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport, so a phone shows it at the phone's own width. The favicon is the platform's one icon, which a browser shows in the page's tab. The stylesheet, its fonts, the launcher's script, the button feedback script, and the favicon are the platform's shared files, which events serves under `/_appkit/` (`S04`); a page makes no request to any third party. Last on each page is the footer, whose text is `events <display>`: the service's name, one space, and `<display>`, the string `events --version` prints under the environment events was started with (`S01`), exactly as it prints it, so a user can tell which code is serving the page. No story fixes its value. When that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`, `<display>` is empty and the footer's text is `events` and the one space.

The landing page shows the same subscribers `subscribers` answers (`S10`), one row per subscriber, sorted by service name, whatever the caller. Each row, `tr[data-subscriber=<service>]`, holds the subscriber's service name (`td.subscriber-service`); its status (`td.subscriber-status`) as a `span.status` whose text is the status, whose `data-status` is the status and whose `data-kind` is the kind it shows as — `ok` shows as `ok`, `paused` as `warn`, and `gone` as `info`; its cursor (`td.subscriber-cursor`), the `seq` of the last event it finished; its lag (`td.subscriber-lag`), how many accepted events it has not finished; and its since (`td.subscriber-since`), when its current status began, the moment `subscribers` gives as its `since`, as a `time` whose text is that moment in UTC to the minute, `2026-10-05 09:14`, and whose `datetime` is the same moment in RFC 3339 UTC. A paused subscriber's status is followed, in the same cell, by muted text (`span.muted`) naming the event it is stuck on and the error it answered, as `subscribers` gives them in its `reason`, without the event's id: `at <name> <seq>: <error>`, where `<name>` is the event's name, `<seq>` its `seq`, and `<error>` the error text exactly as the subscriber answered it. The stories share this state of the log, which the landing page never writes: the last event accepted has `seq` 18204; `scripts` is a subscriber, `ok`, with cursor 18204 and since `2026-10-05T09:14:00Z`; and `sites` is a subscriber, `paused` on the event with `seq` 18177, a `repo.pushed`, whose last delivery `sites` answered with the error `publish failed: commit not found`, with cursor 18176 and since `2026-10-05T08:52:00Z`.

The routes events answers are `/`, the landing page, and `/about`, the about screen, each taking `GET` and `HEAD`; `/_appkit/<name>`, the platform's shared files (`S04`); `/mcp`, exactly that path, the MCP endpoint, whose answers, to every method, are `S05`'s; and `/emit`, exactly that path, where siblings emit their events on the socket, whose answers, to every method, are `S07`'s and never one of the answers below. Every other path, whatever the method, is answered with events' not-found page: an HTML document, `Content-Type: text/html; charset=utf-8`, titled `Not found`, that links `/_appkit/theme.css` and loads `/_appkit/feedback.js` and no other script, whose heading is `Not found` and whose text (`p#notfound`) is `There is nothing at this address.`, with the footer `events <display>` and no banner, answered with status `404`. A page that cannot be drawn because events cannot read its log is answered with events' unavailable page instead: an HTML document, `Content-Type: text/html; charset=utf-8`, titled `Page unavailable`, that links `/_appkit/theme.css` and loads `/_appkit/feedback.js` and no other script, whose heading is `Page unavailable` and whose text (`p#unavailable`) is `This page is not available right now. Try again in a moment.`, with the footer `events <display>` and no banner, answered with status `503`. Every request events answers here adds exactly two events to its trail (`S02`): `request.started`, with the `method` and the `path` as the request arrived, and `request.finished`, once the answer is complete, with the `status`, `duration_us`, `request_bytes`, and `response_bytes`, under the request's id and the caller's user. While telemetry takes every event, events writes nothing to stderr for any answer in this group, the missing-header 500 included. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the landing page

A user who lands on events' name, from the launcher or by typing it, learns what the service is, which services it delivers to, where each one is in the log, which one is stuck and on what, and which tools an agent reaches the bus with. Everything a person looking at a browser sees is in the document that arrives, so a reader who fetches the page with `curl` has it all. The page does not stream; reloading it shows the subscribers as they are then.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `events` that links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport. Its banner holds the mark, showing the platform's favicon, the text `Ikigenba`, and the service's name `events` with no icon, since no entry in the file carries an icon; the profile link, labelled `Profile` and titled `mg@example.com`, leading to `https://auth.sbx.ikigenba.dev/`; and the sign-out button, labelled and titled `Sign out`, in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`; in a browser, that button visibly reacts as the user presses it. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`, since no entry in the file carries an icon. Beneath the banner is the page's top-level heading (`h1`), `events`, then the summary (`p#summary`) `The suite's internal event bus: services emit events here, and each event is delivered, in order, to every service that accepts it.`

Below that is the section `section#subscribers`, headed `Subscribers`, reading `Each service that accepts events, where it is in the log, and how far behind.` Under it is the table `table#subscriber-list`, whose columns are headed `Service`, `Status`, `Cursor`, `Lag`, and `Since`, with exactly two rows, in this order:

- `tr[data-subscriber=scripts]`: the service `scripts`; the status `ok` (`span.status[data-status=ok][data-kind=ok]`), with no muted text after it; the cursor `18204`; the lag `0`; and the since `2026-10-05 09:14`.
- `tr[data-subscriber=sites]`: the service `sites`; the status `paused` (`span.status[data-status=paused][data-kind=warn]`), followed by the muted text `at repo.pushed 18177: publish failed: commit not found`; the cursor `18176`; the lag `28`; and the since `2026-10-05 08:52`.

The page has no `div#no-subscribers`. Below that is the section headed `MCP tools`, reading `Agents inspect the bus and unstick subscribers with these tools, through the MCP gateway's call and mutate.`, with `call` and `mutate` each in a code element, and the list `dl#tools` of exactly five tools, in this order, each a `dt[data-tool=<name>]` holding the tool's name in a code element, followed by a `dd` holding the first line of its description (`S05`): `catalog`, `Every event the suite emits, who emits and accepts it, counts and last seen.`; `search`, `The retained log, newest first, filtered by service, event, user, request id, cause or attributes.`; `subscribers`, `Each subscriber's status, reason, cursor and lag.`; `skip`, `Skip the event a paused subscriber is stuck on and resume it.`; and `resume`, `Retry the event a paused subscriber is stuck on.` After the section is the link `a#about-link`, `About events`, leading to `/about`. Last on the page is the footer reading `events <display>`, where `<display>` is what `events --version` prints under the same environment. The address `mg@example.com` is not in the page's visible text.

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file and is readable by events, and telemetry takes every event.
- The log is in the state this group shares (above).

Postconditions:

- Nothing has changed. The log was read and not written; no subscriber's cursor or status moved, and no delivery was made or held up by the page.
- events wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id events gave the request, 32 lowercase hexadecimal characters, since the request carried no `X-Request-Id`:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `response_bytes` is the length of the page's body. The caller's email is in neither.

## A user opens the landing page before any service accepts events

Until a service that accepts events has been seen, events has no subscriber, and the page says so in the place the table would be, and still names the tools.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, but the section `section#subscribers` holds, after its heading `Subscribers` and the line beneath it, the empty state `div#no-subscribers`, headed `No subscribers yet` and reading `A service that accepts events shows up here once it declares an event it accepts.`, and no `table#subscriber-list`. The `MCP tools` section, its five tools, and the link `About events` are as on that page.

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, and that file holds the suite's services file.
- events records no subscriber (`S10`).

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user's client asks for the landing page's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body. A monitor or a proxy learns events is up without paying for the page. `/about` answers `HEAD` the same way.

Request:

```
HEAD / HTTP/1.1
Host: events.sbx.ikigenba.dev
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

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.
- The log is in the state this group shares.

Postconditions:

- Nothing has changed.
- The request's `request.started` has `method` `HEAD`, and its `request.finished` has `status` 200 and `response_bytes` 0, since no body was written.

## A user opens the about screen

The about screen is where a user checks which code is serving and what the service calls itself. Its three facts are the running events' own: the name, `<display>`, the string `events --version` prints under the environment events was started with (`S01`), and the description the manifest declares, the same line the host publishes in its services file. The screen reads none of them from the services file, so it says the same with or without one, and it reads nothing of the log.

Request:

```
GET /about HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `About events`, with the same stylesheet link, favicon link, feedback script, viewport, banner, and footer as the landing page of `A user opens the landing page`, and no breadcrumb. Beneath the banner is the page's top-level heading, `About events`, then the list `dl#about` of exactly three facts, in this order, each a label and its value: `Name`, `events` (`dd#about-name`); `Version`, `<display>` (`dd#about-version`), where `<display>` is what `events --version` prints under the same environment and the footer shows, and the value is empty when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; and `Description`, `The suite's internal event bus` (`dd#about-description`), the manifest's description exactly as `events manifest` prints it. After the list is the link `a#home-link`, `Back to events`, leading to `/`. The about screen has no `Subscribers` or `MCP tools` section.

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file, and telemetry takes every event.

Postconditions:

- Nothing has changed. The log was not read.
- events wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id events gave the request:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/about"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A visitor on a space without a credential is sent to sign in

A browser with no session asks a space for events' landing page. events serves no guests, so the space's nginx asks auth first, and on auth's refusal sends the browser to auth's sign-in with the address it asked for to come back to; the request never reaches events. Every page of events is answered this way, each with its own address to return to.

Request:

```
$ curl -si https://events.sbx.ikigenba.dev/
```

```
$ curl -si https://events.sbx.ikigenba.dev/about
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https://events.sbx.ikigenba.dev/
```

Status 302. For the second form the `location` ends `?return=https://events.sbx.ikigenba.dev/about`. No story fixes the body.

Preconditions:

- events is deployed and active on `sbx.ikigenba.dev`, and so is auth (`S17`).
- The request carries no credential.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/events.sock`: events recorded no event and wrote nothing to stderr.

## A user on a host with services opens the launcher

On a host, the services file carries the icon of every service whose package ships one, and the launcher is how a user gets from events to any of them without typing an address. Here `/run/ikigenba/services.json` is the suite's services file with an `icon` on each of its eight entries, `auth`, `dummy`, `events`, `mcp`, `repos`, `scripts`, `sites`, and `telemetry`, each holding the SVG text of that service's icon; events' own is the one its package ships, `share/icon.svg` (`S16`). The launcher lists every service that carries an icon, whether or not it is an MCP service.

In a browser, the list is closed when the page loads, and pressing the launcher button opens it. Typing in the search field keeps only the entries whose name contains the typed text, ignoring case and any spaces around it; clearing the field shows them all again. When the text matches no entry, the no-match line appears, reading `No service matches “<text>”.` with the typed text in quotation marks. Pressing Enter in the search field opens the first entry still shown that is a working link, and does nothing when there is none. That filtering is the whole of what `/_appkit/launcher.js` does: every entry, and the no-match line, arrived with the page.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, with the same banner, except that its mark shows events' icon before the name `events`, and the banner also holds the launcher button labelled `Services`, immediately after the mark. The page carries the list of services labelled `Services`, holding the search field labelled `Find a service` with the placeholder `Find a service` and eight entries in the file's order, each showing its icon and then its name: `auth`, a link to `https://auth.sbx.ikigenba.dev`; `dummy`, a link to `https://dummy.sbx.ikigenba.dev`; `events`, a link to `https://events.sbx.ikigenba.dev`, marked as the current page; `mcp`, a link to `https://mcp.sbx.ikigenba.dev`; `repos`, a link to `https://repos.sbx.ikigenba.dev`; `scripts`, a link to `https://scripts.sbx.ikigenba.dev`; `sites`, a link to `https://sites.sbx.ikigenba.dev`; and `telemetry`, a link to `https://telemetry.sbx.ikigenba.dev`. The no-match line is in the page and hidden. The page loads the script `/_appkit/launcher.js`. The about screen carries the same launcher.

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file with an `icon` string on each entry.
- The log is in the state this group shares.

Postconditions:

- Nothing has changed. The services file is as it was.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A broken services file never breaks a page, so events draws the page without a launcher and reports nothing about the file: this is not a fault of the request, and the answer is the same as when no file is named at all. A file that exists but cannot be read, is not a JSON object with a `services` array, or has no usable entry that carries an icon is answered the same way, as far as the launcher goes. With no file there is no `auth` entry either, so auth's links are read from the request's `Host` and `X-Forwarded-Proto`, which here name the same space and scheme, so the page reads as it does with the file. The subscribers are the log's, so the table is the same too.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`: the profile link leading to `https://auth.sbx.ikigenba.dev/`, the sign-out button POSTing to `https://auth.sbx.ikigenba.dev/logout`, the same two subscribers in `table#subscriber-list`, no launcher button, no list of services, no `Find a service` field, no no-match line, and no `/_appkit/launcher.js`.

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` does not exist.
- The log is in the state this group shares.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr about the services file. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as an `events: undelivered event: <event>` line, and nothing else for this request.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched on or off, and events reads the file afresh for every page, so the next page a user loads shows the new list without events being restarted. Here the host has switched `dummy` off since events started: `/run/ikigenba/services.json` is the file of `A user on a host with services opens the launcher` with `dummy`'s `enabled` now `false`.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
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

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment while `/run/ikigenba/services.json` held the file of `A user on a host with services opens the launcher`, with `dummy` switched on, and it has not been restarted since.
- `/run/ikigenba/services.json` now lists `dummy` with `enabled` `false`.
- The log is in the state this group shares.

Postconditions:

- Nothing has changed.

## A user on a host whose services file names no auth still gets auth's links

The banner's profile link and sign-out form address auth on the space the request names in its `Host` when the services file has no `auth` entry to take them from, so one events build serves whichever space it is installed on. Here `/run/ikigenba/services.json` is the suite's services file without the `auth` entry.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
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

Status 200. The body is the landing page of `A user opens the landing page`, and for every form above its banner's profile link, titled `mg@example.com`, leads to `https://auth.sbx.ikigenba.dev/`, and the sign-out button labelled `Sign out` is in a form whose method is `post` and whose action is `https://auth.sbx.ikigenba.dev/logout`. With `X-Forwarded-Proto: http` and `Host: events.sbx.ikigenba.dev`, the link leads to `http://auth.sbx.ikigenba.dev/` and the form's action is `http://auth.sbx.ikigenba.dev/logout`. The about screen's banner is built the same way.

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file without the entry named `auth`.
- The log is in the state this group shares.

Postconditions:

- Nothing has changed.
- events set no cookie.

## A request arrives without the identity headers

In production this cannot happen from outside: nginx sets the headers on every request it forwards, a sibling forwards the ones it received, and nothing but nginx and the suite's apps can reach events' socket. So a request without `X-User-Id` says nginx or a sibling is misconfigured, which is events' fault to report, not the caller's to fix — hence a 500 and not a 400 or a 401. A developer meets it by forgetting the headers, as here. The one route that takes no identity is `/emit` (`S07`), where a sibling, not a caller, emits.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`, ending in a newline. It has no banner and no footer. A `HEAD` is answered with the same status and headers and an empty body. An `X-User-Id` header whose value is empty is answered the same way as no header at all, and so is a request that carries `X-User-Email` and no `X-User-Id`. Every path but `/emit` answers this way — `/`, `/about`, the shared files under `/_appkit/` (`S04`), the MCP endpoint `/mcp` (`S05`), and every path that names nothing; the identity check runs before events looks at the path or the method, so a request with no identity is never a 404 or a 405, and reads nothing of the log.

Preconditions:

- events is serving, and telemetry takes every event.
- The request carries no `X-User-Id` header, or one whose value is empty, and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. The log was not read.
- events wrote nothing to stderr. The 500 is a handled failure, so it is in the trail and not the journal: telemetry has received the request's two events, with an empty user, under the id events gave the request, 32 lowercase hexadecimal characters:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A request from nginx arrives without the identity headers

nginx sets `X-Request-Id` on every request it forwards, so when it is misconfigured and forwards a request without `X-User-Id`, events records the 500 under the id nginx gave the request, and the operator who finds the 500 in the trail can find the same request in nginx's log. The developer here stands in for such an nginx by sending the id by hand.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the same one line, `identity header missing`, ending in a newline.

Preconditions:

- events is serving, and telemetry takes every event.
- The request carries `X-Request-Id` and no `X-User-Id` header.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr. telemetry has received the request's two events under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user, so a trace of that id finds them:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A caller asks for a path that does not exist

events serves the landing page, the about screen, the shared files, the MCP endpoint, and `/emit`, and nothing else, so any other path is answered with events' not-found page, whatever the method: a path that names nothing is never a 405. The MCP endpoint is `/mcp` alone (`S05`) and `/emit` is that path alone (`S07`): `/mcp/`, `/emit/`, or any path beneath either names nothing like any other path. `/about/` names nothing either. events serves nothing under `/assets/`; the pages' stylesheet is under `/_appkit/` (`S04`), whose own missing paths are answered as `S04` tells.

Request:

```
GET /nope HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /assets/theme.css HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /mcp/ HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
POST /nope HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is events' not-found page: titled `Not found`, its heading `Not found` and its text `There is nothing at this address.`, with the footer `events <display>`, no banner, and no script but the button feedback script. A `HEAD` is answered with the same status and headers and an empty body.

Preconditions:

- events is serving, and telemetry takes every event.

Postconditions:

- Nothing has changed. The log was not read.
- events wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 404.

## A user opens the landing page while events cannot read its log

The landing page lists subscribers from the log, so when events cannot read it there is nothing true to list. It does not draw the page as if there were no subscribers, which would tell the user every service had stopped taking events; it says plainly that the page is not available, quoting nothing of the database's own error, and the user may try again later. The tools answer the same state in their own way (`S05`). The about screen and the not-found page read nothing of the log and are still served.

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
```

Status 503. The body is events' unavailable page: titled `Page unavailable`, its heading `Page unavailable` and its text `This page is not available right now. Try again in a moment.`, with the footer `events <display>`, no banner, and no script but the button feedback script. It has no `Subscribers` or `MCP tools` section. A `HEAD` is answered with the same status and headers and an empty body.

Preconditions:

- events is serving, and telemetry takes every event.
- events' database cannot be read: `state/events.db` has become unreadable since events opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr. telemetry has received the request's two events under user `u_7f3a9c21`, the `request.finished` with `status` 503.
- events is still serving.

## A caller sends a page a method it does not take

The pages only read, so `/` and `/about` take `GET` and `HEAD` and nothing else, and `Allow` names those two. No page is sent. The identity check comes first, so a request with no `X-User-Id` is answered with the 500 of `A request arrives without the identity headers`, whatever its method. The MCP endpoint and `/emit` refuse a method they do not take in their own way (`S05`, `S07`).

Request:

```
POST / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /about HTTP/1.1
Host: events.sbx.ikigenba.dev
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

- events is serving.

Postconditions:

- Nothing has changed. The log was not read.
- events wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 405.
