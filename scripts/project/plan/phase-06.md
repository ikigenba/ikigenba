# Phase 06 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 8. Touches `internal/web/landing.html`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (`StaticHandler` is already wired at `GET /static/` in
`cmd/scripts/main.go`). Independent of all other open work.*

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than scripts's own: `landing.html` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/scripts/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load scripts's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them.

In **`internal/web/landing.html`**:
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/scripts/static/tokens.css`,
  scripts's own copy).
- Add two font preloads in `<head>`, beside the stylesheet link:
  ```html
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/space-grotesk.woff2">
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/ibm-plex-sans.woff2">
  ```
  `crossorigin` is mandatory; the document-relative `href` resolves to the same
  URL as the new `@font-face` `src`. Do not preload the mono family.

In **`internal/web/static/tokens.css`** (all four `@font-face` blocks — Space
Grotesk, IBM Plex Sans, IBM Plex Mono 400, IBM Plex Mono 500):
- `font-display: swap` → `font-display: optional`.
- `src: url('/static/fonts/X.woff2')` → `url('fonts/X.woff2')`.

In **`etc/nginx.conf`**, add a session-gated static location (mirroring the
existing `= /srv/scripts/` session gate and wiki's `/srv/wiki/static/`):
```nginx
location /srv/scripts/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/scripts/` landing location, the bearer-gated
`/srv/scripts/` prefix (and its `@prompts_authn_500` rate-limit handling), the
`= /srv/scripts/feed` denial, and the PRM bootstrap location unchanged.

**Update the superseded assertions (keep ids where present):**
- `web_test.go` **R-ASST-7Y1N** (`TestLandingHandlerLinksOnlyAppLocalStaticAssets`):
  change the assertion from `href="/static/tokens.css"` to `href="static/tokens.css"`
  (the link now points at scripts's own copy — D3's original intent, corrected
  form). The `forbidden` list (`dashboard`, `https://`, `http://`) stays satisfied
  by the new preloads.
- `web_test.go` `TestTokensCSSDeclaresEmbeddedFontFaces` (no `R-` id): change the
  asserted `tokens.css` substrings from `url('/static/fonts/…')` to `url('fonts/…')`.
- `web_test.go` `TestLandingHandlerUsesCronCanonicalStructureForScripts` (no `R-`
  id): change `<link rel="stylesheet" href="/static/tokens.css">` to the relative
  `href="static/tokens.css"` form.
- `web_test.go` **R-ASST-5X8M / R-ASST-9Z3P** (`TestStaticHandlerServesTokensAndFonts`
  — StaticHandler serves `/static/tokens.css` + the four `/static/fonts/*.woff2`
  with correct content types) stay **unchanged** — they remain the real-substrate
  proof that scripts serves its own assets at its own paths.

**Done when:** the suite is green (per design *Conventions*: `cd scripts && go build
./...`, `cd scripts && go vet ./...`, `cd scripts && gofmt -l .` (no output),
`cd scripts && go test ./...`, and `bin/check-migrations scripts` all succeed with
zero failures) and these ids are covered by clearly-named tests:

- **R-M59W-5CAW** — scripts's embedded `tokens.css` contains `font-display: optional`
  in every `@font-face` block and **no** `font-display: swap` occurrence. *(served
  `GET /static/tokens.css`)*
- **R-M6HS-J41L** — scripts's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-M8XL-ANIZ** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest `LandingHandler`)*
- **R-MA5H-OF9O** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`)*
- **R-MBDE-270D** — `etc/nginx.conf` contains a `location /srv/scripts/static/` block
  whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;`; the existing exact landing,
  bearer prefix, feed-denial, and PRM locations are unchanged. *(fragment grep
  against `etc/nginx.conf`, mirroring the existing R-NGNX tests)*
</content>
