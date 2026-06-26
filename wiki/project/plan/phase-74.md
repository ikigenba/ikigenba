# Phase 74 — The markdown → sanitized-HTML renderer (`internal/markdown`)

*Realizes design Decision 48 (the markdown rendering seam). New standalone pure
package `internal/markdown`; adds the `github.com/yuin/goldmark` and
`github.com/microcosm-cc/bluemonday` dependencies to `go.mod`/`go.sum`. No web
change, no migration, no LLM, no DB. Depends on no earlier phase — it is a
self-contained pure package built ahead of its Phase 75 consumer.*

The read surface needs compiled-page and answer prose rendered as **formatted,
sanitized HTML** instead of escaped raw text. This phase builds the one function
that does that transform, behind a package boundary, with nothing wired to it
yet.

In **`internal/markdown`**: a single exported
`func Render(prose string) template.HTML`. It parses `prose` with **goldmark**
configured with the **GFM extension** (`extension.GFM` — for tables) and
**without** `goldmark.WithUnsafe()` (so raw HTML is escaped by default), renders
to HTML, then passes that HTML through **bluemonday** (`UGCPolicy()`) to enforce
the element allowlist and a safe URL-scheme allowlist (http/https/mailto). The
goldmark renderer and bluemonday policy are built once as package-level
singletons and reused (both are safe for concurrent reuse). The result is
returned as `template.HTML` so a template emits it without re-escaping. Pure
function: no IO, no DB, no identity.

Both libraries are pure-Go (no cgo, consistent with `modernc.org/sqlite`) and
proxy-fetched with **no** committed `replace` (like `agentkit`); add them to
`go.mod` and commit the updated `go.sum`.

**Done when:** the suite is green (per design *Conventions* — including the two
new modules resolving so `go build ./...` and `go test ./...` succeed) and these
ids are covered by clearly-named tests that call `markdown.Render` directly
against the **real** goldmark + bluemonday (no mock, no network — the real
libraries are the falsifying substrate):

- **R-SS0J-U7PG** — `Render("## Acme")` output contains an `<h2` element wrapping
  `Acme` and does **not** contain the literal `## Acme`.
- **R-ST8G-7ZG5** — `Render("**bold** and *italic*")` contains
  `<strong>bold</strong>` and `<em>italic</em>`, with no literal `**`/`*`.
- **R-SUGC-LR6U** — a `-` bulleted block renders `<ul>`…`<li>` and a `1.` block
  renders `<ol>`…`<li>`, not literal `-`/`1.` lines.
- **R-SVO8-ZIXJ** — inline `` `x` `` renders a `<code>` element; a triple-backtick
  fenced block renders `<pre>` wrapping `<code>` — not literal backticks.
- **R-SWW5-DAO8** — `Render("> quoted")` renders a `<blockquote>` containing
  `quoted`, not a literal `>`.
- **R-SY41-R2EX** — a GFM pipe table (`| A | B |\n| - | - |\n| 1 | 2 |`) renders a
  `<table>` with `<th>`/`<td>` cells (`A`,`B`,`1`,`2`) and contains no literal `|`
  row text — proving the GFM table extension is enabled (a base-CommonMark
  renderer would leave the pipes).
- **R-SZBY-4U5M** — `Render("[Acme](https://x.test)")` renders `<a` with
  `href="https://x.test"` wrapping `Acme`.
- **R-T0JU-ILWB** — `Render` of prose containing `<script>alert(1)</script>`
  returns output with **no** `<script` substring and no executable element — the
  raw tag does not survive.
- **R-T1RQ-WDN0** — `Render("[x](javascript:alert(1))")` returns output whose any
  emitted `<a>` carries **no** `href` beginning `javascript:` — the dangerous
  scheme is removed by the URL allowlist (a wrong impl using goldmark alone would
  emit `href="javascript:alert(1)"` and fail this).
- **R-T2ZN-A5DP** — `Render("Just a sentence.")` yields `<p>Just a sentence.</p>`
  (text intact, wrapped in a paragraph), proving unmarked prose renders cleanly.
