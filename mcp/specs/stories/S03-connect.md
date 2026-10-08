# Stories — connect

The connect page and the gateway's routing outside MCP: what a running mcp answers at every path but `/mcp` and `/mcp/<scope>`, and the frame its one page is drawn in. The connect page, at `/`, tells a person how to connect an MCP client to the gateway: the command that adds the gateway to Claude Code, the command that adds it to Codex, and, for any other client, the endpoint alone, since a client that speaks MCP's authorization flow learns the rest from the gateway's protected-resource metadata (`S12`) and signs its user in through auth. It is server-rendered HTML, and the whole of its content arrives in the response body, the launcher's list included. No script adds content a user sees; the page's scripts only act on what the server sent. The page carries no script of its own: it loads the platform's button feedback script, `/_appkit/feedback.js`, which makes an enabled button visibly react when the user presses it and makes each `Copy` button put the text beside it on the clipboard; and, when the page carries the launcher, the launcher's script, `/_appkit/launcher.js`, which only filters the launcher's list as the user types. The host's nginx lets guests through to every path of mcp but `/mcp`, the paths beneath it, and git's smart HTTP paths (its manifest sets `guests = true`, `S01`; opsctl's `S5-nginx.md`): a request from a signed-in user carries `X-User-Id` and `X-User-Email`, which the gate sets from auth's answer, and a guest's carries neither, while every request nginx forwards carries the request's id in `X-Request-Id`. A request whose `X-User-Id` is absent or empty is a guest's, whatever `X-User-Email` it carries. mcp trusts those headers absolutely. The connect page is for signed-in users only, and a guest who asks for it is sent to auth's sign-in with the page's URL to come back to, so mcp draws no signed-out banner; the shared files (`S04`) and the protected-resource metadata (`S12`) are served to guests and users alike, and no guest is sent to sign in from either, and a path that does not exist is answered the same for both. The requests go to a running mcp (`S02`), each shown as the HTTP request mcp receives, with the headers the story depends on. A developer stands in for the gate by passing those headers by hand. mcp is started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, and that file, the host's services file, holds the suite's services file unless a story says otherwise:

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

The page reads the file for two things: auth's address, for the banner's links, and the launcher's list. mcp takes the file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows on the next page without a restart. The file is a JSON object whose `services` member is an array; each entry is an object with `name`, a non-empty string; `url`, `description`, and `socket`, strings; `enabled`, `true` or `false`, `false` for a service switched off; `mcp`, `true` or `false`; and, optionally, `icon`, a string holding the SVG text of the service's icon. Members mcp does not know are ignored, and an entry that lacks one of the six others, or holds one of the wrong kind, is left out while the rest are still used. With no variable, no readable file, or a file that is not such an object, mcp treats the file as listing no services and writes nothing about it, since a broken services file never breaks the page.

The page lists no services: what the endpoint reaches is the gateway's `services` tool's to say (`S06`).

The endpoint the page shows is `<scheme>://<host>/mcp`. `<host>` is the request's `Host` exactly as it arrived, port included. The scheme is the request's `X-Forwarded-Proto` when that header is exactly `http` or exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty value, or no header at all.

Both commands name the gateway in the client by a server name derived from the space the request names: the space is read from the `Host` as for auth's links (below), a trailing port dropped and then a single leading `mcp.` label, and the server name is that space with every character that is not an ASCII letter or digit turned into `-`, in lower case, with no prefix. So `Host: mcp.sbx.ikigenba.dev` gives `sbx-ikigenba-dev`, and a sandbox at `Host: mcp.wip-mcp.localhost:7403` gives `wip-mcp-localhost`. The Claude Code command is a command that adds the gateway to Claude Code under the server name, at the endpoint the page shows, and the Codex command is one that adds it to Codex the same way; each holds the server name and the endpoint, and the page, not these stories, fixes the rest of its wording. The services file plays no part in either.

The page is drawn in the banner every app of the platform draws, at the top of the page. It holds the mark, the launcher button, the profile link, and the sign-out icon button, in that order; the launcher button is there only when the services file lists services that carry an icon (below), immediately after the mark. The mark shows the platform's favicon, then the product name `Ikigenba`, then the service it fronts, `mcp`: mcp's own icon and then its name when the services file lists `mcp` with an icon, and the name alone otherwise. The favicon in the mark is decoration and has no text of its own. The service's name is lowercase `mcp` everywhere it appears, and the page's title, the one a browser shows on its tab, is `mcp`. The profile link is the `user-circle` icon with no text of its own, labelled `Profile` for assistive technology and titled with the caller's `X-User-Email` value, exactly as it arrived, so hovering it shows who is signed in; the email is in no visible text on the page. The sign-out button is a form, not a link: pressing it POSTs to `<auth-logout>`. Like the profile link, it shows the `logout` icon and no text of its own: it is labelled `Sign out` for assistive technology and titled `Sign out`, which a browser shows as its tooltip. Following the profile link or submitting the form leaves mcp for auth; what auth does there is told in auth's own stories. mcp serves no logout route and sets no cookie.

`<auth-profile>`, where the banner's profile link leads, is auth's profile page: the `url` of the services file's entry named `auth`, followed by `/`, so `https://auth.sbx.ikigenba.dev/` for the suite's services file; `<auth-logout>` is that same `url` followed by `/logout`. When the file has no entry named `auth`, or that entry's `url` is empty, mcp reads the space from the request's own `Host`: a trailing port is dropped, then a single leading `mcp.` label; what remains is the space, `<auth-profile>` is `<scheme>://auth.<space>/`, and `<auth-logout>` is `<scheme>://auth.<space>/logout`, the scheme chosen as for the endpoint. A `Host` with no `mcp.` label is the space whole. There is no fixed fallback address.

The launcher is the banner's way to the platform's other services, and it is the same launcher every app draws. It offers only the services file's entries that carry an icon: an entry with no `icon`, or one that is not a string, stays out of it, and with no such entry the page has no launcher and is otherwise the same page. When the launcher is there, the banner holds a launcher button labelled `Services`; pressing it opens the list, which is closed when the page loads. The list holds a search field labelled `Find a service`, with the placeholder `Find a service`, and one entry per service that carries an icon, in the file's order, each showing the service's icon and then its name. An enabled service's entry is a link to its `url`; a service switched off keeps its place but is not a working link, and is titled `<name> is unavailable`. mcp's own entry, the one named `mcp`, is marked as the current page. The list, the search field, and a hidden no-match line are all in the page as served; the page loads `/_appkit/launcher.js` only when the launcher is there.

The page links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport, so a phone shows it at the phone's own width. The favicon is the platform's one icon, which a browser shows in the page's tab. The stylesheet, its fonts, the launcher's script, the button feedback script, and the favicon are the platform's shared files, which mcp serves under `/_appkit/` (`S04`); the page makes no request to any third party. Last on the page is the footer, whose text is `mcp <display>`: the service's name, one space, and `<display>`, the string `mcp --version` prints under the environment mcp was started with (`S01`), exactly as it prints it, so a user can tell which code is serving the page. No story fixes its value. When that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`, `<display>` is empty and the footer's text is `mcp` and the one space.

The connect page is mcp's only HTML. Every other answer in this group is bare: the 404 is one line of plain text, and the 405 and a guest's redirect fix no body; none has a banner or a footer. mcp writes nothing to stderr about any answer in this group; only an event it cannot deliver reaches stderr (`S02`): every request it answers here adds exactly two events to its trail (`S02`), `request.started` with its method and path as it arrives and `request.finished` with the status mcp answered once the answer is complete, and serving the page contacts no backend.

The routes are `/`, the connect page, which takes `GET` and `HEAD` and sends a guest to sign in; `/.well-known/oauth-protected-resource` and every path beneath it, the protected-resource metadata (`S12`); `/_appkit/<name>`, the platform's shared files (`S04`); and `/mcp` and `/mcp/<scope>`, the MCP endpoint, whose answers, to every method, are `S05`'s and never one of the answers below. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the connect page

A user who wants to use the suite's services from an AI assistant opens the gateway's page to learn how to connect the client. The page gives one command for Claude Code and one for Codex, each to paste into a terminal, and the endpoint alone for any other client; the client then signs the user in through a browser the first time it connects. Everything a person looking at a browser sees is in the document that arrives, so a reader who fetches the page with `curl` has it all. The page says nothing about tokens.

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

Status 200. The body is an HTML document titled `mcp` that links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport. Its banner holds the mark, showing the platform's favicon, the text `Ikigenba`, and the service's name `mcp` with no icon, since no entry in the file carries an icon; the profile link, labelled `Profile` and titled `mg@example.com`, leading to `https://auth.sbx.ikigenba.dev/`; and the sign-out button, labelled and titled `Sign out`, in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`, since no entry in the file carries an icon. Beneath the banner is the page's top-level heading, `Connect MCP Client`, and under it three sections, in this order, each holding one line of code with a button reading `Copy` beside it and nothing else. The first is headed `Claude Code` and holds, as code, the command that adds the gateway to Claude Code under the server name `sbx-ikigenba-dev` at the endpoint `https://mcp.sbx.ikigenba.dev/mcp`; in a browser, pressing its `Copy` puts the command, and nothing else, on the clipboard and shows a toast reading `Copied to clipboard`, or, when the clipboard refuses it, selects the command so the user can copy it themselves and shows an error toast saying so. The second is headed `Codex` and holds, as code, the command that adds the gateway to Codex under the same server name at the same endpoint, whose `Copy` puts that command, and nothing else, on the clipboard, with the same toasts. The third is headed `Other clients` and holds the endpoint `https://mcp.sbx.ikigenba.dev/mcp` as code, whose `Copy` puts the endpoint, and nothing else, on the clipboard, with the same toasts. The page has no other section and no other text: it lists no services, it says nothing about git, about tokens, or about an `Authorization` header, and it has no `profile` link of its own; the banner's profile link is the page's only way to auth's profile. In a browser, `Sign out` and each `Copy` button visibly react as the user presses them. Last on the page is the footer reading `mcp <display>`, where `<display>` is what `mcp --version` prints under the same environment. The address `mg@example.com` is not in the page's visible text.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file and is readable by mcp.

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

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.

## A user on a host whose services file is missing still gets the page

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

Status 200. The body is the connect page of `A user opens the connect page`: the Claude Code and Codex commands, each naming the server `sbx-ikigenba-dev` and the endpoint `https://mcp.sbx.ikigenba.dev/mcp`, the endpoint `https://mcp.sbx.ikigenba.dev/mcp`, the profile link leading to `https://auth.sbx.ikigenba.dev/`, and the sign-out button POSTing to `https://auth.sbx.ikigenba.dev/logout`. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` does not exist.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr about the services file. Without one, though, mcp cannot find the telemetry service either, so no event of the request reaches the trail, and stderr holds one `undelivered event` line for each (`S02`), in this order, where `<id>` is the request id mcp made up for the request, each `<time>` is when mcp recorded that event, each `<us>` a duration in whole microseconds, and each `<bytes>` a count of body bytes:

  ```
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.started","request_id":"<id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.finished","request_id":"<id>","user":"u_7f3a9c21","attrs":{"duration_us":<us>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A user on a host whose services file names no auth still gets auth's links

The banner's profile link and sign-out form address auth on the space the request names in its `Host` when the services file has no `auth` entry to take them from, so one mcp build serves whichever space it is installed on. The endpoint and the server name the page shows are built from the same headers. Here `/run/ikigenba/services.json` is the suite's services file without the `auth` entry.

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

Status 200. The body is the connect page of `A user opens the connect page`, and for every form above its banner's profile link, titled `mg@example.com`, leads to `https://auth.sbx.ikigenba.dev/`, and the sign-out button labelled `Sign out` is in a form whose method is `post` and whose action is `https://auth.sbx.ikigenba.dev/logout`. The endpoint names the `Host` as it arrived: `https://mcp.sbx.ikigenba.dev:443/mcp` for the first form, `https://mcp.sbx.ikigenba.dev/mcp` for the second and third, and `https://sbx.ikigenba.dev/mcp` for the fourth, and each command names the endpoint of its form. The server name is `sbx-ikigenba-dev` for every form, the port and the `mcp.` label dropped, so the first form's Claude Code and Codex commands each add the gateway under `sbx-ikigenba-dev` at `https://mcp.sbx.ikigenba.dev:443/mcp`. With `X-Forwarded-Proto: http` and `Host: mcp.sbx.ikigenba.dev`, the link leads to `http://auth.sbx.ikigenba.dev/`, the form's action is `http://auth.sbx.ikigenba.dev/logout`, and the endpoint is `http://mcp.sbx.ikigenba.dev/mcp`, in the `Other clients` section and in both commands. A sandbox's request, `Host: mcp.wip-mcp.localhost:7403` with `X-Forwarded-Proto: http`, gets a Claude Code command and a Codex command that each add the gateway under the server name `wip-mcp-localhost` at `http://mcp.wip-mcp.localhost:7403/mcp`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file without the entry named `auth`.

Postconditions:

- Nothing has changed.
- mcp set no cookie.

## A user on a host with services opens the launcher

On a host, the services file carries the icon of every service whose package ships one, and the launcher is how a user gets from mcp to any of them without typing an address. Here `/run/ikigenba/services.json` is the suite's services file with an `icon` on each of its four entries, `auth`, `dummy`, `mcp`, and `notes`, each holding the SVG text of that service's icon. The launcher lists every service that carries an icon, not only the MCP services, so `auth` and mcp itself are in it.

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

Status 200. The body is the connect page of `A user opens the connect page`, with the same banner, except that its mark shows mcp's icon before the name `mcp`, and the banner also holds the launcher button labelled `Services`, immediately after the mark. The page carries the list of services labelled `Services`, holding the search field labelled `Find a service` with the placeholder `Find a service` and four entries in the file's order, each showing its icon and then its name: `auth`, a link to `https://auth.sbx.ikigenba.dev`; `dummy`, a link to `https://dummy.sbx.ikigenba.dev`; `mcp`, a link to `https://mcp.sbx.ikigenba.dev`, marked as the current page; and `notes`, not a working link, titled `notes is unavailable`. The no-match line is in the page and hidden. The page loads the script `/_appkit/launcher.js`. The rest of the page is unchanged.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file with an `icon` string on each entry.

Postconditions:

- Nothing has changed. The services file is as it was.

## A guest asks for the connect page and is sent to sign in

The connect page names the caller in its banner, so it is for signed-in users. A guest — a browser with no session, which nginx lets through because mcp serves guests the protected-resource metadata — is sent to auth's sign-in, carrying the URL it asked for so auth can bring it back once it has signed in. The sign-in address is `<auth-profile>`, derived as for the banner's profile link, followed by `?return=` and the URL to return to, `<scheme>://<Host><path and query>`, percent-encoded as a query component, the scheme chosen as for the endpoint. The request carries no `X-User-Id`; one with an empty `X-User-Id`, or an `X-User-Email` and no `X-User-Id`, is a guest's all the same.

Request:

```
GET / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
HEAD / HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2F
```

Status 302. No story fixes the body. The response sets no cookie. `GET /?from=launcher` is answered with `Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2F%3Ffrom%3Dlauncher`, and with `X-Forwarded-Proto: http` and a services file naming no `auth`, both addresses are `http`: `Location: http://auth.sbx.ikigenba.dev/?return=http%3A%2F%2Fmcp.sbx.ikigenba.dev%2F`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.
- The request carries no `X-User-Id` and no `X-User-Email`, as nginx forwards a guest's request, and the `X-Request-Id` nginx gave it.

Postconditions:

- Nothing has changed. No page was drawn and no backend was contacted.
- mcp wrote nothing to stderr. The trail holds two events for the request, under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user:

  ```
  request.started method=GET path=/
  request.finished status=302
  ```

## A caller asks for a path that does not exist

The gateway serves the connect page, the protected-resource metadata, the shared files, and the MCP endpoint, and nothing else; it has no widgets, no panel, and no page of its own to send a lost caller to, so a path it does not serve is answered with one line of plain text, whatever the method. A malformed scope under `/mcp/` is answered with the same 404, as `S05` tells. Of the paths under `/.well-known/`, only `/.well-known/oauth-protected-resource` and the paths beneath it are served (`S12`): auth, not the gateway, publishes the authorization server's metadata, and `/.well-known/oauth-protected-resourcex` is not beneath the document's path.

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

```
GET /.well-known/oauth-authorization-server HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is exactly the one line `not found`, ending in a newline. It has no banner and no footer. A guest's request for such a path is answered the same, not sent to sign in.

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

Status 405. The body is empty. `PUT`, `PATCH`, and every other method but `GET` and `HEAD` are refused the same way, a guest's included.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr.
