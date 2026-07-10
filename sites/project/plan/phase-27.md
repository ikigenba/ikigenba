# Phase 27 — Style the landing controls with the Carbon components (not native chrome)

*Realizes design Decision 6 (the reworded styling paragraph + the `R-NKTP-317P`
control-styling Verification). Depends on Phase 25 (the search / Clear / pager
controls whose markup this phase styles).*

The control layer built in Phases 24–25 ships the search input and the Clear /
Prev / Next buttons as **native, class-less** HTML elements, so they render as
browser default chrome against the Carbon page (the v0.13.0 look). This phase
makes them read as first-class Carbon controls by giving them the shared
component styling, built entirely from the token custom properties already in
`tokens.css` — no new token values, and the `.site-table`'s own CSS untouched.
HTML + CSS only; no handler, view model, controller logic, store, migration,
MCP, or nginx change.

- **`sites/share/www/landing.html`** — the search input, the Clear button, and
  the Prev/Next pager buttons each carry a component styling class (the Carbon
  `.input` / `.btn` family), and the page's `<style>` gains the matching
  component rules (built from the existing `--control-h-*`, `--color-accent`,
  `--color-border-strong`, `--radius`, … tokens). Which `.btn` variant each
  button takes is an implementation choice, not a contract. The content
  elements' CSS and the table's styling are left exactly as they are.

**Done when:** the sites suite is green (`cd sites && go build ./...`, `go vet
./...`, `gofmt -l .` prints nothing, `go test ./...`), AND `R-NKTP-317P` is
covered by a genuine render test over the repo-real `share/www` (via
`appkit/web`):
- R-NKTP-317P — rendering `landing.html`, each of the search input, the Clear
  button, and the Prev and Next pager buttons carries a **non-empty `class`**,
  and **every class it names resolves to a rule present in the page's
  stylesheet**. A native, class-less control fails; a class with no backing rule
  fails. (Class-agnostic — it pins that the controls are styled, not which
  component/variant classes are used.)
