# Changelog

## v0.43.0 — 2026-08-17

- Adopt the per-service customer-data and dev-config env manifests: `env.list` now authors the shipped `manifest.env`. Redeployed to verify manifest and secret handling end to end. No API, schema, or data changes.
- Remove the retired per-project `.envrc` file; dev-config now flows through the authored manifest.
- Adopt the suite-wide LLM-lint semantic test gate (D31, Phase 85); fixed-duration sleeps replaced by deterministic synchronization. No shipped-behavior change.

## v0.42.0 — 2026-08-15

- Adopted the suite-wide LLM-lint semantic gate (D31); the existing test suite
  already satisfied it, so no code or test changes were required. Bumped as part
  of the suite-wide gate-adoption release. No user-facing behavior, API, schema,
  or data changes; the shipped binary is functionally unchanged.

## v0.41.0 — 2026-08-15

- Internal code-quality hardening: the service now conforms to the suite's strict mechanical lint tier (formatting, complexity, and style rules enforced by the shared lint gate). No user-facing behavior, API, or data changes.

## v0.40.0 — 2026-08-12

- The completion queue now enforces per-consumer ownership leases and partitions: a consumer only ever sees and acknowledges its own items, abandonment surfaces a distinct error code, and store partition guards are proven in the gate.
- The executor's leases and worker pool are hardened for resilience, so an interrupted or slow completion recovers cleanly instead of stranding the pool.
- The queue HTTP contract and health check gained depth coverage against the real binary.
- Flaky 2 s async waits no longer destabilize the test gate.

## v0.39.0 — 2026-08-11

- Completions for sibling services are now a durable per-consumer work queue (`POST/GET/DELETE /completions`): submit returns immediately, results wait until acknowledged (plus a 7-day safety TTL), and interrupted items requeue and re-execute on boot.
- Every completion result is guaranteed valid JSON: prompts enforces a fixed reply envelope with up to 3 internal corrective round trips, and hands consumers only the unwrapped result or an honest error.
- The queue's `context` field is an arbitrary JSON value, echoed back byte-for-byte; malformed context is rejected at submission.
- The synchronous `POST /complete` endpoint is removed — it silently severed every response slower than ~15 s on the chassis write deadline.

## v0.38.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.
- All five prompts pages carry it, not just the index.

## v0.37.0 — 2026-08-09

- baseline; changes before this version are recorded only in git history
