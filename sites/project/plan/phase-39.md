# Phase 39 — The MCP surface speaks name + slug: create, set_visibility, rename, guide

*Realizes design Decisions 20 (MCP surface), 13 (tool partition slice
R-Z8DD-BL71), 21 (guide slice R-01MY-I2ZP), and 25 (rename conformance slice
R-0A69-6H6K). Depends on Phase 38.*

`internal/mcp` takes on the split contract: `create(name, slug?, visibility)`
(name required and `ValidateName`-trimmed at every visibility; slug required
for public/private, forbidden for unlisted where the injectable token source
generates it with one collision retry), `set_visibility(slug, visibility,
new_slug?)` (the full D20 matrix — the display name untouched by every cell),
the new `rename(slug, name)` tool (with its `outputSchema` and closed error
codes, D25), `delete(slug)`, and the `renderSite` projection carrying `slug` +
`name`. The embedded `guide.md` and the tool descriptions are rewritten to the
D21 shape (name+slug examples, the name-survives-rotation anchor). The listed
surface becomes the 14-domain/16-total partition.

Tests tagged with the retired ids (R-Z6FF-WUWS, R-H94T-M54D, R-HACP-ZWV2,
R-HBKM-DOLR, R-HCSI-RGCG, R-HF8B-IZTU, R-HGG7-WRKJ, R-HHO4-AJB8, R-RJFH-78U0,
R-0UUY-N97T, R-HIW0-OB1X) are replaced by the new ids' tests; kept-id tests
whose setup uses `create` (e.g. R-56CN-HE21, R-RI7K-TH3B, R-CW5E-T20N,
R-CYL7-KLI1, R-0KIG-2A5X) get their call sites mechanically updated to the new
argument shape, keeping their tags.

**Done when:** each of R-ZN05-WU3D, R-ZO82-ALU2, R-ZQNV-25BG, R-ZRVR-FX25,
R-ZT3N-TOSU, R-ZUBK-7GJJ, R-ZVJG-L8A8, R-ZWRC-Z00X, R-ZXZ9-CRRM, R-ZZ75-QJIB,
R-00F2-4B90 (D20), R-Z8DD-BL71 (D13), R-01MY-I2ZP (D21), and R-0A69-6H6K (D25)
is covered by a clearly-named test tagged with its id, no retired-id tag
survives under `internal/` or `cmd/`, and the suite is green per design
Conventions.
