# Phase 06 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 8. Touches `internal/web/landing.html`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (`StaticHandler` is already wired at `GET /static/` in
`cmd/sites/main.go`).*

> ⚠️ **Build-order dependency: this phase must run AFTER cron's FOUT phase
> lands.** sites's `internal/web` is byte-pinned to cron's canonical:
> `web_test.go::TestTokensCSSMatchesCronCanonical` asserts
> `bytes.Equal(sites tokens.css, cron tokens.css)`, and
> `TestLandingTemplateConformsToCronCanonicalWithSitesCopy` asserts sites's
> `landing.html` equals cron's with only the three sites name/copy substitutions.
> If sites changes its files to the new FOUT form while cron's canonical still
> carries the old `font-display: swap` / origin-absolute `src` / no-preload form,
> those two conformance tests fail. **cron must change first; sites then mirrors
> cron byte-for-byte** (identical `font-display: optional`, identical relative
> `src`, identical two preload `<link>`s). The `tokens.css` header comment
> "vendored locally for the cron landing page" stays verbatim — it is part of the
> byte-pinned canonical.

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than sites's own: `landing.html` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/sites/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load sites's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them.

In **`internal/web/landing.html`** (mirroring cron's canonical):
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/sites/static/tokens.css`,
  sites's own copy).
- Add two font preloads in `<head>`, beside the stylesheet link:
  ```html
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/space-grotesk.woff2">
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/ibm-plex-sans.woff2">
  ```
  `crossorigin` is mandatory; the document-relative `href` resolves to the same
  URL as the new `@font-face` `src`. Do not preload the mono family.

In **`internal/web/static/tokens.css`** (all four `@font-face` blocks, mirroring
cron's canonical byte-for-byte):
- `font-display: swap` → `font-display: optional`.
- `src: url('/static/fonts/X.woff2')` → `url('fonts/X.woff2')`.

In **`etc/nginx.conf`**, add a session-gated static location that **proxies** the
embedded design assets to the upstream `/static/` route (distinct from the two
disk-backed `alias` tiers, which serve the user's website mirror, not the
process's embedded assets):
```nginx
location /srv/sites/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/sites/` landing location, the exact `= /srv/sites/mcp`
bearer endpoint, the PRM bootstrap, the `/srv/sites/public/` and
`/srv/sites/private/` disk tiers, and the `@sites_authn_500` re-emit location
unchanged.

**Update the two superseded D3 assertions (keep their ids):**
- `web_test.go` **R-ASST-5K9Q** (`TestLandingHTMLReferencesOwnEmbeddedStaticPath`):
  change the asserted landing-HTML substring from `/static/tokens.css` to
  `static/tokens.css` (the link now points at sites's own copy — D3's original
  intent, corrected form). The companion `dashboard` / `://` cross-service checks
  stay (the new preload `href`s are document-relative `static/fonts/…`).
- `web_test.go` **R-ASST-3H7N** (`TestStaticHandlerServesTokensCSS`): change the
  asserted `tokens.css` font substring from `url('/static/fonts/space-grotesk.woff2')`
  to `url('fonts/space-grotesk.woff2')`. Leave the `text/css` content-type check
  and the Carbon design-token presence checks unchanged.
- `web_test.go` **R-ASST-7M2S** (`TestStaticHandlerServesEmbeddedFonts`) stays
  **unchanged** — it remains the real-substrate proof that sites serves its own
  font bytes.

**Done when:** the suite is green (per design *Conventions*: `cd sites && go build
./...`, `cd sites && go vet ./...`, `cd sites && gofmt -l .` prints nothing,
`cd sites && go test ./...`, and `bin/check-migrations sites` all succeed with zero
failures) and these ids are covered by clearly-named tests:

- **R-629P-84O5** — sites's embedded `tokens.css` contains `font-display: optional`
  in every `@font-face` block and **no** `font-display: swap` occurrence. *(served
  `GET /static/tokens.css`)*
- **R-63HL-LWEU** — sites's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-64PH-ZO5J** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest `LandingHandler`)*
- **R-65XE-DFW8** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`)*
- **R-675A-R7MX** — `etc/nginx.conf` contains a `location /srv/sites/static/` block
  whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;` (proxied, not an `alias`); the
  existing exact landing, exact MCP, PRM, public/private disk tiers, and
  `@sites_authn_500` locations are unchanged. *(fragment grep against
  `etc/nginx.conf`, mirroring the existing R-NGNX tests)*

Note the cron-conformance tests (`TestTokensCSSMatchesCronCanonical`,
`TestLandingTemplateConformsToCronCanonicalWithSitesCopy`) must also stay green —
which they will iff cron's FOUT change has already landed and sites mirrors it
exactly (see the build-order callout above).
