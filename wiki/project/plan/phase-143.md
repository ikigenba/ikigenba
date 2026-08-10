# Phase 143 — Ask page links footer matches the subject page's two-column block

*Realizes design Decision 44 (the ask result page — links-footer slice: R-2L34-0E1U, R-2MB0-E5SJ).*

The ask result page's footer becomes the same quiet two-column `links` block the
subject page renders (D45): in `share/www/home.tmpl`, the two bare navs (cited
pages, mentioned subjects) are replaced by a single `class="links"` wrapper
holding a **Cited pages** nav (from `.Cites`, first) and a **Mentions** nav
(from `.Mentions`, second), each with its `<h2>` heading — the same markup shape
`subject.tmpl` emits, so the existing D80 `.links` CSS in `layout.tmpl` styles
both pages identically with no new CSS. A nav is omitted when its list is
empty; the whole wrapper is omitted when both are empty. The web handler
already supplies `Cites` (absolute, tier+scope-composed hrefs) and `Mentions`;
no handler or seam change is expected beyond what the template needs.

Existing D44 tests whose behaviors were resharpened are updated to the current
Verification wording: R-AU31-XI76 (Mentions nav labeled, absolute links),
R-AVAY-B9XV (symmetric omission: one empty list drops only its nav; both empty
drop the wrapper entirely), R-AWIU-P1OK (honest-empty page carries no `links`
wrapper), and R-AXQR-2TF9 (end-to-end page carries the `Mentions` nav).

**Done when:**

- R-2MB0-E5SJ — both footer navs render inside one `class="links"` wrapper,
  Cited pages before Mentions — covered by a named test tagged with the id.
- R-2L34-0E1U — the Cited pages nav links each citation by its absolute
  front-door URL under a `Cited pages` heading — covered by a named test
  tagged with the id.
- The existing tests tagged R-AU31-XI76, R-AVAY-B9XV, R-AWIU-P1OK, and
  R-AXQR-2TF9 assert the resharpened behaviors above and pass.
- `go test ./...` from `wiki/` is green.
