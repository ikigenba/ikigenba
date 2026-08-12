# Phase 22 — The bare `/favicon.ico` root route

*Realizes design Decision 15 (adopt the suite brand icon contract).*

`internal/githubapp/spec.go` mounts `GET /favicon.ico` beside the `GET /{$}` and
`GET /static/` routes it already registers. The handler rewrites the request path
to `/static/favicon.ico` on a shallow copy of the request and delegates to the
same `StaticHandler()`, so the root path returns what the static path returns
without a second read of the embedded tree. It is registered ungated, like the
static mount.

The end state is github answering the bare `/favicon.ico` on its loopback port
and behind the prefix-stripping front door alike, which is the request the
suite's brand icon contract (`root project/design/D29.md`) ships `favicon.ico`
for. github serves its own embedded static tree rather than declaring
`Spec.WWW`, so appkit's chassis route does not reach it and this phase does not
depend on appkit.

**Done when:**

- `R-8MFA-HUNC` is covered by a clearly-named test in `internal/githubapp/`, tagged with
  the id verbatim: the real assembled router, reached through the composition
  root and never a directly-constructed handler, answers `GET /favicon.ico` at
  the root path with `200` (not a `3xx`), `Content-Type` exactly `image/x-icon`,
  and a body byte-identical to the same router's `GET static/favicon.ico`
  response.
  The test assembles the router with the package's existing per-test generated
  RSA key fixture (`IKIGENBA_APP_ID`, `IKIGENBA_GITHUB_ORG`,
  `IKIGENBA_APP_PRIVATE_KEY`), since `Spec()` cannot be built without them; it
  reads no credential from the machine and reaches no network.
- The suite is green as design's Conventions define it: `go build ./...`, `go vet ./...`, `gofmt -l .` printing nothing, and
  `go test ./...` all succeed from `github/`
  with zero failures.
