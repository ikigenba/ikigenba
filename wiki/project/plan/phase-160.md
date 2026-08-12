# Phase 160 — The live path gets its own queue partition, and ErrGone names its id

*Realizes design Decisions 5 (the LLM seam — partition slice) and 4 (prose-aligned stray rule; no D4 ids move).*

Splits `internal/llm`'s single consumer identity in two: `Consumer = "service:wiki"` stays the ingest pipeline's partition (handoff `Ensure`, `Inbox`, `Ack`), and the new `ConsumerAsk = "service:wiki.ask"` becomes the live path's — `Do` submits every item under it, so the D4 applier's 2 s inbox drain can never ack an item a live ask is still polling (the second 2026-08-11 seam defect: every live ask slower than one tick died `completion gone while polling`). `Do`'s ErrGone failure message carries the id it was polling — the current code zeroes `item` on the failed `Get` before formatting `item.ID`, printing an empty id.

No prompts-side change is involved: the consumer grammar `^service:[a-z0-9._-]+$` already admits the dotted name. No migration; no change to key recipes, envelopes, or the applier's logic (its stray arm becomes pure defense, as D4 now states).

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim:
  - R-UCLK-JDHN — `Do`'s raw Ensure JSON carries `consumer` = `service:wiki.ask`; handoff Ensure carries `service:wiki`; `Inbox` queries only `?consumer=service:wiki`
  - R-UDTG-X58C — `Do`'s mid-poll-404 error string contains the exact id being polled
- `go test ./...` from `wiki/` is green.
