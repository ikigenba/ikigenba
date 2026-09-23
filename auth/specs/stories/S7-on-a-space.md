# Stories — on a space

auth reached through a space: the file `S6-package.md` describes, deployed with
`devctl deploy`, installed by `opsctl`, and answered by nginx at `auth.<space>`
over TLS, which nginx proxies to auth's socket, `/run/ikigenba/auth.sock`
(`S2-serve.md`). Deployed like any app, auth is also the authenticator: once
it is installed, the host's nginx routes every other app through auth's
`/check`, a subrequest to that same socket, before serving it. opsctl refuses
to disable auth, so the authenticator is never switched off on a space. These stories prove the whole path from checkout to browser
— auth serving its own sign-in page, and auth deciding another app's requests —
and nothing about auth that the earlier groups do not already say. devctl and
opsctl are named only by their published commands. The routing of other apps
through `/check` is a property of the space, not of auth: it is added by
opsctl's nginx generation (a separate sub-project) and is named here only by
its observable effect, the way dummy's `S7-on-a-space.md` names devctl and
opsctl only by their published commands. `dummy` is the example protected app,
deployed on the same space through its own `S7-on-a-space.md` chain.

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

Status 200. The body is an HTML page containing a link to `/login/google`.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its
  instance is `running`, and `opsctl` is installed on it.
- A tag `auth/v<semver>` points at the commit `devctl build auth` was run at,
  and it wrote `auth/dist/auth-v<semver>.tar.xz`.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev auth/dist/auth-v<semver>.tar.xz`
  exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows
  `auth v<semver> active active -`.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- Nothing has changed.

## A visitor reaches an app on a space without signing in

A visitor asks a protected app for a page with no accepted credential. The
host's nginx asks auth's `/check` first, auth answers 401 (`S4-check.md`), and
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
  `602773793009`, its instance is `running`, `opsctl` is installed, the
  `auth/v<semver>` tag and `auth/dist/auth-v<semver>.tar.xz` exist, the
  `devctl deploy` of auth exited 0, and `space status` shows `auth v<semver>
  active active -`.
- `dummy` is deployed and active on the same space through its own
  `S7-on-a-space.md` chain, so `space status` also shows `dummy v<semver>
  active active -`.
- The host's nginx routes every app other than auth through auth's `/check`
  before serving it: a request with no accepted credential is answered by a
  redirect to `https://auth.sbx.ikigenba.dev/?return=<original URL>`. This
  routing is a property of the space, added by opsctl's nginx generation (a
  separate sub-project); a space without it serves apps unauthenticated.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- Nothing has changed. No session was created; `/check` had no credential to
  count as use.

## An agent reaches an app on a space with a token

An agent asks a protected app for a page carrying a personal access token. The
host's nginx asks auth's `/check`, auth answers 200 with `X-User-Id` and
`X-User-Email` (`S4-check.md`), nginx sets those two headers on the request it
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
  before serving it: a request that `/check` approves is forwarded to the app
  with `X-User-Id` and `X-User-Email` set from auth's answer and any
  client-supplied `X-User-*` removed. This routing is a property of the space,
  added by opsctl's nginx generation (a separate sub-project).
- The agent holds a valid token `ikp_<token>` (`S5-tokens.md`) whose owner
  signed in through Google within the last 30 days, so the token is honored.

Postconditions:

- The token's last-used time is updated by the `/check` subrequest
  (`S4-check.md`); nothing else has changed.

## A visitor asks a space for the check endpoint

`/check` is meant only for nginx's internal subrequest to auth's socket.
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
internal subrequest nginx makes to auth's socket. The body is not fixed.

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
  auth's socket. This is a property of the space's nginx (opsctl's
  generation, a separate sub-project), named here only by its observable effect;
  it is not something auth can do alone, because on auth's socket a
  public `/check` and an nginx subrequest `/check` are indistinguishable.

Postconditions:

- Nothing has changed.
