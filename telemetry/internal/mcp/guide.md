# Telemetry forensic records

Every record has an `id`, reporter-supplied `time`, `correlation_id`, `service`, `kind`, actor, `op`, `params`, `outcome`, and `detail`. The actor can carry `owner_email` and `client_id`. `params` holds operation metadata, `outcome` holds status, error, duration, byte count, and SHA-256 digest, and `detail` holds kind-specific metadata.

The seven kinds and their operation shapes are:

- `edge`: an incoming edge request; `op` is `METHOD /path`.
- `request`: a handled HTTP or MCP request; `op` is `METHOD /path` or `mcp:tool`.
- `outbound`: an outbound HTTP call; `op` is `METHOD service/path`.
- `publish`: an event publication; `op` is the event topic.
- `consume`: an event consumption; `op` is the event topic.
- `root`: creation of a causal root; `op` names the initiating operation.
- `lifecycle`: a service start or stop; `op` is `start` or `stop`.

Values removed at capture time appear as `{"$elided": {"reason": "sensitive"}}`. Request and response bodies and conversations are never stored; only bounded forensic metadata and content digests are retained.

## Worked examples

Basic: call `search` with `since`, `kind: "request"`, `status: "error"`, and `op_contains: "mcp:"` to find a failing MCP call in the last hour, then call `get` with its `id` to inspect the full record.

Advanced: take that record's `correlation_id`, call `chain` with `correlation_id` to follow the request through services, then call `search` with its `sha256` to find where the same bytes appeared elsewhere.
