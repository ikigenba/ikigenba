# Phase 146 — Conditional relative-time anchoring in the extract prompt

*Realizes design Decision 6 (extract stage — the R-8TAF-XBSM / R-8UIC-B3JB slice).*

The embedded extract prompt `internal/extract/prompt.txt` states the new conditional relative-time rule: the received-on date anchors relative expressions only for documents speaking from the real-world present; narrative text keeps its time relative with `occurred_at` empty, and doubt resolves to relative. The prompt carries the exact key phrases "only when the document speaks from the real-world present" and "When in doubt, keep it relative", drops the unconditional "Resolve relative time against the header's" sentence, and adds a short narrative counter-example beside the existing real-world example. The committed tune copy `autotune/extract/prompt.txt` is updated to the identical content (one-time manual copy; no mechanical tie is introduced). The extract tune folder gains the committed dev case `autotune/extract/cases/dev/narrative-relative-time/` (relative-time narrative `input.txt`; `gold.json` expecting `occurred_at:""` and a relative claim), and the `autotune` test package asserts it.

**Done when:**
- R-8TAF-XBSM — `DefaultPromptInstructions` contains the two pinned phrases and not the retired unconditional sentence — covered by a named test.
- R-8UIC-B3JB — the `narrative-relative-time` dev case exists with the pinned input/gold shape — covered by a named test in the `autotune` package.
- `cmp -s wiki/internal/extract/prompt.txt wiki/autotune/extract/prompt.txt` exits 0 at phase completion (one-time sync check; no committed test compares them).
- The suite is green (`go test ./...` from `wiki/`, per design Conventions).
