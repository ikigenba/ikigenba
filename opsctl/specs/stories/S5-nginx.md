# Stories — nginx

Every request to the host arrives at nginx, and opsctl owns exactly one file
under `/etc/nginx`: `/etc/nginx/conf.d/ikigenba.conf`. That file is a pure
function of the configuration store and of what is on disk under `/opt`, so it
is generated rather than edited, and it is never backed up — a restored host
regenerates it.

The top-level usage gains the line `  nginx     generate the platform's nginx
configuration` under `Commands:`, and `init`'s sequence gains the step
`nginx.conf`.

A service is discovered, never registered: any `/opt/<name>/` holding an
`etc/` or a `state/` directory is one. A service is *routed* when it also has
an `etc/manifest.toml` naming a `port`; one without is known to the host but
gets no server block, which is what a service restored from backup but never
installed looks like, and what one that was uninstalled looks like. Routed services answer at `<name>.<host.name>`, and the
one whose manifest says `default = true` also answers at `<host.name>`.

A service's block carries the TLS frame, the app's own `etc/nginx.conf` if it
ships one, and a `location /` that proxies to the port its manifest names. The
app's file is included first, so a longer prefix in it — static files out of
`share/`, say — wins over the proxy; the glob makes the include harmless when
there is no such file.

One routed app is set apart by its name. A routed app named `auth` is the
platform's authenticator, and its presence rewrites every other routed app's
block: each gains an `auth_request` subrequest to `auth`'s `/check`, so a
request reaches the app only once `auth` has answered it a valid session — the
*wired* shape. In a wired block the client's own `X-User-Id` and `X-User-Email`
never reach the app; nginx sets them from `auth`'s answer instead, turns a 401
into a redirect to `auth`, and passes a 403 through. `auth`'s own block is left
*unwired*, and its `/check` answers 404 to any public request — it is reachable
only as that internal subrequest. Recognition is by a routed manifest, not the
name alone: an `/opt/auth/` with no `etc/manifest.toml` naming a port is not
the authenticator, and with no routed `auth` on the host every block is unwired
— the fail-open frame, which is what these stories show unless one says a
routed `auth` is present.

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
not routed — not installed, or installed without a port — the apex answers
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

Generate /etc/nginx/conf.d/ikigenba.conf from the configuration store and the
services under /opt. The file is generated, never edited; opsctl writes no
other file under /etc/nginx.

Subcommands:
  show   print the configuration opsctl would write
  apply  write the file, test it, and reload nginx

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none

A service is any /opt/<name>/ with an etc/ or state/ directory. One with an
etc/manifest.toml naming a port answers at <name>.<host.name>, and the one
whose manifest sets default answers at <host.name> as well. Its own
etc/nginx.conf, if it ships one, is included in its server block. The app
host.apex names also answers at the parent of host.name; until that app is
routed, the parent answers 404. A routed app named auth is the authenticator:
every other app's block then requires a valid session, checked against auth's
/check, while auth's own name is not gated.
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
name and its wildcard answer 404 under the host's certificate.

Command:

```
$ sudo opsctl nginx show
```

Output:

```
# Generated by opsctl. Do not edit; run 'opsctl nginx apply'.

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
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
    return              404;
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- No directory under `/opt/` holds an `etc/` or a `state/`.

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

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
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
    return              404;
}

server {
    listen              443 ssl;
    server_name         crm.sbx.ikigenba.dev sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;

    include /opt/crm/etc/nginx.conf*;

    location / {
        proxy_pass       http://127.0.0.1:3100;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen              443 ssl;
    server_name         dashboard.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;

    include /opt/dashboard/etc/nginx.conf*;

    location / {
        proxy_pass       http://127.0.0.1:3200;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- `/opt/crm/etc/manifest.toml` names `port = 3100` and `default = true`.
- `/opt/dashboard/etc/manifest.toml` names `port = 3200` and no `default`, or
  `default = false`.
- Neither app ships an `etc/nginx.conf`; the include matches nothing and
  nginx accepts it.
- `/opt/gmail/` holds a `state/` and no `etc/manifest.toml`.
- No app named `auth` is routed — there is no `/opt/auth/` with a manifest
  naming a port — so no block carries `auth_request` and the host is fail-open.

Postconditions:

- Nothing has changed. `gmail` has no block: it is a service the host will
  back up, and not one nginx can route.

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

    include /opt/dashboard/etc/nginx.conf*;

    location / {
        proxy_pass       http://127.0.0.1:3200;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
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

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
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
    return              404;
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `crm`.
- No directory under `/opt/` holds an `etc/` or a `state/`; or `/opt/crm/`
  holds a `state/` and no manifest naming a port.
- No routed `auth` is present; nothing is wired, and the 404 frame is
  unchanged.

Postconditions:

- Nothing has changed. With routed apps on the host, the 404 block's line
  would read `*.sbx.ikigenba.dev ikigenba.dev` when one of them is the
  default, and `sbx.ikigenba.dev *.sbx.ikigenba.dev ikigenba.dev` when none
  is.

## An operator reads the configuration of a host running apps behind the authenticator

A routed app named `auth` is on the host, so every other app's block is wired
to it. `auth` (port 3001) is not the default, so it answers only at
`auth.sbx.ikigenba.dev`; `crm` (port 3100) is still the default and still
answers at the space's name; `dashboard` (port 3200) answers at its own name.
The blocks come in ascending name order — `auth`, `crm`, `dashboard` — and
`auth`'s own is the one left unwired: its `/check` answers 404 to any direct
request and is reached only as the other blocks' internal subrequest. In each
wired block a client cannot forge identity: the subrequest to `/check` carries
no client `X-User-Id` or `X-User-Email`, and on a valid session nginx sets
those two headers on the upstream from `auth`'s answer. A 401 from `auth`
becomes a 302 redirect to `auth.sbx.ikigenba.dev` carrying the original request
URL as `return=`; a 403 reaches the client unchanged.

Command:

```
$ sudo opsctl nginx show
```

Output:

```
# Generated by opsctl. Do not edit; run 'opsctl nginx apply'.

server {
    listen      80 default_server;
    listen      [::]:80 default_server;
    server_name _;
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
    return              404;
}

server {
    listen              443 ssl;
    server_name         auth.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;

    include /opt/auth/etc/nginx.conf*;

    location = /check {
        return 404;
    }

    location / {
        proxy_pass       http://127.0.0.1:3001;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen              443 ssl;
    server_name         crm.sbx.ikigenba.dev sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;

    include /opt/crm/etc/nginx.conf*;

    location = /_ikigenba/check {
        internal;
        proxy_pass              http://127.0.0.1:3001/check;
        proxy_pass_request_body off;
        proxy_set_header        Content-Length "";
        proxy_set_header        X-User-Id    "";
        proxy_set_header        X-User-Email "";
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location / {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @auth_redirect;

        proxy_pass       http://127.0.0.1:3100;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }
}

server {
    listen              443 ssl;
    server_name         dashboard.sbx.ikigenba.dev;
    ssl_certificate     /etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sbx.ikigenba.dev/privkey.pem;

    include /opt/dashboard/etc/nginx.conf*;

    location = /_ikigenba/check {
        internal;
        proxy_pass              http://127.0.0.1:3001/check;
        proxy_pass_request_body off;
        proxy_set_header        Content-Length "";
        proxy_set_header        X-User-Id    "";
        proxy_set_header        X-User-Email "";
    }

    location @auth_redirect {
        return 302 https://auth.sbx.ikigenba.dev/?return=$scheme://$host$request_uri;
    }

    location / {
        auth_request     /_ikigenba/check;
        auth_request_set $auth_user_id    $upstream_http_x_user_id;
        auth_request_set $auth_user_email $upstream_http_x_user_email;
        error_page       401 = @auth_redirect;

        proxy_pass       http://127.0.0.1:3200;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-User-Id         $auth_user_id;
        proxy_set_header X-User-Email      $auth_user_email;
    }
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is not set.
- `/opt/auth/etc/manifest.toml` names `port = 3001` and no `default`, or
  `default = false`.
- `/opt/crm/etc/manifest.toml` names `port = 3100` and `default = true`.
- `/opt/dashboard/etc/manifest.toml` names `port = 3200` and no `default`, or
  `default = false`.
- No app ships an `etc/nginx.conf`; each include matches nothing and nginx
  accepts it.

Postconditions:

- Nothing has changed. `show` writes no file and reloads nothing.
- `auth` alone carries no `auth_request`; every other routed app carries it,
  purely because a routed `auth` is present — there is no per-app opt-in or
  opt-out.

## An operator reads the configuration where auth is present but not routed

An `/opt/auth/` is on disk, but it has no `etc/manifest.toml` naming a port — a
`state/` restored from backup, say, or an `etc/` that ships no manifest. It is
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
- `/opt/crm/etc/manifest.toml` names `port = 3100` and `default = true`;
  `/opt/dashboard/etc/manifest.toml` names `port = 3200` and no `default`.
- `/opt/auth/` holds a `state/`, or an `etc/` with no `manifest.toml` naming a
  `port`.

Postconditions:

- Nothing has changed.
- `auth` has no block: unrouted, it is a service the host will back up and one
  nginx cannot route, and it is not the authenticator. No block is wired; the
  host stays fail-open until a routed `auth` manifest names a port.

## An operator applies the configuration

`apply` writes the file, has nginx check the whole configuration, and reloads
it. Nothing is printed; the answer is the exit code.

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
