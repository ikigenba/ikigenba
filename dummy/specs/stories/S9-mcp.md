# Stories — mcp

The widgets offered to models: dummy's MCP interface at `/mcp`, the one route
that answers MCP clients rather than browsers. It is the exact path `/mcp`; a
path beneath it, `/mcp/tools` say, is a path that does not exist and is
answered as `S3` answers one. `/mcp` takes only POST, each POST carries one
JSON-RPC request, and each request is answered with one `application/json`
body: there are no sessions and no streams, and dummy keeps nothing from one
request to the next. It offers two tools and nothing else, in this order:
`list_widgets`, which lists the widgets, and `create_widget`, which creates
one under the same rules as the form (`S5`). There is no tool to change or
remove a widget, as there is no form for either. Both tools work on the same
widgets the panel shows: one in-memory set, reset every time the process
starts, holding at startup exactly three, in this order: `alpha` count 3
status `active`; `beta` count 0 status `paused`; `gamma` count 12 status
`retired`. A widget has a `name`, an integer `count`, and a `status` that is
one of `active`, `paused`, or `retired`, and the widgets are always in
creation order, so a widget created through either way in — the form or
`create_widget` — is last, and shows to the other at once.

The actor is a model working through an MCP client, or the client itself. On
a developer's laptop the client is played with `curl` against a dummy served
with `systemd-socket-activate -l 127.0.0.1:3000 dummy` (`S2`), with no
services file unless a story says otherwise. `/mcp` is behind the same
identity rule as every other route: the gate sets `X-User-Id` and
`X-User-Email`, a sibling forwards them (`S2`), and a request without
`X-User-Id` is answered 500 before anything else is looked at. The curl lines
below carry both headers by hand, and only the missing-header story carries
neither.

A client speaking the protocol revision `2026-07-28` sends, on every request,
the headers `Content-Type: application/json`, `MCP-Protocol-Version:
2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a
`tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry
`_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and
`io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on
that revision is status 200, and its `result`, besides what each story fixes,
carries `resultType` `"complete"` and `_meta` whose
`io.modelcontextprotocol/serverInfo` is
`{"name":"dummy","version":"v<semver>"}`, where `v<semver>` is the version
`dummy --version` prints (`S1`); a `tools/list` or `server/discover` result
also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not
repeated below. A client speaking an earlier revision, `2025-11-25` or
`2025-06-18`, opens with `initialize` and is served the same tools, and its
calls get the same tool results, `isError` refusals included, without those
members; a protocol error, such as an unknown tool, carries the same `code`
and `message` but is answered with status 200 on the earlier revisions. How
the transport answers a request that is not well-formed MCP — a wrong
`Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an
unknown method — is the platform's, the same for every app, and this group
does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`,
a JSON object, and as one text content block whose text is that same object
encoded compactly, with no white space between its tokens. A tool that
refuses its arguments answers status 200 with a result whose `isError` is
`true` and whose one text content block says why. That text is always the
line `invalid arguments:` followed by one line per offence, each
`<field>: <reason>`, separated by LF with no LF after the last, so a model
learns every offence from one answer and can fix them all in one retry.
Arguments are refused at two layers. First they are read against the tool's
input schema: a field missing, of the wrong JSON type, not a whole number
where one is wanted, outside the range a whole number can hold, a status
outside the three, or a field the tool does not have. Only arguments that
pass are then held to dummy's rules for a widget, the form's rules: the name,
trimmed of surrounding white space, must be 1 to 40 characters and not
already taken, letter case counting; the count must be zero or more. Each
rule offence is reported with the message the form shows beside that field
(`S5`), name first, then count. Nothing is created when any offence is found.

A response body below is laid out for reading: its white space is not fixed.
The order of members within a tool and within its schemas is fixed as shown;
elsewhere the order of members is not. A response block shows the status line
and the headers the story fixes; a header it does not show is not fixed.
dummy writes nothing to stderr for any answer in this group except the
missing-header 500.

## An MCP client lists dummy's tools

A client lists the tools before offering them to a model. What it gets is
everything the model is told about each tool: its name, a description written
for the model, the shape of its arguments and its answer, and how careful the
client must be before calling it. The description's first line is a one-line
summary a catalogue can show on its own. `list_widgets` changes nothing, so
it is marked read-only; `create_widget` adds a widget and never changes or
removes one, so it is marked neither read-only nor destructive. Neither
reaches outside the platform's own data, so neither is open-world.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no
`nextCursor` and whose `tools` is an array of exactly two tools, in this
order:

```
[
  {
    "name": "list_widgets",
    "description": "List the widgets, oldest first.",
    "inputSchema": {"type": "object", "additionalProperties": false},
    "outputSchema": <the list_widgets output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "create_widget",
    "description": "Create a widget and return it.\n\nThe name is trimmed of surrounding white space and must then be 1 to 40 characters and not already taken (letter case counts). The count is a whole number, zero or more. Every rule the arguments break is reported in one error, and nothing is created unless all of them hold.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The widget's name: 1 to 40 characters after trimming, unique."},
        "count": {"type": "integer", "description": "How many: a whole number, zero or more."},
        "status": {"type": "string", "enum": ["active", "paused", "retired"], "description": "The widget's status."}
      },
      "required": ["name", "count", "status"],
      "additionalProperties": false
    },
    "outputSchema": <the create_widget output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  }
]
```

The output schemas are not quoted whole. `create_widget`'s describes one
widget: an object closed to other members, with the properties `name`, a
string; `count`, an integer; and `status`, a string whose `enum` is `active`,
`paused`, `retired`; each with the same description as the input property of
the same name. `list_widgets`'s describes an object closed to other members
whose one property, `widgets`, is an array of such widgets. Which members
each output schema marks required, and whether `widgets` carries a
description, are not fixed here. The schemas carry no `$schema` member.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed.

## A model lists the widgets

`list_widgets` takes no arguments and answers with every widget, oldest
first, which is the order the panel's table shows them in (`S3`). A call may
leave `arguments` out or send `{}`; both are the same call.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: list_widgets' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has
no `isError` member, a `structuredContent` of

```
{"widgets":[{"name":"alpha","count":3,"status":"active"},{"name":"beta","count":0,"status":"paused"},{"name":"gamma","count":12,"status":"retired"}]}
```

and a `content` array of one text block, `{"type":"text","text":<text>}`,
whose text is exactly that line.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- Nothing has changed.

## A model creates a widget

The ordinary case, and the one `create_widget` exists for. The answer is the
widget as dummy stored it, so the model need not list the widgets to learn
what it made.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has
no `isError` member, a `structuredContent` of

```
{"name":"delta","count":7,"status":"active"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it, so no widget is
  named `delta`.

Postconditions:

- A widget named `delta`, count 7, status `active`, now exists.
- The set holds four widgets, `delta` last, after `gamma`. The panel's table
  and the table fragment show it as their last row from the next request on
  (`S3`, `S4`), and a fragment poll that carries the `ETag` from before the
  call is answered with the new table, not `304`.

## A model lists the widgets after creating one

A widget a model created is in the next listing, last, because the listing is
in creation order; a widget a user created with the form would be listed the
same way.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: list_widgets' -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has
no `isError` member, a `structuredContent` of

```
{"widgets":[{"name":"alpha","count":3,"status":"active"},{"name":"beta","count":0,"status":"paused"},{"name":"gamma","count":12,"status":"retired"},{"name":"delta","count":7,"status":"active"}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- Since the process started, the only change to the widgets is the
  `create_widget` call of `A model creates a widget`.

Postconditions:

- Nothing has changed.

## A user sees on the panel a widget a model created

The tools and the panel share one set of widgets, so what a model creates a
user sees without doing anything: the panel's script re-fetches the table
fragment every 5 seconds (`S4`), and the next fetch carries the new row.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets/table
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the table fragment of `S4` with four rows, in this
order: `alpha` 3 `active`, `beta` 0 `paused`, `gamma` 12 `retired`, and
`delta` 7 `active`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- Since the process started, the only change to the widgets is the
  `create_widget` call of `A model creates a widget`.

Postconditions:

- Nothing has changed.

## A model creates a widget with spaces around its name

The name is trimmed of surrounding white space before any rule is applied
and before it is stored, as the form trims it, so a model that pads a name
creates the widget it meant. Only the ends are trimmed; space inside a name
is kept. The status is not trimmed: it must be one of the three words
exactly.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"  big delta  ","count":0,"status":"paused"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has
no `isError` member, a `structuredContent` of

```
{"name":"big delta","count":0,"status":"paused"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- A widget named `big delta`, count 0, status `paused`, now exists, last.
  No widget's name begins or ends with a space.

## A model creates a widget with no name

A name is required, so an empty one is refused rather than stored as a blank
name. A name of nothing but white space is empty once trimmed and is refused
the same way.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"   ","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has
`isError` `true`, no `structuredContent`, and a `content` array of one text
block whose text is exactly:

```
invalid arguments:
name: a name is required
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with.

## A model creates a widget with a name longer than 40 characters

40 characters is the limit, counted after trimming, so 41 is refused. The
limit counts characters, not bytes: a name of 40 accented letters is
accepted. The name below is 41 characters.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"a-widget-name-that-is-far-too-long-to-fit","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
name: the name is too long; the limit is 40 characters
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model creates a widget with a name that is already taken

Names are unique across widgets, however the widget was created. The
comparison is made after trimming and is exact, letter case counting: ` alpha `
is taken, because it trims to `alpha`, while `Alpha` is a different name, free
to use, and creating it succeeds as in `A model creates a widget`.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"alpha","count":5,"status":"paused"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
name: that name is already taken
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it, so a widget
  named `alpha` exists.

Postconditions:

- No widget was created. The fixture set is unchanged; in particular `alpha`
  still has count 3 and status `active`. A taken name is refused, never
  merged into the widget that holds it.

## A model creates a widget with a negative count

Zero is allowed — `beta` has a count of 0 — and anything below it is not. A
negative count is a whole number, so it passes the schema, which states no
lower bound, and is refused by dummy's rule.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":-1,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
count: the count cannot be negative
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model breaks several of dummy's rules at once

Every rule is checked and every broken one reported, name before count, so a
model that got both wrong learns both from one answer.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"alpha","count":-4,"status":"retired"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
name: that name is already taken
count: the count cannot be negative
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model sends a status that is not one of the three

The schema lists the three statuses, so a value outside them is refused when
the arguments are read, before dummy's rules are applied, with the
platform's wording, which quotes the allowed values and the one received. The
value must match exactly: `Active` and ` active` are refused too.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"archived"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
status: must be one of "active", "paused", "retired", got "archived"
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model sends a count that is not a number

The count is a JSON number. A count sent as anything else — a string, even
one holding digits, here `"7"` — is refused when the arguments are read,
naming the JSON type that arrived. A widget is never created with a count the
model did not give as a number.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":"7","status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
count: expected integer, got string
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model sends a count that is not a whole number

The count is a whole number. A number with a fractional part is refused,
quoted exactly as it was sent. A number whose fractional part is zero is a
whole number however it is written, so `7.0` is the count 7 and is accepted.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7.5,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
count: expected integer, got 7.5
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model sends a count too large to hold

A count is a 64-bit signed whole number, so a whole number beyond that range
is refused when the arguments are read, with the bounds stated so the model
can correct it. The number is quoted exactly as it was sent.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":1e19,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
count: must be between -9223372036854775808 and 9223372036854775807, got 1e19
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model leaves out a field

All three fields are required. A field left out is refused when the
arguments are read; dummy never supplies a default status or count.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 15 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
status: missing required field
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model sends a field the tool does not have

A widget has three fields and no others, so an argument the tool does not
know is refused rather than ignored: a model that believes it set a colour
must learn that it did not.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"active","colour":"red"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 16 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
colour: unknown field
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model sends several malformed arguments at once

Every offence found while reading the arguments is reported in one answer:
the tool's fields first, in the order `name`, `count`, `status`, then each
unknown field in the order it was sent. `list_widgets` reads its arguments
the same way, so any argument sent to it is an unknown field.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"create_widget","arguments":{"colour":"red","count":"7","status":"archived"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 17 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
name: missing required field
count: expected integer, got string
status: must be one of "active", "paused", "retired", got "archived"
colour: unknown field
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model's arguments are malformed and break dummy's rules too

dummy's rules are applied only to arguments that were read cleanly, so when
both layers would object the answer names only what reading found. Here the
name is taken and the count negative, but the status is not one of the
three, and that is all the answer says. Once the model fixes the status, the
next answer names the rule offences.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"alpha","count":-4,"status":"archived"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 18 whose `result` has
`isError` `true` and a `content` array of one text block whose text is
exactly:

```
invalid arguments:
status: must be one of "active", "paused", "retired", got "archived"
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged.

## A model calls a tool dummy does not have

dummy has two tools. A call naming any other — a tool to remove a widget,
say, which dummy does not offer — is not a tool's refusal but a protocol
error: there is no tool to answer it.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: delete_widget' -d '{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"delete_widget","arguments":{"name":"alpha"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 19 and no `result`,
whose `error` has `code` `-32602` and `message` `Unknown tool: delete_widget`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- Nothing has changed. `alpha` still exists.
- dummy wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

`/mcp` follows the rule every route follows: the gate sets `X-User-Id` on
every request it forwards and a sibling forwards the one it received, so a
request without it says the gate or a sibling is misconfigured, a server
fault answered 500. The identity check runs before anything about MCP is
looked at, so the answer is the same plain text every route gives, not a
JSON-RPC response, and no tool runs, whatever the body asked for.

Request:

```
$ curl -si -X POST -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: create_widget' -d '{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending
with LF.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.
- The widgets are the fixture set as the process started it.

Postconditions:

- Nothing has changed. No widget named `delta` exists.
- dummy wrote one line to stderr, `dummy: request -: X-User-Id is missing`,
  as it does on every route (`S3`).

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client
hoping for a stream, is refused with an empty body: dummy offers no stream and
no page at this address. This is not the panel's 405 page (`S3`): the answer
carries no banner and no HTML.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: POST
```

Status 405. The body is empty.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr.

## A client asks dummy what it is for

A client tells its model what each server is for through the server's
instructions. dummy's instructions are its own description, as the host's
services file gives it, so they are written once, in dummy's manifest
(`S1`), and never anywhere else. dummy reads the file afresh for every
request that asks, as it does for the launcher (`S3`), so a rewrite of the
file shows in the next answer without a restart. A developer stands in for
the host by writing a services file and naming it when serving dummy. The
file here, `/tmp/services.json`, lists dummy:

```
{
  "services": [
    {"name": "dummy", "url": "https://dummy.sbx.ikigenba.dev/", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><rect x='4' y='4' width='16' height='16'/></svg>"}
  ]
}
```

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: server/discover' -d '{"jsonrpc":"2.0","id":21,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 21 whose `result` has
exactly these members besides the `resultType`, `_meta`, `ttlMs`, and
`cacheScope` every such result carries:

```
{
  "supportedVersions": ["2026-07-28", "2025-11-25", "2025-06-18"],
  "capabilities": {"tools": {}},
  "instructions": "Demo widgets to list and create"
}
```

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with
  `systemd-socket-activate -E IKIGENBA_SERVICES=/tmp/services.json -l 127.0.0.1:3000 dummy`.
- `/tmp/services.json` holds the file above and is readable by dummy.

Postconditions:

- Nothing has changed. The services file is as it was.
- dummy wrote nothing to stderr.

## A client asks dummy what it is for on a laptop with no services file

With no services file there is no description to give, so the answer has no
instructions at all rather than empty ones, and is otherwise the same. The
answer is the same when the file named is missing, cannot be read, or has no
entry named `dummy`. The tools are offered all the same.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: server/discover' -d '{"jsonrpc":"2.0","id":22,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 22 whose `result` has
`supportedVersions` `["2026-07-28","2025-11-25","2025-06-18"]`,
`capabilities` `{"tools":{}}`, the members every such result carries, and no
`instructions` member.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with
  `systemd-socket-activate -l 127.0.0.1:3000 dummy`, so `IKIGENBA_SERVICES` is
  unset.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr.

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which
open with `initialize` and send no `_meta`. dummy serves them: `initialize`
echoes the revision the client asked for when it is one of those two, and
answers `2025-11-25` otherwise. The instructions are the same as
`server/discover` gives. dummy keeps no session, so the client's following
`notifications/initialized` is answered `202` with an empty body, and the
client may list or call tools with or without having sent `initialize`.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example-client","version":"1.0.0"}}}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has
exactly these members:

```
{
  "protocolVersion": "2025-11-25",
  "capabilities": {"tools": {}},
  "serverInfo": {"name": "dummy", "version": "v<semver>"},
  "instructions": "Demo widgets to list and create"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- dummy is serving on `127.0.0.1:3000`, started with
  `systemd-socket-activate -E IKIGENBA_SERVICES=/tmp/services.json -l 127.0.0.1:3000 dummy`.
- `/tmp/services.json` holds the file of `A client asks dummy what it is for`.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same two tools, with the same
names, descriptions, schemas, and annotations, and its calls get the same
tool results, `isError` refusals included, as in the stories above. Only the
envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or
`cacheScope`, and a protocol error, such as an unknown tool, carries the same
`code` and `message` as on `2026-07-28` but is answered with status 200, not
400.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2025-11-25' -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' http://127.0.0.1:3000/mcp
```

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2025-06-18' -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' http://127.0.0.1:3000/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has
exactly one member, `tools`, the array of `An MCP client lists dummy's
tools`, member for member.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed.

## A client on a space lists dummy's tools

On a space, dummy's MCP interface is at `https://dummy.<space>/mcp`, through
the space's nginx, which authenticates the request — with a bearer token or
the space's session cookie, as auth decides — and passes the caller to dummy
in `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as for every page
(`S7`). The client sends no identity headers of its own; whatever it sent
would be replaced. Both forms below behave identically.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://dummy.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X POST -b 'ikigenba_session=<session>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://dummy.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is the answer of `An MCP client lists dummy's tools`:
the same two tools, in the same order, member for member, with `v<semver>`
the version the deployed binary's `dummy --version` prints.

Preconditions:

- dummy `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `S7`.
- `<token>` is a bearer token, or `<session>` a session, that the space's gate
  accepts.

Postconditions:

- Nothing has changed.

## A client on a space without a credential is refused before dummy

The space's gate answers an unauthenticated request to `/mcp` itself, with a
challenge an MCP client understands, rather than redirecting to a sign-in page
as it does for a page. That answer is the gate's, the same for every app's
`/mcp`; dummy never sees the request.

Request:

```
$ curl -si -X POST -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://dummy.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 401
www-authenticate: Bearer realm="ikigenba"
```

Status 401. The body is one line of plain text from the gate; this story
does not fix it.

Preconditions:

- dummy `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `S7`.
- The request carries no credential.

Postconditions:

- Nothing has changed. The request never reached dummy, and dummy wrote
  nothing to stderr.

## The mcp gateway calls dummy over its socket

The platform's MCP gateway offers the tools of every service whose entry in
the services file is marked for MCP, and dummy's is. The gateway calls dummy
directly on its socket, not through nginx, on behalf of the caller it is
serving: it speaks `2026-07-28` and forwards that caller's `X-User-Id`,
`X-User-Email`, and `X-Request-Id`, as any sibling does (`S2`). dummy answers
the gateway exactly as it answers a client through nginx; it cannot tell the
two apart and does not try. Here a developer on the host, as the `ikigenba`
user, stands in for the gateway; what the gateway does with the answer is the
gateway's, told in its own stories.

Request:

```
$ curl -si --unix-socket /run/ikigenba/dummy.sock -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: list_widgets' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://dummy/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no
`isError` member and whose `structuredContent` is an object whose one member,
`widgets`, is an array of every widget dummy holds, oldest first, each with
its `name`, `count`, and `status`, with one text content block holding the
same object encoded compactly, as in `A model lists the widgets`.

Preconditions:

- dummy `v<semver>` is deployed and active on the host, serving on
  `/run/ikigenba/dummy.sock`.
- The host's services file lists dummy with `"mcp": true` and the socket
  `/run/ikigenba/dummy.sock`.
- The caller runs as the `ikigenba` user, which can reach the socket.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr.
