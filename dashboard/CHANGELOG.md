# Changelog

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
