# Phase 102 — Honest-empty scoring: agreement on emptiness earns 1.0

*Realizes design Decision 65 (deterministic scorer — the honest-empty carve-out), slice R-ESN9-RFM1.*

`internal/eval`'s rollup gains the empty-denominator carve-out: a component whose gold side and extracted side are both empty scores 1.0 instead of 0, so a case with empty gold and an empty extraction earns composite exactly 1.0, while an empty-gold case with any extracted subject still scores subject-F1 0 and a non-empty-gold case with an empty extraction still scores 0. All existing scorer behavior (R-KM0O-JI4D … R-KWZR-ZFSM) is unchanged; existing tagged tests stay green as-is.

**Done when:**
- R-ESN9-RFM1 — empty gold + empty extraction ⇒ composite 1.0; empty gold + one extracted subject ⇒ subject-F1 0; non-empty gold + empty extraction ⇒ 0 — covered by a tagged test.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`).
