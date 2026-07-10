# Phase 28 — Per-row copy-URL button: markup, replicated styling, and the controller's copy render + delegated handler

*Realizes design Decision 6 (the far-right copy control layout + the replicated
copy-button styling + the `R-NM1L-GSYE` Verification) and Decision 22 (the
controller renders each row's copy button and wires the delegated
copy-to-clipboard side effect). Depends on Phase 25 (the live controller and its
`<tbody>` rebuild this phase extends). The copy behaviour's real-browser proof
is Phase 29.*

Each landing row gains a far-right **copy-URL button** that replicates the
suite's copy-button pattern (captured in `project/research/research.md`; sites
owns its own copy — no `dashboard/` dependency). Structurally the button is a
`.copy-btn` holding an inline copy-icon SVG and a `.copy-label` "Copy", carrying
the row's absolute front-door URL, rendered **hidden-until-JS** like the other
controls. The controller, which already rebuilds `<tbody>` on every action, also
renders the button per row and wires **one delegated** click handler that copies
the clicked button's URL and flips the label to "Copied". HTML + CSS + `landing.js`
only; no handler, store, migration, MCP, or nginx change. This phase proves the
button's **structure** (Phase 29 proves the clipboard actually receives the URL).

- **`sites/share/www/landing.html`** — add a far-right action column: an
  unlabelled header (`sr-only` "Copy", not sortable) and, in each server-rendered
  row, a hidden-until-JS `.copy-btn` (copy-icon SVG + `.copy-label` "Copy")
  carrying that row's absolute front-door URL in a data attribute (the same value
  as the row's slug-anchor `href`). Add the `.copy-btn` / `.copy-label` /
  `.is-copied` rules to the page `<style>`, built from sites' own tokens.
- **`sites/share/www/static/landing.js`** — in the controller's `<tbody>`
  rebuild, render each row's copy button (visible, carrying the row's `url`);
  wire **one delegated** `click` listener on the table body for `.copy-btn` that
  reads the clicked button's URL, writes it to the clipboard
  (`navigator.clipboard.writeText` in a secure context, else the hidden-`<textarea>`
  + `execCommand` fallback), and flips the button's label to "Copied" + toggles
  `.is-copied` for ~1.6s. This is a pure DOM side effect — it does **not** go
  through `reduce`/`computeView`. The pure functions and their goja tests are
  untouched.

**Done when:** the sites suite is green (`cd sites && go build ./...`, `go vet
./...`, `gofmt -l .` prints nothing, `go test ./...`), AND `R-NM1L-GSYE` is
covered by a genuine test driving the `GET /{$}` handler (fixed `baseURL`) over a
seeded store, rendered on the repo-real `share/www`:
- R-NM1L-GSYE — with a store seeded with a **public** site `X` and a **private**
  site `Y`, each rendered row contains, as its **far-right** cell, a copy button
  that (a) carries `hidden` in the server render, (b) exposes that row's absolute
  front-door URL (`<baseURL>public/X/` for `X`, `<baseURL>private/Y/` for `Y`,
  byte-identical to the row's slug-anchor `href`), and (c) carries a styling-hook
  class that resolves to a rule in the page's stylesheet. A row with no copy
  button, a visible-without-JS one, the wrong/no URL, or a rule-less class fails.
