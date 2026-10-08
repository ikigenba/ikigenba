# Stories — on a space

auth reached through a space: the file `S06-package.md` describes, deployed with
`devctl deploy`, installed by `opsctl`, and answered by nginx at `auth.<space>`
over TLS, which nginx passes on to auth (`S02-serve.md`). Deployed like any app,
auth is also the authenticator: once it is installed, the host's nginx routes
every other app through auth's `/check`, or `/check/open` for an app that serves
guests, an internal subrequest to auth, before serving it. opsctl refuses to
disable auth, so the authenticator is never switched off on a space. These
stories prove the whole path from checkout to browser — auth serving its own
sign-in page, and auth deciding another app's requests — and nothing about auth
that the earlier groups do not already say. devctl and opsctl are named only by
their published commands. The routing of other apps through `/check` and
`/check/open` is a property of the space, not of auth: it is added by opsctl's
nginx generation (a separate sub-project) and is named here only by its
observable effect, the way dummy's `S7-on-a-space.md` names devctl and opsctl
only by their published commands. `dummy` is the example protected app, deployed
on the same space through its own `S7-on-a-space.md` chain. The `telemetry`
service is deployed and active on the same space too, so the host's services
file has an entry named `telemetry`, and every event auth records reaches the
trail (`S02-serve.md`). On the `/check` and `/check/open` subrequests the space's
nginx names the request it is deciding in `X-Original-Method`,
`X-Original-Host`, and `X-Original-URI` (`S04-check.md`), and gives it the same
`X-Request-Id` as the request it forwards to the app, so auth's check event and
the app's own record of the request share one request id.

## A visitor reaches auth on a space

A visitor with no session reaches auth's own hostname over TLS and gets the
sign-in page. auth's own server block carries no `auth_request`, so auth
answers this request itself.

Request:

```
$ curl -si https://auth.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the sign-in page (`S03-sign-in.md`): an HTML page
whose title is `auth`, which links `/_appkit/theme.css` as its stylesheet,
which links `/_appkit/favicon.svg` as its icon, which loads
`/_appkit/feedback.js`, and whose visible text includes the
heading `Sign in to ikigenba.dev` and a link to
`/login/google`. The sign-in page has no banner, so it carries no launcher,
whatever the host's services file lists.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its
  instance is `running`, and `opsctl` is installed on it.
- `devctl build auth`, run in a clean tree at the commit `<sha>`, wrote
  `auth/dist/auth-<sha>.tar.xz` (`S06-package.md`). No tag is needed.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev auth/dist/auth-<sha>.tar.xz`
  exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows
  auth's service and socket `active`, in the layout devctl's and opsctl's
  stories own.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- Nothing has changed.
- auth records `request.started` with `method=GET` and `path=/`, then
  `request.finished` with `status=200`, `duration_us=<microseconds>`,
  `request_bytes=0`, and `response_bytes=<bytes>`, both under request id
  `<request-id>`, the one nginx gave the request, and no user.

## A user on a space opens the service launcher

On a space the host sets `IKIGENBA_SERVICES` in auth's environment to the
path of its services file (`S02-serve.md`), and that file lists auth with an
icon because auth's package ships `share/icon.svg` (`S06-package.md`).
So the profile a
signed-in user reaches at auth's own hostname carries the launcher in its
banner, and auth is one of the services it offers. The launcher's text and
behavior are `S03-sign-in.md`'s; this story fixes only what the user sees on a
space.

Request:

```
$ curl -si --cookie 'ikigenba_session=<opaque>' https://auth.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the profile (`S03-sign-in.md`), drawn with the banner,
and its stylesheet is `https://auth.sbx.ikigenba.dev/_appkit/theme.css`. Its
banner carries the launcher button labelled `Services`. In a browser, pressing
the button opens a list of the space's services with a search box labelled
`Find a service`; each entry shows a service's icon and name, as
`S03-sign-in.md` tells. auth's own entry is in the list and is marked as the
current page. The launcher's script is
`https://auth.sbx.ikigenba.dev/_appkit/launcher.js` (`S08-assets.md`), so the
launcher, like the style, needs nothing from any other origin. Its button
feedback script is `https://auth.sbx.ikigenba.dev/_appkit/feedback.js` and
its icon `https://auth.sbx.ikigenba.dev/_appkit/favicon.svg`, both from the
same host.

Preconditions:

- The auth deploy chain above holds: the space exists in account
  `602773793009`, its instance is `running`, `opsctl` is installed,
  `auth/dist/auth-<sha>.tar.xz` exists, the `devctl deploy` of auth exited 0,
  and `space status` shows auth's service and socket `active`.
- `auth/dist/auth-<sha>.tar.xz` holds `share/icon.svg` (`S06-package.md`).
- The host sets `IKIGENBA_SERVICES` in auth's environment to the path of its
  services file, and that file lists auth with its icon.
- The request carries an `ikigenba_session` cookie naming a live session on
  this space, from a sign-in through Google at
  `https://auth.sbx.ikigenba.dev/`.

Postconditions:

- Nothing has changed. Serving the profile does not touch the session
  (`S03-sign-in.md`).

## A visitor reaches an app on a space without signing in

A visitor asks a protected app for a page with no accepted credential. The
host's nginx asks auth's `/check` first, auth answers 401 (`S04-check.md`), and
nginx turns that into a redirect to auth's sign-in page carrying the original
URL to return to. auth itself does not serve this request; it only decides it,
and the space's nginx executes the redirect.

Request:

```
$ curl -si https://dummy.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https://dummy.sbx.ikigenba.dev/
```

Status 302. The visitor is sent to auth's sign-in page with the original URL as
`return`; the body is not fixed.

Preconditions:

- The auth deploy chain above holds: the space exists in account
  `602773793009`, its instance is `running`, `opsctl` is installed,
  `auth/dist/auth-<sha>.tar.xz` exists, the `devctl deploy` of auth exited 0,
  and `space status` shows auth's service and socket `active`.
- `dummy` is deployed and active on the same space through its own
  `S7-on-a-space.md` chain, so `space status` also shows dummy's service and
  socket `active`.
- The host's nginx routes every app other than auth through auth's `/check`
  before serving it, naming the request on the subrequest in
  `X-Original-Method`, `X-Original-Host`, and `X-Original-URI`: a request with
  no accepted credential is answered by a redirect to
  `https://auth.sbx.ikigenba.dev/?return=<original URL>`. This routing is a
  property of the space, added by opsctl's nginx generation (a separate
  sub-project); a space without it serves apps unauthenticated.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- Nothing has changed. No session was created; `/check` had no credential to
  count as use.
- auth records `check.refused` with `outcome=unauthenticated`,
  `credential=none`, `method=GET`, `host=dummy.sbx.ikigenba.dev`, and
  `path=/`, under request id `<request-id>`, the one nginx gave the request,
  and no user. It comes between the `request.started` and `request.finished`
  of the `/check` subrequest, whose `request.finished` has `status=401`. dummy
  never saw the request, so this is the trail's only record of it.

## An agent reaches an app on a space with a token

An agent asks a protected app for a page carrying a personal access token. The
host's nginx asks auth's `/check`, auth answers 200 with `X-User-Id` and
`X-User-Email` (`S04-check.md`), nginx sets those two headers on the request it
forwards to the app — removing any `X-User-*` the agent supplied — and the app
answers. The app sees the identity headers; what it does with them is the app's
own behavior, not auth's.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://dummy.sbx.ikigenba.dev/widgets
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The app answered its own page (for `dummy`, the panel of
`S3-panel.md`): an HTML document whose visible text carries the caller's email
address and the widgets that exist. The request reached it with `X-User-Id` and
`X-User-Email` set from auth's answer, so the email the page shows is the one
auth sent — the token's owner. Had the agent set its own `X-User-Id` or
`X-User-Email` on the request, the space would have stripped it before the app
saw it, so the identity the app reads is always auth's.

Preconditions:

- The auth deploy chain above holds, and `dummy` is deployed and active on the
  same space through its own `S7-on-a-space.md` chain.
- The host's nginx routes every app other than auth through auth's `/check`
  before serving it, naming the request on the subrequest in
  `X-Original-Method`, `X-Original-Host`, and `X-Original-URI`: a request that
  `/check` approves is forwarded to the app with `X-User-Id` and
  `X-User-Email` set from auth's answer and any client-supplied `X-User-*`
  removed. This routing is a property of the space, added by opsctl's nginx
  generation (a separate sub-project).
- The agent holds a valid token `ikp_<token>` (`S05-tokens.md`) whose id is
  `<token-id>`, `tok_` followed by 26 Crockford base32 characters, and whose
  owner, the user `<user-id>`, signed in through Google within the last 30
  days, so the token is honored.

Postconditions:

- The token's last-used time is updated by the `/check` subrequest
  (`S04-check.md`); nothing else has changed.
- auth records `check.allowed` with `outcome=allowed`, `credential=token`,
  `method=GET`, `host=dummy.sbx.ikigenba.dev`, `path=/widgets`, and
  `token=<token-id>`, under request id `<request-id>`, the one nginx gave the
  request, and user `<user-id>`. The token's secret appears nowhere in the
  trail.

## A visitor asks a space for the check endpoint

`/check` is meant only for nginx's internal subrequest to auth.
auth's own hostname answers a public `/check` with 404 by design. The space's
own host answers 404 too on this space, but because no app answers at the bare
space name here — so this path falls to the catch-all like any other — not
because `/check` is singled out there.

Request:

```
$ curl -si https://auth.sbx.ikigenba.dev/check
```

```
$ curl -si https://sbx.ikigenba.dev/check
```

Response:

```
HTTP/2 404
```

Status 404. Both forms answer 404, for different reasons: auth's own host holds
`/check` behind a 404 by design, while the bare space host answers 404 to every
path because no app answers at that name on this space. Neither reaches the
internal subrequest nginx makes to auth. The body is not fixed.

Preconditions:

- The auth deploy chain above holds: the space exists in account
  `602773793009`, its instance is `running`, `opsctl` is installed, and auth is
  deployed and active.
- No app deployed on this space is the default: `auth` and `dummy` both declare
  `default = false`, so nothing answers at the bare space name
  `sbx.ikigenba.dev`, and it answers 404 to every path.
- The space's nginx keeps `/check` off the public side: auth's own server block
  answers a public `/check` with 404, and on any other app's host `/check` is
  not a public endpoint — it is treated like any other path and taken through
  the space's normal auth flow, never the internal subrequest nginx makes to
  auth. This is a property of the space's nginx (opsctl's
  generation, a separate sub-project), named here only by its observable effect;
  it is not something auth can do alone, because a public `/check` and an
  nginx subrequest `/check` reach auth as the same request.

Postconditions:

- Nothing has changed.
- Neither request reached auth, so auth records no event for either: no check
  event, and no `request.started`.

## A visitor asks a space for the open check endpoint

`/check/open` is meant only for nginx's internal subrequest to auth, exactly
as `/check` is. auth's own hostname answers a public `/check/open` with 404 by
design. The space's own host answers 404 too on this space, but because no app
answers at the bare space name here — so this path falls to the catch-all like
any other — not because `/check/open` is singled out there.

Request:

```
$ curl -si https://auth.sbx.ikigenba.dev/check/open
```

```
$ curl -si https://sbx.ikigenba.dev/check/open
```

Response:

```
HTTP/2 404
```

Status 404. Both forms answer 404, for different reasons: auth's own host holds
`/check/open` behind a 404 by design, while the bare space host answers 404 to
every path because no app answers at that name on this space. Neither reaches
the internal subrequest nginx makes to auth. The body is not fixed.

Preconditions:

- The auth deploy chain above holds: the space exists in account
  `602773793009`, its instance is `running`, `opsctl` is installed, and auth is
  deployed and active.
- No app deployed on this space is the default: `auth` and `dummy` both declare
  `default = false`, so nothing answers at the bare space name
  `sbx.ikigenba.dev`, and it answers 404 to every path.
- The space's nginx keeps `/check/open` off the public side: auth's own server
  block answers a public `/check/open` with 404, and on any other app's host
  `/check/open` is not a public endpoint — it is treated like any other path
  and taken through the space's normal auth flow, never the internal
  subrequest nginx makes to auth. This is a property of the space's nginx
  (opsctl's generation, a separate sub-project), named here only by its
  observable effect; it is not something auth can do alone, because a public
  `/check/open` and an nginx subrequest `/check/open` reach auth as the same
  request.

Postconditions:

- Nothing has changed.
- Neither request reached auth, so auth records no event for either: no check
  event, and no `request.started`.
