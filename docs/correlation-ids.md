# Correlation ids

The suite-wide standard for correlating work across services back to the user
action that caused it. The telemetry protocol in `docs/telemetry-protocol.md`
uses this document as the current statement of id shape and propagation.

## Shape

A correlation id is a **bare suite ULID**: 26 characters, Crockford base32,
encoding 48 bits of millisecond timestamp followed by 80 bits of cryptographic
randomness (the format `prompts/internal/ids.NewULID` mints, e.g.
`AGPXX34WA3IGS4MQVE5LMRXK7U`). No prefix, no separators, no internal structure
consumers may parse. Crockford base32 is the sole allowed alphabet. The id is
opaque, unique, and time-sortable by construction.

## Semantics

- **Minted once, at the initial user action.** The id is created at the
  outermost cause of a causal chain — the user's MCP call, web request, or a
  trigger firing on the user's behalf — never mid-chain. Everything the chain
  touches carries the same id.
- **Propagated verbatim.** A service that receives a correlation id passes it
  on unchanged to any downstream work it causes. Re-minting mid-chain severs
  the trail and is always wrong.
- **Transported over HTTP as `X-Correlation-Id`.** The header value is exactly
  the bare 26-character ULID, with no wrapper or prefix. Outbound propagation is
  for loopback peers only and is never sent to third parties.
- **Carried on events as an envelope field.** The older event-payload-field
  convention is superseded by the event envelope's first-class
  `correlation_id` field. The existing wiki-to-prompts `group_id` adopter is
  re-homed by those services' own specs rather than by this standard.
- **Edge strips and mints.** A public caller can never supply the id. The edge
  strips any client-provided `X-Correlation-Id` and mints for gated routes; for
  ungated public locations it blanks the header so the service mints.
- **Durable-root reuse.** When the chain is rooted at a durable entity that
  already has a suite ULID — an ingest job, a run — that entity's own id **is**
  the correlation id; do not mint a second one. Mint fresh only when no durable
  root exists (e.g. a one-shot ask).

## Adoption notes

- New adopters need no registration. Mint with the shared ULID shape and
  propagate through `X-Correlation-Id`, event envelopes, and service-native job
  records as appropriate.
- If a service lacks a ULID minter, copy the 26-character time-plus-random
  Crockford-base32 construction rather than inventing a variant.
