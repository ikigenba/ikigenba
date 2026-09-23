# Stories — on a space

The panel reached through a space: the file `S6-package.md` describes,
deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at
`dummy.<space>` over TLS. A space is one label under the root domain and an
app is `<app>.<space>`, so dummy on the space `sbx.ikigenba.dev` answers at
`dummy.sbx.ikigenba.dev`. nginx on the space proxies to dummy's socket,
`/run/ikigenba/dummy.sock` (`S2`). The space authenticates every request
before it reaches dummy and passes the caller on in `X-User-Id` and
`X-User-Email`, with the request's id in `X-Request-Id`;
dummy itself has no unauthenticated case, so a request that arrives at all is
one of a known caller. The story proves the whole path from checkout to
browser and nothing about dummy that the earlier groups do not already say.
devctl and opsctl are named only by their published commands.

## A visitor reaches the dummy panel on a space

The visitor asks for the panel over TLS at dummy's hostname on the space. The
gate in front of dummy authenticates the request and hands dummy the caller's
identity, and dummy renders the panel for that caller.

Request:

```
$ curl -si https://dummy.sbx.ikigenba.dev/widgets
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is an HTML page whose visible text carries the email
address of the caller the gate authenticated and a table of the widgets that
exist.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance
  is `running`, and `opsctl` is installed on it.
- A tag `dummy/v<semver>` points at the commit `devctl build dummy` was run
  at, and it wrote `dummy/dist/dummy-v<semver>.tar.xz`.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev dummy/dist/dummy-v<semver>.tar.xz`
  exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows
  `dummy v<semver> active active -`.
- The space routes `dummy.sbx.ikigenba.dev` through its authenticating gate:
  the gate admits the request and sets `X-User-Id` and `X-User-Email` on what
  it passes to dummy, and refuses a request it cannot authenticate before
  dummy sees it.
- The caller holds a credential the gate accepts, and the email that
  credential names is the one the panel shows.

Postconditions:

- Nothing has changed.
