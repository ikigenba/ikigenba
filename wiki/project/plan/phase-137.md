# Phase 137 — Subject-page restyle and centered scope-home ask bar

*Realizes design Decision 45 (subject page — header question form, full-width
content, quiet two-column links) and 80 (shell — centered scope-home question
form, right-aligned header group).*

Rework the read surface's templates and layout CSS (`share/www/layout.tmpl`,
`share/www/subject.tmpl`) to the approved mockup:

- The scope home's question form is horizontally centered within the boxed
  column (D80). Nothing else on the home moves.
- Subject pages (found and not-found) carry the tier's question form in the
  header, immediately after the scope-selector form, as a compact right-aligned
  group (selector `margin-left: auto`; form `--control-h-sm` controls, fixed
  360px width, `margin-inline: 0`); the form's `q` input and tier-labeled
  submit act on the base `.`. The bottom "Ask another question" link is gone
  from the subject template.
- Subject content (title, prose, links) spans the full boxed column — the
  narrower content caps are removed.
- The Mentions / Mentioned by sections render inside one `class="links"`
  wrapper: two equal columns under a top hairline with `--space-8` top margin,
  a hairline vertical rule between them (stacking with a horizontal rule below
  640px), headings as small mono uppercase muted labels at `font-weight: 700`,
  lists in small muted type darkening on hover. Omission rules unchanged
  (empty side omitted; wrapper omitted when both sides are empty).

Existing tests tagged R-PLY0-NAK3 (styled 404 — its affordance is now the
header question form) and R-8JEJ-RCR7 (relative navigation — the subject-page
half now checks the header form's action) assert the old "ask another
question" control on subject pages and must be updated to the rewritten
behaviors in D45/D59. The test tagged R-PKQ4-9ITE is deleted with its retired
behavior.

**Done when:**

- R-Y42U-45K2 — header carries the tier question form after the selector on
  found and 404 subject pages, action resolving to the base, and no "ask
  another question" anchor renders — covered by a named test.
- R-Y5AQ-HXAR — both link sections render inside a single `class="links"`
  wrapper (Mentions before Mentioned by), absent when both sides are empty —
  covered by a named test.
- Tests tagged R-PLY0-NAK3 and R-8JEJ-RCR7 assert the rewritten behaviors and
  pass.
- `grep -r 'R-PKQ4-9ITE' --include='*_test.go' .` from `wiki/` returns
  nothing.
- `grep -ri 'ask another' share/www` from `wiki/` returns nothing.
- `go test ./...` from `wiki/` is green.
