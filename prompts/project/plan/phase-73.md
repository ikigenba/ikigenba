# Phase 73 — Suite discovery rewritten: the peer set and the live catalog

*Realizes design Decision 6 (peer set + catalog) and the Decision 45 slice
R-P5DX-Y02K. Depends on Phase 72.*

Rewrite `prompts/internal/suite` to D6's shape: `Peer{Name, BaseURL, Headers}`,
`Discover` returning `[]Peer` (inventory-driven, self-excluded,
registry-addressed, identity trio + conditional `X-Correlation-Id` in
`Headers`, bare service names, no `agentkit.MCPServer` anywhere in the
package), and `Catalog` (concurrent `Instructions` fan-out through the D60
client's seam, first-sentence summaries, empty-summary rows for peers that do
not answer, deterministic order, never fails). The package no longer imports
`agentkit`.

**Done when:** R-ORZ1-QIWX (peer set + headers), R-OT6Y-4ANM (first-sentence
summaries), R-OUEU-I2EB (down peer keeps its row, no error), and R-P5DX-Y02K
(correlation header present/absent by argument) are each covered by a test
tagged with its id; `grep -rn "MCPServer" internal/suite --include='*.go'`
returns no matches; and the suite is green (design Conventions).
