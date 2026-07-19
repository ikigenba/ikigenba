# Phase 16 — forward all four owner headers on both identity-forwarding nginx locations

*Realizes design Decision 4 (nginx fragment: owner-identity forwarding contract).*

The shipped nginx fragment `crm/etc/nginx.conf` forwards only `X-Owner-Email` on
the locations that carry identity today; the dashboard now emits four owner
headers on every allow, and the extra three (`X-Owner-Id`, `X-Owner-Name`,
`X-Owner-Picture`) die at nginx. This phase brings the two identity-forwarding
locations up to the full-owner-identity contract in D04.

Observable end state, in `crm/etc/nginx.conf`:

- The bearer-gated service prefix `location /srv/crm/ {` captures each of the four
  owner headers from the `/_authn` subrequest into crm-prefixed variables
  (`$crm_owner_id`, `$crm_owner` for email, `$crm_owner_name`,
  `$crm_owner_picture`) via `auth_request_set`, and forwards each to the upstream
  via `proxy_set_header` (`X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`,
  `X-Owner-Picture`). Its existing `X-Client-Id` (`$crm_client`) forwarding and the
  `@crm_authn_500` rate-limit machinery are retained unchanged.
- The session-gated landing `location = /srv/crm/ {` captures the same four owner
  headers from the `/_session-authn` subrequest into session-scoped crm-prefixed
  variables (`$crm_session_owner_id`, `$crm_session_owner` for email,
  `$crm_session_owner_name`, `$crm_session_owner_picture`) and forwards each. Its
  `auth_request /_session-authn`, `error_page 401 = @login_bounce`, and root
  `proxy_pass` are retained unchanged.
- The static-asset, PRM-bootstrap, and feed-denial locations are untouched — they
  forward no identity and stay that way.
- Identity hygiene holds: exactly one `proxy_set_header` per owner-header name,
  each sourced from its auth-subrequest variable, so a client cannot smuggle any
  owner header through.

The change is config-only over the fragment; crm's binary is not required to
consume the new headers in this phase. Tests are string assertions over
`crm/etc/nginx.conf` in `cmd/crm/main_test.go`, matching the existing nginx
content-assertion idiom (`nginxLocationBlock`).

**Done when:**

- R-FS4N-9HTJ — a genuine test asserts the bearer prefix block
  (`location /srv/crm/ {`) contains the three new `auth_request_set` captures
  (`$crm_owner_id`←`$upstream_http_x_owner_id`,
  `$crm_owner_name`←`$upstream_http_x_owner_name`,
  `$crm_owner_picture`←`$upstream_http_x_owner_picture`) alongside the existing
  `$crm_owner`/`$crm_client`, and a `proxy_set_header` for each of `X-Owner-Id`,
  `X-Owner-Name`, `X-Owner-Picture` (with `X-Owner-Email` and `X-Client-Id` still
  forwarded).
- R-FTCJ-N9K8 — a genuine test asserts the session landing block
  (`location = /srv/crm/ {`) contains the three new `auth_request_set` captures
  (`$crm_session_owner_id`←`$upstream_http_x_owner_id`,
  `$crm_session_owner_name`←`$upstream_http_x_owner_name`,
  `$crm_session_owner_picture`←`$upstream_http_x_owner_picture`) alongside the
  existing `$crm_session_owner`, and a `proxy_set_header` for each of `X-Owner-Id`,
  `X-Owner-Name`, `X-Owner-Picture` (with `X-Owner-Email` still forwarded).
- Both ids appear verbatim as tags in `cmd/crm/main_test.go`, and the suite is
  green per design's Conventions: `cd crm && go build ./...`, `cd crm && go vet
  ./...`, `cd crm && gofmt -l .` (no output), and `cd crm && go test ./...` all
  succeed with zero failures.
