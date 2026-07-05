# Phase 9 — Realign the shipped landing page to the true canonical layout

*Realizes design Decision 6 (landing page): the corrected canonical layout for
the existing content/fidelity ids `R-EVZ3-VXJZ`, `R-7NJI-UTHM`, `R-7ORF-8L8B`,
`R-7PZB-MCZ0`, `R-WYSR-NPL3`, `R-X00O-1HBS`, plus the new structural-contract id
`R-31CG-6FPW`. Depends on Phase 8 (the token-integrity and golden-render tests).*

## What gets built

Phases 7 and 8 gave the page the canonical *content* and pinned it with a
token-integrity check and a byte-for-byte golden fixture — and all those tests
pass. But the shipped `internal/web/landing.html` `<style>` and markup are not the
canonical suite layout at all: they are a github-only variant, and Phase 8
regenerated the golden fixture *from that variant*, so the golden test pins the
drift instead of catching it. The visible result is a page that renders unlike
every other service: a blue eyebrow (`.eyebrow { color: var(--color-accent) }`
instead of muted), a `Home` link pinned to the viewport corner
(`position: fixed` instead of `absolute` within the centred `main`), renamed
`.detail`/`.description` classes, and a `<p class="eyebrow">` where the suite uses
a `<section>`-wrapped `<div class="eyebrow">`. This phase replaces that template
with the actual canonical layout, regenerates the golden from the corrected
template, and adds an independent structural-contract test so a golden regenerated
from a non-canonical template can no longer hide the drift.

`internal/web/landing.html` — replace the `<style>` block and the `<main>` markup
with the **canonical suite landing layout** (the current
`crm`/`gmail`/`ledger` `internal/web/landing.html` share this identical structure
and `<style>`; use it verbatim), carrying github's own copy: `<title>{{.Service}}
· github</title>`; the `Home` link (`<a class="home" href="/">Home</a>`) as the
first element inside `<main>`, styled `position: absolute; left: 0` in
`var(--font-mono)` and muted; a `<section aria-labelledby="page-title">` wrapping
`<div class="eyebrow">GitHub connector</div>`, `<h1 id="page-title">{{.Service}}</h1>`,
and `<p>Github connects the suite to GitHub through one shared GitHub App and
exposes repository, pull request, and issue actions as MCP tools.</p>`; and a
`<dl aria-label="Service details">` Service·Version·API grid whose API cell is
`<code>POST /mcp</code>`. The eyebrow, version, and `dt` share the muted
`var(--color-text-muted)` rule (no `var(--color-accent)`); spacing uses the
`var(--space-*)` tokens; the body uses `var(--font-body)`. No `.detail`,
`.description`, `<p class="eyebrow">`, `position: fixed`, `var(--color-accent)`,
or `var(--font-sans)` survives anywhere in the page. `{{.Service}}`/`{{.Version}}`
stay injected through `html/template` (HTML-escaped).

`internal/web/testdata/landing.golden.html` — regenerate the golden fixture as the
exact HTML `LandingHandler` renders for the fixed `service`/`version` pair the
`R-X00O-1HBS` test uses, from the corrected template.

`internal/web/web_test.go` — add one clearly-named test tagged `R-31CG-6FPW`: the
rendered HTML **contains** each canonical marker (`<section aria-labelledby="page-title">`,
`<div class="eyebrow">`, `<dl aria-label="Service details">`, `dl > div` cells)
and **contains none** of the drift markers (`<p class="eyebrow">`, `class="detail"`,
`class="description"`, `color: var(--color-accent)`, `position: fixed`). Keep the
pre-existing D6 tests (`R-EVZ3-VXJZ`, `R-7NJI-UTHM`, `R-7ORF-8L8B`, `R-7PZB-MCZ0`,
`R-WYSR-NPL3`, `R-X00O-1HBS`, `R-EX70-9PAO`, `R-EYEW-NH1D`) green against the
corrected template and regenerated golden.

Left untouched because they are already canonical: `web.go`, `embed.go`, the
embedded font/CSS assets (`static/tokens.css` is already byte-identical to the
canonical), and `etc/nginx.conf` / `internal/web/nginx_test.go`.

Observable end state: a logged-in browser at `/srv/github/` sees a page visually
identical to `/srv/crm/` and the other services — the same muted eyebrow, the
`Home` link anchored top-left within the centred column, the same spacing, grid,
and fonts — not the blue-eyebrow, corner-pinned variant that merely carries the
right words.

## Done when

All hold on identical repo state, from `github/`:

- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0; `gofmt -l .`
  empty; `go vet ./...` clean.
- Clearly-named offline tests cover and pass for `R-31CG-6FPW` (rendered HTML
  contains every canonical marker and none of the drift markers) and the
  regenerated `R-X00O-1HBS` golden match, and the pre-existing `R-EVZ3-VXJZ`,
  `R-7NJI-UTHM`, `R-7ORF-8L8B`, `R-7PZB-MCZ0`, `R-WYSR-NPL3`, `R-EX70-9PAO`, and
  `R-EYEW-NH1D` tests remain green.
- The drift markers are gone from the shipped template, scoped away from the
  `project/` docs that quote them — all four of these return `0`:
  `grep -c 'class="detail"' github/internal/web/landing.html`,
  `grep -c 'class="description"' github/internal/web/landing.html`,
  `grep -c 'color: var(--color-accent)' github/internal/web/landing.html`,
  `grep -c 'position: fixed' github/internal/web/landing.html`.
- The canonical markers are present in the shipped template — all three of these
  return `1`:
  `grep -c '<section aria-labelledby="page-title">' github/internal/web/landing.html`,
  `grep -c '<div class="eyebrow">GitHub connector</div>' github/internal/web/landing.html`,
  `grep -c '<dl aria-label="Service details">' github/internal/web/landing.html`.
