# Phase 82 — The queue's wire contract and its visible depth

*Realizes design Decision 29 (the completion queue) — the HTTP slice: error codes, the key cap, the pinned inbox shape — and design Decision 63 (queue observability) in full. Depends on Phase 80.*

`internal/completion/http.go` and the composition root in `cmd/prompts` finish the contract.

Every error response carries a stable machine-readable `code` beside its human-readable `error`, drawn verbatim from the product's code vocabulary: a `4xx` code means the submission is permanently wrong, a `5xx` code means the environment is temporarily unable. prompts performs no internal retry of a transient condition — it labels it and answers. The `key` cap rises from 256 to 1024 bytes. The inbox response is pinned as a bare JSON array, emitting `[]` rather than `null` for an empty partition. Get and Ack take the consumer whose partition they address, matching Phase 80's store scoping.

The queue store gains one read-only snapshot method, and the composition root wires it into the chassis health payload as a `completions` object carrying the status counts, the oldest queued item's age, and the count of items that have been reclaimed. Every field is a derived query — no counter column, no metrics table, no in-process accumulator.

**Done when:** every id below is covered by a clearly-named test — the HTTP ids through `net/http/httptest` over real temp SQLite, and both D63 ids through the composed boot smoke in `cmd/prompts/main_test.go`, which builds the real binary and serves on a loopback port rather than constructing the handler; `go test ./...` from `prompts/` passes; `gofmt -l .` emits no output; and the design-only coverage difference is empty.

- R-ZVRL-2K5W — a 1024-byte key is accepted with 202 and a 1025-byte key is rejected with 400 naming the limit, storing no row
- R-ZWZH-GBWL — the inbox body decodes into a JSON array and fails to decode into an object, both when populated and when empty
- R-ZY7D-U3NA — a rejected Ensure carries a stable `code` beside `error` on a 4xx, identical across two submissions of the same malformed body
- R-ZZFA-7VDZ — a store failure answers 5xx with its own code, distinct from every 4xx rejection code, with no internal retry before answering
- R-07YK-W9KU — the assembled binary's health response carries the `completions` counts and the oldest queued item's age, tracking a seeded mix of items
- R-15VT-PVYT — `reclaimed_items` is 0 for a queue whose items were only claimed and released, becomes non-zero after a lease is reclaimed, and is recomputed from the rows after a restart rather than persisted
