# Phase 31 — Telemetry adoption, Go side: injected instrumented outbound clients, daemon root chains, context-threaded `Append`

*Realizes design Decision 26 (telemetry adoption, Go side).*

**External dependency, operator-sequenced (not built here).** This phase does
not compile until **appkit** (the Router's instrumented outbound client seam and
its root-start helper) and **eventplane** (the context-taking `Outbox.Append`)
are built and their `replace`-sibling modules updated in place.
Both are separate workspaces with their own plans; the operator runs them first.
The `Append` signature change is compile-caught, so a premature run of this phase
fails at build, not silently.

**What gets built.** dropbox stops constructing outbound HTTP clients and starts
minting root correlation chains for the work nobody asked for.

- `internal/dropbox/client.go` — `NewClient(cfg Config, rpc, longpoll
  *http.Client) *Client`: both clients injected, neither constructed here, no
  nil-means-default fallback. The `tokenSource` keeps using the rpc client.
- `internal/mcp/tools.go` — `Tools(svc, sourcePortAllowed, source *http.Client)`:
  the `source_url` fetcher is injected too; the package constructs no client.
- `internal/dropbox/sync.go` — `Engine.bootstrap` and each `steadyState`
  iteration open a fresh root chain through the **chassis's root-start helper**
  (origins `dropbox:sync/bootstrap` and `dropbox:sync/longpoll`) and run the
  whole cycle on the returned context. One root per cycle — never per detected
  change or per emitted event. dropbox mints no id and assembles no record
  itself.
- `internal/dropbox/uploader.go` — each drain pass opens its own root chain the
  same way (origin `dropbox:upload/drain`) and runs its Dropbox write calls on
  it.
- `internal/dropbox/events.go` — the `EventSink` seam takes a
  `context.Context`; the one `Append` call site passes the context the change is
  already on. No payload field is added.
- `cmd/dropbox/main.go` — obtains all three clients from the Router's
  instrumented outbound client seam (`rt.HTTPClient(…)`, already wired to the
  recorder), each keeping its own policy: the rpc/content client's ~100s
  timeout, the longpoll client's `longpollClientTimeout` (≥600s), and the
  `source_url` client's 5s dial / 5s response-header transport with
  `http.ErrUseLastResponse`. The wiring is factored so a test can install a
  recording `http.RoundTripper` through **the same path production uses**.

Observable end state: every dropbox outbound call is recorded as `outbound`
telemetry; no correlation header reaches Dropbox; the loopback `source_url`
fetch carries one; background work lands on a chain instead of an empty id; and
dropbox's behavior is otherwise byte-for-byte what it was.

**Done when:** the suite is green — `cd dropbox && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), `go test ./...` all succeed with zero
failures — and each id below is covered by a clearly-named, genuinely-asserting
test:

- **R-TABR-0FWD** — rpc/content/token-refresh calls driven against the
  `httptest` fake Dropbox on a context carrying a valid correlation id are all
  observed at the injected instrumented seam, and none carries an
  `X-Correlation-Id` header.
- **R-TBJN-E7N2** — the composition root's wiring yields three distinct clients
  over the instrumented seam with preserved policies: longpoll `Timeout` ≥ 600s,
  rpc/content `Timeout` > 0 and ≤ 120s, the two not the same client value, and
  the `source_url` client's `CheckRedirect` returning `http.ErrUseLastResponse`.
- **R-TCRJ-RZDR** — an MCP `put` with a `source_url` on a real loopback
  `httptest` server, driven through the assembled MCP handler on a context
  carrying a known id, reaches that server with `X-Correlation-Id` equal to the
  id, is observed at the instrumented seam, and the fetched bytes land in the
  mirror.
- **R-TDZG-5R4G** — starting from a context with **no** correlation id, a sync
  cycle and an uploader drain pass each run on a non-empty, 26-character,
  Crockford-valid id that differs between two consecutive cycles of the same
  daemon and is **one and the same** id across every Dropbox API call and every
  event a single multi-change cycle makes (a root-per-change build fails).
- **R-TF7C-JIV5** — outbox rows read back by SQL from a real temp SQLite
  database migrated through the full embedded set: an event from a service write
  on a known-id context stores that exact id in `correlation_id`, and an event
  from a sync apply stores the cycle's root id (non-empty, Crockford-valid).
