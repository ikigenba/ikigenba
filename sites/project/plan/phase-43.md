# Phase 43 — The dropbox mirror client moves onto the chassis's instrumented outbound HTTP client

*Realizes design Decision 28 (instrumented outbound client). No dependency on
another pending phase — but this phase cannot build until the suite-level
`appkit` work has landed the Router-provided instrumented outbound HTTP client (`rt.HTTPClient`),
because the seam it consumes lives there. The operator runs the appkit/eventplane loops first.*

`internal/sites/sync.go`'s `httpMirrorClient` stops using `http.DefaultClient`.
`NewMirrorClient` gains an injected `*http.Client` parameter, and the composition
root supplies the Router-provided instrumented client:
`sites.NewMirrorClient(base, rt.HTTPClient(…))` inside `cmd/sites/main.go`'s
`Spec.Handlers` closure, where `rt` is already in scope. Resolve
`rt.HTTPClient`'s exact argument list from appkit's `project/design/INDEX.md` and
its shipped source, both of which exist by the time this phase runs.
`MirrorClient`, `List`, and `Fetch` keep their signatures, and no nil-client
fallback is added.

Nothing else in sites needs code: inbound recording, the read-or-mint
correlation middleware, and the `lifecycle` records all arrive with the appkit
rebuild, and sites produces no events, so the `eventplane` `Append` change cannot
reach its source. The `sync` verb already threads its handler context to the
mirror client and sites' non-test source contains no `context.Background()` —
this phase keeps both true and pins them with a test.

**Done when** (all commands from `sites/`):

- `R-BOE2-6LI3` is covered by a test that drives `List` (across at least two
  cursor pages) and `Fetch` through `NewMirrorClient` against an
  `httptest.NewServer`, with the injected client's `Transport` a recording
  `RoundTripper`, and asserts the recorder observed **every** request the server
  received — counts equal, nothing escaping to `http.DefaultClient`.
- `R-BPLY-KD8S` is covered by a test that drives the **`sync` MCP verb** through
  the `internal/mcp` handler over a temp `SITES_ROOT` and a real migrated DB,
  with the dropbox mirror stood up as an `httptest` server and the recording
  transport injected, and asserts the transport saw the inbound request's
  correlation id on the `Context()` of every outgoing request.
- `grep -rn 'http\.DefaultClient' --include='*.go' . | grep -v '_test\.go'` prints nothing (exit 1 from the final grep).
- The suite is green as design's *Conventions* define it: `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all clean.
