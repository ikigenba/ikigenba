# Binary footer coverage blocked by protected wiring tests

Filed during the user-invoked `build-spec` run at initial commit `28d5c3bb68fad8006016c206c6119d0b9c69a753`, after independent discovery and fresh blocker validation.

Requirement `R-7J0U-GFGT` requires the binary response to contain exactly one footer whose normalized text uses `panel.ServiceName` and the source-declared `cli.Version`. D01 identifies the one exec'ing test as the proof of this wiring.

The existing `TestMainWiring` carries matched requirements `R-Z46Q-6N1H`, `R-Z5EM-KES6`, and `R-Z7UF-BY9K`; its `serveAndSignal` helper carries matched `R-M7M9-WQE9` and `R-MJT9-QFT7`. The helper reads the SIGTERM response and checks the launcher only; it has no footer-count or footer-text assertion, and its SIGINT exercise sends no request. These matched tests are outside the fixed build gap. The build skill states their tests “are never judged, strengthened, or rewritten.”

The sub-project's AGENTS.md permits “One exec'ing test, and only one” and explicitly states: “Any other test that builds, execs, waits on, or signals a process is a bug.” Thus adding footer assertions to the protected exercise is forbidden during this run, while a separate process-running test is forbidden by the declared discipline. Reusing its helper from another test still starts another process-running test. In-process handler or run-seam tests substitute the banner source and cannot establish the binary's appkit constructor arguments. Source-reading tests are prohibited and cannot close this gap.

Two independent read-only reports confirmed this conflict against the unchanged binary tests at the initial commit. No legal test addition within this run can prove the binary requirement. AGENTS.md already describes the intended existing wiring exercise as asserting both launcher and footer; the current matched helper does not do the latter.

Suggested resolution: use `draft-design` to replace and re-mint the affected matched binary wiring requirement `R-Z7UF-BY9K`, coordinating its replacement with `R-7J0U-GFGT`. Address the matched requirements on `serveAndSignal` as necessary so the existing wiring exercise can enter a subsequent build gap. Design and AGENTS.md remain read-only in this run; this issue does not authorize editing protected tests.

Independent frame and form work can finish and pass gates. The source-only appkit constructor wiring fix can support that partial result, but `R-7J0U-GFGT` remains mechanically open until lawful binary coverage becomes available.
