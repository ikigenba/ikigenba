# Stories — on a space

The panel reached through a space: the tree `S6-package.md` describes,
deployed in the suite release with `devctl deploy`, activated by `opsctl`, and
answered by nginx at
`dummy.<space>` over TLS. A space is one label under the root domain and an
app is `<app>.<space>`, so dummy on the space `sbx.ikigenba.dev` answers at
`dummy.sbx.ikigenba.dev`. nginx on the space proxies to dummy's socket,
`/run/ikigenba/dummy.sock` (`S2`). The space authenticates every request
before it reaches dummy and passes the caller on in `X-User-Id` and
`X-User-Email`, with the request's id in `X-Request-Id`; dummy itself has no
unauthenticated case, so a request that arrives at all is one of a known
caller. The stories prove the whole path from checkout to browser and nothing
about dummy that the earlier groups do not already say. The same deployment
also offers dummy's MCP endpoint, at `https://dummy.<space>/mcp` and through
the space's MCP gateway; those stories are `S9-mcp.md`'s. devctl and opsctl
are named only by their published commands.

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

Status 200. The body is an HTML page whose banner's profile link is titled
with the email address of the caller the gate authenticated, whose visible
text carries a table of the widgets that exist, and whose footer reads
`dummy <display>`, where `<display>` is whatever display string the host's
environment gives dummy: the string the deployed binary's `dummy --version`
prints under that same environment (`S1`), and empty when the host sets
neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`. Its stylesheet is
`https://dummy.sbx.ikigenba.dev/_appkit/theme.css`, and the fonts that
stylesheet loads are under the same `https://dummy.sbx.ikigenba.dev/_appkit/`
(`S8-assets.md`): a browser showing the panel requests its style from dummy's
own host and from no other origin, Google Fonts included. Its button feedback
script is `https://dummy.sbx.ikigenba.dev/_appkit/feedback.js` and its icon
`https://dummy.sbx.ikigenba.dev/_appkit/favicon.svg`, both from the same host.
In the banner, the profile link leads to `https://auth.sbx.ikigenba.dev/`,
their profile in auth on the same space, and the `Sign out` button is in a
form that POSTs to `https://auth.sbx.ikigenba.dev/logout` (`S3`); submitting
it signs the visitor out of the space, as auth's stories tell.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance
  is `running`, and `opsctl` is installed on it.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev <sha>` exited 0,
  making the suite release at the commit `<sha>` active (`S6-package.md`).
  No tag is needed.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows
  dummy's service and socket `active`, in the layout devctl's and opsctl's
  stories own.
- The space routes `dummy.sbx.ikigenba.dev` through its authenticating gate:
  the gate admits the request and sets `X-User-Id` and `X-User-Email` on what
  it passes to dummy, and refuses a request it cannot authenticate before
  dummy sees it.
- The caller holds a credential the gate accepts, and the email that
  credential names is the one the panel's profile link is titled with.

Postconditions:

- Nothing has changed.

## A visitor on a space opens the service launcher

On a space the host sets `IKIGENBA_SERVICES` in dummy's environment to the
path of its services file (`S2`). That file lists every service installed on
the host, and dummy's entry carries an icon because dummy's package ships
`share/icon.svg` (`S6-package.md`), which is what puts dummy in the launcher
(`S3`).
So the panel a visitor reaches on a space carries the launcher in its banner,
and dummy is one of the services it offers. The launcher's text and behavior
are `S3`'s; this story fixes only what the visitor sees on a space.

Request:

```
$ curl -si https://dummy.sbx.ikigenba.dev/widgets
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the panel page of the story above, and its banner
carries the launcher button (`S3`). In a browser, pressing the button opens a
list of the space's services with a search box labelled `Find a service`;
each entry shows a service's icon and name, as `S3` tells. dummy's own entry
is in the list and is marked as the current page. The launcher's script is
`https://dummy.sbx.ikigenba.dev/_appkit/launcher.js` (`S8-assets.md`), so the
launcher, like the style, needs nothing from any other origin.

Preconditions:

- Everything the story above requires holds: dummy is deployed
  and active on `sbx.ikigenba.dev`, and the caller holds a credential the gate
  accepts.
- The release's `<sha>/dummy/` holds `share/icon.svg` (`S6-package.md`).
- The host sets `IKIGENBA_SERVICES` in dummy's environment to the path of
  its services file, and that file lists dummy with its icon.

Postconditions:

- Nothing has changed.
