# Phase 2 — Data model, tokens & blob store

*Realizes design Decision 2 (data model). Depends on Phase 1.*

The persistence layer: the timestamped migrations creating `artifacts` and
`uploads` (D2's exact columns and CHECK constraints, via
`bin/create-migration artifacts <name>`), the `migrations.sha256` manifest +
guard, the `internal/db` store API (`CreateUpload`, `GetUpload`,
`ConsumeUpload`, `PurgeExpiredUploads`, `CreateArtifact`, `GetArtifact`,
`ListArtifacts`, `UpdateArtifact`, `DeleteArtifact`,
`IncrementDownloadCount`), `artifacts.NewToken()` behind its injectable
seam, `artifacts.ValidateFilename`, and the temp+rename `BlobStore` over
`<state>/blobs/`. End state: a fresh DB migrates cleanly and every store
operation behaves as D2 specifies against real SQLite and a real temp dir.

**Done when:** the suite is green and each of R-3D7T-1KPU (schema + CHECK),
R-3EFP-FCGJ (token shape + collision retry), R-3FNL-T478 (filename rules,
byte semantics), R-3GVI-6VXX (temp+rename blob visibility), R-3I3E-KNOM
(single-winner consume), R-3JBA-YFFB (purge semantics), R-3LR3-PYWP
(count increment + timestamp discipline), R-NFQ1-NA7N (migration manifest
guard) is covered by a test tagged with its id.
