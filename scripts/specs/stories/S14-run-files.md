# Stories — run files

Downloading what a run kept: what a running scripts (`S02`) answers at the paths beneath a run's page, `/<name>/runs/<run id>/<file>`, which the run page's Download buttons lead to (`S13`). A run has exactly four kinds of file to give: `/<name>/runs/<run id>/input.json`, the input the run was given (`S08`, `S15`); `/<name>/runs/<run id>/stdout` and `/<name>/runs/<run id>/stderr`, what its process wrote to each stream, as kept (`S15`, `S17`); and `/<name>/runs/<run id>/out/<path>`, a file the script wrote under its `out/` folder, where `<path>` is the file's path relative to `out/`, its segments separated by `/`. Each is read from the run's folder, `state/runs/<script id>/<run id>/` under scripts' working directory (`S20`), as it is at the moment of the request, and from nowhere else. scripts answers such a path in this order: the caller's identity first, as on every route (`S03`); then a method other than `GET` or `HEAD`, refused with `405`; then the name, looked up among the caller's own scripts (`S12`), and the run, looked up among that script's runs (`S13`), a catalog it cannot read being answered `503`; and then the file. A path names a file only when it is one of the four forms above; every segment is a name, never empty and never `.` or `..`, whether written plainly or percent-encoded, and scripts does not tidy a path into another one; and the file it names is a regular file reached without following a symlink, at any step of the path. Anything else — a path naming no file, a directory, a symlink, a path through a symlinked directory, a file the run never wrote, any file of a run whose folder is gone, and any path under a run that is not the caller's or not that script's — is answered with scripts' not-found page (`S12`), status `404`, and is never redirected. A served file's body is the file's bytes exactly, nothing converted and nothing added, and `Content-Length` is its size in bytes. `input.json`, `stdout` and `stderr` are answered `Content-Type: text/plain; charset=utf-8` with no `Content-Disposition`, so a browser shows them; a file under `out/` is answered `Content-Type: application/octet-stream` with `Content-Disposition: attachment; filename="<base name>"`, its name without the folders above it, whatever its extension, so a browser saves it and never renders a script's output as a page of scripts. Every file is answered with `X-Content-Type-Options: nosniff`. No caching header is fixed.

Unless a story says otherwise, scripts runs with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json`, the suite's services file (`S03`), and telemetry takes every event. The catalog is `S06`'s shared catalog, and the caller is `u_7f3a9c21`, `mg@example.com`, who owns `nightly-report`, `scr_6d1f4a9b2e8c7035`, and `sync-crm`; `digest` is `u_2b8e1d04`'s. Most stories here download the files of `run_3f9a1c2e8b7d4a60`, the run of `nightly-report` that exited 0, whose folder, `state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60/`, holds `tree/`, the commit's files, `main.py` among them; `input.json`, 69 bytes; `stdout`, 1229 bytes; `stderr`, empty; and under `out/`, `charts/sales.svg`, 12034 bytes, `report.csv`, 7904 bytes, and `report.html`, 48211 bytes. `<file:<path>>` below stands for the bytes of `<path>` in that folder. The requests carry `Host: scripts.sbx.ikigenba.dev`, `X-Forwarded-Proto: https`, the caller's `X-User-Id` and `X-User-Email`, and nginx's `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`. Every request adds exactly two events to scripts' trail, `request.started` and `request.finished` (`S02`), under that request id and the caller's user, and no other: a download is no domain event. A download reads the catalog and the run's folder only: it reads no repository, runs no git and no script, and changes nothing, not even for a run still running. No answer in this group earns a line on stderr. Every answer in this group answers `HEAD` with the status and headers its `GET` has and an empty body. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A user downloads a run's input

The run page shows the input the run was given; its Download button fetches the file itself, the same bytes the script read from `IKIGENBA_INPUT`, so the run can be replayed by hand.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/input.json HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Content-Length: 69
X-Content-Type-Options: nosniff
```

Status 200. The body is exactly `<file:input.json>`, these 69 bytes, with no LF after them:

```
{"since":"2026-10-04","channels":["sales","support"],"dry_run":false}
```

There is no `Content-Disposition`. A run given no input has the input `{}` (`S08`): `GET /nightly-report/runs/run_d4a7e2c9f1b8630a/input.json`, the run that failed to start, is answered the same way with `Content-Length: 2` and the body `{}`.

Preconditions:

- scripts is serving, and telemetry takes every event.
- The catalog holds `S06`'s shared catalog, and `run_3f9a1c2e8b7d4a60`'s folder is as the preamble says.

Postconditions:

- Nothing has changed. The catalog and the run's folder were read and not written; no repository was read, and no git ran.
- scripts wrote nothing to stderr and set no cookie. telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/nightly-report/runs/run_3f9a1c2e8b7d4a60/input.json"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":69,"status":200}}
  ```

## A user downloads a run's standard output

A script's output can be longer than a page shows comfortably, and a user may want it in a file of its own or in a tool of their own. Each stream downloads whole, as kept, and an empty stream downloads as an empty file, not as a missing one.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/stderr HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Content-Length: <bytes>
X-Content-Type-Options: nosniff
```

Status 200. For `stdout`, `Content-Length` is `1229` and the body is exactly `<file:stdout>`, its 1229 bytes, whose first line, an excerpt, is `reading events since 2026-10-04 from telemetry`. For `stderr`, `Content-Length` is `0` and the body is empty. There is no `Content-Disposition`. A stream cut at `OUTPUT_MAX_BYTES` is served as kept: `GET /nightly-report/runs/run_5e8b3d7a1c0f6294/stdout`, the run that timed out, is answered with the 1048576 bytes of its `stdout`, and `Content-Length: 1048576`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received each request's two events, the `request.finished` with `status` 200 and `response_bytes` 1229 for `stdout` and 0 for `stderr`.

## A user downloads the output of a run still running

The run page does not stream (`S13`), and neither does a download: it is the file as it stands when scripts reads it, and the user downloads again for more. The run is not touched by it and runs on.

Request:

```
GET /nightly-report/runs/run_8a2c6e1f9b3d5074/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Content-Length: 214
X-Content-Type-Options: nosniff
```

Status 200. The body is exactly the 214 bytes `state/runs/scr_6d1f4a9b2e8c7035/run_8a2c6e1f9b3d5074/stdout` held when scripts read it. A later request answers with what the file holds by then. A file the script has already written under `out/` downloads the same way, as it stands.

Preconditions:

- The preamble's: `run_8a2c6e1f9b3d5074` is `running`, and its `stdout` holds 214 bytes and grows no further while the request is answered.

Postconditions:

- Nothing has changed. `run_8a2c6e1f9b3d5074` is still `running`, its process untouched.
- telemetry has received the request's two events, the `request.finished` with `status` 200 and `response_bytes` 214.

## A user downloads a file the run wrote

The Files table on the run page lists what the script wrote under `out/` (`S13`), and each file downloads at its path beneath `out/`, folders included. Whatever the script named it, it is the script's output, not a page of scripts: it is sent as opaque bytes for the browser to save, under its own name.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts/sales.svg HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/octet-stream
Content-Length: 12034
Content-Disposition: attachment; filename="sales.svg"
X-Content-Type-Options: nosniff
```

Status 200. The body is exactly `<file:out/charts/sales.svg>`, its 12034 bytes. `GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/report.html` is answered the same way, never as `text/html`, with `Content-Length: 48211` and `Content-Disposition: attachment; filename="report.html"`, and `out/report.csv` with `Content-Length: 7904` and `filename="report.csv"`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events, the `request.started` with `path` `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts/sales.svg` and the `request.finished` with `status` 200 and `response_bytes` 12034.

## A user's client asks for a run file's headers

A `HEAD` is answered as the `GET` would be, status and headers alike, `Content-Length` the length the `GET` would send, with no body.

Request:

```
HEAD /nightly-report/runs/run_3f9a1c2e8b7d4a60/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
HEAD /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts/sales.svg HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: <type>
Content-Length: <bytes>
X-Content-Type-Options: nosniff
```

Status 200. The body is empty. For `stdout`, `<type>` is `text/plain; charset=utf-8` and `<bytes>` is `1229`; for `out/charts/sales.svg`, `<type>` is `application/octet-stream`, `<bytes>` is `12034`, and the answer also carries `Content-Disposition: attachment; filename="sales.svg"`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received each request's two events, the `request.started` with `method` `HEAD` and the `request.finished` with `status` 200 and `response_bytes` 0.

## A user asks for a stream a run never wrote

`run_d4a7e2c9f1b8630a` failed to start because its ref named no commit, so no tree was unpacked and no process ran (`S08`): its folder holds its `input.json` alone, and it has no standard output or error to give. A stream that does not exist is not an empty one, so it is answered not found, and the run page offers no download for it (`S13`). A queued run, whose script has not started, has neither stream yet either, and its `stdout` and `stderr` are answered the same way until its process starts (`A user opens a queued run`, `S13`).

Request:

```
GET /nightly-report/runs/run_d4a7e2c9f1b8630a/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_d4a7e2c9f1b8630a/stderr HTTP/1.1
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

Status 404. The body is scripts' not-found page (`S12`): titled `Not found`, its heading `Not found` and its text `There is nothing at this address.`, with the footer `scripts v<semver>`, no banner, and no script but the button feedback script.

Preconditions:

- The preamble's: `run_d4a7e2c9f1b8630a` is `failed` with reason `commit_missing`, and its folder, `state/runs/scr_6d1f4a9b2e8c7035/run_d4a7e2c9f1b8630a/`, holds `input.json`, and no `tree/`, no `stdout` and no `stderr`.

Postconditions:

- Nothing has changed: no `stdout` or `stderr` was created in the run's folder.
- telemetry has received each request's two events, the `request.finished` with `status` 404.

## A user asks for a path under a run that is not one of its files

A run gives its input, its two streams and the files under `out/`, and nothing else. Its `tree/` is the commit's files, which belong to the repository, not to the run, and is never served. A directory is not a file: `out/` and `out/charts` are answered not found, with or without a trailing `/`, and are not redirected, since a run has no listing of its own beyond the run page. A path naming a file the run did not write names nothing.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/tree/main.py HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts/ HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/missing.txt HTTP/1.1
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

Status 404. The body is scripts' not-found page (`S12`). `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out`, `/nightly-report/runs/run_3f9a1c2e8b7d4a60/tree/`, `/nightly-report/runs/run_3f9a1c2e8b7d4a60/stdout/`, `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out//report.csv`, and `/nightly-report/runs/run_3f9a1c2e8b7d4a60/Stdout` are answered the same way; none is redirected.

Preconditions:

- The preamble's: `run_3f9a1c2e8b7d4a60`'s `tree/` holds `main.py`, and its `out/` holds the directory `charts` and no `missing.txt`.

Postconditions:

- Nothing has changed. No file under `tree/` was read.
- telemetry has received each request's two events, the `request.started` with its `path` as it arrived and the `request.finished` with `status` 404.

## A user's request climbs out of out/ with dot segments

A file under `out/` is found within `out/` and never above it, and scripts does not tidy a path into another one: a segment that is `..` or `.` makes the path name nothing, even where tidying would land on a real file. So a request can reach neither the run's own `input.json` or `tree/` through `out/`, nor another run's folder, nor a file outside the run, nor one file under a second address.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts/../report.csv HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/../tree/main.py HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/../../run_c71d0b5e4a2f9386/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/./report.csv HTTP/1.1
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

Status 404. The body is scripts' not-found page (`S12`). No form is redirected, and none is answered with `report.csv`, `main.py`, or another run's output. A segment percent-encoded as `%2e%2e` or `%2E`, `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out/%2e%2e/input.json` say, is the same segment and is answered the same way, and so is a `..` or `.` before `out/`, `/nightly-report/runs/run_3f9a1c2e8b7d4a60/./stdout` say.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No file outside `state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60/out/` was read.
- telemetry has received each request's two events, the `request.started` with its `path` as it arrived, `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out/charts/../report.csv` say, and the `request.finished` with `status` 404.

## A user asks for a symlink a run wrote under out/

A script may leave anything under `out/`, a symlink included, and a symlink is never followed, whether it points inside `out/`, elsewhere in the run's folder, or out of it altogether, so a script cannot make scripts serve a file it did not write there, nor one file under two names. A path that passes through a symlinked directory is answered the same way.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/latest.csv HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/input.json HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/passwd HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/graphs/sales.svg HTTP/1.1
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

Status 404. The body is scripts' not-found page (`S12`), never `<file:out/report.csv>`, `<file:input.json>`, the host's `/etc/passwd`, or `<file:out/charts/sales.svg>`. The files the symlinks point at are served at their own addresses as ever: `out/report.csv` and `input.json` as in `A user downloads a file the run wrote` and `A user downloads a run's input`.

Preconditions:

- The preamble's, and besides its files the script also left under its `out/`: `latest.csv`, a symlink to `report.csv`; `input.json`, a symlink to `../input.json`; `passwd`, a symlink to `/etc/passwd`; and `graphs`, a symlink to the directory `charts`.

Postconditions:

- Nothing has changed. No symlink was followed, and `/etc/passwd` was not read.
- telemetry has received each request's two events, the `request.finished` with `status` 404.

## A user downloads a hidden file a run wrote

A run's `out/` is its owner's own output, not a site published to the world, so a file or folder whose name begins with `.` is served like any other: the script chose to write it, and the run page lists it with the rest (`S13`).

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/.cache/state.json HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/octet-stream
Content-Length: <bytes>
Content-Disposition: attachment; filename="state.json"
X-Content-Type-Options: nosniff
```

Status 200. The body is exactly `<file:out/.cache/state.json>`, and `Content-Length` is its size. A file named `.env` directly under `out/` is served the same way, with `filename=".env"`.

Preconditions:

- The preamble's, and besides its files the script also wrote `out/.cache/state.json`, a regular file.

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events, the `request.finished` with `status` 200.

## A user asks for a file of a run whose files are gone

A run whose folder is gone keeps its record (`S20`), and its page says its files are no longer kept (`S13`), so it offers no download. Asked for anyway, every file of such a run is answered not found: there is nothing to serve, and it is not an error.

Request:

```
GET /nightly-report/runs/run_72b0c8f5e3d1a946/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_72b0c8f5e3d1a946/input.json HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_72b0c8f5e3d1a946/out/report.html HTTP/1.1
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

Status 404. The body is scripts' not-found page (`S12`). `stderr` and every other path under the run's page are answered the same way.

Preconditions:

- The preamble's: the catalog holds `run_72b0c8f5e3d1a946`, `exited` 0, and `state/runs/scr_6d1f4a9b2e8c7035/run_72b0c8f5e3d1a946/` does not exist.

Postconditions:

- Nothing has changed: the run's record stays, and no folder was created for it.
- scripts wrote nothing to stderr. telemetry has received each request's two events, the `request.finished` with `status` 404.

## A user asks for a file of another user's run

A user sees only their own runs. `run_0c4e8a2f6b1d9375` is a run of `digest`, which is `ann@example.com`'s, so to `mg@example.com` its files are answered exactly as files of a run that does not exist. A run is also found only under its own script: `run_3f9a1c2e8b7d4a60` is `nightly-report`'s, so under `sync-crm`, though the caller owns both, it names nothing. And the caller's own run is not found by another user.

Request:

```
GET /digest/runs/run_0c4e8a2f6b1d9375/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /sync-crm/runs/run_3f9a1c2e8b7d4a60/input.json HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/report.csv HTTP/1.1
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

Status 404. The body is scripts' not-found page (`S12`), byte for byte the page a path naming nothing gets. A run id that no run has, `/nightly-report/runs/run_0000000000000000/stdout` say, is answered the same way.

Preconditions:

- The preamble's: `digest`, owned by `u_2b8e1d04`, holds `run_0c4e8a2f6b1d9375`, whose folder holds its `stdout`; `sync-crm` and `nightly-report` are `u_7f3a9c21`'s.

Postconditions:

- Nothing has changed. No run folder was read.
- telemetry has received each request's two events under its caller's user, the `request.finished` with `status` 404.

## A user asks for a run file while scripts cannot reach its catalog

scripts finds a run by its script and its id in the catalog, so when it cannot read the catalog it cannot tell whose a run folder is. It serves nothing it cannot vouch for, and does not answer as if the run were gone; it says plainly that it cannot reach the catalog, as the catalog page does (`S03`), quoting nothing of the database's own error.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/stdout HTTP/1.1
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

Status 503. The body is exactly the one line `cannot reach the catalog; try again later`, ending in a newline. Every path this group answers, but for a method it does not take, is answered the same way.

Preconditions:

- scripts is serving, and telemetry takes every event.
- scripts' database cannot be read: `state/scripts.db` has become unreadable since scripts opened it, its storage failing reads, say.
- `state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60/stdout` is there and readable.

Postconditions:

- Nothing has changed. No run folder was read.
- scripts wrote nothing to stderr. telemetry has received the request's two events, the `request.finished` with `status` 503.
- scripts is still serving.

## A caller sends a run file a method it does not take

A run's files are its record and are read-only through the web: a run file's path takes `GET` and `HEAD` and nothing else, and `Allow` names those two. The method is looked at after the caller's identity (`S03`) and before anything else, so the answer is the same for a file the run has, a path that names nothing, and a run or script that is not the caller's, and the catalog is not read.

Request:

```
PUT /nightly-report/runs/run_3f9a1c2e8b7d4a60/out/report.csv HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
DELETE /nightly-report/runs/run_3f9a1c2e8b7d4a60/stdout HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
POST /digest/runs/run_0c4e8a2f6b1d9375/input.json HTTP/1.1
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

Status 405. The body is empty. `PATCH`, `OPTIONS`, and every other method but `GET` and `HEAD` are refused the same way, on every path this group answers.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: every file in the run's folder is as it was. The catalog was not read.
- telemetry has received the request's two events, the `request.finished` with `status` 405 and `response_bytes` 0.
