# Phase 41 — The client controls filter and sort by name

*Realizes design Decision 22 (client logic slice R-02UU-VUQE, R-042R-9MH3,
R-05AN-NE7S) and Decision 23 (browser wiring slice R-06IK-15YH, R-08YC-SPFV).
Depends on Phase 40.*

`share/www/static/landing.js` takes on the split: `filterSites` matches the
query as a case-insensitive subsequence of the display **name or the slug**,
`sortRows`' `name` key sorts the **display name** case-insensitively with the
slug tie-break, and the controller's row rebuild renders the name anchor via
`textContent`. The chromedp scenario re-seeds per D23 (names distinct from
slugs, name order ≠ slug order) and re-proves the filter and Name-header sort
wiring against the real browser.

Tests tagged with the retired ids (R-HU67-LJBW, R-HVE3-ZB2L, R-HZ1T-4MAO in
goja; R-88J5-WXUT, R-89R2-APLI in the browser scenario) are replaced by the new
ids' tests; the other goja/browser ids keep their tags with fixtures
mechanically updated where row objects gain `name`.

**Done when:** each of R-02UU-VUQE (subsequence over name or slug), R-042R-9MH3
(case-insensitive over both fields), R-05AN-NE7S (name sort semantics) (D22),
R-06IK-15YH (filter wiring shows the name), and R-08YC-SPFV (Name-header sort
wiring) (D23) is covered by a clearly-named test tagged with its id, no
retired-id tag survives, and the suite is green per design Conventions
(including the headless-Chrome test).
