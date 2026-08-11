# Phase 149 — The `instructions` MCP verb, guide, and count supersessions

*Realizes design Decision 82 (the R-8PMQ-S0KJ / R-8QUN-5SB8 / R-8S2J-JK1X slice) and the superseded count pins D10 R-MUQ4-K1JS and D57 R-YF06-03HO. Depends on Phase 147.*

The MCP surface gains the `instructions` verb (`scope` + `action` required, `action` enum `["get","set"]`, optional `text`; get returns `{scope, instructions}`, set stores byte-exact with empty clearing) with typed `scope_not_found` and `instructions_too_long` errors; the guide document and `initialize` instructions are extended per D82; and the existing membership/count tests tagged R-MUQ4-K1JS and R-YF06-03HO are updated to the nineteen-verb / twenty-tool pins.

**Done when:**
- R-8PMQ-S0KJ — schema shape + get/set/clear happy path through the assembled handler — covered by a named test.
- R-8QUN-5SB8 — set-without-text, unknown-scope, and over-cap typed errors (value unchanged) — covered by a named test.
- R-8S2J-JK1X — guide + initialize-instructions content and the D57 pointer rule — covered by a named test.
- The tests tagged R-MUQ4-K1JS and R-YF06-03HO assert the updated membership (nineteen verbs / twenty tools including `instructions`) and pass.
- The suite is green (`go test ./...` from `wiki/`).
