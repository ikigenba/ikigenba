# Phase 17 — Forward all four owner identity headers through the nginx fragment

*Realizes design Decision 10 (nginx fragment & the canonical landing page).*

Bring the shipped `etc/nginx.conf` fragment up to the owner-identity header
contract in D10. Today the two identity-forwarding locations forward only
`X-Owner-Email` (the session landing `location = /srv/repos/`) and
`X-Owner-Email` + `X-Client-Id` (the bearer prefix `location /srv/repos/`). The
introspection hooks now emit four owner headers on every allow, so both
locations must capture (`auth_request_set`) and forward (`proxy_set_header`) all
four — `X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`, `X-Owner-Picture` — each
via its own service-prefixed variable, preserving identity hygiene (one
`proxy_set_header` per name overrides any client-smuggled inbound header). The
bearer tier keeps forwarding `X-Client-Id` from `$repos_client` unchanged. The
static-assets location (`location /srv/repos/static/`), the PRM bootstrap, the
`/feed` denial, and the 429-recovery block forward no identity and stay exactly
as they are.

Observable end state: the session landing block sets `$repos_session_owner`,
`$repos_session_owner_id`, `$repos_session_owner_name`,
`$repos_session_owner_picture` from the matching `$upstream_http_x_owner_*`
subrequest variables and forwards them as the four `X-Owner-*` headers; the
bearer prefix block sets `$repos_owner`, `$repos_owner_id`, `$repos_owner_name`,
`$repos_owner_picture` likewise. The content assertions live in
`cmd/repos/nginx_test.go` (the existing D10 substrate).

**Done when:**
- R-UZVS-S08C — `cmd/repos/nginx_test.go` asserts the session landing block
  (`location = /srv/repos/`) captures and forwards all four owner headers via
  `$repos_session_owner` → `X-Owner-Email`, `$repos_session_owner_id` →
  `X-Owner-Id`, `$repos_session_owner_name` → `X-Owner-Name`, and
  `$repos_session_owner_picture` → `X-Owner-Picture`.
- R-V13P-5RZ1 — `cmd/repos/nginx_test.go` asserts the bearer prefix block
  (`location /srv/repos/`) captures and forwards all four owner headers via
  `$repos_owner` → `X-Owner-Email`, `$repos_owner_id` → `X-Owner-Id`,
  `$repos_owner_name` → `X-Owner-Name`, `$repos_owner_picture` →
  `X-Owner-Picture`, with `X-Client-Id` from `$repos_client` still forwarded.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  and `go test ./...` all exit 0 from `repos/`, and `gofmt -l .` prints nothing.
