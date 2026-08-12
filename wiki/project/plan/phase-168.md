# Phase 168 — `status` reports the job's chain handle

*Realizes design Decision 10 (`status` result shape). Depends on Phase 167.*

Once the worker's prompts calls carry the job's correlation id rather than its job
id, a caller holding a job id can no longer reach that job's calls on prompts'
record: the group handle is a value wiki never returned. `status` closes that gap.

**End state.** The `status` tool's result carries `correlation_id` alongside
`status`, `received_at`, and the rest — the job's D65 correlation id, meaning the
chain id stored on its row, or `job.ID` when the row stores none. The field is
always populated: never absent, never empty. It is the same value every prompts
call that job caused was submitted under, so it filters prompts' `calls`/`usage`
and telemetry's `chain` to that one ingest.

The read path already loads the row; `JobStatus` carries the value out and the MCP
handler serializes it. No migration, no new tool, no change to any other verb.

**Done when:**
- `go test ./...` from `wiki/` is green.
- `R-N729-RY1I` is covered by a test driving the **assembled MCP handler** over a
  real temp SQLite: a job ingested under chain id `X` returns `correlation_id == X`
  from `tools/call status`, and a job row stored with an empty `correlation_id`
  returns `correlation_id` equal to that job's own id. A result omitting the field,
  returning it empty, or returning `job.ID` for the stored-chain job fails.
- `R-MUQ4-K1JS` still passes unchanged: the verb list is still exactly nineteen
  tools, since this phase adds a result field and no verb.
- The `$ikispec` coverage check emits no output.
