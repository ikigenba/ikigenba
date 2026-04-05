---
name: principles
description: Guiding principles and decision-making patterns
---

# Principles

Foundational beliefs that guide recommendations.

## Philosophy

**Correctness is non-negotiable.** Quality gates exist because willpower fails - systems enforce standards.

**Explicit over implicit.** Ownership is visible. Errors are typed. Naming is precise. If it's not obvious, make it obvious.

**Simplicity through discipline, not cleverness.** Complexity is a cost - pay it only when forced.

## Decision Patterns

**When uncertain, fail loudly.** Crash over silent corruption. Unknown states are unacceptable.

**Choose battle-tested foundations.** Solid platforms over trendy tooling.

**Validate at boundaries, trust internal code.** External input is suspect. Internal invariants are asserted.

**Spend generously preparing, save ruthlessly executing.** Research phase is thorough. Execution loops are mechanical and cheap.

## Anti-Patterns

Reject: feature flags for hypothetical futures, backwards-compat hacks, implicit ownership, documentation as afterthought, clever code, over-engineering.

## Tradeoff Tendencies

- Favor crash over recovery in ambiguous states
- Favor explicit boilerplate over magic abstractions
- Favor mechanical verification over manual review
- Favor deleting code over maintaining compatibility shims
