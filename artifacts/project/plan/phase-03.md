# Phase 3 — Signed upload links: mint + the public upload ingress

*Realizes design Decision 3 (upload lifecycle). Depends on Phase 2.*

The domain service's upload half in `internal/artifacts`:
`Service.MintUpload` (validation, opportunistic purge, 24h-constant expiry,
front-door URL + `curl` command composition) and the bare-mounted
`PUT /u/<token>` ingress handler with D3's strict pipeline — method gate,
identity-header defense-in-depth, uniform-404 token authentication,
streamed cap-and-hash into the blob store, and the single-winner
transactional commit (artifact row + consume; the event hook lands in
Phase 7). End state: a minted link accepts exactly one successful `curl -T`
upload within 24 hours, failures leave it usable, and nothing about token
state is distinguishable from outside.

**Done when:** the suite is green and each of R-3MZ0-3QNE, R-3O6W-HIE3,
R-3PES-VA4S, R-3QMP-91VH, R-3RUL-MTM6, R-3T2I-0LCV, R-3UAE-ED3K,
R-3VIA-S4U9 is covered by a test tagged with its id.
