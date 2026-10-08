# Stories — nginx

Every request to the host arrives at nginx, and opsctl owns exactly one file
under `/etc/nginx`: `/etc/nginx/conf.d/ikigenba.conf`. That file is a pure
function of the configuration store, of the release `/opt/ikigenba/current`
names, and of which apps systemd reports disabled, so it is generated rather
than edited, and it is never backed up — a restored host regenerates it.
`opsctl activate` and `opsctl rollback` regenerate it in their `nginx` step
(`S10-releases.md`).

The top-level usage gains the line `  nginx     generate the platform's nginx
configuration` under `Commands:`, and `init`'s sequence gains the step
`nginx.conf`.

A service is discovered, never registered. On a released host, one where
`/opt/ikigenba/current` exists, a service is an app the release `current`
names — a directory `/opt/ikigenba/current/<name>/` holding `bin/<name>` and
`etc/manifest.toml` — or any `/var/opt/ikigenba/<name>/` holding a `state/`;
an `/opt/<name>/` makes no service. A service is *routed* when it is an app in
the current release. One that only keeps state, a *data-only* service — what
an app the release no longer holds leaves behind — is known to the host but
gets no server block. Routed services answer at `<name>.<host.name>`, and the
one whose manifest says `default = true` also answers at `<host.name>`. These
stories are written for a released host; on a host still laid out per app
(`S07-apps.md`), with no `current`, `nginx` keeps that layout's rule: any
`/opt/<name>/` holding an `etc/` is a service, routed when its
`/opt/<name>/etc/manifest.toml` names its `app`, and its block includes
`/opt/<name>/etc/nginx.conf*`.

A routed app is *disabled* when systemd reports its socket unit,
`ikigenba-<name>.socket`, disabled — the state `opsctl disable` leaves it in
(`S07-apps.md`). Nothing else records it: nginx asks systemd, the way `status`
does. A disabled app keeps its block and every name it answers at, but the
block answers `503` to every request but a preflight, which it answers `204`
as every app's block does (below), and neither includes the app's own
`etc/nginx.conf` nor proxies anywhere, so a disabled app reads as unavailable
rather than as missing. A routed service with no units at all is not
disabled; it is routed as any other.

A service's block carries the TLS frame, the app's own `etc/nginx.conf` if it
ships one, and a `location /` that proxies to the app's Unix socket,
`/run/ikigenba/<name>.sock`. No app listens on a TCP port: the socket is held
by the app's socket unit (`S07-apps.md`), readable and writable by nginx and by
no process outside the platform, so nginx is the only way in from the network.
The app's file is included first, so a longer prefix in it — static files out
of `share/`, say — wins over the proxy; the glob makes the include harmless
when there is no such file. It is included at the level of the server block,
so a directive in it that nginx allows there and does not set in a location
— a body-size limit, a proxy timeout, request buffering — reaches every
location in the block, the generated ones included, and so every request for
the app. That is how an app that takes large, slow uploads widens the limits
for itself alone; no manifest key and no configuration key does it.

Every request gets one id. nginx sets `X-Request-Id` to its own `$request_id`
on every request it proxies to an app, and on the subrequest to `auth`'s
`/check` or `/check/open`, always overwriting whatever the client sent, so the id an app sees
is nginx's and never the client's. The same id ends every line nginx writes
to its access log, in the `ikigenba` format the file declares, so an app's
diagnostic naming a request and the access-log line for it can be matched.

One routed app is set apart by its name. A routed app named `auth` is the
platform's authenticator, and its presence rewrites every other routed app's
block: each gains an `auth_request` subrequest to `auth`'s `/check`, so a
request reaches the app only once `auth` has answered it a valid session — the
*wired* shape, save for an app that serves guests (below). In a wired block the client's own `X-User-Id` and `X-User-Email`
never reach the app; nginx sets them from `auth`'s answer instead, turns a 401
into a redirect to `auth`, and passes a 403 through (save under `/mcp`,
below). The subrequest tells
`auth` which request it is checking: the client's method, host, and request
target — path and any query string — as `X-Original-Method`,
`X-Original-Host`, and `X-Original-URI`, always overwriting whatever the
client sent under those names. `auth`'s own block is left
*unwired*, and its `/check` and `/check/open` answer 404 to any public request
— each is reachable only as an internal subrequest, and the guard is there
whether or not any app serves guests. Recognition is by an app in the current
release, not the name alone: an `auth` that only keeps state under
`/var/opt/ikigenba/auth/` is not the authenticator, and with no routed `auth` on the host every block is unwired
— the fail-open frame, which is what these stories show unless one says a
routed `auth` is present.

The paths `/mcp` and `/mcp/...` are reserved across the suite for MCP, the
protocol programs rather than browsers use to call an app's tools. Every wired
block answers them with the same subrequest to `/check` and the same identity
relay as `location /`, with one difference: a request `auth` answers 401 is
not sent to sign in, since the client is not a browser that could follow the
redirect. It is answered `401` with `WWW-Authenticate: Bearer
realm="ikigenba", resource_metadata="<mcp origin>/.well-known/oauth-protected-resource"`
and the one line of plain text `authentication required: send Authorization:
Bearer <token>`, where `<token>` is literal text telling the client what to
send. A request `auth` answers 403 — a token it refuses — is answered `401`
too, with `WWW-Authenticate: Bearer error="invalid_token",
resource_metadata="<mcp origin>/.well-known/oauth-protected-resource"` and the
same line, because an MCP client starts signing in again only on a 401. The
`<mcp origin>` is the MCP gateway's, `https://mcp.<host.name>`, whichever app
was asked and whether or not an `mcp` app is routed; the document it names is
the gateway's to serve. Everywhere outside `/mcp` and `/mcp/...` a 403 from
`auth` still reaches the client unchanged. Every wired block
carries these locations whatever the app's manifest says about `mcp`: the
paths are the suite's, not an app's opt-in. Only `/mcp` itself and paths
under `/mcp/` are reserved — `/mcpx` is an ordinary path and redirects to sign
in like any other. Plain blocks on a host with no authenticator, `auth`'s own
block, and a disabled app's block are unchanged.

The paths `/api` and `/api/...` are reserved across the suite the same way,
for the HTTP interfaces apps offer to programs and to scripts on a page. Every
wired block answers them with the same subrequest to `/check` and the same
identity relay as `location /`, so a request `auth` admits, by session cookie
or by token, reaches the app with the identity `auth` gave. A request `auth`
answers 401 is not sent to sign in, since neither a program nor a page's
script can follow the redirect. It is answered `401` with `WWW-Authenticate:
Bearer realm="ikigenba"`, naming no metadata, and the one line of plain text
`authentication required: sign in or send Authorization: Bearer <token>`,
where `<token>` is literal text. A request `auth` answers 403 gets that 403
unchanged, as everywhere outside `/mcp`. Like `/mcp`, the paths are the
suite's, carried whatever the manifest says; only `/api` itself and paths
under `/api/` are reserved, so `/apix` is an ordinary path; and plain blocks,
`auth`'s own block, and a disabled app's block are unchanged.

git's smart HTTP protocol is reserved the same way. A git client sends no
credential until the server challenges it, and the challenge must be `401`
with `WWW-Authenticate: Basic`; a redirect to sign in is something git cannot
follow. So every wired block answers a path ending `/info/refs`,
`/git-upload-pack`, or `/git-receive-pack` — the three a clone, fetch, or
push asks for — with the same subrequest to `/check` and identity relay as
`location /`, except that a 401 from `auth` is answered `401` with
`WWW-Authenticate: Basic realm="ikigenba"` and the one line of plain text
`authentication required: send your token as the password`. Git then asks its
credential helper and sends the token as the Basic password, which `auth`
accepts as it accepts a bearer token. A 403 still reaches the client
unchanged. Like `/mcp`, these paths are the suite's, carried whatever the
manifest says, and absent from plain, unwired, and disabled blocks. The match
is on the path's end alone, so `/notes/info/refs` is reserved too, while
`/info/refsx` is an ordinary path.

An app that serves guests — visitors with no credential at all — says so with
`guests = true` in its manifest (`S07-apps.md`); with no `guests`, or `guests =
false`, it does not. On a host with a routed `auth`, such an app's wired block
differs from any other in two places. It gains a second internal check,
`/_ikigenba/check/open`, beside `/_ikigenba/check` and identical to it except
that it asks `auth`'s `/check/open`, which answers exactly as `/check` does
except that where `/check` would answer 401 it answers 200 with no identity.
And its `location /` asks that check instead and has no `error_page 401`, so
nothing there is sent to sign in: a guest reaches the app with no `X-User-Id`
or `X-User-Email` and with nginx's request id, a signed-in visitor reaches it
with the identity `auth` relays, and a 403 from `auth` still reaches the
client unchanged. `/mcp`, `/mcp/...`, `/api`, `/api/...`, and the git paths
are not part of it: they keep the strict `/_ikigenba/check` and their 401
challenges, exactly as on any wired block. When the app also holds the apex,
the apex is a name on that same block and is served the same way. `guests`
changes nothing else: on a host with no authenticator the app gets the plain
block every app gets, a disabled app's block answers 503 as any other, and
`auth`'s own block is the same with or without such an app.

A page that `sites` publishes is served at `https://sites.<host.name>`, and
its script calls other apps from the browser, carrying the suite's session
cookie. So every block that serves an app grants cross-origin access to that
one origin, the *allowed origin*, and to no other. It is derived from
`host.name` alone, `https://sites.sbx.ikigenba.dev` on a host named
`sbx.ikigenba.dev`, whether or not a `sites` app is routed; no configuration
key changes it. nginx decides it, and an app never sees a preflight: every
`OPTIONS` request to an app's block is answered `204` by nginx before
anything else in the block, so with no subrequest to `auth` and nothing
proxied. When the request's `Origin` is exactly the allowed origin, the
response carries `Access-Control-Allow-Origin` naming that origin, never `*`,
and `Access-Control-Allow-Credentials: true`. The `204` also carries
`Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS`,
`Access-Control-Allow-Headers: Content-Type, Accept, Mcp-Session-Id,
Mcp-Protocol-Version, Last-Event-ID`, and `Access-Control-Max-Age: 600`;
every other response carries `Access-Control-Expose-Headers: Mcp-Session-Id,
WWW-Authenticate` instead. Any other `Origin`, or none, gets no
`Access-Control-` header at all. Every response from an app's block carries
`Vary: Origin`. The headers are on every response such a block gives,
whatever its status: the app's own, nginx's 401 challenges, the 302 to sign
in, a 403 from `auth`, the 404 of `auth`'s guarded `/check`, and a disabled
app's 503. That holds for a plain block on a host with no authenticator, a
wired block, one that serves guests, `auth`'s own block, a disabled app's
block, and the apex wherever it is a name on an app's block. It holds for no
other block: the port-80 redirect, the handshake-rejecting default, and the
404 block answer as before. An app refuses a call whose `Origin` names
another host, so when nginx passes a request on to an app, or to `auth`'s
`/check` on its behalf, it leaves out an `Origin` that is exactly the allowed
origin and passes any other `Origin` on unchanged, for the app to judge.
`auth`'s own block passes even the allowed origin on, because `auth` checks it
to let any app on the space, `sites` included, sign its user out. The file
declares what these headers and that `Origin` are built from once, after the log format, on every host, a bare one included, so the
frame is the same whether or not any app is routed.

One host in the account also answers at the root domain's apex,
`ikigenba.dev`. Which host that is, and which app answers there, is a
decision made from the developer's machine (`devctl apex set`), and it
reaches the host as one configuration key:

| key | value |
|---|---|
| `host.apex` | the name of the app that answers at the apex, e.g. `crm`; unset or empty means this host does not hold the apex |

The apex hostname itself is never stored: it is `host.name` with its first
label removed, `ikigenba.dev` for a host named `sbx.ikigenba.dev`. A host name
with fewer than three labels has no parent to answer at, and a `host.apex`
set on one is refused by everything that reads it. The apex app is chosen
independently of the manifest's `default`: one app may answer at the space's
name and another at the apex, or the same app at both. When the named app is
not routed — not in the current release, or only its data left on the host — the apex answers
404 under the host's certificate until it is, so the name never falls to the
handshake-rejecting default block once the certificate carries it.

## An operator asks what `nginx` can do

Command:

```
$ opsctl nginx --help
```

```
$ opsctl nginx -h
```

Output:

```
Usage: opsctl nginx <subcommand>

Generate /etc/nginx/conf.d/ikigenba.conf from the configuration store, the
release /opt/ikigenba/current names, and which apps systemd reports disabled.
The file is generated, never edited; opsctl writes no other file under
/etc/nginx.

Subcommands:
  show   print the configuration opsctl would write
  apply  write the file, test it, and reload nginx

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none

Each app in the current release, /opt/ikigenba/current/<name>/, answers at
<name>.<host.name>, proxied to its socket /run/ikigenba/<name>.sock, and the
one whose manifest sets default answers at <host.name> as well. Its own
/opt/ikigenba/current/<name>/etc/nginx.conf, if it ships one, is included in
its server block. A service that only keeps state under
/var/opt/ikigenba/<name>/ gets no block. On a host with no
/opt/ikigenba/current, the apps are those under /opt/<name>/ instead. An
app whose socket unit systemd reports disabled keeps its names, and its block answers 503. Every proxied request
carries X-Request-Id set to nginx's own request id, which also ends its
access-log line. The app
host.apex names also answers at the parent of host.name; until that app is
routed, the parent answers 404. A routed app named auth is the authenticator:
every other app's block then requires a valid session, checked against auth's
/check, while auth's own name is not gated. Under /mcp, a request without a
valid credential, or with one auth refuses, is answered 401 naming the MCP
gateway's protected-resource metadata instead of being sent to sign in or
refused; under /api, a request without a valid credential is answered 401
with a Bearer challenge; a git smart HTTP request without a credential is
answered 401 too, with a Basic challenge so git asks for the token. An app
whose manifest sets guests admits a request without a credential elsewhere,
checked against auth's /check/open. Every app's block answers an OPTIONS
request 204 itself, and grants cross-origin access with credentials to
https://sites.<host.name> alone.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator reads the configuration a bare host would get

Before any release is activated, the file is the frame: port 80 redirects
everything to 443, an unknown name has its TLS handshake rejected outright
rather than being answered with someone else's certificate, and the host's own
name and its wildcard answer 404 under the host's certificate. The
`ikigenba` log format comes first, and every block that answers a request
logs in it; the handshake-rejecting block answers none. The maps that decide
the cross-origin headers, and the `Origin` an app is passed, follow it, built around the allowed origin
`https://sites.sbx.ikigenba.dev`; no block here serves an app, so none uses
them yet.

Command:

```
$ sudo opsctl nginx show
```

Output:

```
# Generated by opsctl. Do not edit; run 'opsctl nginx apply'.

log_format ikigenba '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" $request_id';

map $http_origin $ikigenba_cors_origin {
    default                               "";
    "~^https://sites\.sbx\.ikigenba\.dev$" $http_origin;
}

map $ikigenba_cors_origin $ikigenba_cors_credentials {
    ""      "";
    default "true";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_methods {
    default        "";
    "OPTIONS true" "GET, POST, PUT, PATCH, DELETE, OPTIONS";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_headers {
    default        "";
    "OPTIONS true" "Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_max_age {
    default        "";
    "OPTIONS true" "600";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_expose {
    default        "";
    "OPTIONS true" "";
    "~ true$"      "Mcp-Session-Id, WWW-Authenticate";
}

map $ikigenba_cors_origin $ikigenba_upstream_origin {
    ""      $http_origin;
    default "";
}

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
    access_log  /var/log/nginx/access.log ikigenba;
    return      301 https://$host$request_uri;
}

server {
    listen              443 ssl default_server;
    listen              [::]:443 ssl default_server;
    ssl_reject_handshake on;
}

server {
    listen              443 ssl;
    server_name         sbx.ikigenba.dev *.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    return              404;
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- The host is fresh, as `init` leaves it before the first `activate`: there
  is no `/opt/ikigenba/current` and no app laid out under `/opt/<name>/`. A
  `state/` under `/var/opt/ikigenba/<name>/` would add no block.

Postconditions:

- Nothing has changed. `show` writes no file and reloads nothing.
- No app is routed, so there is no `auth` to recognize and nothing to wire;
  the frame is fail-open because no release is active, not by choice.

## An operator reads the configuration of a host running apps

Each routed service gains one block, in name order, at its own name under the
host's wildcard certificate. `crm` is the default app, so the space's name
answers from it and leaves the 404 block holding the wildcard alone. No routed
app here is named `auth`, so the host has no authenticator: every block is the
plain proxy, with no `auth_request` and no identity headers set from a
subrequest. This is the fail-open case, shown for what it is — the honest
absence of an authenticator, not a per-app security choice. Each app's block
adds the cross-origin headers and `Vary: Origin` to every response, and
answers an `OPTIONS` request `204` before its include and its location; the
404 block does neither.

Command:

```
$ sudo opsctl nginx show
```

Output:

```
# Generated by opsctl. Do not edit; run 'opsctl nginx apply'.

log_format ikigenba '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" $request_id';

map $http_origin $ikigenba_cors_origin {
    default                               "";
    "~^https://sites\.sbx\.ikigenba\.dev$" $http_origin;
}

map $ikigenba_cors_origin $ikigenba_cors_credentials {
    ""      "";
    default "true";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_methods {
    default        "";
    "OPTIONS true" "GET, POST, PUT, PATCH, DELETE, OPTIONS";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_headers {
    default        "";
    "OPTIONS true" "Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_max_age {
    default        "";
    "OPTIONS true" "600";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_expose {
    default        "";
    "OPTIONS true" "";
    "~ true$"      "Mcp-Session-Id, WWW-Authenticate";
}

map $ikigenba_cors_origin $ikigenba_upstream_origin {
    ""      $http_origin;
    default "";
}

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
    access_log  /var/log/nginx/access.log ikigenba;
    return      301 https://$host$request_uri;
}

server {
    listen              443 ssl default_server;
    listen              [::]:443 ssl default_server;
    ssl_reject_handshake on;
}

server {
    listen              443 ssl;
    server_name         *.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    return              404;
}

server {
    listen              443 ssl;
    server_name         crm.sbx.ikigenba.dev sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/crm/etc/nginx.conf*;

    location / {
        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
    }
}

server {
    listen              443 ssl;
    server_name         dashboard.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/dashboard/etc/nginx.conf*;

    location / {
        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- `/opt/ikigenba/current` names the release `c604e32`, whose apps are `crm`
  and `dashboard`.
- `/opt/ikigenba/current/crm/etc/manifest.toml` names `app = "crm"` and
  `default = true`.
- `/opt/ikigenba/current/dashboard/etc/manifest.toml` names `app =
  "dashboard"` and no `default`, or `default = false`.
- Neither app ships an `etc/nginx.conf`; the include matches nothing and
  nginx accepts it.
- `/var/opt/ikigenba/gmail/` holds a `state/`, and the current release holds
  no `gmail`: it is a data-only service.
- No app named `auth` is in the current release, so no block carries
  `auth_request` and the host is fail-open.

Postconditions:

- Nothing has changed. `gmail` has no block: it is a service the host will
  back up, and not one nginx can route. An `/opt/gmail/` left on the host
  would change nothing.
- Once applied, a request to `https://crm.sbx.ikigenba.dev` reaches `crm`
  through `/run/ikigenba/crm.sock` carrying an `X-Request-Id` that nginx
  generated, whatever `X-Request-Id` the client sent, and the access-log line
  for that request ends with the same id.
- `show` reads no socket: a block proxies to `/run/ikigenba/<name>.sock`
  whether or not that socket exists.

## An operator reads the configuration of the host that holds the apex

`devctl apex set dashboard.sbx` has named `dashboard` the apex app on this
host, so `ikigenba.dev` answers from `dashboard`'s block. `crm` is still the
default app and still answers at the space's name: the two choices are
independent, and here they name different apps. The file is the previous
story's with one line changed.

Command:

```
$ sudo opsctl nginx show
```

Output: the previous story's text, with `dashboard`'s block reading

```
server {
    listen              443 ssl;
    server_name         dashboard.sbx.ikigenba.dev ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/dashboard/etc/nginx.conf*;

    location / {
        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- As for the previous story, and `host.apex` is `dashboard`.
- No routed `auth` is on the host, so the blocks keep the plain proxy shape
  shown; the apex changes only which names a block answers at, not its wiring.
- The host's certificate covers `ikigenba.dev` as well (see
  `S06-certificates.md`); `show` does not check, since it writes nothing.

Postconditions:

- Nothing has changed. Had `crm` been named instead, its block's line would
  read `crm.sbx.ikigenba.dev sbx.ikigenba.dev ikigenba.dev` and `dashboard`'s
  would be as in the previous story: an app that is both default and apex
  answers at all three names.
- Were a routed `auth` present, `dashboard` — the apex app, and a routed
  non-auth app — would carry the wiring like any other block; holding the apex
  exempts a block from nothing.

## An operator reads the configuration while an app is disabled

`crm` has been disabled with `opsctl disable crm`. Its block keeps both of its
names, so the space's name and `crm.sbx.ikigenba.dev` answer `503` under the
host's certificate instead of falling to the 404 block. Its cross-origin
headers stay, so the `503` carries them, and a preflight is still answered
`204` before the `503` is reached. `dashboard` is unaffected.

Command:

```
$ sudo opsctl nginx show
```

Output: the `host running apps` story's file, with `crm`'s block reading

```
server {
    listen              443 ssl;
    server_name         crm.sbx.ikigenba.dev sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    return              503;
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- As for the `host running apps` story, and systemd reports
  `ikigenba-crm.socket` disabled.

Postconditions:

- Nothing has changed. `show` asked systemd whether each routed app's socket
  unit is enabled and changed nothing there.
- Once applied, every request to `crm`'s names but a preflight is answered
  `503` by nginx, and a preflight `204`, as the `page on the sites origin
  calls a disabled app` story shows; nothing reaches `/run/ikigenba/crm.sock`,
  which does not exist while `crm` is disabled. Had `crm` also held the apex,
  `ikigenba.dev` would answer `503` from the same block.
- An `activate` or `rollback` leaves `crm` disabled, so the file it
  regenerates holds this same block until `opsctl enable crm`.

## An operator reads the configuration when the apex app is not routed

`host.apex` names an app that is not in the current release — not yet
deployed, or dropped from the release with only its data kept. The apex still
belongs to this host, so its name goes on the 404 block beside the space's
name and wildcard, and answers 404 under the host's certificate rather than
having its handshake rejected. The next `activate` of a release that holds
that app moves the name to the app's block.

Command:

```
$ sudo opsctl nginx show
```

Output:

```
# Generated by opsctl. Do not edit; run 'opsctl nginx apply'.

log_format ikigenba '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" $request_id';

map $http_origin $ikigenba_cors_origin {
    default                               "";
    "~^https://sites\.sbx\.ikigenba\.dev$" $http_origin;
}

map $ikigenba_cors_origin $ikigenba_cors_credentials {
    ""      "";
    default "true";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_methods {
    default        "";
    "OPTIONS true" "GET, POST, PUT, PATCH, DELETE, OPTIONS";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_headers {
    default        "";
    "OPTIONS true" "Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_max_age {
    default        "";
    "OPTIONS true" "600";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_expose {
    default        "";
    "OPTIONS true" "";
    "~ true$"      "Mcp-Session-Id, WWW-Authenticate";
}

map $ikigenba_cors_origin $ikigenba_upstream_origin {
    ""      $http_origin;
    default "";
}

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
    access_log  /var/log/nginx/access.log ikigenba;
    return      301 https://$host$request_uri;
}

server {
    listen              443 ssl default_server;
    listen              [::]:443 ssl default_server;
    ssl_reject_handshake on;
}

server {
    listen              443 ssl;
    server_name         sbx.ikigenba.dev *.sbx.ikigenba.dev ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    return              404;
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `crm`.
- The host is fresh, as in the `bare host` story: no release is active, so no
  app is routed. `/var/opt/ikigenba/crm/` may hold a `state/`; a data-only
  `crm` is not routed either.
- No routed `auth` is present; nothing is wired, and the 404 frame is
  unchanged.

Postconditions:

- Nothing has changed. With routed apps on the host, the 404 block's line
  would read `*.sbx.ikigenba.dev ikigenba.dev` when one of them is the
  default, and `sbx.ikigenba.dev *.sbx.ikigenba.dev ikigenba.dev` when none
  is.

## An operator reads the configuration of a host running apps behind the authenticator

A routed app named `auth` is on the host, so every other app's block is wired
to it. `auth` is not the default, so it answers only at
`auth.sbx.ikigenba.dev`; `crm` is still the default and still answers at the
space's name; `dashboard` answers at its own name. The blocks come in
ascending name order — `auth`, `crm`, `dashboard` — and `auth`'s own is the
one left unwired: its `/check` and `/check/open` answer 404 to any direct
request, and `/check` is reached only as the other blocks' internal
subrequest. In each wired block a client cannot forge identity: the subrequest
to `/check` carries no client `X-User-Id` or `X-User-Email`, and on a valid
session nginx sets those two headers on the upstream from `auth`'s answer. The
subrequest also carries the original request's method, host, and request
target as `X-Original-Method`, `X-Original-Host`, and `X-Original-URI`. A 401
from `auth` becomes a 302 redirect to `auth.sbx.ikigenba.dev` carrying the
original request URL as `return=`; a 403 reaches the client unchanged. Between
the redirect and `location /`, each wired block carries the MCP locations:
`/mcp` exactly and everything under `/mcp/` are checked and relayed like
`location /`, but a 401 from `auth` is answered by `@mcp_unauthorized` — the
`401` with its `WWW-Authenticate` header and one-line body — instead of the
redirect, and a 403 from `auth` by `@mcp_invalid_token`, the `invalid_token`
401 with the same body. Both headers name `https://mcp.sbx.ikigenba.dev`, the
gateway's origin on this host, though the release holds no `mcp` app. The API
locations follow them: `/api` exactly and everything under `/api/` are checked
and relayed the same way, a 401 from `auth` is answered by
`@api_unauthorized`, the plain Bearer challenge with its own one-line body,
and a 403 from `auth` reaches the client unchanged. After them comes the git
location: a path ending `/info/refs`, `/git-upload-pack`, or
`/git-receive-pack` is checked and relayed the same way, and a 401 from `auth`
is answered by `@git_unauthorized`, a Basic challenge. The `\n` in each
`return` is the two characters backslash and `n` in the file. Every app's
block, `auth`'s included, adds the cross-origin headers and `Vary: Origin` and
answers an `OPTIONS` request `204` before anything else in it, so before the
subrequest. A location that sets a header of its own does not inherit the
block's, so each of the four named locations that answers a challenge repeats
the cross-origin headers a non-preflight response carries beside its
`WWW-Authenticate`.

Command:

```
$ sudo opsctl nginx show
```

Output:

```
# Generated by opsctl. Do not edit; run 'opsctl nginx apply'.

log_format ikigenba '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" $request_id';

map $http_origin $ikigenba_cors_origin {
    default                               "";
    "~^https://sites\.sbx\.ikigenba\.dev$" $http_origin;
}

map $ikigenba_cors_origin $ikigenba_cors_credentials {
    ""      "";
    default "true";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_methods {
    default        "";
    "OPTIONS true" "GET, POST, PUT, PATCH, DELETE, OPTIONS";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_headers {
    default        "";
    "OPTIONS true" "Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_max_age {
    default        "";
    "OPTIONS true" "600";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_expose {
    default        "";
    "OPTIONS true" "";
    "~ true$"      "Mcp-Session-Id, WWW-Authenticate";
}

map $ikigenba_cors_origin $ikigenba_upstream_origin {
    ""      $http_origin;
    default "";
}

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
    access_log  /var/log/nginx/access.log ikigenba;
    return      301 https://$host$request_uri;
}

server {
    listen              443 ssl default_server;
    listen              [::]:443 ssl default_server;
    ssl_reject_handshake on;
}

server {
    listen              443 ssl;
    server_name         *.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    return              404;
}

server {
    listen              443 ssl;
    server_name         auth.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/auth/etc/nginx.conf*;

    location = /check {
        return 404;
    }

    location = /check/open {
        return 404;
    }

    location / {
        proxy_pass       http://unix:/run/ikigenba/auth.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
    }
}

server {
    listen              443 ssl;
    server_name         crm.sbx.ikigenba.dev sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/crm/etc/nginx.conf*;

    location = /_ikigenba/check {
        internal;
        proxy_pass              http://unix:/run/ikigenba/auth.sock:/check;
        proxy_pass_request_body off;
        proxy_set_header        Content-Length "";
        proxy_set_header        X-User-Id    "";
        proxy_set_header        X-User-Email "";
        proxy_set_header        X-Request-Id $request_id;
        proxy_set_header        X-Original-Method $request_method;
        proxy_set_header        X-Original-Host   $host;
        proxy_set_header        X-Original-URI    $request_uri;
        proxy_set_header        Origin            $ikigenba_upstream_origin;
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location @mcp_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @mcp_invalid_token {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer error="invalid_token", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @api_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer realm="ikigenba"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: sign in or send Authorization: Bearer <token>\n";
    }

    location @git_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Basic realm="ikigenba"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send your token as the password\n";
    }

    location = /mcp {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @mcp_unauthorized;
        error_page       403 = @mcp_invalid_token;

        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ^~ /mcp/ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @mcp_unauthorized;
        error_page       403 = @mcp_invalid_token;

        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location = /api {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @api_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ^~ /api/ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @api_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ~ /(info/refs|git-upload-pack|git-receive-pack)$ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @git_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location / {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @auth_redirect;

        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }
}

server {
    listen              443 ssl;
    server_name         dashboard.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/dashboard/etc/nginx.conf*;

    location = /_ikigenba/check {
        internal;
        proxy_pass              http://unix:/run/ikigenba/auth.sock:/check;
        proxy_pass_request_body off;
        proxy_set_header        Content-Length "";
        proxy_set_header        X-User-Id    "";
        proxy_set_header        X-User-Email "";
        proxy_set_header        X-Request-Id $request_id;
        proxy_set_header        X-Original-Method $request_method;
        proxy_set_header        X-Original-Host   $host;
        proxy_set_header        X-Original-URI    $request_uri;
        proxy_set_header        Origin            $ikigenba_upstream_origin;
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location @mcp_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @mcp_invalid_token {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer error="invalid_token", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @api_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer realm="ikigenba"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: sign in or send Authorization: Bearer <token>\n";
    }

    location @git_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Basic realm="ikigenba"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send your token as the password\n";
    }

    location = /mcp {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @mcp_unauthorized;
        error_page       403 = @mcp_invalid_token;

        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ^~ /mcp/ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @mcp_unauthorized;
        error_page       403 = @mcp_invalid_token;

        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location = /api {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @api_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ^~ /api/ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @api_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ~ /(info/refs|git-upload-pack|git-receive-pack)$ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @git_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location / {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @auth_redirect;

        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- `/opt/ikigenba/current` names the release `c604e32`, whose apps are
  `auth`, `crm`, and `dashboard`.
- `/opt/ikigenba/current/auth/etc/manifest.toml` names `app = "auth"` and no
  `default`, or `default = false`.
- `/opt/ikigenba/current/crm/etc/manifest.toml` names `app = "crm"` and
  `default = true`.
- `/opt/ikigenba/current/dashboard/etc/manifest.toml` names `app =
  "dashboard"` and no `default`, or `default = false`.
- No app ships an `etc/nginx.conf`; each include matches nothing and nginx
  accepts it.

Postconditions:

- Nothing has changed. `show` writes no file and reloads nothing.
- `auth` alone carries no `auth_request`; every other routed app carries it,
  purely because a routed `auth` is present — there is no per-app opt-in or
  opt-out. Neither manifest sets `guests`, so neither block carries
  `/_ikigenba/check/open`; `auth`'s block guards `/check/open` all the same.
- `crm` and `dashboard` carry the MCP locations, the API locations, and the
  git location whether
  their manifests set `mcp = true`, `mcp = false`, or no `mcp` at all, and
  whether or not either serves git; `auth`'s block carries none.
- Once applied, the subrequest to `/check` and the request it admits carry the
  same `X-Request-Id`: nginx's id for the client's request, never the
  client's own value. This holds under `/mcp` as under `/`: a request to
  `/mcp` or `/mcp/<anything>` that `auth` admits reaches the app with
  `X-Request-Id`, `X-User-Id`, and `X-User-Email` set exactly as a request to
  `/` would.
- Once applied, the subrequest to `/check` carries the client's request, not
  its own: a `POST` to `https://crm.sbx.ikigenba.dev/notes?page=2` reaches
  `auth` with `X-Original-Method: POST`, `X-Original-Host:
  crm.sbx.ikigenba.dev`, and `X-Original-URI: /notes?page=2`, whatever the
  client itself sent under those names.

## An operator reads the configuration of a host running an app that serves guests

`sites` serves public pages to anyone, so its manifest sets `guests = true`,
and `devctl apex set sites.sbx` has made it the apex app. A routed `auth` is
on the host, so `sites` is wired; no app is the default, so the space's name
stays on the 404 block with the wildcard. `sites`'s block is the wired block
of the `host running apps behind the authenticator` story with two
differences: the internal `/_ikigenba/check/open` location follows
`/_ikigenba/check`, asking `auth`'s `/check/open`, and `location /` asks it
with no `error_page 401` line. Its `/mcp` locations, `/api` locations, and git
location are unchanged, still asking `/_ikigenba/check` and still answering
401 with their challenges, and `@auth_redirect` is still there though
`location /` no longer uses it. The apex, `ikigenba.dev`, is a name on the
same block, so it is served the same way. `auth`'s block is the one every
wired host gets.

Command:

```
$ sudo opsctl nginx show
```

Output:

```
# Generated by opsctl. Do not edit; run 'opsctl nginx apply'.

log_format ikigenba '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" $request_id';

map $http_origin $ikigenba_cors_origin {
    default                               "";
    "~^https://sites\.sbx\.ikigenba\.dev$" $http_origin;
}

map $ikigenba_cors_origin $ikigenba_cors_credentials {
    ""      "";
    default "true";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_methods {
    default        "";
    "OPTIONS true" "GET, POST, PUT, PATCH, DELETE, OPTIONS";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_headers {
    default        "";
    "OPTIONS true" "Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_max_age {
    default        "";
    "OPTIONS true" "600";
}

map "$request_method $ikigenba_cors_credentials" $ikigenba_cors_expose {
    default        "";
    "OPTIONS true" "";
    "~ true$"      "Mcp-Session-Id, WWW-Authenticate";
}

map $ikigenba_cors_origin $ikigenba_upstream_origin {
    ""      $http_origin;
    default "";
}

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
    access_log  /var/log/nginx/access.log ikigenba;
    return      301 https://$host$request_uri;
}

server {
    listen              443 ssl default_server;
    listen              [::]:443 ssl default_server;
    ssl_reject_handshake on;
}

server {
    listen              443 ssl;
    server_name         sbx.ikigenba.dev *.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    return              404;
}

server {
    listen              443 ssl;
    server_name         auth.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/auth/etc/nginx.conf*;

    location = /check {
        return 404;
    }

    location = /check/open {
        return 404;
    }

    location / {
        proxy_pass       http://unix:/run/ikigenba/auth.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
    }
}

server {
    listen              443 ssl;
    server_name         sites.sbx.ikigenba.dev ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;
    add_header          Access-Control-Allow-Origin      $ikigenba_cors_origin always;
    add_header          Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
    add_header          Access-Control-Allow-Methods     $ikigenba_cors_methods always;
    add_header          Access-Control-Allow-Headers     $ikigenba_cors_headers always;
    add_header          Access-Control-Max-Age           $ikigenba_cors_max_age always;
    add_header          Access-Control-Expose-Headers    $ikigenba_cors_expose always;
    add_header          Vary                             Origin always;

    if ($request_method = OPTIONS) {
        return 204;
    }

    include /opt/ikigenba/current/sites/etc/nginx.conf*;

    location = /_ikigenba/check {
        internal;
        proxy_pass              http://unix:/run/ikigenba/auth.sock:/check;
        proxy_pass_request_body off;
        proxy_set_header        Content-Length "";
        proxy_set_header        X-User-Id    "";
        proxy_set_header        X-User-Email "";
        proxy_set_header        X-Request-Id $request_id;
        proxy_set_header        X-Original-Method $request_method;
        proxy_set_header        X-Original-Host   $host;
        proxy_set_header        X-Original-URI    $request_uri;
        proxy_set_header        Origin            $ikigenba_upstream_origin;
    }

    location = /_ikigenba/check/open {
        internal;
        proxy_pass              http://unix:/run/ikigenba/auth.sock:/check/open;
        proxy_pass_request_body off;
        proxy_set_header        Content-Length "";
        proxy_set_header        X-User-Id    "";
        proxy_set_header        X-User-Email "";
        proxy_set_header        X-Request-Id $request_id;
        proxy_set_header        X-Original-Method $request_method;
        proxy_set_header        X-Original-Host   $host;
        proxy_set_header        X-Original-URI    $request_uri;
        proxy_set_header        Origin            $ikigenba_upstream_origin;
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location @mcp_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @mcp_invalid_token {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer error="invalid_token", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @api_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Bearer realm="ikigenba"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: sign in or send Authorization: Bearer <token>\n";
    }

    location @git_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate                 'Basic realm="ikigenba"' always;
        add_header   Access-Control-Allow-Origin      $ikigenba_cors_origin always;
        add_header   Access-Control-Allow-Credentials $ikigenba_cors_credentials always;
        add_header   Access-Control-Expose-Headers    $ikigenba_cors_expose always;
        add_header   Vary                             Origin always;
        return       401 "authentication required: send your token as the password\n";
    }

    location = /mcp {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @mcp_unauthorized;
        error_page       403 = @mcp_invalid_token;

        proxy_pass       http://unix:/run/ikigenba/sites.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ^~ /mcp/ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @mcp_unauthorized;
        error_page       403 = @mcp_invalid_token;

        proxy_pass       http://unix:/run/ikigenba/sites.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location = /api {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @api_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/sites.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ^~ /api/ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @api_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/sites.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location ~ /(info/refs|git-upload-pack|git-receive-pack)$ {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @git_unauthorized;

        proxy_pass       http://unix:/run/ikigenba/sites.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }

    location / {
        auth_request     /_ikigenba/check/open;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;

        proxy_pass       http://unix:/run/ikigenba/sites.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_set_header Origin            $ikigenba_upstream_origin;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `sites`.
- `/opt/ikigenba/current` names the release `c604e32`, whose apps are `auth`
  and `sites`.
- `/opt/ikigenba/current/auth/etc/manifest.toml` names `app = "auth"` and no
  `default`, or `default = false`.
- `/opt/ikigenba/current/sites/etc/manifest.toml` names `app = "sites"` and
  `guests = true`, and no `default`, or `default = false`.
- No app ships an `etc/nginx.conf`; each include matches nothing and nginx
  accepts it.
- The host's certificate covers `ikigenba.dev` as well (see
  `S06-certificates.md`); `show` does not check, since it writes nothing.

Postconditions:

- Nothing has changed. `show` writes no file and reloads nothing.
- `guests` reaches only the block of the app that sets it. Had `crm` of the
  `host running apps behind the authenticator` story been in the release
  beside `sites`, its block would be exactly as that story shows.
- Had no routed `auth` been on the host, `sites`'s block would be the plain
  proxy block of the `host running apps` story, answering at the same names,
  with no `auth_request` of either kind: with no authenticator every app is
  fail-open, and `guests` changes nothing there.
- Had systemd reported `ikigenba-sites.socket` disabled, `sites`'s block would
  be the `503` block of the `app is disabled` story, answering at
  `sites.sbx.ikigenba.dev ikigenba.dev`.
- Once applied, the subrequest to `/check/open` carries the same
  `X-Request-Id`, `X-Original-Method`, `X-Original-Host`, and
  `X-Original-URI` the subrequest to `/check` would, and no client
  `X-User-Id` or `X-User-Email`.

## An operator reads the configuration where auth is present but not routed

An `auth` service keeps its state on the host, but the current release holds
no `auth` — the `/var/opt/ikigenba/auth/state/` a release that dropped `auth`
leaves behind, say. It is a data-only service, known but not routed, so it is
not the authenticator: there is nowhere to send a subrequest. Recognition
takes an app in the current release, not the directory and not the name, so
the host stays fail-open and `auth` gets no block of its own. An
`/opt/auth/` left on the host changes nothing: on a released host an
`/opt/<name>/` makes no service.

Command:

```
$ sudo opsctl nginx show
```

Output: exactly the `host running apps` story's file — the frame, then `crm`
and `dashboard` as plain proxy blocks, with no `auth` block and no
`auth_request` anywhere.

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- `/opt/ikigenba/current` names the release `c604e32`, whose apps are `crm`
  and `dashboard`; `/opt/ikigenba/current/crm/etc/manifest.toml` names `app =
  "crm"` and `default = true`, and
  `/opt/ikigenba/current/dashboard/etc/manifest.toml` names `app =
  "dashboard"` and no `default`.
- `/var/opt/ikigenba/auth/` holds a `state/`, and the current release holds
  no `auth`.

Postconditions:

- Nothing has changed.
- `auth` has no block: unrouted, it is a service the host will back up and one
  nginx cannot route, and it is not the authenticator. No block is wired; the
  host stays fail-open until a release that holds `auth` is activated.

## An MCP client reaches a wired app without a credential

An MCP client is a program, not a browser: a redirect to a sign-in page is
nothing it can follow. So on a wired app, a request under `/mcp` that `auth`
answers 401 is answered `401` by nginx itself, with a challenge naming the
scheme the client should use and where to find out how to get a token — the
MCP gateway's protected-resource metadata — and one line saying what to send.
`/mcp` itself and any path under `/mcp/` behave the same, and the metadata is
the gateway's whichever app was asked.

Request:

```
$ curl -si https://crm.sbx.ikigenba.dev/mcp
```

```
$ curl -si https://crm.sbx.ikigenba.dev/mcp/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"
```

Status 401. The body is the one line `authentication required: send
Authorization: Bearer <token>`, ending in a newline, where `<token>` is those
seven characters as written, not a value filled in.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`; the request
  was decided by the `/check` subrequest alone.
- The access-log line for the request records status `401` and ends with
  nginx's request id, the same id the subrequest to `/check` carried.

## An MCP client whose token the authenticator refuses reaches a wired app

A client that sends a bearer token `auth` will not honor — unknown, revoked,
disabled, expired, issued for another host, or its owner's sign-in lapsed —
is told its token is no good and where to get another. An MCP client starts
signing in again only on a 401, so under `/mcp` nginx answers `auth`'s 403
with a `401` whose challenge says `invalid_token` and names the MCP gateway's
protected-resource metadata, with the same one line as a request with no
credential. That way an expired or revoked token heals itself: the client's
next call prompts its user to sign in.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://crm.sbx.ikigenba.dev/mcp
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://crm.sbx.ikigenba.dev/mcp/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer error="invalid_token", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"
```

Status 401. The body is the one line `authentication required: send
Authorization: Bearer <token>`, ending in a newline, where `<token>` is those
seven characters as written, not a value filled in.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- `auth`'s `/check` answers 403 for `ikp_<token>`.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`; `/check`
  itself answered 403, and only nginx's answer to the client changed.
- Outside `/mcp` and `/mcp/...` the same token still gets `auth`'s 403
  unchanged: at `https://crm.sbx.ikigenba.dev/`, and at a git path such as
  `https://crm.sbx.ikigenba.dev/notes.git/info/refs`, as the `git client sends
  its token as the password` story says.
- Under the configuration of the `host running an app that serves guests`
  story, `https://sites.sbx.ikigenba.dev/mcp` answers the same.

## A browser reaches a wired app outside `/mcp` without signing in

The MCP answer belongs to `/mcp` and `/mcp/...` alone. Everywhere else a wired
block still sends a visitor with no credential to sign in, including a path
that merely begins with the same letters.

Request:

```
$ curl -si https://crm.sbx.ikigenba.dev/
```

```
$ curl -si https://crm.sbx.ikigenba.dev/mcpx
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: https://auth.sbx.ikigenba.dev/?return=https://crm.sbx.ikigenba.dev/
```

Status 302. For the second form the `Location` ends
`?return=https://crm.sbx.ikigenba.dev/mcpx`: the original URL, whatever its
path. The response carries no `WWW-Authenticate` header; the body is not
fixed.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`.

## A git client reaches a wired app without a credential

git asks for a repository's refs before anything else and sends no
credential until it is challenged. On a wired app, `auth` answers that first
request 401, and nginx turns it into the Basic challenge rather than a
redirect, so git asks its credential helper for a username and password and
tries again. The same answer meets the pack requests a fetch or push makes
when they arrive without a credential.

Request:

```
$ curl -si 'https://crm.sbx.ikigenba.dev/notes.git/info/refs?service=git-upload-pack'
```

```
$ curl -si -X POST https://crm.sbx.ikigenba.dev/notes.git/git-receive-pack
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Basic realm="ikigenba"
```

Status 401. The body is the one line `authentication required: send your
token as the password`, ending in a newline. `/notes.git/git-upload-pack`
answers the same.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`.
- `https://crm.sbx.ikigenba.dev/notes.git/info/refsx` is an ordinary path:
  without a credential it redirects to sign in, as `/mcpx` does in the
  `browser reaches a wired app outside /mcp` story.

## A git client sends its token as the password to a wired app

Once challenged, git sends `Authorization: Basic` holding a username and the
token as the password. `auth` decides it as it decides a bearer token, and
the request reaches the app as a request at `/` does once admitted, with the
identity `auth` gave. A token `auth` refuses gets its 403 unchanged, as at
`/`; only `/mcp` and `/mcp/...` turn a refusal into a 401.

Request:

```
$ curl -si -u 'git:ikp_<token>' 'https://crm.sbx.ikigenba.dev/notes.git/info/refs?service=git-upload-pack'
```

Response: not fixed here; it is `crm`'s answer to the request.

Status is whatever `crm` answers; nginx adds no status of its own.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- `auth`'s `/check` admits a Basic credential whose password is
  `ikp_<token>`, whatever the username.

Postconditions:

- `crm` received `GET /notes.git/info/refs?service=git-upload-pack` with
  `X-User-Id` and `X-User-Email` set from `auth`'s answer, `X-Request-Id`
  set by nginx, and the client's `Authorization` header as it was sent.
- The subrequest to `/check` carried `X-Original-URI:
  /notes.git/info/refs?service=git-upload-pack`.

## A guest reaches an app that serves guests

A visitor with no credential at all asks for a page of an app that serves
guests. `auth`'s `/check/open` admits the request with no identity, so nginx
proxies it to the app with no identity headers and sends nobody to sign in.
The apex answers from the same block, so a guest reaches `sites` there too.

Request:

```
$ curl -si https://sites.sbx.ikigenba.dev/
```

```
$ curl -si https://ikigenba.dev/
```

Response: not fixed here; it is `sites`'s answer to the request.

Status is whatever `sites` answers; nginx adds no status of its own. The
response is never a redirect to `auth.sbx.ikigenba.dev`.

Preconditions:

- The configuration of the `host running an app that serves guests` story has
  been applied, and `auth` and `sites` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check/open` answers 200 with neither `X-User-Id` nor
  `X-User-Email`.

Postconditions:

- `sites` received `GET /` with no `X-User-Id` and no `X-User-Email`, and with
  `X-Request-Id` set by nginx, the same id the subrequest to `/check/open`
  carried and the access-log line for the request ends with. Had the client
  sent its own `X-User-Id` or `X-User-Email`, `sites` would not have received
  it.
- Any path outside `/mcp`, `/mcp/...`, `/api`, `/api/...`, and the git paths
  behaves the same: `https://sites.sbx.ikigenba.dev/mcpx` and
  `https://sites.sbx.ikigenba.dev/apix` reach `sites` as a guest too.
- A signed-in visitor, whose session `auth` honors, reaches `sites` the same
  way but with `X-User-Id` and `X-User-Email` set from `auth`'s answer, as on
  any wired app.
- A visitor whose token `auth` refuses gets `auth`'s 403 unchanged, and
  nothing reaches `/run/ikigenba/sites.sock`.

## An MCP client reaches an app that serves guests without a credential

Serving guests opens only `location /`. `/mcp` and everything under `/mcp/`
are checked against the strict `/check` on every wired block, so an MCP client
with no credential gets the same challenge here as at any wired app. A git
client with no credential likewise gets the Basic challenge of the `git
client reaches a wired app without a credential` story.

Request:

```
$ curl -si https://sites.sbx.ikigenba.dev/mcp
```

```
$ curl -si https://sites.sbx.ikigenba.dev/mcp/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"
```

Status 401. The body is the one line `authentication required: send
Authorization: Bearer <token>`, ending in a newline, where `<token>` is those
seven characters as written, not a value filled in.

Preconditions:

- The configuration of the `host running an app that serves guests` story has
  been applied, and `auth` and `sites` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/sites.sock`; the request
  was decided by the `/check` subrequest alone, and `/check/open` was not
  asked.
- `https://ikigenba.dev/mcp` answers the same.

## A client reaches `/api` on a wired app without a credential

A program, or a page's script, calls an app's HTTP interface. Neither can
follow a redirect to a sign-in page, so on a wired app a request under `/api`
that `auth` answers 401 is answered `401` by nginx itself, with a plain Bearer
challenge and one line saying what to do. Unlike `/mcp`, the challenge names
no protected-resource metadata: there is no sign-in flow for a program to
start here, only a session to hold or a token to send. `/api` itself and any
path under `/api/` behave the same.

Request:

```
$ curl -si https://crm.sbx.ikigenba.dev/api
```

```
$ curl -si https://crm.sbx.ikigenba.dev/api/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the one line `authentication required: sign in or
send Authorization: Bearer <token>`, ending in a newline, where `<token>` is
those seven characters as written, not a value filled in.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`; the request
  was decided by the `/check` subrequest alone.
- `https://crm.sbx.ikigenba.dev/apix` is an ordinary path: without a
  credential it redirects to sign in, as `/mcpx` does in the `browser reaches
  a wired app outside /mcp` story.

## A client whose token the authenticator refuses reaches `/api` on a wired app

Only `/mcp` turns a refusal into a 401, because only an MCP client starts
signing in again on one. Under `/api`, a token `auth` will not honor gets
`auth`'s 403 unchanged, as at `/` and at the git paths.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://crm.sbx.ikigenba.dev/api/<anything>
```

Response:

```
HTTP/1.1 403 Forbidden
```

Status 403. The response carries no `WWW-Authenticate` header; the body is not
fixed.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- `auth`'s `/check` answers 403 for `ikp_<token>`.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`.
- `https://crm.sbx.ikigenba.dev/api` answers the same.

## A client with a valid credential reaches `/api` on a wired app

A request `auth` admits, by session cookie or by token, reaches the app under
`/api` exactly as it would at `/`, with the identity `auth` gave.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' 'https://crm.sbx.ikigenba.dev/api/notes?page=2'
```

```
$ curl -si -b 'ikigenba_session=<session>' 'https://crm.sbx.ikigenba.dev/api/notes?page=2'
```

Response: not fixed here; it is `crm`'s answer to the request.

Status is whatever `crm` answers; nginx adds no status of its own.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- `auth`'s `/check` admits `ikp_<token>` and `<session>`.

Postconditions:

- `crm` received `GET /api/notes?page=2` with `X-User-Id` and `X-User-Email`
  set from `auth`'s answer and `X-Request-Id` set by nginx, whatever the
  client sent under those names.
- The subrequest to `/check` carried `X-Original-URI: /api/notes?page=2`.

## A client reaches `/api` on an app that serves guests without a credential

Serving guests opens only `location /`. `/api` and everything under `/api/`
are checked against the strict `/check` on every wired block, so a client with
no credential gets the same challenge here as at any wired app.

Request:

```
$ curl -si https://sites.sbx.ikigenba.dev/api
```

```
$ curl -si https://sites.sbx.ikigenba.dev/api/<anything>
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the one line `authentication required: sign in or
send Authorization: Bearer <token>`, ending in a newline.

Preconditions:

- The configuration of the `host running an app that serves guests` story has
  been applied, and `auth` and `sites` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/sites.sock`, and
  `/check/open` was not asked.
- `https://ikigenba.dev/api` answers the same.

## A page on the sites origin preflights a call to a wired app

A page at `https://sites.sbx.ikigenba.dev` is about to call `crm`'s `/mcp`
with a JSON body and an MCP session. The browser first asks `crm`'s name
whether it may, with an `OPTIONS` request carrying no cookie. nginx answers it
`204` itself, with what the page may send, before asking `auth` anything.

Request:

```
$ curl -si -X OPTIONS -H 'Origin: https://sites.sbx.ikigenba.dev' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type, mcp-session-id' https://crm.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/1.1 204 No Content
Access-Control-Allow-Origin: https://sites.sbx.ikigenba.dev
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID
Access-Control-Max-Age: 600
Vary: Origin
```

Status 204. There is no body, and no `Access-Control-Expose-Headers` header.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied.

Postconditions:

- Nothing has changed. No subrequest went to `/check` and nothing reached
  `/run/ikigenba/crm.sock`; the answer is nginx's whether or not `auth` and
  `crm` are active.
- The answer is the same at any path on `crm`'s names, whatever
  `Access-Control-Request-Method` and `Access-Control-Request-Headers` the
  request names, and with or without a cookie or token.
- `auth.sbx.ikigenba.dev`, and every name of the `sites` block in the `host
  running an app that serves guests` story, answer the same preflight the
  same way.
- The access-log line for the request records status `204`.

## A page on the sites origin calls a wired app with the visitor's session

Once the preflight allows it, the page's script sends the call with the
browser's session cookie. `auth` admits it as it would any request carrying
that session, `crm` answers, and nginx adds the headers that let the script
read the answer, `Mcp-Session-Id` included.

Request:

```
$ curl -si -X POST -H 'Origin: https://sites.sbx.ikigenba.dev' -H 'Content-Type: application/json' -b 'ikigenba_session=<session>' -d '<json-rpc request>' https://crm.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/1.1 <status>
Access-Control-Allow-Origin: https://sites.sbx.ikigenba.dev
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status is whatever `crm` answers, and the body is `crm`'s; nginx adds only
the headers shown. There is no `Access-Control-Allow-Methods`,
`Access-Control-Allow-Headers`, or `Access-Control-Max-Age` header.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- `auth`'s `/check` admits `<session>`.

Postconditions:

- `crm` received the `POST /mcp` with `X-User-Id` and `X-User-Email` set from
  `auth`'s answer, as any admitted request under `/mcp` does, and with no
  `Origin` header, so it takes the call as it takes one from its own pages.
  `auth`'s `/check` received no `Origin` header either.
- A call to `/api`, `/api/...`, or any other path on `crm`'s names carries
  the same headers on `crm`'s answer.

## A page on the sites origin calls a wired app without a session

The visitor's session has lapsed, or they never signed in. The call is
refused as any such call is, but the refusal carries the cross-origin headers,
so the page's script can read the status and the `WWW-Authenticate` challenge
and send its visitor to sign in, rather than seeing an opaque network error.

Request:

```
$ curl -si -X POST -H 'Origin: https://sites.sbx.ikigenba.dev' https://crm.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"
Access-Control-Allow-Origin: https://sites.sbx.ikigenba.dev
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status 401. The body is the one line of the `MCP client reaches a wired app
without a credential` story.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` and `crm` are active.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header, so `auth`'s `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`.
- Every other answer nginx gives in `crm`'s block carries the same four
  headers beside its own: the `401` of the `client reaches /api on a wired
  app without a credential` story, the `invalid_token` 401 under `/mcp`, the
  Basic challenge at a git path, the `302` to sign in at `/`, and `auth`'s 403
  outside `/mcp`.

## A page on the sites origin signs its user out

The banner on `sites`' own pages signs the user out with a form that POSTs to
`auth`'s `/logout`, as every app's banner does. `auth` lets any app on the
space sign its user out and decides that from the request's `Origin`, so
`auth`'s own block passes the allowed origin on to `auth` unchanged.

Request:

```
$ curl -si -X POST -H 'Origin: https://sites.sbx.ikigenba.dev' -b 'ikigenba_session=<session>' https://auth.sbx.ikigenba.dev/logout
```

Response:

```
HTTP/1.1 <status>
Access-Control-Allow-Origin: https://sites.sbx.ikigenba.dev
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status is whatever `auth` answers, and the body is `auth`'s.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied, and `auth` is active.

Postconditions:

- `auth` received the `POST /logout` with the request's
  `Origin: https://sites.sbx.ikigenba.dev` unchanged, as it receives any
  request's `Origin` at any path of its own block.

## A page on another origin calls an app

Only the allowed origin is granted anything. A page anywhere else, including a
name that merely begins like it, the same name over `http`, or the same name
in other letter case, gets no `Access-Control-` header at all, so its browser
refuses to let it send the call or read the answer. The preflight is still
answered `204` by nginx, and `Vary: Origin` is still on every response, so no
cache hands one origin's answer to another.

Request:

```
$ curl -si -X OPTIONS -H 'Origin: https://elsewhere.example' -H 'Access-Control-Request-Method: POST' https://crm.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X OPTIONS -H 'Origin: https://sites.sbx.ikigenba.dev.elsewhere.example' -H 'Access-Control-Request-Method: POST' https://crm.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X OPTIONS -H 'Origin: http://sites.sbx.ikigenba.dev' -H 'Access-Control-Request-Method: POST' https://crm.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X OPTIONS -H 'Origin: HTTPS://SITES.SBX.IKIGENBA.DEV' -H 'Access-Control-Request-Method: POST' https://crm.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X OPTIONS https://crm.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/1.1 204 No Content
Vary: Origin
```

Status 204. The response carries no `Access-Control-` header; there is no
body.

Preconditions:

- The configuration of the `host running apps behind the authenticator` story
  has been applied.

Postconditions:

- Nothing has changed. No subrequest went to `/check` and nothing reached
  `/run/ikigenba/crm.sock`.
- Any other request with such an `Origin`, or none, is answered as it would
  be without the cross-origin grant, plus `Vary: Origin`, and with no
  `Access-Control-` header: a request with a valid session reaches `crm`, with
  its `Origin` header unchanged, or none, for `crm` to judge, and one without
  gets the challenge or redirect its path calls for.

## A page on the sites origin calls an app on a host with no authenticator

A host with no routed `auth` is fail-open, and every app's block is the plain
proxy. The cross-origin grant does not depend on the authenticator: the plain
block answers the preflight `204` itself and adds the headers to the app's
answer just as a wired block does.

Request:

```
$ curl -si -X POST -H 'Origin: https://sites.sbx.ikigenba.dev' https://crm.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/1.1 <status>
Access-Control-Allow-Origin: https://sites.sbx.ikigenba.dev
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status is whatever `crm` answers, and the body is `crm`'s.

Preconditions:

- The configuration of the `host running apps` story has been applied, and
  `crm` is active. The current release holds no `sites` app.

Postconditions:

- `crm` received the `POST /mcp` with no identity headers set by nginx, as
  any request on a fail-open host does, and with no `Origin` header.
- A preflight to `crm`'s names, `sbx.ikigenba.dev` included, is answered
  exactly as in the `page on the sites origin preflights a call to a wired
  app` story, and never reaches `crm`.

## A page on the sites origin calls a disabled app

A disabled app answers `503` to everything but a preflight, and the `503`
carries the cross-origin headers, so the page's script can tell the app is
unavailable. The preflight itself is still answered `204`, so the browser
sends the call and the script sees the `503` rather than a refused preflight.

Request:

```
$ curl -si -X POST -H 'Origin: https://sites.sbx.ikigenba.dev' https://crm.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/1.1 503 Service Temporarily Unavailable
Access-Control-Allow-Origin: https://sites.sbx.ikigenba.dev
Access-Control-Allow-Credentials: true
Access-Control-Expose-Headers: Mcp-Session-Id, WWW-Authenticate
Vary: Origin
```

Status 503. The body is not fixed.

Preconditions:

- The configuration of the `app is disabled` story has been applied.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/crm.sock`.
- A preflight to `crm`'s names is answered exactly as in the `page on the
  sites origin preflights a call to a wired app` story.

## A client asks auth's /check directly

`/check` and `/check/open` are `auth`'s answers to nginx's internal
subrequests, not pages. Asked from outside, each answers 404 from nginx
itself, whether or not any app on the host serves guests.

Request:

```
$ curl -si https://auth.sbx.ikigenba.dev/check
```

```
$ curl -si https://auth.sbx.ikigenba.dev/check/open
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed.

Preconditions:

- The configuration of the `host running apps behind the authenticator`
  story, or of the `host running an app that serves guests` story, has been
  applied, and `auth` is active.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/auth.sock`.

## An app widens the request limits for its own requests

An app that takes large uploads over slow connections — git pushes, say —
needs nginx to accept a body larger than its default 1 MiB, to wait longer
than its default 60 seconds for the app to answer, and to pass the body on as
it arrives rather than spooling it to disk first. The app says so in its own
`etc/nginx.conf`, and opsctl changes nothing to let it: the file is included
in the app's server block, and these three directives are ones nginx allows
there and the generated locations do not set, so each location inherits them.
The file comes with the app's tree in the release.

`/opt/ikigenba/current/crm/etc/nginx.conf`:

```
client_max_body_size    0;
proxy_read_timeout      3600s;
proxy_request_buffering off;
```

Command:

```
$ sudo opsctl nginx show
```

Output: the configuration of the `host running apps behind the authenticator`
story, byte for byte. The fragment is not copied into the file; the line
`include /opt/ikigenba/current/crm/etc/nginx.conf*;` already in `crm`'s block is what brings
it in.

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The host is the one in the `host running apps behind the authenticator`
  story, and `/opt/ikigenba/current/crm/etc/nginx.conf` holds the three lines
  above.

Postconditions:

- Nothing has changed.
- Once applied, `nginx -t` passes, and in `crm`'s block every location — `/`,
  `/mcp`, `/mcp/`, `/api`, `/api/`, and the git location — accepts a request
  body of any size, waits up to an hour between reads from `crm`'s socket, and
  passes a request body to `crm` as it arrives. A push of several hundred MiB
  to `crm` is admitted by `/check` and streamed to the app.
- The subrequest to `/check` carries no body whatever the fragment says, so
  `auth` is unaffected.
- `dashboard`'s and `auth`'s blocks keep nginx's defaults: a body larger
  than 1 MiB sent to `dashboard` is refused `413`. The fragment reaches only
  the app that ships it.

## An operator applies the configuration

`apply` writes the file, has nginx check the whole configuration, and reloads
it. It also rewrites the services file, `/run/ikigenba/services.json`
(`S09-services.md`), so the two never disagree about which apps are disabled.
Nothing is printed; the answer is the exit code.

Command:

```
$ sudo opsctl nginx apply
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `host.name` is set and the host's certificate exists at
  `/etc/letsencrypt/live/<host.name>/`.
- `/opt/ikigenba/current` names a release.
- `nginx` and `systemctl` are on the PATH.

Postconditions:

- `/etc/nginx/conf.d/ikigenba.conf` holds byte for byte what `show` prints,
  with mode `0644`, written to a temporary file in the same directory and
  renamed over the old one, so nginx never reads a half-written file.
- `nginx -t` passed and nginx has been reloaded; it is serving the new
  configuration and never stopped serving the old one.
- No other file under `/etc/nginx` was created, modified, or removed.
- `/run/ikigenba/services.json` has been rewritten from the same store,
  current release, and disabled apps the nginx file was. Nothing was printed
  about it.
- Applying again with nothing else changed rewrites the same bytes, passes
  the same test, reloads again, and exits 0.
- When a routed `auth` wires the other blocks, `nginx -t` still passes: `-t`
  checks the configuration's form and does not dial the `/check` upstream, so
  the `auth_request` directives test valid whether or not `auth` is answering.
  The reload behaves exactly as above.

## An operator applies before the certificate exists

Every 443 block names the host's certificate, so a host with none has a
configuration nginx will refuse. The refusal is nginx's, and its output
follows the diagnostic, quoted line by line.

Command:

```
$ sudo opsctl nginx apply
```

Output:

```
opsctl: nginx -t: exit status 1

> nginx: [emerg] cannot load certificate "/etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem": BIO_new_file() failed
> nginx: configuration file /etc/nginx/nginx.conf test failed
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`.
- `/etc/letsencrypt/live/sbx.ikigenba.dev/` does not exist; `opsctl cert
  obtain` has not been run.

Postconditions:

- `/etc/nginx/conf.d/ikigenba.conf` is exactly as it was before the command:
  the candidate was written, rejected, and the previous content put back.
- nginx was not reloaded and is serving what it was serving.
- `/run/ikigenba/services.json` is as it was before the command.

## An agent applies on a host with no host.name

Command:

```
$ sudo opsctl nginx show
```

```
$ sudo opsctl nginx apply
```

Output:

```
opsctl: host.name not set
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `host.name` is unset or empty.

Postconditions:

- Nothing has changed.

## An agent applies with an apex the host name cannot carry

A `host.apex` on a host whose name has no parent domain describes something
that cannot exist, so both subcommands refuse before rendering anything. The
store is the agent's to fix: either the key was set on the wrong host, or the
host's name is wrong.

Command:

```
$ sudo opsctl nginx show
```

```
$ sudo opsctl nginx apply
```

Output:

```
opsctl: host.apex is set but host.name 'ikigenba.dev' has no parent domain
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `host.name` is `ikigenba.dev` and `host.apex` is `crm`.

Postconditions:

- Nothing has changed. No file was written and nginx was not reloaded.

## An operator runs `nginx` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl nginx
```

Output:

```
opsctl: no nginx subcommand given

see 'opsctl nginx --help' for usage
```

Command:

```
$ sudo opsctl nginx reload
```

Output:

```
opsctl: unknown nginx subcommand 'reload'

see 'opsctl nginx --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
