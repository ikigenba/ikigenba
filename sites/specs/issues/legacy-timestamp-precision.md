# Legacy timestamp precision conflicts with returned times

Filed during the human-invoked sites build-spec run starting at `02f7959d44321298366569051965a9f233befb9b`. The store verifier reported this conflict and a fresh independent verifier confirmed it.

In `specs/design/D04-store.md`, R-WEVU-4WL2 accepts legacy `Created` and `Published` strings in the form `time.Time.MarshalJSON` writes and requires returned fields to be equal under `time.Time.Equal` to the times those strings spell. R-2K28-H01H requires every successfully returned site's relevant timestamps to have location `time.UTC` and zero nanoseconds. R-1VO8-TL7L requires a healthy `List(ctx, owner)` to return nil error and every matching site, so rejecting a successful read does not resolve the conflict.

A valid legacy row can have both timestamps `2026-10-01T12:34:56.000000123Z`, with otherwise valid fields and matching relational columns, and an empty settings table. Standard `time.Time.MarshalJSON` emits this timestamp. Preserving its instant under `Equal` requires nanoseconds 123; returning zero nanoseconds changes the instant. UTC normalization preserves the instant but does not remove the fraction. No acceptance condition in R-WEVU-4WL2 excludes this input.

This witness is reachable using the permitted test seam: create the legacy schema and payload through a test-owned handle's `DB.Write`, remove its migration ledger, reopen with `sites.Migrations()`, and call `store.List`. Existing whole-second adoption fixtures cannot distinguish the conflicting requirements. The surrounding prose's whole-second convention does not narrow the explicit legacy acceptance conditions.

The build cannot satisfy both requirements without changing a read-only contract. Suggested resolution: restrict accepted legacy timestamps to whole seconds, or exempt preserved legacy fractional timestamps from the universal zero-nanosecond return rule. Replace the changed requirement with a newly minted id in a human-authorized design run.
