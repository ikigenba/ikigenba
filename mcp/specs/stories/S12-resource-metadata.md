# Stories — resource metadata

The gateway's protected-resource metadata: the document an MCP client reads to learn which authorization server issues the tokens `/mcp` accepts. A client that is refused at `/mcp` without a token is pointed at `<scheme>://<host>/.well-known/oauth-protected-resource` by the gate's challenge (opsctl's `S5-nginx.md`), or probes that path itself, or the path-specific form for the endpoint it was given, `/.well-known/oauth-protected-resource/mcp`; so mcp serves one document at `/.well-known/oauth-protected-resource` and at every path beneath it, the same document at each, whatever follows. A path is beneath it when it continues with `/`; `/.well-known/oauth-protected-resourcex` is not, and is answered 404 (`S03`). The document is for anyone: a client asks for it before it has a token, so a guest and a signed-in user get the same answer, byte for byte, and no guest is sent to sign in. It is a JSON object with exactly three members: `resource`, the endpoint `<scheme>://<host>/mcp`, built from the request's `Host` and `X-Forwarded-Proto` exactly as the connect page builds its endpoint (`S03`), whatever path beneath the document was asked for, since every scoped endpoint lies beneath it; `authorization_servers`, an array holding one string, auth's address; and `bearer_methods_supported`, the array `["header"]`. It advertises no scopes. Auth's address is the origin of the `url` of the services file's entry named `auth`: its scheme, `://` and its host with any port, with no path, query, fragment or trailing slash, so `https://auth.sbx.ikigenba.dev/` and `https://auth.sbx.ikigenba.dev/x?y=1` both give `https://auth.sbx.ikigenba.dev`. When the file has no such entry, or its `url` is empty or lacks a scheme or a host, auth's address is `<scheme>://auth.<space>`, the space and the scheme read from the request as the connect page reads them for its links to auth (`S03`). mcp takes the services file's path from `IKIGENBA_SERVICES` and reads the file afresh for every request, under the rules `S03` gives; a broken services file never breaks the document. The requests go to a running mcp (`S02`), shown as the HTTP request mcp receives, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, that file holding the suite's services file of `S03` unless a story says otherwise. Every request adds exactly two events to mcp's trail, `request.started` and `request.finished`, as for every answer of `S03`, and serving the document contacts no backend.

## An MCP client reads the gateway's protected-resource metadata

A client was refused at `https://mcp.sbx.ikigenba.dev/mcp` and told where the metadata is. It has no token yet, so it asks as a guest, and learns that the endpoint it was given is the resource and that auth issues its tokens. The request carries no `X-User-Id`; a request that carries a user's `X-User-Id` and `X-User-Email` is answered the same, byte for byte.

Request:

```
GET /.well-known/oauth-protected-resource HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON object with exactly these members, which no story fixes the order or spacing of:

```
{"resource":"https://mcp.sbx.ikigenba.dev/mcp","authorization_servers":["https://auth.sbx.ikigenba.dev"],"bearer_methods_supported":["header"]}
```

The response sets no cookie.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file, whose `auth` entry has the `url` `https://auth.sbx.ikigenba.dev`.
- The request carries no `X-User-Id` and no `X-User-Email`, as nginx forwards a guest's request, and the `X-Request-Id` nginx gave it.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr. The trail holds two events for the request, under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user:

  ```
  request.started method=GET path=/.well-known/oauth-protected-resource
  request.finished status=200
  ```

## An MCP client probes the metadata for the endpoint it was given

A client that was not pointed at the metadata looks for it at the path-specific form for its endpoint: the document's path followed by the endpoint's path. A client given a scoped endpoint probes beneath the scope. Every path beneath the document's is answered with the same document, and its `resource` stays `https://mcp.sbx.ikigenba.dev/mcp`, which every scoped endpoint lies beneath. The path after the document's is not read: a scope that is not well-formed, or a path that is no endpoint at all, gets the same document.

Request:

```
GET /.well-known/oauth-protected-resource/mcp HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

```
GET /.well-known/oauth-protected-resource/mcp/dummy,notes HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

```
GET /.well-known/oauth-protected-resource/ HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

```
GET /.well-known/oauth-protected-resource/anything/else HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. For every form the body is the document of `An MCP client reads the gateway's protected-resource metadata`, member for member:

```
{"resource":"https://mcp.sbx.ikigenba.dev/mcp","authorization_servers":["https://auth.sbx.ikigenba.dev"],"bearer_methods_supported":["header"]}
```

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.
- The request carries no `X-User-Id` and no `X-User-Email`.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr. The trail's `request.started` event for each request carries the path as it arrived, `path=/.well-known/oauth-protected-resource/mcp` for the first form, and so on.

## An MCP client reads the metadata on a host whose services file names no auth

The document names the resource by the `Host` the request arrived with and auth by the services file's `auth` entry, so one mcp build serves whichever space it is installed on. Here `/run/ikigenba/services.json` is the suite's services file without the `auth` entry, so auth's address is read from the request: a trailing port is dropped from the `Host`, then a single leading `mcp.` label, and what remains is the space.

Request:

```
GET /.well-known/oauth-protected-resource HTTP/1.1
Host: mcp.sbx.ikigenba.dev:443
X-Forwarded-Proto: https
```

```
GET /.well-known/oauth-protected-resource HTTP/1.1
Host: mcp.sbx.ikigenba.dev
```

```
GET /.well-known/oauth-protected-resource HTTP/1.1
Host: sbx.ikigenba.dev
X-Forwarded-Proto: https
```

```
GET /.well-known/oauth-protected-resource HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: http
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON object with exactly the three members of `An MCP client reads the gateway's protected-resource metadata`, and `bearer_methods_supported` is `["header"]` for every form. For the first form `resource` is `https://mcp.sbx.ikigenba.dev:443/mcp`, the `Host` as it arrived; for the second, which carries no `X-Forwarded-Proto` and so is `https`, it is `https://mcp.sbx.ikigenba.dev/mcp`; for the third, `https://sbx.ikigenba.dev/mcp`; and for the fourth, `http://mcp.sbx.ikigenba.dev/mcp`. `authorization_servers` is `["https://auth.sbx.ikigenba.dev"]` for the first three forms and `["http://auth.sbx.ikigenba.dev"]` for the fourth.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file without the entry named `auth`.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr about the services file.

## A client asks for the metadata's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body, at the document's path and at every path beneath it.

Request:

```
HEAD /.well-known/oauth-protected-resource HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

```
HEAD /.well-known/oauth-protected-resource/mcp HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is empty.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.

## A caller sends the metadata a method it does not take

The document is only read, so its path and every path beneath it take `GET` and `HEAD` and nothing else, and `Allow` names those two. No document is sent.

Request:

```
POST /.well-known/oauth-protected-resource HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

```
DELETE /.well-known/oauth-protected-resource/mcp HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. `PUT`, `PATCH`, and every other method but `GET` and `HEAD` are refused the same way, a signed-in user's included.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr.
