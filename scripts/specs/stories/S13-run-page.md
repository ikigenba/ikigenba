# Stories — run page

One run, seen in a browser: what a running scripts (`S02`) answers at `/<name>/runs/<run id>/`, the page of one run of one of the caller's scripts, and at `/<name>/runs/<run id>` without its trailing `/`. The page is server-rendered HTML drawn from scripts' template `run`, in the frame every page of scripts is drawn in (`S03`): the banner, the stylesheet `/_appkit/theme.css`, the favicon `/_appkit/favicon.svg`, the button feedback script `/_appkit/feedback.js`, the phone-width viewport, and the footer `scripts <display>`; the whole of its content arrives in the response body, and it carries no script of its own. Its title is `<run id> · <name> · scripts`. Every route of scripts is behind the identity rule `S03` tells, so a request with no `X-User-Id` never reaches anything told here, and on a host a visitor with no credential is sent to sign in before any request reaches scripts (`S03`). A run's page is its script owner's alone: scripts looks `<name>` up among the caller's own scripts and `<run id>` among that script's runs, and anything else at a run's address, a run that does not exist, a run of another of the caller's scripts, a run of another user's script, is answered with scripts' not-found page, status 404, exactly as a script that does not exist is (`S12`), so the page never tells a caller that someone else's run exists. The script's own page, which lists its runs and links each to its page, is `S12`'s; the files the run page links to download, `input.json`, `stdout`, `stderr` and each file under `out/`, are served as `S14` tells.

The page has a breadcrumb, `nav.crumbs` labelled `Breadcrumb`, an ordered list of three levels: `scripts`, a link to `/`; the script's name, a link to `/<name>/`; and the run's id, the current page, with `aria-current="page"` and no link. Beneath it is the top-level heading (`h1`), the run's id, and then the headline, `p#headline`, holding the run's status and, muted, `started <started>` followed by ` · <duration>` when the run has ended, or, for a run that failed to start, the start time alone. A status reads as a `span.status` whose `data-kind` is its kind: `queued` and `running`, kind `info`; `exited 0`, kind `ok`; `exited <n>` for any other exit code, `timed out` and `killed`, kind `warn`; and `failed`, kind `err`. The run card, `section#run-card`, is headed `Run` and reads `Started by the run tool.` for a run the `run` tool started (`S08`), and `Started by the event <event id>.`, where `<event id>` is the id of the event that started it, for a run an event started (`S27`); its list `dl#run` holds, in this order, `Status`, the status and, only when the run's output was truncated (`S17`), the badge `span.badge[data-kind=warn]` reading `output truncated`, once for the run whichever stream was cut; `Script`, the script's name as a link to `/<name>/`; `Commit`, the full 40-character commit the run resolved, in a code element, and nothing when its ref never resolved; `Ref`, the ref the run was resolved from, in a code element; `Started`; `Finished` and `Duration`, only once the run has ended, a run that failed to start showing `Finished` and no `Duration`; `Trigger`, the run's trigger, `manual` or `event`; `User`, the id of the user the run acts as; `Request`, the 32-character request id the run was caused by, in a code element; and `Output`, `stdout <size> · stderr <size>`, the bytes kept of each stream. A time reads `2026-10-05 09:14:02 UTC`, in UTC to the second, in a `time` element whose `datetime` is the same moment as RFC 3339, `2026-10-05T09:14:02Z`. A duration reads `12s` under a minute and `3m 41s` from a minute on. A size reads `<n> B` under 1000 bytes and otherwise in thousands with one decimal, `kB` and then `MB`: 0 bytes read `0 B`, 1229 read `1.2 kB`, 12034 read `12.0 kB`, and 1048576 read `1.0 MB`.

After the card come the run folder's contents, each only when its file exists: `section#input`, headed `Input`; `section#stdout`, headed `Standard output`; and `section#stderr`, headed `Standard error`. Each names its file and size, `input.json, 69 B` say; holds a `Download` link (`a.button[download]`) to the file's address, `/<name>/runs/<run id>/input.json`, `/<name>/runs/<run id>/stdout` or `/<name>/runs/<run id>/stderr`; and shows the file's whole content as text in a `pre`, or, when the file is empty, the muted line `No input.` or `No output.` and no `pre`. Last is `section#files`, headed `Files` and reading `What the script wrote under out/. Each downloads as a file.`, which holds either the line `p#no-files` reading `No files.`, when the run's `out/` holds no file, or the table `table#file-list` with one row, `tr[data-file=<path>]`, per regular file under `out/`, at any depth, sorted by path: the path relative to `out/`, `/`-separated, in a code element; its size; and a `Download` link (`a.button[download]`) to `/<name>/runs/<run id>/out/<path>`. A run whose folder is gone shows none of these four sections (`A user opens a run whose files are gone`). The page shows what is on disk when it is drawn and does not stream: a run still running shows what exists so far, and a reload shows more (`A user opens a run still running and reloads it`).

The stories share `S06`'s shared catalog. The caller is `u_7f3a9c21` (`mg@example.com`), who owns `nightly-report`, id `scr_6d1f4a9b2e8c7035`, of repository `rep_9c2e4b7a1d3f8e05`, repos' `nightly-report`, whose bare directory holds `ikigenba.name` `nightly-report`; `u_2b8e1d04` (`ann@example.com`) owns `digest`. The runs of `nightly-report`, all with trigger `manual` and user `u_7f3a9c21` unless a story adds one, are these:

| run | status | exit | commit | ref | started | finished | request id |
|---|---|---|---|---|---|---|---|
| `run_8a2c6e1f9b3d5074` | running | | `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` | `main` | 2026-10-05T09:31:40Z | | `9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2` |
| `run_3f9a1c2e8b7d4a60` | exited | 0 | `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` | `main` | 2026-10-05T09:14:02Z | 2026-10-05T09:14:14Z | `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` |
| `run_c71d0b5e4a2f9386` | exited | 1 | `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` | `main` | 2026-10-04T09:14:02Z | 2026-10-04T09:14:11Z | `5b1f9d3e7a2c4806e1b5d9f3a7c2e468` |
| `run_5e8b3d7a1c0f6294` | timed_out | | `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37` | `main` | 2026-10-03T09:14:02Z | 2026-10-03T09:24:02Z | `c9e3a7f1d5b20846a3e7c1f9d5b2a084` |
| `run_19f6a4d2c8e3b705` | killed | | `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37` | `v1` | 2026-10-02T09:14:02Z | 2026-10-02T09:17:43Z | `81d5b9f3e7a20c46d8b2f6a0e4c9d137` |
| `run_d4a7e2c9f1b8630a` | failed, `commit_missing` | | | `release` | 2026-10-01T09:14:02Z | 2026-10-01T09:14:02Z | `c2f5a8d1e4b7093a6d9c2f5e8b1a4d70` |
| `run_72b0c8f5e3d1a946` | exited | 0 | `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60` | `main` | 2026-09-30T09:14:02Z | 2026-09-30T09:14:13Z | `5d8a1e4b7c0f3926a5d8e1b4c7f0a396` |

Each run's folder is `state/runs/scr_6d1f4a9b2e8c7035/<run id>/`, and every run above has one but `run_72b0c8f5e3d1a946`; what each holds is told in the story that opens it. `sync-crm`, `scr_a2e7c4f9b1d03856`, also the caller's, has the run `run_6b2d8f4a0c9e1735`, and `digest` has `run_0c4e8a2f6b1d9375`. Unless a story says otherwise, scripts runs with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json`, the suite's services file (`S03`), telemetry takes every event, and the requests carry `Host: scripts.sbx.ikigenba.dev`, `X-Forwarded-Proto: https`, the caller's `X-User-Id` and `X-User-Email`, and nginx's `X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284`, the page request's own id, which is not any run's. Serving a run page reads the catalog and the run's folder, and runs git only to read the repository's name for a failure reason that names it; it writes nothing, starts and stops no script, and the run is exactly as it was. Every request here adds its `request.started` and `request.finished` to the trail and nothing else (`S02`); no answer in this group earns a line on stderr. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed.

## A user opens a run that exited 0

A user who wants to know what a run did, from the script's page or a link an agent gave them, opens the run's page and sees its outcome, what it was given, what it printed, and what it wrote. Everything is in the document that arrives.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML document titled `run_3f9a1c2e8b7d4a60 · nightly-report · scripts`, in scripts' frame (`S03`). Its breadcrumb reads `scripts`, linking to `/`; `nightly-report`, linking to `/nightly-report/`; and `run_3f9a1c2e8b7d4a60`, the current page, with no link. Its heading is `run_3f9a1c2e8b7d4a60`, and its headline reads `exited 0`, of kind `ok`, then `started 2026-10-05 09:14:02 UTC · 12s`, the time in a `time` element whose `datetime` is `2026-10-05T09:14:02Z`. The page has no `div#running`, no `a#reload`, no `div#failure`, and no `div#files-gone`.

The run card is headed `Run` and reads `Started by the run tool.`; its list holds, in this order: `Status`, `exited 0` of kind `ok` and no `output truncated` badge; `Script`, `nightly-report`, linking to `/nightly-report/`; `Commit`, `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`; `Ref`, `main`; `Started`, `2026-10-05 09:14:02 UTC`; `Finished`, `2026-10-05 09:14:14 UTC`, its `datetime` `2026-10-05T09:14:14Z`; `Duration`, `12s`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`; and `Output`, `stdout 1.2 kB · stderr 0 B`. The address `mg@example.com` is not in the run card.

`section#input` is headed `Input`, reads `input.json, 69 B`, has a `Download` link to `/nightly-report/runs/run_3f9a1c2e8b7d4a60/input.json`, and shows in its `pre` the file's text exactly as the file holds it, on one line, not reformatted: `{"since":"2026-10-04","channels":["sales","support"],"dry_run":false}`. `section#stdout` is headed `Standard output`, reads `stdout, 1.2 kB`, has a `Download` link to `/nightly-report/runs/run_3f9a1c2e8b7d4a60/stdout`, and shows in its `pre` all 1229 bytes of the file, beginning with the line `reading events since 2026-10-04 from telemetry`. `section#stderr` is headed `Standard error`, reads `stderr, 0 B`, has a `Download` link to `/nightly-report/runs/run_3f9a1c2e8b7d4a60/stderr`, and in place of a `pre` shows the muted line `No output.`. `section#files` is headed `Files` and holds `table#file-list` with exactly three rows, in this order:

- `tr[data-file="charts/sales.svg"]`: `charts/sales.svg`, `12.0 kB`, and a `Download` link to `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts/sales.svg`.
- `tr[data-file="report.csv"]`: `report.csv`, `7.9 kB`, and a `Download` link to `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out/report.csv`.
- `tr[data-file="report.html"]`: `report.html`, `48.2 kB`, and a `Download` link to `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out/report.html`.

The page has no `p#no-files`. Last on the page is the footer `scripts <display>`.

Preconditions:

- The preamble's.
- `state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60/` holds `input.json`, the run's input as `S06` gives it, these 69 bytes with no LF after them, `{"since":"2026-10-04","channels":["sales","support"],"dry_run":false}`; `stdout`, 1229 bytes; `stderr`, empty; and under `out/`, `charts/sales.svg`, 12034 bytes, `report.csv`, 7904 bytes, and `report.html`, 48211 bytes. The catalog records the run's `stdout` as 1229 bytes and its `stderr` as 0, not truncated.

Postconditions:

- Nothing has changed. The catalog and the run's folder were read and not written, and no git ran, since nothing on the page names the repository.
- scripts wrote nothing to stderr. telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"9b3f7d1a5e0c2846b9d3f7a1e5c0d284","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/nightly-report/runs/run_3f9a1c2e8b7d4a60/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"9b3f7d1a5e0c2846b9d3f7a1e5c0d284","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  No `run.*` event was recorded, and neither event carries the run's own request id.

## A user opens a run an event started

A script subscribed to an event runs when the events app delivers it (`S27`), with nobody having asked for that run by hand, so its page says what started it: the event, by its id, which the user can look up in the events app. Its input is the event record, which the page shows like any input. The rest of the page is that of any run.

Request:

```
GET /nightly-report/runs/run_e2b6d0a4c8f17359/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_e2b6d0a4c8f17359`, titled `run_e2b6d0a4c8f17359 · nightly-report · scripts`. Its headline reads `exited 0`, of kind `ok`, then `started 2026-10-05 09:31:52 UTC · 5s`. The run card is headed `Run` and reads `Started by the event evt_3a7d9c1e5b2f8064.`, and not `Started by the run tool.`; its list holds `Status`, `exited 0` of kind `ok`; `Script`, `nightly-report`, linking to `/nightly-report/`; `Commit`, `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`; `Ref`, `main`; `Started`, `2026-10-05 09:31:52 UTC`; `Finished`, `2026-10-05 09:31:57 UTC`; `Duration`, `5s`; `Trigger`, `event`; `User`, `u_7f3a9c21`; `Request`, `<delivery request id>`, the id of the request by which the events app delivered the event to scripts (`S27`); and `Output`, `stdout 5 B · stderr 0 B`. `section#input` reads `input.json, <size>`, where `<size>` is the file's size, has a `Download` link to `/nightly-report/runs/run_e2b6d0a4c8f17359/input.json`, and shows in its `pre` the file's text exactly as the file holds it: the event record, as the events app sent it. `section#stdout` shows `done`; `section#stderr` reads `stderr, 0 B` and shows `No output.`; `section#files` shows `No files.`.

Preconditions:

- The preamble's, and `nightly-report` is subscribed to `repo.pushed` (`S26`).
- The events app delivered the event `evt_3a7d9c1e5b2f8064`, a `repo.pushed`, at `2026-10-05T09:31:52Z`, which started the run `run_e2b6d0a4c8f17359` of `nightly-report` as `u_7f3a9c21`, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, with request id `<delivery request id>`, the delivery's; it printed `done` and exited 0 at `2026-10-05T09:31:57Z`. Its folder holds `input.json`, the event record as the events app sent it; `stdout`, `done` and a LF; `stderr`, empty; and an empty `out/`.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's two events, the `request.finished` with `status` 200, and no `run.*` event.

## A user opens a run still running and reloads it

A user who has just started a run watches it from its page. The page is drawn from what exists when it is asked for and does not stream, so it says so and offers a link back to itself; each reload shows the output so far, and once the run has ended a reload shows how it ended.

Request:

```
GET /nightly-report/runs/run_8a2c6e1f9b3d5074/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_8a2c6e1f9b3d5074`, titled `run_8a2c6e1f9b3d5074 · nightly-report · scripts`, its breadcrumb's last level and its heading `run_8a2c6e1f9b3d5074`. Its headline reads `running`, of kind `info`, then `started 2026-10-05 09:31:40 UTC` and no duration. Beneath it is `div#running`, of kind `info`, reading `Running` and `The page does not stream. Reload it to see more output.`, and the link `a#reload`, `Reload`, leading to `/nightly-report/runs/run_8a2c6e1f9b3d5074/`, the page itself. The run card's list holds `Status`, `running` of kind `info`; `Script`, `nightly-report`; `Commit`, `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`; `Ref`, `main`; `Started`, `2026-10-05 09:31:40 UTC`; no `Finished` and no `Duration`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2`; and `Output`, `stdout 214 B · stderr 0 B`, what the streams hold so far. `section#input` reads `input.json, 2 B` and shows `{}`. `section#stdout` reads `stdout, 214 B` and shows in its `pre` the 214 bytes written so far, the eight lines `S11` shows `result` answering for this run, the first of them `reading events since 2026-10-04 from telemetry` and the last `drawing sales chart`. `section#stderr` reads `stderr, 0 B` and shows `No output.`. `section#files` shows `No files.`.

The user follows `Reload` once the script has written more: the same request, made again, is answered with the same page but for what has grown. Its `Output` reads `stdout 1.2 kB · stderr 0 B`, `section#stdout` reads `stdout, 1.2 kB`, and its `pre` holds the 1229 bytes the file holds by then, the first 214 of them the bytes the first answer showed. The run is still running, so the headline, `div#running` and `a#reload` are as before. Once the run has ended, the same request is answered with the page of an ended run, as the stories below show one: its status and duration in the headline, `Finished` and `Duration` in the card, and no `div#running` and no `a#reload`.

Preconditions:

- The preamble's.
- `run_8a2c6e1f9b3d5074` is running: its process lives, started at 2026-10-05T09:31:40Z without an `input` argument. Its folder holds `input.json`, `{}`; `stdout`, the 214 bytes the script has written so far, as `S11` shows them; `stderr`, empty; and an empty `out/`. Before the reload, the script writes more to its standard output, so that `stdout` holds 1229 bytes, and nothing to its standard error or `out/`.

Postconditions:

- Nothing has changed by either request: the run is still running, its process untouched, and the catalog and the folder were read and not written. No git ran.
- scripts wrote nothing to stderr. telemetry has received each request's two events, each `request.finished` with `status` 200, and no `run.*` event.

## A user opens a queued run

A run asked for while `RUN_MAX_ACTIVE` runs are already running waits in the queue for a slot (`S08`). Its ref has resolved and its commit is unpacked, so its page shows the commit and the input it was given, but its script has not started: it has no output yet and has written no files. Like a running run it has not ended, so the page says it does not stream, offers a link back to itself, and shows no duration and no `Finished`; its `Started` is when the run was asked for, and the duration it shows once it has ended counts from then, the time it waited included.

Request:

```
GET /nightly-report/runs/run_c4a8e2f6b0d93157/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_c4a8e2f6b0d93157`, titled `run_c4a8e2f6b0d93157 · nightly-report · scripts`, its breadcrumb's last level and its heading `run_c4a8e2f6b0d93157`. Its headline reads `queued`, of kind `info`, then `started 2026-10-05 09:31:50 UTC` and no duration. Beneath it is `div#running`, of kind `info`, reading `Queued` and `The page does not stream. Reload it to see more output.`, and the link `a#reload`, `Reload`, leading to `/nightly-report/runs/run_c4a8e2f6b0d93157/`, the page itself. The run card is headed `Run` and reads `Started by the run tool.`; its list holds `Status`, `queued` of kind `info` and no `output truncated` badge; `Script`, `nightly-report`, linking to `/nightly-report/`; `Commit`, `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`; `Ref`, `main`; `Started`, `2026-10-05 09:31:50 UTC`; no `Finished` and no `Duration`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5`; and `Output`, `stdout 0 B · stderr 0 B`. `section#input` reads `input.json, 2 B` and shows `{}`. There is no `section#stdout` and no `section#stderr`, since the run's script has not started and its folder has neither file. `section#files` shows `No files.`. The page has no `div#failure` and no `div#files-gone`.

The user follows `Reload`: once a slot frees and the run starts, the same request is answered with the page of a running run, as `A user opens a run still running and reloads it` shows one, its `div#running` reading `Running`, and once it has ended, with the page of an ended run, with no `div#running` and no `a#reload`.

Preconditions:

- The preamble's, with `RUN_MAX_ACTIVE` 2: `run_8a2c6e1f9b3d5074` and `sync-crm`'s `run_6b2d8f4a0c9e1735` are running.
- `run_c4a8e2f6b0d93157` of `nightly-report` is recorded `queued`, with trigger `manual` and user `u_7f3a9c21`, asked for at 2026-10-05T09:31:50Z by a `run` call without an `input` argument, carrying `X-Request-Id: 5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5`, which resolved `main` to `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` and was answered `queued` (`S08`). Its folder, `state/runs/scr_6d1f4a9b2e8c7035/run_c4a8e2f6b0d93157/`, holds `input.json`, `{}`; `tree/`; and an empty `out/`; and no `stdout` and no `stderr`.

Postconditions:

- Nothing has changed: the run is still `queued`, still waiting for a slot, and was not started; the catalog and the folder were read and not written. No git ran.
- scripts wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 200, and no `run.*` event.

## A user opens a run that exited non-zero

A script that ends with a non-zero exit code ran and ended on its own; the page shows the code in the status, with kind `warn`, and the user reads why in its standard error. The code is shown as the script ended with it, 128 plus the signal number for a script that died of a signal scripts did not send (`S15`), `exited 137` say.

Request:

```
GET /nightly-report/runs/run_c71d0b5e4a2f9386/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_c71d0b5e4a2f9386`. Its headline reads `exited 1`, of kind `warn`, then `started 2026-10-04 09:14:02 UTC · 9s`. The run card's list holds `Status`, `exited 1` of kind `warn` and no `output truncated` badge; `Script`, `nightly-report`, linking to `/nightly-report/`; `Commit`, `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`; `Ref`, `main`; `Started`, `2026-10-04 09:14:02 UTC`; `Finished`, `2026-10-04 09:14:11 UTC`; `Duration`, `9s`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `5b1f9d3e7a2c4806e1b5d9f3a7c2e468`; and `Output`, `stdout 47 B · stderr 34 B`. `section#input` reads `input.json, 2 B` and shows `{}`. `section#stdout` reads `stdout, 47 B` and its `pre` holds the line `reading events since 2026-10-03 from telemetry`. `section#stderr` reads `stderr, 34 B` and its `pre` holds the line `telemetry answered 503; giving up`. `section#files` shows `No files.`. The page has no `div#running`, `div#failure` or `div#files-gone`.

Preconditions:

- The preamble's.
- `run_c71d0b5e4a2f9386`'s folder holds `input.json`, `{}`; `stdout`, the 47 bytes `reading events since 2026-10-03 from telemetry` and a newline; `stderr`, the 34 bytes `telemetry answered 503; giving up` and a newline; and an empty `out/`.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user opens a run that timed out with its output truncated

A script that ran past `SCRIPT_SECONDS` was killed by scripts and recorded `timed out` (`S17`); this one also printed more than `OUTPUT_MAX_BYTES` to its standard output, of which scripts kept the head (`S17`). The page says each once: the status `timed out`, and beside it a single `output truncated` badge for the run, whichever stream was cut. The sizes are the bytes kept, so a truncated stream reads as the bound.

Request:

```
GET /nightly-report/runs/run_5e8b3d7a1c0f6294/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_5e8b3d7a1c0f6294`. Its headline reads `timed out`, of kind `warn`, then `started 2026-10-03 09:14:02 UTC · 10m 0s`. The run card's list holds `Status`, `timed out` of kind `warn` followed by the badge `output truncated` of kind `warn`, the one such badge on the page; `Script`, `nightly-report`, linking to `/nightly-report/`; `Commit`, `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`; `Ref`, `main`; `Started`, `2026-10-03 09:14:02 UTC`; `Finished`, `2026-10-03 09:24:02 UTC`; `Duration`, `10m 0s`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `c9e3a7f1d5b20846a3e7c1f9d5b2a084`; and `Output`, exactly `stdout 1.0 MB · stderr 212 B`, with no `(truncated)` after either size. `section#input` reads `input.json, 2 B` and shows `{}`. `section#stdout` reads `stdout, 1.0 MB` and its `pre` holds the 1048576 bytes kept; `section#stderr` reads `stderr, 212 B` and its `pre` holds the file's 212 bytes. `section#files` shows `No files.`.

Preconditions:

- The preamble's, with `OUTPUT_MAX_BYTES` and `SCRIPT_SECONDS` their defaults, 1048576 and 600.
- `run_5e8b3d7a1c0f6294` is recorded `timed_out`, truncated, with `stdout` 1048576 bytes and `stderr` 212. Its folder holds `input.json`, `{}`; `stdout`, the 1048576 bytes kept; `stderr`, 212 bytes; and an empty `out/`.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user opens a run that was killed

A run scripts killed reads `killed`, of kind `warn`: here the user ended it with `cancel` (`S11`), and a run killed at the drain deadline (`S18`), or found still `running` when scripts next started, reads the same. This run was started with the `ref` argument `v1`, which named the commit for that run only (`S08`); the card shows the ref the run was resolved from, not the script's.

Request:

```
GET /nightly-report/runs/run_19f6a4d2c8e3b705/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_19f6a4d2c8e3b705`. Its headline reads `killed`, of kind `warn`, then `started 2026-10-02 09:14:02 UTC · 3m 41s`. The run card's list holds `Status`, `killed` of kind `warn` and no `output truncated` badge; `Script`, `nightly-report`, linking to `/nightly-report/`; `Commit`, `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`; `Ref`, `v1`; `Started`, `2026-10-02 09:14:02 UTC`; `Finished`, `2026-10-02 09:17:43 UTC`; `Duration`, `3m 41s`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `81d5b9f3e7a20c46d8b2f6a0e4c9d137`; and `Output`, `stdout 0 B · stderr 0 B`. `section#input` reads `input.json, 2 B` and shows `{}`. `section#stdout` reads `stdout, 0 B` and `section#stderr` reads `stderr, 0 B`, each with the muted line `No output.` and no `pre`. `section#files` shows `No files.`.

Preconditions:

- The preamble's.
- `run_19f6a4d2c8e3b705` was cancelled at 2026-10-02T09:17:43Z and is recorded `killed`. Its folder holds `input.json`, `{}`; `stdout` and `stderr`, both empty; and an empty `out/`.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user opens a run that failed to start

A run scripts could not start is still a run (`S08`), and its page says why in plain words: a title and a sentence, both plain text, in place of anything the script would have said. The headline holds the status and the time alone, since there is no duration to show; the card shows when the failure was recorded as `Finished`, and no `Duration`. The folder holds what was produced before the failure, and the page shows what is there: here, for a ref that did not resolve, `input.json` alone, so the page shows it, no output sections, since this run never opened a stream, and `No files.` under Files, since nothing was written under `out/`. A run that failed another way shows whatever its folder holds, the `stderr` git wrote for `git_failed` (`S08`) among them, and the `input.json` and empty `out/` of a run abandoned in the queue (`A user opens a run abandoned in the queue`). Each reason has its own title and sentence, where `<rep id>` is the repository's id, `<repo>` the repository's name read from its bare directory as the page is drawn, or its id when that cannot be read (`A user opens a failed run whose repository's name cannot be read`), and the numbers are scripts' settings:

| reason | title | sentence |
|---|---|---|
| `repository_missing` | `The repository is unavailable` | `The repository <rep id> is not there. The script was not started.` |
| `commit_missing` | `The ref did not resolve` | `'<ref>' names no commit in the repository <repo>. The script was not started.` |
| `too_large` | `The tree is too large` | `The commit's files add up to more than <TREE_MAX_BYTES> bytes. The script was not started.` |
| `git_failed` | `git failed` | `git could not read the repository <repo>. The script was not started.` |
| `timed_out` | `git took too long` | `git took longer than <OPERATION_SECONDS> seconds. The script was not started.` |
| `start_failed` | `The script could not start` | `The script's process could not be launched. The script was not started.` |
| `queue_abandoned` | `The run never left the queue` | `The run was waiting for a slot when scripts stopped. The script was not started.` |

Request:

```
GET /nightly-report/runs/run_d4a7e2c9f1b8630a/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_d4a7e2c9f1b8630a`. Its headline reads `failed`, of kind `err`, then `2026-10-01 09:14:02 UTC`, with no `started` before it and no duration after it. Beneath it is `div#failure`, of kind `err`, reading `The ref did not resolve` and `'release' names no commit in the repository nightly-report. The script was not started.` The run card's list holds `Status`, `failed` of kind `err`; `Script`, `nightly-report`; `Commit`, empty, since the ref never resolved; `Ref`, `release`; `Started`, `2026-10-01 09:14:02 UTC`; `Finished`, `2026-10-01 09:14:02 UTC`; no `Duration`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `c2f5a8d1e4b7093a6d9c2f5e8b1a4d70`; and `Output`, `stdout 0 B · stderr 0 B`. `section#input` reads `input.json, 2 B` and shows `{}`. There is no `section#stdout` and no `section#stderr`. `section#files` shows `No files.`. The page has no `div#running` and no `div#files-gone`.

Preconditions:

- The preamble's: `nightly-report`'s repository has no branch or tag `release`, and its bare directory's config holds `ikigenba.name` `nightly-report`.
- `run_d4a7e2c9f1b8630a` is recorded `failed` with reason `commit_missing`. Its folder holds `input.json`, `{}`, and no `tree/`, no `stdout` and no `stderr`.

Postconditions:

- Nothing has changed. git ran only to read the repository's name from `rep_9c2e4b7a1d3f8e05.git`, and wrote nothing.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user opens a failed run whose repository's name cannot be read

A failure sentence that names the repository names it as it is now, read as the page is drawn, so a repository renamed in repos shows its new name on the next page. When there is no name to read, the sentence names the repository by its id instead, still as plain text, and the page is otherwise the same: a repository whose bare directory is gone from `REPOS_DIR` and one whose directory holds no `ikigenba.name`, or one git cannot read, are worded alike. The page is still served, never answered as an error.

Request:

```
GET /nightly-report/runs/run_d4a7e2c9f1b8630a/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the page of `A user opens a run that failed to start`, but `div#failure` reads `The ref did not resolve` and `'release' names no commit in the repository rep_9c2e4b7a1d3f8e05. The script was not started.` The name `nightly-report` appears on the page only as the script's.

Preconditions:

- The preamble's, and `run_d4a7e2c9f1b8630a` as in `A user opens a run that failed to start`.
- `rep_9c2e4b7a1d3f8e05.git` has been removed from `REPOS_DIR` since the run failed. The page is the same when the directory is there but its config holds no `ikigenba.name`.

Postconditions:

- Nothing has changed. The run's record and folder are as they were, and nothing was written under `REPOS_DIR`.
- scripts wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user opens a run abandoned in the queue

A run still waiting in the queue when scripts stopped never started and never will: scripts records it `failed`, with the reason `queue_abandoned`, as it does one it finds still `queued` when it next starts (`S18`). Its page is that of any run that failed to start: the status and the time alone in the headline, the reason's title and sentence, `Finished` and no `Duration`. Unlike a run whose ref never resolved, its commit was resolved and unpacked before it was queued, so the card shows it.

Request:

```
GET /nightly-report/runs/run_c4a8e2f6b0d93157/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_c4a8e2f6b0d93157`. Its headline reads `failed`, of kind `err`, then `2026-10-05 09:31:50 UTC`, with no `started` before it and no duration after it. Beneath it is `div#failure`, of kind `err`, reading `The run never left the queue` and `The run was waiting for a slot when scripts stopped. The script was not started.` The run card's list holds `Status`, `failed` of kind `err`; `Script`, `nightly-report`; `Commit`, `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`; `Ref`, `main`; `Started`, `2026-10-05 09:31:50 UTC`; `Finished`, `2026-10-05 09:32:10 UTC`; no `Duration`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5`; and `Output`, `stdout 0 B · stderr 0 B`. `section#input` reads `input.json, 2 B` and shows `{}`. There is no `section#stdout` and no `section#stderr`. `section#files` shows `No files.`. The page has no `div#running` and no `div#files-gone`.

Preconditions:

- The preamble's.
- `run_c4a8e2f6b0d93157` of `nightly-report`, asked for at 2026-10-05T09:31:50Z as in `A user opens a queued run`, was still queued when scripts was told to stop at 2026-10-05T09:32:10Z, and is recorded `failed` with reason `queue_abandoned`, finished at that moment (`S18`). Its folder holds `input.json`, `{}`; `tree/`; and an empty `out/`; and no `stdout` and no `stderr`.

Postconditions:

- Nothing has changed. No git ran, since the sentence names no repository.
- scripts wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user opens a run whose files are gone

A run's record outlives its folder (`S20`): when the folder is no longer there, the page still shows the record whole and says, without error, that the files are no longer kept. It shows no input, no output and no files, and their downloads are not found (`S14`).

Request:

```
GET /nightly-report/runs/run_72b0c8f5e3d1a946/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run page of `run_72b0c8f5e3d1a946`. Its headline reads `exited 0`, of kind `ok`, then `started 2026-09-30 09:14:02 UTC · 11s`. Beneath it is `div#files-gone`, reading `Files no longer kept` and `This run's folder is gone, so its input, output and files cannot be shown. The record stays.` The run card's list holds `Status`, `exited 0` of kind `ok`; `Script`, `nightly-report`, linking to `/nightly-report/`; `Commit`, `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60`; `Ref`, `main`; `Started`, `2026-09-30 09:14:02 UTC`; `Finished`, `2026-09-30 09:14:13 UTC`; `Duration`, `11s`; `Trigger`, `manual`; `User`, `u_7f3a9c21`; `Request`, `5d8a1e4b7c0f3926a5d8e1b4c7f0a396`; and `Output`, `stdout 1.2 kB · stderr 0 B`, the sizes the record kept. The page has no `section#input`, `section#stdout`, `section#stderr` or `section#files`, and no `Download` link.

Preconditions:

- The preamble's.
- `run_72b0c8f5e3d1a946` is recorded `exited` with exit code 0, with `stdout` 1204 bytes and `stderr` 0, not truncated; `state/runs/scr_6d1f4a9b2e8c7035/run_72b0c8f5e3d1a946/` does not exist.

Postconditions:

- Nothing has changed. The record was not removed or marked, and no folder was created. No git ran.
- scripts wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user's client asks for a run page's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body. Every answer in this group answers `HEAD` the same way, with its own status and headers.

Request:

```
HEAD /nightly-report/runs/run_3f9a1c2e8b7d4a60/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is empty.

Preconditions:

- The preamble's, and `run_3f9a1c2e8b7d4a60` as in `A user opens a run that exited 0`.

Postconditions:

- Nothing has changed.
- The request's `request.started` has `method` `HEAD`, and its `request.finished` has `status` 200 and `response_bytes` 0, since no body was written.

## A user types a run's address without the trailing slash

`/nightly-report/runs/run_3f9a1c2e8b7d4a60` names the run, but its page's address ends in `/`, so scripts sends the browser there before drawing anything, the query going with it. It redirects only an address that names one of the caller's runs under that script; any other such address is not found, and is not redirected (`A user asks for a run that does not exist`).

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60?from=chat HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 301 Moved Permanently
Location: /nightly-report/runs/run_3f9a1c2e8b7d4a60/?from=chat
```

Status 301. No story fixes the body. Without a query, `GET /nightly-report/runs/run_3f9a1c2e8b7d4a60` is redirected to `Location: /nightly-report/runs/run_3f9a1c2e8b7d4a60/`. A `HEAD` is redirected the same way.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events, the `request.started` with `path` `/nightly-report/runs/run_3f9a1c2e8b7d4a60`, without its query, and the `request.finished` with `status` 301.

## A user asks for a run that does not exist

A run id that names no run of the script is answered with scripts' not-found page (`S12`). So is a run that once existed and is gone, pruned (`S19`) or deleted with its script (`S10`), and a last segment that is not a run id at all. An address without its trailing `/` is not redirected, since there is no run to add a slash for.

Request:

```
GET /nightly-report/runs/run_0a0b0c0d0e0f1a2b/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

```
GET /nightly-report/runs/run_0a0b0c0d0e0f1a2b HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

```
GET /nightly-report/runs/latest/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is scripts' not-found page (`S12`). Run ids are matched exactly: `/nightly-report/runs/RUN_3F9A1C2E8B7D4A60/` names no run.

Preconditions:

- The preamble's: no run has the id `run_0a0b0c0d0e0f1a2b`.

Postconditions:

- Nothing has changed. No run folder was read or created, and no git ran.
- scripts wrote nothing to stderr. telemetry has received the request's two events:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"9b3f7d1a5e0c2846b9d3f7a1e5c0d284","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/nightly-report/runs/run_0a0b0c0d0e0f1a2b/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"9b3f7d1a5e0c2846b9d3f7a1e5c0d284","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":404}}
  ```

## A user asks for a run under another of their scripts, or of another user's script

A run is found only under its own script and only among the caller's scripts, so its page has exactly one address and one viewer. A run of `sync-crm` asked for under `nightly-report`, `digest`'s run asked for under `nightly-report` or under `digest` itself, and a run of `nightly-report` asked for by another user are each answered with the not-found page, exactly as a run that does not exist; none is redirected, and the answer does not say the run exists or whose it is. `digest` is `u_2b8e1d04`'s, so `/digest/...` names none of `u_7f3a9c21`'s scripts; asked for by its owner, the same address is `digest`'s run page.

Request:

```
GET /nightly-report/runs/run_6b2d8f4a0c9e1735/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

```
GET /nightly-report/runs/run_0c4e8a2f6b1d9375/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

```
GET /digest/runs/run_0c4e8a2f6b1d9375/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is scripts' not-found page (`S12`), the same for each request, and the same as for a run that does not exist. Each address without its trailing `/` is answered the same way, not redirected.

Preconditions:

- The preamble's: `run_6b2d8f4a0c9e1735` is a run of `sync-crm`, and `run_0c4e8a2f6b1d9375` a run of `digest`, which `u_2b8e1d04` owns.

Postconditions:

- Nothing has changed. No run folder was read, and no git ran.
- scripts wrote nothing to stderr. telemetry has received each request's two events, each `request.finished` with `status` 404, under the caller's user, `u_2b8e1d04` for the last.

## A user opens a run page while scripts cannot reach its catalog

scripts finds a run by its script and id in the catalog, so when it cannot read the catalog it cannot tell a run from an address that names none. It does not answer not found, which would tell the user their run was gone, nor draw a page it cannot vouch for; it says plainly that it cannot reach the catalog, as every page does (`S03`), quoting nothing of the database's own error, and the user may try again later.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
```

Status 503. The body is exactly the one line `cannot reach the catalog; try again later`, ending in a newline. It has no banner and no footer. A `HEAD` is answered with the same status and headers and an empty body. Every run-level address is answered the same way, the address without its trailing `/` and a run id that names no run included.

Preconditions:

- scripts is serving, and telemetry takes every event.
- scripts' database cannot be read: `state/scripts.db` has become unreadable since scripts opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed. No run folder was read, and no git ran; a run still running goes on running.
- scripts wrote nothing to stderr. telemetry has received the request's two events under user `u_7f3a9c21`, the `request.finished` with `status` 503.
- scripts is still serving.

## A caller sends a run page a method it does not take

A run page only reads: it takes `GET` and `HEAD` and nothing else, and `Allow` names those two. A run is cancelled with the `cancel` tool (`S11`), never through its page, so a `DELETE` of a running run's page ends nothing. The method is checked before anything else, so the answer is the same for a run of the caller's, a run that does not exist, and a name that is no script.

Request:

```
POST /nightly-report/runs/run_3f9a1c2e8b7d4a60/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

```
DELETE /nightly-report/runs/run_8a2c6e1f9b3d5074/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

```
PUT /nosuch/runs/run_0a0b0c0d0e0f1a2b/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9b3f7d1a5e0c2846b9d3f7a1e5c0d284
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. `PATCH`, `OPTIONS`, and every other method but `GET` and `HEAD` are refused the same way, at a run's address with or without its trailing `/`.

Preconditions:

- The preamble's: `run_8a2c6e1f9b3d5074` is running, and no script is named `nosuch`.

Postconditions:

- Nothing has changed. `run_8a2c6e1f9b3d5074` is still running, its process untouched; no run was killed or recorded, and no catalog or run folder was read.
- scripts wrote nothing to stderr. telemetry has received each request's two events, each `request.finished` with `status` 405 and `response_bytes` 0, and no `run.*` event.
