# Phase 16 — Forward the full owner header set on both identity-forwarding nginx locations

*Realizes design Decision 4 (nginx fragment: session-gated `= /srv/notify/`
location and the owner identity headers every gated location forwards).*

The dashboard introspection hooks now emit four owner headers on every allow
(`X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`, `X-Owner-Picture`), but notify's
`etc/nginx.conf` fragment captures and forwards only `X-Owner-Email`, so the
three new headers die at nginx. This phase extends the fragment's two
identity-forwarding locations to capture (`auth_request_set`) and forward
(`proxy_set_header`) all four owner headers, each sourced once from its own
auth-subrequest variable so a client cannot smuggle identity:

- The **bearer-gated** `location /srv/notify/` prefix gains
  `$notify_owner_id`/`$notify_owner_name`/`$notify_owner_picture` captured from
  `$upstream_http_x_owner_id`/`_name`/`_picture`, forwarded as
  `X-Owner-Id`/`X-Owner-Name`/`X-Owner-Picture` beside the existing
  `X-Owner-Email $notify_owner`; `X-Client-Id $notify_client` and the
  `@notify_authn_500` rate-limit machinery are unchanged.
- The **session-gated** exact-match `location = /srv/notify/` gains
  `$notify_session_owner_id`/`_name`/`_picture` captured from the same upstream
  headers and forwarded alongside the existing `X-Owner-Email
  $notify_session_owner`.

The unauthenticated PRM bootstrap, the session-gated `/srv/notify/static/` path
(forwards no identity), and `@notify_authn_500` are left exactly as they are.
Downstream consumption of `X-Owner-Id` by the notify Go binary is **not** in this
phase — only the fragment forwarding and its content-assertion tests. The only
files touched are `etc/nginx.conf` and its tests in `cmd/notify/main_test.go`
(the existing R-NGNX content-assertion idiom that reads the fragment from disk);
no product change, no Go behavior change, no migration.

**Done when:**

- R-M9EO-BQGX — a named test reads `etc/nginx.conf` and asserts the bearer-gated
  `location /srv/notify/` prefix contains `auth_request_set $notify_owner_id
  $upstream_http_x_owner_id;`, `auth_request_set $notify_owner_name
  $upstream_http_x_owner_name;`, and `auth_request_set $notify_owner_picture
  $upstream_http_x_owner_picture;`, and a `proxy_set_header` forwarding each of
  `X-Owner-Id $notify_owner_id`, `X-Owner-Email $notify_owner`, `X-Owner-Name
  $notify_owner_name`, and `X-Owner-Picture $notify_owner_picture`, while still
  forwarding `X-Client-Id $notify_client` (dropping any of the three new owner
  headers, or sourcing one from other than its `$notify_owner_*` variable, fails
  the test).
- R-MAMK-PI7M — a named test reads `etc/nginx.conf` and asserts the
  session-gated exact-match `location = /srv/notify/` contains `auth_request_set
  $notify_session_owner_id $upstream_http_x_owner_id;`, `auth_request_set
  $notify_session_owner_name $upstream_http_x_owner_name;`, and
  `auth_request_set $notify_session_owner_picture $upstream_http_x_owner_picture;`,
  and a `proxy_set_header` forwarding each of `X-Owner-Id $notify_session_owner_id`,
  `X-Owner-Email $notify_session_owner`, `X-Owner-Name $notify_session_owner_name`,
  and `X-Owner-Picture $notify_session_owner_picture` (dropping any of the three
  new owner headers, or sourcing one from other than its `$notify_session_owner_*`
  variable, fails the test).
- The notify suite is green per design Conventions:
  `cd notify && go build ./...`, `go vet ./...`, `gofmt -l .` (empty), and
  `go test ./...` all succeed with zero failures.
