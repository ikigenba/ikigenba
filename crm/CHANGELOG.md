# Changelog

## v0.22.0 — 2026-08-17

- Adopt the per-service customer-data and dev-config env manifests: `env.list` now authors the shipped `manifest.env`. Redeployed to verify manifest and secret handling end to end. No API, schema, or data changes.

## v0.21.0 — 2026-08-15

- Adopted the suite-wide LLM-lint semantic test gate (D31). The test suite now
  passes the semantic lint tier, with fixed-duration sleeps replaced by
  deterministic synchronization. No user-facing behavior, API, schema, or data
  changes; the shipped binary is functionally unchanged.

## v0.20.0 — 2026-08-15

- Internal code-quality hardening: the service now conforms to the suite's strict mechanical lint tier (formatting, complexity, and style rules enforced by the shared lint gate). No user-facing behavior, API, or data changes.

## v0.19.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.

## v0.18.0 — 2026-08-07

- baseline; changes before this version are recorded only in git history
