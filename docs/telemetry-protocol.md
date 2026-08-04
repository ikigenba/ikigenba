# Telemetry protocol

This document is the suite-wide normative contract for forensic telemetry
records. Implementing workspaces consume this contract; they do not restate it
normatively.

## Record shape

A forensic record carries only the explicitly chosen fields below:

- `id`: recorder-minted ULID.
- `time`: RFC3339Nano UTC timestamp.
- `correlation_id`: the suite correlation id for the causal chain.
- `service`: emitting service name.
- `kind`: one of the seven record kinds.
- `actor`: `owner_email`, `client_id`, or omitted when unknown.
- `op`: kind-specific operation string.
- `params`: structured request parameters captured under the limits below.
- `outcome`: `status`, `error`, `duration_ms`, `bytes`, and `sha256`.
- `detail`: kind-specific structured detail.

Raw header dumps and raw bodies are never captured. Response content is never
stored; records keep outcome, size, and digest only. Digests are SHA-256 encoded
as lowercase hexadecimal.

## Kinds

The seven record kinds and their `op` idioms are:

- `edge`: `<METHOD> <original-uri>`, with `detail.decision` equal to `allow`,
  `deny`, or `rate_limited`.
- `request`: `mcp:<tool>` or `<METHOD> <path>`.
- `outbound`: `<METHOD> <host><path>`.
- `publish`: the routing key `<source>:<kind>/<subject>`.
- `consume`: the routing key `<source>:<kind>/<subject>`.
- `root`: the origin, for example `cron:tick/<name>` or `run:<run-id>`.
- `lifecycle`: `start` or `stop`, with `detail.version`.

## Parameter capture

Structured params are recorded per field. A JSON-encoded value of 1024 bytes or
less is recorded literally. A value over that cap, or a value declared sensitive
at tool registration, is replaced in place by:

```json
{"$elided":{"bytes":N,"sha256":"<hex>"}}
```

The per-record `params` budget is 8192 bytes. When the record exceeds that
budget, the largest values are elided first until the encoded params are under
budget.

## Correlation

There is one id per causal chain: a bare 26-character Crockford-base32 ULID in
the `X-Correlation-Id` header, which is the product's contractual HTTP
transport constant.

The chassis reads an incoming id or mints one. The edge strips any
client-supplied id and mints a new one: the introspection endpoint mints for
gated routes and returns the id in its auth response, fragments overwrite
anything client-supplied, and ungated public locations blank the header so the
service mints. Self-originated work mints a root.

The id rides the event envelope as a first-class field. Outbound propagation is
loopback-peers-only and is never sent to third parties.

## Ingest

Send records to:

```text
POST http://127.0.0.1:<telemetry-port>/ingest
```

The telemetry port is `registry.MustPort("telemetry")`. The loopback-only
request body is:

```json
{"records":[...],"dropped":0}
```

`dropped` is optional. The endpoint answers `202`.

Delivery is best-effort: bounded buffering, batching, and fire-and-forget. A
down or slow sink drops records and never blocks, errors, or crashes a sender.

## Recursion boundary

The ingest path itself is never recorded. The telemetry service's own MCP tools
are recorded like any service's tools.

## Lifecycle

Every service emits `lifecycle` records at start, including version, and at
graceful stop. This includes the telemetry service itself.

## Scope exclusions

The telemetry contract excludes operator tooling, sandbox internals, and
nginx-answered requests that reach no service.
