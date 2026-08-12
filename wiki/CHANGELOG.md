# Changelog

## v0.33.0 — 2026-08-11

- Ingest is now restart-proof: extract and compile work is handed off to prompts' durable completion queue and applied from its inbox, with jobs resting in a new `waiting` state — no in-flight work is lost to a restart, and long generations no longer die at the 15 s write deadline (the cause of the production ingest failures).
- Ask runs on the live queue path under its own queue partition, so background ingest processing can never discard an in-progress answer; ask-serving routes clear the chassis write deadline.
- The prompts wire contract is proven in the default test gate against the real prompts binary, not a mock.

## v0.32.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.

## v0.31.0 — 2026-08-11

- Deleting a scope now takes effect immediately and completely: search and ask reflect the deletion at once with no restart, and recreating the same scope name starts a genuinely empty scope — the deleted generation's content can never resurface, even from ingests that were still running at the delete. This fixes asks failing with a bare "ask wiki" page after a scope was deleted and re-ingested.
- Re-ingested and re-embedded pages are now searchable by meaning in their own scope right away, and never bleed into another scope's answers (a runtime mislabeling hid them from their scope and leaked them into `default`).
- Ask no longer fails when its search index is momentarily ahead of a deletion: a hit that no longer resolves is skipped and the answer comes from the pages that exist — or an honest "nothing here" when none do.
- Exact-name questions pin their subject again, and only within the scope being asked: the pin previously looked names up in the wrong scope and built hits that could never resolve, so any ask that pinned failed outright.
- When answering genuinely fails, the web now shows a styled error page with the question kept in the box ready to retry — never a bare one-line error body — and logs the underlying cause for diagnosis.
- No embedding row can outlive its subject anymore: writes are guarded, startup tolerates a stray orphan instead of refusing to boot, and the background sweep cleans any up on its own.

## v0.30.0 — 2026-08-10

- Asking the same question twice in a scope now answers instantly the second time: answers are cached in memory (the 500 most recently used, tunable via `ASK_CACHE_CAP`; `0` disables), and identical questions arriving at the same moment share one computation instead of running twice.
- Cached answers never go stale against the wiki's content: any change to a scope's knowledge — new ingests, merges, changed scope instructions, or scope deletion — drops that scope's cached answers so the next ask reflects the current pages.
- The cache is invisible apart from speed: MCP and web asks share it, a cached answer renders identically to a fresh one, and a restart starts empty.

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
