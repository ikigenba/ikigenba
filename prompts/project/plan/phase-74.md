# Phase 74 — The gateway tools package

*Realizes design Decision 19, slice R-OWUN-9LVP, R-OY2J-NDME, R-OZAG-15D3,
R-P0IC-EX3S, R-P1Q8-SOUH. Depends on Phase 72 and Phase 73.*

Build `prompts/internal/gateway`: the three `agentkit.NewTool`-constructed
tools of D19 — `suite_services` (D6 catalog as JSON), `suite_tools` (live
per-service instructions + verbatim schemas; unknown service errors name the
valid services), `suite_call` (string-carried `args` parsed as one JSON
object, rejected without dispatch when malformed; bare-name dispatch through
the D60 client; result relay) — with the D19 error framing on every failure
path (service + tool named, `suite_tools("<service>")` pointed at). Per-call
failure only: nothing in this package can fail a run.

**Done when:** R-OWUN-9LVP, R-OY2J-NDME, R-OZAG-15D3, R-P0IC-EX3S, and
R-P1Q8-SOUH's gateway-level half (an unreachable peer yields an error result
from the tool, not a Go panic or run abort — the run-level clause lands in
Phase 75's end-to-end) are each covered by a test tagged with its id, and the
suite is green (design Conventions).
