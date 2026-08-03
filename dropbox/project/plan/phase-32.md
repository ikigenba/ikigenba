# Phase 32 — nginx fragment: forward the edge-minted correlation id on gated locations, clear it on the public one

*Realizes design Decision 27 (fragment correlation-id contract).*

**What gets built.** `dropbox/etc/nginx.conf` gains the suite-wide
`X-Correlation-Id` lines, and the existing fragment-content test in
`cmd/dropbox` grows the two assertions that pin them. Purely additive — no
existing location, variable, `auth_request`, or `proxy_pass` is rewritten.

- `location = /srv/dropbox/` (session tier) captures
  `$dropbox_session_correlation_id` from `$upstream_http_x_correlation_id` and
  forwards it as `X-Correlation-Id`.
- `location /srv/dropbox/static/` (session tier) does the same with
  `$dropbox_static_correlation_id`.
- `location /srv/dropbox/` (bearer tier) does the same with
  `$dropbox_correlation_id`, beside its existing owner/client forwards.
- `location = /srv/dropbox/.well-known/oauth-protected-resource` (ungated,
  public) sets `proxy_set_header X-Correlation-Id "";` so the chassis mints.
- `location = /srv/dropbox/content` and `location @dropbox_authn_500` proxy to
  no upstream and are left untouched.

Observable end state: a gated request arrives at dropbox carrying the id the
dashboard's introspection minted for that chain, whatever the client sent; a
public PRM fetch arrives with the header empty.

**Done when:** the suite is green — `cd dropbox && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), `go test ./...` all succeed with zero
failures — and both ids below are covered by genuinely-asserting additions to
the existing `cmd/dropbox` fragment test (which reads `etc/nginx.conf` from
disk):

- **R-VGTA-PXIA** — all three gated locations carry both halves of the
  capture/forward pair with their own service-prefixed variables
  (`$dropbox_session_correlation_id`, `$dropbox_static_correlation_id`,
  `$dropbox_correlation_id`), and the bearer prefix's existing `X-Owner-*` /
  `X-Client-Id` forwards are retained unchanged.
- **R-VI17-3P8Z** — the public PRM location carries `proxy_set_header
  X-Correlation-Id "";` with the empty value, and the file holds exactly four
  `proxy_set_header X-Correlation-Id` directives in total.
