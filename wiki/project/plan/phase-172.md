# Phase 172 — Extract classifies statements: claims and corrections

*Realizes design Decision 6 (the extract stage — corrections slice: R-7BO4-JMO9, R-7CW0-XEEY). Depends on Phase 171.*

Extends `internal/extract`: `ExtractedSubject` gains `Corrections []string`; `validate` accepts corrections (with or without claims), requires ≥1 statement of either kind, and rejects empty-after-trim entries; `prompt.txt` gains the classification discipline with the exact key phrases "only when the document itself rules a fact false" and "a plain assertion is always a claim", plus the worked denial-plus-truth example yielding one correction and one claim. Downstream consumption of the new field is Phase 174's; the autotune correction case (R-7E3X-B65N) is Phase 177's.

**Done when:** the suite is green and each id is covered by a genuine tagged test:

- R-7BO4-JMO9 — `validate` acceptance/rejection over the statement kinds.
- R-7CW0-XEEY — `DefaultPromptInstructions` carries the exact classification phrases.
