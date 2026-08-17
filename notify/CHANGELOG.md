# Changelog

## v0.27.0 — 2026-08-17

- Operational minor release: suite-wide redeploy to verify env-manifest and secret handling across all services. No code, API, schema, or data changes since the previous version.

## v0.26.0 — 2026-08-17

- Purged stale references to the retired `.envrc` / `~/.secrets` secret-injection
  mechanism from the service's startup configuration comment and its two
  fail-loud error messages, so operator-facing text names the current secret
  sources. No user-facing behavior, API, schema, or data changes; the shipped
  binary is functionally unchanged apart from the wording of a startup error.

## v0.25.0 — 2026-08-15

- Adopted the suite-wide LLM-lint semantic test gate (D31). The test suite now
  passes the semantic lint tier, with fixed-duration sleeps replaced by
  deterministic synchronization. No user-facing behavior, API, schema, or data
  changes; the shipped binary is functionally unchanged.

## v0.24.0 — 2026-08-15

- Internal code-quality hardening: the service now conforms to the suite's strict mechanical lint tier (formatting, complexity, and style rules enforced by the shared lint gate). No user-facing behavior, API, or data changes.

## v0.23.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.

## v0.22.0 — 2026-08-07

- baseline; changes before this version are recorded only in git history
