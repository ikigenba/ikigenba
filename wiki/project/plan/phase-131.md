# Phase 131 — nginx: the two scope-tier locations

*Realizes design Decision 77 (structural). Depends on Phase 130.*

Rewrites `wiki/etc/nginx.conf` to the D77 fragment: open `/srv/wiki/public/` (identity headers blanked, correlation stripped), session-gated `/srv/wiki/private/` with the `@login_bounce` line, ungated `/srv/wiki/static/`, the `/srv/wiki/subject/` location removed, root and bearer locations unchanged.

**Done when (deterministic, no R-ids — structural):**
- `grep -c 'auth_request' wiki/etc/nginx.conf` matches only the root, `/srv/wiki/private/`, and the bearer/MCP locations — a grep for `location /srv/wiki/public/` finds a block containing **no** `auth_request`, a grep for `location /srv/wiki/private/` finds `auth_request /_session-authn` and `error_page 401 = @login_bounce;`, `grep 'location /srv/wiki/subject/' wiki/etc/nginx.conf` returns nothing, and the `/srv/wiki/static/` block contains no `auth_request`.
- The suite is green (`cd wiki && go build ./... && go vet ./... && go test ./...`), and the local nginx front door (`bin/start`) serves a public-scope page logged-out and bounces a private-tier navigation to sign-in.
