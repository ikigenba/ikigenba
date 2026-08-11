# Phase 58 — Adopt the suite brand icon set

*Realizes design Decision 39 (adopt the suite brand icon contract).*

The three shipped icon files are committed into `sites/share/www/static/` — copied
verbatim from `design/brand/` — and the three `<link>` elements are added to
the document that sites serves, in the `href` form D39 fixes for this tree.
This phase changes sites' own landing page and nothing it hosts: the static websites an owner creates are the owner's documents, and sites never injects markup into them.

The observable end state: a browser loading any page sites serves shows the
suite's icon in its tab, and each of the three asset paths answers through the
service's real mounted router with the pinned content type.

**Done when:**

- `R-RYDN-YNR5` is covered by a test tagged with the id verbatim in `cmd/sites/`: it
  enumerates the documents sites serves from the committed tree, renders each
  through the real rendering path, and asserts all three link elements are
  present in every one. Removing the links from any single document must make it
  fail.
- `R-RZLK-CFHU` is covered by a test tagged with the id verbatim in `cmd/sites/`: it
  drives sites's assembled router and asserts `GET` of each of the three asset
  paths returns `200`, a non-empty body, and `Content-Type` exactly
  `image/x-icon` (ICO) or `image/png` (both PNGs).
- The three files exist in `sites/share/www/static/` and are byte-identical to their
  counterparts in `design/brand/`.
- The suite is green exactly as `sites/project/design/README.md`'s Conventions
  define it, with zero failures.
