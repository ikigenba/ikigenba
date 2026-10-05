# Stories — landing

The catalog, the about screen, and the frame scripts' pages are drawn in: what a running scripts answers at `/` and `/about`, and the rules every route of scripts shares. The catalog, at `/`, tells a signed-in user what scripts is, lists their own scripts with each one's repository, ref and last run, and names the nine tools an agent manages and runs scripts with; the about screen, at `/about`, shows scripts' name, its version, and its description. Both are server-rendered HTML drawn from scripts' templates `landing` and `about`, and the whole of each page's content arrives in the response body, the launcher's list included; neither page carries a script of its own, and the one script either may load is the launcher's, `/_appkit/launcher.js`, which only filters the launcher's list as the user types. Every page of scripts is for a signed-in user, and scripts serves nothing to guests: its manifest declares no `guests` (`S01`), so on a host with an authenticator nginx sends a visitor with no credential to auth's sign-in before the request reaches scripts. nginx sets `X-User-Id` and `X-User-Email` on every request it passes upstream, the caller auth authenticated, with the request's id in `X-Request-Id`, and scripts trusts those headers (`S02`). So a request that arrives without `X-User-Id`, or with it empty, means nginx or a sibling is misconfigured — a server fault, not a bad request — and every route of scripts answers it the same way, before it looks at the path or the method (below). The requests go to a running scripts (`S02`), each shown as the HTTP request scripts receives, with the headers the story depends on; a developer stands in for nginx by passing those headers by hand. scripts is started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file, the host's services file, holds the suite's services file unless a story says otherwise:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "https://dummy.sbx.ikigenba.dev", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true },
    { "name": "mcp", "url": "https://mcp.sbx.ikigenba.dev", "description": "Connect AI assistants to your services", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false },
    { "name": "repos", "url": "https://repos.sbx.ikigenba.dev", "description": "Git repositories for the suite's content", "socket": "/run/ikigenba/repos.sock", "enabled": true, "mcp": true },
    { "name": "scripts", "url": "https://scripts.sbx.ikigenba.dev", "description": "Python scripts run from the suite's repositories", "socket": "/run/ikigenba/scripts.sock", "enabled": true, "mcp": true },
    { "name": "sites", "url": "https://sites.sbx.ikigenba.dev", "description": "Static sites from the suite's repositories", "socket": "/run/ikigenba/sites.sock", "enabled": true, "mcp": true },
    { "name": "telemetry", "url": "https://telemetry.sbx.ikigenba.dev", "description": "The suite's trail of events", "socket": "/run/ikigenba/telemetry.sock", "enabled": true, "mcp": true }
  ]
}
```

scripts takes the file's path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows on the next page without a restart. The file is a JSON object whose `services` member is an array; each entry is an object with `name`, a non-empty string; `url`, `description`, and `socket`, strings; `enabled`, `true` or `false`, `false` for a service switched off; `mcp`, `true` or `false`; and, optionally, `icon`, a string holding the SVG text of the service's icon. Members scripts does not know are ignored, and an entry that lacks one of the six others, or holds one of the wrong kind, is left out while the rest are still used. With no variable, no readable file, or a file that is not such an object, scripts treats the file as listing no services and writes nothing about it, since a broken services file never breaks a page. The same file names the telemetry service scripts sends its trail to (`S02`); in this group telemetry takes every event unless a story says otherwise.

Both pages are drawn in the banner every app of the platform draws, at the top of the page. It holds the mark, whose text is `ikigenba` and which names the service it fronts, `scripts`, so a browser shows `ikigenba │ scripts`; a profile link; a sign-out button; and, when the services file lists services that carry an icon, the launcher button (below). The service's name is lowercase `scripts` everywhere it appears. The catalog's title, the one a browser shows on its tab, is `scripts`, and the about screen's is `About scripts`. The catalog is the root of scripts' pages, so it has no breadcrumb, and neither has the about screen; the pages beneath the catalog carry one (`S12`, `S13`). The profile link is the `user-circle` icon with no text of its own, labelled `Profile` for assistive technology and titled with the caller's `X-User-Email` value, exactly as it arrived, so hovering it shows who is signed in; the email is in no visible text on the page. The sign-out button is a form, not a link: pressing it POSTs to `<auth-logout>`, and its `logout` icon is drawn before the text and hidden from assistive technology, so its accessible text is `Sign out` alone. Following the profile link or submitting the form leaves scripts for auth; what auth does there is auth's own. scripts serves no logout route, and no page of scripts sets a cookie.

`<auth-profile>`, where the banner's profile link leads, is auth's profile page: the `url` of the services file's entry named `auth`, followed by `/`, so `https://auth.sbx.ikigenba.dev/` for the suite's services file; `<auth-logout>` is that same `url` followed by `/logout`. When the file has no entry named `auth`, or that entry's `url` is empty, scripts reads the space from the request's own `Host`: a trailing port is dropped, then a single leading `scripts.` label; what remains is the space, `<auth-profile>` is `<scheme>://auth.<space>/`, and `<auth-logout>` is `<scheme>://auth.<space>/logout`. The scheme is the request's `X-Forwarded-Proto` when that header is exactly `http` or exactly `https`, and `https` otherwise — `HTTPS`, `https, http`, an empty value, or no header at all. A `Host` with no `scripts.` label is the space whole. There is no fixed fallback address.

The launcher is the banner's way to the platform's other services, and it is the same launcher every app draws. It offers only the services file's entries that carry an icon: an entry with no `icon`, or one that is not a string, stays out of it, and with no such entry the page has no launcher and is otherwise the same page. When the launcher is there, the banner holds a launcher button labelled `Services`; pressing it opens the list, which is closed when the page loads. The list holds a search field labelled `Find a service`, with the placeholder `Find a service`, and one entry per service that carries an icon, in the file's order, each showing the service's icon and then its name. An enabled service's entry is a link to its `url`; a service switched off keeps its place but is not a working link, and is titled `<name> is unavailable`. scripts' own entry, the one named `scripts`, is marked as the current page. The list, the search field, and a hidden no-match line are all in the page as served; a page loads `/_appkit/launcher.js` only when the launcher is there.

Each page links `/_appkit/theme.css` as its stylesheet and declares the phone-width viewport, so a phone shows it at the phone's own width. The stylesheet, its fonts, and the launcher's script are the platform's shared files, which scripts serves under `/_appkit/` (`S04`); a page makes no request to any third party. Last on each page is the footer, whose text is `scripts v<semver>`: the service's name, one space, and the version `scripts --version` prints (`S01`), exactly as it prints it. The version is data, and no story fixes its value.

The catalog lists the caller's own scripts and no one else's, sorted by name, whoever else's scripts the space holds; there is no listing of another user's scripts anywhere in scripts (`S07`). Each row is one script, `tr[data-script=<name>]`, holding its name as a link (`a.script-link`) to its page, `/<name>/` (`S12`); its repository (`td.script-repo`), shown by the name repos gives it, read from the repository's directory when the page is drawn, with the repository's `rep_` id as the title of the name, or, when the directory is gone or its name cannot be read, the `rep_` id itself, muted (`span.muted`) and titled `the repository is gone`; its ref (`td.script-ref`); and its last run, the script's newest: its status (`td.script-last`) as a `span.status` whose `data-kind` is the status's kind, and its start time (`td.script-when`) as a `time` whose `datetime` is the start in RFC 3339 UTC to the second and whose text is the same moment in UTC to the minute, `2026-10-05 09:31`. A status reads as a word with a kind: `running`, kind `info`; `exited 0`, kind `ok`; `exited <n>` for a non-zero `<n>`, `timed out`, and `killed`, kind `warn`; and `failed`, kind `err`. A script that has never run has, in place of the status, the muted text `never run`, and an empty `td.script-when`. The catalog stores a script's repository by its id only, so the name the page shows is whatever repos calls the repository at that moment. The stories share `S06`'s shared catalog, summarised below; the repositories it names are repos', in `REPOS_DIR`, and now is `2026-10-05T09:32:00Z`. Drawing the catalog reads the catalog once, and runs git only to read each listed repository's name from its directory's config, `ikigenba.name`; it resolves no ref, unpacks nothing, reads no run folder, and starts no script.

| name | id | owner | repository | repository's name in repos | ref | last run | its status | started |
|---|---|---|---|---|---|---|---|---|
| `backfill` | `scr_e8f2a6c0d4b19357` | `u_7f3a9c21` | `rep_0f6a2d9e8c4b7153` | gone from repos | `main` | none | | |
| `digest` | `scr_3b7f9d1c5e0a2846` | `u_2b8e1d04` | `rep_d41c7a9e05b28f63` | `journal` | `main` | `run_0c4e8a2f6b1d9375` | `exited`, 0 | `2026-10-04T07:00:00Z` |
| `nightly-report` | `scr_6d1f4a9b2e8c7035` | `u_7f3a9c21` | `rep_9c2e4b7a1d3f8e05` | `nightly-report` | `main` | `run_8a2c6e1f9b3d5074` | `running` | `2026-10-05T09:31:40Z` |
| `rotate-keys` | `scr_5c9b1e3a7f2d4068` | `u_7f3a9c21` | `rep_7b3e9a0c5d1f2846` | `ops-tools` | `release` | `run_1e9c3a7f5b0d2864` | `failed`, `commit_missing` | `2026-10-04T22:00:00Z` |
| `sync-crm` | `scr_a2e7c4f9b1d03856` | `u_7f3a9c21` | `rep_41d8f0a6b2c97e13` | `crm-sync` | `main` | `run_6b2d8f4a0c9e1735` | `running` | `2026-10-05T09:31:00Z` |

`u_7f3a9c21` is `mg@example.com`, and `u_2b8e1d04` is `ann@example.com`.

The routes scripts answers are `/`, the catalog, and `/about`, the about screen, each taking `GET` and `HEAD`; `/_appkit/<name>`, the platform's shared files (`S04`); `/mcp`, exactly that path, the MCP endpoint, whose answers are `S05`'s; `/<name>/`, a script's page, and `/<name>`, which sends the caller there (`S12`); `/<name>/runs/<run id>/`, a run's page, and `/<name>/runs/<run id>`, which sends the caller there (`S13`); and `/<name>/runs/<run id>/input.json`, `/<name>/runs/<run id>/stdout`, `/<name>/runs/<run id>/stderr`, and `/<name>/runs/<run id>/out/<path>`, a run's files (`S14`). Every other path is answered with scripts' not-found page, status 404, as `S12`, `S13`, and `S14` tell: `/nope`, `/about/`, and `/mcp/tools` each name a script that does not exist (`about` and `mcp` can never be a script's name, `S06`), and a path naming another user's script or run is answered exactly as one naming none. Every request scripts answers here adds exactly two events to its trail (`S02`): `request.started`, with the `method` and the `path` as the request arrived, and `request.finished`, once the answer is complete, with the `status`, `duration_us`, `request_bytes`, and `response_bytes`, under the request's id and the caller's user. While telemetry takes every event, scripts writes nothing to stderr for any answer in this group, the missing-header 500 included. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens the catalog

A user who lands on scripts' name, from the launcher or by typing it, learns what the service is, which scripts are theirs, where each one runs from, how its last run went, and which tools an agent manages and runs them with. Everything a person looking at a browser sees is in the document that arrives, so a reader who fetches the page with `curl` has it all. A run still running shows as `running`; the page does not stream, and reloading it shows the run's progress.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `scripts` that links `/_appkit/theme.css` as its stylesheet and declares the phone-width viewport. Its banner holds the mark, whose text is `ikigenba` and which names the service `scripts`; the profile link, labelled `Profile` and titled `mg@example.com`, leading to `https://auth.sbx.ikigenba.dev/`; and the sign-out button reading `Sign out` in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`. The banner holds no launcher button, and the page loads no `/_appkit/launcher.js`, since no entry in the file carries an icon. The page has no breadcrumb. Beneath the banner is the page's top-level heading (`h1`), `scripts`, then the summary (`p#summary`) `Python scripts run from repositories that repos holds. An agent creates a script from one of your repositories and runs it; every run keeps its folder, output and outcome here.`

Below that is the section `section#scripts`, headed `Your scripts`, reading `Each one is a repository and a ref. A run checks the ref out and runs main.py.`, with `main.py` in a code element. Under it is the table `table#script-list`, whose columns are headed `Script`, `Repository`, `Ref`, `Last run`, and `When`, with exactly four rows, in this order:

- `tr[data-script=backfill]`: the link `backfill` to `/backfill/`; the repository `rep_0f6a2d9e8c4b7153`, muted and titled `the repository is gone`, since its directory is gone from repos; the ref `main`; in place of a last run, the muted text `never run`; and an empty `td.script-when`.
- `tr[data-script=nightly-report]`: the link `nightly-report` to `/nightly-report/`; the repository `nightly-report`, titled `rep_9c2e4b7a1d3f8e05`; the ref `main`; the status `running` (`span.status[data-kind=info]`); and the time `2026-10-05 09:31`, whose `datetime` is `2026-10-05T09:31:40Z`.
- `tr[data-script=rotate-keys]`: the link `rotate-keys` to `/rotate-keys/`; the repository `ops-tools`, titled `rep_7b3e9a0c5d1f2846`; the ref `release`; the status `failed` (`span.status[data-kind=err]`); and the time `2026-10-04 22:00`, whose `datetime` is `2026-10-04T22:00:00Z`.
- `tr[data-script=sync-crm]`: the link `sync-crm` to `/sync-crm/`; the repository `crm-sync`, titled `rep_41d8f0a6b2c97e13`; the ref `main`; the status `running` (`span.status[data-kind=info]`); and the time `2026-10-05 09:31`, whose `datetime` is `2026-10-05T09:31:00Z`.

The page has no `div#no-scripts`. Below that is the section headed `MCP tools`, reading `Agents manage and run scripts with these tools, through the MCP gateway's call and mutate.`, and the list `dl#tools` of exactly nine tools, in this order, each a `dt[data-tool=<name>]` holding the tool's name in a code element, followed by a `dd` holding the first line of its description (`S05`): `list`, `The scripts you own, by name.`; `show`, `One of your scripts, with its repository, its ref and its last run.`; `create`, `Create a script from one of your repositories and a ref.`; `update`, `Change the ref one of your scripts runs from.`; `delete`, `Delete one of your scripts and every run it has.`; `run`, `Start a run of one of your scripts and return its id, status and commit.`; `runs`, `The runs of one of your scripts, newest first.`; `result`, `One run whole: its details, its output so far, and the files it wrote.`; and `cancel`, `End one of your runs that is still running.` After the section is the link `a#about-link`, `About scripts`, leading to `/about`. Last on the page is the footer reading `scripts v<semver>`, where `v<semver>` is what `scripts --version` prints. The address `mg@example.com` is not in the page's visible text. `digest`, `journal`, and `rep_d41c7a9e05b28f63` are nowhere in the page, and no script's id, run's id, or commit is on it; of the repositories' ids, only `backfill`'s is in the visible text.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file and is readable by scripts, and telemetry takes every event.
- The catalog holds `S06`'s shared catalog, and the repositories it names are as summarised above: `rep_0f6a2d9e8c4b7153.git` is not in `REPOS_DIR`, and each of the others is there with its `ikigenba.name`.

Postconditions:

- Nothing has changed. The catalog was read and not written; git ran only to read the repositories' names, and no ref was resolved, nothing was unpacked, no run folder was read, and no script started. The two running runs run on.
- scripts wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id scripts gave the request, 32 lowercase hexadecimal characters, since the request carried no `X-Request-Id`:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `response_bytes` is the length of the page's body. The caller's email and the scripts' names are in neither.

## Another user opens the catalog and sees only their own scripts

The catalog is one user's, not the space's: another signed-in user sees their own scripts and nothing of `mg@example.com`'s, matching what the tools give them (`S07`). Script names are one namespace for the whole space (`S06`), but the page never shows whose a name is.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the catalog of `A user opens the catalog`, but for its banner, whose profile link is titled `ann@example.com`, and its table `table#script-list`, which has exactly one row: `tr[data-script=digest]`, holding the link `digest` to `/digest/`; the repository `journal`, titled `rep_d41c7a9e05b28f63`; the ref `main`; the status `exited 0` (`span.status[data-kind=ok]`); and the time `2026-10-04 07:00`, whose `datetime` is `2026-10-04T07:00:00Z`. None of `backfill`, `nightly-report`, `rotate-keys`, and `sync-crm` is anywhere in the page, and neither is any of their repositories' names or ids.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file holds the suite's services file.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed. git ran only to read the name of `rep_d41c7a9e05b28f63`; no other repository was read.
- telemetry has received the request's two events under user `u_2b8e1d04`, the `request.finished` with `status` 200.

## A user with no scripts opens the catalog

Before a user has made a script, the page says so in the place the list would be, and still names the tools that make one. Other users' scripts do not count: a space whose only scripts are someone else's shows this user the same empty catalog.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the catalog of `A user opens the catalog`, but the section `section#scripts` holds, after its heading `Your scripts` and the line beneath it, the empty state `div#no-scripts`, headed `No scripts yet` and reading `A script you create with the create tool, from one of your repositories, shows up here.`, with `create` in a code element, and no `table#script-list`. `digest` is nowhere in the page. The `MCP tools` section, its nine tools, and the link `About scripts` are as on that page. The page is the same when the catalog holds no script at all.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file holds the suite's services file.
- The catalog holds one script, `u_2b8e1d04`'s `digest`, and its run; `u_7f3a9c21` owns no script.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user's client asks for the catalog's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body. A monitor or a proxy learns scripts is up without paying for the page. `/about` answers `HEAD` the same way.

Request:

```
HEAD / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
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

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.
- The request's `request.started` has `method` `HEAD`, and its `request.finished` has `status` 200 and `response_bytes` 0, since no body was written.

## A user opens the catalog after a repository was renamed in repos

A script names its repository by id, which never changes, so a rename in repos neither breaks the script nor needs anything done in scripts (`S06`, `S20`). The page shows the name repos gives the repository now, read from its directory as the page is drawn, so the next page after the rename shows the new name without scripts being restarted. Here the owner has renamed `crm-sync` to `crm-pipeline` in repos since scripts started.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the catalog of `A user opens the catalog`, except that the row `tr[data-script=sync-crm]` shows the repository `crm-pipeline`, still titled `rep_41d8f0a6b2c97e13`. `crm-sync` is nowhere in the page. Were the directory there but its config to hold no `ikigenba.name`, or git unable to read it, the cell would show `rep_41d8f0a6b2c97e13`, muted and titled `the repository is gone`, as `backfill`'s does.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file holds the suite's services file.
- scripts was started while the catalog held `S06`'s shared catalog and `rep_41d8f0a6b2c97e13.git` was named `crm-sync`, and it has not been restarted since.
- `rep_41d8f0a6b2c97e13.git`'s config now holds the name `crm-pipeline`.

Postconditions:

- Nothing has changed. The catalog was not written: `sync-crm`'s repository is still `rep_41d8f0a6b2c97e13`, as `show` gives it (`S07`).

## A user opens the about screen

The about screen is where a user checks which release is serving and what the service calls itself. Its three facts are the binary's own: the name, the version `scripts --version` prints, and the description the manifest declares (`S01`), the same line the host publishes in its services file. The screen reads none of them from the services file, so it says the same with or without one, and it reads no catalog and runs no git.

Request:

```
GET /about HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `About scripts`, with the same stylesheet link, viewport, banner, and footer as the catalog of `A user opens the catalog`, and no breadcrumb. Beneath the banner is the page's top-level heading, `About scripts`, then the list `dl#about` of exactly three facts, in this order, each a label and its value: `Name`, `scripts` (`dd#about-name`); `Version`, `v<semver>` (`dd#about-version`), where `v<semver>` is what `scripts --version` prints and the footer shows; and `Description`, `Python scripts run from the suite's repositories` (`dd#about-description`), the manifest's description. After the list is the link `a#home-link`, `Back to scripts`, leading to `/`. The about screen has no `Your scripts` or `MCP tools` section.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file, and telemetry takes every event.

Postconditions:

- Nothing has changed. The catalog was not read, and no git ran.
- scripts wrote nothing to stderr and set no cookie.
- telemetry has received the request's two events, in this order, where `<request-id>` is the id scripts gave the request:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/about"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A visitor on a space without a credential is sent to sign in

A browser with no session asks a space for scripts' catalog. scripts serves no guests, so the space's nginx asks auth first, and on auth's refusal sends the browser to auth's sign-in with the address it asked for to come back to; the request never reaches scripts. Every page of scripts, a script's page, a run's page, and a run's files included, is answered this way, each with its own address to return to.

Request:

```
$ curl -si https://scripts.sbx.ikigenba.dev/
```

```
$ curl -si https://scripts.sbx.ikigenba.dev/nightly-report/
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https://scripts.sbx.ikigenba.dev/
```

Status 302. For the second form the `location` ends `?return=https://scripts.sbx.ikigenba.dev/nightly-report/`. No story fixes the body.

Preconditions:

- scripts `v<semver>` is deployed and active on `sbx.ikigenba.dev`, and so is auth (`S24`); scripts' manifest declares no `guests` (`S01`).
- The request carries no credential.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/scripts.sock`: scripts recorded no event and wrote nothing to stderr.

## A user on a host with services opens the launcher

On a host, the services file carries the icon of every service whose package ships one, and the launcher is how a user gets from scripts to any of them without typing an address. Here `/var/lib/ikigenba/services.json` is the suite's services file with an `icon` on each of its seven entries, `auth`, `dummy`, `mcp`, `repos`, `scripts`, `sites`, and `telemetry`, each holding the SVG text of that service's icon; scripts' own is the one its package ships, `share/icon.svg` (`S23`). The launcher lists every service that carries an icon, whether or not it is an MCP service.

In a browser, the list is closed when the page loads, and pressing the launcher button opens it. Typing in the search field keeps only the entries whose name contains the typed text, ignoring case and any spaces around it; clearing the field shows them all again. When the text matches no entry, the no-match line appears, reading `No service matches “<text>”.` with the typed text in quotation marks. Pressing Enter in the search field opens the first entry still shown that is a working link, and does nothing when there is none. That filtering is the whole of what `/_appkit/launcher.js` does: every entry, and the no-match line, arrived with the page.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the catalog of `A user opens the catalog`, with the same banner, and the banner also holds the launcher button labelled `Services`. The page carries the list of services labelled `Services`, holding the search field labelled `Find a service` with the placeholder `Find a service` and seven entries in the file's order, each showing its icon and then its name: `auth`, a link to `https://auth.sbx.ikigenba.dev`; `dummy`, a link to `https://dummy.sbx.ikigenba.dev`; `mcp`, a link to `https://mcp.sbx.ikigenba.dev`; `repos`, a link to `https://repos.sbx.ikigenba.dev`; `scripts`, a link to `https://scripts.sbx.ikigenba.dev`, marked as the current page; `sites`, a link to `https://sites.sbx.ikigenba.dev`; and `telemetry`, a link to `https://telemetry.sbx.ikigenba.dev`. The no-match line is in the page and hidden. The page loads the script `/_appkit/launcher.js`. The about screen, a script's page (`S12`), and a run's page (`S13`) carry the same launcher.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file with an `icon` string on each entry.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed. The services file is as it was.

## A user on a host whose services file is missing sees no launcher

`IKIGENBA_SERVICES` names a file, but there is nothing there to read. A broken services file never breaks a page, so scripts draws the page without a launcher and reports nothing about the file: this is not a fault of the request, and the answer is the same as when no file is named at all. A file that exists but cannot be read, is not a JSON object with a `services` array, or has no usable entry that carries an icon is answered the same way, as far as the launcher goes. With no file there is no `auth` entry either, so auth's links are read from the request's `Host` and `X-Forwarded-Proto`, which here name the same space and scheme, so the page reads as it does with the file.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the catalog of `A user opens the catalog`: the profile link leading to `https://auth.sbx.ikigenba.dev/`, the sign-out button POSTing to `https://auth.sbx.ikigenba.dev/logout`, the same four scripts in `table#script-list`, no launcher button, no list of services, no `Find a service` field, no no-match line, and no `/_appkit/launcher.js`.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` does not exist.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.
- scripts wrote nothing to stderr about the services file. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as a `scripts: undelivered event: <event>` line, and nothing else for this request.

## A user sees the launcher follow a change to the services file

The host rewrites the services file when a service is installed or switched on or off, and scripts reads the file afresh for every page, so the next page a user loads shows the new list without scripts being restarted. Here the host has switched `dummy` off since scripts started: `/var/lib/ikigenba/services.json` is the file of `A user on a host with services opens the launcher` with `dummy`'s `enabled` now `false`.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the catalog with the launcher, as in `A user on a host with services opens the launcher`, except that the `dummy` entry is not a working link and is titled `dummy is unavailable`; it keeps its place and still shows its icon and name.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment while `/var/lib/ikigenba/services.json` held the file of `A user on a host with services opens the launcher`, with `dummy` switched on, and it has not been restarted since.
- `/var/lib/ikigenba/services.json` now lists `dummy` with `enabled` `false`.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.

## A user on a host whose services file names no auth still gets auth's links

The banner's profile link and sign-out form address auth on the space the request names in its `Host` when the services file has no `auth` entry to take them from, so one scripts build serves whichever space it is installed on. Here `/var/lib/ikigenba/services.json` is the suite's services file without the `auth` entry.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
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

Status 200. The body is the catalog of `A user opens the catalog`, and for every form above its banner's profile link, titled `mg@example.com`, leads to `https://auth.sbx.ikigenba.dev/`, and the sign-out button reading `Sign out` is in a form whose method is `post` and whose action is `https://auth.sbx.ikigenba.dev/logout`. With `X-Forwarded-Proto: http` and `Host: scripts.sbx.ikigenba.dev`, the link leads to `http://auth.sbx.ikigenba.dev/` and the form's action is `http://auth.sbx.ikigenba.dev/logout`. Each script's link is `/<name>/` for every form, whatever the `Host`. The about screen's banner is built the same way.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file without the entry named `auth`.
- The catalog holds `S06`'s shared catalog.

Postconditions:

- Nothing has changed.
- scripts set no cookie.

## A request arrives without the identity headers

In production this cannot happen from outside: nginx sets the headers on every request it forwards, a sibling forwards the ones it received, and nothing but nginx and the suite's apps can reach scripts' socket. So a request without `X-User-Id` says nginx or a sibling is misconfigured, which is scripts' fault to report, not the caller's to fix — hence a 500 and not a 400 or a 401. A developer meets it by forgetting the headers, as here. scripts has no route that takes no identity: without a caller there is no owner to show scripts for.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`, ending in a newline. It has no banner and no footer. A `HEAD` is answered with the same status and headers and an empty body. An `X-User-Id` header whose value is empty is answered the same way as no header at all, and so is a request that carries `X-User-Email` and no `X-User-Id`. Every path answers this way — `/`, `/about`, the shared files under `/_appkit/` (`S04`), the MCP endpoint `/mcp` (`S05`), a script's page and a run's page and files (`S12`, `S13`, `S14`), and every path that names nothing; the identity check runs before scripts looks at the path or the method, so a request with no identity is never a 404 or a 405, reads no catalog, and runs no git.

Preconditions:

- scripts is serving, and telemetry takes every event.
- The request carries no `X-User-Id` header, or one whose value is empty, and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. The catalog was not read, and no git ran.
- scripts wrote nothing to stderr. The 500 is a handled failure, so it is in the trail and not the journal: telemetry has received the request's two events, with an empty user, under the id scripts gave the request, 32 lowercase hexadecimal characters:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A request from nginx arrives without the identity headers

nginx sets `X-Request-Id` on every request it forwards, so when it is misconfigured and forwards a request without `X-User-Id`, scripts records the 500 under the id nginx gave the request, and the operator who finds the 500 in the trail can find the same request in nginx's log. The developer here stands in for such an nginx by sending the id by hand.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the same one line, `identity header missing`, ending in a newline.

Preconditions:

- scripts is serving, and telemetry takes every event.
- The request carries `X-Request-Id` and no `X-User-Id` header.

Postconditions:

- Nothing has changed.
- scripts wrote nothing to stderr. telemetry has received the request's two events under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user, so a trace of that id finds them:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A user opens the catalog while scripts cannot reach its catalog

The page lists scripts from the catalog, so when scripts cannot read it there is nothing true to list. It does not draw the page as if the user had no scripts, which would tell them their scripts were gone; it says plainly that it cannot reach the catalog, quoting nothing of the database's own error, and the user may try again later. A script's page, a run's page, and a run's files answer the same way (`S12`, `S13`, `S14`), as the tools do (`S05`). The about screen reads no catalog and is still served.

Request:

```
GET / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
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

- scripts is serving, and telemetry takes every event.
- scripts' database cannot be read: `state/scripts.db` has become unreadable since scripts opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed. No git ran.
- scripts wrote nothing to stderr. telemetry has received the request's two events under user `u_7f3a9c21`, the `request.finished` with `status` 503.
- scripts is still serving.

## A caller sends a page a method it does not take

The pages only read, so `/` and `/about` take `GET` and `HEAD` and nothing else, and `Allow` names those two. No page is sent. The identity check comes first, so a request with no `X-User-Id` is answered with the 500 of `A request arrives without the identity headers`, whatever its method. The MCP endpoint refuses a method it does not take in its own way (`S05`), and a script's page, a run's page, and a run's files refuse one as these pages do (`S12`, `S13`, `S14`).

Request:

```
POST / HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /about HTTP/1.1
Host: scripts.sbx.ikigenba.dev
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

- scripts is serving.

Postconditions:

- Nothing has changed. The catalog was not read, and no git ran.
- scripts wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 405.
