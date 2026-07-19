# Phase 30 — Rebuild the login page composition around a brand title and a borderless etymology table

*Realizes design Decision 7 (login composition).*

Rewrite the `{{else}}` (logged-out) branch of `dashboard/ui/html/index.html` and
the associated rules in `dashboard/ui/static/app.css` to match D07's new markup
and CSS exactly: the brand title `<h1 class="signin-title">Ikigenba</h1>`, the
`<p class="signin-tagline">`, the `<p class="signin-lede">`, the
`<hr class="signin-rule">` placed immediately above the `Sign in with Google`
CTA, the unchanged CTA itself, and a borderless `<table class="name-origin-table">`
etymology block (replacing the retired `dt`/`dd` list) followed by the unchanged
`name-origin-say` pronunciation line.

Update `dashboard/internal/server/index_test.go`: delete or rewrite the tests
tagged `R-HBWF-GM4D`, `R-JNSL-OLCI`, and `R-DB18-KEEP` (their asserted markup no
longer exists — these ids are retired from the design and must not remain
referenced in the suite), and add new tests tagged `R-JA3I-IY1F`, `R-JCJB-AHIT`,
`R-JDR7-O99I`, `R-JEZ4-2107`, `R-JG70-FSQW`, and `R-JHEW-TKHL` per D07's
Verification list. Update the existing `R-O7K1-XEN7` and `R-DB19-LAND` tests so
they assert against the new markup (pronunciation foot placed after
`name-origin-table`; colophon/title/tagline/lede/rule elements absent from the
signed-in landing) while keeping their same ids.

**Done when:**
- `R-JA3I-IY1F`, `R-JCJB-AHIT`, `R-JDR7-O99I`, `R-JEZ4-2107`, `R-JG70-FSQW`,
  `R-JHEW-TKHL`, `R-O7K1-XEN7`, and `R-DB19-LAND` each appear as a tag comment
  in `dashboard/internal/server/index_test.go` on a test that asserts the exact
  behavior its id names in `project/design/D07.md`.
- `R-HBWF-GM4D`, `R-JNSL-OLCI`, and `R-DB18-KEEP` no longer appear anywhere
  under `dashboard/` (grep returns nothing) — their retired behavior is not
  left half-tested.
- `cd dashboard && go build ./... && go vet ./... && gofmt -l . && go test ./...`
  all succeed with zero failures and `gofmt -l .` prints no output.
