# Phase 06 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 8. Touches `internal/web/landing.html`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (`StaticHandler` is already wired at `GET /static/` in
`cmd/gmail/main.go`). Independent of all other open work.*

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than gmail's own: `landing.html` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/gmail/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load gmail's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them.

In **`internal/web/landing.html`**:
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/gmail/static/tokens.css`,
  gmail's own copy).
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
existing `= /srv/gmail/` session gate and wiki's `/srv/wiki/static/`):
```nginx
location /srv/gmail/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/gmail/` landing location, the bearer-gated `/srv/gmail/`
prefix, the `= /srv/gmail/feed` denial, and the PRM bootstrap location unchanged.

**Update the two superseded assertions (keep their ids):**
- `web_test.go` **R-LAND-7J2N**: change the assertion from
  `href="/static/tokens.css"` to `href="static/tokens.css"` (the link now points
  at gmail's own copy — D1's original intent, corrected form).
- `web_test.go` **R-ASST-7Y9Z**: change the asserted `tokens.css` substrings from
  `url('/static/fonts/…')` to `url('fonts/…')`.
- `web_test.go` **R-ASST-3T5V / R-ASST-5W7X** (StaticHandler serves
  `/static/tokens.css` + the four `/static/fonts/*.woff2` with correct content
  types) stay **unchanged** — they remain the real-substrate proof that gmail
  serves its own assets.

**Done when:** the suite is green (per design *Conventions*: `cd gmail && go build
./...`, `cd gmail && go vet ./...`, `cd gmail && gofmt -l .` (no output),
`cd gmail && go test ./...`, and `bin/check-migrations gmail` all succeed with zero
failures) and these ids are covered by clearly-named tests:

- **R-3X4A-Y8CI** — gmail's embedded `tokens.css` contains `font-display: optional`
  in every `@font-face` block and **no** `font-display: swap` occurrence. *(served
  `GET /static/tokens.css`)*
- **R-3YC7-C037** — gmail's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-3ZK3-PRTW** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest `LandingHandler`)*
- **R-40S0-3JKL** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`)*
- **R-41ZW-HBBA** — `etc/nginx.conf` contains a `location /srv/gmail/static/` block
  whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;`; the existing exact landing,
  bearer prefix, feed-denial, and PRM locations are unchanged. *(fragment grep
  against `etc/nginx.conf`, mirroring the existing R-NGNX tests)*
