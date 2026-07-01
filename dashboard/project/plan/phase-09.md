# Phase 9 — Tighten the name-origin colophon copy and add a pronunciation guide

*Realizes design Decision 7 (`R-DB17-ORIG` reworded + the new `R-O7K1-XEN7`). One
build phase covering both the colophon copy edit and the pronunciation guide, since
they touch the same `name-origin` block. Touches only `dashboard/ui/html/index.html`
(the `name-origin` colophon in the `{{else}}`/logged-out branch),
`dashboard/ui/static/app.css` (one appended rule for the pronunciation line), and
the pinned assertions in `dashboard/internal/server/index_test.go`. No
`internal/server` logic change, no route, no migration, no view-model, no schema.
Independent of all other work.*

Two coupled copy/markup changes to the same `name-origin` `<aside>`:

**1. Tighten the existing copy.** The lede loses its "livelihood" gloss and both
part-glosses shorten. **2. Add a pronunciation guide** as the quiet foot of the
block, below the two parts.

In **`ui/html/index.html`**, the `name-origin` `<aside>` becomes exactly:

```html
<aside class="name-origin" aria-label="What ikigenba means">
  <p class="name-origin-lede"><b>ikigenba</b> — A portmanteau of two Japanese words:</p>
  <dl class="name-origin-parts">
    <div>
      <dt><b class="seam">iki</b>gai <span lang="ja">生き甲斐</span></dt>
      <dd>&ldquo;reason for being&rdquo;; work worth doing.</dd>
    </div>
    <div>
      <dt><b class="seam">genba</b> <span lang="ja">現場</span></dt>
      <dd>the actual place; where the work happens.</dd>
    </div>
  </dl>
  <p class="name-origin-say">pronounced <b>EE-kee-GEN-buh</b></p>
</aside>
```

Copy notes: the lede drops "where your livelihood actually gets done"; the ikigai
gloss keeps quotes on "reason for being" but drops "the work worth doing; your
business"; the genba gloss drops the quotes around "the actual place" and shortens
to "the actual place; where the work happens." The `dt` markup, `seam` bolds,
Japanese spans, `aria-label`, and placement below the CTA are otherwise unchanged.
The new `<p class="name-origin-say">` is added **after** the `</dl>`, last inside the
aside, and holds the phonetic string `EE-kee-GEN-buh`.

In **`ui/static/app.css`**, append the pronunciation rule to the existing
name-origin colophon block (semantic tokens only, no hard-coded values), matching
D7's Styling section:

```css
.name-origin .name-origin-say {
  max-width: none;
  margin: var(--space-3) 0 0;
  font-size: var(--text-small-size);
  line-height: var(--text-small-lh);
  color: var(--color-text-subtle);
}
.name-origin .name-origin-say b {
  color: var(--color-text-muted); font-weight: 600; letter-spacing: .04em;
}
```

In **`dashboard/internal/server/index_test.go`**, update the pinned assertions:
- Replace the old lede and the two old `<dd>` exact strings (the ones asserting
  `where your livelihood actually gets done`,
  `&ldquo;reason for being&rdquo; — the work worth doing; your business.`, and
  `&ldquo;the actual place&rdquo; — the floor where the work really happens.`) with
  the new strings above.
- The `aside` now holds **two** paragraphs, so the assertion that counts `<p` in the
  aside must change from `== 1` to `== 2`; keep the `name-origin-lede` count at
  exactly 1 and **add** an assertion that `name-origin-say` count is exactly 1 and
  contains `EE-kee-GEN-buh`. Leave the `dt`/`dd`/`seam`/Japanese-span counts (2 each)
  and the "after the CTA / last in the wall" ordering checks exactly as they are —
  the two-part structure did not change.

**Done when:** the suite is green — `cd dashboard && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), `go test ./...`, and
`bin/check-migrations dashboard` all succeed with zero failures (per design
*Conventions*) — and these ids are covered:

- **R-DB17-ORIG** — the logged-out `GET /` renders the colophon as a name with two
  subcomponents, not three peers: a single `name-origin-lede` naming `ikigenba` and
  calling it a portmanteau of two Japanese words (and **not** containing the old
  `where your livelihood actually gets done` phrase), plus a `name-origin-parts` list
  of **exactly two** items — `ikigai` (生き甲斐) and `genba` (現場), each `seam`-marked
  with its one-clause gloss (`work worth doing.` / `where the work happens.`). Exactly
  two `dt`/`dd` pairs; no third peer gloss. *(httptest via `testServer`/`do`,
  logged-out `GET /`)*
- **R-O7K1-XEN7** — the logged-out `GET /` renders a single
  `<p class="name-origin-say">` after the parts list and last inside the
  `name-origin` aside, containing `EE-kee-GEN-buh`; the aside carries exactly two
  paragraphs (lede + say-line); and the say-line is **absent** from the signed-in
  landing. *(httptest via `testServer`/`do`; logged-out and logged-in `GET /`)*
