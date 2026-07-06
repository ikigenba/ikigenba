# Phase 08 — Serve the web surface from `share/www` through the chassis

*Realizes design Decision 12 (de-embed via `Spec.WWW`), carrying D1/D2/D3's
retained ids onto the new substrate. Depends on no earlier crm phase; depends
on the appkit chassis providing `Config.WWWPath`, `appkit/web`, and `Spec.WWW`
(appkit plan Phases 05–07), consumed through the committed
`replace appkit => ../appkit` as a fixed external contract.*

Observable end state:

- `crm/share/www/landing.html` and `crm/share/www/static/{tokens.css,fonts/*}`
  exist with the former `internal/web` bytes; `crm/internal/web/` no longer
  exists (no `//go:embed` of web assets remains anywhere in crm).
- `cmd/crm/main.go` sets `WWW: true`; the `GET /static/` mount is gone from
  `Handlers` (chassis-mounted); `GET /{$}` renders `landing.html` through
  `rt.WWW()` via the D1 handler shape.
- The landing/asset tests live in `cmd/crm`, loading the repo-real `share/www`
  via `appkit/web`, covering the retained D1/D3 ids and the new D12 ids; the
  D2 mux tests are rewired to the new handler.
- `bin/start`'s `launch_crm` exports `CRM_WWW_PATH="$repo/crm/share/www"`
  (the D12 boundary-crossing line, verified by the live smoke, not Go tests).

**Done when:** the suite is green — `cd crm && go build ./...`,
`cd crm && go vet ./...`, `cd crm && gofmt -l .` (no output), and
`cd crm && go test ./...` all succeed with zero failures — and:

- R-MTM5-0PXH and R-MUU1-EHO6 (D12) are covered by clearly-named tests over the
  real `crm/share/www` tree;
- R-LAND-2K7P, R-LAND-4M9Q, R-LAND-6N3R, R-LAND-8P5S (D1), R-ROUT-3T2V,
  R-ROUT-5W4X, R-ROUT-7Y6Z (D2), R-ASST-2B8C, R-ASST-4D1E, R-ASST-6F3G (D3),
  and R-SRS9-B2RI, R-ST05-OUI7, R-SU82-2M8W, R-SVFY-GDZL (D8's Go-side ids)
  remain covered by tests on the new substrate;
- `ls crm/internal/web 2>/dev/null` reports no such directory, and
  `grep -rn "go:embed" crm/cmd crm/internal --include=*.go | grep -v internal/db | grep -v internal/mcp`
  returns no matches;
- `diff crm/share/www/static/tokens.css cron/internal/web/static/tokens.css`
  (repo root) prints nothing (D6's conform check on the moved file).
