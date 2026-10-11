# Untestable broker default sources

Filed during the user-invoked connections build-spec run, initially at commit 505678732f2d666c56e965332f511079faf9ce4c. The OAuth verifier reported the gap and a fresh runtime verifier independently confirmed it.

Requirement involved: R-RJAT-2UCZ in D06-connect.md.

The requirement declares that nil broker `Config.Now` and `Config.Rand` mean `time.Now` and `crypto/rand.Reader`. The sub-project AGENTS.md test discipline says that a test never reads or changes the real clock or randomness. Injected sources can prove the configuration shape and injected behavior, but cannot distinguish a compliant default from a different default. Observing the fallback through broker operations would consume the forbidden real source; inspecting source is also prohibited as requirement coverage by the spec skill.

The single permitted binary test does not provide a permitted observation of this broker fallback. Its allowed requests do not make provider calls, and `cli.Run` supplies its process defaults before constructing the broker, so the broker's nil-source branch is not exercised through that path.

The requirement cannot be fully proved by an admissible by-use test. The build leaves its tag absent, retains independent injected-source tests, and does not claim this id closed. Changing test discipline or the contract is outside the build's authority.

Suggested resolution: move selection of real defaults entirely to the process boundary and require injected sources for the broker, or define an explicit permitted and observable way to test broker defaults. Replace the affected requirement id when changing its text.
