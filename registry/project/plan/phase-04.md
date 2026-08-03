# Phase 4 — The `telemetry` row: name owns 3008 before the service exists

*Realizes design Decision 2 (the service table) — the `telemetry` slice of its
Verification ids only (`R-ZNFW-ORR6`); D2's other ids are already realized by
tagged tests in `registry_test.go`.*

The suite gains a fifteenth deployable service, `telemetry` (the forensic
telemetry store; see the suite-level `docs/telemetry-protocol.md` once it
exists). Its loopback port is reserved now, registry-first, so every consumer —
the appkit recorder's default ingest origin, `bin/start`, nginx wiring — derives
`3008` from this table and never hardcodes it. The observable end state: the
`Services` table carries a `{"telemetry", 3008, Core}` row (appended in the Core
run after `repos`/3007, per the append-only rule), and the resolution API
answers for it (`MustPort("telemetry")` → `3008`,
`BaseURL("telemetry")` → `http://127.0.0.1:3008`).

**Done when:**

- `R-ZNFW-ORR6` — `telemetry` is present, pinned to port `3008`, in the `Core`
  block — covered by a clearly-named test in `registry_test.go` tagged with the
  id verbatim.
- The existing guardrail ids stay green (uniqueness, block ranges, dashboard
  pin) — no existing row moved.
- The suite is green per design Conventions: `GOWORK=off go build ./...` and
  `GOWORK=off go test ./...` pass from `registry/` with no failures and no
  `SKIP`.
