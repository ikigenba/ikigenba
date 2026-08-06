# appkit — Research

External ground truth appkit's design depends on — the structured-results work
(D8/D9 revisions, D12) and the suite telemetry work (D14–D19). Non-contractual:
design cites these facts; nothing downstream reads this file mechanically.
Rewritten in place.

## MCP specification, revision 2025-06-18 (structured tool output)

The Model Context Protocol's 2025-06-18 revision is the first that carries
structured tool output. The facts the design uses:

- **`outputSchema`** — a tool descriptor may declare an `outputSchema` (a JSON
  Schema object) alongside `inputSchema` in `tools/list`. It describes the
  shape of the tool's structured result and is optional per tool.
- **`structuredContent`** — a `tools/call` result may carry a
  `structuredContent` field: a plain JSON object, sibling to the `content`
  array. When a tool declares an `outputSchema`, its results are expected to
  carry conforming `structuredContent`; servers **should** also return a
  functionally-equivalent text block in `content` for backward compatibility
  with clients that predate the field. `structuredContent` is an optional
  field on any tool result, so carrying it on `isError` results is legal.
- **Error semantics** — the spec distinguishes *protocol* errors (JSON-RPC
  error objects: `-32700` parse, `-32601` method not found, `-32602` invalid
  params — including an unknown tool name, `-32603` internal error) from
  *tool execution* errors, which are reported **inside** a successful JSON-RPC
  response as a result with `isError: true`. Domain failures belong on the
  `isError` channel; protocol-level codes are for transport/dispatch faults.
- **Version negotiation** — the client proposes a protocol version in
  `initialize`; the server replies with the version it speaks; on HTTP
  transports the client then stamps `MCP-Protocol-Version` on subsequent
  requests. Nothing obliges a minimal server to reject a client's newer
  version; replying with its own supported version is the negotiation.
- **The streamable-HTTP transport's `GET`, and the 405 that refuses it** — on
  an HTTP transport the endpoint carries two methods, not one: `POST` delivers
  JSON-RPC messages, and the client additionally opens a **`GET`** on the same
  URL to hold a server-to-client SSE stream for server-initiated messages. The
  spec provides explicitly for servers that offer no such stream: they
  **SHOULD** answer the `GET` with **`405 Method Not Allowed`**, which the
  client reads as a definitive refusal and does not retry. This is the fact
  that makes `405` the correct answer for a non-`POST` verb rather than `400`
  or `501`. The failure mode when it is not returned is not an error the client
  reports: a `200` whose body is not `text/event-stream` reads as a stream that
  opened and immediately closed, so the client reconnects on its retry timer
  (order of one second) for the life of the session, indefinitely and silently.
- Later revisions (e.g. 2025-11-25) add capabilities (tasks, elicitation,
  richer transports) that appkit's deliberately minimal plain-POST transport
  does not implement; `2025-06-18` is the lowest revision that carries
  everything this design needs, which is why it is the pinned answer.

## agentkit client compatibility (verified 2026-07-14)

agentkit (`github.com/ikigenba/agentkit`, the prompts service's agent chassis,
separate repo, checkout at `~/projects/agentkit`, v0.2.1) is the one existing
MCP *client* of appkit-served surfaces. Verified against its source:

- `internal/mcp/mcp.go:100` — `CallResult` decodes only `content` and
  `isError` with plain `encoding/json` (no `DisallowUnknownFields`); an added
  `structuredContent` sibling is ignored, and the full raw result JSON is
  retained in `CallResult.Raw`.
- `mcp.go:255` (`mcpResultText`) — the text an in-run agent sees is the join
  of the `content` text blocks; a mirrored-text result renders for agents
  exactly as today's `JSONResult` output does.
- `internal/mcp/mcp.go:85` — `Tool.UnmarshalJSON` extracts only
  `name`/`description`/`inputSchema`; an added `outputSchema` key in
  `tools/list` is ignored.
- `mcp.go:18,194` — agentkit proposes protocol `2025-11-25` and simply adopts
  the version the server returns; it does not reject a server that answers
  with an older revision.

Conclusion: appkit's protocol bump and result-shape additions require **no
agentkit change**. agentkit is a **lenient** client, though — it ignores
`outputSchema` entirely — so verifying against it proves nothing about clients
that *do* validate the advertised schemas. The strict client below is the one
that constrains the schema shapes.

## Strict MCP client schema validation (Claude Code / Anthropic tools API, verified 2026-07-15)

Claude Code is the suite's real end-user MCP *client*, and unlike agentkit it
**strictly validates every advertised `inputSchema`/`outputSchema`** before it
will use a server's tools. The rules the design must satisfy, established from
the Claude Code issue tracker and confirmed against the live box:

- **Draft 2020-12 + a top-level-object constraint.** Every advertised schema is
  checked as JSON Schema draft 2020-12 **and** against the Anthropic tools-API
  rule that the schema's **top level must be a plain object** — `"type":
  "object"` at the root, and **no `oneOf` / `anyOf` / `allOf` at the top
  level**. The API rejects a top-level composition with
  `input_schema does not support oneOf, allOf, or anyOf at the top level`;
  Claude Code's client-side validator likewise skips a tool/server whose schema
  has composition at the root (e.g. "invalid schema (oneOf at top level)").
- **One bad schema fails the whole server.** Validation is all-or-nothing per
  server: a single non-conforming tool descriptor fails the entire `tools/list`,
  and the client reports **`tools fetch failed`**. Because `initialize` carries
  no schemas, it still succeeds, so the server shows **`connected` yet
  `tools fetch failed`** — connected but tool-less. In the worst case a
  malformed schema 400s the whole session.
- **Curl is not a test.** A raw `curl` of `tools/list` returns 200 with a
  well-formed body — curl does no schema validation. Only a strict client
  (Claude Code, MCP Inspector's strict mode, any Zod/JSON-Schema-validating
  consumer) exercises this contract. The live proof is a strict client actually
  fetching the tools, not a 200 from curl.
- **Nested composition may be tolerated**, but appkit deliberately advertises
  **no** `oneOf`/`anyOf`/`allOf` anywhere in its schemas, so chassis correctness
  never rides on that uncertainty.

Evidence: claude-code issues #10606 (strict schema validation introduced
v2.0.21+, top-level `oneOf` server skipped), #10031 / #28620 (client requires a
top-level `"type": "object"`; a top-level `anyOf`/`oneOf` without it is
rejected). Observed 2026-07-15 on `int.ikigenba.com`: **all twelve services**
showed `△ connected · tools fetch failed` in Claude Code while `curl` of the
same `/srv/<svc>/mcp` `tools/list` returned 200 — because the one appkit schema
with a top-level `oneOf` (`reflection`'s `outputSchema`, D9) is present on every
service via the shared chassis. The design blind spot: only agentkit — which
ignores `outputSchema` — had been modeled as a client, so no strict validator
ever saw the emitted schemas until they reached Claude Code in production.

## Error-code vocabulary in live use (suite survey, 2026-07-14)

The string codes services already emit in tool-error text/JSON today:
`validation`, `not_found`, `conflict`, `too_large` (dropbox MCP, dropbox
filesystem API error vocabulary), `source_unavailable` (dropbox `put
source_url`, prompts sandbox Fetch/File* taxonomy). prompts' MCP layer mostly
returns bare `err.Error()` strings (uncoded — the gap the typed vocabulary
closes). No other code word is in suite-wide use; `internal` is added as the
residue code for faults that are no caller's fault.

## The suite telemetry contract (settled; appkit implements its chassis share)

The suite-wide telemetry capability is fixed by the suite-level protocol
document (`docs/telemetry-protocol.md`, written in the root workspace). appkit
consumes these values; it does not choose them and must not restate them with
different numbers.

- **The sink.** A new fifteenth deployable service, **telemetry**, on loopback
  port **3008**, mount `/srv/telemetry/`. Ingest is
  `POST http://127.0.0.1:3008/ingest`, loopback-only, body
  `{"records": [<record>, …], "dropped": <int, optional>}`, answered `202`.
  Best-effort by contract: telemetry being down never blocks, errors, or
  crashes a recording service. The registry row `{"telemetry", 3008, Core}` is
  added in the `registry` workspace, so `registry.BaseURL("telemetry")` is the
  address's single source.
- **The correlation header** is `X-Correlation-Id`, carrying a **bare 26-char
  Crockford base32 ULID** — the shape `docs/correlation-ids.md` already
  standardises (48 bits ms timestamp + 80 bits crypto randomness; alphabet
  `0123456789ABCDEFGHJKMNPQRSTVWXYZ`, i.e. no `I`, `L`, `O`, `U`). appkit's
  existing `logging.NewULID` encodes with **RFC 4648** base32 (`A–Z2–7`) and is
  therefore non-conforming; fixing it is part of this work.
- **The record's fields** are an allowlist: `id`, `time` (RFC3339Nano UTC),
  `correlation_id`, `service`, `kind` (one of `edge`, `request`, `outbound`,
  `publish`, `consume`, `root`, `lifecycle`), `actor`
  (`{owner_email, client_id}`, omitted when unknown), `op`, `params`,
  `outcome` (`{status, error, duration_ms, bytes, sha256}`), and `detail`
  (small kind-specific extras — `lifecycle` → `{version}`). Raw header dumps
  and raw bodies are never captured; response content is represented only by
  size + digest.
- **`op` conventions.** `request` → `mcp:<tool>` or `<METHOD> <path>`;
  `outbound` → `<METHOD> <host><path>`; `publish`/`consume` → the routing key
  `<source>:<kind>/<subject>`; `root` → the origin; `lifecycle` → `start` or
  `stop`; `edge` → `<METHOD> <original-uri>` (dashboard-owned, not appkit's).
- **Digests** are **SHA-256, lowercase hex**, everywhere.
- **Param capture thresholds** (contractual): per-value literal cap **1024
  bytes** and per-record params budget **8192 bytes**, both on the JSON
  encoding; over budget, elide the largest values first. An elided value is
  replaced **in place** by `{"$elided": {"bytes": N, "sha256": "<hex>"}}`.
  Params declared sensitive at tool registration are always elided.
- **Retention** (`TELEMETRY_RETENTION_DAYS`, default 90) is the telemetry
  service's business, not the chassis's.
- **Recorder defaults** (explicitly *not* contractual — appkit's to tune): ring
  ~4096 records dropping oldest, batches ≤256, flush ~1s or on a full batch,
  fire-and-forget POST, dropped count reported in the next successful batch.
- **Recursion rule.** The ingest path itself is never recorded. Telemetry's own
  MCP tools are recorded like any other service's. Every service emits
  `lifecycle` `start` (with version) and `stop` (graceful shutdown only — a
  crash has no `stop`, and the next `start` bounds the gap).
- **Propagation rule.** Outbound calls carry `X-Correlation-Id` **only** to
  loopback suite peers, never to third parties.
- **Chain roots.** Work with no inbound request to inherit from is a chain
  origin that mints its own id and emits a `root` record: a cron tick, a
  prompts/scripts run spawn, and **any background poll/watch/sync cycle** that
  publishes events or makes outbound calls (gmail's poll loop, dropbox's sync
  cycle, wiki's self-started pipeline work). The rule is **one root id per
  cycle, not per event**, so everything one cycle caused shares one chain. The
  eventplane library **never mints at `Append`** — an empty correlation id on
  an appended event is a recorded gap, not a licence to mint mid-chain. appkit
  owns the single root-start helper (mint, install on context, emit the `root`
  record) so nine services do not hand-roll the idiom.

## `eventplane/correlation` and the eventplane observation hooks (consumed)

Both are owned by the **eventplane** workspace and specified there; appkit
consumes them as fixed external facts and designs none of their internals. They
live in eventplane rather than appkit for one structural reason: **eventplane
must never import appkit**, and the correlation id must sit on a context key
both libraries read.

- **`eventplane/correlation`** is a stdlib-only leaf package owning the id's
  every primitive, and is the single suite-wide home of them (a second context
  key anywhere would silently split a chain). Its consumed surface:

  ```go
  const Header = "X-Correlation-Id"
  func New() string                                    // 26-char Crockford ULID
  func Valid(s string) bool                            // 26 chars, Crockford alphabet
  func WithContext(ctx context.Context, id string) context.Context // IGNORES an invalid id
  func FromContext(ctx context.Context) string         // "" when none
  func Ensure(ctx context.Context) (context.Context, string) // read-or-mint; never re-mints
  ```

  `WithContext` silently ignores an id that fails `Valid`, so a malformed value
  can never enter a chain; `Ensure` is the read-or-mint primitive every inbound
  edge uses.
- **`correlation_id` becomes a first-class outbox column and wire-envelope
  field**, populated by eventplane from the append context and surfaced into
  the consumer handler's context. This **supersedes** the payload-field
  convention in `docs/correlation-ids.md`. On the consumer side the *wire*
  value is reported verbatim on the delivered event (`""` when the event was
  uncorrelated) while the **handler's context** always carries a valid id —
  the wire id when it is valid, otherwise a fresh root eventplane mints per
  delivery. `consumer.Event.CorrelationID` reports the wire value **verbatim and
  is never backfilled** with that minted root — a guarantee eventplane pins in
  both directions, and the only signal available that an event arrived
  uncorrelated. Note a non-empty but **malformed** wire id is reported verbatim
  too, so the test for "arrived uncorrelated" is `!correlation.Valid(...)`,
  never `== ""`.
- **`eventplane/observe`** is the injectable hook seam, so eventplane observes
  hops without knowing what telemetry is:

  ```go
  package observe

  type Hop string
  const (HopPublish Hop = "publish"; HopConsume Hop = "consume")

  type Event struct {
      Hop           Hop
      Source, Kind, Subject string
      EventID       string        // "" on a publish that failed before minting
      CorrelationID string        // the chain the hop belonged to
      Err           error
      Duration      time.Duration
  }
  func (e Event) Key() string     // routing.Key(Source, Kind, Subject)

  type Hook func(ctx context.Context, ev Event)
  ```

  `outbox.Options` and `consumer.Config` each gain an `Observe observe.Hook`
  (nil by default). The publish hook fires once per `Append` — successes **and**
  failures. The consume hook fires once per delivery with the id the handler's
  context carried (so a minted root is reported, never the empty wire value),
  and is not fired for control frames or unparseable envelopes. A hook must not
  block; eventplane recovers from a panicking hook, and the hook can never
  affect delivery or cursor progress.
- **Consequence appkit must design around:** `observe.Event` deliberately does
  **not** say whether the consume-side correlation id was propagated or minted,
  so the chassis cannot tell a chain's origin from the hook. Chain origin is
  recoverable downstream instead — the earliest record bearing an id is where
  that chain starts.

## The suite contract this design implements

`docs/structured-mcp-design.md` (repo root) is the suite-level design this
appkit work is the root dependency of: structured MCP results as the single
verb surface for agents and machines, caller-asserted identity on loopback,
and the loopback guard narrowed to `X-Forwarded-Proto`. The per-service
adoption and the scripts runtime land through those units' own `project/`
loops and cite that document; this research file exists so appkit's design
does not re-derive the external MCP-spec facts above.
