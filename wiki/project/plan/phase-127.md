# Phase 127 — The hard wall: scope threaded through pipeline, retrieval, ask, links, orphans, merge

*Realizes design Decision 74 (scope model), slice: the partition behaviors. Depends on Phase 126.*

Threads the scope as a leading parameter through every content seam (D74's list): `Service.Ingest` stamps `jobs.scope` and the worker integrates into the job's scope; `ask.Asker.Ask`, the D31 keyword lane, and the D32 meaning lane are scope-bounded; mention detection / linkify / `PageWithLinks` load their key sets per scope; `Orphans` takes a scope; `Merge`/`ListMerges` operate within one scope with per-scope aliases; `ListJobs`/`CountJobs` partition by scope. Id-addressed operations (`JobStatus`/`Abort`/`Rerun`) stay scope-free.

**Done when:** the suite is green and these ids are covered by tagged tests:
- R-GXIJ-YZV5 — ingest mints and updates only in its own scope.
- R-GYQG-CRLU — ask retrieves/cites only its scope; honest-empty despite an answer in another scope.
- R-GZYC-QJCJ — links and inline linkify never match across scopes.
- R-H169-4B38 — orphans computed per scope.
- R-H2E5-I2TX — merge and its alias routing confined to one scope.
- R-H61U-NE20 — keyword lane scope-bounded.
- R-H79R-15SP — meaning lane scope-bounded.
