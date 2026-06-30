# Phase 06 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 8. Touches `internal/web/landing.html`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (ledger has **no** separate `StaticHandler`; the one `LandingHandler`
serves both `/` and `/static/`, already wired at `GET /static/{file...}` in
`cmd/ledger/main.go`). Independent of all other open work.*

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than ledger's own: `landing.html` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/ledger/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load ledger's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them.

In **`internal/web/landing.html`**:
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/ledger/static/tokens.css`,
  ledger's own copy).
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
existing `= /srv/ledger/` session gate and wiki's `/srv/wiki/static/`):
```nginx
location /srv/ledger/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/ledger/` landing location, the bearer-gated `/srv/ledger/`
prefix, the unauthenticated PRM bootstrap
(`= /srv/ledger/.well-known/oauth-protected-resource`), and the
`@ledger_authn_500` re-emit helper unchanged.

**Update the two superseded assertions (keep their ids):**
- `web_test.go` **R-LAND-7G2H**: change the assertion from
  `href="/static/tokens.css"` to `href="static/tokens.css"` (the link now points
  at ledger's own copy — D1's original intent, corrected form).
- `web_test.go` **R-ASST-3T7V**: change the asserted `tokens.css` substrings from
  `url('/static/fonts/…')` to `url('fonts/…')`.
- `web_test.go` **R-ROUT-4P8Q** (`LandingHandler` serves `/static/tokens.css` with
  `text/css`) and **R-ASST-5W9X** (`LandingHandler` serves the four
  `/static/fonts/*.woff2` with `font/woff2`) stay **unchanged** — they remain the
  real-substrate proof that ledger serves its own assets.

**Done when:** the suite is green (per design *Conventions*: `cd ledger && go
build ./...`, `cd ledger && go vet ./...`, `cd ledger && gofmt -l .` (no output),
`cd ledger && go test ./...`, and `bin/check-migrations ledger` all succeed with
zero failures) and these ids are covered by clearly-named tests:

- **R-7AW0-4QF8** — ledger's embedded `tokens.css` contains `font-display:
  optional` in every `@font-face` block and **no** `font-display: swap`
  occurrence. *(served `GET /static/tokens.css`)*
- **R-7DBS-W9WM** — ledger's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-7EJP-A1NB** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest `LandingHandler`)*
- **R-7FRL-NTE0** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`)*
- **R-7GZI-1L4P** — `etc/nginx.conf` contains a `location /srv/ledger/static/`
  block whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;`; the existing exact landing,
  bearer prefix, PRM bootstrap, and `@ledger_authn_500` locations are unchanged.
  *(fragment grep against `etc/nginx.conf`, mirroring the existing R-NGNX tests)*
