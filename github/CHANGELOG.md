# Changelog

## v0.15.0 — 2026-08-17

- Adopt the per-service customer-data and dev-config env manifests: `env.list` now authors the shipped `manifest.env`. Redeployed to verify manifest and secret handling end to end. No API, schema, or data changes.
- Remove the retired per-project `.envrc` file; dev-config now flows through the authored manifest.

## v0.14.0 — 2026-08-15

- Adopted the suite-wide LLM-lint semantic gate (D31); the existing test suite
  already satisfied it, so no code or test changes were required. Bumped as part
  of the suite-wide gate-adoption release. No user-facing behavior, API, schema,
  or data changes; the shipped binary is functionally unchanged.

## v0.13.0 — 2026-08-15

- Internal code-quality hardening: the service now conforms to the suite's strict mechanical lint tier (formatting, complexity, and style rules enforced by the shared lint gate). No user-facing behavior, API, or data changes.

## v0.12.0 — 2026-08-13

- The connector landing page and its assets (styles, fonts, and icons) are now
  served from files on disk through the shared web mount, instead of being
  compiled into the service binary. The page looks and behaves exactly as
  before; this is an internal consistency change that brings github in line with
  how the other services serve their web assets.

## v0.11.1 — 2026-08-12

- A browser's unprompted request for the site icon at the service root now
  returns the ikigenba mark instead of "not found". This is the request a client
  makes without reading any page markup, and it had never been answered.
- Nothing else changed. Tabs and bookmarks already showed the mark from the page
  markup and are unaffected; the icon is served without requiring a login.

## v0.11.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.

## v0.10.0 — 2026-08-07

- baseline; changes before this version are recorded only in git history
