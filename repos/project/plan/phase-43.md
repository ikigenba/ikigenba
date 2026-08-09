# Phase 43 — Browser wiring proof (chromedp)

*Realizes design Decision 26 (browser wiring proof). Depends on Phase 42.*

Build the single headless-Chrome test D26 shapes: an `httptest` server over
the real landing handler, a real migrated store, and twelve real bare
repositories (real commits, exactly one key carrying the subsequence `cdm` —
`code/demo`), the repo-real `share/www` assets, clipboard permission granted
via CDP, one launch retry, no scenario retries, ~30s timeout. One session
touches each control once: boot, filter, sort toggle, clear, page, copy (with
the icon-namespace and width-stability assertions). `github.com/chromedp/chromedp`
joins `go.mod` test-only. `AGENTS.md`'s Tests section grows the
`google-chrome` precondition (D16's declaration id already asserts the
section's completeness).

**Done when:** the suite is green and these ids are covered by clearly-named
tagged tests:

- R-SPNZ-LS3V — boot reveals the hidden controls
- R-SQVV-ZJUK — typing `cdm` leaves exactly the `code/demo` row
- R-SS3S-DBL9 — Name-header clicks toggle order and stamp `aria-sort`
- R-STBO-R3BY — Clear restores the default first-page listing
- R-SUJL-4V2N — pager pages 12 rows as `Page 1 of 2` / `Page 2 of 2`
- R-SVRH-IMTC — the clipboard receives origin + clone path; label reads
  `Copied`
- R-SWZD-WEK1 — the rebuilt copy icon is SVG-namespaced with a non-zero box
- R-SY7A-A6AQ — table width identical before and after the copy click
- R-SZF6-NY1F — `go list -deps ./cmd/repos` contains neither chromedp nor
  goja
