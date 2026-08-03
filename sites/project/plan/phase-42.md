# Phase 42 — nginx fragment: capture the minted correlation id, strip it on the public tiers

*Realizes design Decision 29 (fragment correlation lines). No dependency on
another pending phase.*

`sites/etc/nginx.conf` is the only file this phase changes. Every **gated**
location gains an `auth_request_set` that captures `X-Correlation-Id` off the
auth subrequest's response plus a `proxy_set_header X-Correlation-Id` that
forwards it, overwriting whatever the client sent:

- `= /srv/sites/mcp` (bearer `/_authn`) uses `$sites_correlation`.
- `= /srv/sites/`, `/srv/sites/static/`, and `/srv/sites/private/` (session
  `/_session-authn`) use `$sites_session_correlation`.

Every **ungated** location that proxies — the PRM bootstrap
`= /srv/sites/.well-known/oauth-protected-resource` and the public site tier
`/srv/sites/public/` — instead gets `proxy_set_header X-Correlation-Id "";`, so
nginx passes no such field upstream and the chassis mints one. The named
`@sites_authn_500` location proxies nothing and is untouched.

Nothing else moves: the location set, each location's auth posture, the owner
header captures and forwards, the `@login_bounce` opt-ins, the
`@sites_authn_500` re-emit, and the literal port `3004` all stay exactly as they
are. No Go source changes in this phase.

The assertions extend the existing fragment test in `cmd/sites/main_test.go`,
which already reads `sites/etc/nginx.conf` from disk under D4's, D18's, D24's,
and D26's ids — same file, same style, two new id-tagged cases.

**Done when** (all commands from `sites/`):

- `R-BN65-STRE` is covered by a test asserting that **each** of the four gated
  locations (`= /srv/sites/mcp`, `= /srv/sites/`, `/srv/sites/static/`,
  `/srv/sites/private/`) contains both an `auth_request_set` binding its
  `$sites_correlation` / `$sites_session_correlation` variable to
  `$upstream_http_x_correlation_id` **and** a `proxy_set_header X-Correlation-Id`
  whose value is that same variable — per location, not merely somewhere in the
  file, and never `$http_x_correlation_id`.
- `R-9CMM-G2ZU` is covered by a test asserting that both ungated proxying
  locations (the PRM `.well-known` exact-match block and `/srv/sites/public/`)
  contain `proxy_set_header X-Correlation-Id "";` and no `auth_request`, and that
  no gated location carries the empty-value form.
- `grep -c 'auth_request' etc/nginx.conf` is unchanged from its pre-phase value (no location gains or loses a gate).
- The suite is green as design's *Conventions* define it: `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all clean — including the pre-existing fragment assertions for D4/D18/D24/D26, which must still pass unmodified.
