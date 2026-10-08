# Stories — limits

What bounds the work sites does with git, so that no repository can fill its disk or hold it up. Two settings, each a positive whole number read once, at start, from sites' environment, whose default is the manifest's `[env]` (`S01`), which the host writes into `/etc/opt/ikigenba/sites/env`; an operator changes one there and restarts sites, and a developer sets it on the command line. Unset or empty, a setting is its default; a value that is not a positive whole number keeps sites from starting (`S02`). `SITE_MAX_BYTES`, 268435456 (256 MiB), is the most a site's tree may hold: the sum of the sizes of the files of the commit, as `git archive` emits them, neither the repository's size on disk nor the archive's. A tree whose files sum to exactly `SITE_MAX_BYTES` is unpacked; one byte more, and it is not. sites stops unpacking as soon as the sum passes the limit, and an unpack that stops leaves nothing of itself behind: a tree is servable only once it is whole. `OPERATION_SECONDS`, 600, is the longest one git run may take — resolving a ref, reading a repository's config, or unpacking a tree, each timed on its own; a git still running at the deadline is killed, and its run has failed. Both bound every unpack, a publish's and a rebuild's alike. A publish they stop is refused with a tool error, `site exceeds <n> bytes` or `git took longer than <n> seconds`, `<n>` being the setting, and changes nothing: the site goes on being served at the commit it had, by the requests that came during the publish too, and its cache holds what it held. A rebuild they stop leaves the site unavailable, with `reason` `too_large` or `timed_out` (`S16`). A tree already in the cache is not measured again: a lower limit applies to the next unpack, not to what is served now. The ordinary refusal of a publish over the default limit is `S08`'s; the stories here are the limits' edges and their settings. The actor is a model publishing through an MCP client, as `S08` sends `publish`, or a visitor reading the site meanwhile; sites runs on the host with the suite's services file (`S05`), telemetry takes every event, and the catalog holds `S06`'s shared catalog, among them the caller `u_7f3a9c21`'s `blog`, `sit_4e7a1c9b0d2f8635`, public and listed, over `rep_8c21d4e0f7a3b915` at `main`, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `2026-10-01T10:00:00Z`, whose tree is in `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/` and holds no file `big.bin`. The owner has since pushed one commit to the repository's `main`, `<sha>`, which adds `big.bin`. Every setting is unset unless a story sets it. Nothing in this group earns a line on stderr.

## A model publishes a site exactly at the size limit

The limit is inclusive: a tree whose files sum to exactly `SITE_MAX_BYTES` is published like any other.

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

- The preamble's, with `SITE_MAX_BYTES=1048576` set in `/etc/opt/ikigenba/sites/env` and sites restarted since.
- The files of the tree at `<sha>` sum to exactly 1048576 bytes.

Postconditions:

- `blog` is published at `<sha>` as of `<published>`. `cache/sites/sit_4e7a1c9b0d2f8635/` holds one tree, `<sha>/`, whose files sum to 1048576 bytes.
- `GET /blog/big.bin` is answered `200` with the bytes of `big.bin` at `<sha>` and `ETag: "<sha>"`.
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.published","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"<sha>","ref":"main","site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"publish"}}
  ```

## A model publishes a site one byte over the size limit

One byte past `SITE_MAX_BYTES` and the publish is refused, quoting the limit, so the model can tell its owner how much the site must shed. The site goes on being served at the commit it had.

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

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
site exceeds 1048576 bytes
```

Preconditions:

- The preamble's, with `SITE_MAX_BYTES=1048576` set in `/etc/opt/ikigenba/sites/env` and sites restarted since.
- The files of the tree at `<sha>` sum to 1048577 bytes.

Postconditions:

- `blog` is unchanged: published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `2026-10-01T10:00:00Z`, as `show` answers.
- `cache/sites/sit_4e7a1c9b0d2f8635/` holds one tree, `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`, as it was, and nothing of `<sha>`.
- `GET /blog/` is answered `200` with `ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"`; `GET /blog/big.bin` is answered `404` with the bytes of `404.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`: no part of the unpack that stopped is served.
- The repository is as it was.
- telemetry has received the request's three events, in this order, and no `site.published`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"publish"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A visitor reads a site while a publish over the limit is unpacking

A publish unpacks the new tree beside the one being served, and the site moves to the new commit only once the tree is whole (`S08`). A visitor who arrives while an oversized tree is on its way in is served the old commit, and is served it still once the publish has been refused.

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

Status 200, answered at once, without waiting for the publish. The body is exactly the bytes of `index.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's, with `SITE_MAX_BYTES=1048576` set in `/etc/opt/ikigenba/sites/env` and sites restarted since; the files of the tree at `<sha>` sum to 1048577 bytes.
- The model's `publish` of `blog`, as in `A model publishes a site one byte over the size limit`, is unpacking `<sha>` when this request arrives. The request is a guest's.

Postconditions:

- Nothing has changed but the trail. The publish is refused with `site exceeds 1048576 bytes`, and `blog` stays at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.
- The request recorded its `site.viewed` with `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and `status` 200.

## A model publishes again after the operator raises the size limit

The limit is the operator's to move. A site refused under one limit is published under a higher one, with no change to the repository.

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

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member and a `structuredContent` that is the one of `A model publishes a site exactly at the size limit`, `commit` `<sha>`; its `content` array is one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's; the files of the tree at `<sha>` sum to 1048577 bytes.
- With `SITE_MAX_BYTES=1048576`, a `publish` of `blog` was refused with `site exceeds 1048576 bytes`.
- The operator has since set `SITE_MAX_BYTES=2097152` in `/etc/opt/ikigenba/sites/env` and restarted sites.

Postconditions:

- `blog` is published at `<sha>`, and `cache/sites/sit_4e7a1c9b0d2f8635/` holds one tree, `<sha>/`.
- The request recorded `site.published` with `commit` `<sha>`, `ref` `main`, and `site` `sit_4e7a1c9b0d2f8635`, and its `tool.called` has `outcome` `ok`.

## A visitor reads a site that is over a lowered size limit

The limit is checked when a tree is unpacked, not each time it is served. Lowering it does not take down a site whose tree is already in the cache; the site goes on being served until its tree has to be unpacked again, by a publish (refused) or a rebuild (unavailable, `S16`).

Request:

```
GET /blog/about/ HTTP/1.1
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

Status 200. The body is exactly the bytes of `about/index.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's: `blog`'s tree at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` is in the cache. Its files sum to more than 1024 bytes.
- The operator has set `SITE_MAX_BYTES=1024` in `/etc/opt/ikigenba/sites/env` and restarted sites. The request is a guest's.

Postconditions:

- Nothing has changed but the trail. git did not run, and the tree in the cache is as it was.
- The request recorded its `site.viewed` with `status` 200 and no `site.unavailable`.

## A model publishes a site whose git runs past the deadline

A git run that is still going at `OPERATION_SECONDS` — a repository on a stalled disk, a tree that unpacks too slowly — is killed, and the publish is refused, quoting the deadline. Like any refused publish it changes nothing, and nothing of the unpack it cut off is served.

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

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, answered about 60 seconds after the git run that overran began. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
git took longer than 60 seconds
```

With `OPERATION_SECONDS` unset, the text is `git took longer than 600 seconds`.

Preconditions:

- The preamble's, with `OPERATION_SECONDS=60` set in `/etc/opt/ikigenba/sites/env` and sites restarted since.
- git, reading `/var/opt/ikigenba/repos/state/repos/rep_8c21d4e0f7a3b915.git`, would take longer than 60 seconds to unpack `<sha>`.

Postconditions:

- `blog` is unchanged: published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, its tree in the cache as it was, and nothing of `<sha>` there.
- No git started by sites is still running.
- telemetry has received the request's `request.started`, then its `tool.called` with `kind` `additive`, `outcome` `error`, and `tool` `publish`, then its `request.finished`; no `site.published`.
