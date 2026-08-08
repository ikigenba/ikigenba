# Phase 38 — `repos` joins the trigger sources

*Realizes design Decision 39 (repos as a trigger source).*

`knownFamilies` gains `"repos": {"push"}` and `scriptsSpec()` gains a sixth
`scriptsConsumerEntry("repos")`; nothing else changes — the fan-out, the
subscription shape, and the matcher are already generic. Because the known-source
set and the consumer count are asserted by tests already in the tree, this phase
also updates those existing assertions (`R-7UZ2-4KOT`'s six-source rejection
message, `R-8WN1-0VQI`'s per-source entry list, and the manifest `CONSUMES=`
expectation behind `R-8IAN-FB87`) so they state the current set.

**Done when:** the suite is green — which requires the pre-existing
`R-7UZ2-4KOT`, `R-8WN1-0VQI` and `R-8IAN-FB87` tests to be updated to the
six-source set rather than deleted — and each of these ids is covered by a
genuine test:

- R-2TS6-VE9D — `repos:push/…` filters validate, `repos:nosuchkind/**` is
  rejected, and the unknown-source error names all six sources and not
  `scripts`.
- R-2V03-9602 — `scriptsSpec()` declares exactly the six consumer entries with
  `"**"` subscriptions, and `ScriptsForEvent` routes a `repos:push/sites/blog`
  key to the subject-matching trigger only.
