# Phase 17 — adopt the LLM-lint semantic gate

*Structural phase; realizes no local Decision. Adopts the suite-wide LLM-lint gate defined by root project/design/D31.md.*

This tree comes under the suite's LLM-lint gate (`root project/design/D31.md`):
the `llm-lint` binary, run over this tree with the whole builtin rule catalog,
reports no findings. The observable end state is that `llm-lint .` from the tree
root exits 0 — any fixed-duration test sleep the catalog flags is either replaced
by a real synchronization or bounded condition-poll, or, where the delay is itself
the behavior under test, marked with an inline `llm-lint:ignore` — while the
tree's existing green bar is unchanged.

**Done when:**
- Running `llm-lint .` from the artifacts root exits 0 (no findings).
- The tree's suite is green per its own Design Conventions, unchanged from before
  this phase (this is a `realizes —` structural phase and covers no requirement id).
