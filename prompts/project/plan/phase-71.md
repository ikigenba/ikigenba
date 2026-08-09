# Phase 71 — Adopt the run-lifetime contract: 6h-bounded run TTL, 10-minute token margin

*Realizes design Decision 49 (bounded run TTL) and Decision 56 (run-token
margin and per-run minting).*

The composition root validates `PROMPTS_RUN_TTL` against the run-lifetime
contract's 6h maximum (`root project/design/D26.md`) and refuses to boot on a
value over it (or zero/negative), with an error naming the variable and the
maximum. The spawn path's mint request changes its margin from five minutes to
the contract's fixed ten: requested TTL = configured run TTL + 10m exactly.
The test carrying retired id R-S6PA-P6GP is replaced by the adopted-id and
new-id tests below; the door/credential ids (R-S49H-XMZB, R-S5HE-BEQ0,
R-RZDW-EK0J) are untouched.

**Done when:**

- R-34EZ-J9BF (adopted, `root project/design/D26.md`) — `PROMPTS_RUN_TTL=7h`
  fails `serve` at startup: nonzero exit, error names `PROMPTS_RUN_TTL` and
  the `6h` maximum, no server; `PROMPTS_RUN_TTL=6h` boots — covered by a
  tagged hermetic test with an injected environment.
- R-35MV-X124 (adopted, `root project/design/D26.md`) — the recorded
  `RunToken` request's TTL equals the configured run TTL plus exactly 10m
  (30m → `40m`) — covered by a tagged test on the recorded-plumbing fake
  plane.
- R-3AIH-G40W — one token minted per run; a second run mints a second token —
  covered by a tagged test.
- R-S6PA-P6GP appears in no test file; the suite is green per design
  Conventions.
