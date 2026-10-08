# Stories — script page

One script's page: what a running scripts (`S02`) answers at `/<name>/`, at `/<name>`, and at every path whose first segment names none of the caller's scripts or that names nothing beneath one of them. The page shows the caller one of their own scripts: its record, the events it is subscribed to (`S26`), and its runs, newest first, each linking to its own page (`S13`). It is server-rendered HTML drawn from scripts' template `script`, titled `<name> · scripts`, with the stylesheet link, the favicon link, the button feedback script, the viewport, the banner, the launcher when the services file offers one, and the footer `scripts <display>` exactly as the catalog has them (`S03`), and no script of its own. The first segment of every path but `/`, `/about`, `/mcp` (`S05`), `/events` and `/declarations`, which only siblings on the socket reach and which no story in this group answers, and `/_appkit/` and every path beneath it, whose answers, a missing shared file's included, are `S04`'s, is a script's name (`S03`), and scripts answers it in this order: the caller's identity first, as on every route (`S03`); then a method other than `GET` or `HEAD`, refused with `405`; then the name, looked up in the catalog among the caller's own scripts only, a catalog it cannot read being answered `503`; then `/<name>` without its trailing `/` is redirected to `/<name>/`, `/<name>/` is the page, the paths of a run, `/<name>/runs/<run id>/` and what lies beneath it, are `S13`'s and `S14`'s, and every other path is answered with the not-found page. A name that is none of the caller's scripts is answered with the not-found page whatever follows it, exactly as a name no script has, so a page never tells a user that another user's script exists. scripts' not-found page is an HTML document, `Content-Type: text/html; charset=utf-8`, titled `Not found`, that links `/_appkit/theme.css` and links `/_appkit/favicon.svg` as its icon, whose heading is `Not found` and whose text (`p#notfound`) is `There is nothing at this address.`, with the footer `scripts <display>`, no banner, and no script but the button feedback script, `/_appkit/feedback.js`; it is answered with status `404`. A script's repository is shown by its name, which scripts reads when it draws the page from the bare repository's config, `ikigenba.name` in `<REPOS_DIR>/<repository id>.git`, with the host's git; it is not stored, so a repository renamed in repos shows its new name on the next page, and when the repository's directory is gone or the name cannot be read the page shows the repository's id instead, muted, titled `the repository is gone`. That read is the only git a script page runs, and it reads no run folder and runs no script. A script's subscriptions are the catalog's, each shown by its event name, sorted by event name, in `section#subscriptions`, headed `Subscriptions`: a list `ul#subscription-list` of one `li[data-event=<event>]` per subscription, whose visible text is the event name, or, for a script subscribed to nothing, no list and `div#no-subscriptions` reading `This script is subscribed to no events.`; the page shows no subscription's time. Status reads as a word with a kind (`span.status[data-kind=<kind>]`): `queued` and `running`, `info`; `exited 0`, `ok`; `exited <n>` for a non-zero `<n>`, `timed out` and `killed`, `warn`; `failed`, `err`. A run's commit is its short hash, the first seven hexadecimal digits of its sha, and is empty when its ref never resolved; its duration is empty while it is queued or running and when it failed to start; and its exit code is shown only when its process exited on its own.

Unless a story says otherwise, scripts runs with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json`, the suite's services file (`S03`), and telemetry takes every event. The catalog is `S06`'s shared catalog, and now is `2026-10-05T09:32:00Z`. The caller is `u_7f3a9c21`, `mg@example.com`, who owns `nightly-report`, `scr_6d1f4a9b2e8c7035`, created `2026-09-18T16:40:00Z`, running `main` of `rep_9c2e4b7a1d3f8e05`, whose name in repos is `nightly-report`; `sync-crm`; `rotate-keys`, `scr_5c9b1e3a7f2d4068`, created `2026-09-28T08:00:00Z`, running `release` of `rep_7b3e9a0c5d1f2846`, whose name is `ops-tools`; and `backfill`, `scr_e8f2a6c0d4b19357`, created `2026-10-02T12:00:00Z`, running `main` of `rep_0f6a2d9e8c4b7153`, whose directory is gone from `../repos/state/repos`. `digest` is `u_2b8e1d04`'s, `ann@example.com`. `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` are their defaults, 15 and 10. The requests carry `Host: scripts.sbx.ikigenba.dev`, `X-Forwarded-Proto: https`, the caller's `X-User-Id` and `X-User-Email`, and nginx's `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`. Every request adds exactly two events to scripts' trail, `request.started` and `request.finished` (`S02`), under that request id and the caller's user, and no other: a page view is no domain event. No answer in this group earns a line on stderr. Every answer in this group answers `HEAD` with the status and headers its `GET` has and an empty body. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user opens one of their scripts

A user follows a script's link on the catalog to see what it runs, what sets it off, and how its runs went. The page names the repository and the ref, gives the script's record, lists the events the script is subscribed to, and lists every run the catalog keeps for it, newest first, by when each started, so the run that is still going is on top. A run whose files are gone (`S20`) is a row like any other: its record stays, and the script page shows only records.

Request:

```
GET /nightly-report/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `nightly-report · scripts`, drawn in the catalog's banner (`S03`), whose profile link is titled `mg@example.com`. Beneath the banner is the breadcrumb, `nav.crumbs` labelled `Breadcrumb`, an ordered list of two levels: `scripts`, a link to `/`; then `nightly-report`, the current page, carrying `aria-current="page"` and no link. Then the page's top-level heading (`h1`), `nightly-report`, and `p#about-script` reading `Runs main.py from the repository nightly-report at main.`, the repository's name in a `strong` and `main.py` and the ref each in a code element.

Then the card `section#script-card`, headed `Script`, reading `Changed by an agent through the update tool.`, and holding the list `dl#script` of exactly five facts, in this order: `Id`, `scr_6d1f4a9b2e8c7035` in a code element; `Repository`, `nightly-report` over the repository's id, `rep_9c2e4b7a1d3f8e05`, in a code element; `Ref`, `main` in a code element; `Created`, `2026-09-18 16:40 UTC`, in a `time` whose `datetime` is `2026-09-18T16:40:00Z`; and `Runs kept`, `7 · the newest 10 are kept past 15 days`, the number of runs the catalog holds for the script and the retention rule of `RUN_KEEP_COUNT` and `RUN_KEEP_DAYS` (`S19`).

Then the section `section#subscriptions`, headed `Subscriptions`, holding `ul#subscription-list` with exactly two items, in this order: `li[data-event=crm.contact_updated]`, reading `crm.contact_updated`, then `li[data-event=repo.pushed]`, reading `repo.pushed`, sorted by event name and not by when each was made. The page has no `div#no-subscriptions`.

Then the section `section#runs`, headed `Runs`, reading `Newest first. A run still running shows its progress when the page is reloaded.`, and the table `table#run-list`, whose columns are `Run`, `Status`, `Commit`, `Started`, `Duration` and `Exit`, with exactly seven rows, `tr[data-run=<run id>]`, in this order. Each row's run id is a link (`a.run-link`) to `/nightly-report/runs/<run id>/`, and its start is a `time` whose `datetime` is the start in RFC 3339 UTC to the second:

| row | Status (kind) | Commit | Started (`datetime`) | Duration | Exit |
|---|---|---|---|---|---|
| `run_8a2c6e1f9b3d5074` | `running` (`info`) | `e4f1c9a` | `2026-10-05 09:31` (`2026-10-05T09:31:40Z`) | | |
| `run_3f9a1c2e8b7d4a60` | `exited 0` (`ok`) | `e4f1c9a` | `2026-10-05 09:14` (`2026-10-05T09:14:02Z`) | `12s` | `0` |
| `run_c71d0b5e4a2f9386` | `exited 1` (`warn`) | `e4f1c9a` | `2026-10-04 09:14` (`2026-10-04T09:14:02Z`) | `9s` | `1` |
| `run_5e8b3d7a1c0f6294` | `timed out` (`warn`) | `b07d2e3` | `2026-10-03 09:14` (`2026-10-03T09:14:02Z`) | `10m 0s` | |
| `run_19f6a4d2c8e3b705` | `killed` (`warn`) | `b07d2e3` | `2026-10-02 09:14` (`2026-10-02T09:14:02Z`) | `3m 41s` | |
| `run_d4a7e2c9f1b8630a` | `failed` (`err`) | | `2026-10-01 09:14` (`2026-10-01T09:14:02Z`) | | |
| `run_72b0c8f5e3d1a946` | `exited 0` (`ok`) | `9a3c5f0` | `2026-09-30 09:14` (`2026-09-30T09:14:02Z`) | `11s` | `0` |

An empty cell is empty: the running run has no duration yet, the failed run resolved no commit and never started, and only a process that exited on its own has an exit code. `run_72b0c8f5e3d1a946`'s row, whose folder is gone, reads as any finished run's. The page has no `div#no-runs`. Last on the page is the footer reading `scripts <display>`. The page names neither the runs' refs nor their users.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file holds the suite's services file; telemetry takes every event.
- The catalog holds `S06`'s shared catalog: `nightly-report`'s seven runs, `run_8a2c6e1f9b3d5074` still running, and `run_72b0c8f5e3d1a946`'s folder gone from `state/runs/scr_6d1f4a9b2e8c7035/`.
- `nightly-report` is subscribed to two events: `repo.pushed`, made `2026-09-20T10:00:00Z`, and `crm.contact_updated`, made `2026-10-01T08:30:00Z`.
- `../repos/state/repos/rep_9c2e4b7a1d3f8e05.git` exists, and its config names it `nightly-report`.

Postconditions:

- Nothing has changed. The catalog was read and not written; git read `rep_9c2e4b7a1d3f8e05`'s name and nothing else; no run folder was read; no script ran, and `run_8a2c6e1f9b3d5074` runs on.
- scripts wrote nothing to stderr and set no cookie. telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/nightly-report/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `response_bytes` is the length of the page's body.

## A user opens a script that has never run, whose repository is gone

`backfill` was created from a repository that has since been deleted in repos, nobody has run it, and nothing subscribes it to an event. The page still shows the script: its record is the catalog's and stays whatever became of the repository. With no name to read, the repository is shown by the id the catalog keeps, muted, titled `the repository is gone`; where the subscriptions would be, the page says there are none; and where the runs would be, the page says there are none.

Request:

```
GET /backfill/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the script page of `A user opens one of their scripts`, for `backfill`: titled `backfill · scripts`; the breadcrumb `scripts`, a link to `/`, then `backfill`, the current page; the heading `backfill`; and `p#about-script` reading `Runs main.py from the repository rep_0f6a2d9e8c4b7153 at main.`, the id in a `span.muted` titled `the repository is gone`, with no `strong`. `dl#script` holds `Id`, `scr_e8f2a6c0d4b19357`; `Repository`, `rep_0f6a2d9e8c4b7153` in a code element within a `span.muted` titled `the repository is gone`, and no name; `Ref`, `main`; `Created`, `2026-10-02 12:00 UTC`, in a `time` whose `datetime` is `2026-10-02T12:00:00Z`; and `Runs kept`, `0 · the newest 10 are kept past 15 days`. The section `section#subscriptions`, headed `Subscriptions`, holds no `ul#subscription-list` and holds `div#no-subscriptions` reading `This script is subscribed to no events.` The section `section#runs`, headed `Runs`, holds no `table#run-list` and holds `div#no-runs`, whose heading is `No runs yet` and whose text reads `Runs started with the run tool show up here, newest first.` A repository whose directory is there but whose config has no `ikigenba.name`, or one git cannot read, is shown the same way.

Preconditions:

- scripts is serving.
- The catalog holds `backfill`, owned by `u_7f3a9c21`, with no runs and no subscriptions.
- `../repos/state/repos/rep_0f6a2d9e8c4b7153.git` does not exist.

Postconditions:

- Nothing has changed. The catalog was read and not written, and nothing was created under `state/runs/` or `../repos/state/repos/`.
- scripts wrote nothing to stderr: a repository that is gone is shown, not reported. telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user opens a script whose only run failed to start

`rotate-keys` runs `release`, a branch `ops-tools` does not have, so its one run could not start (`S08`). The run is still a run, and the script page lists it like any other: `failed`, with no commit, no duration and no exit code. Why it failed is on the run's own page (`S13`).

Request:

```
GET /rotate-keys/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the script page of `A user opens one of their scripts`, for `rotate-keys`: titled `rotate-keys · scripts`; the heading `rotate-keys`; `p#about-script` reading `Runs main.py from the repository ops-tools at release.`; and `dl#script` holding `Id`, `scr_5c9b1e3a7f2d4068`; `Repository`, `ops-tools` over `rep_7b3e9a0c5d1f2846`; `Ref`, `release`; `Created`, `2026-09-28 08:00 UTC`, `datetime` `2026-09-28T08:00:00Z`; and `Runs kept`, `1 · the newest 10 are kept past 15 days`. `section#subscriptions` holds `div#no-subscriptions` and no list. `table#run-list` has exactly one row, `tr[data-run=run_1e9c3a7f5b0d2864]`: the link `run_1e9c3a7f5b0d2864` to `/rotate-keys/runs/run_1e9c3a7f5b0d2864/`; the status `failed`, kind `err`; an empty commit; the start `2026-10-04 22:00`, `datetime` `2026-10-04T22:00:00Z`; an empty duration; and an empty exit. The page carries no reason for the failure.

Preconditions:

- scripts is serving.
- The catalog holds `rotate-keys` with one run, `run_1e9c3a7f5b0d2864`, `failed` with reason `commit_missing`, and no subscriptions.
- `../repos/state/repos/rep_7b3e9a0c5d1f2846.git` exists, its config names it `ops-tools`, and it has no branch `release`.

Postconditions:

- Nothing has changed. git read the repository's name and resolved no ref; no run was started.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user's client asks for a script page's headers

A `HEAD` is answered as the `GET` would be, status and headers alike, with no body, and reads what the `GET` reads.

Request:

```
HEAD /nightly-report/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is empty. A `HEAD` of a path that names nothing is answered `404` with the not-found page's headers and no body, and a `HEAD` of `/nightly-report` is redirected as its `GET` is.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"HEAD","path":"/nightly-report/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":0,"status":200}}
  ```

## A user types a script's address without the trailing slash

`/nightly-report` is the script, but its page is `/nightly-report/`, the address its runs' links and the catalog's link use, so scripts sends the browser there before drawing anything. The query goes with it. Only a name that is one of the caller's scripts is redirected: there is nothing to add a slash for otherwise.

Request:

```
GET /nightly-report?from=landing HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 301 Moved Permanently
Location: /nightly-report/?from=landing
```

Status 301. No story fixes the body. Without a query, `GET /nightly-report` is redirected to `Location: /nightly-report/`, and `GET /backfill`, a script that has never run, to `Location: /backfill/`. A `HEAD` is redirected the same way. `/nope` and `/digest`, which name none of the caller's scripts, are not redirected but answered with the not-found page (`A user asks for a script that does not exist`, `A user asks for another user's script`).

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's two events, the `request.started` with `path` `/nightly-report`, without its query, and the `request.finished` with `status` 301.

## A user asks for a script that does not exist

A name that no script has, a mistyped one or a script deleted since (`S10`), is answered with the not-found page, with or without the trailing slash, and whatever follows the name. Names are matched exactly, so `/Nightly-Report/` names no script.

Request:

```
GET /nope/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nope HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /Nightly-Report/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nope/runs/run_3f9a1c2e8b7d4a60/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is scripts' not-found page: titled `Not found`, its heading `Not found` and its text `There is nothing at this address.`, with the footer `scripts <display>`, no banner, and no script but the button feedback script. `/nope` is not redirected. The run named under `/nope/` is `nightly-report`'s, but it is not looked for under a name that is no script.

Preconditions:

- The preamble's: no script is named `nope` or `Nightly-Report`.

Postconditions:

- Nothing has changed. No git ran, and no run folder was read.
- scripts wrote nothing to stderr. telemetry has received the request's two events:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/nope/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":404}}
  ```

## A user asks for another user's script

A user sees only their own scripts. `digest` exists, and its name is taken for the whole space (`S06`), but it is `ann@example.com`'s, so to `mg@example.com` its address is answered exactly as one no script has: the same not-found page, not redirected, with nothing in the answer that tells it apart. The same holds the other way round.

Request:

```
GET /digest/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /digest HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is scripts' not-found page, as in `A user asks for a script that does not exist`, byte for byte the page `/nope/` gets. `/digest` is not redirected.

Preconditions:

- The preamble's: `digest` is owned by `u_2b8e1d04`, and `nightly-report` by `u_7f3a9c21`.

Postconditions:

- Nothing has changed. No git ran: neither script's repository was read.
- telemetry has received each request's two events under its caller's user, the `request.finished` with `status` 404.

## A user asks for a path under a script that names nothing

Beneath a script, scripts answers only its page and its runs' paths (`S13`, `S14`). `/nightly-report/runs` and `/nightly-report/runs/` are not a list of runs, which is the script page itself, and any other path under the script names nothing. None is redirected, and none is tidied into another: a `.` or `..` segment, literal or percent-encoded, makes the path name nothing even where tidying would land on the page.

Request:

```
GET /nightly-report/runs HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/other HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/./ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is scripts' not-found page, as in `A user asks for a script that does not exist`. `/nightly-report/other/`, `/nightly-report/index.html`, and `/nightly-report/%2e/` are answered the same way.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No git ran, and no run folder was read.
- telemetry has received each request's two events, the `request.started` with its `path` as it arrived, `/nightly-report/runs` say, and the `request.finished` with `status` 404.

## A user asks for a path beneath scripts' own pages

`/about` and `/mcp` are scripts' own exact paths (`S03`, `S05`), as `/events` and `/declarations` are for its siblings on the socket, and `about`, `mcp`, `events`, and `declarations` can never be a script's name (`S06`). So a path beneath any of the four is a script path whose name no script has, answered with the not-found page; this group does not say what scripts answers on the exact paths `/events` and `/declarations`. scripts has no plain-text `not found`.

Request:

```
GET /about/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /mcp/tools HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /events/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /declarations/repo.pushed HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is scripts' not-found page, as in `A user asks for a script that does not exist`. `/about/` is not redirected to `/about`, and `/mcp/` and `/mcp/tools` are not MCP. Neither `/events/` nor `/declarations/repo.pushed` is redirected or reaches what scripts answers on `/events` or `/declarations`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received each request's two events, the `request.finished` with `status` 404, and no `tool.called`.

## A user opens a script page while scripts cannot reach its catalog

scripts finds a script by its name among the caller's scripts in the catalog, so when it cannot read the catalog it cannot tell a script from a name that is none. It does not answer as if the script were gone; it says plainly that it cannot reach the catalog, as the catalog page does (`S03`), quoting nothing of the database's own error, and the user may try again later.

Request:

```
GET /nightly-report/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
```

Status 503. The body is exactly the one line `cannot reach the catalog; try again later`, ending in a newline. It has no banner and no footer. `/nightly-report`, `/nope/`, and every other path this group answers, but for a method it does not take, are answered the same way.

Preconditions:

- scripts is serving, and telemetry takes every event.
- scripts' database cannot be read: `state/scripts.db` has become unreadable since scripts opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed. No git ran.
- scripts wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 503.
- scripts is still serving.

## A caller sends a script page a method it does not take

The pages only read, so a script path takes `GET` and `HEAD` and nothing else, and `Allow` names those two. The method is looked at after the caller's identity (`S03`) and before anything else, so the answer is the same for one of the caller's scripts, a path beneath it that names nothing, and a name that is no script, and the catalog is not read.

Request:

```
POST /nightly-report/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
PUT /nightly-report HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
DELETE /nope/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. `PATCH`, `OPTIONS`, and every other method but `GET` and `HEAD` are refused the same way, on every path this group answers, another user's script's included. A script is deleted with the `delete` tool (`S10`), never through its page.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `nightly-report` and its runs are as they were. The catalog was not read, and no git ran.
- telemetry has received the request's two events, the `request.finished` with `status` 405 and `response_bytes` 0.
