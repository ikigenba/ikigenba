# Phase 29 — Render each name-origin part on one line

*Realizes design Decision 7 (login composition — name-origin colophon), the R-JNSL-OLCI slice.*

The logged-out `GET /` name-origin colophon renders each part as a single
baseline row — `iki 生き = to live` and `genba 現場 = the place` — instead of
stacking the gloss under the romaji/kanji. The whole colophon is four lines
(lede, two parts, pronunciation foot), not six.

This is a CSS-only change in `ui/static/app.css`: a new
`.name-origin-parts > div` rule makes each pair a baseline-aligned flex row, and
`.name-origin-parts dd` drops its leading top offset to `margin: 0`. The markup
in `ui/html/index.html` is **unchanged** — the `dt`/`dd` pairs stay (the gloss is
not folded into the `dt`), so R-HBWF-GM4D's existing assertions must continue to
pass untouched.

**Done when:**

- `internal/server/index_test.go` has a clearly-named test tagged
  `R-JNSL-OLCI` that fetches `/static/app.css` and asserts the
  `.name-origin-parts > div` rule exists with `display: flex` and
  `align-items: baseline`, and that the `.name-origin-parts dd` rule sets
  `margin: 0` and no longer carries `margin: var(--space-1) 0 0`.
- The existing R-HBWF-GM4D test still passes with no edits to its assertions.
- `go build ./...` succeeds and `go test ./...` is green.
