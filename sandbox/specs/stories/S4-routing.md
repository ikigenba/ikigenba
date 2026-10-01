# Stories — routing

Every request to a sandbox arrives at its own nginx, `sandbox-wip-nginx.service`, a user unit run as the developer that `up` starts and configures, listening on `127.0.0.1:7400` in plain HTTP and proxying to each app's socket. It routes as the platform's nginx does on a host, except that it drops client-supplied identity headers on every host, so an app behaves the same in the sandbox as deployed: by host name, one name per app, with `auth`, when the checkout holds it, standing between every other app and the outside. Browsers and curl resolve every name under `localhost` to the loopback address, so the names below need no DNS. The stories share one setting unless they say otherwise: the sandbox `wip` is up on port `7400` from the worktree `/home/me/src/ikigenba/wip`, its apps are `auth` and `dummy`, and neither is the default app. When `auth` is present, every request for another app is first put to auth's `/check` as an internal subrequest carrying the request's `Cookie` and `Authorization` headers and no body; what `/check` answers decides the request. Every request nginx passes to an app carries `Host` as the client sent it, `X-Real-IP`, `X-Forwarded-For`, `X-Forwarded-Proto: http`, and an `X-Request-Id` nginx made for that request, never the client's own; the `X-User-Id` and `X-User-Email` an app receives are only ever the ones auth gave, never the client's. nginx drops a client's own `X-User-Id` and `X-User-Email` on every host it serves, auth's own name included, and in a sandbox without `auth` as well, where an app sees no user at all.

## A browser reaches an app at its own name

Each app answers at `http://<app>.wip.localhost:7400`, and nginx passes the request to that app's socket.

Request:

```
$ curl -si -H 'Cookie: session=<session>' 'http://dummy.wip.localhost:7400/widgets?page=2'
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

## A browser reaches the default app at the sandbox's bare name

The app whose manifest sets `default = true` answers at `http://wip.localhost:7400` as well as at its own name.

Request:

```
$ curl -si -H 'Cookie: session=<session>' http://wip.localhost:7400/widgets
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

Google accepts a sign-in redirect to `http://localhost:<port>/...` but never to a name under `localhost`, so auth asks Google to send the browser back to `http://localhost:7400`, the origin it finds in `IKIGENBA_CALLBACK_URL`. nginx answers every request whose host is bare `localhost:7400`, whatever its path, by sending the browser on to the same path and query at auth's own name.

Request:

```
$ curl -si 'http://localhost:7400/callback?code=4/0Ab&state=xyz'
```

```
$ curl -si http://localhost:7400/
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/callback?code=4/0Ab&state=xyz
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
$ curl -si -H 'Cookie: session=<session>' http://dummy.wip.localhost:7400/widgets
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

A program that cannot sign in through a browser sends the bearer token a human created at auth. auth's `/check` sees the `Authorization` header and decides the request exactly as it decides a session.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for `ikp_<token>`, with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.
- dummy answers `GET /mcp` with 200.

Postconditions:

- Nothing has changed.
- auth's `/check` received the request's `Authorization` header and no body.
- dummy received `GET /mcp` with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.

## An MCP client reaches an app without a credential

An MCP client is a program, not a browser, and cannot follow a redirect to a sign-in page. So under `/mcp`, a 401 from `/check` is answered `401` by nginx itself, with a challenge naming the scheme to use and one line saying what to send. `/mcp` itself and every path under `/mcp/` behave the same; a path that only begins with the same letters, such as `/mcpx`, is sent to sign in like any other.

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
$ curl -si -H 'Cookie: session=<session>' -H 'X-User-Id: 1' -H 'X-User-Email: boss@michaelgreenly.dev' http://dummy.wip.localhost:7400/widgets
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
$ curl -si -H 'Cookie: session=<session>' -H 'X-Request-Id: mine' http://dummy.wip.localhost:7400/widgets
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
$ curl -si -H 'Cookie: session=<session>' http://auth.wip.localhost:7400/check
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

A checkout with no app named `auth` has no authenticator: no request is put to a `/check`, and every request goes straight to its app. Identity headers a client sends are still dropped, so an app in such a sandbox sees no user at all.

Request:

```
$ curl -si -H 'X-User-Id: 1' -H 'X-User-Email: boss@michaelgreenly.dev' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- The checkout holds `dummy` and no `auth`, and `wip` is up from an `up` run with it so.
- dummy answers `GET /widgets` with 200.

Postconditions:

- Nothing has changed.
- dummy received `GET /widgets` with no `X-User-Id` and no `X-User-Email` header, and with an `X-Request-Id` nginx made.

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
