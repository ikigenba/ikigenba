# Changelog

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
