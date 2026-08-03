# Phase 21 — The github peers move onto the chassis's instrumented outbound HTTP client

*Realizes design Decision 11 (instrumented outbound client). No dependency on
another pending phase — but this phase cannot build until the suite-level
`appkit` work has landed the Router-provided instrumented outbound HTTP client (`rt.HTTPClient`),
because the seam it consumes lives there. The operator runs the appkit and
eventplane loops first.*

`http.DefaultClient` leaves repos entirely:

- `cmd/repos/spec.go` — the `NewHTTPTokenSource(...)` and `NewGitHubPeer(...)`
  calls in the `Handlers` closure take `rt.HTTPClient(…)`, the Router-provided
  instrumented client (`rt` is already in scope there). Resolve `rt.HTTPClient`'s
  exact argument list from appkit's `project/design/INDEX.md` and its shipped
  source, both of which exist by the time this phase runs.
- `internal/repos/git.go` and `internal/repos/ghpeer.go` — the two nil-client
  fallbacks to `http.DefaultClient` are **deleted**, not repointed.

The injected `*http.Client` parameters stay: they are the seam tests use to point
either peer at an `httptest` server. No signature changes, no behavior change to
any GitHub verb.

Explicitly **not** in this phase: git subprocesses and dispatched agent sessions
are out of recording scope (D11) — nothing wraps `exec.Command`, and no
correlation id is ever added to a git invocation, which pushes to github.com, a
third party.

**Done when** (all commands from `repos/`):

- `R-BT9N-POGV` is covered by a test that drives a `GitHubPeer` verb against an
  `httptest` server with an injected client whose `Transport` records each
  request, asserting the request went through that client and its `Context()`
  carried the exact correlation id placed on the call's context.
- `R-BUHK-3G7K` is covered by the same shape for `HTTPTokenSource.Token` against
  an `httptest` `/token` stub — **including the refetch** after the injected
  clock advances past the cached token's expiry, so a refresh is proven to stay
  on the chain.
- `grep -rn 'http\.DefaultClient' --include='*.go' . | grep -v '_test\.go'` prints nothing (exit 1 from the final grep).
- The suite is green as design's *Conventions* define it: `go build ./...`, `go vet ./...`, `go test ./...` all clean and `gofmt -l .` empty.
