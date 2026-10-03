# Stories — unavailable

What happens when the tree a site is published at is not in sites' cache, and what a visitor sees when sites cannot put it back. A published site is served from `cache/sites/<site id>/<sha>/` under sites' working directory, `/opt/sites` on a host (`S18`). `cache/` is disposable: it is never backed up, so it is empty after a host is restored, and an operator may empty it at any time. The catalog keeps each site's published commit regardless, and sites rebuilds a missing tree on demand, in the request that needs it, never at start: it unpacks the commit from the site's repository under `REPOS_DIR` with the host's `git archive`, bounded by `SITE_MAX_BYTES` and `OPERATION_SECONDS` (`S17`), and then answers the request as if the tree had been there all along. Requests for the same site that arrive while its rebuild runs wait for that rebuild and share it: git unpacks the tree once. A tree is servable only once it is whole; a rebuild that fails leaves nothing of itself behind. When a rebuild fails, the visitor is answered `503` with sites' unavailable page — an HTML page whose `h1` reads `Site unavailable` and whose `p#unavailable` reads `This site is not available right now. Try again in a moment.`, with the footer and no banner, whether the visitor is signed in or not — with `Content-Type: text/html; charset=utf-8`, `Retry-After: 60`, the site's `Cache-Control`, and no `ETag`; the visitor cookie is set when the request carried none (`S14`). The request records its `site.viewed`, with `status` 503 and the published commit, then `site.unavailable`, with `site`, `commit`, and `reason`, the one word that says why (`S15`): `repository_missing`, `commit_missing`, `too_large`, `timed_out`, or `git_failed`. Every request answered `503` records its own pair. Nothing about the site changes: it stays published at the same commit, as `show` and `list` (`S07`) and the landing page (`S03`) say, and sites remembers no failure, so the next request tries the rebuild again and is served the moment it succeeds. What git wrote to its stderr goes nowhere, and sites writes nothing to its own stderr: an unavailable site is a fact of the trail (`S15`), not trouble. Unless a story says otherwise, sites runs on the host with `REPOS_DIR`, `SITE_MAX_BYTES`, and `OPERATION_SECONDS` unset, so it reads repos' repositories at `/opt/repos/state/repos` (`S18`); telemetry takes every event; and the catalog holds `S06`'s shared catalog, among them the caller `u_7f3a9c21`'s `blog`, `sit_4e7a1c9b0d2f8635`, public, over `rep_8c21d4e0f7a3b915`, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `2026-10-01T10:00:00Z`, whose tree holds `index.html`, `about/index.html`, `style.css`, `logo.png`, `notes.txt`, `404.html`, `.env`, `link.html`, and `data.blob`; and `handbook`, `sit_9a3c5e7b1d0f2468`, private, over `rep_3f9a0c1d2e4b5a69`, published at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`. `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree: `cache/` was emptied, or the host was restored without it. The visitor's browser holds the cookie `ikigenba_visitor=vis_1a2b3c4d5e6f7081`.

## A visitor opens a site whose tree is not in the cache

The first request for `blog` after its tree went missing finds no tree, so sites unpacks the published commit from the repository and serves from it. The visitor sees the page, a moment later than usual, and nothing else; later requests are served from the rebuilt tree without git.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

Status 200. The body is exactly the bytes of `index.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's: `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` exists and holds `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`. The request is a guest's.

Postconditions:

- `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/` holds the commit's tree, as `S18` lays it out.
- `blog` is unchanged in the catalog: still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `2026-10-01T10:00:00Z`.
- A request for `/blog/style.css` made now is answered `200` from that tree, and git does not run for it.
- telemetry has received the request's three events, in this order; a rebuild that succeeds records no event of its own:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"vis_1a2b3c4d5e6f7081"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

## Two visitors open a site while its tree is being rebuilt

A page brings its stylesheet and images with it, so the requests after a cache is emptied come together. The second request for `blog`, arriving while the first one's rebuild runs, waits for that rebuild rather than starting another, and both are answered from the one tree it unpacks.

Request:

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
Cache-Control: public, no-cache
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

Status 200, answered once the rebuild the earlier request started has finished. The body is exactly the bytes of `style.css` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's: the repository holds the commit.
- A guest's `GET /blog/`, request id `<request-id>`, arrived first; its rebuild of `blog`'s tree is running when this request arrives.

Postconditions:

- The guest's `GET /blog/` was answered `200` with the bytes of `index.html`, as in `A visitor opens a site whose tree is not in the cache`.
- git unpacked `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` once for the two requests: one `git archive` ran.
- `cache/sites/sit_4e7a1c9b0d2f8635/` holds one tree, `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`.
- Each request recorded its own `site.viewed` with `status` 200 under its own request id; this one's is:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/style.css","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"vis_1a2b3c4d5e6f7081"}}
  ```

## A visitor opens a site whose repository is gone

The tree must be rebuilt, but the site's repository is no longer in repos' directory: its owner deleted it in repos, or it was lost and not yet restored (`S18`). There is nothing to unpack, so the visitor is told the site is unavailable for now, and the trail says why.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Retry-After: 60
```

Status 503. The body is sites' unavailable page: its `h1` reads `Site unavailable` and its `p#unavailable` reads `This site is not available right now. Try again in a moment.`; it carries the footer and no banner. There is no `ETag`.

Preconditions:

- The preamble's, except that `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` does not exist. The request is a guest's.

Postconditions:

- `blog` is still published: `show` with `{"name":"blog"}` answers `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and `published` `2026-10-01T10:00:00Z`, as before.
- `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree.
- Nothing was created under `/opt/repos/state/repos/`.
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":503,"visitor":"vis_1a2b3c4d5e6f7081"}}
  {"time":"<time>","service":"sites","event":"site.unavailable","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","reason":"repository_missing","site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":503}}
  ```

- sites wrote nothing to stderr.

## A visitor opens a site whose commit is gone from its repository

The repository is there, but no longer holds the commit the site is published at: its owner rewrote `main` without it, and repos' maintenance has since pruned it. sites serves only the commit the site was published at, never whatever the ref names now, so it cannot serve the site until its owner publishes again (`A model republishes a site that could not be rebuilt`).

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Retry-After: 60
```

Status 503. The body is sites' unavailable page, as in `A visitor opens a site whose repository is gone`; the visitor is signed in, and the page still carries no banner.

Preconditions:

- The preamble's, except that `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` does not hold `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`: `git --git-dir=/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git cat-file -e 5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` exits non-zero. Its `main` is at `<sha>`, another commit.

Postconditions:

- `blog` is still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; nothing of `<sha>` was unpacked, and `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree.
- Between the request's `request.started` and its `request.finished`, whose `status` is 503, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":503,"visitor":"vis_1a2b3c4d5e6f7081"}}
  {"time":"<time>","service":"sites","event":"site.unavailable","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","reason":"commit_missing","site":"sit_4e7a1c9b0d2f8635"}}
  ```

## A visitor opens a site whose tree is now over the size limit

`SITE_MAX_BYTES` bounds every unpack, a rebuild's as much as a publish's (`S17`). An operator lowered it after `blog` was published, and the tree that was within the old limit is over the new one, so the rebuild stops without unpacking it and the site is unavailable until the limit is raised again or the owner publishes a smaller commit.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Retry-After: 60
```

Status 503. The body is sites' unavailable page, as in `A visitor opens a site whose repository is gone`.

Preconditions:

- The preamble's, except that `SITE_MAX_BYTES=1024` is set in `/opt/sites/etc/env` and sites was restarted since; the files of `blog`'s tree at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` sum to more than 1024 bytes. The repository holds the commit. The request is a guest's.

Postconditions:

- `blog` is still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree and no part of one.
- Between the request's `request.started` and its `request.finished`, whose `status` is 503, telemetry has received the request's `site.viewed`, with `status` 503, and then:

  ```
  {"time":"<time>","service":"sites","event":"site.unavailable","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","reason":"too_large","site":"sit_4e7a1c9b0d2f8635"}}
  ```

## A visitor opens a site whose rebuild runs past the deadline

Each git run of a rebuild has `OPERATION_SECONDS` (`S17`). A git still running at the deadline — the repository's disk has stalled, say — is killed, and the request that was waiting on it is answered `503` then, rather than left hanging. Every request that shared the rebuild is answered the same way at the same moment.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Retry-After: 60
```

Status 503, answered about 60 seconds after the rebuild's git began. The body is sites' unavailable page, as in `A visitor opens a site whose repository is gone`.

Preconditions:

- The preamble's, except that `OPERATION_SECONDS=60` is set in `/opt/sites/etc/env` and sites was restarted since, and git, reading `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git`, would take longer than 60 seconds to unpack `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`. The request is a guest's.

Postconditions:

- `blog` is still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree and no part of one; no git started by sites is still running.
- Between the request's `request.started` and its `request.finished`, whose `status` is 503, telemetry has received the request's `site.viewed`, with `status` 503, and then:

  ```
  {"time":"<time>","service":"sites","event":"site.unavailable","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","reason":"timed_out","site":"sit_4e7a1c9b0d2f8635"}}
  ```

## A visitor opens a site after git was removed from the host

sites checks for `git` when it starts (`S02`), but a host's `git` can go after that, uninstalled while sites runs. A rebuild that cannot run git fails like one whose git fails, and the site is unavailable until git is back; sites goes on serving every tree already in its cache.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Retry-After: 60
```

Status 503. The body is sites' unavailable page, as in `A visitor opens a site whose repository is gone`.

Preconditions:

- The preamble's, except that sites started with `git` on its `PATH`, and `git` has since been removed from the host, so no directory on sites' `PATH` holds it. The repository holds the commit. The request is a guest's.

Postconditions:

- `blog` is still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree.
- sites is still running, and a request for a site whose tree is in the cache is answered `200` from it.
- Between the request's `request.started` and its `request.finished`, whose `status` is 503, telemetry has received the request's `site.viewed`, with `status` 503, and then:

  ```
  {"time":"<time>","service":"sites","event":"site.unavailable","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","reason":"git_failed","site":"sit_4e7a1c9b0d2f8635"}}
  ```

- sites wrote nothing to stderr.

## A visitor opens a site whose repository git cannot read

The repository is there and holds the commit, but git fails to unpack it: one of the files of the commit's tree is missing from the repository, which is damaged. git's own complaint goes nowhere; the trail says only that git failed. The unpack that got part of the way leaves nothing servable behind.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Retry-After: 60
```

Status 503. The body is sites' unavailable page, as in `A visitor opens a site whose repository is gone`.

Preconditions:

- The preamble's, except that `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` holds the commit `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` but not the blob of its `style.css`, so `git --git-dir=/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git archive --format=tar 5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` exits non-zero. The request is a guest's.

Postconditions:

- `blog` is still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree and no part of one, so a request for `/blog/index.html` is answered `503` too, never from a half-unpacked tree.
- The repository is as it was: sites did not try to repair it.
- Between the request's `request.started` and its `request.finished`, whose `status` is 503, telemetry has received the request's `site.viewed`, with `status` 503, and then:

  ```
  {"time":"<time>","service":"sites","event":"site.unavailable","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","reason":"git_failed","site":"sit_4e7a1c9b0d2f8635"}}
  ```

- sites wrote nothing to stderr.

## A visitor opens a site again once its repository is back

An unavailable site heals by itself when what it lacked comes back. sites keeps no record of the failure, so the first request after the repository is restored to repos' directory rebuilds the tree as if nothing had happened, and the site is served again. No one had to republish.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

Status 200. The body is exactly the bytes of `index.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's. A minute ago, with `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` missing, a request for `/blog/` was answered `503` and recorded `site.unavailable` with `reason` `repository_missing`, as in `A visitor opens a site whose repository is gone`.
- Since then the repository was restored to `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git`, holding `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`. sites was not restarted, and no tool was called. The request is a guest's.

Postconditions:

- `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/` holds the commit's tree.
- `blog` is unchanged in the catalog.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received its `site.viewed`, with `status` 200, and no `site.unavailable`.

## A model republishes a site that could not be rebuilt

When the commit a site is published at is gone for good, its owner publishes again. `publish` (`S08`) resolves the site's ref to the commit it names now and unpacks that, so the site is served again, from the new commit, at the same URL.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"<sha>","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<published>` is the time of the call.

Preconditions:

- The preamble's, except that the repository no longer holds `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and its `main` is at `<sha>`, as in `A visitor opens a site whose commit is gone from its repository`; requests for `/blog/` have been answered `503`.

Postconditions:

- `blog` is published at `<sha>` as of `<published>`, and `cache/sites/sit_4e7a1c9b0d2f8635/` holds one tree, `<sha>/`.
- A `GET /blog/` made now is answered `200` with `ETag: "<sha>"` and the bytes of `index.html` at `<sha>`, and records `site.viewed` with `commit` `<sha>` and `status` 200.
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.published","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"<sha>","ref":"main","site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"publish"}}
  ```

## A guest opens a private site whose tree is not in the cache

A private site sends a guest to sign in before anything else is done for the request (`S12`), so a guest never sets off a rebuild of a private site, and never learns whether one would succeed. The tree is rebuilt for the first signed-in user who asks.

Request:

```
GET /handbook/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fhandbook%2F
```

Status 302. No `Set-Cookie`.

Preconditions:

- The preamble's, except that `cache/sites/sit_9a3c5e7b1d0f2468/` holds no tree either, and the browser holds no visitor cookie. The request carries no identity headers.

Postconditions:

- git did not run: `cache/sites/sit_9a3c5e7b1d0f2468/` still holds no tree.
- telemetry has received only the request's two events, `request.started` and `request.finished` with `status` 302, each with an empty `user`; no `site.viewed` and no `site.unavailable`.
