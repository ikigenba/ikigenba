# Phase 136 — The new page shell and suggested-pages landing

*Realizes design Decisions 80 (page shell), 81 (suggested pages), 41 (Home
link, rewritten), 43 (home page, rewritten), and 78 (public tier, rewritten).*

Rebuild the web surface's frame and landing content to the operator-approved
design:

- **`share/www/layout.tmpl`** — the D80 shell: a boxed top-down column
  (`--layout-max-width` + `--layout-gutter`, hairlines inside the column, no
  vertical centering; footer pinned to the viewport bottom on short pages), a
  header bar as the first body element holding the Home link (mono uppercase
  label, `href` = the injected mount — D41) and the restyled compact scope
  selector (mono `Scope` label + `--control-h-sm` bordered select,
  change-submit inline script, submit button only inside `<noscript>`), and
  the name+version footer. The question form is restyled to a
  `--control-h-lg` input with a separate primary button. D79's labels,
  `data-busy` wiring, and busy CSS carry over.
- **`share/www/home.tmpl`** — the D43/D78 scope home: `<h1>` service title,
  the question form, and the D81 **Suggested pages** section (mono label
  heading + ruled rows: subject link left, type tag right), omitted whole for
  an empty scope. The public tier renders the same shape with its Search
  labels; the browse-all index and the orphan list are removed from the
  template.
- **`internal/web`** — the `OrphanLister`/`WithOrphanLister` seam becomes
  `RecentLister`/`WithRecentLister` with the D81 `SubjectRef{Href, Name, Type}`
  row; the home handlers (both tiers) call `Recent(ctx, scope, 7)`; the public
  home's browse path is deleted.
- **`internal/wiki`** — the orphan computation (`Orphans` and its query) is
  deleted; a `Recent` lister (scope-bounded `ORDER BY id DESC LIMIT n` over
  subjects) replaces it, wired through the adapter in `cmd/wiki/main.go`.
- **Tests** — new tagged tests for the ids below; the tests tagging the
  retired ids (`R-HOME-3U5Y`, `R-ONZU-Z1EX`, `R-OP7R-CT5M`, `R-QSR2-AFAD`,
  `R-QTYY-O712`, `R-QV6V-1YRR`, `R-QWER-FQIG`, `R-HUFU-ASVW`, `R-HZBF-TVUO`,
  `R-H169-4B38` — in `internal/web/web_test.go` and
  `internal/wiki/orphans_test.go`) are deleted or rewritten under the new ids;
  existing shell-adjacent assertions (selector listing R-HQS5-5HNT, select
  action R-HOCC-DY6F, D79 labels/busy, D52 render-from-share ids) are updated
  to the new markup where they pin the old frame, with their behaviors intact.

**Done when:**

- These Verification ids are covered by clearly-named tagged tests and the
  suite is green (`go build ./...`, `go vet ./...`, `gofmt -l .` silent,
  `go test ./...` from `wiki/`):
  - R-HG6J-SNRN — every page state wears the shell (header bar with Home +
    selector before content, footer after) — D80.
  - R-HHEG-6FIC — selector change-submit script + `<noscript>`-only submit
    button — D80.
  - R-HIMC-K791 — the Home link's href is exactly the injected mount on every
    page, both tiers — D41.
  - R-HJU8-XYZQ — seven newest subjects, newest first, typed and linked
    (real temp SQLite) — D81.
  - R-HL25-BQQF — the suggested list honors the scope wall (real temp
    SQLite) — D81.
  - R-HMA1-PIH4 — an empty scope omits the section whole — D81.
  - R-HOPU-H1YI — the public tier renders the same list and no browse-all
    index — D81.
  - R-HPXQ-UTP7 — the private tier keeps ask, scoped; the public tier never
    invokes it — D78.
- The retired ids are gone from the tree:
  `grep -rE 'R-HOME-3U5Y|R-ONZU-Z1EX|R-OP7R-CT5M|R-QSR2-AFAD|R-QTYY-O712|R-QV6V-1YRR|R-QWER-FQIG|R-HUFU-ASVW|R-HZBF-TVUO|R-H169-4B38' --include='*.go' .`
  (run from `wiki/`) prints nothing.
- `grep -rn 'Orphan' --include='*.go' internal/ cmd/` (run from `wiki/`)
  prints nothing.
