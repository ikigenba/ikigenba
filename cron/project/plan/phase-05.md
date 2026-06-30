# Phase 05 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 7. Touches `internal/web/landing.html`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (`StaticHandler` is already wired at `GET /static/` in
`cmd/cron/main.go`). Independent of all other open work.*

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than cron's own: `landing.html` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/cron/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load cron's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them.

In **`internal/web/landing.html`**:
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/cron/static/tokens.css`,
  cron's own copy).
- Add two font preloads in `<head>`, beside the stylesheet link:
  ```html
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/space-grotesk.woff2">
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/ibm-plex-sans.woff2">
  ```
  `crossorigin` is mandatory; the document-relative `href` resolves to the same
  URL as the new `@font-face` `src`. Do not preload the mono family (the two IBM
  Plex Mono weights).

In **`internal/web/static/tokens.css`** (all four `@font-face` blocks — Space
Grotesk, IBM Plex Sans, and the two IBM Plex Mono weights):
- `font-display: swap` → `font-display: optional`.
- `src: url('/static/fonts/X.woff2')` → `url('fonts/X.woff2')`.

In **`etc/nginx.conf`**, add a session-gated static location (mirroring the
existing `= /srv/cron/` session gate):
```nginx
location /srv/cron/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/cron/` landing location, the bearer-gated `/srv/cron/`
prefix, the `= /srv/cron/feed` denial, the PRM bootstrap location, and the
`@cron_authn_500` recovery unchanged.

**Update the superseded D3 assertions (keep their ids):**
- `web_test.go` **R-ASST-5X9Y**: change the landing-body assertion from
  `href="/static/tokens.css"` to `href="static/tokens.css"` (the link now points
  at cron's own copy — D3's original intent, corrected form). The no-external-
  reference checks (`https://`, `dashboard`, …) are unaffected.
- `web_test.go` **R-ASST-3V7W**: change the asserted `tokens.css` font-`src`
  substrings from `url('/static/fonts/…')` to `url('fonts/…')`. The same test's
  proof that `StaticHandler` serves `/static/tokens.css` with
  `text/css; charset=utf-8` stays unchanged.
- `web_test.go` **R-ASST-7Z2A** (StaticHandler serves the four
  `/static/fonts/*.woff2` with `font/woff2` and real `wOF2` payloads) stays
  **unchanged** — it remains the real-substrate proof that cron serves its own
  assets.

**Done when:** the suite is green (per design *Conventions*: `cd cron && go build
./...`, `cd cron && go vet ./...`, `cd cron && gofmt -l .` clean, `cd cron && go
test ./...`, and `bin/check-migrations cron` all pass) and these ids are covered
by clearly-named tests:

- **R-21DE-LOX3** — cron's embedded `tokens.css` contains `font-display: optional`
  in every `@font-face` block and **no** `font-display: swap` occurrence. *(served
  `GET /static/tokens.css`)*
- **R-22LA-ZGNS** — cron's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-23T7-D8EH** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest `LandingHandler`)*
- **R-2513-R056** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`)*
- **R-2690-4RVV** — `etc/nginx.conf` contains a `location /srv/cron/static/` block
  whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;`; the existing exact landing,
  bearer prefix, feed-denial, and PRM locations are unchanged. *(fragment grep
  against `etc/nginx.conf`, mirroring the existing R-NGNX tests in
  `internal/web/nginx_test.go`)*
