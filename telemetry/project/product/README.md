# telemetry — Product

**Authority: intent.** This doc owns *why* the telemetry service exists, *for
whom*, what is in and out of scope, and the user-facing promises — stated once,
in outcome terms. It does **not** state mechanism, exact paths, wire formats,
schemas, exit codes, or test assertions; those belong to `project/design/`.
Where the two could overlap (observable behavior), product states the *promise*
and design states the **exact, checkable proof** of that promise. This boundary
is load-bearing and keeps product, design, and plan from restating each other.

## Problem

The suite is many small services on one box, tied together by an event plane, a
front-door auth chain, and agents that call across service boundaries on a
user's behalf. When something goes wrong — a notification that never arrived, a
script that ran twice, a bill that came out wrong, a webhook that seemed to
vanish — there is **no way to reconstruct what actually happened**. Each service
logs into its own text stream, in its own words, with no shared identity for the
work in flight. Answering "who did what, when, in what order" means reading N
log files by hand, guessing at timestamps, and hoping the interesting hop was
logged at all. In practice the question goes unanswered.

Worse, the obvious fix — log more — makes things worse, not better: dumping
request bodies, response bodies, and headers into a store on a single box means
duplicating customer content, duplicating LLM conversations that are already
archived elsewhere, and turning the forensic store into the largest
concentration of secrets on the machine.

## Purpose

telemetry is the suite's **forensic record store**. Every service reports the
*skeleton* of the work it does — who asked, what operation, when, how long, how
it ended — tagged with a shared id for the causal chain it belongs to.
telemetry keeps those records for a retention window and gives a forensic agent
three ways to ask about them: search across them, follow one chain end to end,
or read one record in full. It does exactly that one job: **make any sequence of
events in the suite reconstructible after the fact, without becoming a copy of
the suite's data.**

## Users

- **A forensic agent** (the owner's agent, working an incident or a "why did
  that happen" question) is the only direct user of the service's surface. It
  searches, follows a chain, and reads records, then explains what happened.
- **Every other suite service** is a *reporter*, not a user: services hand
  telemetry their records over a private on-box path and never read them back.
  Reporting is something the shared chassis does on their behalf; a service
  author does not call telemetry.
- **The operator**, indirectly: deploys, restarts, and crashes become visible in
  the record stream as service lifecycle events, so "when did this service last
  restart, and on what version" is answerable from the same place as everything
  else.

## Scope

telemetry **does**:

- **Accept records from services on the box**, in batches, over a private path
  that is never reachable from outside the box. Acceptance is best-effort by
  design: a reporting service is never blocked, slowed, or failed by telemetry.
- **Store the skeleton of each recorded step** — when it happened, which service
  did it, what kind of step it was, who it was on behalf of, which operation,
  how it ended, how long it took, and the size and digest of any body involved.
- **Tie steps into chains.** Every step of one causal chain carries the same
  chain id, so the whole chain — across every service it touched — can be
  recovered as one time-ordered sequence.
- **Let an agent search** the records by time range, service, kind of step,
  actor, operation, outcome, and content digest, returning a bounded page at a
  time with a way to ask for the next.
- **Let an agent follow one chain** by its id and get the complete, time-ordered
  sequence of steps across all services.
- **Let an agent read one record in full**, by that record's id.
- **Teach an agent its own record model** on demand — the kinds of step, what
  each field means, and worked examples — so the surface is usable without an
  external doc.
- **Keep records for a bounded retention window** and delete them once they age
  out, so the store's size is bounded on a single box. The window is
  operator-configurable, with a sane default.
- **Report service lifecycle** — starts (with the running version) and graceful
  stops — so restarts, deploys, rollbacks, and crashes are all reconstructible
  from the record stream. A crash leaves no stop; the next start bounds the gap.
- **Account for what it lost.** When a reporting service had to discard records
  (because telemetry was down or its buffer overflowed), the loss is recorded as
  a count, so an agent reading the store can tell "nothing happened" apart from
  "we didn't see what happened".
- **Record its own agent-facing surface** like any other service's — a search or
  a chain lookup is itself a recorded step — while the private reporting path is
  never recorded, so reporting can never feed itself.
- **Serve the suite's uniform landing page** at its mount to a signed-in
  browser: the service's name, what it is in one line, and the running version —
  the same page every other suite service shows, differing only in its text.
  The working surface remains agent-only; the page identifies the service, it
  does not operate it.

telemetry **does nothing else**. In particular, it deliberately excludes:

- **Content.** Request bodies, response bodies, raw headers, and LLM
  conversations are never stored. Arguments are captured only when small, and
  anything bulky is represented by its size and digest instead. A response is
  recorded by how it ended and how big it was, never by what it said. This is
  what keeps the store from becoming a second copy of customer data and a
  concentration of secrets.
- **Analytics.** No aggregation, rollup, percentile, dashboard, chart, alert, or
  trend surface of any kind. telemetry answers "what happened", never "how is it
  trending". Anything of that shape is a separate product decision, not a
  missing feature here.
- **Mutation.** The agent-facing surface is read-only. There is no way to
  delete, edit, redact, or annotate a record through it; the only thing that
  removes a record is the retention window expiring. Records are facts, and a
  forensic store an agent can edit is not evidence.
- **Alerting or reaction.** telemetry publishes nothing on the event plane,
  triggers nothing, and notifies no one. It is a store that answers questions.
- **A human UI.** Beyond the uniform landing page, there is no browser-facing
  surface: no record browser, no query form, no page that operates the service.
  The working surface is agent-only.
- **Cross-box or historical import.** It records what happens on this box from
  the moment it is running. There is no backfill and no ingestion of other
  boxes' records.

## Contractual constants

These values are promised suite-wide and are used verbatim; design must not
re-declare or redefine them.

- **Service name `telemetry`**, mounted at **`/srv/telemetry/`** by the suite's
  service-name convention.
- **Starting version `v0.1.0`** — the service's first committed `VERSION`, per
  the suite convention for a brand-new deployable app.
- **Default retention window: 90 days**, overridable by the operator through the
  service's configuration.

The service's loopback port is **owned by the suite registry**, not by this
document: telemetry takes its port from the registry by name and promises
nothing about the number. The record model itself — the field set, the kinds of
step, the capture thresholds, the digest algorithm, and the chain-id format —
is the **suite telemetry protocol's**, consumed as an external contract and
documented in `project/research/research.md`.

## What we promise (user-facing behavior)

- **A reporting service is never harmed by telemetry.** If telemetry is stopped,
  slow, or broken, nothing else on the box fails, blocks, or errors because of
  it. The cost of telemetry being down is *missing records*, never an outage.
  This promise outranks completeness: dropping a record is always preferable to
  delaying the work that produced it.
- **What is accepted is durable.** Once telemetry has accepted a batch of
  records, those records survive a restart of the service and are findable
  afterwards. Records live in the service's own durable state and are captured
  by the suite's ordinary nightly backup, like every other service's data.
- **A chain is complete or honestly bounded.** Asking for a chain id returns
  *every* record telemetry holds for that chain, from every service that
  participated, in the order they happened. Where a chain reaches back past the
  retention window, the answer says so rather than silently presenting a partial
  chain as a whole one.
- **Search finds records along the axes that matter in an incident.** An agent
  can narrow by when, by which service, by the kind of step, by who it was for,
  by which operation, by how it ended, and by the digest of a body — and combine
  those, getting a bounded page plus a way to ask for the next one. The same
  bytes appearing in two different services are findable as such.
- **Any record can be read in full.** A record found in a search or a chain can
  be fetched by its id and returns everything telemetry kept about that step —
  which is the skeleton, and never the content.
- **The store never contains bodies.** No recorded step carries a request body,
  a response body, a raw header dump, or an LLM conversation. Large arguments
  appear as a size and a digest in place of their value, and arguments a service
  declared sensitive appear that way regardless of size.
- **Nothing outside the box can write to it, and nothing outside can reach the
  reporting path.** Records enter only from processes on the same machine. A
  request that came through the front door can never inject a record.
- **The forensic surface is itself forensic.** An agent's own searches and chain
  lookups appear in the record stream like any other service's calls, so "who
  went looking, and when" is answerable too. The private reporting path is the
  single exception and is never recorded.
- **Loss is visible, not silent.** If records were discarded before telemetry
  saw them, the store can tell an agent how many and from which service — an
  investigator is never quietly shown an incomplete picture as a complete one.
- **Old records disappear on schedule.** Records age out at the configured
  retention window without operator action, and the window is a plain setting
  with a 90-day default.
- **The surface explains itself.** A connecting agent can learn the record
  model, the kinds of step, and how to drive each tool from the service itself,
  with no external skill or document installed.
- **The mount identifies itself to a signed-in browser.** Visiting the
  service's mount while signed in to the box shows the suite's uniform landing
  page — the service's name, a one-line description, and the running version —
  visually identical to every other service's page apart from its text. A
  visitor who is not signed in is sent to sign in, not shown the page.

## Success criteria (outcomes)

Each item is confirmable end-to-end against the running service.

- With telemetry stopped, every other service on the box continues to work
  normally: calls succeed, nothing errors or hangs because telemetry is absent.
- A batch of records submitted from a process on the box is acknowledged, and
  every record in it is afterwards findable through search — including after
  telemetry is restarted.
- A batch containing a malformed record still stores the well-formed records in
  it, and the response says how many were rejected; a submission that is not a
  well-formed batch at all is refused outright and stores nothing.
- A submission attempted from outside the box (through the front door) is
  refused and stores nothing.
- Given a chain id spanning several services, asking for that chain returns all
  of its records from all of those services, ordered by when they happened, and
  states the retention boundary so a truncated chain is recognizable as one.
- Asking for a chain id that has no records returns an empty answer rather than
  an error.
- A search narrowed by time range, service, kind, actor, operation text,
  outcome, or digest returns exactly the records matching all of the given
  constraints and none that miss any of them; asking again with the returned
  continuation yields the next page with no record repeated or skipped.
- Two records from different services carrying the same body digest are both
  returned by a digest search.
- A record id taken from a search result can be read back in full; an
  unrecognized record id reports "not found" rather than failing opaquely.
- No record returned by any of the three tools contains a request body, a
  response body, a raw header dump, or conversation text; oversized and
  sensitive arguments appear as a size and digest in place of the value.
- The agent-facing surface offers only searching, chain following, reading a
  record, and the self-describing guide plus the standard chassis tools — there
  is no tool that deletes, edits, redacts, annotates, or aggregates.
- Calling one of telemetry's own agent tools produces a record for that call;
  submitting a batch on the private reporting path produces no record about the
  submission itself.
- After a service starts and is stopped gracefully, its start (naming the
  running version) and its stop are both findable; after a service is killed
  without a graceful stop, the start is findable and no stop exists for it.
- When a reporting service discarded records before telemetry received them, the
  store can report the discarded count, attributed to that service.
- A record older than the configured retention window is gone from the store
  without any operator action, while one inside the window remains; with no
  configuration set, that window is 90 days.
- A connecting agent that reads only what the service itself provides can
  correctly perform a search, follow a chain, and read a record, without any
  externally installed skill or document.
- Visiting the service's mount in a signed-in browser shows the suite's uniform
  landing page naming the service and the running version; visiting it signed
  out leads to sign-in instead.
