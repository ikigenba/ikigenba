# Stories — disk

What sites reads and writes on disk. sites is one of the consumers repos' `S15-disk.md` serves: it reads repos' repositories straight from their directories with the host's own `git`, running as the same `ikigenba` user, and never reaches repos over HTTP or through anything else of repos', so repos receives no request on sites' account and records no event for it. repos keeps one bare repository per repository at `<REPOS_DIR>/<rep id>.git`, named by the repository's id (`rep_` and 16 lowercase hexadecimal digits), never its name, whose `HEAD` names `refs/heads/main` and whose own git config holds `ikigenba.id`, `ikigenba.name`, `ikigenba.owner`, and `ikigenba.created` (repos' `S15-disk.md`). `REPOS_DIR` is a setting read once, at start, from sites' environment; unset or empty it is the manifest's `../repos/state/repos` (`S01`), and a relative value is resolved against sites' working directory, so on a host, where sites runs in `/opt/sites` and repos in `/opt/repos`, it is `/opt/sites/../repos/state/repos`, which is `/opt/repos/state/repos`, and in a sandbox `<data>/apps/sites/../repos/state/repos` (`S22`). sites does not check it at start, since repos may be installed later (`S02`). sites reads a repository three ways and no other: resolving a ref to a commit, reading `ikigenba.owner`, and unpacking a commit's tree with `git archive`; it never writes under `REPOS_DIR` — no ref, no object, no config, no lock left behind — so a repository's directory is byte for byte what it was before sites read it. A site names its repository by id, so a site keeps working across a rename in repos, and a repository deleted in repos is gone for sites too. What sites writes is under its own working directory: its catalog `state/sites.db`, which opsctl backs up, and `cache/sites/<site id>/<sha>/`, the files of a published commit exactly as `git archive` emits them — the same paths, the same bytes, symbolic links as links — which nothing backs up: `cache/` may be emptied at any time, and a tree missing from it is rebuilt on demand (`S16`). A site's directory holds the tree of its published commit and no other once a publish has finished (`S08`); deleting a site removes its directory (`S10`). The actor is a model working through an MCP client, an operator on the host, or a visitor; sites runs on the host in `/opt/sites` with `REPOS_DIR` unset unless a story sets it, and with the suite's services file (`S05`); telemetry takes every event; and the catalog holds `S06`'s shared catalog, among them the caller `u_7f3a9c21`'s `blog`, `sit_4e7a1c9b0d2f8635`, public and listed, over `rep_8c21d4e0f7a3b915` at `main`, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `2026-10-01T10:00:00Z`, its tree in the cache. `/opt/repos/state/repos/` holds `rep_8c21d4e0f7a3b915.git` (repos' `site`) and `rep_3f9a0c1d2e4b5a69.git` (repos' `notes`), both with `ikigenba.owner` `u_7f3a9c21`, and `rep_d41c7a9e05b28f63.git` (repos' `journal`), with `ikigenba.owner` `u_2b8e1d04`.

## A model creates a site over a repository in repos' directory

With `REPOS_DIR` unset, sites finds the caller's repository where repos keeps it on the same host, by its id, and takes it as the caller's because its own config says so. It asks repos nothing.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_3f9a0c1d2e4b5a69"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"docs","slug":"docs","url":"https://sites.sbx.ikigenba.dev/docs/","repo":"rep_3f9a0c1d2e4b5a69","ref":"main","visibility":"public","listed":true,"created":"<created>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `REPOS_DIR` is unset, and `/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git` is a bare repository whose `ikigenba.owner` is `u_7f3a9c21`. No site is named `docs`.

Postconditions:

- The catalog holds `docs`, unpublished, over `rep_3f9a0c1d2e4b5a69` (`S06`). Nothing was unpacked: `cache/sites/<id>/` holds no tree.
- `/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git` is as it was. repos received no request.
- The request recorded `site.created` with `repo` `rep_3f9a0c1d2e4b5a69` and `site` `<id>` (`S15`).

## An operator points sites at another repositories directory

`REPOS_DIR` says where repos' repositories are, for a host that keeps them somewhere other than beside sites, or a developer whose repositories are in a directory of their own. Once it is set, sites looks there and only there: the default place is not consulted.

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

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"<sha>","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<published>` is the time of the call.

Preconditions:

- The preamble's, except that the operator has set `REPOS_DIR=/srv/ikigenba/repos` in `/opt/sites/etc/env` and restarted sites.
- `/srv/ikigenba/repos/rep_8c21d4e0f7a3b915.git` is a bare repository whose `main` is at `<sha>`. `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` still exists, its `main` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Postconditions:

- `blog` is published at `<sha>`, unpacked from `/srv/ikigenba/repos/rep_8c21d4e0f7a3b915.git`; `cache/sites/sit_4e7a1c9b0d2f8635/` holds one tree, `<sha>/`.
- Neither repository changed.
- The request recorded `site.published` with `commit` `<sha>`, `ref` `main`, and `site` `sit_4e7a1c9b0d2f8635`.
- With `REPOS_DIR=../repos-b/state/repos` instead, sites would have read `/opt/repos-b/state/repos/rep_8c21d4e0f7a3b915.git`, the relative value resolved against `/opt/sites`.

## A model creates a site over a repository another user owns

Whose a repository is, sites learns from the repository's own config, `ikigenba.owner`, which repos wrote when it made it. `rep_d41c7a9e05b28f63` is `u_2b8e1d04`'s, so for the caller it is not there: the answer is the one for a repository that does not exist, and tells the caller nothing of another user's. The owner is checked only here, at create; a site's later publishes do not read it again.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_d41c7a9e05b28f63"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_d41c7a9e05b28f63'
```

Preconditions:

- The preamble's: `git config --file /opt/repos/state/repos/rep_d41c7a9e05b28f63.git/config ikigenba.owner` prints `u_2b8e1d04`. No site is named `docs`.

Postconditions:

- No site was created. The repository is as it was.
- The request recorded no `site.created`; its `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a site over a directory repos did not make

A bare repository under `REPOS_DIR` whose config holds no `ikigenba.owner` is no user's, so it is no one's to publish: something other than repos put it there. sites refuses it as it refuses a repository that does not exist.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_5e6f7a8b9c0d1e2f"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_5e6f7a8b9c0d1e2f'
```

Preconditions:

- The preamble's, and `/opt/repos/state/repos/rep_5e6f7a8b9c0d1e2f.git` is a bare repository with commits on `main` whose config holds no `ikigenba.` key. No site is named `docs`.

Postconditions:

- No site was created. The directory is as it was: sites wrote no `ikigenba.owner` into it.
- The request recorded no `site.created`; its `tool.called` has `outcome` `error`.

## A model publishes a site whose repository was renamed in repos

The owner renamed repos' `site` to `www` (repos' `S08-rename.md`). Nothing moved on disk: the directory is named by the id, which the site holds, so publishing goes on as before. The site's own name and slug are sites' and do not follow the repository's.

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

and a `content` array of one text block whose text is that object encoded compactly. `<published>` is the time of the call.

Preconditions:

- The preamble's; the owner then renamed `site` to `www` in repos, so `git config --file /opt/repos/state/repos/rep_8c21d4e0f7a3b915.git/config ikigenba.name` prints `www`. Its `main` is still at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Postconditions:

- `blog` is published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `<published>`, still over `rep_8c21d4e0f7a3b915`, and still served at `/blog/`.
- The request recorded `site.published` with `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, `ref` `main`, and `site` `sit_4e7a1c9b0d2f8635`; neither `site` nor `www` is in any event.

## A model publishes a site whose repository was deleted in repos

The owner deleted repos' `site` (repos' `S09-delete.md`), and its directory is gone. There is nothing to publish from, and the answer names the repository by its id so the model can tell its owner which one. The site is not changed: it goes on being served from the tree already in the cache.

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

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
repository 'rep_8c21d4e0f7a3b915' is unavailable
```

Preconditions:

- The preamble's, except that `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` does not exist.

Postconditions:

- `blog` is unchanged: published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `2026-10-01T10:00:00Z`, its tree in the cache as it was.
- sites created nothing under `/opt/repos/state/repos/`.
- The request recorded no `site.published`; its `tool.called` has `kind` `additive` and `outcome` `error`.

## A visitor reads a site whose repository was deleted in repos

A site is served from its tree in sites' cache, not from the repository, so deleting the repository in repos does not take the site down. It goes on being served at its published commit for as long as the tree stays in the cache; once the tree has to be rebuilt, the site is unavailable (`S16`).

Request:

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
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

Status 200. The body is exactly the bytes of `style.css` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's, except that `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` does not exist; `blog`'s tree is still in `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`. The request is a guest's.

Postconditions:

- Nothing has changed but the trail. git did not run.
- The request recorded its `site.viewed` with `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and `status` 200, and no `site.unavailable`.

## A model publishes a site and the repository is left as it was

Every read sites makes of a repository is a read. After a publish, the repository's directory holds the same files, with the same bytes and the same modification times, as before it: sites took no lock in it, wrote no ref or config, and left nothing behind. repos learned nothing of the publish.

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

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"preview"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<published>` is the time of the call.

Preconditions:

- The preamble's: the branch `preview` of `rep_8c21d4e0f7a3b915` is at `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`.
- Before the call, the operator recorded every path under `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` with its size, its modification time, and a checksum of its bytes.

Postconditions:

- `blog` is published at `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`, and `cache/sites/sit_4e7a1c9b0d2f8635/` holds one tree, `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170/`.
- Recorded again, every path under `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` has the size, modification time, and checksum it had; no path was added or removed. Its `preview` and `main` are where they were.
- repos received no request and recorded no event.

## An operator lists the files of a site in the cache

An operator looking at a site's tree on disk finds the files of the published commit, in a directory named by the site's id and the commit's sha, laid out exactly as `git archive` emits them: every file the commit holds, `.env` and `404.html` among them, whether or not sites will serve it (`S11`), with the same bytes, and a symbolic link as a link.

Command:

```
$ cd /opt/sites/cache/sites/sit_4e7a1c9b0d2f8635 && find . -mindepth 1 | LC_ALL=C sort
```

Output:

```
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/.env
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/404.html
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/about
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/about/index.html
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/data.blob
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/index.html
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/link.html
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/logo.png
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/notes.txt
./5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/style.css
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The preamble's: `blog` is published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and its tree is in the cache.
- The command runs on the host as the `ikigenba` user.

Postconditions:

- Nothing has changed.
- Each file's bytes are the bytes `git --git-dir=/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git archive --format=tar 5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` emits for it; `link.html` is a symbolic link whose target is `index.html`, as the commit holds it.

## An operator empties a site's cache while sites runs

The cache is sites' to rebuild, so an operator reclaiming disk may remove any of it, a site's directory or all of `cache/`, without stopping sites and without losing anything: the catalog still knows every site's published commit, and the next request for the site unpacks it again (`S16`).

Command:

```
$ rm -rf /opt/sites/cache/sites/sit_4e7a1c9b0d2f8635
```

Output: none.

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The preamble's: sites is running, and `blog`'s tree is in the cache. The command runs as the `ikigenba` user.

Postconditions:

- `cache/sites/sit_4e7a1c9b0d2f8635/` is gone. The catalog is unchanged: `show` with `{"name":"blog"}` answers `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and `published` `2026-10-01T10:00:00Z`.
- sites recorded no event and wrote nothing to stderr.
- The next `GET /blog/` is answered `200` from a tree sites unpacks for it, as in `S16`'s `A visitor opens a site whose tree is not in the cache`, and `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/` is back.
