# Phase 27 — Correlation lines in the committed nginx fragment

*Realizes design Decision 31 (nginx fragment: capture on the gated locations,
strip on the ungated one). No dependency on any other pending phase.*

`scripts/etc/nginx.conf` gains the correlation-header plumbing D31 specifies,
and nothing else. The fragment is inert configuration, so this phase can be
built before the revised `eventplane`/`appkit` exist (its effect becomes
observable when they do). Its proof extends the existing per-location content
assertions in `cmd/scripts/main_test.go`, the way every other tier in this
fragment is already pinned.

End state of the fragment:

- The three **gated** locations each capture the chain id the dashboard minted,
  from the auth subresponse, into their own service-prefixed variable and
  forward it with a single `proxy_set_header`: the bearer prefix
  `location /srv/scripts/` → `$scripts_corr`, the session landing
  `location = /srv/scripts/` → `$scripts_session_corr`, and the session assets
  `location /srv/scripts/static/` → `$scripts_static_corr`.
- The one **ungated proxying** location — the PRM bootstrap
  `= /srv/scripts/.well-known/oauth-protected-resource` — carries
  `proxy_set_header X-Correlation-Id "";`, so no client value survives and the
  chassis mints.
- `= /srv/scripts/feed` (a bare `return 404`) and the `@scripts_authn_500`
  re-emit are untouched, as are every tier's gate, precedence, upstream, and
  owner-header plumbing.

**Done when:** both ids below are covered by clearly-named per-location
assertions in `cmd/scripts/main_test.go` (reading the repo-real
`etc/nginx.conf` from disk and asserting **inside each location block** through
the existing block-extraction helper — a whole-file substring search does not
satisfy them), and the suite is green per design's *Conventions*
(`go build ./...`, `go vet ./...`, `gofmt -l .` printing nothing, and
`go test ./...` clean, with every pre-existing fragment assertion still passing
— including that the string `prompts` does not appear in the file):

- **R-ENOZ-F427** — the ungated PRM block carries
  `proxy_set_header X-Correlation-Id "";` with no `auth_request` and no
  correlation `auth_request_set`, and no other block carries the strip.
- **R-EOWV-SVSW** — each of the three gated blocks (the bearer prefix, the
  session landing, the session assets) captures
  `$upstream_http_x_correlation_id` into its own variable (`$scripts_corr`,
  `$scripts_session_corr`, `$scripts_static_corr` — three distinct names) and
  forwards exactly that variable with one `proxy_set_header X-Correlation-Id` in
  the same block.

As a fast structural sanity check while building (not the acceptance bar):
`grep -c 'proxy_set_header X-Correlation-Id' etc/nginx.conf` should print `4`,
`grep -c 'proxy_set_header X-Correlation-Id "";'` should print `1`, and
`grep -c 'upstream_http_x_correlation_id'` should print `3`.
