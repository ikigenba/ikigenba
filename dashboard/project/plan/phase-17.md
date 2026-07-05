# Phase 17 — Chart rendering: hero line charts + stacked-area charts as pure SVG

*Realizes design Decision 15 (chart rendering). Depends on Phase 15 (the `Snapshot`
type the builders consume). Adds pure SVG builder functions + `humanBytes` +
the committed `bandPalette` to `internal/telemetry`; no HTTP handler, no route, no
`cmd/` change. Confined to the dashboard tree.*

Build the pure, unit-testable chart geometry the telemetry page (Phase 18) will
embed. Every builder is a pure function of a `telemetry.Snapshot` returning inline
SVG as `template.HTML`; no client library, no external asset.

**1. Hero line charts (D15).** `heroChart(title, samples, total)` — a polyline over
the series with the **y-axis mapped `0 → total`** (a sample equal to `total` plots
at the top of the plot area, `0` on the baseline), the latest sample shown as a
**current-value label** via `humanBytes`, and native SVG `<title>` per vertex for
hover (no JS crosshair). Stroke uses the Carbon accent token.

**2. Stacked-area charts (D15).** `stackedChart(title, series, order)` — services
sorted by latest value descending, the **top 7** as their own bands and the rest
folded into one **"Other"** band; the stack is **accumulated** (top of the stack at
each x = the sum of all services' values there); light-fill bands with a hairline
top edge, a 2px surface gap, and a **legend** naming every band; each visible band
drawn with a **distinct** color from the committed CVD-validated `bandPalette`.

**3. `humanBytes(int64) string` (D15)** — binary units: `1073741824 → "1.0 GiB"`,
`1048576 → "1.0 MiB"`, `0 → "0 B"`.

**4. `bandPalette`** — the fixed, ordered, colorblind-safe categorical set (8 hues),
committed as constants (validated with the dataviz validator at authoring time; the
suite asserts the render *uses* it, not that CI re-validates).

**5. Tests** in `internal/telemetry/*_test.go` assert computed geometry/coordinates,
band folding, legend presence, distinct band colors, and the `humanBytes`
boundaries — no pixel/screenshot diffing.

**Done when:** the suite is green — `cd dashboard && go build ./...`, `go vet
./...`, `gofmt -l .` (no output), `go test ./...`, and `bin/check-migrations
dashboard` (from the repo root) all succeed with zero failures — and every id below
is covered by a genuine named test:

- **R-FO9Q-65IH** — `humanBytes` formats binary units at the GiB/MiB/0 boundaries.
- **R-FPHM-JX96** — hero y-axis maps `0 → total` (top-for-total, baseline-for-0).
- **R-FQPI-XOZV** — hero renders the latest sample as a current-value label.
- **R-FRXF-BGQK** — stacked chart folds >7 services into one "Other" band (none at ≤7).
- **R-FT5B-P8H9** — stacked chart is accumulated (stack top = sum at each x).
- **R-FUD8-307Y** — stacked chart renders a legend naming every band.
- **R-FVL4-GRYN** — each visible band uses a distinct `bandPalette` color.
