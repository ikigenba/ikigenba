# Phase 52 — Adopt the suite brand icon set

*Realizes design Decision 41 (adopt the suite brand icon contract).*

The three shipped icon files are committed into `dashboard/ui/static/` — copied
verbatim from `design/brand/` — and the three `<link>` elements are added to
every document that dashboard serves, in the `href` form D41 fixes for this tree.
`internal/server/static.go` serves the embedded tree with `http.FileServerFS` rather than through `appkit/web`, so the dashboard pins the `.ico` type itself. The bare `/favicon.ico` request the dashboard receives at the apex is answered by the same file through the existing `/static/` route and needs no route of its own.

The observable end state: a browser loading any page dashboard serves shows the
suite's icon in its tab, and each of the three asset paths answers through the
service's real mounted router with the pinned content type.

**Done when:**

- `R-RYDN-YNR5` is covered by a test tagged with the id verbatim in `internal/server/`: it
  enumerates the documents dashboard serves from the committed tree, renders each
  through the real rendering path, and asserts all three link elements are
  present in every one. Removing the links from any single document must make it
  fail.
- `R-RZLK-CFHU` is covered by a test tagged with the id verbatim in `internal/server/`: it
  drives dashboard's assembled router and asserts `GET` of each of the three asset
  paths returns `200`, a non-empty body, and `Content-Type` exactly
  `image/x-icon` (ICO) or `image/png` (both PNGs).
- The three files exist in `dashboard/ui/static/` and are byte-identical to their
  counterparts in `design/brand/`.
- The suite is green exactly as `dashboard/project/design/README.md`'s Conventions
  define it, with zero failures.
