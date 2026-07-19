# Phase 25 — forward all four X-Owner-* identity headers through the nginx fragment

*Realizes design Decision 16 (bearer-tier identity plumbing) and 4 (session-tier
landing location).*

The two locations in `scripts/etc/nginx.conf` that forward identity today capture
and forward only `X-Owner-Email` (plus `X-Client-Id` on the bearer tier). The
dashboard's introspection hooks now emit four owner headers on every allow
(`X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`, `X-Owner-Picture`); the three new
ones die at nginx. This phase extends both forwarding locations to capture
(`auth_request_set`) and forward (`proxy_set_header`) **all four** owner headers,
each through its own service-prefixed variable so names stay unique across the
fragments included into the one apex server block:

- **Bearer prefix** `location /srv/scripts/` (`auth_request /_authn`): adds
  `$scripts_owner_id`/`$scripts_owner_name`/`$scripts_owner_picture` captures and
  their `proxy_set_header`s alongside the existing `$scripts_owner` (email) and
  `$scripts_client` (`X-Client-Id`, unchanged). The 429 re-emit machinery and the
  scripts-named `@scripts_authn_500` are untouched (D16).
- **Session exact-match** `location = /srv/scripts/` (`auth_request
  /_session-authn`): adds `$scripts_session_owner_id`/`_name`/`_picture` captures
  and their `proxy_set_header`s alongside the existing `$scripts_session_owner`
  (email). No `X-Client-Id` (bearer-only) (D4).

Locations that forward no identity today stay exactly as they are: the PRM
bootstrap well-known, the `= /srv/scripts/feed` 404 stub, the `/srv/scripts/static/`
asset tier (session-gated but forwards no owner headers), and the
`@scripts_authn_500` re-emit. Identity hygiene holds for the new headers: one
`proxy_set_header` per name replaces any client-smuggled inbound header.
Downstream consumption of `X-Owner-Id` by the scripts binary is out of scope.

The proof follows the fragment's existing idiom: string assertions over
`scripts/etc/nginx.conf` read from disk in `cmd/scripts/main_test.go` (nginx is
not run by the suite).

**Done when:**

- R-LXR5-KN5G — a genuinely-asserting test tagged `// R-LXR5-KN5G` proves the
  bearer prefix `location /srv/scripts/` captures all four owner headers into
  `$scripts_owner`/`$scripts_owner_id`/`$scripts_owner_name`/`$scripts_owner_picture`
  from `$upstream_http_x_owner_{email,id,name,picture}` and forwards each with its
  own `proxy_set_header` (`X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`,
  `X-Owner-Picture`), with `X-Client-Id $scripts_client` unchanged.
- R-LYZ1-YEW5 — a genuinely-asserting test tagged `// R-LYZ1-YEW5` proves the
  exact-match session location `= /srv/scripts/` captures all four owner headers
  into `$scripts_session_owner`/`_id`/`_name`/`_picture` from the matching
  `$upstream_http_x_owner_*` and forwards each with its own `proxy_set_header`, and
  forwards no `X-Client-Id`.
- The suite is green per design's Conventions: `cd scripts && go build ./...`,
  `cd scripts && go vet ./...`, `cd scripts && gofmt -l .` (no output), and
  `cd scripts && go test ./...` all succeed with zero failures.
