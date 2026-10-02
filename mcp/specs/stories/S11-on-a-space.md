# Stories — on a space

The gateway reached through a space: the file `S10` describes, deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at `mcp.<space>` over TLS. A space is one label under the root domain and an app is `<app>.<space>`, so mcp on the space `sbx.ikigenba.dev` answers at `mcp.sbx.ikigenba.dev`. nginx on the space proxies to mcp's socket, `/run/ikigenba/mcp.sock` (`S02`). The space authenticates every request before it reaches mcp and passes the caller on in `X-User-Id` and `X-User-Email`, with the request's id in `X-Request-Id`; mcp itself has no unauthenticated case, so a request that arrives at all is one of a known caller. A page request without a credential is the gate's to answer, as for every app. The host's services file is `/var/lib/ikigenba/services.json`, which opsctl writes and names in mcp's environment (`S02`). The gateway reaches each MCP service directly on the socket its entry names, never through nginx, forwarding the caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id` (`S08`). The stories prove the whole path from checkout to client and nothing about mcp that the earlier groups do not already say. devctl and opsctl are named only by their published commands. The MCP requests below are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision (`S05`) are not repeated.

## A visitor reaches the connect page on a space

The visitor asks for the connect page over TLS at the gateway's hostname on the space, to learn what to tell their AI assistant. The gate authenticates the request and hands mcp the caller's identity, and mcp renders the page for that caller, naming the endpoint by the hostname the visitor reached.

Request:

```
$ curl -si https://mcp.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the connect page (`S03`): an HTML page whose banner's profile link is titled with the email address of the caller the gate authenticated, whose visible text carries the heading `Connect an MCP client`, the endpoint `https://mcp.sbx.ikigenba.dev/mcp`, and the line `Every request must send the header Authorization: Bearer <token>. Create a token on your profile.`, in which `your profile` leads to `https://auth.sbx.ikigenba.dev/`, and whose `Services` section lists the MCP services installed on the space, `dummy` among them, `available`, with its endpoint `https://mcp.sbx.ikigenba.dev/mcp/dummy`. There is no row for `mcp` or `auth`. The footer reads `mcp v<semver>`, the version the deployed binary's `mcp --version` prints (`S01`), the same one `space status` reports for mcp. Its stylesheet is `https://mcp.sbx.ikigenba.dev/_appkit/theme.css`, and the fonts that stylesheet loads are under the same `https://mcp.sbx.ikigenba.dev/_appkit/` (`S04`): a browser showing the page requests its style from mcp's own host and from no other origin. In the banner, the profile link leads to `https://auth.sbx.ikigenba.dev/`, and the `Sign out` button is in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout` (`S03`); submitting it signs the visitor out of the space, as auth's stories tell.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance is `running`, and `opsctl` is installed on it.
- A tag `mcp/v<semver>` points at the commit `devctl build mcp` was run at, and it wrote `mcp/dist/mcp-v<semver>.tar.xz`.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev mcp/dist/mcp-v<semver>.tar.xz` exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows `mcp v<semver> active active -`.
- auth and dummy are deployed and active on the space, and dummy's manifest has `mcp = true`, so the host's services file lists `auth`, and lists `dummy` enabled and marked for MCP.
- The space routes `mcp.sbx.ikigenba.dev` through its authenticating gate: the gate admits the request and sets `X-User-Id` and `X-User-Email` on what it passes to mcp, and refuses a request it cannot authenticate before mcp sees it.
- The caller holds a credential the gate accepts, and the email that credential names is the one the page's profile link is titled with.

Postconditions:

- Nothing has changed. No backend was contacted.

## A visitor on a space opens the service launcher

On a space the host's services file lists every service installed on the host, and mcp's entry carries an icon because mcp's package ships `share/icon.svg` (`S10`), which is what puts mcp in the launcher (`S03`). So the page a visitor reaches on a space carries the launcher in its banner, and mcp is one of the services it offers. The launcher's text and behaviour are `S03`'s; this story fixes only what the visitor sees on a space.

Request:

```
$ curl -si https://mcp.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the connect page of the story above, and its banner carries the launcher button (`S03`). In a browser, pressing the button opens a list of the space's services with a search box labelled `Find a service`; each entry shows a service's icon and name, as `S03` tells. mcp's own entry is in the list and is marked as the current page. The launcher's script is `https://mcp.sbx.ikigenba.dev/_appkit/launcher.js` (`S04`), so the launcher, like the style, needs nothing from any other origin.

Preconditions:

- Everything the story above requires holds: mcp `v<semver>` is deployed and active on `sbx.ikigenba.dev`, and the caller holds a credential the gate accepts.
- `mcp/dist/mcp-v<semver>.tar.xz` holds `share/icon.svg` (`S10`).
- The host's services file lists mcp with its icon.

Postconditions:

- Nothing has changed.

## A client on a space lists the gateway's tools

On a space, the gateway's MCP endpoint is `https://mcp.<space>/mcp`, the endpoint the connect page shows, through the space's nginx, which authenticates the request — with a bearer token or the space's session cookie, as auth decides — and passes the caller to mcp in `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as for the page. The client sends no identity headers of its own; whatever it sent would be replaced. Both forms below behave identically.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X POST -b 'ikigenba_session=<session>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is the answer of `An MCP client lists the gateway's tools` (`S05`): the same four tools, `services`, `describe`, `call`, and `mutate`, in that order, member for member, with the result's `io.modelcontextprotocol/serverInfo` `{"name":"mcp","version":"v<semver>"}`, where `v<semver>` is the version the deployed binary's `mcp --version` prints.

Preconditions:

- mcp `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches the connect page on a space`.
- `<token>` is a bearer token, or `<session>` a session, that the space's gate accepts.

Postconditions:

- Nothing has changed. No backend was contacted, and mcp wrote nothing to stderr.

## A client on a space without a credential is refused before the gateway

The space's gate answers an unauthenticated request to `/mcp`, or to any path beneath it, itself, with a challenge an MCP client understands, rather than redirecting to a sign-in page as it does for a page. That answer is the gate's, the same for every app's `/mcp`; mcp never sees the request, so a scoped endpoint is refused the same way, well-formed or not.

Request:

```
$ curl -si -X POST -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X POST -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp/dummy
```

Response:

```
HTTP/2 401
www-authenticate: Bearer realm="ikigenba"
```

Status 401. The body is one line of plain text from the gate; this story does not fix it.

Preconditions:

- mcp `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches the connect page on a space`.
- The request carries no credential.

Postconditions:

- Nothing has changed. The request never reached mcp, and mcp wrote nothing to stderr.

## A model on a space calls dummy through the gateway

This is what the gateway is for: a model connected to the one endpoint runs a tool of a service on the same space. The model names the service and the tool, and the gateway reaches dummy directly on `/run/ikigenba/dummy.sock`, lists its tools to learn that `list_widgets` is a read tool, calls it on the caller's behalf, and relays dummy's answer (`S08`). dummy sees the call as one from the caller, with the same `X-Request-Id` nginx gave the request to the gateway.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` is dummy's answer to `list_widgets`, relayed: it has no `isError` member, its `structuredContent` is an object whose one member, `widgets`, is an array of every widget dummy holds, oldest first, each with its `name`, `count`, and `status`, and its `content` is one text block holding that same object encoded compactly, as dummy's own stories tell. The result's `io.modelcontextprotocol/serverInfo` is the gateway's, `{"name":"mcp","version":"v<semver>"}`, not dummy's.

Preconditions:

- mcp `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches the connect page on a space`.
- dummy is deployed and active on the space, serving on `/run/ikigenba/dummy.sock`, and its manifest has `mcp = true`, so the host's services file lists `dummy` enabled, marked for MCP, with that socket.
- `<token>` is a bearer token that the space's gate accepts.
- The telemetry service is deployed and active on the space, and the host's services file lists it as `telemetry`.

Postconditions:

- Nothing has changed. No widget was created.
- mcp wrote nothing to stderr, and neither did dummy.
- The space's telemetry service holds mcp's trail for the request, under `<id>`, the `X-Request-Id` nginx set on the request, and the id of the user `<token>` belongs to (`S08`):

  ```
  request.started method=POST path=/mcp
  sibling.called target=dummy method=POST path=/mcp status=200
  sibling.called target=dummy method=POST path=/mcp status=200
  tool.called tool=call kind=read outcome=ok
  request.finished status=200
  ```

  It holds dummy's own events for the two requests the gateway made under the same `<id>` and user, dummy's `tool.called` for `list_widgets` among them, so a trace of `<id>` shows the forward and the execution.
