# Phase 9 — Serve the web surface from `share/www` through the chassis

*Realizes design Decision 11 (de-embed via `Spec.WWW`), carrying D1/D2/D3/D4/D7/D8's
retained ids onto the new substrate. Depends on phases 7–8 only for a settled
`main.go` and the registry-derived nginx test; depends on the appkit chassis
providing `Config.WWWPath`, `appkit/web`, and `Spec.WWW` (appkit design D5–D7),
consumed through the committed `replace appkit => ../appkit` as a fixed external
contract.*

Observable end state:

- `dropbox/share/www/landing.html` and
  `dropbox/share/www/static/{tokens.css,fonts/*}` exist with the former
  `internal/web` bytes (`tokens.css` + the four woff2 fonts:
  `space-grotesk.woff2`, `ibm-plex-sans.woff2`, `ibm-plex-mono-400.woff2`,
  `ibm-plex-mono-500.woff2`); `dropbox/internal/web/` no longer exists (no
  `//go:embed` of web assets remains anywhere in dropbox).
- `cmd/dropbox/main.go` sets `WWW: true`; the service-side `GET /static/` mount
  (`web.StaticHandler()`) is gone from `Handlers` (chassis-mounted); `GET /{$}`
  renders `landing.html` through `rt.WWW().Render(w, "landing.html", {Service, Version})`
  via the D1/D2 handler shape (404 a non-root path). The loopback `GET /content`
  and `GET /list` mounts and the `POST /mcp` mount stay exactly as they are.
- The landing/asset tests live in `cmd/dropbox`, loading the repo-real
  `share/www` via `appkit/web`, covering the retained D1/D3/D7/D8 ids and the new
  D11 ids; the D2 mux tests and the nginx fragment tests (D4/D8/D10 — already
  registry-derived from phase 8) are rewired/relocated to `cmd/dropbox` with their
  assertions intact.
- `bin/start`'s `launch_dropbox` exports
  `DROPBOX_WWW_PATH="$repo/dropbox/share/www"` beside its existing
  `DROPBOX_DB_PATH` / `DROPBOX_MIRROR_PATH` exports (the D11 boundary-crossing
  line, **verified by the live `bin/start` smoke, not the Go suite**).

**Done when:** the suite is green — `cd dropbox && go build ./...`,
`cd dropbox && go vet ./...`, `cd dropbox && gofmt -l .` (no output),
`cd dropbox && go test ./...`, and `bin/check-migrations dropbox` all succeed with
zero failures — and:

- R-QO40-U0VH and R-QPBX-7SM6 (D11) are covered by clearly-named tests over the
  real `dropbox/share/www` tree;
- R-LAND-3C9X, R-LAND-5E2Y, R-LAND-7G4Z, R-LAND-9J6A (D1), R-ROUT-2B5C,
  R-ROUT-4D7E, R-ROUT-6F9G (D2), R-ASST-3H6J, R-ASST-5K8L, R-ASST-7M1N (D3),
  R-HOME-6P8T (D7), R-LQXL-095Q, R-LS5H-E0WF, R-LTDD-RSN4, R-LULA-5KDT,
  R-LVT6-JC4I (D8), and R-NGNX-2P4Q, R-NGNX-4R6S, R-NGNX-6T8U, R-NGNX-8V1W (D4),
  plus R-QLO8-2HE3, R-QMW4-G94S (D10), remain covered by tests on the new
  substrate;
- `ls dropbox/internal/web 2>/dev/null` reports no such directory, and
  `grep -rn "go:embed" dropbox/cmd dropbox/internal --include=*.go | grep -v internal/db`
  returns no matches.
