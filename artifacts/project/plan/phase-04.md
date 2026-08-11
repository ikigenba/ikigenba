# Phase 4 — The download surface: public and private tiers

*Realizes design Decision 4 (downloads). Depends on Phase 3.*

The serving half: the shared `serveArtifact` logic and the two mounts —
bare self-guarding `GET /f/<id>/<filename>` (public) and identity-gated
`GET /p/<id>/<filename>` (private) — with exact filename matching,
tier-mismatch-invisible 404s, extension-derived `Content-Type`, attachment
`Content-Disposition`, streamed bodies, and the post-response download-count
increment. End state: every stored file downloads byte-identically on
exactly its tier-correct URL and nowhere else.

**Done when:** the suite is green and each of R-3WQ7-5WKY, R-3XY3-JOBN,
R-3Z5Z-XG2C, R-40DW-B7T1, R-41LS-OZJQ, R-42TP-2RAF is covered by a test
tagged with its id.
