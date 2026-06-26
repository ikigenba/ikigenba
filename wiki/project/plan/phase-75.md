# Phase 75 — Render web prose as styled, sanitized markdown

*Realizes design Decision 44 (the ask answer's prose-rendering slice), Decision 45
(the subject page's prose-rendering slice), Decision 49 (token-based CSS for the
markdown element set), and Decision 48's web-wiring (the `md` template func).
Touches only `internal/web` — the template func registration, `home.tmpl`,
`subject.tmpl`, and `layout.tmpl` — no `cmd/wiki/main.go` change, no migration, no
LLM, no DB. Depends on Phase 74 (`markdown.Render`), Phase 70 (the ask result page
in `home.tmpl`), and Phase 71 (the subject page in `subject.tmpl`).*

The ask answer and subject prose currently render escaped inside `<p>{{.X}}</p>`,
so the LLM's markdown shows as literal `##`/`**`/`|`. This phase renders both
through the Phase 74 `md` func as sanitized HTML, styles the resulting element set
with the shared design tokens, and retires the now-obsolete escape guarantee
(D48's sanitizer supersedes it).

In **`internal/web`**:
- Register the renderer as a template func: build `homeTemplates` and
  `subjectTemplates` with `.Funcs(template.FuncMap{"md": markdown.Render})` before
  `ParseFS`.
- In **`home.tmpl`** (the `{{if .Asked}}` answer branch): replace
  `<p>{{.Answer.Text}}</p>` with `<div class="prose">{{ md .Answer.Text }}</div>`.
- In **`subject.tmpl`**: replace `<p>{{.Subject.Body}}</p>` with
  `<div class="prose">{{ md .Subject.Body }}</div>` (the not-found body string
  flows through `md` too and round-trips to a paragraph).
- In **`layout.tmpl`**'s `<style>`: add `.prose`-scoped rules covering the
  markdown element set — headings, `ul`/`ol`/`li`, `code`/`pre`, `blockquote`, and
  GFM `table`/`th`/`td` (plus `strong`/`em`/`a` as needed) — every value drawn from
  the existing design tokens (`var(--color-…)`, `var(--space-…)`, `var(--text-…)`,
  `var(--font-mono)`, `var(--border-width)`); introduce no new color/size/font
  literal and no second stylesheet. **Drop** the now-obsolete
  `p { white-space: pre-wrap }` rule (it existed only for the prior raw-text body).
- **Retire** the Phase 71 escape test for **R-PN5X-12AS** (the body is no longer
  HTML-escaped; its injection guarantee now lives in D48 / Phase 74's
  R-T0JU-ILWB and R-T1RQ-WDN0).

**Done when:** the suite is green (per design *Conventions*), these ids are
covered by clearly-named tests, and the retirement check passes:

- **R-NPVU-26CX** — `GET /?q=…` with a stub `Asker` returning
  `Answer{Found:true, Text:"**Acme** makes widgets."}` produces a page whose body
  contains `<strong>Acme</strong>` — the answer text is rendered through `md` (a
  wrong impl emitting literal `**Acme**` fails this). *(httptest, stub Asker)*
- **R-NONX-OEM8** — `GET /subject/entity/acme-corp` with a stub `PageFinder`
  returning `SubjectView{Body:"## Overview\n\nAcme makes **widgets**."}` produces a
  page whose body contains an `<h2` element and `<strong>widgets</strong>` — the
  prose is rendered through `md`, not escaped. *(httptest, stub PageFinder)*
- **R-9EPS-LWWY** — both the ask answer page (`GET /?q=…`, stub `Asker`) and a
  subject page (`GET /subject/…`, stub `PageFinder`) emit the rendered body inside
  a `class="prose"` element — the scoped styling hook is present on both surfaces.
  *(httptest, stubs)*
- **R-9FXO-ZONN** — a served page's inline `<style>` contains `.prose`-scoped
  rules covering headings, lists (`ul`/`ol`/`li`), `code`/`pre`, `blockquote`, and
  `table`, and those `.prose` rules reference `var(--` design tokens — proving the
  markdown elements are styled and reuse the shared token system. *(httptest:
  assert the served `<style>` contains the `.prose`-scoped selectors for each
  element group and at least one `var(--…)` reference within them)*
- **R-PN5X-12AS is retired:** `grep -rn 'R-PN5X-12AS' wiki/internal/` returns
  nothing — the escape test is removed (its guarantee is now Phase 74's
  R-T0JU-ILWB / R-T1RQ-WDN0). *(scoped to `wiki/internal/` so the `project/` plan
  history that names the id is excluded; the check is reachable.)*
