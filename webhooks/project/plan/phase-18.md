# Phase 18 — forward all four X-Owner-* identity headers on the identity tiers

*Realizes design Decision 7 (nginx location fragment (tiers)).*

The committed nginx fragment (`etc/nginx.conf`) forwards the full owner identity
on every tier that already forwards identity today. Two tiers change; every other
tier (PRM bootstrap, `/feed` shield, static assets, public `/in/` ingress,
catch-all) is left byte-for-byte as-is.

Observable end state:

- The bearer-gated `location = /srv/webhooks/mcp` captures all four owner headers
  from the `/_authn` subresponse — a per-header `auth_request_set` from
  `$upstream_http_x_owner_id`, `$upstream_http_x_owner_email`,
  `$upstream_http_x_owner_name`, `$upstream_http_x_owner_picture` into
  service-prefixed `$wh_owner*` variables — and forwards each with one
  `proxy_set_header` per name (`X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`,
  `X-Owner-Picture`), alongside the unchanged `X-Client-Id`.
- The session-gated `location = /srv/webhooks/` captures the same four owner
  headers from the `/_session-authn` subresponse into service-prefixed
  `$wh_session_owner*` variables (distinct from the bearer tier's names) and
  forwards each with one `proxy_set_header` per name.
- The public `/in/` ingress and the static-assets tier still forward no owner
  header; the PRM, `/feed`, and catch-all tiers are unchanged.

The proof substrate is a content assertion over the committed fragment in
`cmd/webhooks/nginx_test.go` (the D7 idiom): each new id extracts the location
block and asserts the four `auth_request_set` captures and the four
`proxy_set_header` forwards by name. Downstream consumption of `X-Owner-Id` by the
service binary is out of scope for this phase.

**Done when:**

- R-XK5N-0I1E — a genuine test asserts the bearer `location = /srv/webhooks/mcp`
  captures all four `$upstream_http_x_owner_*` headers and forwards
  `X-Owner-Id`/`X-Owner-Email`/`X-Owner-Name`/`X-Owner-Picture` (plus the unchanged
  `X-Client-Id`), one `proxy_set_header` per name.
- R-XLDJ-E9S3 — a genuine test asserts the session `location = /srv/webhooks/`
  captures all four `$upstream_http_x_owner_*` headers into `$wh_session_owner*`
  variables distinct from the bearer tier's and forwards all four `X-Owner-*`
  headers, one `proxy_set_header` per name.
- The suite is green per design Conventions: from `webhooks/`, `go build ./...`,
  `go vet ./...`, and `go test ./...` all exit 0.
