# Typed copy constants cannot be proved by the permitted tests

Filed during the human-authorized accounts build-spec run, starting at `82528d17b2e7655685d56c70c5f38f9763fb97fc`. The core verifier raised this conflict; a fresh independent verifier confirmed it at `5c267b3436c98cc72487434d68e435b3e59a7324`.

An exhaustive design search by the fresh verifier and a separate discovery leaf confirmed nine affected requirements, covering thirty constants:

- D01: R-CZLU-ZS5N (`WantSeconds`).
- D02: R-RZFT-8RE5 (`Usage`).
- D04: R-OOYM-N4BV (`Unreachable`).
- D05: R-HZ7G-HB3J (seven `Refuse…` constants), R-WJLH-OUSV (`HostFailed`).
- D06: R-A7DB-O1RF (`Description`).
- D07: R-5695-CWFS (thirteen `Describe…` constants), R-57H1-QO6H (`ArgGroup`, `ArgUser`, `ArgKey`), R-58OY-4FX6 (`Failed`, `KeysUnread`).

Each requirement explicitly declares `const <Name> string`. Their values, newline restrictions, distinctness, formatting and use are testable, but their explicit string typedness is not distinguishable by the tests accounts permits. The spec design format explicitly makes a copy constant's name and type contractual; D05 separately requires its protocol constants to be untyped, so dropping typedness would change the contract. Requirements that merely call something a string constant, without requiring an explicit string type, are not included in this issue.

For example, `const HostFailed string = "illustrative failure"` is compliant, whereas `const HostFailed = "illustrative failure"` is not. Both satisfy constant-context checks, assignments to string, content assertions and behavioral comparisons. Boxing, runtime reflection and unconstrained generic inference yield string for both. Every positive use accepting the typed string constant also accepts the same-valued untyped constant.

A distinguishing compile probe is `type Other string; var _ Other = host.HostFailed`: it fails to compile for the compliant typed constant and succeeds for the untyped mutant. Testing that distinction requires asserting compilation failure. Accounts' test discipline prohibits source inspection and permits process building/execution only for the one prescribed binary test; an additional negative compiler invocation is outside that exception. The spec skill requires an allowed test that distinguishes compliant from noncompliant implementations, and build-spec requires every tagged test to prove its entire requirement by use. Design and AGENTS.md are read-only during this run.

The reviewers confirmed that the implementation already declares these constants with explicit string types. This is a testability conflict, not an implementation defect. The nine ids remain unclosed rather than claiming partial assertions prove the entire requirements; the remaining independently verified work can be preserved.

Suggested resolution outside this run: re-mint the nine requirements to require string-valued constants without explicit typedness, or explicitly permit narrowly scoped negative compilation tests. No contract change or additional compiler test is made here.
