# Stories — mcp endpoint

The sites offered to models: sites' MCP interface at `/mcp`, the one route that answers MCP clients rather than browsers. It is the exact path `/mcp`; a path beneath it, `/mcp/tools` say, is a site path whose slug is `mcp`, which no site can have (`S06`), and is answered as `S11` answers a site that does not exist. `/mcp` takes only POST, each POST carries one JSON-RPC request, and each request is answered with one `application/json` body: there are no sessions and no streams, and sites keeps nothing from one request to the next. It offers seven tools and nothing else, in this order: `list`, which lists the caller's sites (`S07`); `show`, which shows one of them with its URL, its repository, and what is published (`S07`); `create`, which creates a site from one of the caller's repositories (`S06`); `publish`, which publishes a site at a commit of its repository (`S08`); `update`, which changes a site's visibility, its listing, or the ref it tracks (`S09`); `delete`, which removes one (`S10`); and `apex`, which shows, sets, or clears the site the space's apex host redirects to (`S13`). A site's files never pass through a tool: they are a commit of a repository repos holds, pushed with git (repos' `S11-git.md`), and a tool only names which commit a site serves. Each tool's arguments, its result, and its failures are told in its own group; what they share is fixed here.

A site has an id, `sit_` and 16 lowercase hexadecimal digits, which sites gives it when it is created and which never changes; a name, 1 to 64 characters, each a lowercase ASCII letter, a digit, or `-`, the first a letter or a digit, unique across the space; a slug, the segment of its URL; and an owner, the `X-User-Id` of the caller who created it. A tool that acts on one site takes it as `name` and looks it up among the caller's own sites only: to every caller, a site it does not own is indistinguishable from one that does not exist (`no site named '<name>'`), though the name is still taken (`S06`). A site object, as `show`, `create`, `publish`, and `update` return it, has these members in this order: `id`, `name`, `slug`, `url`, `repo`, `ref`, `visibility`, `listed`, `commit`, `created`, `published`, where `url` is `<sites-url>/<slug>/` (`S03`) and `commit` and `published` are absent before the site's first publish.

On a host, a model does not reach `/mcp` directly: it reaches sites' tools through the mcp gateway, naming the service `sites` and the tool, and the gateway calls sites at its socket on the model's behalf (below, and `S21`). sites' manifest marks it an MCP service (`S01`), so the host's services file carries `"mcp": true` for it and the gateway's `describe` lists its seven tools with their kinds: `list` and `show` are of kind `read` and run with the gateway's `call`; `create`, `publish`, `update`, and `apex` are `additive`, and `delete` is `destructive`, and those five run with `mutate`. The actor in this group is a model working through an MCP client, the client itself, or the gateway standing in for either. Each request is shown as the HTTP request sites receives on a running sites (`S02`), started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, the host's services file, which holds the suite's services file (`S03`) unless a story says otherwise, and whose `telemetry` entry names the telemetry service, which takes every event (`S02`). sites serves guests its sites, its shared files, and the sign-in redirect of its pages, but not `/mcp`: nginx keeps its strict check on `/mcp` for an app that serves guests (opsctl's `S5-nginx.md`), so a request reaches `/mcp` only with the caller nginx authenticated in `X-User-Id` and `X-User-Email`, which the gateway forwards (`S02`), and a request without `X-User-Id` is answered 500 before anything else is looked at. The requests below carry both headers by hand, and only the missing-header story carries neither. The gateway names no service in `Host`: it sends `Host: backend` to every service, and a `Host` with no `.` is a sibling calling over the socket, never a request at the apex host (`S13`). The requests below carry `Host: sites.sbx.ikigenba.dev`, as nginx passes it, but for the gateway's.

A client speaking the protocol revision `2026-07-28` sends, on every request, the headers `Content-Type: application/json`, `MCP-Protocol-Version: 2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a `tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry `_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and `io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on that revision is status 200, and its `result`, besides what each story fixes, carries `resultType` `"complete"` and `_meta` whose `io.modelcontextprotocol/serverInfo` is `{"name":"sites","version":"<display>"}`, where `<display>` is the string `sites --version` prints under the environment sites was started with (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; a `tools/list` or `server/discover` result also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated in this group or in `S06` to `S10` and `S13`. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`, opens with `initialize` and is served the same tools, and its calls get the same tool results, `isError` results included, without those members; a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200 on the earlier revisions. How the transport answers a request that is not well-formed MCP — a wrong `Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the platform's, the same for every app, and this group does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that same object encoded compactly, with no white space between its tokens. A tool that cannot do what it was asked answers status 200 with a result whose `isError` is `true`, with no `structuredContent` and a `content` array of exactly one text block saying why. The text takes one of three forms:

- Arguments refused as they are read against the tool's input schema — a field missing, of the wrong JSON type, or one the tool does not have — are reported with the platform's wording: the line `invalid arguments:` followed by one line per offence, each `<field>: <reason>` (`name: missing required field`, `name: expected string, got number`, `bogus: unknown field`), separated by LF with no LF after the last; the tool's fields come first, in the order of its input schema, then each unknown field in the order it was sent.
- Arguments that pass that reading but that sites refuses, or a call it cannot carry out as things stand, are refused with one line in sites' own words, each told in the group of the tool that meets it: `invalid name '<name>'`, `a site named '<name>' already exists`, `visibility must be public or private`, `invalid ref '<ref>'`, `no repository '<repo>'`, `no site named '<name>'`, `no commit for '<ref>'`, `repository '<repo>' is unavailable`, `site exceeds <SITE_MAX_BYTES> bytes`, `git took longer than <OPERATION_SECONDS> seconds`, `update needs at least one of visibility, listed, ref`, `apex site must be public`, and `apex takes name or clear, not both`. `<name>`, `<ref>`, and `<repo>` are quoted exactly as they were sent. The one refusal of more than one line is `git failed`, followed by one empty line and then each line git wrote to its stderr, prefixed `> ` (`S08`).
- Every tool, when sites cannot read or write its catalog, refuses with exactly `cannot reach the catalog; try again later`, quoting nothing of the underlying error.

Nothing is created, published, changed, or deleted by a call that is refused, and a refused call records no `site.*` event.

Every request to `/mcp` is recorded in sites' trail as every request is, by its `request.started` and `request.finished` (`S02`, `S03`). A `tools/call` that reaches one of the seven tools and is answered with a `result`, an `isError` result included, also records `tool.called`, after anything the tool recorded and before the request's `request.finished`, under the caller's request id and user. Its attributes are `tool`, the tool's name; `kind`, `read` for `list` and `show`, `additive` for `create`, `publish`, `update`, and `apex`, and `destructive` for `delete`; `outcome`, `ok` for a result with no `isError`, `invalid_arguments` when the arguments were refused as they were read against the input schema, and `error` for every other refusal; and `duration_us`, how long the tool took, in whole microseconds, 0 when the arguments were refused as they were read and the tool never ran. The arguments themselves, the text of a refusal, and the name or slug of any site are never recorded. A call answered with a protocol error, such as an unknown tool, reached no tool and records no `tool.called`; nor does any other method. A call that creates, publishes, changes, or deletes a site, or sets or clears the apex, also records `site.created`, `site.published`, `site.updated`, `site.deleted`, or `site.apex` before its `tool.called`, as its group tells; `list`, `show`, and an `apex` call that only reads record nothing of their own.

A response body below is laid out for reading: its white space is not fixed. The order of members within a tool and within its schemas, and within a tool's result where a story shows one, is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed. No answer in this group or in `S06` to `S10` and `S13` earns a line on stderr, the missing-header 500 included; only an event sites cannot deliver reaches stderr (`S02`).

## An MCP client lists sites' tools

A client lists the tools before offering them to a model. What it gets is everything the model is told about each tool: its name, a description written for the model, the shape of its arguments and its answer, and how careful the client must be before calling it. The description's first line is a one-line summary a catalogue can show on its own, the same line the landing page shows beside the tool's name (`S03`). `list` and `show` change nothing, so they are marked read-only; `create`, `publish`, `update`, and `apex` add a site, a publish, or a setting, and what they change can be changed back by calling them again, so they are marked neither read-only nor destructive; `delete` removes a site and frees its name for anyone to take, so it is marked destructive. None reaches outside the platform's own data, so none is open-world. `visibility` is a string in every schema, not an enumeration, so a value other than `public` or `private` reaches the tool and is refused in sites' own words (`S06`).

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
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

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `nextCursor` and whose `tools` is an array of exactly seven tools, in this order:

```
[
  {
    "name": "list",
    "description": "The sites you own, by name.\n\nTakes no arguments. Each site has its id, name, slug, url (the address it answers at), visibility (public or private), listed, and commit (the sha it is published at, absent before the first publish). Use show for one site's repository and ref.",
    "inputSchema": {"type": "object", "additionalProperties": false},
    "outputSchema": <the list output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "show",
    "description": "One of your sites, with its URL, its repository, and what is published.\n\nPass name, the site's name. The result has its id, name, slug, url, repo (the id of the repos repository it is served from), ref (the ref it tracks), visibility, listed, commit (the sha it is published at), created, and published (when it was last published); commit and published are absent before the first publish.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The site's name."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the site output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "create",
    "description": "Create a site from one of your repositories and return it; publish it to make it live.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, is none of about, mcp, or api, and must not already name a site in the space: names are shared by every user, because a listed site answers at its name. repo is the id of one of your repositories in repos. ref is the branch, tag, or commit the site tracks, main unless given. visibility is public, served to anyone, or private, served only to users signed in to the space; public unless given. listed is true unless given: a listed site answers at its name and is on the landing page; an unlisted one answers at its name followed by '-' and 8 random hexadecimal digits, and is on the landing page only for you. The site serves nothing until you publish it. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The new site's name: 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, not about, mcp, or api, and not already a site's name in the space."},
        "repo": {"type": "string", "description": "The id of one of your repositories in repos (rep_ and 16 hexadecimal digits)."},
        "ref": {"type": "string", "description": "The branch, tag, or commit the site tracks; main unless given."},
        "visibility": {"type": "string", "description": "public or private; public unless given."},
        "listed": {"type": "boolean", "description": "Whether the site is on the landing page and answers at its name; true unless given."}
      },
      "required": ["name", "repo"],
      "additionalProperties": false
    },
    "outputSchema": <the site output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "publish",
    "description": "Publish one of your sites at a commit of its repository: the ref it tracks, or a ref or commit you name.\n\nPass name, and ref to publish a branch, tag, or commit sha other than the one the site tracks; a ref given here is used for this publish only and does not change the site's ref. The site serves the files of that commit exactly as git holds them, from the moment publish returns, and what it served before until then. Push to the repository, then publish again, to change what a site serves. The result is what show returns, with the new commit.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The site's name."},
        "ref": {"type": "string", "description": "The branch, tag, or commit to publish, for this publish only; the site's own ref unless given."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the site output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "update",
    "description": "Change one of your sites' visibility, whether it is listed, or the ref it tracks.\n\nPass name and at least one of visibility, listed, and ref, under the rules of create. Changing ref does not publish: call publish to serve the new ref. Changing listed never changes the slug, so a site keeps answering at the address it was created with. The apex site must stay public. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The site's name."},
        "visibility": {"type": "string", "description": "public or private."},
        "listed": {"type": "boolean", "description": "Whether the site is on the landing page."},
        "ref": {"type": "string", "description": "The branch, tag, or commit the site tracks from now on."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the site output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "delete",
    "description": "Delete one of your sites; its repository is untouched.\n\nPass name. The site stops answering at once, its name is free for anyone to take, and if it was the apex site the apex is cleared. The repository and its history stay in repos. The result is the id of the deleted site.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The site's name."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the delete output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  },
  {
    "name": "apex",
    "description": "Show, set or clear the site the space's apex domain redirects to.\n\nWith no arguments, the result is the apex site, or null when there is none. Pass name to make one of your public sites the apex, or clear true to clear it, whoever set it; not both. The result is the apex after the call.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The name of one of your public sites, to make the apex."},
        "clear": {"type": "boolean", "description": "true to clear the apex."}
      },
      "additionalProperties": false
    },
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  }
]
```

The output schemas are not quoted whole; each describes an object closed to other members, with its properties in the order given here. The site output schema, which `show`, `create`, `publish`, and `update` all carry, describes one site: `id`, a string; `name`, a string; `slug`, a string; `url`, a string; `repo`, a string; `ref`, a string; `visibility`, a string; `listed`, a boolean; `commit`, a string, not always present; `created`, a string; and `published`, a string, not always present. `list`'s has one property, `sites`, an array of objects, each closed to other members, with `id`, a string; `name`, a string; `slug`, a string; `url`, a string; `visibility`, a string; `listed`, a boolean; and `commit`, a string, not always present. `delete`'s has `deleted`, a boolean, and `id`, a string. `apex` has no output schema, because its result may be `{"apex": null}`. Which members each output schema marks required, and which carry a description, are not fixed here. The schemas carry no `$schema` member.

Preconditions:

- sites is serving, and telemetry takes every event.

Postconditions:

- Nothing has changed. No catalog entry and no repository was read, and no git ran.
- sites wrote nothing to stderr. telemetry has received the request's two events, under user `u_7f3a9c21` and the id sites gave the request, and no `tool.called`, since no tool ran:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `request_bytes` is the length of the request's body and `response_bytes` the length of the response's.

## A client asks sites what it is for

A client tells its model what each server is for through the server's instructions. sites' instructions are its own description, as the host's services file gives it, so they are written once, in sites' manifest (`S01`), and never anywhere else. sites reads the file afresh for every request that asks, as it does for the launcher (`S03`), so a rewrite of the file shows in the next answer without a restart.

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
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
  "instructions": "Static sites from the suite's repositories"
}
```

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file, whose entry named `sites` has the description `Static sites from the suite's repositories`.

Postconditions:

- Nothing has changed. The services file is as it was.
- sites wrote nothing to stderr.

## A client asks sites what it is for on a host with no services file

With no services file there is no description to give, so the answer has no instructions at all rather than empty ones, and is otherwise the same. The answer is the same when the file named is missing, cannot be read, or has no entry named `sites`. The tools are offered all the same.

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
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

- sites is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- sites wrote nothing to stderr about the missing instructions. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as a `sites: undelivered event: <event>` line, and nothing else for this request.

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which open with `initialize` and send no `_meta`. sites serves them as every app does: `initialize` echoes the revision the client asked for when it is one of those two, and answers `2025-11-25` otherwise. The instructions are the ones `server/discover` gives. sites keeps no session, so the client's following `notifications/initialized` is answered `202` with an empty body, and the client may list or call tools with or without having sent `initialize`.

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
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
  "serverInfo": {"name": "sites", "version": "<display>"},
  "instructions": "Static sites from the suite's repositories"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- sites is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- sites wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same seven tools, with the same names, descriptions, schemas, and annotations, and its calls get the same tool results, `isError` results included, as on `2026-07-28`. Only the envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or `cacheScope`, and a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200, not 400.

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-11-25

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
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

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly one member, `tools`, the array of `An MCP client lists sites' tools`, member for member.

Preconditions:

- sites is serving.

Postconditions:

- Nothing has changed.
- sites wrote nothing to stderr.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client hoping for a stream, is refused with an empty body: sites offers no stream and no page at this address, and its landing page is at `/` (`S03`). A DELETE, from a client ending a session it believes it has, is refused the same way, since there are no sessions, and so is any other method that is not POST. No visitor cookie is set: `/mcp` is not a site.

Request:

```
GET /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
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

- sites is serving.

Postconditions:

- Nothing has changed.
- sites wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

Every other route of sites serves guests, but `/mcp` does not: every tool works for its caller, and a site belongs to the user who created it. nginx never lets a request without a credential through to `/mcp`, and the gateway forwards the caller it received, so a request without `X-User-Id` here says nginx or the gateway is misconfigured, a server fault answered 500. The identity check runs before anything about MCP is looked at, so the answer is plain text, not a JSON-RPC response, and no tool runs, whatever the body asked for. An `X-User-Id` header whose value is empty is answered the same way.

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"name":"portfolio","repo":"rep_8c21d4e0f7a3b915"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending with LF. A `GET /mcp` with no identity is answered the same way, not with the 405 of `A browser opens /mcp`.

Preconditions:

- sites is serving, and telemetry takes every event.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. No site was created, no repository was read, and no git ran.
- sites wrote nothing to stderr about the 500. telemetry has received the request's two events, with an empty user, under the id sites gave the request (`S02`); no tool ran, so there is no `tool.called` and no `site.created`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A model calls a tool sites does not have

sites has seven tools. A call naming any other — a tool to upload a file into a site, say, which sites does not offer, since a site's files come from a commit pushed to its repository — is not a tool's refusal but a protocol error: there is no tool to answer it.

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: upload

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"upload","arguments":{"name":"blog","path":"index.html","content":"hello"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 5 and no `result`, whose `error` has `code` `-32602` and `message` `Unknown tool: upload`.

Preconditions:

- sites is serving, and telemetry takes every event.
- The caller owns the site `blog`, `sit_4e7a1c9b0d2f8635`, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Postconditions:

- Nothing has changed. `blog` is as it was, its tree under `cache/sites/sit_4e7a1c9b0d2f8635/` is untouched, and no git ran.
- sites wrote nothing to stderr. No tool ran, so sites recorded no `tool.called`: telemetry has received only the request's `request.started` and its `request.finished`, whose `status` is 400, under user `u_7f3a9c21`.

## A model calls a tool while sites cannot reach its catalog

Every tool answers from the catalog, so a tool that cannot read it has nothing true to say. It does not answer as if the caller had no sites, which would tell the model its work was gone; it refuses, in the same words whichever of the seven tools was called, and quotes nothing of the database's own error. The model can tell this from a refusal of its arguments and may try again later. sites keeps serving; a visitor who opens a site meanwhile is answered as `S11` tells, with the same line as plain text.

Request:

```
POST /mcp HTTP/1.1
Host: sites.sbx.ikigenba.dev
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
cannot reach the catalog; try again later
```

A `show`, `create`, `publish`, `update`, `delete`, or `apex` call whose arguments the tool would otherwise act on is refused with the same text, and a `create`, `publish`, `update`, `delete`, or `apex` so refused has changed nothing, in the catalog or under `cache/`. A call the tool refuses whatever the catalog holds — arguments refused as they are read against the input schema, a name that breaks the naming rule, a visibility that is neither `public` nor `private` — is refused as its own group says.

Preconditions:

- sites is serving, and telemetry takes every event.
- sites' database cannot be read: `state/sites.db` has become unreadable since sites opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed.
- sites wrote nothing to stderr. telemetry has received the request's three events, under user `u_7f3a9c21` and request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"list"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- sites is still serving.

## The mcp gateway calls sites over its socket

The platform's MCP gateway offers the tools of every service whose entry in the services file is marked for MCP, and sites' is. A model asks the gateway to `call` the service `sites` and a read tool, or to `mutate` with any other, and the gateway calls sites directly on its socket, not through nginx, on behalf of the caller it is serving: it speaks `2026-07-28`, names no service in `Host`, sending `Host: backend` as it does to every service, and forwards that caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as any sibling does (`S02`). A `Host` with no `.` is a sibling calling over the socket, never a request at the apex host (`S13`), so sites answers the gateway, which follows no redirect, exactly as it answers a client through nginx; it cannot tell the two apart and does not try, and the caller the gateway forwards is the owner every tool works for. Here a developer on the host, as the `ikigenba` user, stands in for the gateway's `call` of `list`; what the gateway does with the answer is the gateway's, told in its own stories.

Request:

```
POST /mcp HTTP/1.1
Host: backend
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
{"sites":[{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"},{"id":"sit_9a3c5e7b1d0f2468","name":"handbook","slug":"handbook","url":"https://sites.sbx.ikigenba.dev/handbook/","visibility":"private","listed":true,"commit":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d"},{"id":"sit_2d6f8a0c4e1b3957","name":"scratch","slug":"scratch-7c1e9a4f","url":"https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/","visibility":"public","listed":false}]}
```

and a `content` array of one text block whose text is exactly that line, as `S07` tells. Each `url` is under `https://sites.sbx.ikigenba.dev`, the services file's `url` for sites, not under the `Host` the gateway sent. `scratch` has no `commit`, since it has never been published. `ann@example.com`'s site `recipes` is not in it.

Preconditions:

- sites is deployed and active on the host, serving on `/run/ikigenba/sites.sock`.
- The host's services file lists sites with `"mcp": true`, the `url` `https://sites.sbx.ikigenba.dev`, and the socket `/run/ikigenba/sites.sock`, and lists the telemetry service, which takes every event.
- The caller runs as the `ikigenba` user, which can reach the socket.
- The catalog holds `S06`'s shared catalog: `u_7f3a9c21` owns `blog`, `handbook`, and `scratch`, and `u_2b8e1d04` owns `recipes`. The catalog's apex is unset.

Postconditions:

- Nothing has changed. No repository was read and no git ran.
- sites wrote nothing to stderr.
- telemetry has received sites' three events for the call under the id the developer sent and the user it sent, as the gateway would forward them, so a trace of that id shows the gateway's forward and sites' execution together:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No site's name or slug is in them.
