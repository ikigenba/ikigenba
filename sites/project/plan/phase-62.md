# Phase 62 — Relative trailing-slash redirect in the static server

*Realizes design Decision 17 (in-process static serving).*

`internal/serve`'s directory-without-trailing-slash handling emits a **relative**
`Location` — the final path segment plus `/` (request `…/blog` → `Location:
blog/`) — instead of the default absolute path that carries the `/public/` (or
`/private/`) mount prefix. The relative reference resolves against whatever URL
the client requested, so the redirect is correct both on the internal host and
behind a custom-domain proxy that prefixes the forwarded path; the mount-prefix
leak and the double-prefix `404` are gone. The retired absolute-redirect test
(`R-R4SO-LZXO`) is removed and replaced by the new pin below.

**Done when:**

- R-FIQQ-Q2B5 — a `GET` for an existing directory without a trailing slash returns
  `301` whose `Location` is the relative reference `<final-segment>/` (does not
  begin with `/`, does not contain the `/public/` mount prefix); driven a second
  time with the same handler reached under a deeper request path (as a
  custom-domain proxy forwards it) still returning the sibling relative
  `Location`.
- No test tagged `R-R4SO-LZXO` remains in the tree
  (`grep -rn 'R-R4SO-LZXO' --include='*_test.go' .` is empty).
- `cd sites && go build ./... && go vet ./... && gofmt -l . && go test ./...` is
  green (the D17 handler suite included).
