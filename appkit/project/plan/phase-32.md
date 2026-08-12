# Phase 32 — The chassis root-path favicon route

*Realizes design Decision 7 (chassis integration: `Spec.WWW`, the auto-mounted
static route, `Router.WWW()`).*

`appkit/server` gains `faviconAlias`, a handler that answers the bare
`GET /favicon.ico` by rewriting the request path to `/static/favicon.ico` on a
shallow copy of the request and delegating to the handler it wraps. `server.New`
mounts it on the same condition that already mounts the static route — when
`Options.WWW != nil` and the server is not apex — so a `WWW`-enabled service
answers the root path with the same bytes and the same registered content type
its `/static/favicon.ico` response carries, and a service without `Options.WWW`
is untouched.

The end state is a service built on the chassis that answers `GET /favicon.ico`
at its own root with no route registration of its own, satisfying the root-path
half of the suite's brand icon contract (`root project/design/D29.md`) for every
service that mounts static through `Spec.WWW`.

The three trees that register their own static handlers — `artifacts`, `github`,
and `dashboard` — do not receive the route from this phase and carry their own,
each in its own tree under its own Decision. Nothing in this phase reaches
outside `appkit/`.

**Done when:**

- `R-8NN6-VME1` is covered by a clearly-named test in `appkit/server`, tagged
  with the id verbatim: a non-apex server built with `Options.WWW` over a real
  `t.TempDir()` root serves `GET /favicon.ico` with `200` (not a `3xx`), content
  type exactly `image/x-icon`, and a body byte-identical to the same server's
  `GET /static/favicon.ico` response, with no service-side `Handlers`
  registration for either path; the root's `static/favicon.ico` holds bytes that
  are not ICO magic, so a lost `.ico` registration fails on the content type;
  and the same server built without `Options.WWW` returns `404` for
  `GET /favicon.ico`.
- The suite is green from `appkit/` as design's Conventions define it:
  `go build ./...`, `go vet ./...`, `gofmt -l .` printing nothing, and
  `go test ./...` all succeed with zero failures.
