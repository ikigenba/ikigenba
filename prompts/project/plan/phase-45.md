# Phase 45 — The `ui/` namespace and the two list tabs; the landing card retires

*Realizes design Decisions 34 (`ui/` namespace) and 35 (browse UI), list-page
slice. Depends on Phase 44.*

The human surface moves under `/ui/` and becomes the two-tab browse UI:

- **nginx** (`etc/nginx.conf`): the new session-gated
  `location /srv/prompts/ui/` block per D34 (session gate, `@login_bounce`,
  four owner headers, `proxy_pass http://127.0.0.1:3002/ui/`).
- **Root redirect**: `GET /{$}` becomes the relative `ui/` redirect (303),
  still exact-match and ungated in-process (D10 rewritten).
- **Templates** (`share/www/`): the shared chrome (Home link, service
  name+version header, two-tab nav, Carbon via the mount-literal asset paths
  per D34/D13) and the two list pages `ui-prompts.html` / `ui-runs.html`;
  `landing.html` is **deleted**.
- **Handlers** (`cmd/prompts`, `registerRoutes`): `GET /ui/{$}` (prompts tab)
  and `GET /ui/runs` over Phase 44's browse queries — server-side filters
  (`q`, `status`, `prompt_id`), 50-row pages, filter-preserving pager.
- **Test truing-up**: tests tagged with the retired id `R-LAND-PG01` and the
  old D11 landing-markup conform check are deleted; retained ids re-anchor to
  the new surface — `R-LAND-ROOT`/`R-LAND-UNGT` (root now a 303),
  `R-LAND-NMVR`/`R-LAND-CARB` (UI chrome), `R-HOME-2T4X` (every UI page),
  `R-DI0I-AFH8`/`R-DJ8E-O77X` (mount-literal asset links/preloads),
  `R-DIAW-ZFMC` (renders `ui-prompts.html`); `R-DFKP-IVZU`, `R-DGSL-WNQJ`,
  `R-DKGB-1YYM`, `R-DJIT-D7D1`, `R-7NY0-UIO6`, `R-7P5X-8AEV` keep their
  existing assertions.

**Done when:** the suite is green (design Conventions) and these ids are
covered by clearly-named tagged tests:

- R-ZW7P-88WL — root `GET /` returns 303 with relative `Location: ui/`.
- R-ZXFL-M0NA — the `location /srv/prompts/ui/` fragment block (gate, bounce,
  four headers, no `X-Client-Id`, upstream `/ui/`).
- R-ZYNH-ZSDZ — `/ui/` pages render with no identity header (ungated
  in-process).
- R-04QZ-WN3G — prompts tab columns, order, row links, tab nav.
- R-05YW-AEU5 — prompts `q` filters server-side (non-matches absent from the
  HTML).
- R-076S-O6KU — 50-row pages; page 2 correct; pager preserves filters.
- R-08EP-1YBJ — runs tab columns (incl. computed duration and trigger), order,
  row links.
- R-09ML-FQ28 — runs `status` and `q` filters.
- R-0AUH-THSX — runs `prompt_id` filter.

…and the retained re-anchored ids above remain tagged and green, while
`grep -rn 'R-LAND-PG01' --include='*_test.go' .` (from `prompts/`) returns
nothing and `share/www/landing.html` no longer exists.
