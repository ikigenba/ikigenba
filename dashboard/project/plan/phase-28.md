# Phase 28 — Rewrite the login page's name-origin colophon copy

*Realizes design Decision 7 (login composition — name-origin colophon), the R-HBWF-GM4D slice.*

The logged-out `GET /` name-origin colophon carries the new copy. In the
`{{else}}` branch of `ui/html/index.html`, the `name-origin` aside's lede reads
`<b>ikigenba</b> — The place life actually happens.` and its two
`name-origin-parts` items are `iki` / 生き / `= to live` and `genba` / 現場 /
`= the place`, with each `dt`'s **whole** romaji word wrapped in
`<b class="seam">`. The retired copy is gone: no `portmanteau`, `生き甲斐`,
`ikigai`, `reason for being`, or `the actual place` anywhere in the rendered
page. Everything else in D7 is untouched — the aside's structure and classes,
the CTA-relative placement, the centered `name-origin-say` pronunciation foot
(R-O7K1-XEN7), the logged-out-only rule (R-DB19-LAND), and the whole
`ui/static/app.css` name-origin block, which needs no change.

`internal/server/index_test.go` currently pins the retired copy under the
deleted id `R-DB17-ORIG`. That id is gone from design: delete its tag and
replace those assertions with the R-HBWF-GM4D behavior. No `R-DB17-ORIG` tag may
remain in the tree.

**Done when:**

- `internal/server/index_test.go` has a clearly-named test tagged
  `R-HBWF-GM4D` asserting the logged-out body contains the exact lede paragraph
  and both new `dt`/`dd` pairs; that the aside holds exactly two `dt`, two `dd`,
  two `<b class="seam">`, two `span lang="ja"`, and one `dl.name-origin-parts`;
  that each `dt`'s romaji is fully inside its `seam`; and that the body contains
  none of `portmanteau`, `生き甲斐`, `ikigai`, `reason for being`, `the actual
  place`.
- `grep -rn 'R-DB17-ORIG' --exclude-dir=project .` exits non-zero (no matches).
- `go build ./...` succeeds and `go test ./...` is green.
