# Stories — services

The gateway's `services` tool, offered at `/mcp` and at every scoped `/mcp/<scope>` (`S05`): it lists the services the connection reaches and whether each is available, so a model learns what it can use before it asks `describe` (`S07`) for a service's tools. It takes no arguments. Its answer is an object whose one member, `services`, is an array with one entry per service the connection reaches, in name order: on `/mcp` the MCP services of the services file, and on a scoped endpoint exactly the scope's names (`S05`). Each entry's members are, in this order, `name`; `description`, the entry's `description` from the services file, or `""` for a name that is not installed; `available`, `true` or `false`; and, only when `available` is `false`, `reason`, which is `disabled` for an MCP service whose `enabled` is `false` and `not installed` for a scoped name the file does not hold as an MCP service. The answer is read from the services file as it stands at the moment of the call. `services` contacts no backend: whether a service is available is what the file says, not whether its socket answers. Requests, the envelope, the result's two forms, the gateway's silence on stderr, and the `tool.called` event each call adds to the trail are as `S05` fixes them; `services` writes nothing to stderr.

## A model lists the services

The ordinary case. A model calls `services` first, to learn which services it may use and which it may not, and why. `notes` is switched off on this host, so it is listed as unavailable, `disabled`; `auth` and `mcp` are not MCP services and are not listed at all. A call may leave `arguments` out or send `{}`; both are the same call.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: services

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"services","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[{"name":"dummy","description":"Demo widgets to list and create","available":true},{"name":"notes","description":"Notes to keep and search","available":false,"reason":"disabled"}]}
```

and a `content` array of one text block, `{"type":"text","text":<text>}`, whose text is exactly that line.

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file (`S05`).

Postconditions:

- Nothing has changed. The services file is as it was.
- No backend was contacted; neither dummy nor notes received a request.
- The gateway wrote nothing to stderr.
- The trail holds, under user `u_7f3a9c21` and a request id mcp made up for it:

  ```
  request.started method=POST path=/mcp
  tool.called tool=services kind=read outcome=ok
  request.finished status=200
  ```

## A model lists the services through a scoped endpoint

On a scoped endpoint the list is the scope, nothing more and nothing less, in name order whatever order the path gave. A name the gateway cannot reach as an MCP service is still listed, so the model learns why it cannot use it rather than finding it missing: `ghost`, which the file does not hold; `auth`, whose entry has `mcp` `false`; and `mcp`, the gateway itself, are each `not installed`, with an empty description. `notes`, an MCP service outside the scope, is not listed.

Request:

```
POST /mcp/mcp,ghost,dummy,auth HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: services

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"services","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[{"name":"auth","description":"","available":false,"reason":"not installed"},{"name":"dummy","description":"Demo widgets to list and create","available":true},{"name":"ghost","description":"","available":false,"reason":"not installed"},{"name":"mcp","description":"","available":false,"reason":"not installed"}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file (`S05`), which has no entry named `ghost`.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.
- The trail holds, under user `u_7f3a9c21` and a request id mcp made up for it:

  ```
  request.started method=POST path=/mcp/mcp,ghost,dummy,auth
  tool.called tool=services kind=read outcome=ok
  request.finished status=200
  ```

## A model lists the services on a host with no services file

With no services file, `/mcp` reaches no services, and the list is empty rather than an error: the model learns there is nothing to use. The answer is the same when the file named is missing, cannot be read, is not a services file, or lists no MCP service.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: services

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"services","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The gateway is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- The gateway wrote nothing to stderr about the services file. Without one, though, it cannot find the telemetry service either, so no event of the request reaches the trail, and stderr holds one `undelivered event` line for each (`S02`), in this order, where `<id>` is the request id mcp made up for the request, each `<time>` is when mcp recorded that event, each `<us>` a duration in whole microseconds, and each `<bytes>` a count of body bytes:

  ```
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.started","request_id":"<id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"tool.called","request_id":"<id>","user":"u_7f3a9c21","attrs":{"duration_us":<us>,"kind":"read","outcome":"ok","tool":"services"}}
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.finished","request_id":"<id>","user":"u_7f3a9c21","attrs":{"duration_us":<us>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model never sees the gateway listed

The gateway is not one of its own services: listing itself would let a model call the gateway through the gateway. An entry named `mcp` is never an MCP service, even when the file marks it `true`, so the list is the one the suite's services file gives. Here `/var/lib/ikigenba/services.json` is the suite's services file with the `mcp` entry's `mcp` now `true`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: services

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"services","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is the answer of `A model lists the services`, with `id` 4: `dummy` available and `notes` `disabled`, and no entry named `mcp`.

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file (`S05`) with the entry named `mcp` marked `"mcp": true`.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.
- The trail holds, under user `u_7f3a9c21` and a request id mcp made up for it:

  ```
  request.started method=POST path=/mcp
  tool.called tool=services kind=read outcome=ok
  request.finished status=200
  ```

## A model sees the list follow a change to the services file

The host rewrites the services file when a service is installed or switched on or off, and the gateway reads the file afresh for every call, so the next `services` call shows the change without the gateway being restarted. Here the host has switched `notes` on since the gateway started: `/var/lib/ikigenba/services.json` is the suite's services file with `notes`'s `enabled` now `true`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: services

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"services","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[{"name":"dummy","description":"Demo widgets to list and create","available":true},{"name":"notes","description":"Notes to keep and search","available":true}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The gateway is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment while `/var/lib/ikigenba/services.json` held the suite's services file with `notes` switched off, and it has not been restarted since.
- `/var/lib/ikigenba/services.json` now lists `notes` with `enabled` `true`.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr. The call added the three events of `A model lists the services` to the trail.

## A model passes services an argument it does not take

`services` takes no arguments, so any argument sent to it is an unknown field and is refused with the platform's wording rather than ignored: a model that believes it filtered the list must learn that it did not.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: services

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"services","arguments":{"bogus":true},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
bogus: unknown field
```

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file (`S05`).

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.
- The trail holds, under user `u_7f3a9c21` and a request id mcp made up for it:

  ```
  request.started method=POST path=/mcp
  tool.called tool=services kind=read outcome=invalid_arguments
  request.finished status=200
  ```
