# Run constructor count is unobservable

Filed during user-invoked `build-spec` for dummy at initial commit `48231bba3c6a3d73140dbf900a07ad0e0dbe3527`, after independent validation of the implementation leaf's blocker report.

R-EFZQ-D53P requires `Run` to call `widget.NewStore` exactly once and then `server.Serve` exactly once, passing the original context, listener, constructed handler and drain. R-DSTN-3I0I declares the complete `Process` interface without a constructor or serving callback. R-EFDU-M6U6 declares the fixed `NewStore() *Store` function; R-EYW8-QIPA and R-GV6M-5O86 require deterministic initial state and independence between stores.

The constructor count cannot be established through the declared interface. In an otherwise compliant implementation, inserting `_ = widget.NewStore()` before the required store creation violates the exact count while leaving every permitted observation unchanged: the discarded independent store produces no environment access, listener activity, banner call, MCP response, HTTP response or diagnostic. A counting listener or custom context does not observe the extra constructor call.

The existing private `newStore`, `serve` and `panelHandler` aliases do not solve this problem. Replacing an alias in a test records calls to that alias, while the extra direct `widget.NewStore()` bypasses its recorder. Checking an alias's initial function pointer cannot establish which direct calls `Run` performs. Allocation or profiling measurements do not provide the declared by-use contract and cannot reliably identify constructor calls.

dummy's AGENTS.md prohibits reading the checkout or inspecting source in tests. The spec skill requires tests to prove requirements by use, and build-spec requires complete assertion of each added requirement. Source inspection, adding an undeclared test seam, or tagging only the observable portions would not resolve R-EFZQ-D53P within this run's authority.

Evidence: implementation report `/tmp/scratch.oqyFQu` and fresh independent validation `/tmp/scratch.GuoF88`. Both identify the discarded-constructor counterexample; the verifier additionally challenged and rejected the existing alias-injection approach. No gate failure is alleged: this is an unreachable test obligation in the read-only contract.

Suggested resolution: use `draft-design` to replace and re-mint R-EFZQ-D53P around observable initialized/shared-state, cancellation and writer behavior, or declare an explicit callable factory/serving seam and require counts on that seam. Until resolved, the requirement remains untagged; independent testable gap work may finish without claiming the build run complete.
