# Phase 157 — Adopt the suite brand icon set

*Realizes design Decision 90 (adopt the suite brand icon contract).*

The three shipped icon files are committed into `wiki/share/www/static/` — copied
verbatim from `design/brand/` — and the three `<link>` elements are added to
every document that wiki serves, in the `href` form D90 fixes for this tree.
The existing brand `<img>` in the layout header is a different asset with a different job and is not touched.

The observable end state: a browser loading any page wiki serves shows the
suite's icon in its tab, and each of the three asset paths answers through the
service's real mounted router with the pinned content type.

**Done when:**

- `R-RYDN-YNR5` is covered by a test tagged with the id verbatim in `cmd/wiki/`: it
  enumerates the documents wiki serves from the committed tree, renders each
  through the real rendering path, and asserts all three link elements are
  present in every one. Removing the links from any single document must make it
  fail.
- `R-RZLK-CFHU` is covered by a test tagged with the id verbatim in `cmd/wiki/`: it
  drives wiki's assembled router and asserts `GET` of each of the three asset
  paths returns `200`, a non-empty body, and `Content-Type` exactly
  `image/x-icon` (ICO) or `image/png` (both PNGs).
- The three files exist in `wiki/share/www/static/` and are byte-identical to their
  counterparts in `design/brand/`.
- The suite is green exactly as `wiki/project/design/README.md`'s Conventions
  define it, with zero failures.
