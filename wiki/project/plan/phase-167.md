# Phase 167 — The worker's prompts calls carry the job's chain id, not its job id

*Realizes design Decision 64 (worker-path attribution), 65 (empty-correlation durable-root reuse), 35 (after-commit embed attribution), 38 (merge-path embed attribution), and 4 (attribution paragraph, `R-OWNH-GXSO` retired).*

`jobAttribution` in `internal/wiki/service.go` currently returns
`llm.Attribution{Origin: origin, GroupID: job.ID}`. That is the shipped defect: an
ingest's extract, compile, and embed-page calls all reach prompts under the job id,
so the chain that enqueued the job dies at the async boundary and telemetry's
`chain` on the stored id returns only wiki's own inbound request.

**End state.** The per-job attribution derivation returns the job's correlation id:
the row's stored `correlation_id` when it is non-empty, and `job.ID` when it is
empty (root `project/design/D14.md` durable-root reuse — the job is its own outermost
cause). The value is identical across that job's extract, compile, and embed-page
calls, and identical to the value the derived job context carries, so the outbound
`X-Correlation-Id` header (D64) and the `group_id` field agree.

**Four tagged tests presently assert the opposite of their ids and must be
replaced, not extended.** This phase is the one case where removing an existing
`R-`-tagged test is correct:

- `internal/wiki/service_test.go` — `TestJobContextResumesStoredChainWhileAttributionUsesJobID`
  (tagged `R-XJ27-56BR`) asserts `jobAttribution(storedJob).GroupID == storedJob.ID`
  for a job with a stored chain, and asserts a freshly minted id for the empty case.
  Both assertions are now wrong. Rewrite the test to the current statement, including
  its name, which encodes the retired behavior.
- `internal/wiki/ingest_pipeline_test.go` — `TestWorkerThreadsStoredChainThroughExtractCompileAndEmbed`
  (tagged `R-XP5P-2118`) asserts `call.GroupID == jobID` for jobs ingested under an
  explicit chain id. Rewrite to assert the chain id.
- `internal/wiki/page_embedding_test.go` — both tests tagged `R-6YVT-TFOD` pass
  `llm.Attribution{}` and assert nothing about correlation at all. The id is unproven.
  Drive the embed with a job that has a stored chain differing from its id and capture
  the group at the embed seam.
- `internal/wiki/merge_worker_test.go` — the test tagged `R-MRG8-K2WP` asserts
  `groupID != jobID` is an error, pinning the merge path to the same retired rule.
  Rewrite it to assert the merge job's stored chain id. Its statement also names
  `CallRecord` / `LLMCallStore`, seams D63 retired and which no longer exist in the
  tree; the rewritten statement drops them and captures at the embed seam instead.

`internal/wiki/completion_queue_test.go` — `TestQueueCallsForIngestAreGroupedByReturnedJobID`
(tagged `R-OWNH-GXSO`) proves a behavior design no longer mints. Delete the test with
the id.

**Done when:**
- `go test ./...` from `wiki/` is green.
- `R-XJ27-56BR` is covered: the derivation returns the stored `correlation_id` for a
  job whose stored id differs from `job.ID` (returning `job.ID` fails), returns exactly
  `job.ID` when the stored value is empty (never empty, never a freshly minted id),
  the value is identical across that job's stages, and the derived context carries it.
- `R-XP5P-2118` is covered: a worker-processed job whose row stores correlation id `X`
  while its `job.ID` differs posts `group_id` `X` on its extract, compile, and
  embed-page calls, captured at the httptest prompts.
- `R-6YVT-TFOD` is covered: after such a job integrates, the captured `embed-page`
  call's `GroupID` is exactly `X` — `job.ID`, an empty group, or an id differing from
  that job's other calls each fail.
- `R-MRG8-K2WP` is covered: a merge job whose row stores chain id `X` while its
  `job.ID` differs issues exactly one after-commit `embed-page` call whose `GroupID`
  is `X` (a call carrying `job.ID` fails), and the winner's `page_embeddings`
  `content_hash` equals the post-merge page fingerprint.
- `grep -rn 'R-OWNH-GXSO' --include='*_test.go' .` returns no matches, and
  `grep -rn 'R-OWNH-GXSO' project/design/` returns no matches.
- The `$ikispec` coverage check emits no output.
