# Phase 24 — apex login-bounce nginx primitive (`@login_bounce`)

*Realizes design Decision 20 (apex login-bounce primitive) — ids R-XJBT-7YIF,
R-XKJP-LQ94. Depends on no earlier phase (a self-contained change to the apex
nginx fragment plus its content-assertion test).*

Adds one dashboard-owned, box-global named location — `@login_bounce` — to the
apex server block in `dashboard/etc/nginx.conf`, and nothing else in that file
changes. The block bounces a logged-out **navigation** (or a request with no
`Sec-Fetch-Mode`) to `/login?return_to=$request_uri` via a fallthrough
`return 302`, and answers the three scripted fetch modes (`cors`, `same-origin`,
`no-cors`) with a bare `return 401`, using the return-only `if` form so the whole
primitive stays inside the server-block fragment (no `http{}`-scope `map`, no
`opsctl` change). Services opt in later from their own `project/` with a single
`error_page 401 = @login_bounce;` line; this phase does **not** touch any service
fragment. The dev front-door mirror (`nginx/nginx.conf`, repo root) is suite
infrastructure outside this tree and is not built here.

The proof is a Go **content-assertion** test (in `cmd/dashboard`, reading
`etc/nginx.conf` from the module root, the pattern the sibling `sites` service
uses) — nginx is not run by the suite.

**Done when:** the suite is green (per design *Conventions*) and each id below is
covered by a clearly-named test that reads `dashboard/etc/nginx.conf` from disk
and asserts over its content:

- R-XJBT-7YIF — the file defines a `location @login_bounce` block whose
  fallthrough is `return 302 /login?return_to=$request_uri;` (so a `navigate`
  request and one with empty/absent `Sec-Fetch-Mode` both reach the bounce), and
  the redirect target is `/login` carrying `return_to=$request_uri`.
- R-XKJP-LQ94 — the same block matches each of `cors`, `same-origin`, and
  `no-cors` on `$http_sec_fetch_mode` and answers `return 401;` — all three
  present, so a scripted fetch is never bounced.
