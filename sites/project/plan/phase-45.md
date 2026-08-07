# Phase 45 — Tool-shape tag (R-DB3A-15YZ) and the R-IEWI-3MXP re-home

*Realizes design Decision 13, slice R-DB3A-15YZ, and Decision 19, slice
R-IEWI-3MXP. No dependencies.*

Two tag corrections in test files; the shipped code is already correct.

1. **Tag the existing tool-shape assertion.** `internal/mcp/tools_test.go`'s
   `TestToolsList` already asserts, for every listed tool, a non-empty
   `description` and `inputSchema["type"] == "object"` (~lines 278-283). Add the
   `// R-DB3A-15YZ` tag on that assertion block (the loop over `result.Tools`).
   The existing `// R-Z8DD-BL71` tag on the membership/count assertions stays
   where it is.
2. **Re-home R-IEWI-3MXP onto a test that asserts what D19 states.** D19's
   R-IEWI-3MXP is island-`url` / anchor-`href` **byte-identity** for a public
   and a private site through the `GET /{$}` handler. The tag currently sits in
   `cmd/sites/main_test.go` on `TestWWWLandingRendersProgressiveControlMarkup`
   (~line 327), which asserts progressive-control markup — a different behavior.
   Remove that tag comment (the markup test itself stays as it is, and keeps its
   `// R-ICGP-C3GB` tag), and realize the id for real: in
   `TestLandingHandlerRendersJSONIslandFromSiteRows` (same file, public `atlas`
   + private `vault` seeds), additionally extract each row's visible anchor
   `href` from the rendered body and assert it is byte-identical to that same
   row's island `url`, tagged `// R-IEWI-3MXP` on the new assertions. (The
   unlisted case is already pinned separately by R-ZLS9-J2CO in
   `landing_visibility_test.go`.)

**Done when:**

- `grep -rn 'R-DB3A-15YZ' --include='*_test.go' .` from the sites root prints
  exactly one line, in `internal/mcp/tools_test.go` inside `TestToolsList` —
  every listed tool carries a non-empty description and an object `inputSchema`.
- `grep -rn 'R-IEWI-3MXP' --include='*_test.go' .` prints exactly one line, in
  `cmd/sites/main_test.go` inside
  `TestLandingHandlerRendersJSONIslandFromSiteRows` — each island element's
  `url` byte-identical to that row's anchor `href` for the public and the
  private site.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` silent, `go test ./...` all passing, Chrome present for D23).
