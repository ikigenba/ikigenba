# nginx inactive at init, 2026-09-17

An authoring observation supporting D06's apply requirements (`R-2NUY-GTYW`,
`R-2P2U-ULPL`), which replaced `R-5JCI-QYZD` and `R-W5Q7-VOVY`. It is not a
build run and no gate was involved.

## What was observed

Host: the live box `ikigenba.dev` (Amazon Linux 2023), a fresh space whose
first boot ran `systemctl enable nginx` and never started it. opsctl v0.1.1.
Run as root by an operator on 2026-09-17.

`sudo opsctl init` passed its whole preflight and the `certificate` step (a
real Let's Encrypt issuance through the route53 DNS-01 hooks), then failed
at `nginx.conf`. stderr, verbatim:

```
opsctl: systemctl reload nginx: exit status 1

> nginx.service is not active, cannot reload.
```

Before the failure, `Apply` had published `/etc/nginx/conf.d/ikigenba.conf`
and `nginx -t` had passed. `systemctl is-active nginx` reported `inactive`
and `systemctl is-enabled nginx` reported `enabled`. The unit file declares
`ExecReload=/usr/sbin/nginx -s reload`.

## What it proves

- `systemctl reload` fails with exit status 1 on an inactive unit, so
  `Apply` as specified by `R-5JCI-QYZD` could never complete on a host where
  nginx had not been started, and init could not heal that state.
- Nothing in D05's preflight or in the host's first boot guaranteed a running
  nginx; init was the only step that assumed one. init already brings
  litestream.service from disabled to running, so nginx was the one unit
  left to an unstated assumption.

## What the fix relies on

systemctl(1) documents `reload-or-restart` as: "Reload one or more units if
they support it. If not, stop and then start them instead. If the units are
not running yet, they will be started." With `ExecReload` declared, a running
nginx is therefore reloaded in place, and an inactive one is started. This is
published behavior of a well-known tool and is proven by that documentation;
it has not yet been exercised on the host, which is the next init run.

The certbot renewal hook in D07 stays `try-reload-or-restart`, which the same
page documents as doing nothing when the unit is not running. Renewal should
not start a server an operator stopped; setup should.

## Also seen, not addressed here

`nginx -t` warned `conflicting server name "_" on 0.0.0.0:80, ignored` because
the image's stock `nginx.conf` ships its own port-80 default server beside the
generated one. The warning is about the duplicate name `_`, which nginx never
matches against a real Host header anyway; the generated block carries
`default_server` and the stock block does not, so nginx's documented request
processing routes every port-80 request to the generated redirect. The stock
block is unreachable and the warning is cosmetic. Removing it means replacing
the distribution's `/etc/nginx/nginx.conf`. We decided in D06 that opsctl
writes only its own file under `/etc/nginx`, so the stock file stays the
image's, and that decision is worth keeping while the warning costs nothing:
the image's first boot is the natural place to trim the stock file if we ever
want it gone.
