# Process defaults conflict with required test seams

Filed by the user-invoked gdrive build-spec run starting at commit `fda872c880b1866b9634594a3ea4cd1d58d64e68`. The integration test implementer reported the conflict and an independent verifier confirmed it. Read-only inputs remain unchanged.

Requirement `R-QT3I-YQ65` in `specs/design/D01-layout-and-run-seam.md` requires nil `Rand` to use `crypto/rand.Reader`, and nil `Inherit` to take the real inherited descriptor 3. The test discipline in `AGENTS.md` requires injected randomness that the test controls, forbids reading or changing real randomness, and says a test “never leaves the inherited-listener step unset”.

A test supplying its own random source does not prove the nil fallback. A run that never consumes randomness cannot distinguish a missing fallback; a run consuming the required fallback reads real randomness, forbidden by the discipline. Substituting the process-global reader also violates that rule. Exercising nil `Inherit` is expressly forbidden even if a test could arrange a controlled descriptor. Source inspection or private-field inspection does not prove these behaviors by the declared interface. Thus no permitted test can prove the whole requirement as the spec skill requires. The inadequate coverage tag is removed rather than claiming the id built.

This conflict concerns the default clauses, not the injected process seam. Empty `Dir` has an explicit permitted `t.Chdir` test exception and is not implicated. Other process behavior remains testable.

Suggested resolution: explicitly permit bounded tests of these defaults, or revise the design to require nonnil injected seams. Either changes read-only build inputs and must occur outside the build run, with a new id for changed requirement text. This issue remains open and prevents full completion.
