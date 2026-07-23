# Phase 40 — The landing page lists sites by name

*Realizes design Decision 19 (landing render) and Decision 6 (layout slice
R-ZGWN-ZZDW). Depends on Phase 38.*

The landing surface takes on the split: `siteRow` gains `Name` (beside `Slug`),
`share/www/landing.html` renders each row's **name** as the linked identity
(`<a href="{{.URL}}">{{.Name}}</a>` — free-form text, template-escaped), the
visible **Slug column is removed** and the first sortable header reads **Name**
(`data-sort-key="name"`), and the JSON data island elements carry `slug` +
`name` alongside the existing keys. The URL computation, session gate, copy
button markup, and empty state are unchanged.

Tests tagged with the retired ids (R-HK3X-22SM, R-WMWB-7EWX, R-HLBT-FUJB,
R-HMJP-TMA0, R-83NK-DUW1) are replaced by the new ids' tests; kept-id tests
over the same substrate (R-RC42-WMDU, R-IEWI-3MXP, R-NM1L-GSYE, R-WKGI-FVFJ
etc.) get seed/view-model call sites mechanically updated, keeping their tags.

**Done when:** each of R-ZI4K-DR4L (rows show names, token never the link
text), R-ZJCG-RIVA (name anchor over slug-built href), R-ZKKD-5ALZ (island
carries slug+name; empty `[]`), R-ZLS9-J2CO (unlisted name anchor at the public
segment) (D19), and R-ZGWN-ZZDW (Name/Creator/Created headers, no Slug column)
(D6) is covered by a clearly-named test tagged with its id, no retired-id tag
survives, and the suite is green per design Conventions. (The D22/D23 client
ids stay pending for Phase 41; the goja/chromedp suite must still pass at this
phase's end — adapt its fixtures mechanically if the island/markup change
requires it.)
