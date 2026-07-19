# Phase 43 — Forward all four owner identity headers through the nginx fragment

*Realizes design Decision 10 (landing page / nginx fragment identity forwarding).*

`etc/nginx.conf` forwards **all four** dashboard owner headers (`X-Owner-Id`,
`X-Owner-Email`, `X-Owner-Name`, `X-Owner-Picture`) on **every** location that
forwards identity today, replacing the old email-only forwarding:

- The bearer prefix `location /srv/prompts/` (`auth_request /_authn`) captures
  each of the four owner headers with `auth_request_set` into `$prompts_owner`,
  `$prompts_owner_id`, `$prompts_owner_name`, `$prompts_owner_picture` and
  forwards each with a matching `proxy_set_header` — while retaining
  `proxy_set_header X-Client-Id $prompts_client;` and the existing 429 re-emit
  wiring.
- The session landing exact `location = /srv/prompts/` (`auth_request
  /_session-authn`) captures and forwards the same four owner headers into the
  same `$prompts_owner*` variables (no `X-Client-Id`; the `@login_bounce` opt-in
  is untouched).

The three new `$prompts_owner*` variables are `prompts`-prefixed and reused
across both locations (matching today's `$prompts_owner`). The PRM bootstrap,
`= /srv/prompts/feed` guard, and the `/srv/prompts/static/` location (which
forwards no identity) are unchanged. Downstream consumption of `X-Owner-Id` by
the prompts binary is out of scope. Tests are string assertions over the
committed `etc/nginx.conf` location blocks in `cmd/prompts/web_test.go`, in the
existing `nginxLocationBlock` idiom.

**Done when:**

- R-7NY0-UIO6 — a genuine test asserts the `location /srv/prompts/` block
  captures all four owner headers (`auth_request_set $prompts_owner
  $upstream_http_x_owner_email;`, `$prompts_owner_id`, `$prompts_owner_name`,
  `$prompts_owner_picture`) and forwards each (`proxy_set_header X-Owner-Email
  $prompts_owner;`, `X-Owner-Id $prompts_owner_id;`, `X-Owner-Name
  $prompts_owner_name;`, `X-Owner-Picture $prompts_owner_picture;`) plus retains
  `proxy_set_header X-Client-Id $prompts_client;`.
- R-7P5X-8AEV — a genuine test asserts the `location = /srv/prompts/` block
  captures and forwards the same four owner headers into `$prompts_owner`,
  `$prompts_owner_id`, `$prompts_owner_name`, `$prompts_owner_picture` (not
  email-only).
- Both ids appear verbatim as tags in `cmd/prompts/web_test.go`, and the suite is
  green per the design's *Conventions* (`go test ./...` from `prompts/`, no race
  violations).
