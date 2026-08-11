# Phase 159 — Ask on the live queue path, and the write-deadline clearing

*Realizes design Decision 92 (ask-serving routes clear the chassis write deadline); completes the D9/D36 ask path over the D5 live client. Depends on Phase 157.*

Moves ask's two structured generations onto `llm.JSON`'s live queue path (ensure + poll + ack; per-ask keys from the chain id) and adds the D92 deadline clearing: the web ask branch clears the response write deadline before invoking the asker, and `cmd/wiki` wraps its MCP mount in the one-line clearing middleware. The existing ask ids (D9, D36, D56, D78, D82, D83, D86, D89) keep their tests, adapted to the queue-playing httptest server.

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim: R-KJAJ-CTB1, R-KKIF-QL1Q.
- R-A0XE-WA4H (D36) and R-9ZPI-IIDS (D9) remain tagged by green tests capturing the ensured item's payload (system/user split) through the queue-playing httptest prompts.
- `go test ./...` from `wiki/` is green.
