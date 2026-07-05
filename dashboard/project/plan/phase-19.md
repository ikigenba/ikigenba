# Phase 19 — Landing entry: a tile linking to the telemetry page

*Realizes design Decision 16 (landing entry). Depends on Phase 18 (the
`/telemetry` page exists). Touches `ui/html/index.html` (signed-in branch) and
the landing test in `internal/server/`; reuses existing tile/list chrome + CSS —
no new component, no route, no schema. Confined to the dashboard tree.*

Give the signed-in landing its one entry point to telemetry.

**1. The tile (D16).** In `ui/html/index.html`, inside the signed-in
`{{if .Owner}}` branch (near the service list), add a link whose href is the
literal `/telemetry`, styled with the landing's existing tile/list chrome. The
logged-out login branch gains **no** such link. No banner change (D10 stands), no
service-row change (D5 stands).

**2. Tests.** Extend the landing/composition test in `internal/server/*_test.go`:
assert `GET /` **with** a session contains an anchor to `/telemetry`, and `GET /`
**without** a session does not.

**Done when:** the suite is green — `cd dashboard && go build ./...`, `go vet
./...`, `gofmt -l .` (no output), `go test ./...`, and `bin/check-migrations
dashboard` (from the repo root) all succeed with zero failures — and both ids
below are covered by a genuine named test:

- **R-FWT0-UJPC** — signed-in `GET /` renders a link with href `/telemetry`.
- **R-FY0X-8BG1** — logged-out `GET /` contains no `/telemetry` link.
