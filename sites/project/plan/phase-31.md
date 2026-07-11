# Phase 31 — session-gated locations opt into the apex `@login_bounce`

*Realizes design Decision 24 (opt into `@login_bounce`). Depends on no earlier
phase (a purely additive change to `sites/etc/nginx.conf` plus its
content-assertion test).*

Adds one line — `error_page 401 = @login_bounce;` — to each of the three
session-gated locations in `sites/etc/nginx.conf`: the exact-match landing root
`= /srv/sites/`, the landing-asset tier `/srv/sites/static/`, and the private
tier `/srv/sites/private/`. Each retains its `auth_request /_session-authn;` and
`proxy_pass`; nothing else in the fragment changes. The public tier
`/srv/sites/public/` and the bearer `= /srv/sites/mcp` deliberately do **not** get
the line. `@login_bounce` is a dashboard-owned apex external contract (defined by
the dashboard's own `project/`, D20) that sites only references — like
`/_session-authn`. The proof extends the existing nginx content-assertion test
(`cmd/sites/main_test.go`, reading `../../etc/nginx.conf`); nginx is not run by the
suite.

**Done when:** the suite is green (per design *Conventions*, including the
headless-Chrome gate and `bin/check-migrations sites`) and each id below is
covered by a clearly-named test reading `sites/etc/nginx.conf` from disk:

- R-XVIT-1NXD — each of `= /srv/sites/`, `/srv/sites/static/`, and
  `/srv/sites/private/` contains both `auth_request /_session-authn;` and
  `error_page 401 = @login_bounce;`.
- R-XWQP-FFO2 — `location /srv/sites/public/` and `= /srv/sites/mcp` do **not**
  contain `error_page 401 = @login_bounce;`.
- R-XXYL-T7ER — the change is additive: every pre-existing location still appears
  and each session-gated location keeps its `auth_request /_session-authn;` and
  `proxy_pass` (nothing removed or rewritten).
