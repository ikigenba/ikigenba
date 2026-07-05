# Phase 18 — The telemetry page: routes, refresh fragment, store wiring, and the 60s poll

*Realizes design Decision 14 (HTTP surface). Depends on Phase 16 (the shared store
is created + collected in main) and Phase 17 (the chart builders the page embeds).
Touches `internal/server/` (new handlers, `routes.go`, `server.go`
`Options`/`*app`/`ParseFS`), `cmd/dashboard/main.go` (thread the store into
`server.Register`), `ui/html/telemetry.html` + `ui/html/partials/telemetry_charts.tmpl`,
and `ui/static/app.js`. No schema. Confined to the dashboard tree.*

Stand up the owner-only telemetry page and its once-a-minute refresh.

**1. Wire the store into the server (D14).** `server.Options` gains `Telemetry
*telemetry.Store`; `*app` gains the field; `newApp` stores it. `cmd/dashboard/main.go`
passes the shared store (from Phase 16) into `server.Register`. Add
`html/telemetry.html` and `html/partials/telemetry_charts.tmpl` to the
`template.ParseFS(ui.Files, …)` list in `server.go`.

**2. The two handlers (D14).** `handleTelemetry` — session-gated full page:
signed-out **redirects `303 → /`**; signed-in renders `telemetry.html` from a fresh
`Snapshot()` (the Phase 17 hero + stacked charts), buffered-then-written.
`handleTelemetryFragment` — renders just the `telemetry_charts` partial from a
`Snapshot()`; signed-out returns **`401`**. Register both in `routes.go`
(`GET /telemetry`, `GET /telemetry/fragment`).

**3. The poll (`app.js`) (D14).** A block that finds `#telemetry-block`, reads its
`data-fragment` URL, and every **60000 ms** `fetch`es it (`credentials:
"same-origin"`) and swaps `innerHTML`, leaving the stale block on a non-OK response.
The telemetry template carries the matching `id` + `data-fragment` on its charts
container.

**4. Tests** co-located in `internal/server/*_test.go` (package `server`) drive the
real route table via `(*app).routes()` with a minted session (signed in) and without
(signed out), asserting status, `Location`, and rendered markers; a served-asset
assertion checks `app.js` wires the poll.

**Done when:** the suite is green — `cd dashboard && go build ./...`, `go vet
./...`, `gofmt -l .` (no output), `go test ./...`, and `bin/check-migrations
dashboard` (from the repo root) all succeed with zero failures — and every id below
is covered by a genuine named test:

- **R-FI68-9AT0** — `GET /telemetry` signed in → `200` with the charts container.
- **R-FJE4-N2JP** — `GET /telemetry` signed out → `303`, `Location: /`, no charts.
- **R-FKM1-0UAE** — `GET /telemetry/fragment` signed in → `200` with the charts block.
- **R-FLTX-EM13** — `GET /telemetry/fragment` signed out → `401`, no charts.
- **R-FN1T-SDRS** — served `app.js` fetches the telemetry fragment on a `60000` ms interval.
