# Phase 55 — nginx fragment: capture the chain id on gated locations, strip it on the ungated one

*Realizes design Decision 42 (nginx correlation capture/strip).*

`prompts/etc/nginx.conf` adopts the suite's edge rule. Each of the four gated
locations — the bearer prefix `location /srv/prompts/`, the session root
`location = /srv/prompts/`, the browse-UI prefix `location /srv/prompts/ui/`,
and the assets prefix `location /srv/prompts/static/` — gains
`auth_request_set $prompts_corr $upstream_http_x_correlation_id;` and
`proxy_set_header X-Correlation-Id $prompts_corr;`, so the introspection-minted
id reaches the service and any client-supplied one is overwritten. The ungated
PRM bootstrap location gains `proxy_set_header X-Correlation-Id "";` so the
service mints and a public caller can never inject a chain. The `/feed` 404
guard proxies nothing and is untouched; no owner header moves. The assertions
live in `cmd/prompts/web_test.go` beside the existing fragment string
assertions (the fragment is config, not Go — it is shipped verbatim).

This phase is independent of phases 48–50 and of the chassis rebuild; it edits
config and its test only.

**Done when:** `go build ./...` and `go test ./...` from `prompts/` are green
(design *Conventions*), with these ids covered by clearly-named tests:

- R-HWX8-NVCX — each of the four gated blocks of the repo-real
  `etc/nginx.conf` contains **both** `auth_request_set $prompts_corr
  $upstream_http_x_correlation_id;` and `proxy_set_header X-Correlation-Id
  $prompts_corr;`; a block with only one of the pair fails.
- R-HY55-1N3M — the ungated PRM block contains `proxy_set_header
  X-Correlation-Id "";` and no `auth_request_set` for the correlation header.
