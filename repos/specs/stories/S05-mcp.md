# Stories — mcp endpoint

The repositories offered to models: repos' MCP interface at `/mcp`, the one route that answers MCP clients rather than browsers or git. It is the exact path `/mcp`; a path beneath it, `/mcp/tools` say, is a path that does not exist and is answered as `S03` answers one. `/mcp` takes only POST, each POST carries one JSON-RPC request, and each request is answered with one `application/json` body: there are no sessions and no streams, and repos keeps nothing from one request to the next. It offers six tools and nothing else, in this order: `list`, which lists the caller's repositories (`S07`); `show`, which shows one of them with its clone URL and the credentials text (`S07`); `status`, which shows how busy git is on the host and how close each of the caller's repositories is to its size limit (`S10`); `create`, which creates an empty repository (`S06`); `rename`, which gives one a new name (`S08`); and `delete`, which removes one (`S09`). Content never passes through a tool: a model reads and writes what is in a repository with git, at the clone URL (`S11`). Each tool's arguments, its result, and its failures are told in its own group; what they share is fixed here.

A repository has an id, `rep_` and 16 lowercase hexadecimal digits, which repos gives it when it is created and which never changes; a name, 1 to 64 characters, each a lowercase ASCII letter, a digit, or `-`, the first a letter or a digit, which can change (`S08`); and an owner, the `X-User-Id` of the caller who created it. Every tool works for its caller alone: a repository is visible to and usable by its owner and no one else, and to every other caller a repository it does not own is indistinguishable from one that does not exist. Names are unique per owner, so two users may each have a `notes`. A tool that acts on one repository takes it as `repo`, a string that is the repository's id when it begins `rep_` and its name otherwise; either is looked up among the caller's own repositories only.

On a host, a model does not reach `/mcp` directly: it reaches repos' tools through the mcp gateway, naming the service `repos` and the tool, and the gateway calls repos at its socket on the model's behalf (below, and `S17`). repos' manifest marks it an MCP service (`S01`), so the host's services file carries `"mcp": true` for it and the gateway's `describe` lists its six tools with their kinds: `list`, `show`, and `status` are of kind `read` and run with the gateway's `call`; `create` is `additive` and `rename` and `delete` are `destructive`, and those three run with `mutate`. The actor in this group is a model working through an MCP client, the client itself, or the gateway standing in for either. Each request is shown as the HTTP request repos receives on a running repos (`S02`), started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, the host's services file, which holds the suite's services file (`S03`) unless a story says otherwise, and whose `telemetry` entry names the telemetry service, which takes every event (`S02`). `/mcp` is behind the same identity rule as every route: the gate sets `X-User-Id` and `X-User-Email`, the gateway forwards them (`S02`), and a request without `X-User-Id` is answered 500 before anything else is looked at. The requests below carry both headers by hand, and only the missing-header story carries neither.

A client speaking the protocol revision `2026-07-28` sends, on every request, the headers `Content-Type: application/json`, `MCP-Protocol-Version: 2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a `tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry `_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and `io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on that revision is status 200, and its `result`, besides what each story fixes, carries `resultType` `"complete"` and `_meta` whose `io.modelcontextprotocol/serverInfo` is `{"name":"repos","version":"<display>"}`, where `<display>` is the string `repos --version` prints under the environment repos was started with (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; a `tools/list` or `server/discover` result also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated in this group or in `S06` to `S10`. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`, opens with `initialize` and is served the same tools, and its calls get the same tool results, `isError` results included, without those members; a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200 on the earlier revisions. How the transport answers a request that is not well-formed MCP — a wrong `Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the platform's, the same for every app, and this group does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that same object encoded compactly, with no white space between its tokens. A tool that cannot do what it was asked answers status 200 with a result whose `isError` is `true`, with no `structuredContent` and a `content` array of exactly one text block saying why. The text takes one of three forms:

- Arguments refused as they are read against the tool's input schema — a field missing, of the wrong JSON type, or one the tool does not have — are reported with the platform's wording: the line `invalid arguments:` followed by one line per offence, each `<field>: <reason>` (`repo: missing required field`, `name: expected string, got number`, `bogus: unknown field`), separated by LF with no LF after the last; the tool's fields come first, in the order of its input schema, then each unknown field in the order it was sent.
- Arguments that pass that reading but break repos' rules are reported in the same form, `invalid arguments:` and then one `<field>: <reason>` line per offence, every rule checked and every offence reported, in the order of the tool's input schema, so a model learns every offence from one answer. The reasons are repos' own and are these three, each told in the group of the tool that meets it: `name: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit`; `name: '<name>' is already one of your repositories`; and `repo: no repository '<repo>'`, for a `repo` that names no repository of the caller's — one that does not exist and one another user owns alike. `<name>` and `<repo>` are quoted exactly as they were sent. Rules are held only to arguments that were read cleanly, so when both layers would object the answer names only what reading found.
- A call whose arguments are sound but which repos cannot carry out as things stand is refused with one line in words its group fixes, with no `invalid arguments:` line: `delete` refuses a repository while git is working on it (`S09`), and every tool, when repos cannot read or write its catalog or its repositories on disk, refuses with exactly `cannot reach the repositories; try again later`, quoting nothing of the underlying error.

Nothing is created, renamed, or deleted by a call that is refused.

Every request to `/mcp` is recorded in repos' trail as every request is, by its `request.started` and `request.finished` (`S02`, `S03`). A `tools/call` that reaches one of the six tools and is answered with a `result`, an `isError` result included, also records `tool.called`, after anything the tool recorded and before the request's `request.finished`, under the caller's request id and user. Its attributes are `tool`, the tool's name; `kind`, `read` for `list`, `show`, and `status`, `additive` for `create`, and `destructive` for `rename` and `delete`; `outcome`, which says how the call was answered: `ok` for a result with no `isError`, `invalid_arguments` when the arguments were refused as they were read against the input schema, and `error` for every other refusal — a rule broken, a repository that is busy, a store that cannot be reached; and `duration_us`, how long the tool took, in whole microseconds, 0 when the arguments were refused as they were read and the tool never ran. The arguments themselves, the text of a refusal, and the name of any repository are never recorded. A call answered with a protocol error, such as an unknown tool, reached no tool and records no `tool.called`; nor does any other method. A call that creates, renames, or deletes a repository also records `repo.created`, `repo.renamed`, or `repo.deleted`, with the repository's id and its owner, as its group tells; the three read tools record nothing of their own.

A response body below is laid out for reading: its white space is not fixed. The order of members within a tool and within its schemas, and within a tool's result where a story shows one, is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed. No answer in this group or in `S06` to `S10` earns a line on stderr, the missing-header 500 included; only an event repos cannot deliver reaches stderr (`S02`).

## An MCP client lists repos' tools

A client lists the tools before offering them to a model. What it gets is everything the model is told about each tool: its name, a description written for the model, the shape of its arguments and its answer, and how careful the client must be before calling it. The description's first line is a one-line summary a catalogue can show on its own, the same line the landing page shows beside the tool's name (`S03`). The descriptions of `show` and `create`, the two tools whose result carries a clone URL, tell the model that the result's `credentials` says how to give git the token without putting it in the URL or on a command line, and not to use the store helper, so a model learns it at the moment it is about to clone. `list`, `show`, and `status` change nothing, so they are marked read-only; `create` adds a repository and never changes or removes one, so it is marked neither read-only nor destructive; `rename` breaks the remote URL of every existing clone, and `delete` removes a repository and its history for good, so both are marked destructive. None reaches outside the platform's own data, so none is open-world.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `nextCursor` and whose `tools` is an array of exactly six tools, in this order:

```
[
  {
    "name": "list",
    "description": "The repositories you own, by name.\n\nTakes no arguments. Each repository has its id, its name, size_bytes, its size on disk, head, the sha its main branch points at (absent before the first push), and available, false when repos found it damaged at startup and will not serve it. Use show for one repository's clone URL.",
    "inputSchema": {"type": "object", "additionalProperties": false},
    "outputSchema": <the list output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "show",
    "description": "One of your repositories, with its clone URL and how to give git your token.\n\nPass repo, the repository's id or its name. The result has its id, name, default_branch (always main), head (absent before the first push), size_bytes, available, created, clone_url, and credentials. The clone URL never holds a credential: credentials tells how to give git your personal access token without putting it in the URL or on a command line. Do not use credential.helper store, which writes the token to disk.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "repo": {"type": "string", "description": "The repository's id (rep_ and 16 hexadecimal digits) or its name."}
      },
      "required": ["repo"],
      "additionalProperties": false
    },
    "outputSchema": <the show output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "status",
    "description": "How busy repos is, and how close each of your repositories is to its size limit.\n\nTakes no arguments. read covers clones and fetches, write covers pushes and maintenance: slots is how many run at once, active how many are running, and queued how many wait for a slot or their repository's lock. repos lists each of your repositories with size_bytes, limit_bytes, the size at which pushes to it are refused, available, and busy, true while a git operation or maintenance runs on it; delete is refused while busy.",
    "inputSchema": {"type": "object", "additionalProperties": false},
    "outputSchema": <the status output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "create",
    "description": "Create an empty repository and return it, with its clone URL and how to give git your token.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, and must not already be one of your repositories. The repository starts with no commits; its default branch is main. Push to clone_url to fill it. The result is what show returns: credentials tells how to give git your personal access token without putting it in the URL or on a command line. Do not use credential.helper store, which writes the token to disk.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The new repository's name: 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, not already one of your repositories."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the show output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "rename",
    "description": "Give one of your repositories a new name; its id does not change.\n\nPass repo, its id or current name, and name, the new name, under the rules of create. The clone URL follows the name, so a clone made under the old name must have its remote's URL updated before it can fetch or push again. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "repo": {"type": "string", "description": "The repository's id (rep_ and 16 hexadecimal digits) or its name."},
        "name": {"type": "string", "description": "The repository's new name, under the rules of create."}
      },
      "required": ["repo", "name"],
      "additionalProperties": false
    },
    "outputSchema": <the show output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  },
  {
    "name": "delete",
    "description": "Delete one of your repositories and everything in it.\n\nPass repo, its id or name. The repository and its history are gone for good; this cannot be undone. Refused while a git operation or maintenance runs on it; check busy with status and try again once it is false. The result is the id and name of the deleted repository.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "repo": {"type": "string", "description": "The repository's id (rep_ and 16 hexadecimal digits) or its name."}
      },
      "required": ["repo"],
      "additionalProperties": false
    },
    "outputSchema": <the delete output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  }
]
```

The output schemas are not quoted whole; each describes an object closed to other members, with its properties in the order given here. The show output schema, which `show`, `create`, and `rename` all carry, describes one repository: `id`, a string; `name`, a string; `default_branch`, a string; `head`, a string, the one member not always present; `size_bytes`, an integer; `available`, a boolean; `created`, a string; `clone_url`, a string; and `credentials`, a string. `list`'s has one property, `repos`, an array of objects, each closed to other members, with `id`, a string; `name`, a string; `size_bytes`, an integer; `head`, a string, not always present; and `available`, a boolean. `status`'s has `read` and `write`, each an object closed to other members with `slots`, `active`, and `queued`, integers; and `repos`, an array of objects, each closed to other members, with `id`, a string; `name`, a string; `size_bytes`, an integer; `limit_bytes`, an integer; `available`, a boolean; and `busy`, a boolean. `delete`'s has `id`, a string, and `name`, a string. Which members each output schema marks required, and which carry a description, are not fixed here. The schemas carry no `$schema` member.

Preconditions:

- repos is serving, and telemetry takes every event.

Postconditions:

- Nothing has changed. No repository and no catalog entry was read.
- repos wrote nothing to stderr. telemetry has received the request's two events, under user `u_7f3a9c21` and the id repos gave the request, and no `tool.called`, since no tool ran:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `request_bytes` is the length of the request's body and `response_bytes` the length of the response's.

## A client asks repos what it is for

A client tells its model what each server is for through the server's instructions. repos' instructions are its own description, as the host's services file gives it, so they are written once, in repos' manifest (`S01`), and never anywhere else. repos reads the file afresh for every request that asks, as it does for the launcher (`S03`), so a rewrite of the file shows in the next answer without a restart.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly these members besides the `resultType`, `_meta`, `ttlMs`, and `cacheScope` every such result carries:

```
{
  "supportedVersions": ["2026-07-28", "2025-11-25", "2025-06-18"],
  "capabilities": {"tools": {}},
  "instructions": "Git repositories for the suite's content"
}
```

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file, whose entry named `repos` has the description `Git repositories for the suite's content`.

Postconditions:

- Nothing has changed. The services file is as it was.
- repos wrote nothing to stderr.

## A client asks repos what it is for on a host with no services file

With no services file there is no description to give, so the answer has no instructions at all rather than empty ones, and is otherwise the same. The answer is the same when the file named is missing, cannot be read, or has no entry named `repos`. The tools are offered all the same.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":3,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `supportedVersions` `["2026-07-28","2025-11-25","2025-06-18"]`, `capabilities` `{"tools":{}}`, the members every such result carries, and no `instructions` member.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr about the missing instructions. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as a `repos: undelivered event: <event>` line, and nothing else for this request.

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which open with `initialize` and send no `_meta`. repos serves them as every app does: `initialize` echoes the revision the client asked for when it is one of those two, and answers `2025-11-25` otherwise. The instructions are the ones `server/discover` gives. repos keeps no session, so the client's following `notifications/initialized` is answered `202` with an empty body, and the client may list or call tools with or without having sent `initialize`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example-client","version":"1.0.0"}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has exactly these members:

```
{
  "protocolVersion": "2025-11-25",
  "capabilities": {"tools": {}},
  "serverInfo": {"name": "repos", "version": "<display>"},
  "instructions": "Git repositories for the suite's content"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- repos is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same six tools, with the same names, descriptions, schemas, and annotations, and its calls get the same tool results, `isError` results included, as on `2026-07-28`. Only the envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or `cacheScope`, and a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200, not 400.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-11-25

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-06-18

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly one member, `tools`, the array of `An MCP client lists repos' tools`, member for member.

Preconditions:

- repos is serving.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client hoping for a stream, is refused with an empty body: repos offers no stream and no page at this address, and its landing page is at `/` (`S03`). A DELETE, from a client ending a session it believes it has, is refused the same way, since there are no sessions, and so is any other method that is not POST.

Request:

```
GET /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: POST
```

Status 405. The body is empty.

Preconditions:

- repos is serving.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

`/mcp` follows the rule every route follows: the gate sets `X-User-Id` on every request it forwards and the gateway forwards the one it received, so a request without it says the gate or the gateway is misconfigured, a server fault answered 500. The identity check runs before anything about MCP is looked at, so the answer is the same plain text every route gives, not a JSON-RPC response, and no tool runs, whatever the body asked for. Without an owner there is no one to create a repository for.

Request:

```
POST /mcp HTTP/1.1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"name":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending with LF.

Preconditions:

- repos is serving, and telemetry takes every event.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. No repository was created, and nothing was added under `state/repos/`.
- repos wrote nothing to stderr about the 500. telemetry has received the request's two events, with an empty user, under the id repos gave the request (`S02`); no tool ran, so there is no `tool.called` and no `repo.created`:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A model calls a tool repos does not have

repos has six tools. A call naming any other — a tool to write a file into a repository, say, which repos does not offer, since content goes through git — is not a tool's refusal but a protocol error: there is no tool to answer it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: write_file

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"write_file","arguments":{"repo":"notes","path":"README.md","content":"hello"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 5 and no `result`, whose `error` has `code` `-32602` and `message` `Unknown tool: write_file`.

Preconditions:

- repos is serving, and telemetry takes every event.
- The caller owns a repository named `notes`, `rep_3f9a0c1d2e4b5a69`.

Postconditions:

- Nothing has changed. `notes` is as it was, and no git ran.
- repos wrote nothing to stderr. No tool ran, so repos recorded no `tool.called`: telemetry has received only the request's `request.started` and its `request.finished`, whose `status` is 400, under user `u_7f3a9c21`.

## A model calls a tool while repos cannot reach its catalog

Every tool answers from the catalog, so a tool that cannot read it has nothing true to say. It does not answer as if the caller had no repositories, which would tell the model its work was gone; it refuses, in the same words whichever of the six tools was called, and quotes nothing of the database's own error. The model can tell this from a refusal of its arguments and may try again later. repos keeps serving, and git requests are `S11`'s to answer.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cannot reach the repositories; try again later
```

A `show`, `status`, `create`, `rename`, or `delete` call whose arguments the tool would otherwise act on is refused with the same text, and a `create`, `rename`, or `delete` so refused has changed nothing, on disk or in the catalog. A call the tool refuses whatever the catalog holds — arguments refused as they are read against the input schema, a name that breaks the naming rule — is refused as its own group says.

Preconditions:

- repos is serving, and telemetry takes every event.
- repos' database cannot be read: `state/repos.db` has become unreadable since repos opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr. telemetry has received the request's three events, under user `u_7f3a9c21` and request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"list"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- repos is still serving.

## The mcp gateway calls repos over its socket

The platform's MCP gateway offers the tools of every service whose entry in the services file is marked for MCP, and repos' is. A model asks the gateway to `call` the service `repos` and a read tool, or to `mutate` with a write tool, and the gateway calls repos directly on its socket, not through nginx, on behalf of the caller it is serving: it speaks `2026-07-28` and forwards that caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as any sibling does (`S02`). repos answers the gateway exactly as it answers a client through nginx; it cannot tell the two apart and does not try, and the caller the gateway forwards is the owner every tool works for. Here a developer on the host, as the `ikigenba` user, stands in for the gateway's `call` of `list`; what the gateway does with the answer is the gateway's, told in its own stories.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":<n>,"head":"<sha>","available":true},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<n>,"available":true}]}
```

and a `content` array of one text block whose text is exactly that line, as `S07` tells. `<sha>` is the 40 lowercase hexadecimal digits `refs/heads/main` of `notes` points at, and each `<n>` is that repository's size on disk in bytes. `ann@example.com`'s repository is not in it.

Preconditions:

- repos is deployed and active on the host, serving on `/run/ikigenba/repos.sock`.
- The host's services file lists repos with `"mcp": true` and the socket `/run/ikigenba/repos.sock`, and lists the telemetry service, which takes every event.
- The caller runs as the `ikigenba` user, which can reach the socket.
- `u_7f3a9c21` owns two repositories: `notes`, `rep_3f9a0c1d2e4b5a69`, with commits on `main`, and `site`, `rep_8c21d4e0f7a3b915`, with none. `u_2b8e1d04` owns a repository named `journal`, `rep_d41c7a9e05b28f63`.

Postconditions:

- Nothing has changed.
- repos wrote nothing to stderr.
- telemetry has received repos' three events for the call under the id the developer sent and the user it sent, as the gateway would forward them, so a trace of that id shows the gateway's forward and repos' execution together:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  Neither repository's name is in them.
