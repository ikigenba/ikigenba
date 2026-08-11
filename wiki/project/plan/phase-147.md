# Phase 147 — Scope instructions: storage, cap, and the system composer

*Realizes design Decision 82 (scope instructions — the R-8FVJ-PUMZ / R-8H3G-3MDO / R-8JJ8-V5V2 / R-8KR5-8XLR slice).*

A new timestamped migration (`bin/create-migration wiki <name>`) additively adds `instructions TEXT NOT NULL DEFAULT ''` with the 4,000-char `CHECK` to `scopes`, preserving existing rows. `internal/wiki` grows: the `Scope` struct's `Instructions` field (returned by `Get`/`List`), `ScopeStore.SetInstructions` (byte-exact storage, empty clears, `ErrScopeNotFound`, `ErrInstructionsTooLong` over `InstructionsCharCap` = 4000 runes), and `ComposeSystem(base, instructions)` with the exact wrapper line pinned in D82.

**Done when:**
- R-8FVJ-PUMZ — additive migration preserves seeded scopes rows and enforces the CHECK — covered by a named test.
- R-8H3G-3MDO — byte-exact set/get round-trip, clear via empty, unknown-scope typed error — covered by a named test.
- R-8JJ8-V5V2 — 4,001-rune reject (value unchanged) and 4,000-rune accept — covered by a named test.
- R-8KR5-8XLR — `ComposeSystem` byte-identical pass-through on empty and exact composed form on non-empty — covered by a named test.
- The suite is green (`go test ./...` from `wiki/`).
