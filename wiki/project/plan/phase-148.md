# Phase 148 — Inject scope instructions into the four inference stages

*Realizes design Decision 82 (the R-8LZ1-MPCG / R-8N6Y-0H35 / R-8OEU-E8TU slice). Depends on Phase 147.*

The worker loads the job's scope instructions at job start and composes them into the extract and compile call sites via `ComposeSystem` (merge-triggered compiles included); `ask.Asker.Ask` loads them once per ask and composes both the `ask-subject` and `ask-synthesis` sites. A scope with empty instructions serves byte-identical base prompts (the existing D6/D7/D36/D9 captured-system tests stay green unchanged).

**Done when:**
- R-8LZ1-MPCG — captured extract `system` is the composed form with instructions set and the bare base prompt without — covered by a named test.
- R-8N6Y-0H35 — captured compile `system` likewise — covered by a named test.
- R-8OEU-E8TU — both captured ask-stage `system`s likewise — covered by a named test.
- The suite is green (`go test ./...` from `wiki/`).
