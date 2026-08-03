# eventplane — Product

**Authority: intent.** This document owns the *why* of the current work — who
it serves, what is in and out of scope, and what we promise — stated in
outcome terms only. It does not state mechanism, formats, exit codes, or test
assertions; those belong to design. Product states the promise; design states
the exact, checkable proof of that promise.

## Problem

Two gaps in how the suite's event plane addresses and accounts for its events.

**Addressing.** Events were addressed by a flat `type` string from a closed
per-service list, and the *thing the event happened to* — a file path, a
schedule name, a prompt name — was buried in the payload where selectors could
not see it. A prompt that cares only about PDFs landing under `/bills/` had to
be triggered by *every* dropbox file event on the box and burn a full agent run
discovering each one was not for it. Only cron escaped this, by baking its
subject into the type — which in turn broke the closed-list model and forced
consumers to special-case it. Routing precision existed for one producer and by
accident.

**Accountability.** Work in this suite crosses service boundaries by
*publishing a fact*, not by calling an API: a user action lands in one service,
which publishes, and a second service reacts minutes later in a different
process. Nothing carries the identity of the originating action across that
hop, so the causal thread breaks exactly where it is hardest to reconstruct by
hand. A service that wants to account for the hop — to record that this event
was published, that this handler ran and how it ended — has nowhere to stand:
the plumbing that knows those facts is a library, and the machinery that
records them is the application chassis that *depends on* this library, so the
library cannot reach for it.

## Purpose

`eventplane` is the suite's shared event-plane library (producer `outbox` +
consumer engine `consumer`). It gives every event a producer-chosen **routing
address** — what happened, and to what — with one shared way to match against
it, so a consumer fires for exactly the events it cares about and nothing else.
It also carries the **correlation id of the work that caused each event**
across the hop as a first-class envelope fact, and exposes an **observation
seam** so whatever wants to account for publish and consume hops can, without
the library depending on it.

## Users

Suite service developers (and the build loops acting for them): producers that
publish events through `outbox`, consumers that receive them through
`consumer`, services that validate or evaluate event selectors (e.g. a trigger
filter) and need the one shared matcher, and the application chassis that
observes and records what crosses the plane.

## Scope

This library covers event **addressing and selection**, the **correlation of
events with the work that caused them**, and the **observation seam** for
publish and consume hops. The correlation id is an envelope fact and never a
payload convention; the library is its carrier, never its interpreter — it
neither stores, searches, nor reports on what it carries.

The library also owns the one home for the suite's correlation-id primitives —
the header name, the id format, and the request-scoped accessor — because the
chassis and the services that need them all sit downstream of this library and
must share exactly one of each.

Nothing else about the event plane changes: delivery semantics (ordering,
at-least-once, stall/skip), cursors and resync, retention, backoff, and the
outbox's atomicity all stand exactly as built. Filters select on the routing
address only — never on payload contents. Observation is passive: it can never
change what is delivered, in what order, or whether it is delivered at all.
Each consuming service adopts these surfaces in its own spec; this library
ships only its own package surfaces and the shared schema constants.

## Contractual constants

These come from the suite addressing model
(`docs/event-routing-design.md`) and the suite correlation standard
(`docs/correlation-ids.md`), and are promised verbatim:

- An event is addressed by `source` (the producing service), `kind` (the fact
  class, lowercase `[a-z0-9_.-]+`), and `subject` (either empty or a
  `/`-rooted, `/`-separated routing path).
- The canonical routing key is `source + ":" + kind + subject` — e.g.
  `dropbox:create/bills/aws/2026-06.pdf`, `cron:tick/bill-sweep`,
  `ledger:recorded`.
- Selection is one glob over the whole key: `*` matches within a path segment
  (never across `/`), `**` crosses segments, `?` matches one character,
  character classes as in standard glob, no brace expansion.
- A correlation id is a bare 26-character Crockford base32 ULID: 48 bits of
  millisecond timestamp followed by 80 bits of cryptographic randomness. It is
  opaque — nothing parses it.
- The correlation id travels between processes in the header
  `X-Correlation-Id`, and on the event plane in the envelope field
  `correlation_id`.

## What we promise (user-facing behavior)

- A producer publishes an event addressed by kind and subject; the address
  travels with the event and is visible to every consumer.
- A consumer selects events with a single glob against the canonical key —
  `dropbox:create/bills/**/*.pdf` fires for exactly the PDF files under
  `/bills/`, at any depth, and for nothing else; `ledger:recorded` matches a
  subjectless event literally.
- Filter semantics are identical everywhere in the suite, because there is
  exactly one matcher and one key rendering, both owned by this library —
  no service renders or parses keys itself.
- A producer declares its vocabulary as families (kind + subject shape +
  payload sample); reflection describes those families, and a proposed filter
  can be checked against them — a filter that could never match anything the
  source emits is rejected with the declared vocabulary in hand. cron stops
  being a special case: dynamic subjects are ordinary.
- A malformed address never enters the plane: publishing with an invalid kind
  or subject fails loudly at the producer.
- An event published from inside correlated work carries that work's
  correlation id, and the producer does nothing to make that happen beyond
  passing along the context it already has. The id is never something the
  producer packs into its payload.
- A consumer's handler runs inside the same correlated work that published the
  event: whatever it publishes, calls, or spawns in turn belongs to the same
  chain, again without the handler doing anything to arrange it.
- An event that arrives carrying no correlation gives its handler a fresh
  chain of its own, so downstream work is never orphaned — and two such events
  never share a chain by accident.
- Anything that wants to account for the plane can be handed a single callback
  and will be told about every publish and every consume: what the event was,
  which chain it belonged to, how it ended, and how long it took. The library
  needs to know nothing about what is listening.
- Observation cannot hurt delivery. A listener that is slow, that fails, or
  that crashes outright changes nothing about which events are delivered, in
  what order, or whether the cursor advances.

## Success criteria (outcomes)

- Publishing an event with kind `create` and subject
  `/bills/aws/2026-06.pdf` from source `dropbox` yields an event whose
  observable address is `dropbox:create/bills/aws/2026-06.pdf`, end to end
  from producer append to consumer delivery.
- A consumer filtering with `dropbox:create/bills/**/*.pdf` receives the
  matching event above and does not receive `dropbox:create/notes.txt` or
  `dropbox:delete/bills/aws/2026-06.pdf`.
- A subjectless event (`ledger:recorded`) is selectable by its literal key.
- Asking a producer's reflection surface describes each family it emits, with
  a payload schema and worked example that agree with each other.
- Validating the filter `dropbox:delete/**` against a producer that only
  declares `create` reports that it cannot match; validating
  `dropbox:create/**` reports that it can.
- Publishing with an uppercase kind, or a subject that is neither empty nor
  `/`-rooted, is refused with an explanatory error.
- An event published from within correlated work arrives at its consumer
  reporting the same correlation id the publishing work had.
- A handler that publishes a further event produces an event carrying the
  *original* correlation id — the chain survives the hop, and survives a
  second hop the same way.
- An event published with no correlation in scope still gives its handler a
  well-formed chain id, and two such events give two different ones.
- A listener wired to a producer and to a consumer sees one publish record and
  one consume record per event, each reporting the event's address, its chain
  id, how it ended, and how long it took — including the failures, not only
  the successes.
- A listener that panics on every single call changes nothing a consumer sees:
  the same events arrive, in the same order, and progress is retained across a
  reconnect exactly as without it.
- The event plane's delivery behavior is otherwise unchanged: events still
  arrive in order, at least once, with the same recovery behavior as before.
