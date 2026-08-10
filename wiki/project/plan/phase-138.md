# Phase 138 — Header brand mark + the question form moves into the header on every page

*Realizes design Decision 80 (page shell — the new brand-mark and universal
header-form slice: R-2MYX-Y4PN, R-2O6U-BWGC, R-2PEQ-PO71, R-2QMN-3FXQ), with
the coupled in-place truings of D41, D43, D44, and D45.*

The shared shell (`share/www/layout.tmpl` + the layout CSS) gains the brand
mark as the header's first element — an anchor with `href="/"` wrapping the
committed `share/www/static/logo.png` (already in the tree; served by the
existing static route), rendered 32px tall — and renders the compact question
form in the header on **every** page of both tiers (it is no longer gated to
subject pages), the input carrying the submitted `q` as its `value` on result
pages and empty otherwise. The home template (`share/www/home.tmpl`) drops its
body question form, leaving title + suggested pages, and the answer state
drops the "ask another question" link. D79 labels and busy behavior are
unchanged and now ride the header form everywhere.

Retired ids whose behaviors are gone: **R-OMRY-L9O8** (home body form),
**R-Y42U-45K2** (subject-only header form), **R-ASV5-JQGH** (ask-another
link) — delete their tagged tests with the behaviors. The tests tagged
R-AVAY-B9XV and R-AWIU-P1OK are updated to their rewritten assertions (no
"ask another question" control expected).

**Done when:**

- Each of R-2MYX-Y4PN (brand mark first in header, `href="/"`, `img` →
  `static/logo.png` served 200 `image/png`, precedes Home), R-2O6U-BWGC
  (header question form on every state of both tiers, GET, input named `q`,
  tier labels), R-2PEQ-PO71 (input `value` = submitted query on `?q=` pages,
  empty on no-`q` pages), and R-2QMN-3FXQ (exactly one `q` input per
  document, none in `main`, no "ask another question" anchor) is covered by a
  test tagged with the id verbatim.
- `grep -rn 'R-OMRY-L9O8\|R-Y42U-45K2\|R-ASV5-JQGH' --include='*_test.go' .`
  from `wiki/` returns no matches (the retired ids' tests are deleted).
- The suite is green: `go test ./...` from `wiki/` exits 0.
