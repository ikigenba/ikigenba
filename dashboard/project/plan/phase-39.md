# Phase 39 — Rename the box-health surface from "telemetry" to "metrics", end to end

*Realizes design Decision 6 (AGENTS.md page truth), 14 (metrics HTTP surface),
and 16 (landing tile), and carries the renaming half of Decisions 11–13 and 15
(whose behavioral ids are already realized and keep their existing tests, moved
into the renamed package).*

The suite now has a `telemetry` service whose job is the forensic record of every
call in it. The dashboard's box-health graph page must stop using that word, so
this phase renames the whole surface — user-visible and internal — to **metrics**
in one sweep. Nothing about what the page shows or how it samples changes.

The end state:

- `dashboard/internal/telemetry/` is `dashboard/internal/metrics/` (`package
  metrics`), with its existing tests moved with it and their `R-` tags intact —
  the store, readers, discovery, collector, and chart builders are unchanged code
  under a new package name.
- `dashboard/internal/server/telemetry.go` is `metrics.go`, its handlers are
  `handleMetrics`/`handleMetricsFragment`, `server.Options.Telemetry` is
  `Options.Metrics`, and the `*app` field follows.
- The routes are `GET /metrics` and `GET /metrics/fragment`. `GET /telemetry`
  and `GET /telemetry/fragment` are **removed, not redirected**.
- `ui/html/telemetry.html` → `metrics.html`,
  `ui/html/partials/telemetry_charts.tmpl` → `metrics_charts.tmpl` (template name
  `metrics_charts`), both re-registered in the `ParseFS` list; the page's title
  and heading read *Metrics*.
- `ui/static/app.js`'s poll block targets `#metrics-block` and its
  `data-fragment` URL, keeping the 60000 ms cadence.
- `ui/html/index.html`'s signed-in tile links to `/metrics` and reads *Metrics*.
- `cmd/dashboard/main.go` constructs `metrics.NewStore()` and runs
  `metrics.Run(…)` on the `Workers` seam.
- `dashboard/AGENTS.md` states the current page truth (D6): four apex pages —
  login, landing, profile, metrics — no "single hybrid page"/"IAM console" rule,
  and an `internal/` package list naming `metrics` with no `telemetry` anywhere
  in the file.

**Done when:**

- These ids are covered by clearly-named tests and the suite is green:
  - R-WWGV-S5V0 — `GET /metrics` + valid session → 200, charts container present,
    title/heading read "Metrics", body contains no "Telemetry".
  - R-WXOS-5XLP — `GET /metrics` + no/invalid session → 303 to `/`, no charts.
  - R-WYWO-JPCE — `GET /metrics/fragment` + valid session → 200 with the
    charts-block markup.
  - R-X1CH-B8TS — `GET /metrics/fragment` + no session → 401, no charts.
  - R-X2KD-P0KH — the served `app.js` references `metrics-block`, fetches the
    fragment URL, uses `60000`, and contains no `telemetry` identifier.
  - R-X3SA-2SB6 — `GET /telemetry` and `GET /telemetry/fragment` with a **valid**
    session each return exactly 404 (not 200, not 301/302/303).
  - R-X506-GK1V — the signed-in landing renders `href="/metrics"` with link text
    `Metrics`, and no `/telemetry` href or the word "Telemetry".
  - R-X682-UBSK — the logged-out login page contains neither a `/metrics` nor a
    `/telemetry` link.
  - R-DB16-DOCS — `AGENTS.md` carries neither stale rule, states the four-page
    truth with token/grant management on the profile page, and names `metrics`
    (never `telemetry`) in its package list. This one is a text check on
    `AGENTS.md`, not a Go test.
  - The pre-existing ids of D11, D12, D13, and D15 (`R-EZVQ-IQOL`, `R-F13M-WIFA`,
    `R-F2BJ-AA5Z`, `R-F4RC-1TND`, `R-F5Z8-FLE2`, `R-F774-TD4R`, `R-F8F1-74VG`,
    `R-F9MX-KWM5`, `R-FAUT-YOCU`, `R-FC2Q-CG3J`, `R-FDAM-Q7U8`, `R-FEIJ-3ZKX`,
    `R-FFQF-HRBM`, `R-FGYB-VJ2B`, `R-FO9Q-65IH`, `R-FPHM-JX96`, `R-FQPI-XOZV`,
    `R-FRXF-BGQK`, `R-FT5B-P8H9`, `R-FUD8-307Y`, `R-FVL4-GRYN`) still appear as
    tags on passing tests in the renamed package — the rename must not drop
    coverage.
- Green means, from `dashboard/`: `go build ./...`, `go vet ./...`, `gofmt -l .`
  (no output), and `go test ./...` all succeed with zero failures.
- The rename is complete, checked from `dashboard/`:
  - `find . -path ./project -prune -o -iname '*telemetr*' -print` prints nothing
    (no file or directory carries the old name).
  - `grep -rl 'telemetr' --include='*.go' --include='*.html' --include='*.tmpl' --include='*.js' --include='*.css' --exclude='*_test.go' --exclude-dir=project .`
    exits non-zero with no output (the word survives only in the test that
    asserts the old routes are gone).
  - `grep -i 'telemetr' AGENTS.md` exits non-zero with no output, and
    `grep -c 'metrics' AGENTS.md` is at least 1.
