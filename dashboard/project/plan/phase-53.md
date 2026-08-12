# Phase 53 — The bare `/favicon.ico` root route

*Realizes design Decision 41 (adopt the suite brand icon contract).*

`register` in `internal/server/routes.go` gains
`mux.Handle("GET /favicon.ico", …)` beside the existing `GET /static/` mount.
The handler rewrites the request path to `/static/favicon.ico` on a shallow copy
of the request and delegates to `a.staticHandler()`, so the root path returns
what the static path returns without a second read of the embedded tree. It is
registered outside every session gate, exactly as `GET /static/` is.

The end state is the apex answering the bare `/favicon.ico` a browser issues
before it has parsed any markup, which is the request the suite's brand icon
contract (`root project/design/D29.md`) ships `favicon.ico` for and which the
dashboard has been returning `404` for.

**Done when:**

- `R-8MFA-HUNC` is covered by a clearly-named test in `internal/server/`, tagged with
  the id verbatim: the real assembled router, reached through the composition
  root and never a directly-constructed handler, answers `GET /favicon.ico` at
  the root path with `200` (not a `3xx`), `Content-Type` exactly `image/x-icon`,
  and a body byte-identical to the same router's `GET static/favicon.ico`
  response.
  The request carries no session cookie and no bearer token, so a route that
  ended up behind the session gate fails on the login redirect.
- The suite is green as design's Conventions define it: `go build ./...`, `go vet ./...`, `gofmt -l .` printing nothing, and
  `go test ./...` all succeed from `dashboard/`
  with zero failures.
