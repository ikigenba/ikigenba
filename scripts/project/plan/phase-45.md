# Phase 45 — Adopt the run-lifetime contract: 6h-bounded run TTL, 10-minute token margin

*Realizes design Decision 33 (bounded run TTL) and Decision 36 (run-token
margin).*

The composition root validates `SCRIPTS_RUN_TTL` against the run-lifetime
contract's 6h maximum (`root project/design/D26.md`) and refuses to boot on a
value over it (or zero/negative), with an error naming the variable and the
maximum. The runner's mint request changes its margin from five minutes to the
contract's fixed ten: requested TTL = configured run TTL + 10m exactly. The
seam-translation id R-IQK1-K7ZD and the manifest-agreement id R-HDCE-C6WU are
untouched.

**Done when:**

- R-34EZ-J9BF (adopted, `root project/design/D26.md`) — `SCRIPTS_RUN_TTL=7h`
  fails `serve` at startup: nonzero exit, error names `SCRIPTS_RUN_TTL` and
  the `6h` maximum, no server; `SCRIPTS_RUN_TTL=6h` boots — covered by a
  tagged hermetic test with an injected environment.
- R-35MV-X124 (adopted, `root project/design/D26.md`) — the `RunToken` request
  the runner issues at run spawn carries a `ttl` of exactly the configured run
  TTL plus 10m (30m → `40m`) — covered by a tagged test driving the runner
  against a recording fake plane.
- The suite is green per design Conventions.
