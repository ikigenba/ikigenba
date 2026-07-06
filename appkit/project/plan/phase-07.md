# Phase 07 — Chassis integration: `Spec.WWW`, auto-mounted static, `Router.WWW()`

*Realizes design Decision 7 (chassis integration). Depends on Phase 05 (the
resolved `WWWPath`) and Phase 06 (the `appkit/web` package).*

The root `appkit` package gains the `Spec.WWW bool` field; `runServe` loads the
site via `web.Load(cfg.WWWPath)` when it is set (a load failure fails the serve
verb with an error naming the path) and threads it into `server.Options` as a
new `WWW *web.Site` field. `appkit/server.New` mounts `GET /static/` →
`site.Static()` in the standard non-apex route table when `Options.WWW` is
non-nil, and `Router` gains the `WWW() *web.Site` accessor (nil when unset).
Non-serve verbs never touch the www root. A Spec without `WWW` is bit-for-bit
unchanged: no `/static/` route, `rt.WWW()` nil, the pre-existing appkit suite
passing untouched.

The one-line dev wiring in a converted service's `bin/start` launch function
(exporting `<APP>_WWW_PATH="$repo/<app>/share/www"`) is deliberately **not**
in this phase — it crosses the `appkit/` boundary and lands with the first
converted service (crm's adoption plan), verified by the live `bin/start`
smoke there.

**Done when:** the suite is green (design Conventions commands, from `appkit/`)
and R-M7NY-4UKZ, R-M8VU-IMBO, R-MA3Q-WE2D, and R-MBBN-A5T2 are each covered by
a clearly-named test at the `server.New`/`httptest` seam (the missing-root id
at the serve-path seam that calls `web.Load`) genuinely asserting the behavior
its D7 Verification line describes.
