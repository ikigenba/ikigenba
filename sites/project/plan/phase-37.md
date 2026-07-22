# Phase 37 — Landing page speaks the visibility enum

*Realizes the changed slices of design Decision 19 (visibility label, island
`visibility` key, unlisted URL segment). Depends on Phase 35 (enum domain);
independent of Phase 36's tool-contract work but ordered after it so the wire
vocabulary lands as one release.*

The landing surface renders the enum: `siteRow.Visibility` (string) replaces
`siteRow.Public` in the `GET /{$}` view model (`cmd/sites`), `landing.html`
prints the label verbatim (`public`/`private`/`unlisted`), the JSON data island
carries `visibility` instead of the retired `public` key, and the client
controller's row rebuild (`share/www/static/landing.js`) prints `row.visibility`
verbatim in the label cell. URLs build from `sites.Seg(v)`, so an unlisted
site's anchor, island `url`, and copy-button URL all carry the `public/`
segment. No filter/sort/paginate logic changes (D22's pure functions are
untouched); no nginx change.

Existing tests tagged with the deleted ids R-RAW6-IUN5 and R-IDOL-PV70 are
replaced by this phase's tests; tests tagged R-RC42-WMDU, R-WMWB-7EWX, and
R-IEWI-3MXP are updated to the new view model and keep their tags. The D23
browser test's seed data is updated mechanically if its fixtures name the old
`public` key; its scenario and ids are unchanged.

**Done when:** each of the following ids is covered by a genuinely-asserting
test tagged with it, and the suite is green (design Conventions):

- R-HK3X-22SM — three seeded sites (one per visibility) each render their
  verbatim visibility label plus slug/creator/created-at.
- R-HLBT-FUJB — the island elements carry the `visibility` string (and no
  `public` key); an empty slice yields `[]`.
- R-HMJP-TMA0 — an unlisted site's anchor href is `<baseURL>public/<slug>/`,
  byte-identical to the island `url`.
