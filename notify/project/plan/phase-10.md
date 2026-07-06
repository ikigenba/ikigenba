# Phase 10 — Serve the web surface from `share/www` through the chassis

*Realizes design Decision 12 (de-embed via `Spec.WWW`), carrying D1/D2/D3/D7/D8's
retained ids onto the new substrate. Depends on Phase 09 only for a settled
`main.go`; depends on the appkit chassis providing `Config.WWWPath`,
`appkit/web`, and `Spec.WWW` (appkit plan Phases 05–07), consumed through the
committed `replace appkit => ../appkit` as a fixed external contract.*

Observable end state:

- `notify/share/www/landing.html` and
  `notify/share/www/static/{tokens.css,fonts/*}` exist with the former
  `internal/web` bytes; `notify/internal/web/` no longer exists (no
  `//go:embed` of web assets remains anywhere in notify).
- `cmd/notify/main.go` sets `WWW: true`; the service-side `GET /static/` mount
  is gone from `Handlers` (chassis-mounted); `GET /{$}` renders `landing.html`
  through `rt.WWW()` via the D1 handler shape.
- The landing/asset tests live in `cmd/notify`, loading the repo-real
  `share/www` via `appkit/web`, covering the retained D1/D3/D7/D8 ids and the
  new D12 ids; the D2 mux tests and the nginx fragment tests (D4/D8/D10) are
  rewired/relocated to `cmd/notify` with their assertions intact.
- `bin/start`'s `launch_notify` exports
  `NOTIFY_WWW_PATH="$repo/notify/share/www"` (the D12 boundary-crossing line,
  verified by the live smoke, not Go tests).

**Done when:** the suite is green — `cd notify && go build ./...`,
`cd notify && go vet ./...`, `cd notify && gofmt -l .` (no output), and
`cd notify && go test ./...` all succeed with zero failures — and:

- R-4FW1-V9QL and R-4H3Y-91HA (D12) are covered by clearly-named tests over the
  real `notify/share/www` tree;
- R-LAND-3C8K, R-LAND-5D1M, R-LAND-7E4N, R-LAND-9F6P (D1), R-ROUT-4G2Q,
  R-ROUT-6H5R, R-ROUT-8J7S (D2), R-ASST-3K9T, R-ASST-5L2V, R-ASST-7M4W (D3),
  R-HOME-5N7S (D7), R-8JS0-IQDX, R-8KZW-WI4M, R-8M7T-A9VB, R-8NFP-O1M0,
  R-8ONM-1TCP (D8), R-NGNX-3N6X, R-NGNX-5P8Y, R-NGNX-7Q1Z, R-NGNX-9R3B (D4),
  and R-RGNL-4E5P, R-RGDR-4F6Q (D10) remain covered by tests on the new
  substrate;
- `ls notify/internal/web 2>/dev/null` reports no such directory, and
  `grep -rn "go:embed" notify/cmd notify/internal --include=*.go | grep -v internal/db`
  returns no matches;
- `diff notify/share/www/static/tokens.css cron/internal/web/static/tokens.css`
  (repo root) prints nothing (D6's conform check on the moved file).
