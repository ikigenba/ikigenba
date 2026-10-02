# Stories — connect

The connect page and the gateway's routing outside MCP: what a running mcp answers at every path but `/mcp` and `/mcp/<scope>`, and the frame its one page is drawn in. The connect page, at `/`, tells a person how to point an MCP client at the gateway: the endpoint to add, the credential every request needs, and the MCP services the endpoint reaches. It is server-rendered HTML, and the whole of its content arrives in the response body, the list of services and the launcher's list included; the page carries no script of its own, and the one script it may load is the launcher's, `/_appkit/launcher.js`, which only filters the launcher's list as the user types. An nginx gate in front of mcp authenticates every request and sets `X-User-Id` and `X-User-Email` on the request it passes upstream, with the request's id in `X-Request-Id`. mcp trusts those headers absolutely and has no unauthenticated case, so there is no sign-in page and no signed-out banner. Only nginx and the suite's own apps can reach mcp's socket, so a request that arrives without `X-User-Id`, or with it empty, means the gate or a sibling is misconfigured — a server fault, not a bad request. The requests go to a running mcp (`S02`), each shown as the HTTP request mcp receives, with the headers the story depends on. A developer stands in for the gate by passing those headers by hand. mcp is started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file, the host's services file, holds the suite's services file unless a story says otherwise:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "https://dummy.sbx.ikigenba.dev", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true },
    { "name": "mcp", "url": "https://mcp.sbx.ikigenba.dev", "description": "Connect AI assistants to your services", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false },
    { "name": "notes", "url": "https://notes.sbx.ikigenba.dev", "description": "Notes to keep and search", "socket": "/run/ikigenba/notes.sock", "enabled": false, "mcp": true }
  ]
}
```

mcp takes the file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows on the next page without a restart. The file is a JSON object whose `services` member is an array; each entry is an object with `name`, a non-empty string; `url`, `description`, and `socket`, strings; `enabled`, `true` or `false`, `false` for a service switched off; `mcp`, `true` or `false`; and, optionally, `icon`, a string holding the SVG text of the service's icon. Members mcp does not know are ignored, and an entry that lacks one of the six others, or holds one of the wrong kind, is left out while the rest are still used. With no variable, no readable file, or a file that is not such an object, mcp treats the file as listing no services and writes nothing about it, since a broken services file never breaks the page.

The page's list of services is the gateway's catalogue: the entries whose `mcp` is `true`, leaving out any entry named `mcp` — the gateway never lists itself, whatever its own entry says — in bytewise order of name. Each is shown with its name, its `description` from the file, its scoped endpoint, and its status: `available` when its `enabled` is `true`, and `disabled` when it is `false`. These are the same services the gateway's `services` tool lists on the unscoped endpoint (`S06`).

The endpoint the page shows is `<scheme>://<host>/mcp`. `<host>` is the request's `Host` exactly as it arrived, port included. The scheme is the request's `X-Forwarded-Proto` when that header is exactly `http` or exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty value, or no header at all. A service's scoped endpoint is the endpoint followed by `/` and the service's name; how the gateway answers at a scoped endpoint is `S05`'s.

The page is drawn in the banner every app of the platform draws, at the top of the page. It holds the mark, whose text is `ikigenba` and which names the service it fronts, `mcp`, so a browser shows `ikigenba │ mcp`; a profile link; and a sign-out button; and, when the services file lists services that carry an icon, the launcher button (below). The service's name is lowercase `mcp` everywhere it appears, and the page's title, the one a browser shows on its tab, is `mcp`. The profile link is the `user-circle` icon with no text of its own, labelled `Profile` for assistive technology and titled with the caller's `X-User-Email` value, exactly as it arrived, so hovering it shows who is signed in; the email is in no visible text on the page. The sign-out button is a form, not a link: pressing it POSTs to `<auth-logout>`, and its `logout` icon is drawn before the text and hidden from assistive technology, so its accessible text is `Sign out` alone. Following the profile link or submitting the form leaves mcp for auth; what auth does there is told in auth's own stories. mcp serves no logout route and sets no cookie.

`<auth-profile>`, where the banner's profile link and the page's own `your profile` link lead, is auth's profile page: the `url` of the services file's entry named `auth`, followed by `/`, so `https://auth.sbx.ikigenba.dev/` for the suite's services file; `<auth-logout>` is that same `url` followed by `/logout`. When the file has no entry named `auth`, or that entry's `url` is empty, mcp reads the space from the request's own `Host`: a trailing port is dropped, then a single leading `mcp.` label; what remains is the space, `<auth-profile>` is `<scheme>://auth.<space>/`, and `<auth-logout>` is `<scheme>://auth.<space>/logout`, the scheme chosen as for the endpoint. A `Host` with no `mcp.` label is the space whole. There is no fixed fallback address.

The launcher is the banner's way to the platform's other services, and it is the same launcher every app draws. It offers only the services file's entries that carry an icon: an entry with no `icon`, or one that is not a string, stays out of it, and with no such entry the page has no launcher and is otherwise the same page. When the launcher is there, the banner holds a launcher button labelled `Services`; pressing it opens the list, which is closed when the page loads. The list holds a search field labelled `Find a service`, with the placeholder `Find a service`, and one entry per service that carries an icon, in the file's order, each showing the service's icon and then its name. An enabled service's entry is a link to its `url`; a service switched off keeps its place but is not a working link, and is titled `<name> is unavailable`. mcp's own entry, the one named `mcp`, is marked as the current page. The list, the search field, and a hidden no-match line are all in the page as served; the page loads `/_appkit/launcher.js` only when the launcher is there.

The page links `/_appkit/theme.css` as its stylesheet and declares the phone-width viewport, so a phone shows it at the phone's own width. The stylesheet, its fonts, and the launcher's script are the platform's shared files, which mcp serves under `/_appkit/` (`S04`); the page makes no request to any third party. Last on the page is the footer, whose text is `mcp v<semver>`: the service's name, one space, and the version `mcp --version` prints (`S01`), exactly as it prints it. The version is data, and no story fixes its value.

The connect page is mcp's only HTML. Every other answer in this group is bare: the missing-header 500 and the 404 are one line of plain text, and the 405 has an empty body; none has a banner or a footer. mcp writes nothing to stderr about any answer in this group, the missing-header 500 included; only an event it cannot deliver reaches stderr (`S02`): every request it answers here adds exactly two events to its trail (`S02`), `request.started` with its method and path as it arrives and `request.finished` with the status mcp answered once the answer is complete, and serving the page contacts no backend.

The routes are `/`, the connect page, which takes `GET` and `HEAD`; `/_appkit/<name>`, the platform's shared files (`S04`); and `/mcp` and `/mcp/<scope>`, the MCP endpoint, whose answers, to every method, are `S05`'s and never one of the answers below. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the connect page

A user who wants to use the suite's services from an AI assistant opens the gateway's page to learn what to tell the client: the address to add, how to authenticate, and which services that address reaches. Everything a person looking at a browser sees is in the document that arrives, so a reader who fetches the page with `curl` has it all. There are no per-client instructions. `auth` and `mcp` are not in the list because their `mcp` is `false`; `notes` is, switched off, so the user knows it exists and why it does not answer.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `mcp` that links `/_appkit/theme.css` as its stylesheet and declares the phone-width viewport. Its banner holds the mark, whose text is `ikigenba` and which names the service `mcp`; the profile link, labelled `Profile` and titled `mg@example.com`, leading to `https://auth.sbx.ikigenba.dev/`; and the sign-out button reading `Sign out` in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`, since no entry in the file carries an icon. Beneath the banner is the page's top-level heading, `Connect an MCP client`, then the line `Add this server to your client as a remote (Streamable HTTP) MCP server.`, then the endpoint `https://mcp.sbx.ikigenba.dev/mcp` as code a user can copy, then the line `Every request must send the header Authorization: Bearer <token>. Create a token on your profile.`, in which `your profile` is a link to `https://auth.sbx.ikigenba.dev/`. Below that is the section headed `Services`, reading `The endpoint above reaches every service. To limit a client to some of them, append their names, separated by commas: https://mcp.sbx.ikigenba.dev/mcp/a,b.`, and a table whose header cells read `Name`, `Description`, `Endpoint`, and `Status`, with exactly two rows, in this order: `dummy`, `Demo widgets to list and create`, `https://mcp.sbx.ikigenba.dev/mcp/dummy`, `available`; and `notes`, `Notes to keep and search`, `https://mcp.sbx.ikigenba.dev/mcp/notes`, `disabled`. There is no row for `auth` or `mcp`, and the line `No MCP services are installed.` is not on the page. Last on the page is the footer reading `mcp v<semver>`, where `v<semver>` is what `mcp --version` prints. The address `mg@example.com` is not in the page's visible text.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file and is readable by mcp.

Postconditions:

- Nothing has changed. The services file is as it was, and no backend was contacted.
- mcp wrote nothing to stderr and set no cookie.
- The trail holds two events for the request, both under user `u_7f3a9c21` and a request id mcp made up for it, 32 lowercase hexadecimal characters, since the request carried no `X-Request-Id`:

  ```
  request.started method=GET path=/
  request.finished status=200
  ```

## A user's client asks for the connect page's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body. The page is what a monitor or a proxy reaches for when it wants to know mcp is up without paying for the page.

Request:

```
HEAD / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
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

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.

## A user on a host with no MCP services sees none listed

A host may have no service marked for MCP, and the page says so in words rather than showing an empty table. Here `/var/lib/ikigenba/services.json` is the suite's services file without the `dummy` and `notes` entries: it lists only `auth` and `mcp`, both with `mcp` `false`.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the connect page of `A user opens the connect page`, with the same banner, endpoint, credential line, profile link, and footer, except that in the `Services` section, after the line explaining how to limit a client, there is no table, and in its place the line `No MCP services are installed.`

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` lists `auth` and `mcp` as in the suite's services file, and no other entry but the stand-in `telemetry` entry every story's file holds (`S02`).

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr.

## A user on a host whose services file is missing sees no services listed

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A broken services file never breaks the page, so mcp treats it as listing no services and reports nothing: this is not a fault of the request. A file that exists but cannot be read, or is not a JSON object with a `services` array, and an `IKIGENBA_SERVICES` that is unset or empty, are all answered the same way. With no file there is no `auth` entry either, so auth's links are read from the request's `Host`, which here names the same space.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the connect page of `A user on a host with no MCP services sees none listed`: the line `No MCP services are installed.` in place of the table, the endpoint `https://mcp.sbx.ikigenba.dev/mcp`, the profile link and the page's `your profile` link leading to `https://auth.sbx.ikigenba.dev/`, and the sign-out button POSTing to `https://auth.sbx.ikigenba.dev/logout`. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` does not exist.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr about the services file. Without one, though, mcp cannot find the telemetry service either, so no event of the request reaches the trail, and stderr holds one `undelivered event` line for each (`S02`), in this order, where `<id>` is the request id mcp made up for the request, each `<time>` is when mcp recorded that event, and each `<us>` a duration in whole microseconds:

  ```
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.started","request_id":"<id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.finished","request_id":"<id>","user":"u_7f3a9c21","attrs":{"duration_us":<us>,"status":200}}
  ```

## A user does not see the gateway in its own list

The gateway's own tools are not services a client reaches through it, and listing them would send the gateway calling itself. So the entry named `mcp` is left out of the list even when the file marks it for MCP, which mcp's own manifest never does (`S01`); `services` likewise never lists it (`S06`). Here `/var/lib/ikigenba/services.json` is the suite's services file with the `mcp` entry's `mcp` `true`.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the connect page of `A user opens the connect page`: the table's rows are exactly `dummy`, `available`, and `notes`, `disabled`, in that order, and there is no row for `mcp`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file, except that the entry named `mcp` has `"mcp": true`.

Postconditions:

- Nothing has changed.

## A user sees the list follow a change to the services file

The host rewrites the services file when a service is installed or switched on or off, and mcp reads the file afresh for every request, so the next page a user loads shows the new list without mcp being restarted. Here the host has switched `notes` on since mcp started: `/var/lib/ikigenba/services.json` is the suite's services file with `notes`'s `enabled` now `true`.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the connect page of `A user opens the connect page`, except that the `notes` row's status reads `available`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment while `/var/lib/ikigenba/services.json` held the suite's services file, with `notes` switched off, and it has not been restarted since.
- `/var/lib/ikigenba/services.json` now lists `notes` with `enabled` `true`.

Postconditions:

- Nothing has changed.

## A user on a host whose services file names no auth still gets auth's links

The banner's profile link and sign-out form, and the page's `your profile` link, address auth on the space the request names in its `Host` when the services file has no `auth` entry to take them from, so one mcp build serves whichever space it is installed on. The endpoint the page shows is built from the same headers. Here `/var/lib/ikigenba/services.json` is the suite's services file without the `auth` entry.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
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

Status 200. The body is the connect page, with the same two rows as in `A user opens the connect page`, and for every form above its banner's profile link, titled `mg@example.com`, and the page's `your profile` link both lead to `https://auth.sbx.ikigenba.dev/`, and the sign-out button reading `Sign out` is in a form whose method is `post` and whose action is `https://auth.sbx.ikigenba.dev/logout`. The endpoint names the `Host` as it arrived: `https://mcp.sbx.ikigenba.dev:443/mcp` for the first form, `https://mcp.sbx.ikigenba.dev/mcp` for the second and third, and `https://sbx.ikigenba.dev/mcp` for the fourth, and each row's scoped endpoint and the example in the `Services` section begin with that same endpoint. With `X-Forwarded-Proto: http` and `Host: mcp.sbx.ikigenba.dev`, the links lead to `http://auth.sbx.ikigenba.dev/`, the form's action is `http://auth.sbx.ikigenba.dev/logout`, and the endpoint is `http://mcp.sbx.ikigenba.dev/mcp`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file without the entry named `auth`.

Postconditions:

- Nothing has changed.
- mcp set no cookie.

## A user on a host with services opens the launcher

On a host, the services file carries the icon of every service whose package ships one, and the launcher is how a user gets from mcp to any of them without typing an address. Here `/var/lib/ikigenba/services.json` is the suite's services file with an `icon` on each of its four entries, `auth`, `dummy`, `mcp`, and `notes`, each holding the SVG text of that service's icon. The launcher lists every service that carries an icon, not only the MCP services, so `auth` and mcp itself are in it though they are not in the page's table.

In a browser, the list is closed when the page loads, and pressing the launcher button opens it. Typing in the search field keeps only the entries whose name contains the typed text, ignoring case and any spaces around it; clearing the field shows them all again. When the text matches no entry, the no-match line appears, reading `No service matches “<text>”.` with the typed text in quotation marks. Pressing Enter in the search field opens the first entry still shown that is a working link, and does nothing when there is none. That filtering is the whole of what `/_appkit/launcher.js` does: every entry, and the no-match line, arrived with the page.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the connect page of `A user opens the connect page`, with the same banner, and the banner also holds the launcher button labelled `Services`. The page carries the list of services labelled `Services`, holding the search field labelled `Find a service` with the placeholder `Find a service` and four entries in the file's order, each showing its icon and then its name: `auth`, a link to `https://auth.sbx.ikigenba.dev`; `dummy`, a link to `https://dummy.sbx.ikigenba.dev`; `mcp`, a link to `https://mcp.sbx.ikigenba.dev`, marked as the current page; and `notes`, not a working link, titled `notes is unavailable`. The no-match line is in the page and hidden. The page loads the script `/_appkit/launcher.js`. The table below is unchanged: `dummy` and `notes` only.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file with an `icon` string on each entry.

Postconditions:

- Nothing has changed. The services file is as it was.

## A request arrives without the identity headers

In production this cannot happen from outside: the gate sets the headers on every request it forwards, and nothing but nginx and the suite's apps can reach mcp's socket. So a request without `X-User-Id` says the gate or a sibling is misconfigured, which is mcp's fault to report, not the caller's to fix — hence a 500 and not a 400 or a 401. A developer meets it by forgetting the headers, as here.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`, ending in a newline. A `HEAD` is answered with the same status and headers and an empty body. An `X-User-Id` header whose value is empty is answered the same way as no header at all. Every path answers this way — `/`, the shared files under `/_appkit/` (`S04`), the MCP endpoint `/mcp` and every `/mcp/<scope>`, well-formed or not (`S05`), and every path that does not exist; the identity check runs before mcp looks at the path, the method, or a scope, so a request with no identity is never a 404 or a 405.

Preconditions:

- mcp is serving.
- The request carries no `X-User-Id` header, or one whose value is empty.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr. The 500 is a handled failure, so it is in the trail and not the journal: the trail holds two events for the request, with an empty user and a request id mcp made up for it, 32 lowercase hexadecimal characters, because it carried no `X-Request-Id`:

  ```
  request.started method=GET path=/
  request.finished status=500
  ```

## A request from nginx arrives without the identity headers

nginx sets `X-Request-Id` on every request it forwards, so when the gate is misconfigured and forwards a request without `X-User-Id`, mcp records the 500 under the id nginx gave the request, and the operator who finds the 500 in the trail can find the same request in nginx's log. The developer here stands in for such an nginx by sending the id by hand.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the same one line, `identity header missing`, ending in a newline.

Preconditions:

- mcp is serving.
- The request carries `X-Request-Id` and no `X-User-Id` header.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr. The trail holds two events for the request, under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user:

  ```
  request.started method=GET path=/
  request.finished status=500
  ```

## A caller asks for a path that does not exist

The gateway serves the connect page, the shared files, and the MCP endpoint, and nothing else; it has no widgets, no panel, and no page of its own to send a lost caller to, so a path it does not serve is answered with one line of plain text, whatever the method. A malformed scope under `/mcp/` is answered with the same 404, as `S05` tells.

Request:

```
GET /nope HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /widgets HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
POST /nope HTTP/1.1
Host: mcp.sbx.ikigenba.dev
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

- mcp is serving.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr.

## A caller sends the connect page a method it does not take

The connect page only reads, so `/` takes `GET` and `HEAD` and nothing else, and `Allow` names those two. No page is sent. The MCP endpoint refuses a method it does not take in its own way, with its own `Allow` (`S05`).

Request:

```
POST / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. `PUT`, `PATCH`, and every other method but `GET` and `HEAD` are refused the same way.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr.
