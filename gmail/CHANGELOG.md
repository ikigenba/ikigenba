# Changelog

## v0.20.0 — 2026-08-17

- The Gmail refresh token now lives in the service's own `state/` directory (a
  backed-up, per-service `state/GMAIL_REFRESH_TOKEN` file) instead of the shared
  app-config secret. gmail reads it at startup and persists any provider-rotated
  token back to the same file, so a rotated credential survives restarts and is
  no longer shared across environments. The static OAuth client id and secret
  are unchanged. Operators seed the token once per box before this version runs;
  a missing file fails startup loudly rather than starting unconfigured.

## v0.19.0 — 2026-08-15

- Adopted the suite-wide LLM-lint semantic gate (D31); the existing test suite
  already satisfied it, so no code or test changes were required. Bumped as part
  of the suite-wide gate-adoption release. No user-facing behavior, API, schema,
  or data changes; the shipped binary is functionally unchanged.

## v0.18.0 — 2026-08-15

- Internal code-quality hardening: the service now conforms to the suite's strict mechanical lint tier (formatting, complexity, and style rules enforced by the shared lint gate). No user-facing behavior, API, or data changes.

## v0.17.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.

## v0.16.0 — 2026-08-07

- baseline; changes before this version are recorded only in git history
