# Phase 36 — MCP surface: explicit visibility, unlisted create, the transition matrix, guide

*Realizes design Decision 20 (the visibility enum on the tool surface), the
changed slices of Decision 25 (the `visibility` field in the site projection /
output schema) and Decision 21 (instructions + guide anchors). Depends on
Phase 35.*

`internal/mcp` reworks the tool contracts onto the enum wire vocabulary:

- `create(name?, visibility)` — `visibility` **required**
  (`public`/`private`/`unlisted`); name required for public/private, forbidden
  for unlisted; the unlisted name comes from the injectable token source
  (default `sites.NewToken`) with one retry on collision (D20/D27).
- `set_visibility(name, visibility, new_name?)` — the full transition matrix:
  public↔private keeps the name; every transition **into** unlisted regenerates
  the token (rotation on the unlisted→unlisted cell); leaving unlisted requires
  a slug-valid `new_name`; `new_name` anywhere else is `validation` (D20).
- `renderSite` projects `visibility` (the enum string) in place of the retired
  `public` boolean; the `create`/`set_visibility`/`list` output schemas carry
  `visibility:string` (D25); `siteURL` builds from `sites.Seg(v)`.
- The Tier-0 `Instructions` and the embedded `guide.md` speak the three
  visibilities: explicit visibility at create, the unlisted secret-link example
  (no name), rotation, and leaving unlisted with `new_name` (D21).

Existing tests tagged with the deleted ids R-554R-3MBC and R-RGZO-FPCM are
replaced by this phase's tests; tests tagged R-Z6FF-WUWS, R-56CN-HE21,
R-RI7K-TH3B, R-RJFH-78U0, R-CW5E-T20N, R-CXDB-6TRC, R-CYL7-KLI1, and the D21
ids are updated to the new wire shapes and keep their tags.

**Done when:** each of the following ids is covered by a genuinely-asserting
test tagged with it, and the suite is green (design Conventions):

- R-H94T-M54D — missing/invalid `visibility` on `create` → `validation`,
  nothing created.
- R-HACP-ZWV2 — public/private `create` requires the name and births row+dir
  at the right segment with the right `url`.
- R-HBKM-DOLR — unlisted `create` forbids a name, generates the
  `^[a-z2-7]{30}$` token, and lands under the public parent with a
  `public/<token>/` url.
- R-HCSI-RGCG — the faked token source proves the single collision retry; the
  production default is `sites.NewToken`.
- R-HF8B-IZTU — public↔private keeps the name, moves the dir, rejects
  `new_name`, and re-asserting the current visibility is an idempotent success.
- R-HGG7-WRKJ — entering unlisted regenerates the token (old name/dir/URL
  gone), a second call rotates again, and `new_name` is rejected.
- R-HHO4-AJB8 — leaving unlisted requires a valid `new_name` and renames
  row+dir to the target tier.
- R-HIW0-OB1X — the guide carries the unlisted anchors (no-name create,
  URL-as-credential, rotation).
