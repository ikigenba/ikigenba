# Phase 51 — The remaining pages adopt the shell: metrics, profile, authorize, show-once

*Realizes design Decision 9 (the shell footer), Decision 10 (the shared shell — template slice), and Decision 16 (metrics entry via the nav) — ids R-VN4Y-ERZ1, R-VOCU-SJPQ, R-VQSN-K374, R-VT8G-BMOI, R-VUGC-PEF7. Depends on Phase 50.*

Every remaining page composes the Phase-50 shell partial:

- `ui/html/metrics.html` — the shell frame around a `Metrics` `<h2>` and the
  four charts (mechanism unchanged, D14/D15) housed in the D10 panels grid
  (`metrics_charts.tmpl` gains the panel containers).
- `ui/html/profile.html` — the shell frame; sections re-headed per D10's
  scale (`Profile` as `<h2>`; `Personal access tokens` and
  `Connected MCP clients` as `<h3>`); the live-dot indicator is removed
  (the SSE refresh wiring is untouched, D4); revoke controls take the
  bordered-neutral button idiom.
- `ui/html/oauth_authorize.html` — the session-less shell form: column, logo
  header without nav/identity controls, footer; body unchanged in function.
- `ui/html/partials/pat_created.tmpl` — the full shell (the owner is in
  hand), body unchanged in function.
- View models for these pages gain the shared version field the footer
  renders; `banner_chrome_test.go`'s replacement coverage lands here as the
  shell-header tests.

**Done when:**
- R-VOCU-SJPQ — the four signed-in pages render the identical shell header
  (logo served at `/static/logo.png`, the three nav links, sign-out, avatar
  last) — covered by a tagged test.
- R-VQSN-K374 — exactly one nav link carries `aria-current="page"` per page,
  matching the page — covered by a tagged test.
- R-VN4Y-ERZ1 — the four signed-in pages each render exactly one version
  footer and the logged-out `/` renders none — covered by a tagged test.
- R-VT8G-BMOI — the show-once page renders the full shell; the authorize
  chooser renders the session-less shell (no nav, no sign-out, no avatar) —
  covered by a tagged test.
- R-VUGC-PEF7 — the metrics charts render inside the four-panel grid —
  covered by a tagged test.
- The dashboard suite is green per design Conventions.
