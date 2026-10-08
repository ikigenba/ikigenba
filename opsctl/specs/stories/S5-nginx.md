# Stories — nginx

Every request to the host arrives at nginx, and opsctl owns exactly one file
under `/etc/nginx`: `/etc/nginx/conf.d/ikigenba.conf`. That file is a pure
function of the configuration store, of what is on disk under `/opt`, and of
which apps systemd reports disabled, so it is generated rather than edited, and it is never backed up — a restored host
regenerates it.

The top-level usage gains the line `  nginx     generate the platform's nginx
configuration` under `Commands:`, and `init`'s sequence gains the step
`nginx.conf`.

A service is discovered, never registered: any `/opt/<name>/` holding an
`etc/`, or any `/var/opt/ikigenba/<name>/` holding a `state/`, is one. A
service is *routed* when it also has an `/opt/<name>/etc/manifest.toml` naming
its `app`; one without is known to the host but gets no server block, which
is what a service with only a `/var/opt/ikigenba/<name>/state/` looks like,
and what one that was uninstalled looks like. Routed services answer at `<name>.<host.name>`, and the
one whose manifest says `default = true` also answers at `<host.name>`.

A routed app is *disabled* when systemd reports its socket unit,
`ikigenba-<name>.socket`, disabled — the state `opsctl disable` leaves it in
(`S7-apps.md`). Nothing else records it: nginx asks systemd, the way `status`
does. A disabled app keeps its block and every name it answers at, but the
block answers `503` to every request and neither includes the app's own
`etc/nginx.conf` nor proxies anywhere, so a disabled app reads as unavailable
rather than as missing. A routed service with no units at all — a restore
that brought a manifest and no binary — is not disabled; it is routed as any
other.

A service's block carries the TLS frame, the app's own `etc/nginx.conf` if it
ships one, and a `location /` that proxies to the app's Unix socket,
`/run/ikigenba/<name>.sock`. No app listens on a TCP port: the socket is held
by the app's socket unit (`S7-apps.md`), readable and writable by nginx and by
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
whether or not any app serves guests. Recognition is by a routed manifest, not the
name alone: an `auth` service with no `/opt/auth/etc/manifest.toml` naming
its `app` is not the authenticator, and with no routed `auth` on the host every block is unwired
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
`guests = true` in its manifest (`S7-apps.md`); with no `guests`, or `guests =
false`, it does not. On a host with a routed `auth`, such an app's wired block
differs from any other in two places. It gains a second internal check,
`/_ikigenba/check/open`, beside `/_ikigenba/check` and identical to it except
that it asks `auth`'s `/check/open`, which answers exactly as `/check` does
except that where `/check` would answer 401 it answers 200 with no identity.
And its `location /` asks that check instead and has no `error_page 401`, so
nothing there is sent to sign in: a guest reaches the app with no `X-User-Id`
or `X-User-Email` and with nginx's request id, a signed-in visitor reaches it
with the identity `auth` relays, and a 403 from `auth` still reaches the client
unchanged. `/mcp`, `/mcp/...`, and the git paths are not part of it: they keep
the strict `/_ikigenba/check` and their 401 challenges, exactly as on any
wired block. When the app also holds the apex, the apex is a name on that same
block and is served the same way. `guests` changes nothing else: on a host
with no authenticator the app gets the plain block every app gets, a disabled
app's block answers 503 as any other, and `auth`'s own block is the same with
or without such an app.

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
not routed — not installed, or restored with no manifest — the apex answers
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
services under /opt, and which apps systemd reports disabled. The file is generated, never edited; opsctl writes no
other file under /etc/nginx.

Subcommands:
  show   print the configuration opsctl would write
  apply  write the file, test it, and reload nginx

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none

A service is any /opt/<name>/ with an etc/ directory, or any
/var/opt/ikigenba/<name>/ with a state/ directory. One with an
/opt/<name>/etc/manifest.toml naming its app answers at <name>.<host.name>,
proxied to its socket /run/ikigenba/<name>.sock, and the one whose manifest
sets default answers at <host.name> as well. Its own etc/nginx.conf, if it ships one, is
included in its server block. An app whose socket unit systemd reports
disabled keeps its names, and its block answers 503. Every proxied request
carries X-Request-Id set to nginx's own request id, which also ends its
access-log line. The app
host.apex names also answers at the parent of host.name; until that app is
routed, the parent answers 404. A routed app named auth is the authenticator:
every other app's block then requires a valid session, checked against auth's
/check, while auth's own name is not gated. Under /mcp, a request without a
valid credential, or with one auth refuses, is answered 401 naming the MCP
gateway's protected-resource metadata instead of being sent to sign in or
refused; a git smart HTTP request without a credential is answered 401 too,
with a Basic challenge so git asks for the token. An app whose manifest sets
guests admits a request without a credential elsewhere, checked against
auth's /check/open.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator reads the configuration a bare host would get

Before any app is installed, the file is the frame: port 80 redirects
everything to 443, an unknown name has its TLS handshake rejected outright
rather than being answered with someone else's certificate, and the host's own
name and its wildcard answer 404 under the host's certificate. The
`ikigenba` log format comes first, and every block that answers a request
logs in it; the handshake-rejecting block answers none.

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
- No directory under `/opt/` holds an `etc/`, and none under
  `/var/opt/ikigenba/` holds a `state/`.

Postconditions:

- Nothing has changed. `show` writes no file and reloads nothing.
- No `/opt/<name>/` is routed, so there is no `auth` to recognize and nothing
  to wire; the frame is fail-open because nothing is installed, not by choice.

## An operator reads the configuration of a host running apps

Each routed service gains one block, in name order, at its own name under the
host's wildcard certificate. `crm` is the default app, so the space's name
answers from it and leaves the 404 block holding the wildcard alone. No routed
app here is named `auth`, so the host has no authenticator: every block is the
plain proxy, with no `auth_request` and no identity headers set from a
subrequest. This is the fail-open case, shown for what it is — the honest
absence of an authenticator, not a per-app security choice.

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

    include /opt/crm/etc/nginx.conf*;

    location / {
        proxy_pass       http://unix:/run/ikigenba/crm.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
    }
}

server {
    listen              443 ssl;
    server_name         dashboard.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;
    access_log          /var/log/nginx/access.log ikigenba;

    include /opt/dashboard/etc/nginx.conf*;

    location / {
        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- `/opt/crm/etc/manifest.toml` names `app = "crm"` and `default = true`.
- `/opt/dashboard/etc/manifest.toml` names `app = "dashboard"` and no `default`, or
  `default = false`.
- Neither app ships an `etc/nginx.conf`; the include matches nothing and
  nginx accepts it.
- `/var/opt/ikigenba/gmail/` holds a `state/`, and there is no
  `/opt/gmail/etc/manifest.toml`.
- No app named `auth` is routed — there is no `/opt/auth/` with a manifest
  naming its `app` — so no block carries `auth_request` and the host is fail-open.

Postconditions:

- Nothing has changed. `gmail` has no block: it is a service the host will
  back up, and not one nginx can route.
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

    include /opt/dashboard/etc/nginx.conf*;

    location / {
        proxy_pass       http://unix:/run/ikigenba/dashboard.sock:;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- As for the previous story, and `host.apex` is `dashboard`.
- No routed `auth` is on the host, so the blocks keep the plain proxy shape
  shown; the apex changes only which names a block answers at, not its wiring.
- The host's certificate covers `ikigenba.dev` as well (see
  `S6-certificates.md`); `show` does not check, since it writes nothing.

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
host's certificate instead of falling to the 404 block. `dashboard` is
unaffected.

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
- Once applied, every request to `crm`'s names is answered `503` by nginx,
  and nothing reaches `/run/ikigenba/crm.sock`, which does not exist while
  `crm` is disabled. Had `crm` also held the apex, `ikigenba.dev` would answer
  `503` from the same block.

## An operator reads the configuration when the apex app is not routed

`host.apex` names an app that is not installed — not yet deployed, or taken
off with `uninstall`, or restored from backup with no binary. The apex still
belongs to this host, so its name goes on the 404 block beside the space's
name and wildcard, and answers 404 under the host's certificate rather than
having its handshake rejected. The next `install` of that app moves the name
to the app's block.

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
- No directory under `/opt/` holds an `etc/`, and none under
  `/var/opt/ikigenba/` holds a `state/`; or `/var/opt/ikigenba/crm/` holds a
  `state/` and there is no `/opt/crm/etc/manifest.toml`.
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
space's name; `dashboard` answers at its own name.
The blocks come in ascending name order — `auth`, `crm`, `dashboard` — and
`auth`'s own is the one left unwired: its `/check` and `/check/open` answer
404 to any direct request, and `/check` is reached only as the other blocks'
internal subrequest. In each
wired block a client cannot forge identity: the subrequest to `/check` carries
no client `X-User-Id` or `X-User-Email`, and on a valid session nginx sets
those two headers on the upstream from `auth`'s answer. The subrequest also
carries the original request's method, host, and request target as
`X-Original-Method`, `X-Original-Host`, and `X-Original-URI`. A 401 from `auth`
becomes a 302 redirect to `auth.sbx.ikigenba.dev` carrying the original request
URL as `return=`; a 403 reaches the client unchanged. Between the redirect and
`location /`, each wired block carries the MCP locations: `/mcp` exactly and
everything under `/mcp/` are checked and relayed like `location /`, but a 401
from `auth` is answered by `@mcp_unauthorized` — the `401` with its
`WWW-Authenticate` header and one-line body — instead of the redirect, and a
403 from `auth` by `@mcp_invalid_token`, the `invalid_token` 401 with the same
body. Both headers name `https://mcp.sbx.ikigenba.dev`, the gateway's origin
on this host, though no `mcp` app is installed here. After
them comes the git location: a path ending `/info/refs`, `/git-upload-pack`,
or `/git-receive-pack` is checked and relayed the same way, and a 401 from
`auth` is answered by `@git_unauthorized`, a Basic challenge. The `\n` in
each `return` is the two characters backslash and `n` in the file.

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

    include /opt/auth/etc/nginx.conf*;

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

    include /opt/crm/etc/nginx.conf*;

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
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location @mcp_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate 'Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @mcp_invalid_token {
        default_type text/plain;
        add_header   WWW-Authenticate 'Bearer error="invalid_token", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @git_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate 'Basic realm="ikigenba"' always;
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

    include /opt/dashboard/etc/nginx.conf*;

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
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location @mcp_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate 'Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @mcp_invalid_token {
        default_type text/plain;
        add_header   WWW-Authenticate 'Bearer error="invalid_token", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @git_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate 'Basic realm="ikigenba"' always;
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
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- `/opt/auth/etc/manifest.toml` names `app = "auth"` and no `default`, or
  `default = false`.
- `/opt/crm/etc/manifest.toml` names `app = "crm"` and `default = true`.
- `/opt/dashboard/etc/manifest.toml` names `app = "dashboard"` and no `default`, or
  `default = false`.
- No app ships an `etc/nginx.conf`; each include matches nothing and nginx
  accepts it.

Postconditions:

- Nothing has changed. `show` writes no file and reloads nothing.
- `auth` alone carries no `auth_request`; every other routed app carries it,
  purely because a routed `auth` is present — there is no per-app opt-in or
  opt-out. Neither manifest sets `guests`, so neither block carries
  `/_ikigenba/check/open`; `auth`'s block guards `/check/open` all the same.
- `crm` and `dashboard` carry the MCP locations and the git location whether
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
with no `error_page 401` line. Its `/mcp` locations and git location are
unchanged, still asking `/_ikigenba/check` and still answering 401 with their
challenges, and `@auth_redirect` is still there though `location /` no longer
uses it. The apex, `ikigenba.dev`, is a name on the same block, so it is
served the same way. `auth`'s block is the one every wired host gets.

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

    include /opt/auth/etc/nginx.conf*;

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

    include /opt/sites/etc/nginx.conf*;

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
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location @mcp_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate 'Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @mcp_invalid_token {
        default_type text/plain;
        add_header   WWW-Authenticate 'Bearer error="invalid_token", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"' always;
        return       401 "authentication required: send Authorization: Bearer <token>\n";
    }

    location @git_unauthorized {
        default_type text/plain;
        add_header   WWW-Authenticate 'Basic realm="ikigenba"' always;
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
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `sites`.
- `/opt/auth/etc/manifest.toml` names `app = "auth"` and no `default`, or
  `default = false`.
- `/opt/sites/etc/manifest.toml` names `app = "sites"` and `guests = true`,
  and no `default`, or `default = false`.
- No app ships an `etc/nginx.conf`; each include matches nothing and nginx
  accepts it.
- The host's certificate covers `ikigenba.dev` as well (see
  `S6-certificates.md`); `show` does not check, since it writes nothing.

Postconditions:

- Nothing has changed. `show` writes no file and reloads nothing.
- `guests` reaches only the block of the app that sets it. Had `crm` of the
  `host running apps behind the authenticator` story been installed beside
  `sites`, its block would be exactly as that story shows.
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

An `auth` service is on disk, but there is no `/opt/auth/etc/manifest.toml`
naming its app — the `/var/opt/ikigenba/auth/state/` an uninstall leaves
behind, say, or an `/opt/auth/etc/` that ships no manifest. It is
a known service but not a routed one, so it is not the authenticator: there is
nowhere to send a subrequest. Recognition takes a routed manifest, not the
directory and not the name, so the host stays fail-open and `auth` gets no
block of its own.

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
- `/opt/crm/etc/manifest.toml` names `app = "crm"` and `default = true`;
  `/opt/dashboard/etc/manifest.toml` names `app = "dashboard"` and no
  `default`.
- `/var/opt/ikigenba/auth/` holds a `state/` and there is no `/opt/auth/`,
  or `/opt/auth/` holds an `etc/` with no `manifest.toml` naming its `app`.

Postconditions:

- Nothing has changed.
- `auth` has no block: unrouted, it is a service the host will back up and one
  nginx cannot route, and it is not the authenticator. No block is wired; the
  host stays fail-open until an `auth` manifest naming its app is in place.

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
- Any path outside `/mcp`, `/mcp/...`, and the git paths behaves the same:
  `https://sites.sbx.ikigenba.dev/mcpx` reaches `sites` as a guest too.
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

`/opt/crm/etc/nginx.conf`:

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
`include /opt/crm/etc/nginx.conf*;` already in `crm`'s block is what brings
it in.

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The host is the one in the `host running apps behind the authenticator`
  story, and `/opt/crm/etc/nginx.conf` holds the three lines above.

Postconditions:

- Nothing has changed.
- Once applied, `nginx -t` passes, and in `crm`'s block every location —
  `/`, `/mcp`, `/mcp/`, and the git location — accepts a request body of any
  size, waits up to an hour between reads from `crm`'s socket, and passes a
  request body to `crm` as it arrives. A push of several hundred MiB to
  `crm` is admitted by `/check` and streamed to the app.
- The subrequest to `/check` carries no body whatever the fragment says, so
  `auth` is unaffected.
- `dashboard`'s and `auth`'s blocks keep nginx's defaults: a body larger
  than 1 MiB sent to `dashboard` is refused `413`. The fragment reaches only
  the app that ships it.

## An operator applies the configuration

`apply` writes the file, has nginx check the whole configuration, and reloads
it. It also rewrites the services file, `/var/lib/ikigenba/services.json`
(`S9-services.md`), so the two never disagree about which apps are disabled.
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
- `nginx` and `systemctl` are on the PATH.

Postconditions:

- `/etc/nginx/conf.d/ikigenba.conf` holds byte for byte what `show` prints,
  with mode `0644`, written to a temporary file in the same directory and
  renamed over the old one, so nginx never reads a half-written file.
- `nginx -t` passed and nginx has been reloaded; it is serving the new
  configuration and never stopped serving the old one.
- No other file under `/etc/nginx` was created, modified, or removed.
- `/var/lib/ikigenba/services.json` has been rewritten from the same store,
  `/opt`, and disabled apps the nginx file was. Nothing was printed about it.
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
- `/var/lib/ikigenba/services.json` is as it was before the command.

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
