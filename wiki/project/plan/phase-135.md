# Phase 135 — Truthful submit controls: Ask/Search labels + busy feedback on the question form

*Realizes design Decision 79 (truthful submit controls).*

The web question form tells the truth about what it does and shows it working. End state, in `share/www/` (D52) and the web handler's template data:

- The private tier's question form renders button text `Ask` (`data-busy-label="Asking…"`, input `aria-label="Ask this wiki"`); the public tier's renders `Search` (`data-busy-label="Searching…"`, input `aria-label="Search this wiki"`).
- The question form carries `data-busy`; the layout ships the inline busy script (on submit: disable the button, swap in its `data-busy-label`, add `.busy` to `<body>`) and the `.busy` CSS that dims `main` under a spinner. Plain GET navigation is untouched (no `preventDefault`, no fetch); the scope selector form carries no `data-busy`.

**Done when:**

- R-VBAY-V6II — private-tier home and ask-result markup carry the `Ask` button, busy label, and aria-label — covered by a named httptest.
- R-VCIV-8Y97 — public-tier home and results markup carry the `Search` button, busy label, and aria-label — covered by a named httptest.
- R-VDQR-MPZW — every question-form page ships `data-busy` on the form, the inline busy script, and the `.busy` spinner/dim CSS hooks, and the selector form carries no `data-busy` — covered by a named httptest.
- The suite is green per design Conventions (`go test ./...` from the wiki module root).
