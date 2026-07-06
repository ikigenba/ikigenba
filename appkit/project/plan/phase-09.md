# Phase 09 — Standard tools: `health` and `reflection` in `appkit/mcp`

*Realizes design Decision 9 (chassis-owned standard tools). Depends on
Phase 08 (the transport).*

`appkit/mcp.New` auto-registers the `health` and `reflection` tools from its
`Options`: `health` renders `server.Envelope(version, service, details)` with
details from `Options.Health` (nil → `{}`); `reflection` renders the
event-graph index from `Options.Publishes()` (preferred) else `Options.Events`
plus `Options.Subscriptions`, serves a registered type's detail schema on
`event_type`, and answers an unknown `event_type` with an `isError` result
naming the known types. Both appear in `tools/list` alongside declared tools;
the reserved-name rejection from D8 now guards real registrations.

**Done when:** the suite is green (design Conventions commands, from `appkit/`)
and R-ML2U-CBQM, R-MMAQ-Q3HB, R-MNIN-3V80, and R-MOQJ-HMYP are each covered by
a clearly-named test in `appkit/mcp` driving the real `ServeHTTP` seam with
real `outbox.Registry`/`consumer.Subscription` values, genuinely asserting the
behavior its D9 Verification line describes.
