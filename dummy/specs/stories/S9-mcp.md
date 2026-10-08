# Stories — mcp

The widgets offered to models: dummy's MCP interface at `/mcp`, the one
route that answers MCP clients rather than browsers. It is the exact path
`/mcp`; a path beneath it, `/mcp/tools` say, is a path that does not
exist and is answered as `S3` answers one. `/mcp` takes only POST, each
POST carries one JSON-RPC request, and each request is answered with one
`application/json` body: there are no sessions and no streams, and dummy
keeps no protocol state from one request to the next. It offers two tools
and nothing else, in this order: `list_widgets`, which lists the widgets,
and `create_widget`, which creates one under the same rules as the form
(`S5`). There is no tool to change or remove a widget, as there is no form
for either. Both tools work on the same widgets the panel shows. The widgets
are kept in dummy's database, `state/dummy.db` under its working directory,
and every request shares them. A database dummy creates holds no widgets;
every widget created since is kept across restarts and deploys, with the
id it was given. A widget has a `name`, an integer `count`, a `status`
that is one of `active`, `paused`, or `retired`, and an `id`, `wgt_` and
16 lowercase hexadecimal digits, which dummy gives it when the widget is
created and keeps with it, and which names the widget in dummy's trail
(`S3`); the tools show the id, so a model that knows a widget by its name
can find it in the trail, and no tool takes one. The widgets are always in
creation order, so a widget created through either way in — the form or
`create_widget` — is last, and shows to the other at once. Unless a story
says otherwise, the stories below start from a database holding exactly
three widgets, created in this order: `alpha` count 3 status `active`;
`beta` count 0 status `paused`; `gamma` count 12 status `retired`. They
are called the three widgets below.

The actor is a model working through an MCP client, or the client itself.
Each request is shown as the HTTP request the client sends to a running dummy
(`S2`), started, unless a story says otherwise, with a services file whose
one entry is the telemetry service's, which takes every event (`S2`), so dummy
has no description to give as instructions. `/mcp` is behind the same
identity rule as every other route: the gate sets `X-User-Id` and
`X-User-Email`, a sibling forwards them (`S2`), and a request without
`X-User-Id` is answered 500 before anything else is looked at. The requests
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
`{"name":"dummy","version":"<display>"}`, where `<display>` is the string
`dummy --version` prints under the environment dummy was started with (`S1`),
the empty string when that environment sets neither `IKIGENBA_COMMIT` nor
`IKIGENBA_RELEASE`; a `tools/list` or `server/discover` result also carries
`ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated
below. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`,
opens with `initialize` and is served the same tools, and its calls get the
same tool results, `isError` refusals included, without those members; a
protocol error, such as an unknown tool, carries the same `code` and `message`
but is answered with status 200 on the earlier revisions. How the transport
answers a request that is not well-formed MCP — a wrong `Content-Type`, a body
over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the
platform's, the same for every app, and this group does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`,
a JSON object, and as one text content block whose text is that same object
encoded compactly, with no white space between its tokens. A tool that
refuses its arguments answers status 200 with a result whose `isError` is
`true` and whose one text content block says why. That text is always the
line `invalid arguments:` followed by one line per offence, each
`<field>: <reason>`, separated by LF with no LF after the last, so a model
learns every offence from one answer and can fix them all in one retry. The
one other refusal is dummy's own: a tool that cannot reach the database
refuses with a line of its own, not a refusal of the arguments (`A model
calls a tool while dummy cannot reach the widgets`).
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
No answer in this group earns a line on stderr, the missing-header 500
included (`S2`, `S3`).

Every request to `/mcp` is recorded in dummy's trail as every request is,
by its `request.started` and `request.finished` (`S3`). A `tools/call` that
reaches one of the two tools and is answered with a `result`, a refusal
included, also records `tool.called`, after anything the tool recorded and
before the request's `request.finished`. Its attributes are `tool`, the
tool's name; `kind`, `read` for `list_widgets` and `additive` for
`create_widget`; `outcome`, which says how the call was answered: `ok` for a
result with no `isError`, `invalid_arguments` when the arguments were refused
as they were read against the input schema, and `error` for every other
refusal: arguments that passed that but broke dummy's rules for a widget, or
a tool that could not reach the database; and `duration_us`, how long the
tool took, in whole microseconds, 0 when the arguments were refused as they
were read and the tool never ran. The arguments themselves, and the text of a
refusal, are never recorded. A call answered with a protocol error, such as
an unknown tool, reached no tool and records no `tool.called`; nor does any
other method. A `create_widget` call that creates a widget records
`widget.created` with the new widget's id, as the form does (`S5`).

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
widget: an object closed to other members, with the properties, in this
order, `id`, a string, with a description of its own that this story does not
fix; `name`, a string; `count`, an integer; and `status`, a string whose
`enum` is `active`, `paused`, `retired`; each of the last three with the same
description as the input property of the same name. `list_widgets`'s
describes an object closed to other members
whose one property, `widgets`, is an array of such widgets. Which members
each output schema marks required, and whether `widgets` carries a
description, are not fixed here. The schemas carry no `$schema` member.

Preconditions:

- dummy is serving.

Postconditions:

- Nothing has changed.

## A model lists the widgets

`list_widgets` takes no arguments and answers with every widget, oldest
first, which is the order the panel's table shows them in (`S3`). A call may
leave `arguments` out or send `{}`; both are the same call.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list_widgets

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has
no `isError` member, a `structuredContent` of

```
{"widgets":[{"id":"<alpha-id>","name":"alpha","count":3,"status":"active"},{"id":"<beta-id>","name":"beta","count":0,"status":"paused"},{"id":"<gamma-id>","name":"gamma","count":12,"status":"retired"}]}
```

and a `content` array of one text block, `{"type":"text","text":<text>}`,
whose text is exactly that line. `<alpha-id>`, `<beta-id>`, and `<gamma-id>`
are the ids dummy gave the three widgets when they were created: three
different values, each `wgt_` and 16 lowercase hexadecimal digits, the same in
every listing, across restarts and deploys.

Preconditions:

- dummy is serving, and telemetry takes every event.
- The database holds exactly the three widgets.

Postconditions:

- Nothing has changed.
- telemetry has received the request's three events, in this order, where
  `<request-id>` is the request's id (`S2`):

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"dummy","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list_widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No widget's id is in them: listing touches every widget and records none.

## A model creates a widget

The ordinary case, and the one `create_widget` exists for. The answer is the
widget as dummy stored it, its id included, so the model need not list the
widgets to learn what it made, and holds the id the trail names the widget
by.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has
no `isError` member, a `structuredContent` of

```
{"id":"<delta-id>","name":"delta","count":7,"status":"active"}
```

and a `content` array of one text block whose text is exactly that line.
`<delta-id>` is the id dummy gave the new widget, `wgt_` and 16 lowercase
hexadecimal digits, different from every other widget's.

Preconditions:

- dummy is serving, and telemetry takes every event.
- The database holds exactly the three widgets, so no widget is named
  `delta`.

Postconditions:

- A widget named `delta`, count 7, status `active`, with the id
  `<delta-id>`, now exists, kept in dummy's database: after dummy restarts,
  or is deployed again, it is still there with the same id, count, and
  status.
- The database holds four widgets, `delta` last, after `gamma`. The panel's
  table and the table fragment show it as their last row from the next
  request on (`S3`, `S4`), and a fragment poll that carries the `ETag` from
  before the call is answered with the new table, not `304`.
- telemetry has received the request's four events, in this order, where
  `<request-id>` is the request's id (`S2`):

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"dummy","event":"widget.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"widget":"<delta-id>"}}
  {"time":"<time>","service":"dummy","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create_widget"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `delta`, its count, and its status are in none of them.

## A model lists the widgets after creating one

A widget a model created is in the next listing, last, because the listing is
in creation order; a widget a user created with the form would be listed the
same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list_widgets

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has
no `isError` member, a `structuredContent` of

```
{"widgets":[{"id":"<alpha-id>","name":"alpha","count":3,"status":"active"},{"id":"<beta-id>","name":"beta","count":0,"status":"paused"},{"id":"<gamma-id>","name":"gamma","count":12,"status":"retired"},{"id":"<delta-id>","name":"delta","count":7,"status":"active"}]}
```

and a `content` array of one text block whose text is exactly that line.
`<alpha-id>`, `<beta-id>`, and `<gamma-id>` are the ids of `A model lists the
widgets`, and `<delta-id>` is the id `A model creates a widget` answered with.

Preconditions:

- dummy is serving.
- The database held exactly the three widgets before the `create_widget`
  call of `A model creates a widget`, and nothing else has changed it since.

Postconditions:

- Nothing has changed.

## A user sees on the panel a widget a model created

The tools and the panel share one set of widgets, so what a model creates a
user sees without doing anything: the panel's script re-fetches the table
fragment every 5 seconds (`S4`), and the next fetch carries the new row.

Request:

```
GET /widgets/table HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

- dummy is serving.
- The database held exactly the three widgets before the `create_widget`
  call of `A model creates a widget`, and nothing else has changed it since.

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
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"  big delta  ","count":0,"status":"paused"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has
no `isError` member, a `structuredContent` of

```
{"id":"<widget-id>","name":"big delta","count":0,"status":"paused"}
```

and a `content` array of one text block whose text is exactly that line.
`<widget-id>` is the id dummy gave the new widget.

Preconditions:

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- A widget named `big delta`, count 0, status `paused`, now exists, last.
  No widget's name begins or ends with a space.

## A model creates a widget with no name

A name is required, so an empty one is refused rather than stored as a blank
name. A name of nothing but white space is empty once trimmed and is refused
the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"   ","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving, and telemetry takes every event.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds `alpha`,
  `beta`, and `gamma`, in that order, with the counts and statuses they had.
- dummy recorded no `widget.created`. Between the request's `request.started`
  and its `request.finished`, whose `status` is 200, telemetry has received
  one event, where `<request-id>` is the request's id (`S2`):

  ```
  {"time":"<time>","service":"dummy","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create_widget"}}
  ```

## A model creates a widget with a name longer than 40 characters

40 characters is the limit, counted after trimming, so 41 is refused. The
limit counts characters, not bytes: a name of 40 accented letters is
accepted. The name below is 41 characters.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"a-widget-name-that-is-far-too-long-to-fit","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model creates a widget with a name that is already taken

Names are unique across widgets, however the widget was created. The
comparison is made after trimming and is exact, letter case counting: ` alpha `
is taken, because it trims to `alpha`, while `Alpha` is a different name, free
to use, and creating it succeeds as in `A model creates a widget`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"alpha","count":5,"status":"paused"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets, so a widget named `alpha`
  exists.

Postconditions:

- No widget was created. The database is unchanged; in particular `alpha`
  still has count 3 and status `active`. A taken name is refused, never
  merged into the widget that holds it.

## A model creates a widget with a negative count

Zero is allowed — `beta` has a count of 0 — and anything below it is not. A
negative count is a whole number, so it passes the schema, which states no
lower bound, and is refused by dummy's rule.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":-1,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model breaks several of dummy's rules at once

Every rule is checked and every broken one reported, name before count, so a
model that got both wrong learns both from one answer.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"alpha","count":-4,"status":"retired"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model sends a status that is not one of the three

The schema lists the three statuses, so a value outside them is refused when
the arguments are read, before dummy's rules are applied, with the
platform's wording, which quotes the allowed values and the one received. The
value must match exactly: `Active` and ` active` are refused too.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"archived"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model sends a count that is not a number

The count is a JSON number. A count sent as anything else — a string, even
one holding digits, here `"7"` — is refused when the arguments are read,
naming the JSON type that arrived. A widget is never created with a count the
model did not give as a number.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":"7","status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model sends a count that is not a whole number

The count is a whole number. A number with a fractional part is refused,
quoted exactly as it was sent. A number whose fractional part is zero is a
whole number however it is written, so `7.0` is the count 7 and is accepted.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7.5,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model sends a count too large to hold

A count is a 64-bit signed whole number, so a whole number beyond that range
is refused when the arguments are read, with the bounds stated so the model
can correct it. The number is quoted exactly as it was sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":1e19,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model leaves out a field

All three fields are required. A field left out is refused when the
arguments are read; dummy never supplies a default status or count.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving, and telemetry takes every event.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.
- dummy recorded no `widget.created`. Between the request's `request.started`
  and its `request.finished`, whose `status` is 200, telemetry has received
  one event, where `<request-id>` is the request's id (`S2`); the tool never
  ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"dummy","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"create_widget"}}
  ```

## A model sends a field the tool does not have

A widget has three fields and no others, so an argument the tool does not
know is refused rather than ignored: a model that believes it set a colour
must learn that it did not.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"active","colour":"red"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model sends several malformed arguments at once

Every offence found while reading the arguments is reported in one answer:
the tool's fields first, in the order `name`, `count`, `status`, then each
unknown field in the order it was sent. `list_widgets` reads its arguments
the same way, so any argument sent to it is an unknown field.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"create_widget","arguments":{"colour":"red","count":"7","status":"archived"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model's arguments are malformed and break dummy's rules too

dummy's rules are applied only to arguments that were read cleanly, so when
both layers would object the answer names only what reading found. Here the
name is taken and the count negative, but the status is not one of the
three, and that is all the answer says. Once the model fixes the status, the
next answer names the rule offences.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"alpha","count":-4,"status":"archived"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving.
- The database holds exactly the three widgets.

Postconditions:

- No widget was created. The database is unchanged: it holds exactly the three
  widgets.

## A model calls a tool dummy does not have

dummy has two tools. A call naming any other — a tool to remove a widget,
say, which dummy does not offer — is not a tool's refusal but a protocol
error: there is no tool to answer it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete_widget

{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"delete_widget","arguments":{"name":"alpha"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 19 and no `result`,
whose `error` has `code` `-32602` and `message` `Unknown tool: delete_widget`.

Preconditions:

- dummy is serving, and telemetry takes every event.
- The database holds exactly the three widgets.

Postconditions:

- Nothing has changed. `alpha` still exists.
- dummy wrote nothing to stderr.
- No tool ran, so dummy recorded no `tool.called`: telemetry has received
  only the request's `request.started` and its `request.finished`, whose
  `status` is 400.

## A model calls a tool while dummy cannot reach the widgets

Both tools answer from the database, so a tool that cannot read or write it
has nothing true to say. `list_widgets` does not answer as if there were no
widgets, which would tell the model they were gone; it refuses, and quotes
nothing of the database's own error. The model can tell this from a refusal
of its arguments and may try again later. dummy keeps serving; a user who
opens the panel meanwhile is answered as `S3` tells, with the same line as
plain text.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list_widgets

{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 23 whose `result` has
`isError` `true`, no `structuredContent`, and a `content` array of one text
block whose text is exactly:

```
cannot reach the widgets; try again later
```

A `create_widget` call whose arguments dummy would otherwise accept is
refused with the same text, and stores nothing: no widget is created and no
`widget.created` is recorded. Its `tool.called` has `kind` `additive` and
`outcome` `error`.

Preconditions:

- dummy is serving, and telemetry takes every event.
- dummy can no longer read `state/dummy.db`: the filesystem holding it has
  failed since dummy opened it, say.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr. telemetry has received the request's three
  events, in this order, where `<request-id>` is the request's id (`S2`):

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"dummy","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"list_widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- dummy is still serving.

## A request to /mcp arrives without the identity headers

`/mcp` follows the rule every route follows: the gate sets `X-User-Id` on
every request it forwards and a sibling forwards the one it received, so a
request without it says the gate or a sibling is misconfigured, a server
fault answered 500. The identity check runs before anything about MCP is
looked at, so the answer is the same plain text every route gives, not a
JSON-RPC response, and no tool runs, whatever the body asked for.

Request:

```
POST /mcp HTTP/1.1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create_widget

{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"create_widget","arguments":{"name":"delta","count":7,"status":"active"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending
with LF.

Preconditions:

- dummy is serving.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.
- The database holds exactly the three widgets.

Postconditions:

- Nothing has changed. No widget named `delta` exists.
- dummy wrote nothing to stderr about the 500. Its trail records the request
  as it records every request (`S3`): a `request.started` with the `method`
  `POST` and the `path` `/mcp`, and a `request.finished` with the `status`
  500, both with an empty user, under the id dummy gave the request (`S2`).
  No tool ran, so there is no `tool.called` and no `widget.created`.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client
hoping for a stream, is refused with an empty body: dummy offers no stream and
no page at this address. This is not the panel's 405 page (`S3`): the answer
carries no banner and no HTML.

Request:

```
GET /mcp HTTP/1.1
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

- dummy is serving.

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
file here, `/tmp/services.json`, lists dummy, and the telemetry service, at a
socket where the developer's stand-in takes every event (`S2`):

```
{
  "services": [
    {"name": "dummy", "url": "https://dummy.sbx.ikigenba.dev/", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><rect x='4' y='4' width='16' height='16'/></svg>"},
    {"name": "telemetry", "url": "https://telemetry.sbx.ikigenba.dev/", "description": "The suite's trail of events", "socket": "/tmp/telemetry.sock", "enabled": true, "mcp": true}
  ]
}
```

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":21,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving, started with `IKIGENBA_SERVICES=/tmp/services.json` in
  its environment.
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
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":22,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- dummy is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr about the missing instructions. With no
  services file it has no telemetry to send to (`S2`), so stderr holds the
  request's two events, `request.started` and `request.finished`, each as a
  `dummy: undelivered event: <event>` line, and nothing else for this
  request.

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

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has
exactly these members:

```
{
  "protocolVersion": "2025-11-25",
  "capabilities": {"tools": {}},
  "serverInfo": {"name": "dummy", "version": "<display>"},
  "instructions": "Demo widgets to list and create"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- dummy is serving, started with `IKIGENBA_SERVICES=/tmp/services.json` in
  its environment.
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

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has
`tools`, the array of `An MCP client lists dummy's tools`, member for
member, and no `nextCursor`, `resultType`, `_meta`, `ttlMs` or
`cacheScope`.

Preconditions:

- dummy is serving.

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
the same two tools, in the same order, member for member, with `<display>`
the string the deployed binary's `dummy --version` prints under the
environment the host gives dummy (`S7`).

Preconditions:

- dummy is deployed and active on `sbx.ikigenba.dev`, as in `S7`.
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

- dummy is deployed and active on `sbx.ikigenba.dev`, as in `S7`.
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
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list_widgets

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no
`isError` member and whose `structuredContent` is an object whose one member,
`widgets`, is an array of every widget dummy holds, oldest first, each with
its `id`, `name`, `count`, and `status`, with one text content block
holding the same object encoded compactly, as in `A model lists the widgets`.

Preconditions:

- dummy is deployed and active on the host, serving on
  `/run/ikigenba/dummy.sock`.
- The host's services file lists dummy with `"mcp": true` and the socket
  `/run/ikigenba/dummy.sock`, and lists the telemetry service, which takes
  every event.
- The caller runs as the `ikigenba` user, which can reach the socket.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr.
- telemetry has received dummy's three events for the call under the id the
  gateway forwarded and the user it forwarded, so a trace of that id shows
  the gateway's forward and dummy's execution together:

  ```
  {"time":"<time>","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"dummy","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list_widgets"}}
  {"time":"<time>","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```
