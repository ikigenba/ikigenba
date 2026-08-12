# Phase 13 — The bare `/favicon.ico` root route

*Realizes design Decision 11 (adopt the suite brand icon contract).*

`cmd/artifacts/main.go` registers `rt.Handle("GET /favicon.ico", …)` beside the
`GET /static/` mount it already makes. The handler rewrites the request path to
`/static/favicon.ico` on a shallow copy of the request and delegates to
`artifactweb.StaticHandler()`, so the root path returns what the static path
returns without a second read of the embedded tree. It is registered ungated,
like the static mount.

The end state is artifacts answering the bare `/favicon.ico` on its loopback
port and behind the prefix-stripping front door alike, which is the request the
suite's brand icon contract (`root project/design/D29.md`) ships `favicon.ico`
for. artifacts serves its own embedded static tree rather than declaring
`Spec.WWW`, so appkit's chassis route does not reach it and this phase does not
depend on appkit.

**Done when:**

- `R-8MFA-HUNC` is covered by a clearly-named test in `cmd/artifacts/`, tagged with
  the id verbatim: the real assembled router, reached through the composition
  root and never a directly-constructed handler, answers `GET /favicon.ico` at
  the root path with `200` (not a `3xx`), `Content-Type` exactly `image/x-icon`,
  and a body byte-identical to the same router's `GET static/favicon.ico`
  response.
- The suite is green as design's Conventions define it: `go build ./...`, `go vet ./...`, `gofmt -l .` printing nothing, and
  `go test ./...` all succeed from `artifacts/`
  with zero failures.
