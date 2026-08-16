# Changelog

## v0.35.0 — 2026-08-15

- Adopted the suite-wide LLM-lint semantic test gate (D31). The test suite now
  passes the semantic lint tier, with fixed-duration sleeps replaced by
  deterministic synchronization. No user-facing behavior, API, schema, or data
  changes; the shipped binary is functionally unchanged.

## v0.34.0 — 2026-08-15

- Internal code-quality hardening: the service now conforms to the suite's strict mechanical lint tier (formatting, complexity, and style rules enforced by the shared lint gate). No user-facing behavior, API, or data changes.

## v0.33.0 — 2026-08-14

- The version shown in every page footer is now the real running version of the
  deployed dashboard. It previously always displayed the placeholder
  `v0.0.0+dev`, because the footer read its version from a source that is empty
  in production builds; it now reads the same build-stamped version the box and
  the `version` command already report.
- Nothing else about the pages changed.

## v0.32.0 — 2026-08-13

- The install page now offers a fourth one-paste command, for Antigravity,
  alongside Claude Code, Codex, and Grok. One curl line registers every MCP
  service on the box in Antigravity's configuration, using the same personal
  access token as the other agents.
- Nothing else about connecting an agent changed: the token still comes from
  the profile page via `IKIGENBA_TOKEN`, and the home page is still just the
  service directory.

## v0.31.0 — 2026-08-13

- The install page now offers a third one-paste command, for Grok, alongside
  Claude Code and Codex. One curl line registers every MCP service on the box
  in Grok's config, using the same personal access token as the other agents.
- Nothing else about connecting an agent changed: the token still comes from
  the profile page via `IKIGENBA_TOKEN`, and the home page is still just the
  service directory.

## v0.30.1 — 2026-08-12

- A browser's unprompted request for the site icon at the account root now
  returns the ikigenba mark instead of "not found". The previous release
  described this as already working; it was not, so any client that asks for the
  icon by its root path rather than by reading a page's markup got nothing back.
- Nothing else changed. Tabs, bookmarks, and the iOS home screen already showed
  the mark from the page markup and are unaffected; the icon is served without
  requiring a login, as it must be for clients that cannot sign in.

## v0.30.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.
- Because the dashboard answers the account's apex, it is also what serves the icon for a browser's unprompted request at the site root, so the mark appears even on pages reached before any service page is opened.

## v0.29.0 — 2026-08-10

- The web surface adopts the suite's shared wiki-style shell: one bounding
  column, a common header (logo, Dashboard/Install/Metrics links, sign-out,
  avatar) and a version footer on every signed-in page.
- The home page is now the MCP service directory alone; connect-your-agent
  instructions move to a new session-gated `/install` page.
- System font stacks replace the self-hosted web fonts (woff2 files and
  preloads removed); the wiki logo is served at `/static/logo.png`.
- Metrics charts render in a two-column panel grid; profile and the OAuth
  authorize / token show-once pages wear the shared shell.

## v0.28.1 — 2026-08-09

- baseline; changes before this version are recorded only in git history
