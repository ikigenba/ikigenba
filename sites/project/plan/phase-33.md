# Phase 33 — nginx fragment forwards all four X-Owner-* identity headers on every identity-forwarding location

*Realizes design Decision 4 (landing root), 18 (private tier), and 26 (bearer MCP
endpoint).*

The dashboard's introspection hooks now emit four owner headers on every allow,
fail-closed — `X-Owner-Id` and `X-Owner-Email` (never empty) and the
percent-encoded `X-Owner-Name` / `X-Owner-Picture` (either may be empty). Today
`sites/etc/nginx.conf` captures and forwards only `X-Owner-Email` (plus
`X-Client-Id` on the bearer tier), so the three new owner headers die at nginx.
This phase updates the fragment so **every location that forwards identity today**
captures all four owner headers with `auth_request_set` and re-emits each with its
own `proxy_set_header`, using the service-prefixed variable convention. The three
identity-forwarding locations:

- **Bearer MCP endpoint** `location = /srv/sites/mcp` (gate `/_authn`) — captures
  into `$sites_owner`, `$sites_owner_id`, `$sites_owner_name`,
  `$sites_owner_picture` (and the unchanged `$sites_client`) and forwards
  `X-Owner-Email`/`X-Owner-Id`/`X-Owner-Name`/`X-Owner-Picture` + `X-Client-Id`.
  Its `$authn_status` rate-limit re-emit machinery is untouched (D26).
- **Session landing root** `location = /srv/sites/` (gate `/_session-authn`) —
  captures into `$sites_session_owner`, `$sites_session_owner_id`,
  `$sites_session_owner_name`, `$sites_session_owner_picture` and forwards all four
  `X-Owner-*` headers (D4).
- **Private tier** `location /srv/sites/private/` (gate `/_session-authn`) — same
  four-header capture/forward with the `$sites_session_owner*` variables (D18).

The locations that forward **no** identity today stay exactly as they are: the PRM
bootstrap, the bearer 500 re-emit `@sites_authn_500`, the public tier
`location /srv/sites/public/`, and the session-gated `location /srv/sites/static/`
landing-asset tier (it gates but forwards no owner header). The per-header identity
hygiene (a single `proxy_set_header` per name replaces any client-smuggled inbound
value) extends to the three new headers. This is a fragment-only change; downstream
consumption of `X-Owner-Id` by the sites binary is out of scope.

The proof substrate is the existing content-assertion idiom: a Go test reads
`sites/etc/nginx.conf` from disk and asserts over each location block (nginx is not
run by the suite), tagged with the requirement id, in `cmd/sites/main_test.go`.

**Done when:**
- R-7MHG-PUOP — a test asserts the `location = /srv/sites/` block captures all four
  owner headers (`$sites_session_owner` ← `$upstream_http_x_owner_email`,
  `$sites_session_owner_id` ← `$upstream_http_x_owner_id`,
  `$sites_session_owner_name` ← `$upstream_http_x_owner_name`,
  `$sites_session_owner_picture` ← `$upstream_http_x_owner_picture`) and forwards
  each with its own `proxy_set_header` (`X-Owner-Email`/`X-Owner-Id`/`X-Owner-Name`/
  `X-Owner-Picture`); dropping any of the three new headers or omitting any capture
  fails it.
- R-7NPD-3MFE — a test asserts the `location /srv/sites/private/` block captures
  and forwards the same four owner headers with the `$sites_session_owner*`
  variables; forwarding only `X-Owner-Email` or omitting any capture fails it.
- R-7L9K-C2Y0 — a test asserts the `location = /srv/sites/mcp` block captures all
  four owner headers into `$sites_owner`/`$sites_owner_id`/`$sites_owner_name`/
  `$sites_owner_picture`, forwards each `X-Owner-*` header plus `X-Client-Id
  $sites_client`, and keeps `auth_request /_authn`; forwarding only
  `X-Owner-Email`/`X-Client-Id` or omitting any of the four owner captures fails it.
- Each id above appears verbatim as a tag in a genuinely-asserting test in
  `cmd/sites/main_test.go`, and the suite is green per design's Conventions
  (`cd sites && go build ./...`, `go vet ./...`, `gofmt -l .` with no output, and
  `go test ./...` all succeed with zero failures, including the Chrome-hard-required
  browser wiring test).
