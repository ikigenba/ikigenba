# Phase 1 — Create the standalone zero-dependency `registry` module

*Realizes design Decision 1 (standalone module). Structural — owns no `R-` ids.
Depends on no earlier phase.*

## What gets built

The new module skeleton at the repo root, entirely within `registry/`:

- `registry/go.mod` — `module registry`, `go 1.26`, and **no `require` block** (no
  third-party dependencies).
- `registry/doc.go` — a package-level doc comment for package `registry` stating it
  is the authoritative name→port table for the suite (this gives the package a
  compilable Go file so the module builds green before D2/D3 add content).

Do **not** wire consumers in this phase: no edits to the root `go.work`, to any
other module's `go.mod`, to `appkit`, `opsctl`, nginx, or `bin/`. Adoption is
downstream. The module must build standalone with `GOWORK=off`.

Observable end state: `registry/` is a self-contained Go module in package
`registry` that builds green in isolation and pulls in nothing outside the Go
standard library.

## Done when

All of the following hold on identical repo state, from the module root
(`registry/`):

- `GOWORK=off go build ./...` exits 0.
- `GOWORK=off go test ./...` exits 0 (a package with no tests yet is still green).
- **Zero third-party dependencies:**
  `GOWORK=off go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./...`
  produces **no output** (every dependency is standard-library).
- `registry/go.mod` exists with `module registry` and contains **no** `require`
  directive: `grep -c '^require' registry/go.mod` returns `0`.
