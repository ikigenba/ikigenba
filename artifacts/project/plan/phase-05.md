# Phase 5 — Content-plane citizenship: holder endpoint + import acceptor

*Realizes design Decision 5 (content plane). Depends on Phase 4.*

Both plane roles: the loopback-guarded `GET /content?id=<id>` holder
endpoint (visibility-blind, hash-pinned, no `rev`), the reference
composition via `registry.BaseURL("artifacts")`, and `Service.Import` with
absolute loopback+registry-port confinement, the shared cap-hash-blob
streaming path, and D5's error mapping (`validation` /
`source_unavailable` / `too_large`). End state: other services can pull
stored files by reference, and artifacts can pull from any suite holder —
bytes never crossing an agent.

**Done when:** the suite is green and each of R-441L-GJ14, R-46HE-82II,
R-47PA-LU97, R-48X6-ZLZW, R-4A53-DDQL, R-4BCZ-R5HA is covered by a test
tagged with its id.
