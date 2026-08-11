# Phase 21 — Adopt the suite brand icon set

*Realizes design Decision 15 (adopt the suite brand icon contract).*

The three shipped icon files are committed into `github/internal/web/static/` — copied
verbatim from `design/brand/` — and the three `<link>` elements are added to
the document that github serves, in the `href` form D15 fixes for this tree.
`internal/web/web.go`'s `staticContentType` switch gains an `.ico` case returning `image/x-icon`, since github serves its own embedded static subtree through `StaticHandler` rather than through `appkit/web`.

The observable end state: a browser loading any page github serves shows the
suite's icon in its tab, and each of the three asset paths answers through the
service's real mounted router with the pinned content type.

**Done when:**

- `R-RYDN-YNR5` is covered by a test tagged with the id verbatim in `internal/githubapp/`: it
  enumerates the documents github serves from the committed tree, renders each
  through the real rendering path, and asserts all three link elements are
  present in every one. Removing the links from any single document must make it
  fail.
- `R-RZLK-CFHU` is covered by a test tagged with the id verbatim in `internal/githubapp/`: it
  drives github's assembled router and asserts `GET` of each of the three asset
  paths returns `200`, a non-empty body, and `Content-Type` exactly
  `image/x-icon` (ICO) or `image/png` (both PNGs).
- The three files exist in `github/internal/web/static/` and are byte-identical to their
  counterparts in `design/brand/`.
- The suite is green exactly as `github/project/design/README.md`'s Conventions
  define it, with zero failures.
