# Phase 29 — forward all four owner identity headers through the nginx fragment

*Realizes design Decision 4 (nginx fragment identity-header forwarding). No
dependency on any pending phase — it extends the shipped
`dropbox/etc/nginx.conf` fragment and its content-assertion test in
`cmd/dropbox/main_test.go`.*

Observable end state:

- The exact-match session-gated landing location `location = /srv/dropbox/`
  captures and forwards **all four** owner headers the dashboard introspection
  hooks emit, not just `X-Owner-Email`: it carries `auth_request_set` for
  `$dropbox_session_owner` (`X-Owner-Email`), `$dropbox_session_owner_id`
  (`X-Owner-Id`), `$dropbox_session_owner_name` (`X-Owner-Name`), and
  `$dropbox_session_owner_picture` (`X-Owner-Picture`), and a matching
  `proxy_set_header` for each of the four owner headers.
- The bearer-gated prefix location `location /srv/dropbox/` likewise captures
  and forwards all four owner headers with the bearer-tier variables
  (`$dropbox_owner`, `$dropbox_owner_id`, `$dropbox_owner_name`,
  `$dropbox_owner_picture`), and retains its existing `X-Client-Id`
  (`$dropbox_client`) forward unchanged.
- The variable names stay service-prefixed and unique; the single
  `proxy_set_header` per name preserves the identity-hygiene property (a
  client-smuggled inbound owner header is replaced by the auth-subrequest value).
- The locations that forward no owner identity today — the
  `= /srv/dropbox/.well-known/oauth-protected-resource` PRM bootstrap, the
  `= /srv/dropbox/content` 404 stub, the `/srv/dropbox/static/` asset tier, and
  the `@dropbox_authn_500` re-emit — are unchanged.

**Done when:** the suite is green (design Conventions commands, from `dropbox/`:
`go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`) and:

- R-KJGZ-FKVP is covered by a test reading `dropbox/etc/nginx.conf` from disk
  and asserting the `location = /srv/dropbox/ {` block contains the four
  `auth_request_set` captures (`$dropbox_session_owner` ←
  `$upstream_http_x_owner_email`, `$dropbox_session_owner_id` ←
  `$upstream_http_x_owner_id`, `$dropbox_session_owner_name` ←
  `$upstream_http_x_owner_name`, `$dropbox_session_owner_picture` ←
  `$upstream_http_x_owner_picture`) and the four matching `proxy_set_header`
  lines (`X-Owner-Email`, `X-Owner-Id`, `X-Owner-Name`, `X-Owner-Picture`).
- R-KKOV-TCME is covered by a test reading the same file and asserting the
  bearer prefix block `location /srv/dropbox/ {` contains the four owner
  `auth_request_set` captures with the bearer-tier variables (`$dropbox_owner`,
  `$dropbox_owner_id`, `$dropbox_owner_name`, `$dropbox_owner_picture`) and the
  four matching `proxy_set_header` owner-header lines, while its
  `proxy_set_header X-Client-Id $dropbox_client;` forward is still present.
