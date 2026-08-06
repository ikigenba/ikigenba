# telemetry — Research

Collected external ground truth the design consumes. Everything here is a fact
about something **outside this codebase** — the suite telemetry protocol, the
appkit chassis, the eventplane library, the suite registry, and the on-box
layout — recorded precisely so design never has to re-derive or re-negotiate
it. Nothing here is a decision of ours; our decisions live in `design/`.

This is the single current statement of that ground truth, rewritten in place.

---

## 1. The suite telemetry protocol (consumed contract)

The normative home of this contract is the suite-level protocol document
(`docs/telemetry-protocol.md`, authored in the repo-root workspace) together
with `docs/correlation-ids.md`. telemetry **implements the receiving half** of
that contract and owns none of it. The parts telemetry depends on, verbatim:

### 1.1 The record

A record is **allowlist-by-construction**: it contains only fields the
instrumentation explicitly chose. Raw header dumps and raw bodies are never
captured. JSON fields:

| field | meaning |
|---|---|
| `id` | ULID minted by the reporting recorder |
| `time` | RFC3339Nano, UTC |
| `correlation_id` | the chain id; may be empty **only** for `lifecycle` |
| `service` | the recording service's name |
| `kind` | one of `edge`, `request`, `outbound`, `publish`, `consume`, `root`, `lifecycle` — a **closed** vocabulary |
| `actor` | `{owner_email, client_id}`; either field omitted when unknown |
| `op` | operation name, per-kind shape below |
| `params` | structured request/tool arguments, captured under threshold (§1.3) |
| `outcome` | `{status, error, duration_ms, bytes, sha256}` |
| `detail` | small kind-specific extras |

`op` by kind:

- `request` → `mcp:<tool>` or `<METHOD> <path>`
- `outbound` → `<METHOD> <host><path>`
- `publish` / `consume` → the routing key `<source>:<kind>/<subject>`
- `root` → the origin, e.g. `cron:tick/<name>`, `run:<run-id>`
- `lifecycle` → `start` or `stop`
- `edge` → `<METHOD> <original-uri>`

`outcome` carries the **status/error class**, the elapsed time, and the **size +
digest** of the response body when there is one. Response content is never
stored literally — which also keeps show-once secrets out of the store by
construction.

`detail` by kind: `lifecycle` → `{version}`; `edge` →
`{decision: allow|deny|rate_limited, service}`. Other kinds carry none today;
the field is open-ended and small.

### 1.2 The chain id

- Format: a bare **26-character Crockford base32 ULID** (48 bits of millisecond
  timestamp + 80 bits of randomness), e.g. `AGPXX34WA3IGS4MQVE5LMRXK7U`. No
  prefix, no separators, opaque to consumers, time-sortable by construction.
- Header name on the wire: **`X-Correlation-Id`**.
- Minted once at the outermost cause of a chain and propagated verbatim on
  every hop. The edge (nginx + the dashboard's introspection endpoint) strips
  any client-supplied value and substitutes the minted one, so a public caller
  can never inject an id. Ungated public locations clear the header so the
  service mints.
- Because the id is a ULID, records of one chain sort correctly by `time` and,
  on ties, by `id`.

### 1.3 Capture thresholds and digests (contractual constants)

- Digests: **SHA-256, lowercase hex**.
- Per-value literal cap: **1024 bytes** (JSON-encoded).
- Per-record `params` budget: **8192 bytes**; the recorder elides the largest
  values first until the record is under budget.
- An elided value is replaced **in place** by
  `{"$elided": {"bytes": N, "sha256": "<hex>"}}`.
- Params a service declared **sensitive** at tool registration are always
  elided regardless of size.

All of this happens **on the reporting side**, before telemetry ever sees the
record. telemetry stores what it is given; it does not re-apply thresholds. Its
only obligation is to never *widen* what it stores beyond these fields.

### 1.4 The ingest API (the half telemetry implements)

```
POST http://127.0.0.1:<telemetry-port>/ingest        (loopback-only)
Content-Type: application/json

{"records": [ <record>, ... ], "dropped": <int, optional>}

→ 202 Accepted
```

- **Loopback-only.** Never reachable through nginx.
- **Best-effort everywhere.** The reporting side fires and forgets; telemetry
  being unreachable is a dropped record, never an error surfaced to the caller's
  work.
- `dropped` is the count of records the reporter had to discard since its last
  successful batch (buffer overflow or telemetry unavailable), reported on the
  next successful batch.

### 1.5 Reporter-side defaults (context only, not telemetry's contract)

Documented so design understands the traffic shape it receives; telemetry
depends on none of these numbers. The appkit recorder keeps an in-memory ring
buffer of roughly 4096 records dropping oldest, batches at most **256** records,
flushes about once per second or when a batch fills, POSTs fire-and-forget, and
reports its dropped count on the next successful batch. Consequences for
telemetry: batches are small and frequent; records arrive **out of order** and
**late** relative to their `time`; the **same record id may be delivered more
than once** if a reporter retries.

### 1.6 Recursion and self-coverage rules

- The telemetry **ingest path itself is never recorded** — otherwise reporting
  feeds itself.
- telemetry's **own MCP tools** (`search`, `chain`, `get`, `guide`, `health`,
  `reflection`) **are** recorded, exactly like any other service's MCP traffic.
- Every service, telemetry included, emits `lifecycle` records: `start` (with
  version) at boot and `stop` on graceful shutdown. A crash produces no stop
  record; the next start bounds the gap.
- The operator plane (opsctl, repo-root `bin/`) is not recorded; its effects are
  visible as version changes between `lifecycle` starts.

### 1.7 Scope exclusions the protocol makes (so telemetry's coverage claims stay honest)

- Traffic originating **inside sandboxes** (a scripts run's Python internals, a
  prompts run's in-agent internals) is not recorded; the sandbox boundary
  (spawn, suite MCP calls, outcome) is fully recorded, and run archives hold the
  inside.
- Requests **nginx answers without a service** (static it serves directly, the
  parked page) are not recorded.
- LLM conversations are never duplicated into telemetry — prompts already
  archives full run conversations, and the chain id resolves to the run.

---

## 2. The appkit chassis (consumed library)

Module `appkit`, consumed via a committed `replace appkit => ../appkit`. Facts
telemetry builds on, read from the live source:

### 2.1 `appkit.Spec` and `appkit.Main`

A service's `main.go` collapses to `appkit.Main(appkit.Spec{…})`. The chassis
owns the fixed verb set (`serve`/`version`/`manifest`/`migrate`/`schema`),
config-from-env, the forward-only migration runner plus downgrade guard, the
loopback HTTP server with the RFC 9728 PRM route and the identity gate, the
optional `/feed` producer mount, and `manifest.env` emit/parse. Spec fields
telemetry uses: `App`, `Mount`, `Port`, `MCP`, `Migrations`, `MigrationsDir`,
`ManifestExtras` (ordered `[]appkit.ManifestKV`, so the emitted manifest is
byte-stable), and `Handlers func(*Router) error`.

### 2.2 `appkit/server.Router`

```go
func (rt *Router) Handle(pattern string, h http.Handler)
func (rt *Router) HandleFunc(pattern string, h http.HandlerFunc)
func (rt *Router) HandleLoopback(pattern string, h http.Handler) // wraps LoopbackOnly
func (rt *Router) RequireIdentity(h http.Handler) http.Handler
func (rt *Router) DB() *sql.DB          // the shared single-writer SQLite handle
func (rt *Router) Logger() *slog.Logger
func (rt *Router) ResourceID() string
func (rt *Router) Service() string
func (rt *Router) Version() string
func (rt *Router) Health() func(context.Context) (map[string]any, error)
```

`LoopbackOnly` is the guard behind `HandleLoopback`:

```go
// nginx stamps X-Forwarded-Proto on every proxied request, so any non-empty
// value is answered with a bare 404. Identity headers are deliberately ignored
// because legitimate loopback machine callers assert them themselves.
func LoopbackOnly(next http.Handler) http.Handler
```

This is exactly the guard the `/ingest` route needs; telemetry does not write
its own.

### 2.3 `appkit/mcp`

Protocol revision `2025-06-18`. One `Tool` is a wire descriptor plus a handler:

```go
type Tool struct {
    Name         string
    Description  string
    InputSchema  map[string]any
    OutputSchema map[string]any
    Handler      func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error)
}
```

`mcp.New(mcp.Options{Service, Version, Instructions, Tools, Health, Events,
Publishes, Subscriptions})` builds the `POST /mcp` handler. `health` and
`reflection` are **reserved names** supplied by the chassis; a service may not
declare them. Result helpers: `StructuredResult(v)` (emits `structuredContent`
plus a mirrored text block from one typed value), `TextResult(text)` (prose
tools, no output schema), and `ErrorResult(code, msg)` over the closed error
vocabulary `validation`, `not_found`, `conflict`, `too_large`,
`source_unavailable`, `internal`. Input/output schemas must conform to the
strict-client rules `validateToolSchemas`/`conformsToStrictClient` enforce.

### 2.4 The telemetry/correlation seams appkit is growing (cross-workspace dependency)

The appkit workspace's own spec lands **before** telemetry is built. From it,
telemetry consumes, as external facts:

- **A correlation package + middleware** implementing read-or-mint on
  `X-Correlation-Id` (Crockford ULID), with a context accessor. This replaces
  the old mint-fresh-per-hop `RequestIDMiddleware` behavior.
- **A recorder** — ring buffer, batching, the ingest client, env-driven config,
  degrading silently when telemetry is absent — wired into MCP dispatch (kind
  `request`, op `mcp:<tool>`), plain-HTTP request recording, `lifecycle`
  records in `Run`, and the shared instrumented outbound client.
- **An exact-path exclusion list from request recording**:
  `appkit.Spec.TelemetryExclude []string`, which `runServe` passes to
  `server.Options.RecordExclude`. A path on that list produces **zero** request
  records. This is the mechanism `/ingest` uses to satisfy §1.6, and it is a
  **Spec-level list, not a per-route wrapper** — the recorder sits in the
  middleware chain above the mux, so it cannot see per-route registration.
  telemetry consumes the seam; it implements no exclusion of its own, and its
  tests observe the *outcome* (no self-record for the ingest path), not
  appkit's internals.
- **The chassis always excludes `POST /mcp` from the HTTP request recorder**,
  because MCP dispatch already emits the richer per-tool record. Consequence for
  telemetry: one of its own tool calls yields exactly **one** record
  (`op` = `mcp:search`), never a duplicate pair.
- **Recorder flush semantics** (`appkit/telemetry`): the flush loop POSTs
  `{"records":[…]}` with `"dropped": N` added only when the counter is
  non-zero, and **clears the counter only on a 2xx response**. A non-2xx is
  logged at debug and the batch is discarded, never retried. This is why
  telemetry's ingest answers 202 for a partially-rejected batch and 400 only for
  a malformed envelope: a 400 leaves the reporter's dropped counter uncleared,
  so the count is re-reported on its next successful batch rather than lost.
- **The ingest address is derived, not literal**: `Config.TelemetryIngestURL`
  defaults to `registry.BaseURL("telemetry") + "/ingest"`. telemetry's route
  path must therefore be exactly `/ingest` on its loopback port.
- **Seams telemetry deliberately does not use.** `Router.Recorder()
  *telemetry.Recorder` and `Router.HTTPClient(timeout) *http.Client` exist for
  services that emit custom records or make outbound calls, and
  `Recorder.StartRoot`/`StartChain` exist for services that originate chains
  (a cron tick, a poll cycle, a run spawn). telemetry needs none of the three:
  it emits no records of its own, makes no outbound HTTP calls, and originates
  no chain (see D1).
- **The `correlation` package** (`appkit/correlation`, mirrored as a leaf
  package eventplane can import without depending on appkit) exports
  `Header = "X-Correlation-Id"`, `New()`, `Valid(s)`, `WithContext(ctx, id)`,
  `FromContext(ctx)`, and `Ensure(ctx)` (read-or-mint). telemetry calls none of
  them — the chassis middleware handles the header on its behalf — but they are
  the names the header contract is realized under.

telemetry therefore **gets its own instrumentation for free** from the chassis
and writes none of it: its MCP tools are recorded because appkit records MCP
dispatch, and its lifecycle records exist because appkit emits them in `Run`.

### 2.5 eventplane

telemetry is **not** an event-plane participant: it neither publishes (no
`Spec.Feed`) nor consumes (no `Spec.Consumes`). The eventplane workspace's
`correlation_id` outbox column and envelope field matter to telemetry only in
that they are how `publish`/`consume` records get their chain id on the
*reporting* side. No eventplane API is called from this codebase.

---

## 3. The suite registry (consumed library)

Module `registry`, the authoritative `name → loopback port` table.

```go
func Port(name string) (port int, ok bool)
func MustPort(name string) int
func BaseURL(name string) string
```

Ports are grouped in blocks; the `Core` block spans 3000–3099 and holds the
suite's services. The registry workspace appends telemetry's row as part of this
work; telemetry reads its port with `registry.MustPort("telemetry")` and hard-codes
the number nowhere in Go source. (The nginx fragment is the documented exception:
per the app-layout contract a fragment is shipped verbatim and must be directly
loadable by nginx, so its `proxy_pass` carries the literal port — which is why the
fragment's literal is guarded against the registry by a test.)

---

## 4. On-box layout and lifecycle (consumed schema)

From `docs/app-layout.md`, normative:

- `/opt/<service>/` is a private FHS root. `state/` is the **only** thing the
  backup captures; `cache/` is transient and reset on restore; the shipped tiers
  (`bin/`, `libexec/`, `etc/`, `share/`) are reproducible from the deploy bundle
  and are not backed up.
- Therefore: telemetry's SQLite database lives at `state/telemetry.db` and is
  backed up nightly by the ordinary box-level `opsctl backup`, with no
  per-service work. This is the whole of telemetry's durability story.
- `etc/<version>/manifest.env` and `etc/<version>/nginx.conf` are **authored
  in-repo** (`telemetry/etc/…`) and **shipped verbatim** in the bundle; nothing
  substitutes them on the box, so the fragment must be directly loadable by
  nginx and must contain no `server{}`, `listen`, or TLS directive.
- The binary's verb set is exactly `serve`/`version`/`manifest`/`migrate`/
  `schema`; there is no per-binary backup or restore verb.

Migration rules (suite-wide, `CLAUDE.md`): schema lives under
`internal/db/migrations/`, applied forward-only by the appkit runner; new
migrations are minted with `bin/create-migration telemetry <name>` (UTC
timestamped, never hand-numbered) and a committed migration is never edited or
deleted.

---

## 5. The MCP self-description convention (consumed pattern)

From `docs/mcp-discovery-convention.md`, normative pattern:

| tier | channel | guarantee | holds |
|---|---|---|---|
| 0 | `initialize` `instructions` | always loaded, once | orientation + routing vocabulary + one pointer to the guide |
| 1 | tool descriptions | always loaded, per tool | when to use each tool, its args, what it returns |
| 2 | a `guide` tool | only when called | full reference: field catalogs and worked examples |

Reference bulk lives in tier 2, never tiers 0/1. `guide` is a **tool** (not an
MCP resource — most clients do not surface resources), flat, read-only,
input-free, returning plain text, with its document embedded in the binary so it
always matches the running version. The guide is referenced in exactly two
places: the `instructions` string and the `guide` tool's own description.

From `docs/structured-mcp-design.md`, normative: every domain verb declares an
`outputSchema` and returns `structuredContent` plus a mirrored text block, via
`StructuredResult`. Documentation tools (`guide`, `describe`) stay plain text
and declare no output schema. Tool errors carry a code from the closed error
vocabulary so machine callers branch on a code, never on message text.

---

## 6. SQLite facts the design leans on

Driver: `modernc.org/sqlite` (pure Go, no cgo), the suite standard. The DB
handle is appkit's shared single-writer `*sql.DB`.

- `INSERT OR IGNORE` on a `TEXT PRIMARY KEY` is the idiomatic idempotent insert:
  a repeated record id is a no-op, not an error — which is exactly the behavior
  §1.5's at-least-once delivery requires.
- `EXPLAIN QUERY PLAN` reports the index chosen for a statement (`USING INDEX
  <name>` / `SEARCH … USING INDEX`, versus `SCAN <table>`). This is the
  falsifiable substrate for "the search axes are index-backed": a missing index
  shows as a table scan on a real database.
- SQLite compares `TEXT` with `BETWEEN`/`<`/`>` lexicographically, and
  RFC3339Nano UTC timestamps are lexicographically ordered iff they are
  fixed-form. Nanosecond precision is variable-width in Go's `RFC3339Nano`
  (trailing zeros are trimmed), so lexicographic ordering of `time` is **not**
  safe on the raw protocol string — this is a real constraint the storage design
  must handle rather than assume away.
- A composite index `(a, b, c)` serves equality on `a` with ordering by `b`;
  a leading-column-only predicate cannot use an index whose first column is a
  different field. Hence one composite index per search axis, each ending in the
  time/id ordering columns.
- Keyset (seek) pagination over `(time, id)` is stable under concurrent inserts
  in a way `OFFSET` is not — and telemetry receives inserts continuously while an
  agent pages, so `OFFSET` would repeat and skip rows.
- SQLite has no `ILIKE`; `LIKE` is case-insensitive for ASCII by default and
  cannot use an index for a leading-wildcard pattern. A substring filter is
  therefore a post-narrowing scan over the indexed candidate set, not an axis of
  its own.

---

## 7. The reference service shape (consumed idiom)

`webhooks` is the closest existing analogue — a small appkit service with an MCP
table and one non-MCP HTTP surface — and its shape is the idiom telemetry
follows: `cmd/<svc>/main.go` as the composition root declaring the `Spec`;
`internal/<domain>` for the domain; `internal/db` for the store and embedded
migrations; `internal/mcp` for the tool table; `internal/e2e` for the end-to-end
layer; `etc/manifest.env` and `etc/nginx.conf` authored in-repo; `VERSION`
committed. Its manifest and fragment are the concrete templates:

```
APP=webhooks
MOUNT=/srv/webhooks/
DEFAULT=false
PORT=3006
MCP=true
FEED=/feed
```

Its fragment demonstrates the tiering telemetry needs: an exact-match ungated
PRM location, an exact-match `auth_request`-gated `/mcp` that sets each identity
header exactly once from the auth subrequest, an exact-match `return 404` for
`/feed`, and a prefix catch-all `return 404` shielding everything else under the
mount. Telemetry's differences from webhooks are that it has **no** public
ingress tier reachable through nginx, **no** event-plane feed, and **no** landing
page.

---

## 8. The agentkit tool-schema subset (consumed contract)

`github.com/ikigenba/agentkit` is the suite's LLM agent library, consumed today
by `prompts` and `repos`. When a conversation attaches an MCP server, agentkit
fetches `tools/list` and turns each entry into a tool. It reads **only the
tool's `inputSchema`**; `outputSchema` is discarded and never reaches any
validation. Every tool schema is then checked against one canonical subset of
JSON Schema at the `Send` boundary, before any provider call.

The normative home of this contract is agentkit's own design,
`project/design/D34.md` ("The canonical tool-schema subset, owned by root") in
the `github.com/ikigenba/agentkit` repository. What follows is the part
telemetry's design consumes.

### 8.1 Why the subset exists

The subset is **derived, not chosen**: a construct is a member exactly when
every provider dialect agentkit supports (Anthropic, OpenAI, Google, OpenRouter,
zai) can render it faithfully. Enforcement is therefore provider-independent —
the same schema is accepted or rejected identically no matter which model a
conversation is configured with.

### 8.2 The subset

Root must be `"type": "object"`. Within it, permitted:

`type` (a single value), `description`, `title`, `properties`, `required`,
`items`, `enum` (string values only), `const` (string value only), `anyOf`,
`oneOf`, `$ref`/`$defs` (non-recursive), `minLength`, `maxLength`, `pattern`
(RE2-safe: no lookahead or lookbehind), `minItems` (values 0 or 1 only),
`default`, a two-branch nullable union, and `format` restricted to nine
values — `date-time`, `date`, `time`, `duration`, `email`, `hostname`, `ipv4`,
`ipv6`, `uuid`.

Excluded, each because at least one dialect cannot express it: `minimum`,
`maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf`, `maxItems`,
`uniqueItems`, `allOf`, `not`, `if`/`then`/`else`, `patternProperties`,
recursive `$ref`, non-string `enum` or `const` values, **`additionalProperties`
in any authored form**, any `format` outside the nine, `propertyNames`,
`prefixItems`, `contains`, `dependentSchemas`, `unevaluated*`, `$schema`, `$id`,
`$comment`.

`additionalProperties` is excluded specifically because no authored value could
be correct for all three major dialects: Anthropic and OpenAI accept the
keyword, Google 400s on it. It is adapter-owned — each provider adapter injects
or omits it per its own dialect — so a server that authors it is rejected
regardless of the value.

### 8.3 How a violation surfaces

A schema outside the subset fails the whole `Send` with `ErrInvalidConfig`
(matchable via `errors.Is`), leaving conversation history unchanged and issuing
no provider call. The error text names the offending construct and its JSON
Pointer location; for an MCP-sourced tool it attributes the failure as
`<server>.<tool>`. Rejection is **whole-toolset**: one offending tool disables
every tool on the conversation, so a single non-conforming server breaks an
agent's entire tool surface, not just its own.

Nothing is dropped and nothing is warned about. agentkit's `AGENTS.md` states
the policy for the other side of the seam directly: every MCP server it connects
to is first-party software, so "a server whose tool schemas are rejected is
fixed at the server, never accommodated here."

### 8.4 Relationship to appkit's construction-time guard

appkit runs its own advertised-schema guard at `mcp.New`
(`conformsToStrictClient`, §2.3), but its rule is derived from **strict MCP
clients** (Claude Code), not from agentkit. It checks only two things: the root
is `"type": "object"`, and there is no top-level `oneOf`/`anyOf`/`allOf`.

The two rules are independent and **neither contains the other**: agentkit
forbids `minimum` and authored `additionalProperties`, which appkit's guard
permits; strict clients forbid `oneOf`/`anyOf` at any level, which agentkit's
subset permits below the root. Passing appkit's guard is therefore **no
evidence** of agentkit conformance, and a schema must satisfy both.

Widening appkit's guard to carry the agentkit subset suite-wide is appkit's
decision to make in appkit's own spec, outside telemetry's scope boundary.
Until it happens, conformance is each service's own responsibility, proven by
its own tests.

### 8.5 The exported surface a test can drive

agentkit exports no schema validator: the gate (`validateToolSchema`) is
unexported and reachable only through `Send`. It is nevertheless drivable from
outside, because the pieces `Send` needs are exported:

```go
type Provider interface {
    RoundTrip(ctx context.Context, req *Request) *RoundTrip
    Identity() Identity
}

type Conversation struct {
    Provider Provider
    Model    string
    Pricing  *Pricing
    Tools    []Tool
    // ...
}
```

`Provider` has two methods and no unexported members, so a caller outside the
module can implement a stub that records whether it was invoked. A
`Conversation` built over that stub, carrying tools whose `JSONSchema()` returns
the schemas under test, exercises the real gate: `Send` returns a `Stream` whose
`Err()` is `ErrInvalidConfig` on a violation, and the stub's `RoundTrip` is
never called. On a conforming tool set, `Err()` is nil and `RoundTrip` runs.

`Tool` is an interface but carries a **sealing method**, so it cannot be
implemented from outside directly:

```go
type Tool interface {
    Name() string
    Description() string
    JSONSchema() json.RawMessage
    Call(ctx context.Context, input json.RawMessage) (string, error)
    isTool() // unexported: seals the interface
}
```

The seal is satisfied by **embedding the interface** in a struct that overrides
the four exported methods — a nil embedded `agentkit.Tool` supplies `isTool()`,
and nothing on the validation path ever calls it. agentkit's own external test
package (`package agentkit_test`, `tool_schema_test.go`) does exactly this to
feed raw schemas to the gate, which proves the technique works from outside the
module.

This matters because the exported constructor `NewTool` builds a schema by
reflecting a Go input struct and **panics** on anything it cannot map into the
subset. It therefore cannot carry an arbitrary hand-authored schema to the gate;
the embedding form is the only way to submit a schema verbatim.

The module is versioned by annotated git tags only (`vMAJOR.MINOR.PATCH`); the
current release is `v0.17.0`.
