# Phase 78 — The completion-queue HTTP surface, and the synchronous endpoint expunged

*Realizes design Decision 29 (the completion queue) — the HTTP slice. Depends on Phase 77.*

Mounts the four queue verbs over `internal/completion` through the chassis loopback guard in `cmd/prompts`: `POST /completions` (validate via `prompt.ValidateConfig` + envelope checks, ensure-idempotent, `202`/`200`), `GET /completions/{id}`, `GET /completions?consumer=…` (terminal unacked, oldest-first, cap 100), `DELETE /completions/{id}`. The two read paths join the server's `RecordExclude`. The synchronous `POST /complete` route, its handler in `internal/inference`, and its tests (the retired D29 ids R-5P5E-5623, R-5QDA-IXSS, R-5ST3-AHA6, R-5U0Z-O90V, R-5V8W-20RK, R-5WGS-FSI9, R-5XOO-TK8Y, R-5YWL-7BZN, R-T1TD-KYWQ) are deleted; `POST /embed` and everything else in `internal/inference` stays.

End state: sibling daemons reach the durable queue on loopback; nothing on the box can reach a synchronous completion.

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim:
  - R-J7QG-FRDY — Ensure inserts `queued`, returns immediately, no provider call on the request path
  - R-J8YC-TJ4N — Ensure idempotent on `(consumer, key)`; distinct consumers are distinct items
  - R-JA69-7AVC — invalid Ensure → `400`, no `completions` row, no `calls` row
  - R-JJXG-9GSW — Get shape at every stage; unknown id → `404`
  - R-JL5C-N8JL — Inbox: own-consumer terminal items only, oldest-first, with key/context/result
  - R-JMD9-10AA — Ack deletes; repeat Ack `404`; re-Ensure after Ack is new work
  - R-JQ0Y-6BID — all four verbs pass the loopback guard with no identity headers
  - R-JR8U-K392 — `RecordExclude` carries exactly the two read paths, wired at composition
  - R-JSGQ-XUZR — `POST /complete` returns `404`; the synchronous handler is gone
- `grep -rn 'R-5P5E-5623\|R-5QDA-IXSS\|R-5ST3-AHA6\|R-5U0Z-O90V\|R-5V8W-20RK\|R-5WGS-FSI9\|R-5XOO-TK8Y\|R-5YWL-7BZN\|R-T1TD-KYWQ' --include='*_test.go' .` from `prompts/` returns nothing (the retired ids' tests are deleted with them).
- `go test ./...` from `prompts/` is green.
