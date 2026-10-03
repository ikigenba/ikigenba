# Stories — publish

`publish`, the tool that puts a commit of a site's repository on the web at the site's URL. It takes `name`, required, a string, the name of one of the caller's sites, and `ref`, optional, a string: a branch, a tag, or a sha. Without `ref` it takes the site's own ref (`S06`); with one, that ref is used for this publish only and the site's ref is unchanged. In the site's repository, `<REPOS_DIR>/<repo>.git`, it resolves the ref with `git --git-dir=<dir> rev-parse --verify '<ref>^{commit}'`, unpacks the commit's tree with `git --git-dir=<dir> archive --format=tar <sha>` into `cache/sites/<site id>/<sha>/`, and only once the tree is whole repoints the site at the sha, in one step: until then every request for the site is served from the commit it had, and from then on from the new one, never from a mix (`S11`). It then removes every other tree under `cache/sites/<site id>/`. The unpack is bounded: a tree whose files add up to more than `SITE_MAX_BYTES` bytes is not unpacked (`S17`), and each git run is killed once it has run `OPERATION_SECONDS` seconds. The repository's owner is not checked again: the site publishes whatever its repository holds. The result is the site object `show` answers (`S07`), with `commit` the sha published and `published` the time of this publish, RFC 3339 UTC to the second. Publishing the sha a site already has is not a refusal: it publishes again and records it again. A publish that fails changes nothing: the site keeps its commit, its `published`, and its tree, and is served as before, and no part of the refused tree is left under `cache/` to be served. The failures are told one per story below, each a single line, or for git's own failure a line and git's stderr; they are checked in this order: the site, the repository, the ref, the unpack.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `blog` (`sit_4e7a1c9b0d2f8635`, repository `rep_8c21d4e0f7a3b915`, ref `main`, published `2026-10-01T10:00:00Z` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, its tree at `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`) and `scratch` (`sit_2d6f8a0c4e1b3957`, slug `scratch-7c1e9a4f`, repository `rep_8c21d4e0f7a3b915`, ref `preview`, unpublished, nothing under `cache/sites/sit_2d6f8a0c4e1b3957/`), and `u_2b8e1d04` owns `recipes`; in `rep_8c21d4e0f7a3b915`, `main` and the tag `v1` are at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and `preview` at `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`. sites runs with `SITE_MAX_BYTES` `268435456` and `OPERATION_SECONDS` `600`, the manifest's values (`S01`). `publish` is of kind `additive`; a publish that succeeds records `site.published`, with `site`, the id, `commit`, the sha, and `ref`, the ref as given or, without one, the site's ref, before its `tool.called`; the name is in no event. A refusal records no `site.*` event; in particular a failed publish is not a `site.unavailable`, which only serving records (`S16`). sites writes nothing to stderr for any answer in this group.

## A model publishes a site

The ordinary case, and a site's first publish: `scratch` tracks `preview`, so a publish with no `ref` resolves `preview` and puts its commit at the site's URL.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish","arguments":{"name":"scratch"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_2d6f8a0c4e1b3957","name":"scratch","slug":"scratch-7c1e9a4f","url":"https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/","repo":"rep_8c21d4e0f7a3b915","ref":"preview","visibility":"public","listed":false,"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","created":"2026-10-02T08:00:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<published>` is the time of the call, RFC 3339 UTC to the second.

Preconditions:

- The preamble's: `scratch` is unpublished.

Postconditions:

- The catalog has `scratch` at commit `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`, published `<published>`; its ref is still `preview`. `show` (`S07`) answers what `publish` answered.
- `cache/sites/sit_2d6f8a0c4e1b3957/9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/` holds the commit's tree, file for file and byte for byte as `git archive` emitted it, and is the only directory under `cache/sites/sit_2d6f8a0c4e1b3957/`.
- `GET /scratch-7c1e9a4f/` is answered 200 from that tree, with `ETag: "9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170"` (`S11`).
- The repository is untouched; `blog` is as it was.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.published","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","ref":"preview","site":"sit_2d6f8a0c4e1b3957"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"publish"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `scratch` and the slug are in none of them.

## A model publishes a site from a ref it does not track

A model can put any branch, tag, or commit on the site for one publish, to show a preview, say, or to roll back to a tag, without changing what the site tracks. Here `blog`, which tracks `main`, is published from `preview`. With `"ref":"v1"` the commit is `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; a sha is `A model publishes a site from a commit sha`. The event's `ref` is the ref as the model gave it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"preview"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `ref` is still `main`.

Preconditions:

- The preamble's.

Postconditions:

- The catalog has `blog` at commit `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`, published `<published>`, ref `main`. The next `publish` of `blog` with no `ref` resolves `main` again.
- `cache/sites/sit_4e7a1c9b0d2f8635/` holds only `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/`: the tree of `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` was removed.
- telemetry has received, between the request's `request.started` and its `tool.called`:

  ```
  {"time":"<time>","service":"sites","event":"site.published","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","ref":"preview","site":"sit_4e7a1c9b0d2f8635"}}
  ```

## A model publishes a site from a commit sha

A model that knows exactly which commit it wants, one it just pushed or one to roll back to, names it by its sha. The sha is resolved like any ref, so it must be a commit in the site's repository; the site's tracked ref is unchanged, and the event's `ref` is the sha as the model gave it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `ref` is still `main`.

Preconditions:

- The preamble's: `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170` is the commit `preview` points at in `rep_8c21d4e0f7a3b915`.

Postconditions:

- The catalog has `blog` at commit `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`, published `<published>`, ref `main`. The next `publish` of `blog` with no `ref` resolves `main` again.
- `cache/sites/sit_4e7a1c9b0d2f8635/` holds only `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/`.
- telemetry has received, between the request's `request.started` and its `tool.called`:

  ```
  {"time":"<time>","service":"sites","event":"site.published","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","ref":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","site":"sit_4e7a1c9b0d2f8635"}}
  ```

## A model publishes a site while visitors are reading it

A publish takes as long as its unpack, and the site stays up throughout: requests that arrive before the new tree is whole are answered from the old commit, those after from the new, and no request sees a half-unpacked tree or a missing one. Only then is the old tree removed. Here a push has moved `main` on, and the model publishes `blog` to put it up.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, except that a push to repos has moved `main` of `rep_8c21d4e0f7a3b915` to `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`.
- While the publish is unpacking, a visitor sends `GET /blog/` (`S11`).

Postconditions:

- The visitor's request was answered 200 from the tree of `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, with `ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"`, and its `site.viewed` has `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.
- Every request for `blog` that arrives after the publish has answered is served from `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`, with that sha as its `ETag`; a browser revalidating with `If-None-Match: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"` gets the new page, not a 304.
- `cache/sites/sit_4e7a1c9b0d2f8635/` holds only `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/`.
- telemetry has received `site.published` with attributes `{"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","ref":"main","site":"sit_4e7a1c9b0d2f8635"}`.

## A model publishes the commit a site already has

Nothing has been pushed since the last publish, and the model publishes again. That is not a refusal: the site is published at the same sha, its `published` time moves to now, and the publish is recorded again, so the trail shows every publish a model made. The tree already in the cache serves throughout.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<published>` is the time of this call, later than `2026-10-01T10:00:00Z`.

Preconditions:

- The preamble's: `main` is still at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, the commit `blog` is published at.

Postconditions:

- The catalog has `blog` at the same commit, published `<published>`.
- `cache/sites/sit_4e7a1c9b0d2f8635/` holds only `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`, with the same files as before. Every request for `blog` during and after the call was answered 200 from it, with the same `ETag`.
- telemetry has received `site.published` again, with attributes `{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","ref":"main","site":"sit_4e7a1c9b0d2f8635"}`.

## A model publishes from a ref that names no commit

A branch that was never pushed, a tag that was deleted, a mistyped sha: whatever git cannot resolve to a commit in the site's repository is refused, quoting the ref as resolved. Without `ref` that is the site's own: had `preview` been deleted, a publish of `scratch` would be refused with `no commit for 'preview'`. A `ref` git would not accept as a name at all, `..bad` say, names no commit either and is refused the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"nope"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no commit for 'nope'
```

Preconditions:

- The preamble's: `rep_8c21d4e0f7a3b915` has no branch, tag, or commit `nope`.

Postconditions:

- Nothing has changed: `blog` is at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, published `2026-10-01T10:00:00Z`, and served from its tree as before. Nothing was added under `cache/sites/sit_4e7a1c9b0d2f8635/`.
- sites recorded no `site.published`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"publish"}}
  ```

## A model publishes a site whose repository is gone

The site names a repository that is no longer under `REPOS_DIR`: repos has deleted it, or repos is not installed. The site is not deleted with it; the publish is refused naming the repository, and the site goes on being served from its cached tree while that lasts (`S16`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
repository 'rep_8c21d4e0f7a3b915' is unavailable
```

Preconditions:

- The preamble's, except that `../repos/state/repos/rep_8c21d4e0f7a3b915.git` does not exist.

Postconditions:

- Nothing has changed: `blog` is at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and its tree under `cache/sites/sit_4e7a1c9b0d2f8635/` is still there and still served.
- sites recorded no `site.published`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model publishes a site too large to serve

A tree whose files add up to more than `SITE_MAX_BYTES` is refused, naming the limit, so one site cannot fill the host's disk. A tree of exactly the limit is published; how the size is counted is `S17`'s. The unpack stops as soon as the limit is passed, and what it had written is removed.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"preview"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
site exceeds 268435456 bytes
```

Preconditions:

- The preamble's, except that the files of the tree at `preview` add up to 268435457 bytes.

Postconditions:

- Nothing has changed: `blog` is at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and served from its tree. There is no `cache/sites/sit_4e7a1c9b0d2f8635/9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/`.
- sites recorded no `site.published`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model publishes a site and git takes too long

Every git run sites makes is bounded by `OPERATION_SECONDS`, so a repository on failing storage cannot hold a tool call open for ever. A git still running when the time is up is killed and the publish is refused, naming the bound. The same answer comes whichever git run it was, the resolve or the unpack.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"preview"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
git took longer than 600 seconds
```

Preconditions:

- The preamble's, except that `git archive` of `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170` is still running 600 seconds after it started, the repository's storage stalling, say.

Postconditions:

- The git was killed; no git sites started for the call is still running.
- Nothing has changed: `blog` is at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and served from its tree. There is no `cache/sites/sit_4e7a1c9b0d2f8635/9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/`.
- sites recorded no `site.published`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model publishes a site and git fails

When git itself fails, a damaged object in the repository, say, sites cannot say more than git did, so it passes git's own words on: the line `git failed`, one empty line, then each line git wrote to stderr, prefixed `> `, separated by LF with no LF after the last.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"preview"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
git failed

> error: inflate: data stream error (incorrect header check)
> fatal: unable to read tree 7e1f0a3c5b9d2e4f6a8c0b1d3e5f7a9c2b4d6e8f
```

Preconditions:

- The preamble's, except that an object of the commit at `preview` is damaged, so `git --git-dir=../repos/state/repos/rep_8c21d4e0f7a3b915.git archive --format=tar 9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170` exits 128 having written exactly these two lines to stderr:

  ```
  error: inflate: data stream error (incorrect header check)
  fatal: unable to read tree 7e1f0a3c5b9d2e4f6a8c0b1d3e5f7a9c2b4d6e8f
  ```

Postconditions:

- Nothing has changed: `blog` is at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and served from its tree. There is no `cache/sites/sit_4e7a1c9b0d2f8635/9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/`.
- sites wrote nothing to stderr: git's words went to the caller, not to sites' log. sites recorded no `site.published`; the request's `tool.called` has `kind` `additive` and `outcome` `error`, and the text of the refusal is in no event.

## A model publishes another user's site

Only a site's owner publishes it. Another user's site does not exist for the caller, and gets exactly the answer a site that does not exist gets; no git runs.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"publish","arguments":{"name":"recipes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'recipes'
```

Preconditions:

- The preamble's: `recipes` is `u_2b8e1d04`'s, published at `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`.

Postconditions:

- Nothing has changed: `recipes` is at the same commit with the same `published`, and its tree is as it was.
- sites recorded no `site.published`; the request's `tool.called` has `kind` `additive` and `outcome` `error`, as for a site that does not exist.

## A model publishes a site it does not have

A name among none of the caller's sites is refused, quoting it as sent, before any git runs.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"publish","arguments":{"name":"wiki"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'wiki'
```

Preconditions:

- The preamble's: no site is named `wiki`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.published`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"publish"}}
  ```

## A model calls publish without saying which site

`name` is required. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"publish","arguments":{"ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. sites recorded no `site.published`; the request's `tool.called` has `tool` `publish`, `kind` `additive`, `outcome` `invalid_arguments`, and `duration_us` 0.
