# Load write row limit has no permitted observation

Filed during the user-started build-spec run against initial commit `fc61dcbbf0db72122938590b4c222b0c217ca7aa`, after independent store review and fresh blocker validation.

Requirement: R-RHXB-8EET, specifically “each write inserting at most `Config.LoadRows` rows” in `specs/design/D04-store.md`.

The implementation performs bounded writes, but the declared test seams cannot distinguish a compliant write sequence from an oversized write followed by a smaller one. The store implementer, store verifier and independent blocker verifier all confirmed this limitation. The previous test helper read temporary database tables; those assertions have been removed because AGENTS.md permits run-file table reads only for the completed compatibility-file check under R-PPAM-YK40. The unsupported requirement tag has also been removed; the behavior tests remain.

R-N377-8DHQ exposes only context, bytes and duration through `Step`, without the number of rows in a write. Public row reads hide running runs and promise row contents only for done, intact files (R-RJ57-M65I and R-RLL0-DPMW). AGENTS.md says tests prove stored records “never by reading tables”; its sole run-file table-read exception is “as R-PPAM-YK40 states.” Counting callbacks and checking final rows cannot distinguish writes containing `[2,2,1]` rows from `[3,1,1]` when `LoadRows` is 2. Byte-growth checks do not specify a row count either.

Copying an unfinished database into a separate done-run fixture does not provide a contractual observation: R-QM7X-AD4R defines intact files as bytes a completed Load of that run left at its file. Such a snapshot is outside the public row-read guarantees.

Evidence: `go test -count=1 -race ./internal/store` passed after removal of the prohibited reads, but passing aggregate step, byte, publication and final-row assertions does not prove this per-write clause. No gate failure is being claimed.

This cannot be resolved inside the build run because design and AGENTS.md are read-only. Suggested resolution: outside the build run, add a permitted per-write row-count observation or explicitly allow the narrow temporary-file row-count check, and re-mint any changed requirement. Then rerun build-spec to restore the requirement tag with an adequate test.
