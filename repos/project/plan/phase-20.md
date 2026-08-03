# Phase 20 — nginx fragment: capture the minted correlation id, strip it on the PRM bootstrap

*Realizes design Decision 13 (fragment correlation lines). No dependency on
another pending phase.*

`repos/etc/nginx.conf` is the only file this phase changes. Each **gated**
location gains an `auth_request_set` capturing `X-Correlation-Id` off the auth
subrequest's response plus a `proxy_set_header X-Correlation-Id` forwarding it,
overwriting whatever the client sent:

- the bearer-gated catch-all `location /srv/repos/` (via `/_authn`) uses
  `$repos_correlation`;
- `= /srv/repos/` and `/srv/repos/static/` (via `/_session-authn`) use
  `$repos_session_correlation`.

The ungated PRM bootstrap
`= /srv/repos/.well-known/oauth-protected-resource` instead gets
`proxy_set_header X-Correlation-Id "";`, so nginx passes no such field upstream
and the chassis mints one. `= /srv/repos/feed` (a bare `return 404;`) and the
named `@repos_authn_500` location proxy nothing and are untouched.

Nothing else moves: the location set, each location's auth posture, the owner
header captures and forwards, the `@login_bounce` opt-ins, the
`@repos_authn_500` re-emit, and the literal port `3007` all stay as they are.
No Go source changes in this phase.

The assertions extend `cmd/repos/nginx_test.go`, which already reads
`repos/etc/nginx.conf` from disk under D10's ids — same file, same style, two new
id-tagged cases.

**Done when** (all commands from `repos/`):

- `R-9DUI-TUQJ` is covered by a test asserting that **each** of the three gated
  locations (the bearer-gated `location /srv/repos/`, `= /srv/repos/`,
  `/srv/repos/static/`) contains both an `auth_request_set` binding its
  `$repos_correlation` / `$repos_session_correlation` variable to
  `$upstream_http_x_correlation_id` **and** a `proxy_set_header X-Correlation-Id`
  whose value is that same variable — per location, not merely somewhere in the
  file, and never `$http_x_correlation_id`.
- `R-9F2F-7MH8` is covered by a test asserting the
  `.well-known/oauth-protected-resource` exact-match block contains
  `proxy_set_header X-Correlation-Id "";` and no `auth_request`, and that no
  gated location carries the empty-value form.
- `grep -c 'auth_request' etc/nginx.conf` is unchanged from its pre-phase value (no location gains or loses a gate).
- The suite is green as design's *Conventions* define it: `go build ./...`, `go vet ./...`, `go test ./...` all clean and `gofmt -l .` empty — including D10's pre-existing fragment assertions, which must still pass unmodified.
