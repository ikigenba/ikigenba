# Stories — landing

The landing page, the about screen, and repos' routing outside MCP and git: what a running repos answers at every path but `/mcp` and the git paths, and the frame its two pages are drawn in. The landing page, at `/`, tells a person what repos is, names the six tools an agent reaches it with, and says how to clone with git without writing the token to disk; the about screen, at `/about`, shows its name, its version, and its description. Both are server-rendered HTML, and the whole of each page's content arrives in the response body, the launcher's list included; neither page carries a script of its own, and no script adds content a user sees. The scripts a page loads only act on what the server sent: the platform's button feedback script, `/_appkit/feedback.js`, which both pages load with or without a launcher, makes an enabled button visibly react when the user presses it; and the launcher's, `/_appkit/launcher.js`, which a page loads only with the launcher, filters the launcher's list as the user types. An nginx gate in front of repos authenticates every request and sets `X-User-Id` and `X-User-Email` on the request it passes upstream, with the request's id in `X-Request-Id`. repos trusts those headers absolutely and has no unauthenticated case, so there is no sign-in page and no signed-out banner. Only nginx and the suite's own apps can reach repos' socket, so a request that arrives without `X-User-Id`, or with it empty, means the gate or a sibling is misconfigured — a server fault, not a bad request. The requests go to a running repos (`S02`), each shown as the HTTP request repos receives, with the headers the story depends on. A developer stands in for the gate by passing those headers by hand. repos is started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file, the host's services file, holds the suite's services file unless a story says otherwise:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "https://dummy.sbx.ikigenba.dev", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true },
    { "name": "mcp", "url": "https://mcp.sbx.ikigenba.dev", "description": "Connect AI assistants to your services", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false },
    { "name": "repos", "url": "https://repos.sbx.ikigenba.dev", "description": "Git repositories for the suite's content", "socket": "/run/ikigenba/repos.sock", "enabled": true, "mcp": true },
    { "name": "telemetry", "url": "https://telemetry.sbx.ikigenba.dev", "description": "The suite's trail of events", "socket": "/run/ikigenba/telemetry.sock", "enabled": true, "mcp": true }
  ]
}
```

repos takes the file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows on the next page without a restart. The file is a JSON object whose `services` member is an array; each entry is an object with `name`, a non-empty string; `url`, `description`, and `socket`, strings; `enabled`, `true` or `false`, `false` for a service switched off; `mcp`, `true` or `false`; and, optionally, `icon`, a string holding the SVG text of the service's icon. Members repos does not know are ignored, and an entry that lacks one of the six others, or holds one of the wrong kind, is left out while the rest are still used. With no variable, no readable file, or a file that is not such an object, repos treats the file as listing no services and writes nothing about it, since a broken services file never breaks a page. The same file names the telemetry service repos sends its trail to (`S02`); in this group telemetry takes every event unless a story says otherwise.

Both pages are drawn in the banner every app of the platform draws, at the top of the page. It holds the mark, whose text is `ikigenba` and which names the service it fronts, `repos`, so a browser shows `ikigenba │ repos`; a profile link; and a sign-out button; and, when the services file lists services that carry an icon, the launcher button (below). The service's name is lowercase `repos` everywhere it appears. The landing page's title, the one a browser shows on its tab, is `repos`, and the about screen's is `About repos`. The profile link is the `user-circle` icon with no text of its own, labelled `Profile` for assistive technology and titled with the caller's `X-User-Email` value, exactly as it arrived, so hovering it shows who is signed in; the email is in no visible text on the page. The sign-out button is a form, not a link: pressing it POSTs to `<auth-logout>`, and its `logout` icon is drawn before the text and hidden from assistive technology, so its accessible text is `Sign out` alone. Following the profile link or submitting the form leaves repos for auth; what auth does there is told in auth's own stories. repos serves no logout route and sets no cookie.

`<auth-profile>`, where the banner's profile link leads, is auth's profile page: the `url` of the services file's entry named `auth`, followed by `/`, so `https://auth.sbx.ikigenba.dev/` for the suite's services file; `<auth-logout>` is that same `url` followed by `/logout`. When the file has no entry named `auth`, or that entry's `url` is empty, repos reads the space from the request's own `Host`: a trailing port is dropped, then a single leading `repos.` label; what remains is the space, `<auth-profile>` is `<scheme>://auth.<space>/`, and `<auth-logout>` is `<scheme>://auth.<space>/logout`. The scheme is the request's `X-Forwarded-Proto` when that header is exactly `http` or exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty value, or no header at all. A `Host` with no `repos.` label is the space whole. There is no fixed fallback address.

`<repos-url>`, the address git reaches repos at, is the `url` of the services file's entry named `repos`, with one trailing `/` dropped if it has one, so `https://repos.sbx.ikigenba.dev` for the suite's services file. When the file has no entry named `repos`, or that entry's `url` is empty, `<repos-url>` is `<scheme>://<Host>`, with the request's `Host` exactly as it arrived, port kept, and the scheme chosen as for auth's links. A repository's clone URL is `<repos-url>/<name>.git` (`S06`, `S07`); it never holds a credential. The guidance on giving git the token, which this group calls the credentials text, is built from `<repos-url>`: `<scheme>` is its scheme and `<space>` is its host, port kept, with a single leading `repos.` label dropped; a host with no `repos.` label is the space whole. The credentials text is exactly these five lines, joined by LF, shown here for `https://repos.sbx.ikigenba.dev`:

```
Git authenticates with your personal access token as the password; the username is ignored. Keep the token in the environment variable IKIGENBA_TOKEN and give it to git with this credential helper, which reads the variable whenever git asks:

git config --global credential.https://*.sbx.ikigenba.dev.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'

Or set GIT_ASKPASS to a program that prints $IKIGENBA_TOKEN. Never put the token in a remote's URL or on a command line, and never use credential.helper store: each writes it to disk in plain text.
```

The third line's scope is `credential.<scheme>://*.<space>.helper`, and nothing else in the text varies. It is the same guidance the mcp gateway's connect page gives, so a user meets one way of handing git the token wherever they are told about it. The same text is the `credentials` member of what `show` and `create` return (`S06`, `S07`). The landing page's `Clone with git` section shows it whatever the caller holds, with or without repositories of their own; the page lists no repositories.

The launcher is the banner's way to the platform's other services, and it is the same launcher every app draws. It offers only the services file's entries that carry an icon: an entry with no `icon`, or one that is not a string, stays out of it, and with no such entry the page has no launcher and is otherwise the same page. When the launcher is there, the banner holds a launcher button labelled `Services`; pressing it opens the list, which is closed when the page loads. The list holds a search field labelled `Find a service`, with the placeholder `Find a service`, and one entry per service that carries an icon, in the file's order, each showing the service's icon and then its name. An enabled service's entry is a link to its `url`; a service switched off keeps its place but is not a working link, and is titled `<name> is unavailable`. repos' own entry, the one named `repos`, is marked as the current page. The list, the search field, and a hidden no-match line are all in the page as served; a page loads `/_appkit/launcher.js` only when the launcher is there.

Each page links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport, so a phone shows it at the phone's own width. The favicon is the platform's one icon, which a browser shows in the page's tab. The stylesheet, its fonts, the launcher's script, the button feedback script, and the favicon are the platform's shared files, which repos serves under `/_appkit/` (`S04`); a page makes no request to any third party. Last on each page is the footer, whose text is `repos <display>`: the service's name, one space, and `<display>`, the string `repos --version` prints under the environment repos was started with (`S01`), exactly as it prints it, so a user can tell which code is serving the page. No story fixes its value. When that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`, `<display>` is empty and the footer's text is `repos` and the one space.

The landing page and the about screen are repos' only HTML. Every other answer in this group is bare: the missing-header 500 and the 404 are one line of plain text, and the 405 has an empty body; none has a banner or a footer. Every request repos answers here adds exactly two events to its trail (`S02`): `request.started`, with the `method` and the `path` as the request arrived, and `request.finished`, once the answer is complete, with the `status` repos answered, `duration_us`, how long the answer took in whole microseconds, `request_bytes`, the bytes of request body repos read, and `response_bytes`, the bytes of response body it wrote. Serving a page reads no repository and no catalog, and contacts no sibling but telemetry, to which the events are sent. While telemetry takes every event, repos writes nothing to stderr for any answer in this group, the missing-header 500 included.

The routes are `/`, the landing page, and `/about`, the about screen, each taking `GET` and `HEAD`; `/_appkit/<name>`, the platform's shared files — the stylesheet, its fonts and their licences, the launcher's script, the button feedback script, and the favicon (`S04`); `/mcp`, exactly that path, the MCP endpoint, whose answers, to every method, are `S05`'s and never one of the answers below; and the git paths, every path whose first segment ends in `.git` — `/notes.git`, `/notes.git/info/refs`, and everything beneath them — whose answers, to every method, are `S11`'s and `S12`'s and never one of the answers below, but for the missing-header 500, which every route gives. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the landing page

A user who lands on repos' name, from the launcher or by typing it, learns what the service is, how an agent uses it, and how to give git their token. Everything a person looking at a browser sees is in the document that arrives, so a reader who fetches the page with `curl` has it all. The page shows no repositories: an agent finds them through the tools, and the page says which tools those are.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `repos` that links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport. Its banner holds the mark, whose text is `ikigenba` and which names the service `repos`; the profile link, labelled `Profile` and titled `mg@example.com`, leading to `https://auth.sbx.ikigenba.dev/`; and the sign-out button reading `Sign out` in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`; in a browser, that button visibly reacts as the user presses it. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`, since no entry in the file carries an icon. Beneath the banner is the page's top-level heading, `repos`, then the summary `Git repositories for the suite's content. Agents create repositories with the tools below and push to them with ordinary git over HTTPS. Each repository belongs to the user who created it, and only that user can see it or reach it.` Below that is the section headed `MCP tools`, reading `Agents manage repositories with these tools, through the MCP gateway's call and mutate.`, and a list of exactly six tools, in this order, each named and followed by the first line of its description (`S05`): `list`, `The repositories you own, by name.`; `show`, `One of your repositories, with its clone URL and how to give git your token.`; `status`, `How busy repos is, and how close each of your repositories is to its size limit.`; `create`, `Create an empty repository and return it, with its clone URL and how to give git your token.`; `rename`, `Give one of your repositories a new name; its id does not change.`; and `delete`, `Delete one of your repositories and everything in it.` Below that is the section headed `Clone with git`, reading `Clone a repository you own from https://repos.sbx.ikigenba.dev/<name>.git, where <name> is its name.`, then the credentials text for `https://repos.sbx.ikigenba.dev`: its first line as a paragraph; its third line, the `git config --global credential.https://*.sbx.ikigenba.dev.helper` command, as code a user can copy; and its fifth line as a paragraph. After the section is the link `About repos`, leading to `/about`. Last on the page is the footer reading `repos <display>`, where `<display>` is what `repos --version` prints under the same environment. The address `mg@example.com` is not in the page's visible text. The text `Repos` appears nowhere, and neither does `IKIGENBA_TOKEN=` or any token.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file and is readable by repos, and telemetry takes every event.

Postconditions:

- Nothing has changed. The services file is as it was, and no repository and no catalog entry was read.
- repos wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id repos gave the request, 32 lowercase hexadecimal characters, since the request carried no `X-Request-Id`:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `response_bytes` is the length of the page's body. The caller's email is in neither.

## A user's client asks for the landing page's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body. The page is what a monitor or a proxy reaches for when it wants to know repos is up without paying for the page. `/about` answers `HEAD` the same way.

Request:

```
HEAD / HTTP/1.1
Host: repos.sbx.ikigenba.dev
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

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- The request's `request.finished` has `status` 200 and `response_bytes` 0, since no body was written.

## A user opens the about screen

The about screen is where a user checks which code is serving and what the service calls itself. Its three facts are repos' own: the name, `<display>`, the string `repos --version` prints under the environment repos was started with, and the description the manifest declares (`S01`), the same line the host publishes in its services file. The screen reads none of them from the services file, so it says the same with or without one.

Request:

```
GET /about HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `About repos`, with the same stylesheet link, favicon link, feedback script, viewport, banner, and footer as the landing page of `A user opens the landing page`. Beneath the banner is the page's top-level heading, `About repos`, then a list of exactly three facts, in this order, each a label and its value: `Name`, `repos`; `Version`, `<display>`, where `<display>` is what `repos --version` prints under the same environment and the footer shows, empty when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; and `Description`, `Git repositories for the suite's content`, the manifest's description. After the list is the link `Back to repos`, leading to `/`. The about screen has no `MCP tools` or `Clone with git` section.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file, and telemetry takes every event.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id repos gave the request:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/about"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A user on a host with services opens the launcher

On a host, the services file carries the icon of every service whose package ships one, and the launcher is how a user gets from repos to any of them without typing an address. Here `/var/lib/ikigenba/services.json` is the suite's services file with an `icon` on each of its five entries, `auth`, `dummy`, `mcp`, `repos`, and `telemetry`, each holding the SVG text of that service's icon; repos' own is the one its package ships, `share/icon.svg` (`S16`). The launcher lists every service that carries an icon, whether or not it is an MCP service.

In a browser, the list is closed when the page loads, and pressing the launcher button opens it. Typing in the search field keeps only the entries whose name contains the typed text, ignoring case and any spaces around it; clearing the field shows them all again. When the text matches no entry, the no-match line appears, reading `No service matches “<text>”.` with the typed text in quotation marks. Pressing Enter in the search field opens the first entry still shown that is a working link, and does nothing when there is none. That filtering is the whole of what `/_appkit/launcher.js` does: every entry, and the no-match line, arrived with the page.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, with the same banner, and the banner also holds the launcher button labelled `Services`. The page carries the list of services labelled `Services`, holding the search field labelled `Find a service` with the placeholder `Find a service` and five entries in the file's order, each showing its icon and then its name: `auth`, a link to `https://auth.sbx.ikigenba.dev`; `dummy`, a link to `https://dummy.sbx.ikigenba.dev`; `mcp`, a link to `https://mcp.sbx.ikigenba.dev`; `repos`, a link to `https://repos.sbx.ikigenba.dev`, marked as the current page; and `telemetry`, a link to `https://telemetry.sbx.ikigenba.dev`. The no-match line is in the page and hidden. The page loads the script `/_appkit/launcher.js`. The about screen carries the same launcher.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file with an `icon` string on each entry.

Postconditions:

- Nothing has changed. The services file is as it was.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A broken services file never breaks a page, so repos draws the page without a launcher and reports nothing about the file: this is not a fault of the request, and the answer is the same as when no file is named at all. A file that exists but cannot be read, is not a JSON object with a `services` array, or has no usable entry that carries an icon is answered the same way, as far as the launcher goes. With no file there is no `auth` entry and no `repos` entry either, so auth's links, the clone address, and the credentials text are read from the request's `Host`, which here names the same space, so the page reads as it does with the file.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`: the profile link leading to `https://auth.sbx.ikigenba.dev/`, the sign-out button POSTing to `https://auth.sbx.ikigenba.dev/logout`, the `Clone with git` section naming `https://repos.sbx.ikigenba.dev/<name>.git` and holding the credentials text for `https://repos.sbx.ikigenba.dev`, no launcher button, no list of services, no `Find a service` field, no no-match line, and no `/_appkit/launcher.js`.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` does not exist.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr about the services file. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as a `repos: undelivered event: <event>` line, and nothing else for this request.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched on or off, and repos reads the file afresh for every page, so the next page a user loads shows the new list without repos being restarted. Here the host has switched `dummy` off since repos started: `/var/lib/ikigenba/services.json` is the file of `A user on a host with services opens the launcher` with `dummy`'s `enabled` now `false`.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
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

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment while `/var/lib/ikigenba/services.json` held the file of `A user on a host with services opens the launcher`, with `dummy` switched on, and it has not been restarted since.
- `/var/lib/ikigenba/services.json` now lists `dummy` with `enabled` `false`.

Postconditions:

- Nothing has changed.

## A user on a host whose services file names no auth still gets auth's links

The banner's profile link and sign-out form address auth on the space the request names in its `Host` when the services file has no `auth` entry to take them from, so one repos build serves whichever space it is installed on. Here `/var/lib/ikigenba/services.json` is the suite's services file without the `auth` entry.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
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

Status 200. The body is the landing page of `A user opens the landing page`, and for every form above its banner's profile link, titled `mg@example.com`, leads to `https://auth.sbx.ikigenba.dev/`, and the sign-out button reading `Sign out` is in a form whose method is `post` and whose action is `https://auth.sbx.ikigenba.dev/logout`. With `X-Forwarded-Proto: http` and `Host: repos.sbx.ikigenba.dev`, the link leads to `http://auth.sbx.ikigenba.dev/` and the form's action is `http://auth.sbx.ikigenba.dev/logout`. The file still has its `repos` entry, so the `Clone with git` section is the one of `A user opens the landing page` for every form. The about screen's banner is built the same way.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file without the entry named `auth`.

Postconditions:

- Nothing has changed.
- repos set no cookie.

## A user on a host whose services file names no repos reads the clone address from the request

The clone address and the credentials text come from the services file's `repos` entry when there is one, since that is the address the host publishes for repos. Without it, repos names the address the request itself reached it at, so a user is never shown an address their own browser did not just use. Unlike auth's links, the `Host` is kept whole, port and all: it is repos' own address, not a sibling's. Here `/var/lib/ikigenba/services.json` is the suite's services file without the `repos` entry.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
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

```
GET / HTTP/1.1
Host: repos.wip.localhost:7400
X-Forwarded-Proto: http
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, but for its `Clone with git` section, whose clone address and credential scope follow the `Host` as it arrived:

- `Host: repos.sbx.ikigenba.dev:443`: the section reads `Clone a repository you own from https://repos.sbx.ikigenba.dev:443/<name>.git, where <name> is its name.`, and the command begins `git config --global credential.https://*.sbx.ikigenba.dev:443.helper`.
- `Host: repos.sbx.ikigenba.dev` with no `X-Forwarded-Proto`: `https://repos.sbx.ikigenba.dev/<name>.git`, and `credential.https://*.sbx.ikigenba.dev.helper`.
- `Host: sbx.ikigenba.dev`: `https://sbx.ikigenba.dev/<name>.git`, and `credential.https://*.sbx.ikigenba.dev.helper`, since a host with no `repos.` label is the space whole.
- `Host: repos.wip.localhost:7400` over `http`: `http://repos.wip.localhost:7400/<name>.git`, and `credential.http://*.wip.localhost:7400.helper`.

The rest of the credentials text is the same for every form. The file still has its `auth` entry, so auth's links are `https://auth.sbx.ikigenba.dev/` and `https://auth.sbx.ikigenba.dev/logout` for every form.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file without the entry named `repos`.

Postconditions:

- Nothing has changed.
- repos set no cookie.

## A request arrives without the identity headers

In production this cannot happen from outside: the gate sets the headers on every request it forwards, a sibling forwards the ones it received, and nothing but nginx and the suite's apps can reach repos' socket. So a request without `X-User-Id` says the gate or a sibling is misconfigured, which is repos' fault to report, not the caller's to fix — hence a 500 and not a 400 or a 401. A developer meets it by forgetting the headers, as here. repos has no route that takes no identity.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`, ending in a newline. A `HEAD` is answered with the same status and headers and an empty body. An `X-User-Id` header whose value is empty is answered the same way as no header at all. Every path but `/events` and `/declarations` answers this way — `/`, `/about`, the shared files under `/_appkit/` (`S04`), the MCP endpoint `/mcp` (`S05`), the git paths (`S11`), and every path that does not exist; what repos answers at `/events` and `/declarations`, the events app's two paths on its socket (`S17-on-a-space.md`), is not this story's. Elsewhere the identity check runs before repos looks at the path or the method, so a request with no identity is never a 404 or a 405, and no git runs for it.

Preconditions:

- repos is serving, and telemetry takes every event.
- The request carries no `X-User-Id` header, or one whose value is empty, and no `X-Request-Id` header.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr. The 500 is a handled failure, so it is in the trail and not the journal: telemetry has received the request's two events, with an empty user, under the id repos gave the request, 32 lowercase hexadecimal characters:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A request from nginx arrives without the identity headers

nginx sets `X-Request-Id` on every request it forwards, so when the gate is misconfigured and forwards a request without `X-User-Id`, repos records the 500 under the id nginx gave the request, and the operator who finds the 500 in the trail can find the same request in nginx's log. The developer here stands in for such an nginx by sending the id by hand.

Request:

```
GET / HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the same one line, `identity header missing`, ending in a newline.

Preconditions:

- repos is serving, and telemetry takes every event.
- The request carries `X-Request-Id` and no `X-User-Id` header.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr. telemetry has received the request's two events under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user, so a trace of that id finds them:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A caller asks for a path that does not exist

To its callers repos serves the landing page, the about screen, the shared files, the MCP endpoint, and the git paths. Beside them it has two paths for the event bus, the `events` app, `/events` and `/declarations`, which the events app reaches on repos' socket and which never reach repos through nginx (`S17-on-a-space.md`); what repos answers at them is not this story's. Every other path is one repos does not serve; it has no page of its own to send a lost caller to, so a path it does not serve is answered with one line of plain text, whatever the method. The MCP endpoint is `/mcp` alone (`S05`): `/mcp/` or any path beneath it is a path that does not exist like any other. A path whose first segment does not end in `.git` is never a git path, so `/notes` and `/notes/info/refs` are paths that do not exist here, while `/notes.git/...` is `S11`'s to answer. repos serves nothing under `/assets/`; the pages' stylesheet is under `/_appkit/` (`S04`).

Request:

```
GET /nope HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /assets/theme.css HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /mcp/ HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /notes/info/refs?service=git-upload-pack HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
POST /nope HTTP/1.1
Host: repos.sbx.ikigenba.dev
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

- repos is serving, and the caller owns a repository named `notes`.

Postconditions:

- Nothing has changed. No git ran.
- repos wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 404.

## A caller sends a page a method it does not take

The pages only read, so `/` and `/about` take `GET` and `HEAD` and nothing else, and `Allow` names those two. No page is sent. The MCP endpoint and the git paths refuse a method they do not take in their own way (`S05`, `S11`).

Request:

```
POST / HTTP/1.1
Host: repos.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /about HTTP/1.1
Host: repos.sbx.ikigenba.dev
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

- repos is serving.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 405.
