# D30 wrongly lists `registry` as a tree without its own spec

Observed while planning lint-tier adoptions (2026-08-14): `project/design/D30.md`
("Adoption is a local spec move" paragraph) names `registry` alongside the
repo-root module and `bin/bintest` as "trees without their own spec" whose
`.lint-tier` the operator flips directly. In fact `registry/project/` exists and
governs that tree, so registry's adoption should be an ordinary local spec move
like any other governed tree. The repo-root module and `bin/bintest` halves of
the sentence remain correct.

Out of scope here because D30 is sealed and this session is not an authoring
move; the fix is a one-clause amendment to D30 in the next umbrella authoring
session (registry's own adoption move is the natural occasion).
