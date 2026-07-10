# Phase 29 — Browser proof: clicking a row's copy button lands its URL on the clipboard

*Realizes design Decision 23 (the copy step added to the single headless-Chrome
scenario + the `R-NN9H-UKP3` Verification). Depends on Phase 26 (the chromedp
harness and its seeded auth-free `httptest` landing page this phase extends) and
Phase 28 (the copy button must exist to be clicked).*

The copy button's structure is proven by Phase 28, but that a real click actually
reaches the system clipboard is a browser fact no structural or goja test can
see. This phase extends the existing single chromedp session with **one more
step** — no new browser launch (the minimal-chromedp principle: cost is measured
in sessions, not actions). Test-only code; the shipped binary is unchanged and
the import-graph boundary (`R-8EMN-TSKA`) still holds.

- **`sites/cmd/sites/main_test.go`** (or the existing chromedp harness file) —
  grant the browser context clipboard permission for the test origin (CDP
  `Browser.grantPermissions`, `clipboardReadWrite`), then, in the same session
  after the existing boot/filter/sort/clear/page steps, click one row's copy
  button, read the clipboard back with `navigator.clipboard.readText()`, and
  assert it equals that row's absolute front-door URL; assert the clicked
  button's label reads "Copied". The `httptest` origin is `127.0.0.1` (a secure
  context), so the real async-clipboard path is exercised, not the fallback.

**Done when:** the sites suite is green (`cd sites && go build ./...`, `go vet
./...`, `gofmt -l .` prints nothing, `go test ./...`, with `google-chrome` on
`PATH` per D23's hard requirement), AND `R-NN9H-UKP3` is covered:
- R-NN9H-UKP3 — in the seeded headless-Chrome session (clipboard permission
  granted, page served from `127.0.0.1`), clicking a row's copy button then
  reading `navigator.clipboard.readText()` returns **that row's absolute
  front-door URL** (byte-identical to the row's slug-anchor `href`), and the
  clicked button's label reads "Copied". A dead copy listener, a button that
  copies the wrong string or nothing, or one that never confirms fails.
