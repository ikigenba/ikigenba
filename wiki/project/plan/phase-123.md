# Phase 123 — nginx fragment: forward the correlation id on gated locations, strip it on the ungated one

*Realizes design Decision 66 (nginx correlation lines). Structural — no Verification ids.*

**Cross-workspace dependency.** The value being forwarded is minted by the
dashboard's introspection endpoint, so the header only carries real data once
the dashboard change ships. The fragment change is independent of that and can
land any time after the suite's shared modules; the deterministic checks below
are over the shipped artifact and do not need a running suite.

**What gets built.** `wiki/etc/nginx.conf` only — no Go changes.

- The four gated proxying locations — `= /srv/wiki/`, `/srv/wiki/subject/`,
  `/srv/wiki/static/`, and the bearer-gated `/srv/wiki/` prefix — each gain
  `auth_request_set $wiki_correlation $upstream_http_x_correlation_id;` and
  `proxy_set_header X-Correlation-Id $wiki_correlation;`.
- The ungated PRM bootstrap
  `= /srv/wiki/.well-known/oauth-protected-resource` gains
  `proxy_set_header X-Correlation-Id "";`.
- `@wiki_authn_500` proxies nothing and is untouched.

**Done when** (all run from `wiki/`, over the shipped fragment):

- `grep -c 'proxy_pass http://127.0.0.1' etc/nginx.conf` prints `5` and
  `grep -c 'proxy_set_header X-Correlation-Id' etc/nginx.conf` prints `5` — every
  location that proxies to the loopback upstream sets the header.
- `grep -c 'auth_request_set \$wiki_correlation \$upstream_http_x_correlation_id;' etc/nginx.conf`
  prints `4` and
  `grep -c 'proxy_set_header X-Correlation-Id \$wiki_correlation;' etc/nginx.conf`
  prints `4` — the capture and the client-overwrite are paired on all four gated
  locations.
- `grep -c 'proxy_set_header X-Correlation-Id "";' etc/nginx.conf` prints `1` —
  the ungated PRM location strips to the empty literal.
- `nginx -t` is not runnable standalone against a fragment (it is not a vhost),
  so the loadability bar is the existing one: the fragment stays literal, with
  no templating and no `server{}`/`listen`/cert directive —
  `grep -cE '^\s*(server|listen|ssl_certificate)' etc/nginx.conf` prints `0`.
- The suite stays green per design's *Conventions*: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures.
