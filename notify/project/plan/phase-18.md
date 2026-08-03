# Phase 18 — Telemetry adoption, Go side: injected instrumented ntfy client and a correlation id that survives the async push seam

*Realizes design Decision 18 (telemetry adoption, Go side).*

**External dependency, operator-sequenced (not built here).** This phase assumes
**appkit** (the Router's instrumented outbound client seam, `rt.HTTPClient(…)`)
and **eventplane** (the revised consumer, which surfaces the event's correlation
id into the handler's context) are built and their `replace`-sibling modules updated in place. Both are
separate workspaces with their own plans; the operator runs them first. notify
produces nothing, so unlike a producer service it has no compile-caught `Append`
change to absorb — and still no outbox and no migration.

**What gets built.**

- `internal/push/push.go` — `NewClient(baseURL, topic, token string, hc
  *http.Client, logger *slog.Logger) *Client`: the HTTP client is injected and
  the package constructs none. The 10s timeout constant stays here as notify's
  policy, exported as `push.PushTimeout` so the composition root can pass it;
  `Publish`/`Send` are otherwise unchanged.
- `internal/push/push.go` + `internal/push/prompts.go` — both handlers' detached
  push goroutines derive from the handler context:
  `context.WithTimeout(context.WithoutCancel(hctx), PushTimeout)` in place of
  `context.WithTimeout(context.Background(), pushTimeout)`. The handler still
  returns immediately, the engine still commits the cursor without waiting, and
  the goroutine is still bounded by `PushTimeout`.
- `cmd/notify/main.go` — obtains one instrumented client from the Router seam
  (`rt.HTTPClient(…)`, already wired to the recorder), sets `push.PushTimeout`
  on it, and hands it to all three `push.NewClient` sites (the two
  `Spec.Consumers` handler factories and the `Spec.Handlers` MCP `send` client).
  The wiring is factored so a test can install a recording
  `http.RoundTripper` through **the same path production uses**.

Observable end state: every ntfy push is recorded as `outbound` telemetry and
sits on the chain of the event or MCP call that caused it; the owner receives
exactly the same pushes for exactly the same facts as before.

**Done when:** the suite is green — `cd notify && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), `go test ./...` all succeed with zero
failures — and each id below is covered by a clearly-named, genuinely-asserting
test:

- **R-TK2Y-2LTX** — a `push.Client` built the composition root's way POSTs
  through the injected client: the request arrives at the mock ntfy server *and*
  is observed at the recording seam, `internal/push` constructs no
  `http.Client` of its own, and the injected client's `Timeout` is exactly
  `push.PushTimeout`.
- **R-TLAU-GDKM** — the crm `Handler`, given a handler context carrying a known
  correlation id and a matching `contact.created` event, causes a POST that
  reaches the mock on a context whose `correlation.From` equals that id, and the
  POST still completes when the handler context is cancelled right after the
  handler returns.
- **R-TMIQ-U5BB** — `PromptsHandler` does the same for both matched kinds
  (`run.succeeded` and `run.failed`): correct id at the POST, and survival of
  handler-context cancellation.
- **R-TNQN-7X20** — an MCP `send` through the assembled chassis handler with an
  inbound `X-Correlation-Id` reaches the mock on a context whose
  `correlation.From` equals that inbound value, and the tool returns its success
  `structuredContent`.

Note for the builder: do **not** assert anything about whether
`X-Correlation-Id` reaches the mock ntfy server. The mock is on `127.0.0.1`,
which appkit's propagation rule treats as a suite peer, so that substrate cannot
falsify the production behavior (ntfy.sh is a third party). Propagation policy is
appkit's claim, proven there.
