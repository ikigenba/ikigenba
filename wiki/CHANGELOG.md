# Changelog

## v0.29.0 — 2026-08-10

- Each scope can now carry standing instructions ("this scope holds a fictional world; its dates are in-world") via a new `instructions` get/set tool; they are honored by every ingest, page rebuild, and question in that scope, overriding the wiki's general reading habits where they conflict.
- The wiki now respects a document's own timeline: a story's "130 years ago" stays relative instead of being converted into a real-world date computed from the ingest day; only material speaking from the real-world present (notes, correspondence, news) gets relative dates resolved against when it was received.
- Instruction changes apply to future work only; re-run earlier ingests to re-read them under new instructions.

## v0.28.0 — 2026-08-10

- Ask now finds pages phrased differently than the question: the search index stems words, so asking about "kings" matches a page that says "king".
- Keyword search no longer treats the word "or" as a search term of its own, which was letting unrelated pages crowd out relevant ones.
- Ask reads as many relevant pages as fit its reading budget instead of stopping at a fixed eight, so list-style questions ("who are all the kings?") draw on more of the wiki.

## v0.27.0 — 2026-08-10

- The answer page's footer now matches the subject pages: the cited pages and the mentioned subjects sit side by side as quiet labeled columns beneath the answer, in the same two-column layout subject pages use.

## v0.26.0 — 2026-08-10

- Public scopes now answer questions: the question box on a public scope runs the same cited ask as the signed-in surface, for anyone on the internet — the keyword-only public search page is gone.
- The question button reads Ask everywhere; the public tier's Search label is retired.
- Landing on the wiki and switching scopes now arrive at a public scope's public address and a private scope's signed-in address, so the address bar always holds the scope's shareable form.
- All chat inference (extract, compile, both ask stages) now routes through OpenRouter at medium reasoning effort, same gpt-5.6-luna model; embeddings stay on direct OpenAI.

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
