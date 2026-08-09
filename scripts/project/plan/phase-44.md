# Phase 44 — The trigger envelope: one event shape suite-wide

*Realizes design Decision 21 (the `suite` module — `suite.event()` envelope
revision) and Decision 17's run-input paragraph. Depends on Phase 43.*

What exists at the end:

- The fire path builds the **trigger envelope** at spawn:
  `{"source", "kind", "subject", "event_id", "payload"}` JSON assembled from
  the delivery fields `FireFunc` already carries, with the producer's payload
  embedded verbatim (raw JSON) under `payload`. `runner.execute` passes those
  bytes on stdin and `$EVENT_JSON` exactly as before; a manual run still
  passes the literal `"{}"`.
- `internal/runner/suite.py` — `suite.event()`'s docstring states the envelope
  shape (mechanically it still parses `EVENT_JSON` once and caches).
- `internal/mcp/describe.go` — the `suite.event()` line describes the envelope
  (`{source, kind, subject, event_id, payload}`, payload verbatim under
  `payload`; `{}` for a manual run) per D26.
- The old bare-payload probe test (tagged R-HY0I-7A8R) is deleted with its
  retired id; no `R-HY0I-7A8R` tag remains anywhere in the tree.

**Done when:**

- R-NKLN-6D6F — a probe run fired through the real spawn path observes
  `suite.event()` deep-equal to the full envelope (payload verbatim under
  `payload`, the four routing fields beside it, nothing else), and a manual
  run observes exactly `{}` — covered by a tagged test.
- `grep -rn 'R-HY0I-7A8R' --exclude-dir=project .` from `scripts/` prints
  nothing.
- The suite is green per design Conventions (`cd scripts && go build ./... &&
  go vet ./... && go test ./...`, `gofmt -l .` silent).
