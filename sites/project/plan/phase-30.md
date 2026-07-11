# Phase 30 — Fix the copy control: SVG-namespaced icon + width-stable label, proven in the browser

*Realizes design Decision 22 (the copy icon built via `createElementNS` in the
row rebuild), Decision 6 (the width-stable `.copy-label` / action-cell CSS), and
Decision 23 (two assertions added to the single headless-Chrome scenario +
`R-VYEF-053C` and `R-VZMB-DWU1`). Depends on Phase 28 (the copy button and its
CSS this phase corrects), Phase 24/25 (the `initController` row rebuild this
phase corrects), and Phase 29 (the chromedp copy step this phase extends).*

Two defects shipped in the copy control (v0.14.1): the copy button's **icon is
missing** once JS boots, and clicking it **reflows the whole table** as the label
swaps `Copy`→`Copied`. Both live in code already built — D22's controller
rebuilds each row's icon with `document.createElement("svg")`+`innerHTML` (an
HTML-namespaced node the browser will not paint), and D6's `.copy-label` reserves
no width. This phase corrects both and adds the real-browser proof; it is a
correction to the shipped surface plus a test extension (no new browser launch —
the copy step from Phase 29 grows two assertions).

- **`sites/share/www/static/landing.js`** — in the row rebuild, construct the
  copy button's glyph in the SVG namespace: the icon `<svg>` and its
  `<rect>`/`<path>` children via
  `document.createElementNS("http://www.w3.org/2000/svg", …)` (drop
  `createElement("svg")` + `innerHTML`), so the JS-rebuilt row renders the same
  glyph the server markup carries.
- **`sites/share/www/landing.html`** (the page `<style>`) — make the copy control
  width-stable: give `.copy-label` a `min-width` sized to hold `Copied` (from the
  existing token scale, no new token values) with its text right-aligned, and
  right-align the button within the far-right action cell, so the button's right
  edge is pinned to the table edge and the reserved slack sits to the left. The
  `Copy`→`Copied` swap must not change the button, cell, or table width. The
  `.site-table`'s own styling stays untouched.
- **`sites/cmd/sites/main_test.go`** (or the existing chromedp harness file) — in
  the same session as Phase 29's copy step and with no new launch: before the
  click, assert the copy button's glyph is an element in the SVG namespace
  (`namespaceURI === "http://www.w3.org/2000/svg"`) with a non-zero
  `getBoundingClientRect()` box, and record the table's rendered width; after the
  label reads `Copied`, assert the table's rendered width equals the pre-click
  measurement.

**Done when:** the sites suite is green (`cd sites && go build ./...`, `go vet
./...`, `gofmt -l .` prints nothing, `go test ./...`, with `google-chrome` on
`PATH` per D23's hard requirement), AND both ids are covered:
- R-VYEF-053C — in the seeded headless-Chrome session, after boot the copy glyph
  inside a row's JS-rebuilt button is an element in the SVG namespace
  (`namespaceURI === "http://www.w3.org/2000/svg"`) whose `getBoundingClientRect()`
  width and height are both `> 0`. A controller building the icon with
  `document.createElement("svg")` + `innerHTML` yields an HTML-namespaced,
  unrendered (zero-box) node and fails.
- R-VZMB-DWU1 — in the same session, the table's rendered width measured
  immediately before clicking a row's copy button equals its rendered width after
  the label has swapped to `Copied`. A label that is not width-reserved grows the
  button and reflows the table, and fails.
