# Phase 72 — nginx: session-gate `/srv/wiki/subject/` and `/srv/wiki/static/`

*Realizes design Decision 47 (extend the session gate over the whole human
surface). **Structural / config phase — no R-ids.** Edits only
`wiki/etc/nginx.conf`; touches no Go code, adds no migration. Depends on Phase 71
(the `/subject/…` upstream route) and Phase 69 (the `/static/…` route) the new
locations proxy to.*

D39 (Phase 64) session-gated only the exact root `= /srv/wiki/`; everything else
under the mount fell to the **bearer** prefix, which a browser cannot satisfy. The
subject pages (`/srv/wiki/subject/…`) and embedded assets (`/srv/wiki/static/…`)
must be **cookie**-gated, not bearer-gated. This phase adds the two missing
session-gated **prefix** locations beside the unchanged bearer prefix; the agent
paths (`/mcp`, `/health`, `/feed`) and the PRM bootstrap are untouched.

In **`wiki/etc/nginx.conf`**, add (mirroring the existing `= /srv/wiki/` session
block and `sites/etc/nginx.conf`'s private-tier pattern):

```
location /srv/wiki/subject/ {
    auth_request /_session-authn;
    auth_request_set $wiki_owner $upstream_http_x_owner_email;
    proxy_set_header X-Owner-Email $wiki_owner;
    proxy_pass http://127.0.0.1:__PORT__/subject/;   # preserve the /subject/ sub-path
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}

location /srv/wiki/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;    # preserve the /static/ sub-path
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```

`__PORT__` stays the templated placeholder — do **not** hard-code the port. The
gate is `/_session-authn` (cookie audience), **not** `/_authn`. Each `proxy_pass`
URI **preserves** its sub-path (`…/subject/`, `…/static/`) so the Go mux sees
`/subject/…` and `/static/…` (Phase 69/71 routes).

**Done when:** the Go suite stays green (this phase changes no Go — `cd wiki &&
go build ./... && go vet ./... && gofmt -l . && go test ./...` and
`bin/check-migrations wiki` all unaffected) **and** the named structural check
passes:

- `wiki/etc/nginx.conf` contains a prefix `location /srv/wiki/subject/` whose body
  uses `auth_request /_session-authn`, `proxy_pass
  http://127.0.0.1:__PORT__/subject/` (templated port, sub-path preserved), and
  sets `X-Owner-Email` from the session subrequest.
- `wiki/etc/nginx.conf` contains a prefix `location /srv/wiki/static/` whose body
  uses `auth_request /_session-authn` and `proxy_pass
  http://127.0.0.1:__PORT__/static/`.
- The pre-existing bearer prefix `location /srv/wiki/` (`auth_request /_authn`),
  the exact-match session root `= /srv/wiki/`, and the PRM exact-match
  `= /srv/wiki/.well-known/oauth-protected-resource` are **still present and
  unchanged** — the agent surface and PRM bootstrap are not disturbed.

(Structural phase: "Ids to cover" is **(none — structural phase)**; the build
loop verifies the green suite plus the fragment check above, not an `R-id` test.)
