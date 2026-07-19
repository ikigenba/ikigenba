# Phase 91 — nginx fragment: forward all four `X-Owner-*` identity headers on every identity-forwarding location

*Realizes design Decision 39 (exact-root block, four-header forwarding) and 47
(fragment-wide owner-header forwarding contract). **Structural / config phase —
no R-ids** (wiki's settled convention: the nginx fragment is config proven by a
named fragment check, not a Go `R-id` test; D39/D47/D60 precedent). Edits only
`wiki/etc/nginx.conf`; touches no Go code, adds no migration. Purely additive —
depends on no earlier phase.*

The dashboard's introspection hooks now emit **four** owner headers on every
allow: `X-Owner-Id` and `X-Owner-Email` (never empty) plus percent-encoded
`X-Owner-Name` and `X-Owner-Picture` (either may be empty). Today wiki's fragment
captures and forwards only `X-Owner-Email` (and `X-Client-Id` on the bearer
tier), so the three new headers die at nginx. This phase extends each of the
**three identity-forwarding locations** in `wiki/etc/nginx.conf` to capture and
forward all four owner headers, using wiki's existing single `$wiki_owner*`
variable family:

- the exact-root session location `location = /srv/wiki/`,
- the session prefix `location /srv/wiki/subject/`,
- the bearer prefix `location /srv/wiki/` (which also keeps `X-Client-Id`).

Each gains three `auth_request_set` captures (`$wiki_owner_id`,
`$wiki_owner_name`, `$wiki_owner_picture` from the matching
`$upstream_http_x_owner_id` / `_name` / `_picture` subrequest headers) and three
`proxy_set_header` forwards (`X-Owner-Id`, `X-Owner-Name`, `X-Owner-Picture` from
those variables), beside the pre-existing `X-Owner-Email`. The session-gated
`location /srv/wiki/static/` forwards **no** identity and is left exactly as it
is; the PRM bootstrap, `@wiki_authn_500`, and the D60 `error_page 401 =
@login_bounce;` lines are all untouched. The single `proxy_set_header` per header
name preserves the identity-hygiene property (a client-smuggled inbound header of
that name is replaced) for the three new headers too.

**Done when:** the Go suite stays green (this phase changes no Go — `cd wiki &&
go build ./... && go vet ./... && gofmt -l . && go test ./...` and
`bin/check-migrations wiki` all unaffected) **and** the named structural check
passes:

- Each of the three identity-forwarding locations — `location = /srv/wiki/ {`,
  `location /srv/wiki/subject/ {`, and the bearer prefix `location /srv/wiki/ {`
  (with `auth_request /_authn`) — contains all three new captures
  `auth_request_set $wiki_owner_id $upstream_http_x_owner_id;`,
  `auth_request_set $wiki_owner_name $upstream_http_x_owner_name;`, and
  `auth_request_set $wiki_owner_picture $upstream_http_x_owner_picture;`, and all
  three new forwards `proxy_set_header X-Owner-Id $wiki_owner_id;`,
  `proxy_set_header X-Owner-Name $wiki_owner_name;`, and
  `proxy_set_header X-Owner-Picture $wiki_owner_picture;`, beside the pre-existing
  `proxy_set_header X-Owner-Email $wiki_owner;`.
- The bearer prefix additionally still carries `proxy_set_header X-Client-Id
  $wiki_client;` (unchanged).
- `location /srv/wiki/static/` contains **none** of `X-Owner-Id`, `X-Owner-Name`,
  `X-Owner-Picture`, or `X-Owner-Email` — it still forwards no identity.
- The change is additive: every pre-existing location, `auth_request` gate,
  `proxy_pass`, `error_page 401 = @login_bounce;`, and the `@wiki_authn_500`
  re-emit block still appears and is unchanged — nothing removed or rewritten.

(Structural phase: "Ids to cover" is **(none — structural phase)**; the build
loop verifies the green suite plus the named fragment check above, not an `R-id`
test.)
