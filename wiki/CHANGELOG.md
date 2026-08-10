# Changelog

## v0.25.0 — 2026-08-10

- The header on every page now opens with the ikigenba brand mark at the far left; following it lands on the dashboard's landing page, while Home still returns to the wiki's own landing.
- The Ask/Search box now lives in the header on every page of both tiers — the large scope-home form and the answer page's "ask another question" link are gone, and on a results page the box keeps the question you just asked, ready to edit and resubmit.
- The scope home body is now just the title and Suggested pages; asking always starts from the header.

## v0.24.0 — 2026-08-10

- Subject pages carry the question box in the header, right beside the scope selector, so the next question starts from any page — the bottom "ask another question" link is gone.
- Mentions and Mentioned by are now quiet side-by-side columns below the prose: small muted headings and links divided by a hairline rule, subordinate to the article instead of competing with it.
- Subject-page content spans the same page width as the header and footer, and the scope home's Ask bar is centered on the page.

## v0.23.0 — 2026-08-10

- Every wiki page now wears a shared shell: a boxed, top-down layout with a header bar carrying a Home control (back to the wiki's own landing) and a compact scope selector that switches scope the moment you pick one.
- Scope home pages show **Suggested pages** — the scope's seven most recently added pages, newest first — on both the signed-in and public surfaces, replacing the old orphan-only and browse-all lists (everything stays findable by search).
- The question form is restyled to match the suite's design language: a proper primary Ask/Search button beside the input instead of a fused strip.

## v0.22.0 — 2026-08-10

- Compiled pages are clean article prose: no inline job-id citation markers, no internal ids, and leads that describe the subject in plain language instead of database terms; a sanitizer guarantees no bracketed id survives.
- The web question button now says what it does — **Ask** on the signed-in surface, **Search** on public scopes — and submitting dims the page under a spinner with the button reporting progress until the result arrives.
- The autotune compile folder is trued to the clean-prose contract (gates flag leaked ids instead of checking citations; judge rubric scores natural-language leads).

## v0.21.1 — 2026-08-09

- baseline; changes before this version are recorded only in git history
