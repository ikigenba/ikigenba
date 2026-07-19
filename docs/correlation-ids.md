# Correlation ids

The suite-wide standard for correlating work across services back to the user
action that caused it. First consumer: wiki's calls to prompts (`group_id`);
intended to spread to any field that ties downstream work to its initiating
action (event-plane payloads, job records, future tracing).

## Shape

A correlation id is a **bare suite ULID**: 26 characters, Crockford base32,
encoding 48 bits of millisecond timestamp followed by 80 bits of cryptographic
randomness (the format `prompts/internal/ids.NewULID` mints, e.g.
`AGPXX34WA3IGS4MQVE5LMRXK7U`). No prefix, no separators, no internal structure
consumers may parse — it is opaque, unique, and time-sortable by construction.

## Semantics

- **Minted once, at the initial user action.** The id is created at the
  outermost cause of a causal chain — the user's MCP call, web request, or a
  trigger firing on the user's behalf — never mid-chain. Everything the chain
  touches carries the same id.
- **Propagated verbatim.** A service that receives a correlation id passes it
  on unchanged to any downstream work it causes (a prompts `group_id`, an
  event payload field, a spawned job). Re-minting mid-chain severs the trail
  and is always wrong.
- **Durable-root reuse.** When the chain is rooted at a durable entity that
  already has a suite ULID — an ingest job, a run — that entity's own id **is**
  the correlation id; do not mint a second one. Mint fresh only when no durable
  root exists (e.g. a one-shot ask).

## Adoption notes

- prompts' `calls.group_id` accepts any opaque string; a correlation id per
  this standard is the intended value.
- wiki (the first adopter): pipeline calls carry the ingest job's id; each ask
  mints one fresh id shared across that ask's whole fan-out.
- New adopters need no registration — mint with the shared ULID shape and
  propagate. If a service lacks a ULID minter, copy the 26-char
  time+random Crockford-base32 construction rather than inventing a variant.
