# Phase 06 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 8. Touches `internal/web/landing.html`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (`StaticHandler` is already wired at `GET /static/` in
`cmd/notify/main.go`). Independent of all other open work.*

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than notify's own: `landing.html` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/notify/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load notify's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them.

In **`internal/web/landing.html`**:
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/notify/static/tokens.css`,
  notify's own copy). Do **not** thread the handler's unused `AssetPath` field
  into the template — the document-relative link needs no Go change (design
  *Rejected*).
- Add two font preloads in `<head>`, beside the stylesheet link:
  ```html
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/space-grotesk.woff2">
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/ibm-plex-sans.woff2">
  ```
  `crossorigin` is mandatory; the document-relative `href` resolves to the same
  URL as the new `@font-face` `src`. Do not preload the mono family.

In **`internal/web/static/tokens.css`** (all four `@font-face` blocks):
- `font-display: swap` → `font-display: optional`.
- `src: url('/static/fonts/X.woff2')` → `url('fonts/X.woff2')`.

In **`etc/nginx.conf`**, add a session-gated static location (mirroring the
existing `= /srv/notify/` session gate and wiki's `/srv/wiki/static/`):
```nginx
location /srv/notify/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/notify/` landing location, the bearer-gated `/srv/notify/`
prefix, the unauthenticated PRM bootstrap location, and the `@notify_authn_500`
rate-limit fallback unchanged. (notify is a consumer and serves no `/feed`, so
there is no feed-denial location to preserve.)

**Update the superseded assertions (keep ids):**
- `web_test.go` **R-ASST-5L2V** (`TestLandingReferencesOnlyLocalStaticAssets`):
  change the asserted body substring from `/static/tokens.css` to
  `href="static/tokens.css"` (the link now points at notify's own copy,
  service-relative — D3's original intent, corrected form). Keep the
  cross-service/external forbidden-URL checks (`http://`, `https://`, `dashboard`).
- `web_test.go` `TestTokensCSSUsesEmbeddedNotifyFontURLs` (untagged): change the
  asserted `tokens.css` substrings from `/static/fonts/X.woff2` to `fonts/X.woff2`
  for all four fonts.
- `web_test.go` **R-ASST-3K9T / R-ASST-7M4W** (StaticHandler serves
  `/static/tokens.css` + the `/static/fonts/*.woff2` with correct content types)
  stay **unchanged** — they remain the real-substrate proof that notify serves its
  own assets. The untagged `TestEmbeddedAssetsDoNotReferenceExternalRuntimeOrigins`
  also stays valid (relative paths introduce no external origin).

**Done when:** the suite is green (per design *Conventions*: `cd notify && go build
./...`, `cd notify && go vet ./...`, `cd notify && gofmt -l .` (no output), `cd
notify && go test ./...`, and `bin/check-migrations notify` all succeed with zero
failures) and these ids are covered by clearly-named tests:

- **R-8JS0-IQDX** — notify's embedded `tokens.css` contains `font-display: optional`
  in every `@font-face` block and **no** `font-display: swap` occurrence. *(served
  `GET /static/tokens.css`)*
- **R-8KZW-WI4M** — notify's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-8M7T-A9VB** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest `LandingHandler`)*
- **R-8NFP-O1M0** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`)*
- **R-8ONM-1TCP** — `etc/nginx.conf` contains a `location /srv/notify/static/`
  block whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;`; the existing exact landing,
  bearer prefix, PRM bootstrap, and rate-limit fallback locations are unchanged.
  *(fragment grep against `etc/nginx.conf`, mirroring the existing R-NGNX tests)*
