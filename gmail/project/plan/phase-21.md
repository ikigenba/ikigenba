# Phase 21 — Forward all four owner identity headers through the gmail nginx fragment

*Realizes design Decision 4 (nginx fragment four-header identity forwarding
slice: R-MTLH-R7Z9, R-MUTE-4ZPY). The other D4 ids (R-NGNX-3B6C, R-NGNX-5D8E,
R-NGNX-7F1G, R-NGNX-9H3J) are already realized in the codebase.*

The dashboard's introspection hooks now emit four fail-closed owner headers on
every allow — `X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`, `X-Owner-Picture` —
but gmail's fragment (`gmail/etc/nginx.conf`) captures and forwards only
`X-Owner-Email`, so the other three die at nginx. This phase extends both of the
fragment's identity-forwarding locations to capture and forward all four, with
service-prefixed variables, entirely within `gmail/etc/nginx.conf` and its
content-assertion test `cmd/gmail/nginx_test.go`.

Observable end state:

- The **bearer-gated prefix** `location /srv/gmail/` (`auth_request /_authn`)
  captures the four owner headers into `$gmail_owner` (email, existing),
  `$gmail_owner_id`, `$gmail_owner_name`, `$gmail_owner_picture` and forwards each
  as its `X-Owner-*` header; its `X-Client-Id` forwarding and the
  `@gmail_authn_500` 429 re-emit machinery are unchanged.
- The **session-gated exact-match** `location = /srv/gmail/`
  (`auth_request /_session-authn`) captures the four owner headers into
  `$gmail_session_owner` (email, existing), `$gmail_session_owner_id`,
  `$gmail_session_owner_name`, `$gmail_session_owner_picture` and forwards each as
  its `X-Owner-*` header; its `error_page 401 = @login_bounce;` opt-in and
  `proxy_pass` to the loopback root are unchanged.
- Locations that forward no identity today (PRM well-known bootstrap, the
  `= /srv/gmail/feed` and `= /srv/gmail/attachment` 404 stubs, the
  `/srv/gmail/static/` asset tier, the `@gmail_authn_500` re-emit) are untouched.

**Done when:**

- `cd gmail && go build ./... && go vet ./... && gofmt -l . && go test ./...`
  all succeed with zero failures and no `gofmt` output (design Conventions:
  "the suite is green").
- The realized ids are each covered by a clearly-named test in
  `cmd/gmail/nginx_test.go` (string assertions over `gmail/etc/nginx.conf` read
  from disk), green:
  - R-MTLH-R7Z9 — the bearer-gated prefix `location /srv/gmail/` block contains
    `auth_request_set $gmail_owner $upstream_http_x_owner_email`,
    `auth_request_set $gmail_owner_id $upstream_http_x_owner_id`,
    `auth_request_set $gmail_owner_name $upstream_http_x_owner_name`,
    `auth_request_set $gmail_owner_picture $upstream_http_x_owner_picture`, and a
    matching `proxy_set_header` for each of `X-Owner-Email $gmail_owner`,
    `X-Owner-Id $gmail_owner_id`, `X-Owner-Name $gmail_owner_name`,
    `X-Owner-Picture $gmail_owner_picture`, plus the retained
    `proxy_set_header X-Client-Id $gmail_client`. A fragment forwarding only
    `X-Owner-Email` fails it.
  - R-MUTE-4ZPY — the session-gated exact-match `location = /srv/gmail/` block
    contains `auth_request_set $gmail_session_owner $upstream_http_x_owner_email`,
    `auth_request_set $gmail_session_owner_id $upstream_http_x_owner_id`,
    `auth_request_set $gmail_session_owner_name $upstream_http_x_owner_name`,
    `auth_request_set $gmail_session_owner_picture $upstream_http_x_owner_picture`,
    and a matching `proxy_set_header` for each of `X-Owner-Email
    $gmail_session_owner`, `X-Owner-Id $gmail_session_owner_id`, `X-Owner-Name
    $gmail_session_owner_name`, `X-Owner-Picture $gmail_session_owner_picture`. A
    fragment forwarding only `X-Owner-Email` fails it.
