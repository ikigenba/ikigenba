# Phase 41 — Emit an `edge` telemetry record for every gated auth decision

*Realizes design Decision 31 (edge records) and 32 (the audit log stays).
Depends on Phase 40.*

With the correlation id minted at the edge, every admission decision becomes a
record in the suite's forensic trail: allows tied to the id the request goes on
to carry, denials and rate-limited attempts each chained on their own id. The
dashboard's durable audit log is untouched and keeps writing exactly as it does
today — this phase adds a second, best-effort record beside it, never in place of
it.

**Cross-workspace dependency.** appkit's telemetry recorder lands before this
phase builds (root/registry → appkit and eventplane → telemetry service →
dashboard). It is obtained from the `*appkit.Router` — a Router-level accessor
alongside `rt.HTTPClient(...)` — and **never constructed by the dashboard**; edge
records are the direct-emitter case that goes *through* the chassis recorder, not
around it. The dashboard consumes it via the one-method `telemetryRecorder`
interface it owns, satisfied at the composition root; a nil/disabled recorder is
a valid configuration and makes every call a silent no-op.

The end state:

- `internal/server` owns the `telemetryRecorder` seam and a nil-safe helper that
  builds and hands off the `edge` record; `cmd/dashboard/main.go` satisfies it
  with the recorder taken off the captured `*appkit.Router` (the same `var rt
  *appkit.Router` idiom the metrics collector uses for `rt.Logger()`) — no
  dashboard-constructed recorder and no second ingest client.
- `handleAuthn`, `handleAuthnPAT`, and `handleSessionAuthn` each emit exactly one
  `edge` record per decision, after the status and headers are determined:
  `kind: edge`, `service: dashboard`, the phase-40 correlation id, `op` composed
  from `X-Original-Method` + `X-Original-URI` (falling back to the subrequest's
  own method when the former is absent), `actor` filled as far as identity was
  established, `outcome` carrying the status and the deny reason class, and
  `detail: {decision, service}` with `decision` one of `allow`, `deny`,
  `rate_limited`.
- The record is assembled field by field from named values: no header dump, no
  body, and never the bearer token, PAT, session cookie, or `Authorization`
  value.
- Recording is fire-and-forget: it cannot return an error into the handler and
  cannot change the status, headers, or body.
- `internal/audit` and the `audit_log` schema, event types, and write sites are
  unchanged.

**Done when:** these ids are covered by clearly-named tests and the suite is
green. The endpoints are driven as real HTTP through `(*app).routes()` against a
real temp database, and every claim about a record being *delivered* is asserted
against a **live in-process HTTP sink** — an `httptest.Server` standing in for
`POST /ingest`, wired as the recorder's real ingest target and flushed before the
assertion, with the test reading the decoded JSON the sink received. A stub
recorder is not an acceptable substrate for these ids.

- R-XDJH-4Y8Q — an allow delivers exactly one record with `kind: "edge"`,
  `detail.decision: "allow"`, and `correlation_id` equal to the
  `X-Correlation-Id` the same response returned.
- R-XERD-IPZF — a deny (invalid token, 401) delivers a record with
  `detail.decision: "deny"` and `outcome.status` 401, and two successive denies
  deliver two records with different `correlation_id` values.
- R-XFZ9-WHQ4 — a rate-limited request (429) delivers a record with
  `detail.decision: "rate_limited"` and `outcome.status` 429.
- R-XIF2-O17I — the delivered `op` equals `"<X-Original-Method> <X-Original-URI>"`
  (driven with a method that differs from the subrequest's own, so a
  subrequest-method implementation fails) and `detail.service` is the addressed
  service name.
- R-XJMZ-1SY7 — a `handleSessionAuthn` allow and a `handleSessionAuthn` 401 each
  deliver their own edge record.
- R-XKUV-FKOW — with the sink unreachable (a closed listener) an allow still
  returns 200 with its full identity and correlation headers and a deny still
  returns 401 — byte-identical to the healthy-sink run.
- R-XM2R-TCFL — across an allow and a deny, the raw bytes the sink received
  contain neither the presented bearer token/PAT, nor the session cookie value,
  nor the literal `Authorization` header value.
- R-XNAO-746A — a single allow produces both an `authn.allow` row in the real
  temp database's `audit_log` (with its owner email, client id, and chain id as
  before) and an edge record at the sink, and the audit row is still written when
  the sink is unreachable.

Green means, from `dashboard/`: `go build ./...`, `go vet ./...`, `gofmt -l .`
(no output), and `go test ./...` all succeed with zero failures.
