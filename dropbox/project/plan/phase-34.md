# Phase 34 — Realize the orphaned own-port id with its tagged test

*Realizes design Decision 9 (adopt `registry`), slice R-QJ8F-AXWP.*

R-QJ8F-AXWP is a current design id whose behavior is already built — the
composition root's `Spec.Port` is `registry.MustPort("dropbox")`
(`cmd/dropbox/main.go`) — but whose tag appears in no test file, so the
coverage invariant (every current id realized by a tagged test or assigned to
a pending phase) is broken. This phase adds the missing test; no production
code changes.

What gets built:

- A tagged test in `cmd/dropbox` asserting the assembled `appkit.Spec`'s
  `Port` equals `registry.MustPort("dropbox")` and that no `3200` integer
  literal supplies it — per D9's falsifiability note, a hardcoded
  `Port: 3200` must fail it.

**Done when:**

- R-QJ8F-AXWP appears verbatim as a tag on a genuinely-asserting test in
  `cmd/dropbox` that runs under the plain suite invocation (no skip, no build
  tag, no env gate).
- The suite is green per design Conventions: from `dropbox/`,
  `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0.
