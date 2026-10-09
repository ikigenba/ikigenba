# Stories — routing

Every request to a sandbox arrives at its own nginx, `sandbox-wip-nginx.service`, a user unit run as the developer that `up` starts and configures, listening on `127.0.0.1:7400` in plain HTTP and proxying to each app's socket. It routes as the platform's nginx does on a host, except that it drops client-supplied identity headers on every host, so an app behaves the same in the sandbox as deployed: by host name, one name per app, with `auth`, when the checkout holds it, standing between every other app and the outside. Browsers and curl resolve every name under `localhost` to the loopback address, so the names below need no DNS. The stories share one setting unless they say otherwise: the sandbox `wip` is up on port `7400` from the worktree `/home/me/src/ikigenba/wip`, its apps are `auth` and `dummy`, neither is the default app, and neither manifest sets `guests = true`. When `auth` is present, every request for an app other than `auth`, except an `OPTIONS` request, is first put to auth's `/check` as an internal subrequest carrying the request's `Cookie` and `Authorization` headers and no body, and naming the request it decides in three headers of nginx's own making, never the client's: `X-Original-Method`, its method; `X-Original-Host`, its host name, lowercased and without the port; and `X-Original-URI`, its path and query exactly as the client sent them. What `/check` (or, on the general paths of an app that welcomes guests, `/check/open`) answers decides the request. On every such app's server, whatever its manifest's `mcp` holds, a 401 from `/check` under `/mcp` draws a bearer challenge in place of the sign-in redirect and a 403 from `/check` there draws an `invalid_token` bearer challenge in its place, each naming the MCP gateway's protected-resource metadata at the sandbox's own `mcp` name, as on a host; a 401 from `/check` under `/api`, the path reserved across the suite for programs calling an app, draws a bearer challenge naming no metadata in place of the sign-in redirect, and a 403 there reaches the client unchanged; and a 401 on a path of git's smart HTTP protocol, one ending `/info/refs`, `/git-upload-pack`, or `/git-receive-pack`, draws a Basic challenge in its place. On the server of an app whose manifest sets `guests = true`, every other path puts the same subrequest to auth's `/check/open` instead, which answers as `/check` does except that where `/check` would answer 401 it answers 200 with no `X-User-Id` and no `X-User-Email`, so no 401 arises there and no browser is sent to sign in; `/mcp`, `/api`, every path under either, and git's three paths keep `/check` and their challenges. In a sandbox without `auth`, `/api` is passed to the app as `/mcp` is. Every request nginx passes to an app carries `Host` as the client sent it, `X-Real-IP`, `X-Forwarded-For`, `X-Forwarded-Proto: http`, and an `X-Request-Id` nginx made for that request, never the client's own; the `X-User-Id` and `X-User-Email` an app receives are only ever the ones auth gave, never the client's. nginx drops a client's own `X-User-Id` and `X-User-Email` on every host it serves, auth's own name included, and in a sandbox without `auth` as well, where an app is given no user at all. Each app's server also carries the app's own nginx configuration, when the app ships one, as a host's does. nginx also answers for cross-origin calls, the same way on every name that serves an app: each app's own name, auth's included, the sandbox's bare name when an app is the default, and in a sandbox without `auth` as well; the bare name when no app is the default, bare `localhost:7400` and a host the sandbox does not serve serve no app, so they carry no `Access-Control-` header and no `Vary: Origin`, and an `OPTIONS` request there is answered as any other request is. It answers every `OPTIONS` request there `204` itself, before any `/check` subrequest, so no app ever receives one. The one origin it allows is the one the sandbox serves `sites` at, `http://sites.<sandbox>.localhost:<port>`, here `http://sites.wip.localhost:7400`, whether or not the checkout holds `sites`. When a request's `Origin` is exactly that origin, the response carries `Access-Control-Allow-Origin` naming it, never `*`, and `Access-Control-Allow-Credentials: true`; an answer to `OPTIONS` adds `Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS`, `Access-Control-Allow-Headers: Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID` and `Access-Control-Max-Age: 600`, and every other response adds `Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate`. They are on every response from such a name, whoever made it: the app's own, nginx's challenges, the sign-in redirect, and a 403 from auth passed through. Any other `Origin`, or none, draws no `Access-Control-` header at all. Every response from such a name carries `Vary: Origin`. An app refuses a call whose `Origin` names another host, so when nginx passes a request on to an app other than `auth`, or to auth's `/check` on its behalf, it leaves out an `Origin` that is exactly that origin and passes any other `Origin` on unchanged, for the app to judge. auth's own server passes even that origin on, because auth checks it to let any app in the sandbox, `sites` included, sign its user out.

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

An MCP client is a program, not a browser, and cannot follow a redirect to a sign-in page. So under `/mcp`, a 401 from `/check` is answered `401` by nginx itself, with a challenge naming the scheme to use and where to learn how to get a token — the MCP gateway's protected-resource metadata, at the sandbox's own `mcp` name and port whichever app was asked and whether or not the checkout holds `mcp` — and one line saying what to send. `/mcp` itself and every path under `/mcp/` behave the same; a path that only begins with the same letters, such as `/mcpx`, is sent to sign in like any other. The challenge is given on every app's server auth stands in front of, whatever the app's manifest's `mcp` holds, as on a host.

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
WWW-Authenticate: Bearer realm="ikigenba", resource_metadata="http://mcp.wip.localhost:7400/.well-known/oauth-protected-resource"
```

Status 401. The body is the one line `authentication required: send Authorization: Bearer <token>` ending in a newline, where `<token>` is those seven characters as written, not a value filled in.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A git client reaches an app without a credential

git sends no credential until the server challenges it, and the challenge it understands is `401` with `WWW-Authenticate: Basic`; it cannot follow a redirect to a sign-in page. So on a path ending `/info/refs`, `/git-upload-pack`, or `/git-receive-pack`, the three a clone, fetch, or push asks for, a 401 from `/check` is answered `401` by nginx itself with a Basic challenge and one line saying what to send. git then asks its credential helper and sends the token as the password. The match is on the path's end alone, whatever comes before it; a path such as `/notes.git/info/refsx` is sent to sign in like any other. The challenge is given on every app's server auth stands in front of, whatever the app's manifest holds, as on a host.

Request:

```
$ curl -si 'http://dummy.wip.localhost:7400/notes.git/info/refs?service=git-upload-pack'
```

```
$ curl -si -X POST http://dummy.wip.localhost:7400/notes.git/git-receive-pack
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Basic realm="ikigenba"
```

Status 401. The body is the one line `authentication required: send your token as the password` ending in a newline. `/notes.git/git-upload-pack` answers the same.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A git client sends its token as the password

Once challenged, git sends `Authorization: Basic` with a username and the token as the password. auth's `/check` sees the `Authorization` header and decides the request as it decides a bearer token; the request then reaches the app with the identity auth gave, as any admitted request does.

Request:

```
$ curl -si -u 'git:ikp_<token>' 'http://dummy.wip.localhost:7400/notes.git/info/refs?service=git-upload-pack'
```

Response: not fixed here; it is dummy's answer to the request.

Status is whatever dummy answers; nginx adds no status of its own.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` admits a Basic credential whose password is `ikp_<token>`, whatever the username.

Postconditions:

- Nothing has changed beyond what dummy itself does.
- auth's `/check` received the request's `Authorization` header and no body, with `X-Original-Method: GET`, `X-Original-Host: dummy.wip.localhost` and `X-Original-URI: /notes.git/info/refs?service=git-upload-pack`.
- dummy received `GET /notes.git/info/refs?service=git-upload-pack` with `X-User-Id` and `X-User-Email` set from auth's answer.

## An MCP client whose token auth refuses reaches an app

A client auth knows but will not let in, such as one sending a token that is revoked, expired or issued for another host, is told under `/mcp` that its token is no good and where to get another. An MCP client starts signing in again only on a 401, so nginx answers auth's 403 there with a `401` whose bearer challenge says `invalid_token` and names the MCP gateway's protected-resource metadata, with the same one line as a request with no credential. An expired or revoked token so heals itself: the client's next call prompts its user to sign in. `/mcp` itself and every path under `/mcp/` behave the same, on every app's server auth stands in front of, as on a host.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/mcp/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer error="invalid_token", resource_metadata="http://mcp.wip.localhost:7400/.well-known/oauth-protected-resource"
```

Status 401. The body is the one line `authentication required: send Authorization: Bearer <token>` ending in a newline, where `<token>` is those seven characters as written, not a value filled in.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 403 for `ikp_<token>`.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket; auth's `/check` itself answered 403, and only nginx's answer to the client changed.

## A client reaches an app's `/api` without a credential

`/api` is reserved across the suite for programs that call an app, such as a page's script or an agent, rather than a browser that can be sent to sign in. So under `/api`, a 401 from `/check` is answered `401` by nginx itself, never with a redirect to sign in, with a bearer challenge that names no metadata and one line saying what to do. `/api` itself and every path under `/api/` behave the same; a path that only begins with the same letters, such as `/apix`, is sent to sign in like any other. The challenge is given on every app's server auth stands in front of, whatever the app's manifest holds, as on a host.

Request:

```
$ curl -si http://dummy.wip.localhost:7400/api
```

```
$ curl -si http://dummy.wip.localhost:7400/api/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the one line `authentication required: sign in or send Authorization: Bearer <token>` ending in a newline, where `<token>` is those seven characters as written, not a value filled in. The response carries no `Location` header.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## An agent reaches an app's `/api` with a bearer token

A request under `/api` that auth admits is passed to the app like a request for any other path, with the identity auth gave.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/api/widgets
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for `ikp_<token>`, with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.
- dummy answers `GET /api/widgets` with 200.

Postconditions:

- Nothing has changed.
- auth's `/check` received the request's `Authorization` header and no body, with `X-Original-Method: GET`, `X-Original-Host: dummy.wip.localhost` and `X-Original-URI: /api/widgets`.
- dummy received `GET /api/widgets` with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.

## A client whose credential auth refuses reaches an app

Outside `/mcp`, a client auth knows but will not let in, such as one sending a token that is revoked or expired, gets auth's 403 unchanged, on an app's general paths, under `/api`, and on git's. Only a 401 sends a browser to sign in or draws the git challenge or the `/api` bearer challenge.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/widgets
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/api/<anything>
```

```
$ curl -si -u 'git:ikp_<token>' 'http://dummy.wip.localhost:7400/notes.git/info/refs?service=git-upload-pack'
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

## A browser that has not signed in reaches an app that welcomes guests

An app whose manifest sets `guests = true` serves visitors who bring no credential. On its general paths nginx asks auth's `/check/open` in place of `/check`, and `/check/open` answers 200 with no `X-User-Id` and no `X-User-Email` where `/check` would answer 401. The request passes to the app with no user, so the app serves a guest; nothing sends the browser to sign in.

Request:

```
$ curl -si 'http://dummy.wip.localhost:7400/widgets?page=2'
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- dummy's manifest sets `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check/open` answers 200 with no `X-User-Id` and no `X-User-Email`.
- dummy answers `GET /widgets?page=2` with 200 when the request carries no user.

Postconditions:

- Nothing has changed.
- auth's `/check/open` received `X-Original-Method: GET`, `X-Original-Host: dummy.wip.localhost` and `X-Original-URI: /widgets?page=2`; no `/check` subrequest was made.
- dummy received `GET /widgets?page=2` with no `X-User-Id` and no `X-User-Email` header, and with an `X-Request-Id` nginx made.

## A signed-in browser reaches an app that welcomes guests as its user

An app that welcomes guests still knows a user who brings a credential. auth's `/check/open` answers 200 for the browser's session and says who the user is, as `/check` does, and nginx sets those two headers on the request it passes to the app.

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

- dummy's manifest sets `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- auth's `/check/open` answers 200 for that cookie, with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.
- dummy answers `GET /widgets` with 200.

Postconditions:

- Nothing has changed.
- auth's `/check/open` received the request's `Cookie` header and no body; no `/check` subrequest was made.
- dummy received `GET /widgets` with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.

## A client whose credential auth refuses reaches an app that welcomes guests

Welcoming guests admits a request `/check` would answer 401, not one auth refuses with 403. A client sending a token that is revoked or expired gets auth's 403 from `/check/open` unchanged on the app's general paths.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 403 Forbidden
```

Status 403. The body is not fixed.

Preconditions:

- dummy's manifest sets `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- auth's `/check` and `/check/open` each answer 403 for `ikp_<token>`.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.
- The request was put to `/check/open`, not `/check`.
- Under `/mcp` the same token is put to `/check`, and its 403 draws the `invalid_token` 401 of the `MCP client whose token auth refuses reaches an app` story, exactly as on an app that does not welcome guests.
- Under `/api` the same token is put to `/check`, and its 403 reaches the client unchanged, as on an app that does not welcome guests.

## An MCP client reaches an app that welcomes guests without a credential

Welcoming guests opens an app's general paths only. Under `/mcp` the request is still put to `/check`, and its 401 still draws the bearer challenge, naming the same metadata, as on any other app's server.

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
WWW-Authenticate: Bearer realm="ikigenba", resource_metadata="http://mcp.wip.localhost:7400/.well-known/oauth-protected-resource"
```

Status 401. The body is the one line `authentication required: send Authorization: Bearer <token>` ending in a newline, where `<token>` is those seven characters as written, not a value filled in.

Preconditions:

- dummy's manifest sets `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket, and no `/check/open` subrequest was made.

## A client reaches `/api` on an app that welcomes guests without a credential

Welcoming guests opens an app's general paths only. Under `/api` the request is still put to `/check`, and its 401 still draws the bearer challenge, as on any other app's server.

Request:

```
$ curl -si http://dummy.wip.localhost:7400/api
```

```
$ curl -si http://dummy.wip.localhost:7400/api/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the one line `authentication required: sign in or send Authorization: Bearer <token>` ending in a newline, where `<token>` is those seven characters as written, not a value filled in.

Preconditions:

- dummy's manifest sets `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket, and no `/check/open` subrequest was made.

## A git client reaches an app that welcomes guests without a credential

On a path ending `/info/refs`, `/git-upload-pack`, or `/git-receive-pack` the request is still put to `/check`, and its 401 still draws the Basic challenge, as on any other app's server.

Request:

```
$ curl -si 'http://dummy.wip.localhost:7400/notes.git/info/refs?service=git-upload-pack'
```

```
$ curl -si -X POST http://dummy.wip.localhost:7400/notes.git/git-receive-pack
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Basic realm="ikigenba"
```

Status 401. The body is the one line `authentication required: send your token as the password` ending in a newline. `/notes.git/git-upload-pack` answers the same.

Preconditions:

- dummy's manifest sets `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket, and no `/check/open` subrequest was made.

## A client without a credential forges the identity headers at an app that welcomes guests

A guest reaches the app with no user, and cannot make one up: whatever a client sends in `X-User-Id` and `X-User-Email` itself is dropped before the request reaches the app or auth's `/check/open`.

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

- dummy's manifest sets `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check/open` answers 200 with no `X-User-Id` and no `X-User-Email`.
- dummy answers `GET /widgets` with 200 when the request carries no user.

Postconditions:

- Nothing has changed.
- dummy received `GET /widgets` with no `X-User-Id` and no `X-User-Email` header; neither `1` nor `boss@michaelgreenly.dev` reached dummy or auth's `/check/open`.

## A browser that has not signed in reaches the default app that welcomes guests at the sandbox's bare name

The default app's bare name is served as its own name is, so when the default app welcomes guests, `http://wip.localhost:7400` welcomes them too.

Request:

```
$ curl -si http://wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The body is dummy's own answer.

Preconditions:

- dummy's manifest sets `default = true` and `guests = true`, and `wip` is up from an `up` run with it so; both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check/open` answers 200 with no `X-User-Id` and no `X-User-Email`.
- dummy answers `GET /widgets` with 200 when the request carries no user.

Postconditions:

- Nothing has changed.
- auth's `/check/open` received `X-Original-Method: GET`, `X-Original-Host: wip.localhost` and `X-Original-URI: /widgets`; no `/check` subrequest was made.
- dummy received `GET /widgets` on its socket, with `Host: wip.localhost:7400`, no `X-User-Id` and no `X-User-Email` header, and an `X-Request-Id` nginx made.

## A browser that has not signed in reaches auth itself

auth is where a browser signs in, so its own name is never put to `/check`: every request to it but an `OPTIONS` request goes straight to auth.

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

## A client asks for auth's `/check/open` from outside

`/check/open` is for nginx's subrequests alone, as `/check` is. Asked for directly at auth's name, nginx answers it itself and auth never sees the request.

Request:

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' http://auth.wip.localhost:7400/check/open
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

## A page on the sites origin asks before calling an app

A page `sites` publishes at `http://sites.wip.localhost:7400` calls other apps from the browser with the suite's session cookie. Before a call that is not a simple one, the browser first sends an `OPTIONS` request naming its origin and the method and headers it means to send, and makes the call only if the answer allows it. nginx answers that request itself, without asking auth and without passing it to the app, and the answer is the same whatever path, method and headers it names. The checkout here holds no `sites`; the origin is allowed all the same.

Request:

```
$ curl -si -X OPTIONS -H 'Origin: http://sites.wip.localhost:7400' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-protocol-version' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -X OPTIONS -H 'Origin: http://sites.wip.localhost:7400' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-protocol-version' http://dummy.wip.localhost:7400/api/<anything>
```

```
$ curl -si -X OPTIONS -H 'Origin: http://sites.wip.localhost:7400' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-protocol-version' http://auth.wip.localhost:7400/<anything>
```

Response:

```
HTTP/1.1 204 No Content
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID
Access-Control-Max-Age: 600
Vary: Origin
```

Status 204. The body is empty. The response carries no `Access-Control-Expose-Headers`.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active; the checkout holds no `sites`.
- The request carries no cookie and no `Authorization` header, as a browser's `OPTIONS` request never does.

Postconditions:

- Nothing has changed. No `/check` subrequest was made, and nothing reached dummy's or auth's socket.

## A page on the sites origin calls an app's `/mcp` as its signed-in user

The browser sends the session cookie with the call, and nginx decides the request as it decides any other. The answer names the page's origin and allows credentials, so the browser hands the response to the page, and it exposes `Mcp-Session-Id`, so the page can carry the MCP session on to its next call.

Request:

```
$ curl -si -H 'Origin: http://sites.wip.localhost:7400' -H 'Cookie: ikigenba_session=<session>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://dummy.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status 200. The body is dummy's own answer. The response carries no `Access-Control-Allow-Methods`, no `Access-Control-Allow-Headers` and no `Access-Control-Max-Age`.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for that cookie, with `X-User-Id: 7` and `X-User-Email: me@michaelgreenly.dev`.
- dummy answers that `POST /mcp` with 200.

Postconditions:

- Nothing has changed.
- auth's `/check` received the request's `Cookie` header and no body, with `X-Original-Method: POST`, `X-Original-Host: dummy.wip.localhost` and `X-Original-URI: /mcp`.
- dummy received `POST /mcp` with the request's body, `X-User-Id: 7`, `X-User-Email: me@michaelgreenly.dev` and no `Origin` header, so it takes the call as it takes one from its own pages; auth's `/check` received no `Origin` header either.

## A page on the sites origin calls an app without a session

nginx's own challenge carries the same headers as an app's answer, so the page can read the `401` and its `WWW-Authenticate` and send its user to sign in, instead of meeting an opaque network error.

Request:

```
$ curl -si -H 'Origin: http://sites.wip.localhost:7400' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -H 'Origin: http://sites.wip.localhost:7400' http://dummy.wip.localhost:7400/api/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba", resource_metadata="http://mcp.wip.localhost:7400/.well-known/oauth-protected-resource"
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status 401. The body is the one line `authentication required: send Authorization: Bearer <token>` ending in a newline. For the second form `WWW-Authenticate` is `Bearer realm="ikigenba"` and the body is the one line `authentication required: sign in or send Authorization: Bearer <token>` ending in a newline; the `Access-Control-` headers and `Vary` are the same. In both, `<token>` is those seven characters as written.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A page on the sites origin reaches an app's page without a session

Off `/mcp`, `/api` and git's paths a 401 still sends the browser to sign in, and the redirect carries the same headers as any other answer to that origin.

Request:

```
$ curl -si -H 'Origin: http://sites.wip.localhost:7400' 'http://dummy.wip.localhost:7400/widgets?page=2'
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/?return=http://dummy.wip.localhost:7400/widgets?page=2
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status 302. The body is not fixed.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A page on the sites origin calls an app with a session auth refuses

auth's 403, passed through unchanged, carries the same headers as any other answer to that origin, and so does the `invalid_token` challenge nginx gives in its place under `/mcp`.

Request:

```
$ curl -si -H 'Origin: http://sites.wip.localhost:7400' -H 'Cookie: ikigenba_session=<session>' http://dummy.wip.localhost:7400/api/<anything>
```

```
$ curl -si -H 'Origin: http://sites.wip.localhost:7400' -H 'Cookie: ikigenba_session=<session>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://dummy.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 403 Forbidden
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status 403. The body is not fixed. For the second form the response is the `401` with the `invalid_token` bearer challenge and the body of the `MCP client whose token auth refuses reaches an app` story, carrying the same `Access-Control-` headers and `Vary`.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 403 for that cookie.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A page on the sites origin signs its user out

The banner on `sites`' own pages signs the user out with a form that POSTs to auth's `/logout`, as every app's banner does. auth lets any app in the sandbox sign its user out and decides that from the request's `Origin`, so auth's own server passes the sites origin on to auth unchanged.

Request:

```
$ curl -si -X POST -H 'Origin: http://sites.wip.localhost:7400' -H 'Cookie: ikigenba_session=<session>' http://auth.wip.localhost:7400/logout
```

Response:

```
HTTP/1.1 <status>
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

The status and body are auth's own answer.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.

Postconditions:

- No `/check` subrequest was made.
- auth received `POST /logout` with `Origin: http://sites.wip.localhost:7400` unchanged, as it receives any request's `Origin` on its own server.

## A page on another origin asks before calling an app

Only the sites origin is allowed, matched exactly, letter case included. A page served from any other origin, another app's own name included, and a request naming no origin at all, still get nginx's `204`, but it allows nothing, so a browser does not make the call.

Request:

```
$ curl -si -X OPTIONS -H 'Origin: http://dummy.wip.localhost:7400' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-protocol-version' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -X OPTIONS -H 'Origin: https://sites.wip.localhost:7400' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-protocol-version' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -X OPTIONS -H 'Origin: HTTP://SITES.WIP.LOCALHOST:7400' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-protocol-version' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -X OPTIONS http://dummy.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 204 No Content
Vary: Origin
```

Status 204. The body is empty. The response carries no header whose name begins `Access-Control-`.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- The request carries no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed. No `/check` subrequest was made, and nothing reached dummy's socket.

## A page on another origin calls an app

nginx refuses no request for its origin: it decides it as it decides any other, and only leaves out the headers that would let a browser hand the answer to the page.

Request:

```
$ curl -si -H 'Origin: http://dummy.wip.localhost:7400' -H 'Cookie: ikigenba_session=<session>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -H 'Origin: HTTP://SITES.WIP.LOCALHOST:7400' -H 'Cookie: ikigenba_session=<session>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://dummy.wip.localhost:7400/mcp
```

```
$ curl -si -H 'Cookie: ikigenba_session=<session>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://dummy.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Vary: Origin
```

Status 200. The body is dummy's own answer. The response carries no header whose name begins `Access-Control-`.

Preconditions:

- `wip` is up with `auth` and `dummy`, and both services are active.
- auth's `/check` answers 200 for that cookie.
- dummy answers that `POST /mcp` with 200.

Postconditions:

- Nothing has changed.
- dummy received `POST /mcp` with the request's body, as it does from the sites origin, but with the request's `Origin` header unchanged, or none for the third request, for dummy to judge.

## A browser reaches an app in a sandbox without auth

A checkout with no app named `auth` has no authenticator, as a host without one has none: no request is put to a `/check`, no browser is sent to sign in, and every request but an `OPTIONS` request goes straight to its app. Identity headers a client sends are still dropped, so the app is given no user, and an app that serves only a request carrying a user, as dummy does, answers with its own error.

Request:

```
$ curl -si -H 'X-User-Id: 1' -H 'X-User-Email: boss@michaelgreenly.dev' http://dummy.wip.localhost:7400/widgets
```

Response:

```
HTTP/1.1 500 Internal Server Error
```

Status 500. This is dummy's own answer, not nginx's: the body is appkit's `identity.MissingBody`.

Preconditions:

- The checkout holds `dummy` and no `auth`, and `wip` is up from an `up` run with it so; `sandbox-wip-dummy.service` is active.
- dummy answers any request that carries no `X-User-Id` with 500 and the body appkit's `identity.MissingBody`, and writes to its stderr a diagnostic naming the missing header and the `X-Request-Id` it received.

Postconditions:

- Nothing has changed. No `/check` subrequest was made, and no redirect to sign in was sent.
- dummy received `GET /widgets` with no `X-User-Id` and no `X-User-Email` header, and with an `X-Request-Id` nginx made; neither `1` nor `boss@michaelgreenly.dev` reached dummy.
- dummy's journal holds a diagnostic naming the missing header and that same id.

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

## A page on the sites origin asks before calling an app in a sandbox without auth

A sandbox without `auth` answers cross-origin calls as one with it does: nginx answers every `OPTIONS` request itself, and allows the sites origin alone.

Request:

```
$ curl -si -X OPTIONS -H 'Origin: http://sites.wip.localhost:7400' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-protocol-version' http://dummy.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 204 No Content
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID
Access-Control-Max-Age: 600
Vary: Origin
```

Status 204. The body is empty.

Preconditions:

- The checkout holds `dummy` and no `auth`, and `wip` is up from an `up` run with it so; `sandbox-wip-dummy.service` is active.

Postconditions:

- Nothing has changed. Nothing reached dummy's socket.

## A page on the sites origin calls an app's `/api` in a sandbox without auth

With no authenticator, `/api` is passed straight to the app as every other path is, and the app's answer carries the headers that let the page read it.

Request:

```
$ curl -si -H 'Origin: http://sites.wip.localhost:7400' http://dummy.wip.localhost:7400/api/widgets
```

Response:

```
HTTP/1.1 500 Internal Server Error
Access-Control-Allow-Origin: http://sites.wip.localhost:7400
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status 500. This is dummy's own answer, not nginx's: the body is appkit's `identity.MissingBody`.

Preconditions:

- The checkout holds `dummy` and no `auth`, and `wip` is up from an `up` run with it so; `sandbox-wip-dummy.service` is active.
- dummy answers any request that carries no `X-User-Id` with 500 and the body appkit's `identity.MissingBody`.

Postconditions:

- Nothing has changed. No `/check` subrequest was made, and no challenge or redirect to sign in was sent.
- dummy received `GET /api/widgets` with no `X-User-Id`, no `X-User-Email` and no `Origin` header.

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
