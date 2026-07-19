# Phase 15 — forward all four `X-Owner-*` identity headers through the nginx fragment

*Realizes design Decision 4 (nginx fragment: four-header `X-Owner-*` identity forwarding).*

The committed `ledger/etc/nginx.conf` fragment captures and forwards **all four**
owner headers on **every location that forwards identity today**, so none of the
new headers die at nginx:

- the bearer prefix `location /srv/ledger/` (`auth_request /_authn`) captures
  `$ledger_owner`, `$ledger_owner_id`, `$ledger_owner_name`,
  `$ledger_owner_picture` from `$upstream_http_x_owner_email` /
  `_x_owner_id` / `_x_owner_name` / `_x_owner_picture` and forwards
  `X-Owner-Email`, `X-Owner-Id`, `X-Owner-Name`, `X-Owner-Picture` (plus the
  unchanged `X-Client-Id $ledger_client`);
- the session exact-match `location = /srv/ledger/` (`auth_request
  /_session-authn`) captures `$ledger_session_owner`, `$ledger_session_owner_id`,
  `$ledger_session_owner_name`, `$ledger_session_owner_picture` and forwards the
  same four `X-Owner-*` headers.

The service-prefixed variable names keep uniqueness within the one apex `server`
block, and the single-`proxy_set_header`-per-name identity hygiene extends to the
new headers (a client cannot smuggle any `X-Owner-*`). Locations that forward no
identity today (PRM bootstrap, the static session tier, the `@ledger_authn_500`
re-emit) are unchanged. Downstream consumption of `X-Owner-Id` by the binary is
out of scope. Tests are string assertions over `etc/nginx.conf` in
`cmd/ledger/main_test.go`, the fragment's existing idiom.

**Done when:** both ids are covered by genuine tests in `cmd/ledger/main_test.go`
(each tagged with its id verbatim) and the suite is green per design's Conventions
(`cd ledger && go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`
all pass):

- R-FLV3-9RX8 — the bearer prefix `location /srv/ledger/` captures all four owner
  headers with `$ledger_owner`/`$ledger_owner_id`/`$ledger_owner_name`/
  `$ledger_owner_picture` and forwards `X-Owner-Email`/`X-Owner-Id`/`X-Owner-Name`/
  `X-Owner-Picture` plus `X-Client-Id`; a fragment forwarding only `X-Owner-Email`
  fails.
- R-FN2Z-NJNX — the session exact-match `location = /srv/ledger/` captures all four
  owner headers with the `$ledger_session_owner*` variables and forwards all four
  `X-Owner-*` headers; a fragment forwarding only `X-Owner-Email` on the landing
  root fails.
