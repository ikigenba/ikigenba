# Phase 42 — The `calls` and `usage` MCP tools

*Realizes design Decision 32 (inspection and reporting tools). Depends on Phase 37.*

Add the two domain tools to the `internal/mcp` tool table under the D17/D27 conventions (StructuredResult, typed error codes, output schemas): `calls` (filtered/paginated metric list; `call_id` detail with bodies while retained, `bodies_pruned` marker after; `not_found` on unknown id) and `usage` (aggregation buckets by name/origin/model/day with class and time filters). Identity-gated by the chassis, never row-filtered.

**Done when:** the suite is green and these ids are covered by tagged tests:

- R-6DJD-SKVZ — filtered list matches seeds; list rows carry no bodies
- R-6ERA-6CMO — detail bodies present until pruned, then metrics + `bodies_pruned: true`
- R-6FZ6-K4DD — usage by name sums match seeds
- R-6H72-XW42 — usage by origin separates user/trigger/service buckets
- R-6IEZ-BNUR — rows of other owners and ownerless rows are returned (no row filtering)
