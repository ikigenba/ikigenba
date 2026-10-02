# Stories — routing

Every request to a sandbox arrives at its own nginx, `sandbox-wip-nginx.service`, a user unit run as the developer that `up` starts and configures, listening on `127.0.0.1:7400` in plain HTTP and proxying to each app's socket. It routes as the platform's nginx does on a host, except that it drops client-supplied identity headers on every host, so an app behaves the same in the sandbox as deployed: by host name, one name per app, with `auth`, when the checkout holds it, standing between every other app and the outside. Browsers and curl resolve every name under `localhost` to the loopback address, so the names below need no DNS. The stories share one setting unless they say otherwise: the sandbox `wip` is up on port `7400` from the worktree `/home/me/src/ikigenba/wip`, its apps are `auth` and `dummy`, and neither is the default app. When `auth` is present, every request for an app other than `auth` is first put to auth's `/check` as an internal subrequest carrying the request's `Cookie` and `Authorization` headers and no body, and naming the request it decides in three headers of nginx's own making, never the client's: `X-Original-Method`, its method; `X-Original-Host`, its host name, lowercased and without the port; and `X-Original-URI`, its path and query exactly as the client sent them. What `/check` answers decides the request. On every such app's server, whatever its manifest's `mcp` holds, a 401 from `/check` under `/mcp` draws a bearer challenge in place of the sign-in redirect, as on a host. Every request nginx passes to an app carries `Host` as the client sent it, `X-Real-IP`, `X-Forwarded-For`, `X-Forwarded-Proto: http`, and an `X-Request-Id` nginx made for that request, never the client's own; the `X-User-Id` and `X-User-Email` an app receives are only ever the ones auth gave, never the client's. nginx drops a client's own `X-User-Id` and `X-User-Email` on every host it serves, auth's own name included, and in a sandbox without `auth` as well, where an app is given no user at all. Each app's server also carries the app's own nginx configuration, when the app ships one, as a host's does.

## A browser reaches an app at its own name

Each app answers at `http://<app>.wip.localhost:7400`, and nginx passes the request to that app's socket.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' 'http://dummy.wip.localhost:7400/widgets?page=2'
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for that cookie.
- dummy answers `GET /widgets?page=2` with 200.

Postconditions:

- Nothing has changed.
- dummy received `GET /widgets?page=2` on its socket, with `Host: dummy.wip.localhost:7400`, `X-Forwarded-Proto: http`, and `X-Real-IP` and `X-Forwarded-For` naming `127.0.0.1`.
- auth's `/check` received `X-Original-Method: GET`, `X-Original-Host: dummy.wip.localhost` and `X-Original-URI: /widgets?page=2`.

## A browser reaches the default app at the sandbox's bare name

The app whose manifest sets `default = true` answers at `http://wip.localhost:7400` as well as at its own name.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' http://wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- dummy's manifest sets `default = true`, and `wip` is up from an `up` run with it so.
- auth's `/check` answers 200 for that cookie.
- dummy answers `GET /widgets` with 200.

Postconditions:

- Nothing has changed.
- dummy received `GET /widgets` on its socket, with `Host: wip.localhost:7400`.

## A browser asks for the sandbox's bare name when no app is the default

Request:

```
$ curl -si http://wip.localhost:7400/
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed.

Preconditions:

- `wip` is up with `auth` and `dummy`, and no manifest sets `default = true`.

Postconditions:

- Nothing has changed. No app received the request, and no `/check` subrequest was made.

## A browser asks for a host the sandbox does not serve

A name under `wip.localhost` that is not an app, and any host name the sandbox does not know at all, are answered by nginx itself.

Request:

```
$ curl -si http://nope.wip.localhost:7400/
```

```
$ curl -si -H 'Host: example.com' http://127.0.0.1:7400/
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed.

Preconditions:

- `wip` is up with `auth` and `dummy`; the checkout holds no app named `nope`.

Postconditions:

- Nothing has changed. No app received the request, and no `/check` subrequest was made.

## Google sends a browser back to the bare localhost address

Google accepts a sign-in redirect to `http://localhost:<port>/...` but never to a name under `localhost`, so auth asks Google to send the browser back to `http://localhost:7400`, the origin it finds in `IKIGENBA_CALLBACK_URL`. In a sandbox that holds `auth`, nginx answers every request whose host is bare `localhost:7400`, whatever its path, by sending the browser on to the same path and query at auth's own name.

Request:

```
$ curl -si 'http://localhost:7400/login/google/callback?code=4/0Ab&state=xyz'
```

```
$ curl -si http://localhost:7400/
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/login/google/callback?code=4/0Ab&state=xyz
```

Status 302. For the second form `Location` is `http://auth.wip.localhost:7400/`. The body is not fixed.

Preconditions:

- `wip` is up with `auth` and `dummy`.

Postconditions:

- Nothing has changed. No app received the request, and no `/check` subrequest was made; the browser's next request goes to auth.

## A browser that has not signed in reaches an app

auth's `/check` answers 401 for a request with no credential it accepts, and nginx sends the browser to sign in at auth, carrying the URL it asked for in `return`: the scheme, the host with its port, and the request's path and query, appended as they are, unencoded.

Request:

```
$ curl -si 'http://dummy.wip.localhost:7400/widgets?page=2'
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/?return=http://dummy.wip.localhost:7400/widgets?page=2
```

Status 302. The response carries no `WWW-Authenticate` header; the body is not fixed.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket; the request was decided by the `/check` subrequest alone.

## A signed-in browser reaches an app as its user

auth's `/check` answers 200 for the browser's session and says who the user is in its own `X-User-Id` and `X-User-Email` headers. nginx sets those two headers on the request it passes to the app, so the app knows its user without asking auth itself.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for that cookie, with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.
- dummy answers `GET /widgets` with 200.

Postconditions:

- Nothing has changed.
- auth's `/check` received the request's `Cookie` header and no body.
- dummy received `GET /widgets` with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.

## An agent reaches an app with a bearer token

A program that cannot sign in through a browser sends the bearer token a human created at auth. auth's `/check` sees the `Authorization` header and decides the request exactly as it decides a session. Here the program is an MCP client asking dummy's `/mcp` for its tools; `/mcp` answers only `POST`.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://dummy.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for `ikp_<token>`, with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.
- dummy answers that `POST /mcp` with 200.

Postconditions:

- Nothing has changed.
- auth's `/check` received the request's `Authorization` header and no body, with `X-Original-Method: POST`, `X-Original-Host: dummy.wip.localhost` and `X-Original-URI: /mcp`.
- dummy received `POST /mcp` with the request's body, `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.

## An MCP client reaches an app without a credential

An MCP client is a program, not a browser, and cannot follow a redirect to a sign-in page. So under `/mcp`, a 401 from `/check` is answered `401` by nginx itself, with a challenge naming the scheme to use and one line saying what to send. `/mcp` itself and every path under `/mcp/` behave the same; a path that only begins with the same letters, such as `/mcpx`, is sent to sign in like any other. The challenge is given on every app's server auth stands in front of, whatever the app's manifest's `mcp` holds, as on a host.

Request:

```
$ curl -si http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si http://dummy.wip.localhost:7400/mcp/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the one line `authentication required: send Authorization: Bearer <token>` ending in a newline, where `<token>` is those seven characters as written, not a value filled in.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A client whose credential auth refuses reaches an app

A client auth knows but will not let in, such as one sending a token that is revoked or expired, gets auth's 403 unchanged, under `/mcp` and everywhere else. Only a 401 sends a browser to sign in or draws the MCP challenge.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 403 Forbidden
```

Status 403. The body is not fixed.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 403 for `ikp_<token>`.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A client forges the identity headers

An app trusts `X-User-Id` and `X-User-Email` because only auth's answer can set them. Whatever a client sends in those headers itself is dropped before the request reaches the app or auth's `/check`.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' -H 'X-User-Id: 1' -H 'X-User-Email: boss@michaelgreenly.dev' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for that cookie, with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.
- dummy answers `GET /widgets` with 200.

Postconditions:

- Nothing has changed.
- dummy received exactly one `X-User-Id`, `7`, and exactly one `X-User-Email`, `me@michaelgreenly.dev`; neither `1` nor `boss@michaelgreenly.dev` reached dummy or auth's `/check`.

## A client sends its own request id

nginx gives every request it passes to an app an `X-Request-Id` of its own making, so one request can be followed through the logs. A client's own `X-Request-Id` is replaced, never passed on.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' -H 'X-Request-Id: mine' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for that cookie.
- dummy answers `GET /widgets` with 200.

Postconditions:

- Nothing has changed.
- dummy received exactly one `X-Request-Id`, an id nginx made for this request, not `mine`.
- auth's `/check` subrequest carried the same `X-Request-Id` dummy received.

## A browser that has not signed in reaches auth itself

auth is where a browser signs in, so its own name is never put to `/check`: every request to it goes straight to auth.

Request:

```
$ curl -si http://auth.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is auth's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header.
- auth answers `GET /` with 200.

Postconditions:

- Nothing has changed.
- auth received `GET /` on its socket; no `/check` subrequest was made.

## A client asks for auth's `/check` from outside

`/check` is for nginx's subrequests alone. Asked for directly at auth's name, nginx answers it itself and auth never sees the request.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' http://auth.wip.localhost:7400/check
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.

Postconditions:

- Nothing has changed. Nothing reached auth's socket.

## A browser reaches an app in a sandbox without auth

A checkout with no app named `auth` has no authenticator, as a host without one has none: no request is put to a `/check`, no browser is sent to sign in, and every request goes straight to its app. Identity headers a client sends are still dropped, so the app is given no user, and an app that serves only a request carrying a user, as dummy does, answers with its own error.

Request:

```
$ curl -si -H 'X-User-Id: 1' -H 'X-User-Email: boss@michaelgreenly.dev' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 500 Internal Server Error
```

Status 500. This is dummy's own answer, not nginx's: the body is the one line `identity header missing`.

Preconditions:

- The checkout holds `dummy` and no `auth`, and `wip` is up from an `up` run with it so; `sandbox-wip-dummy.service` is active.
- dummy answers any request that carries no `X-User-Id` with 500 and the body `identity header missing`, and writes `dummy: request <id>: X-User-Id is missing` to its stderr, where `<id>` is the `X-Request-Id` it received.

Postconditions:

- Nothing has changed. No `/check` subrequest was made, and no redirect to sign in was sent.
- dummy received `GET /widgets` with no `X-User-Id` and no `X-User-Email` header, and with an `X-Request-Id` nginx made; neither `1` nor `boss@michaelgreenly.dev` reached dummy.
- dummy's journal holds `dummy: request <id>: X-User-Id is missing`, naming that same id.

## A browser asks for the bare localhost address in a sandbox without auth

Bare `localhost:7400` exists only to hand a browser back from Google to auth. A sandbox without `auth` has no sign-in to return to, so nginx answers that host as it answers a host the sandbox does not serve, whatever the path.

Request:

```
$ curl -si 'http://localhost:7400/login/google/callback?code=4/0Ab&state=xyz'
```

```
$ curl -si http://localhost:7400/
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed.

Preconditions:

- The checkout holds `dummy` and no `auth`, and `wip` is up from an `up` run with it so.

Postconditions:

- Nothing has changed. No app received the request, and no redirect was sent.

## A browser sends an app more than the app's own nginx configuration allows

An app may ship nginx configuration of its own, and the sandbox applies it as a host does: the app's `etc/nginx.conf`, and any other file in its `etc/` whose name begins `nginx.conf`, read from the checkout as it is on disk, is included in the server that answers for that app, ahead of the sandbox's own locations, so its directives apply to every request for the app's host. It is included whether or not the app is gated, auth's own server included, and it reaches every name the app answers at, the sandbox's bare name too when the app is the default. It applies to that app alone: the same request to auth's name is passed to auth. An app that ships no such file gets nothing extra. Here dummy limits the size of a request body.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' --data-binary @two-kib.txt http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 413 Request Entity Too Large
```

Status 413. The body is not fixed.

Preconditions:

- `dummy/etc/nginx.conf` holds the one line `client_max_body_size 1k;`, and `wip` is up from an `up` run with the checkout in that state; both services are active.
- `two-kib.txt` holds 2,048 bytes.
- auth's `/check` answers 200 for that cookie.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## Another machine on the network tries to reach the sandbox

The sandbox serves the developer's own machine and nothing else: its nginx listens on `127.0.0.1` only, so the port is closed on every other address the machine has. The connection is refused before any HTTP is exchanged, so there is no response and no status; curl reports the failure itself. The command runs on another machine on the same network, where `192.168.1.20` is the developer's machine; a request to `192.168.1.20:7400` made on the developer's own machine fails the same way. `-S` makes curl print its error despite `-s`. The milliseconds vary, and curl's wording after the port varies with its version; `curl: (7) Failed to connect to 192.168.1.20 port 7400` is fixed.

Command:

```
$ curl -sSi http://192.168.1.20:7400/
```

Output:

```
curl: (7) Failed to connect to 192.168.1.20 port 7400 after <ms> ms: Couldn't connect to server
```

Exits 7. The line is on stderr; stdout is empty.

Preconditions:

- `wip` is up with `auth` and `dummy`.
- The developer's machine has the address `192.168.1.20` on its network, and no other program listens on port `7400` there.

Postconditions:

- Nothing has changed. No app received anything.
