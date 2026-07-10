# Phase 86 — Inline links in the MCP `ask` answer and `page` body (`internal/mcp`)

*Realizes design Decision 10 (`ask`/`page` result inline links) and Decision 58 (consumes `LinkifyMentions`). Depends on Phase 85 (`Service.LinkifyMentions`) and Phase 82/84 (the MCP `pageBase`, `AuthServer + Mount + "subject/"`).*

Wire the D58 linkifier into the two prose-bearing MCP results, using the `pageBase` `NewHandler` already derives (Phase 84):

- **`ask`** — `handleAskCall` runs `Service.LinkifyMentions(ctx, answer.Text, pageBase, "")` and returns the linkified markdown as `answer`; the `citations` array (D56) is untouched.
- **`page`** — the handler linkifies the prose with `LinkifyMentions(ctx, body, pageBase, subjectID)` (own subject excluded) **then** appends the D12 footer: `RenderFooter(linkified, mentions, mentionedBy)` — footer never re-linkified. Result shape stays `{subject, title, body}`; no internal id introduced.

The result envelopes, tool names, schemas, and not-found paths are unchanged.

**Done when** the suite is green (`go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`, `bin/check-migrations wiki`) and each id below is covered:

- R-8DB1-UI1Q — the assembled `ask` result's `answer` carries an inline front-door link `[Acme Corp](<pageBase>entity/acme-corp)` at the first mention, with the `citations` array still present — driven against a real `Service`/`Asker` over a real temp SQLite + mock provider.
- R-8EIY-89SF — the `page` result `body` inline-links the first occurrence of a *referenced* subject as a front-door link, does **not** inline-link the page's own subject (self-exclusion), and still carries the D12 `Mentions`/`Mentioned by` footer after the prose.

(R-03GW-PX5K's `{subject,title,body}` shape and no-internal-id guarantee, and R-6A8D-0RK9's `ask` contract, remain green — this phase adds inline links inside `body`/`answer` without changing the envelope.)
