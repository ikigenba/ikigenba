# Phase 133 — Clean-prose compile: no ids in the rendered input, the prompt, or the returned body

*Realizes design Decision 7 (compile stage — clean article prose).*

The compile stage stops producing citation markers and internal vocabulary, at the source. End state:

- `internal/compile/prompt.txt` carries the rewritten instruction preamble: no citation tagging anywhere; clean flowing prose with no identifiers or bracketed markers; conflicting accounts keep both statements without ids; the lead states what the subject is in natural category terms and never uses `entity`/`concept`/`event` or other database vocabulary as the subject's category.
- The user-message renderer sends the subject identity plus **plainly rendered claim texts** — no job-id tags, no ULIDs anywhere in the user turn. The tighten corrective note says "keep the lead" (no citation clause).
- `Compile` runs the **id sanitizer** on every parsed candidate before the cap is measured: every `[` + 26-char Crockford-base32 ULID + `]` marker is stripped from the body with whitespace tidied; non-ULID bracketed text is untouched.

**Done when:**

- R-VA32-HERT — a scripted model reply whose body embeds bracketed ULID markers mid-sentence yields a returned body with zero bracketed-ULID matches, intact surrounding prose with tidied whitespace, and an untouched non-ULID bracket (e.g. `[sic]`) — covered by a named test.
- R-9YHM-4QN3 — the captured `/complete` compile request carries `system` = the embedded `prompt.txt` and a user message of only the rendered identity + plain claim texts (plus tighten note on retry), with **no ULID anywhere in the user message** — its existing test updated to assert the new rendering.
- The suite is green per design Conventions (`go test ./...` from the wiki module root).
