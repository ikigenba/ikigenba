# Store default randomness cannot be proved under the test discipline

Filed by the user-invoked gdrive build-spec run starting at commit `fda872c880b1866b9634594a3ea4cd1d58d64e68`. Two independent verifiers confirmed this conflict; the design and AGENTS.md remain unchanged.

Requirement `R-RW4B-7DWD` in `specs/design/D04-store.md` declares `store.Config` and `store.New`, with “a nil Now or Rand meaning time.Now and crypto/rand.Reader”. The test discipline in `AGENTS.md` requires injected time and randomness that the test controls and says, “A test never reads or changes the real environment, clock, or randomness.” The spec skill requires a permitted test that proves a requirement by use and distinguishes a compliant implementation from a noncompliant one.

A test can construct `store.New(d, store.Config{})`, but construction and methods that do not consume randomness cannot distinguish an implementation that supplies the required random fallback from one that omits it. A successful `Add` with nil `Rand` must consume real randomness through `crypto/rand.Reader`, violating the test discipline. Replacing that process-global reader changes real randomness, also prohibited. Inspecting private fields or source would not prove the declared behavior by use. The scoped tests pass, but passing construction alone does not prove this requirement. The inadequate coverage tag is removed rather than counting it as built.

This is a narrow conflict concerning the default behavior, established by the nil random source alone. It does not prevent implementing or testing the store with injected values, and no claim is made that every possible use of `time.Now` necessarily reads the real clock.

Suggested resolution: explicitly permit a bounded test of the default random source, or revise the design to require injected nonnil values. Either resolution changes read-only build inputs and must happen outside this build run. Any changed requirement text needs a new id. Independent work can be retained, but the build cannot claim completion while this issue is open.
