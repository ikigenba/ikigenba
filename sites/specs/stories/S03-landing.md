# Stories — landing

The landing page, the about screen, and the frame they are drawn in: what a running sites answers at `/` and `/about`. The landing page, at `/`, tells a signed-in user what sites is, lists the space's sites they may see, names the seven tools an agent manages sites with, and says at what address every site answers; the about screen, at `/about`, shows sites' name, its version, and its description. Both are server-rendered HTML drawn from sites' templates `landing` and `about`, and the whole of each page's content arrives in the response body, the launcher's list included; neither page carries a script of its own, and no script adds content a user sees: the launcher's, `/_appkit/launcher.js`, which a page loads only with the launcher, filters the launcher's list as the user types; and the platform's button feedback script, `/_appkit/feedback.js`, which every page loads with or without a launcher, makes an enabled button visibly react when the user presses it. The host's nginx lets guests through to sites' pages (its manifest sets `guests = true`; opsctl's `S5-nginx.md`, sandbox's `S4-routing.md`): a request from a signed-in user carries `X-User-Id` and `X-User-Email`, and a guest's carries neither, while every request nginx forwards carries the request's id in `X-Request-Id`. A request whose `X-User-Id` is absent or empty is a guest's, whatever `X-User-Email` it carries. The two pages are for signed-in users only, and a guest who asks for either is sent to auth's sign-in, with the page's URL to come back to. The requests go to a running sites (`S02`), each shown as the HTTP request sites receives, with the headers the story depends on; a developer stands in for nginx by passing those headers by hand. sites is started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, and that file, the host's services file, holds the suite's services file unless a story says otherwise:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "https://dummy.sbx.ikigenba.dev", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true },
    { "name": "mcp", "url": "https://mcp.sbx.ikigenba.dev", "description": "Connect AI assistants to your services", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false },
    { "name": "repos", "url": "https://repos.sbx.ikigenba.dev", "description": "Git repositories for the suite's content", "socket": "/run/ikigenba/repos.sock", "enabled": true, "mcp": true },
    { "name": "sites", "url": "https://sites.sbx.ikigenba.dev", "description": "Static sites from the suite's repositories", "socket": "/run/ikigenba/sites.sock", "enabled": true, "mcp": true },
    { "name": "telemetry", "url": "https://telemetry.sbx.ikigenba.dev", "description": "The suite's trail of events", "socket": "/run/ikigenba/telemetry.sock", "enabled": true, "mcp": true }
  ]
}
```

sites takes the file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows on the next page without a restart. The file's shape, and how sites treats an entry or a file it cannot use, are exactly repos' (repos' `S03-landing.md`): with no variable, no readable file, or a file that is not a JSON object with a `services` array, sites treats the file as listing no services and writes nothing about it, since a broken services file never breaks a page. The same file names the telemetry service sites sends its trail to (`S02`); in this group telemetry takes every event unless a story says otherwise.

Both pages are drawn in the banner every app of the platform draws, exactly as repos draws it (repos' `S03-landing.md`), with `sites` as the service it names: the mark, whose text is `ikigenba` and which names the service `sites`, so a browser shows `ikigenba │ sites`; the profile link, the `user-circle` icon labelled `Profile` and titled with the caller's `X-User-Email` value exactly as it arrived; the sign-out button reading `Sign out`, a form that POSTs to `<auth-logout>`; and, when the services file lists services that carry an icon, the launcher button. The service's name is lowercase `sites` everywhere it appears. The landing page's title is `sites`, and the about screen's is `About sites`. `<auth-profile>` is the `url` of the services file's entry named `auth`, followed by `/`, and `<auth-logout>` that `url` followed by `/logout`; with no such entry, or an empty `url`, sites reads the space from the request's `Host`, dropping a trailing port and then a single leading `sites.` label, so `<auth-profile>` is `<scheme>://auth.<space>/` and `<auth-logout>` is `<scheme>://auth.<space>/logout`, the scheme being `X-Forwarded-Proto` when it is `http` or `https` and `https` otherwise. sites serves no logout route, and neither page sets a cookie.

`<sites-url>`, the address every site answers under, is the `url` of the services file's entry named `sites`, with one trailing `/` dropped if it has one, so `https://sites.sbx.ikigenba.dev` for the suite's services file. With no such entry, or an empty `url`, it is `<scheme>://<Host>`, the `Host` exactly as it arrived, port kept, and the scheme chosen as for auth's links. A site's URL is `<sites-url>/<slug>/`, the same URL `show` gives (`S07`).

The landing page lists the sites the caller may see: every listed site in the space, whoever owns it, and the caller's own unlisted sites, sorted by name. Each row is one site, `tr[data-site=<slug>]`, holding its name as a link (`a.site-link`) to its URL; its visibility as a badge (`span.badge[data-kind=public]` reading `public`, or `span.badge[data-kind=private]` reading `private`); the badge `unlisted` (`span.badge[data-kind=unlisted]`) only when the site is not listed; its state (`span.status[data-status=published]` reading `published` when a commit is published, `span.status[data-status=unpublished]` reading `not published` when none is); and the badge `yours` (`span.badge[data-kind=mine]`) only when the caller owns it. Another user's unlisted site is never on the page. The stories share `S06`'s shared catalog, summarised below, and the repositories it names are repos'; serving a page reads the catalog and never a repository, and runs no git.

| name | id | owner | slug | visibility | listed | published commit |
|---|---|---|---|---|---|---|
| `blog` | `sit_4e7a1c9b0d2f8635` | `u_7f3a9c21` | `blog` | public | yes | `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` |
| `handbook` | `sit_9a3c5e7b1d0f2468` | `u_7f3a9c21` | `handbook` | private | yes | `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d` |
| `recipes` | `sit_6b1d3f5a7c9e0284` | `u_2b8e1d04` | `recipes` | public | yes | `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92` |
| `scratch` | `sit_2d6f8a0c4e1b3957` | `u_7f3a9c21` | `scratch-7c1e9a4f` | public | no | none |

The launcher is the banner's way to the platform's other services, and it is the same launcher every app draws, with the same text and behaviour as repos' (repos' `S03-landing.md`): it offers only the services file's entries that carry an icon, in the file's order; with no such entry the page has no launcher and is otherwise the same page. sites' own entry, the one named `sites`, is marked as the current page. A page loads `/_appkit/launcher.js` only when the launcher is there.

Each page links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport. The stylesheet, its fonts, the launcher's script, the button feedback script, and the icon are the platform's shared files, which sites serves under `/_appkit/` to guests and users alike (`S04`); a page makes no request to any third party. Last on each page is the footer, whose text is `sites <display>`: the service's name, one space, and `<display>`, the string `sites --version` prints under the environment sites was started with (`S01`), exactly as it prints it, so a user can tell which code is serving the page. No story fixes its value. When that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`, `<display>` is empty and the footer's text is `sites` and the one space.

The routes sites answers are `/`, the landing page, and `/about`, the about screen, each taking `GET` and `HEAD`; `/_appkit/<name>`, the platform's shared files (`S04`); `/mcp`, exactly that path, the MCP endpoint, whose answers are `S05`'s; and every other path, whose first segment is a site's slug (`S11`, `S12`). sites has no plain-text `not found`: `/nope`, `/about/`, and `/mcp/tools` each name the slug of a site that does not exist (`about`, `mcp`, and `api` can never be a site's name, `S06`), and are answered with sites' not-found page as `S11` tells. A request whose `Host`, port dropped, contains a `.` and does not begin with `sites.` is a request at the apex host, which `S13` answers whatever its path, `/` and `/about` included. Every request sites answers here adds exactly two events to its trail (`S02`): `request.started`, with the `method` and the `path` as the request arrived, and `request.finished`, once the answer is complete, with the `status`, `duration_us`, `request_bytes`, and `response_bytes`, under the request's id and the caller's user, empty for a guest. Neither page records a `site.viewed` (`S14`). While telemetry takes every event, sites writes nothing to stderr for any answer in this group. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the landing page

A user who lands on sites' name, from the launcher or by typing it, learns what the service is, which sites the space has, which of them are theirs, and which tools an agent manages them with. Everything a person looking at a browser sees is in the document that arrives, so a reader who fetches the page with `curl` has it all.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `sites` that links `/_appkit/theme.css` as its stylesheet, links `/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, and declares the phone-width viewport. Its banner holds the mark, whose text is `ikigenba` and which names the service `sites`; the profile link, labelled `Profile` and titled `mg@example.com`, leading to `https://auth.sbx.ikigenba.dev/`; and the sign-out button reading `Sign out` in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`; in a browser, that button visibly reacts as the user presses it. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`, since no entry in the file carries an icon. Beneath the banner is the page's top-level heading (`h1`), `sites`, then the summary (`p#summary`) `Static sites for the suite, served from repositories that repos holds. An agent creates a site from one of your repositories and publishes it at a commit; a public site is open to anyone, and a private one asks a visitor to sign in to this space.`

Below that is the section `section#sites`, headed `Sites`, whose `p#site-url` reads `Every site answers at https://sites.sbx.ikigenba.dev/<slug>/, where <slug> is its slug.`, its code element holding `https://sites.sbx.ikigenba.dev/<slug>/`. Under it is the table `table#site-list` with exactly four rows, in this order:

- `tr[data-site=blog]`: the link `blog` to `https://sites.sbx.ikigenba.dev/blog/`; the badge `public`; no `unlisted` badge; the state `published`; the badge `yours`.
- `tr[data-site=handbook]`: the link `handbook` to `https://sites.sbx.ikigenba.dev/handbook/`; the badge `private`; no `unlisted` badge; the state `published`; the badge `yours`.
- `tr[data-site=recipes]`: the link `recipes` to `https://sites.sbx.ikigenba.dev/recipes/`; the badge `public`; no `unlisted` badge; the state `published`; no `yours` badge, since `ann@example.com` owns it.
- `tr[data-site=scratch-7c1e9a4f]`: the link `scratch` to `https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/`; the badge `public`; the badge `unlisted`; the state `not published`; the badge `yours`.

The page has no `p#no-sites`. Below that is the section headed `MCP tools`, reading `Agents manage sites with these tools, through the MCP gateway's call and mutate.`, and the list `dl#tools` of exactly seven tools, in this order, each a `dt[data-tool=<name>]` holding the tool's name in a code element, followed by a `dd` holding the first line of its description (`S05`): `list`, `The sites you own, by name.`; `show`, `One of your sites, with its URL, its repository, and what is published.`; `create`, `Create a site from one of your repositories and return it; publish it to make it live.`; `publish`, `Publish one of your sites at a commit of its repository: the ref it tracks, or a ref or commit you name.`; `update`, `Change one of your sites' visibility, whether it is listed, or the ref it tracks.`; `delete`, `Delete one of your sites; its repository is untouched.`; and `apex`, `Show, set or clear the site the space's apex domain redirects to.` After the section is the link `a#about-link`, `About sites`, leading to `/about`. Last on the page is the footer reading `sites v<semver>`, where `v<semver>` is what `sites --version` prints. The address `mg@example.com` is not in the page's visible text, and no site's id, repository, or commit is on the page.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file and is readable by sites, and telemetry takes every event.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed. The catalog was read and not written, no repository was read, and no git ran.
- sites wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id sites gave the request, 32 lowercase hexadecimal characters, since the request carried no `X-Request-Id`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `response_bytes` is the length of the page's body. The caller's email and the sites' names are in neither, and no `site.viewed` was recorded.

## Another user opens the landing page and sees only listed sites of others

The page is the space's, not one user's: another signed-in user sees every listed site, the ones `mg@example.com` owns among them, private ones included, since a private site serves any signed-in user of the space (`S12`). What they do not see is `mg@example.com`'s unlisted site, whose slug is known only to those it was given to; its own owner is the one other person the page shows it to.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, but for its banner, whose profile link is titled `ann@example.com`, and its table `table#site-list`, which has exactly three rows, in this order: `tr[data-site=blog]`, `public`, `published`, no `yours` badge; `tr[data-site=handbook]`, `private`, `published`, no `yours` badge; and `tr[data-site=recipes]`, `public`, `published`, with the badge `yours`. No row has the `unlisted` badge. Neither `scratch` nor `scratch-7c1e9a4f` is anywhere in the page.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, and that file holds the suite's services file.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events under user `u_2b8e1d04`, the `request.finished` with `status` 200.

## A user opens the landing page of a space with no sites

Before anyone has made a site, the page says so in the place the list would be, and still says how sites are made and where they will answer.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, but the section `section#sites` holds, after its heading `Sites` and its `p#site-url`, the line `p#no-sites` reading `No sites yet.` and no `table#site-list`. The `MCP tools` section, its seven tools, and the link `About sites` are as on that page. The page is the same when the catalog holds sites but none the caller may see: only other users' unlisted sites.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, and that file holds the suite's services file.
- The catalog holds no site.

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user's client asks for the landing page's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body. A monitor or a proxy learns sites is up without paying for the page. `/about` answers `HEAD` the same way.

Request:

```
HEAD / HTTP/1.1
Host: sites.sbx.ikigenba.dev
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

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- The request's `request.started` has `method` `HEAD`, and its `request.finished` has `status` 200 and `response_bytes` 0, since no body was written.

## A user opens the about screen

The about screen is where a user checks which code is serving and what the service calls itself. Its three facts are sites' own: the name, `<display>`, the string `sites --version` prints under the environment sites was started with, and the description the manifest declares (`S01`), the same line the host publishes in its services file. The screen reads none of them from the services file, so it says the same with or without one, and it reads no catalog.

Request:

```
GET /about HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `About sites`, with the same stylesheet link, icon link, feedback script, viewport, banner, and footer as the landing page of `A user opens the landing page`. Beneath the banner is the page's top-level heading, `About sites`, then the list `dl#about` of exactly three facts, in this order, each a label and its value: `Name`, `sites` (`dd#about-name`); `Version`, `<display>` (`dd#about-version`), where `<display>` is what `sites --version` prints under the same environment and the footer shows, and the `dd` is empty when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; and `Description`, `Static sites from the suite's repositories` (`dd#about-description`), the manifest's description. After the list is the link `a#home-link`, `Back to sites`, leading to `/`. The about screen has no `Sites` or `MCP tools` section.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file, and telemetry takes every event.

Postconditions:

- Nothing has changed. The catalog was not read.
- sites wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id sites gave the request:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/about"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A guest asks for the landing page and is sent to sign in

The landing page lists the space's sites, private ones among them, and the about screen belongs with it, so both are for signed-in users. A guest — a browser with no session, which nginx lets through because sites serves guests — is sent to auth's sign-in exactly as at a private site (`S12`), carrying the URL it asked for so auth can bring it back once it has signed in. auth is at `auth.` followed by the space, the space being the request's `Host` with its leading `sites.` removed, port kept; the scheme is `X-Forwarded-Proto` when it is `http` or `https` (in any letter case) and `https` otherwise. The URL to return to is `<scheme>://<Host><path and query>`, percent-encoded as a query component. The request carries no `X-User-Id`; one with an empty `X-User-Id`, or an `X-User-Email` and no `X-User-Id`, is a guest's all the same.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
HEAD / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2F
```

Status 302. No story fixes the body. The response sets no cookie. `GET /about` and `HEAD /about` are answered the same way with `Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fabout`, and `GET /?from=launcher` with `Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2F%3Ffrom%3Dlauncher`. With `X-Forwarded-Proto: http`, both the sign-in address and the URL to return to are `http`: `Location: http://auth.sbx.ikigenba.dev/?return=http%3A%2F%2Fsites.sbx.ikigenba.dev%2F`.

Preconditions:

- sites is serving, and telemetry takes every event.
- The request carries no `X-User-Id` and no `X-User-Email`, as nginx forwards a guest's request, and the `X-Request-Id` nginx gave it.

Postconditions:

- Nothing has changed. The catalog was not read, and no page was drawn.
- sites wrote nothing to stderr. telemetry has received the request's two events, under the id nginx gave the request and an empty user, and no `site.viewed`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":302}}
  ```

  For the `HEAD`, the events are the same but that `request.started` has `method` `HEAD` and `request.finished` has `response_bytes` 0, since no body was written.

## A user on a host with services opens the launcher

On a host, the services file carries the icon of every service whose package ships one, and the launcher is how a user gets from sites to any of them without typing an address. Here `/run/ikigenba/services.json` is the suite's services file with an `icon` on each of its six entries, `auth`, `dummy`, `mcp`, `repos`, `sites`, and `telemetry`, each holding the SVG text of that service's icon; sites' own is the one its package ships, `share/icon.svg` (`S20`). The launcher lists every service that carries an icon, whether or not it is an MCP service. In a browser the list, its search field, and its no-match line behave exactly as repos' launcher does (repos' `S03-landing.md`).

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, with the same banner, and the banner also holds the launcher button labelled `Services`. The page carries the list of services labelled `Services`, holding the search field labelled `Find a service` with the placeholder `Find a service` and six entries in the file's order, each showing its icon and then its name: `auth`, a link to `https://auth.sbx.ikigenba.dev`; `dummy`, a link to `https://dummy.sbx.ikigenba.dev`; `mcp`, a link to `https://mcp.sbx.ikigenba.dev`; `repos`, a link to `https://repos.sbx.ikigenba.dev`; `sites`, a link to `https://sites.sbx.ikigenba.dev`, marked as the current page; and `telemetry`, a link to `https://telemetry.sbx.ikigenba.dev`. The no-match line is in the page and hidden. The page loads the script `/_appkit/launcher.js`. The about screen carries the same launcher.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file with an `icon` string on each entry.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed. The services file is as it was.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A broken services file never breaks a page, so sites draws the page without a launcher and reports nothing about the file. With no file there is no `auth` entry and no `sites` entry either, so auth's links and `<sites-url>` are read from the request's `Host` and `X-Forwarded-Proto`, which here name the same space and scheme, so the page reads as it does with the file. A file that exists but cannot be read, or is not a JSON object with a `services` array, is answered the same way.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`: the profile link leading to `https://auth.sbx.ikigenba.dev/`, the sign-out button POSTing to `https://auth.sbx.ikigenba.dev/logout`, `p#site-url` naming `https://sites.sbx.ikigenba.dev/<slug>/`, each site's link under `https://sites.sbx.ikigenba.dev/`, no launcher button, no list of services, no `Find a service` field, no no-match line, and no `/_appkit/launcher.js`.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` does not exist.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.
- sites wrote nothing to stderr about the services file. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as a `sites: undelivered event: <event>` line, and nothing else for this request.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched on or off, and sites reads the file afresh for every page, so the next page a user loads shows the new list without sites being restarted. Here the host has switched `dummy` off since sites started: `/run/ikigenba/services.json` is the file of `A user on a host with services opens the launcher` with `dummy`'s `enabled` now `false`.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
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

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment while `/run/ikigenba/services.json` held the file of `A user on a host with services opens the launcher`, with `dummy` switched on, and it has not been restarted since.
- `/run/ikigenba/services.json` now lists `dummy` with `enabled` `false`.

Postconditions:

- Nothing has changed.

## A user on a host whose services file names no auth still gets auth's links

The banner's profile link and sign-out form address auth on the space the request names in its `Host` when the services file has no `auth` entry to take them from, so one sites build serves whichever space it is installed on. Here `/run/ikigenba/services.json` is the suite's services file without the `auth` entry.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: HTTPS
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, and for every form above its banner's profile link, titled `mg@example.com`, leads to `https://auth.sbx.ikigenba.dev/`, and the sign-out button reading `Sign out` is in a form whose method is `post` and whose action is `https://auth.sbx.ikigenba.dev/logout`. With `X-Forwarded-Proto: http` and `Host: sites.sbx.ikigenba.dev`, the link leads to `http://auth.sbx.ikigenba.dev/` and the form's action is `http://auth.sbx.ikigenba.dev/logout`. The file still has its `sites` entry, so `p#site-url` and every site's link are those of `A user opens the landing page` for every form. The about screen's banner is built the same way.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file without the entry named `auth`.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.
- sites set no cookie.

## A user on a host whose services file names no sites reads the sites' address from the request

Each site's address comes from the services file's `sites` entry when there is one, since that is the address the host publishes for sites. Without it, sites names the address the request itself reached it at, so a user is never shown an address their own browser did not just use. The `Host` is kept whole, port and all: it is sites' own address, not a sibling's. Here `/run/ikigenba/services.json` is the suite's services file without the `sites` entry.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: sites.wip.localhost:7400
X-Forwarded-Proto: http
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page of `A user opens the landing page`, but for `p#site-url` and the sites' links, which follow the `Host` as it arrived:

- `Host: sites.sbx.ikigenba.dev:443`: `p#site-url`'s code element holds `https://sites.sbx.ikigenba.dev:443/<slug>/`, and `blog`'s link is `https://sites.sbx.ikigenba.dev:443/blog/`.
- `Host: sites.sbx.ikigenba.dev` with no `X-Forwarded-Proto`: `https://sites.sbx.ikigenba.dev/<slug>/`, and `https://sites.sbx.ikigenba.dev/blog/`.
- `Host: sites.wip.localhost:7400` over `http`: `http://sites.wip.localhost:7400/<slug>/`, and `http://sites.wip.localhost:7400/blog/`.

Every other site's link follows the same address. The file still has its `auth` entry, so auth's links are `https://auth.sbx.ikigenba.dev/` and `https://auth.sbx.ikigenba.dev/logout` for every form.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file without the entry named `sites`.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.
- sites set no cookie.

## A user opens the landing page while sites cannot reach its catalog

The landing page lists sites from the catalog, so when sites cannot read it there is nothing true to list. It does not draw the page as if the space had no sites, which would tell the user their sites were gone; it says plainly that it cannot reach the catalog, quoting nothing of the database's own error, and the user may try again later. The about screen reads no catalog and is still served.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
```

Status 503. The body is exactly the one line `cannot reach the catalog; try again later`, ending in a newline. It has no banner and no footer. A `HEAD` is answered with the same status and headers and an empty body.

Preconditions:

- sites is serving, and telemetry takes every event.
- sites' database cannot be read: `state/sites.db` has become unreadable since sites opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed.
- sites wrote nothing to stderr. telemetry has received the request's two events under user `u_7f3a9c21`, the `request.finished` with `status` 503.
- sites is still serving.

## A caller sends a page a method it does not take

The pages only read, so `/` and `/about` take `GET` and `HEAD` and nothing else, and `Allow` names those two. No page is sent. The method is looked at before the caller, so a guest is refused the same way and is not sent to sign in. The MCP endpoint and the site paths refuse a method they do not take in their own way (`S05`, `S11`).

Request:

```
POST / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /about HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
POST / HTTP/1.1
Host: sites.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. `PUT`, `PATCH`, and every other method but `GET` and `HEAD` are refused the same way, on both pages, from a user and a guest alike.

Preconditions:

- sites is serving.

Postconditions:

- Nothing has changed. No cookie was set.
- sites wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 405, under the caller's user, empty for the guest.
