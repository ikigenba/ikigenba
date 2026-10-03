# Stories — create

`create`, the tool that makes a repository: an empty bare repository with the name the caller chose, owned by the caller, ready to push to. Its one argument is `name`, required, a string. A name is 1 to 64 characters, each a lowercase ASCII letter, a digit, or `-`, the first a letter or digit; it is taken as given, never trimmed or folded, so a name can never look like an id, which carries `_`. Names are unique per owner, not across the host: two users may each have a `notes`, and neither sees the other's. The result is what `show` answers for the new repository (`S07`), members in this order: `id`, `rep_` and 16 lowercase hexadecimal digits, minted now and never changed; `name`; `default_branch`, always `main`; `head`, absent, since a new repository has no commits; `size_bytes`, the size of its directory on disk in bytes; `available`, `true`; `created`, the time it was made, RFC 3339 UTC to the second; `clone_url`; and `credentials`. `clone_url` is `<repos-url>/<name>.git`, where `<repos-url>` is as `S03` defines it: the `url`, less one trailing `/`, of the services file's entry named `repos`, whatever `Host` the request carried; when the file has no such entry, or its `url` is empty, it is `<scheme>://<Host>`, with the request's `Host` as it arrived, port kept, and `<scheme>` `http` or `https` when `X-Forwarded-Proto` is exactly that, else `https`. It never carries a credential. `credentials` is the guidance `S03` fixes for giving git the caller's token without writing it to disk, scoped to the clone URL's scheme and space. A repository's directory is `state/repos/<id>.git` under repos' working directory (`/opt/repos/state/repos/<id>.git` on a host), named by its id, never its name.

The actor is a model working through an MCP client, or a sibling service creating a repository on a user's behalf; the request shape, the result envelope, the `invalid arguments:` wording, `tool.called`, and the trail are as `S05` fixes them. Each request is the HTTP request a running repos (`S02`) receives on `/mcp`, carrying `X-User-Id` and `X-User-Email` by hand, on revision `2026-07-28`. Unless a story says otherwise, repos runs with the suite's services file (`S03`), whose `repos` entry has the `url` `https://repos.sbx.ikigenba.dev`, and telemetry takes every event; and the catalog holds exactly three repositories, which the groups `S07` to `S10` share: the caller `u_7f3a9c21` (`mg@example.com`) owns `notes`, id `rep_3f9a0c1d2e4b5a69`, created `2026-09-30T10:15:00Z`, whose `refs/heads/main` is at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`, and `site`, id `rep_8c21d4e0f7a3b915`, created `2026-10-01T09:00:00Z`, which has no commits; another user, `u_2b8e1d04` (`ann@example.com`), owns `journal`, id `rep_d41c7a9e05b28f63`, created `2026-10-01T16:30:00Z`. All three are available and no git operation or maintenance is running on any of them. `create` is of kind `additive`; a call that makes a repository records `repo.created`, with `repo`, the new id, and `owner`, the caller's user id, before its `tool.called`; the name is in no event. A refusal creates nothing: no catalog entry, no directory. repos writes nothing to stderr for any answer in this group.

## A model creates a repository

The ordinary case. The answer holds everything the model needs to push: the clone URL, and how to give git the token. Nothing has been pushed, so the result has no `head`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"name":"drafts"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"drafts","default_branch":"main","size_bytes":<n>,"available":true,"created":"<created>","clone_url":"https://repos.sbx.ikigenba.dev/drafts.git","credentials":"<credentials>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the id repos minted, `rep_` and 16 lowercase hexadecimal digits, different from every other repository's; `<created>` is the time of the call, RFC 3339 UTC to the second, `2026-10-02T15:04:05Z` say; `<n>` is the size of the new directory, more than 0. `<credentials>` is the string whose lines, separated by LF, are exactly:

```
Git authenticates with your personal access token as the password; the username is ignored. Keep the token in the environment variable IKIGENBA_TOKEN and give it to git with this credential helper, which reads the variable whenever git asks:

git config --global credential.https://*.sbx.ikigenba.dev.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'

Or set GIT_ASKPASS to a program that prints $IKIGENBA_TOKEN. Never put the token in a remote's URL or on a command line, and never use credential.helper store: each writes it to disk in plain text.
```

Preconditions:

- The preamble's: the caller owns `notes` and `site`, and no repository named `drafts`.

Postconditions:

- The catalog holds a fourth repository: id `<id>`, name `drafts`, owner `u_7f3a9c21`, created `<created>`, available. `list` (`S07`) answers it first, before `notes` and `site`, by name; `show` with `drafts` or `<id>` answers what `create` answered, `size_bytes` as it is at that moment.
- `state/repos/<id>.git` exists and is a bare repository with no refs: `git --git-dir=state/repos/<id>.git for-each-ref` prints nothing, and `git --git-dir=state/repos/<id>.git symbolic-ref HEAD` prints `refs/heads/main`.
- Its own configuration, read with `git config --file state/repos/<id>.git/config --get <key>`, holds `pack.windowMemory` `64m`, `pack.threads` `1`, `core.bigFileThreshold` `16m`, `ikigenba.id` `<id>`, `ikigenba.name` `drafts`, `ikigenba.owner` `u_7f3a9c21`, and `ikigenba.created` `<created>`, so the catalog entry can be rebuilt from the directory alone (`S14`).
- No directory under `state/repos/` is named `drafts`; nothing else under `state/repos/` changed.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"repo.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"owner":"u_7f3a9c21","repo":"<id>"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `drafts` is in none of them.

## A model creates a repository on a host whose services file names no repos

Without a `repos` entry to name its public address, repos builds the clone URL from the request itself, as the request arrived: the `Host` with its port, and the scheme `X-Forwarded-Proto` names. The credential helper's scope follows the clone URL: its host with a single leading `repos.` label dropped, port kept, so the helper answers for every service of the same space and no other host. A `Host` with no `repos.` label is the space whole. Without `X-Forwarded-Proto`, or with any value but exactly `http` or `https`, the scheme is `https`. A `repos` entry whose `url` is empty is the same as no entry.

Request:

```
POST /mcp HTTP/1.1
Host: repos.wip.localhost:7400
X-Forwarded-Proto: http
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create","arguments":{"name":"drafts"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member and a `structuredContent` that is the one of `A model creates a repository` but for two members: `clone_url` is `http://repos.wip.localhost:7400/drafts.git`, and `credentials` is the same text with its command reading:

```
git config --global credential.http://*.wip.localhost:7400.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'
```

The `content` array is one text block whose text is that object encoded compactly. With the same `Host` and no `X-Forwarded-Proto`, `clone_url` is `https://repos.wip.localhost:7400/drafts.git` and the scope `https://*.wip.localhost:7400`.

Preconditions:

- The preamble's, except that repos runs with a services file holding only the telemetry service's entry: no entry is named `repos`.

Postconditions:

- `drafts` exists as in `A model creates a repository`, its directory and configuration included. The clone URL is not stored: `show` builds it again from the services file and the request it answers.

## A model creates a repository with a name another user already has

Names are unique per owner, not across the host. `journal` is `u_2b8e1d04`'s, and the caller may make a `journal` of its own: a different repository with its own id and directory. Neither owner learns of the other's from any tool or from git; the same holds the other way round, when `u_2b8e1d04` creates a `notes`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create","arguments":{"name":"journal"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"journal","default_branch":"main","size_bytes":<n>,"available":true,"created":"<created>","clone_url":"https://repos.sbx.ikigenba.dev/journal.git","credentials":"<credentials>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is a new id, not `rep_d41c7a9e05b28f63`; `<credentials>` is the text of `A model creates a repository`.

Preconditions:

- The preamble's: `u_2b8e1d04` owns `journal`, and the caller owns no repository of that name.

Postconditions:

- The catalog holds two repositories named `journal`: `u_2b8e1d04`'s, unchanged, and the caller's, `<id>`, in its own directory `state/repos/<id>.git`. `rep_d41c7a9e05b28f63`'s directory and configuration are untouched.
- The caller's `list` (`S07`) shows `journal`, `notes`, `site`; `u_2b8e1d04`'s still shows only its own `journal`. The clone URL `https://repos.sbx.ikigenba.dev/journal.git` reaches the caller's `journal` for the caller and `u_2b8e1d04`'s for `u_2b8e1d04` (`S11`).
- telemetry has received `repo.created` with `repo` `<id>` and `owner` `u_7f3a9c21`, as in `A model creates a repository`.

## A model creates a repository with a name that is not allowed

A name is held to one rule, and every way of breaking it gets the same line: upper-case letters, as here; a space, `_`, `.`, or any character outside `a`-`z`, `0`-`9`, and `-`; a first character `-`; the empty name; and more than 64 characters. Nothing is trimmed, so ` notes` is refused rather than read as `notes`, and nothing is folded, so `Notes` is refused rather than read as `notes`. A name ending `.git` is refused by the same rule: the `.git` of the clone URL is repos', never part of a name.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"name":"My Drafts"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit
```

Preconditions:

- The preamble's.

Postconditions:

- No repository was created: the catalog holds the preamble's three, and no directory was added under `state/repos/`.
- repos recorded no `repo.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create"}}
  ```

## A model creates a repository with a name it already has

A caller cannot have two repositories of one name: the clone URL would not know which to reach. The answer quotes the name so the model knows which it collided with; a taken name is refused, never read as a request for the repository that holds it, and the existing one is untouched.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create","arguments":{"name":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: 'notes' is already one of your repositories
```

Preconditions:

- The preamble's: the caller owns `notes`.

Postconditions:

- No repository was created. `notes` is unchanged: same id, `refs/heads/main` still at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`.
- repos recorded no `repo.created`; its `tool.called` has `kind` `additive` and `outcome` `error`.

## A model sends arguments create does not take

`name` is required and is the only argument. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer: the tool's field first, then each unknown field in the order it was sent (`S05`). Here the model named the repository under the wrong key.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create","arguments":{"repo":"drafts"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: missing required field
repo: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- No repository was created.
- repos recorded no `repo.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"create"}}
  ```
