# Phase 15 — Forward all four owner headers through the nginx fragment

*Realizes design Decision 6 (the landing page and nginx fragment).*

The two identity-forwarding locations in `etc/nginx.conf` capture and
re-emit all four dashboard owner headers instead of only `X-Owner-Email`.
The bearer-gated mount prefix `location /srv/github/` gains
`auth_request_set` captures for `$github_owner_id`, `$github_owner_name`,
and `$github_owner_picture` (from `$upstream_http_x_owner_id`,
`$upstream_http_x_owner_name`, `$upstream_http_x_owner_picture`) alongside
its existing `$github_owner`/`$github_client`, and `proxy_set_header` lines
for `X-Owner-Id`, `X-Owner-Name`, and `X-Owner-Picture` beside the existing
`X-Owner-Email`/`X-Client-Id`. The session-gated bare mount root
`location = /srv/github/` gains the parallel `$github_session_owner_id`,
`$github_session_owner_name`, and `$github_session_owner_picture` captures
and their `proxy_set_header` lines beside the existing
`X-Owner-Email $github_session_owner`. The static, PRM, and `/pr`-denial
locations forward no identity and are untouched. Assertions extend
`internal/web/nginx_test.go` (string checks over the shipped
`etc/nginx.conf`), tagged with the new ids.

**Done when:** R-1GOK-GA2F (bearer prefix captures+forwards all four owner
headers plus `X-Client-Id` with the `$github_owner*` variables) and
R-1HWG-U1T4 (session bare-root captures+forwards all four owner headers with
the `$github_session_owner*` variables) are each covered by a clearly-named
test, and the suite is green per design Conventions (`GOWORK=off go build
./...` and `GOWORK=off go test ./...` clean with no SKIP, `gofmt -l .` empty,
`go vet ./...` clean, from `github/`).
