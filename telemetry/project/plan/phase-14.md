# Phase 14 — The canonical landing page and its session-gated routing

*Realizes design Decision 11 (canonical landing page) and the landing/static
slice of Decision 6 (nginx fragment): R-6B96-6B5S, R-6CH2-K2WH, R-67LH-0ZXP,
R-68TD-EROE.*

End state: telemetry serves the suite's uniform landing page.

- `share/www/` exists, copied **verbatim from `crm/share/www/`** (the
  byte-identical suite cluster): `landing.html`, `static/tokens.css`, and the
  four vendored fonts under `static/fonts/`. `landing.html` then differs from
  the source in exactly three text values — `<title>` is
  `{{.Service}} · telemetry`, the eyebrow is `Forensic record store`, and the
  lead paragraph is the D11 sentence — and in nothing else.
- `cmd/telemetry/main.go` sets `WWW: true` on the Spec and registers the
  `GET /{$}` landing route rendering `landing.html` with
  `{{.Service}}`/`{{.Version}}` through `rt.WWW()`, exactly as D1/D11 sketch.
- `etc/nginx.conf` gains the session-gated human tier of D6: the exact-match
  `= /srv/telemetry/` landing block (session subrequest, login bounce, all four
  owner headers via service-prefixed variables, captured correlation id) and
  the `/srv/telemetry/static/` block (session-gated, correlation only, no
  identity). All existing fragment invariants keep holding — in particular the
  file still contains the substring `ingest` zero times.
- The committed `etc/manifest.env` is unchanged (`WWW` emits no manifest key);
  the existing D1 manifest test keeps passing.

**Done when:**

- R-6B96-6B5S — composed-service test: `GET /` over a real loopback listener
  answers 200 with the rendered landing page (service name `telemetry`, running
  version, eyebrow, Home link), byte-identical to the canonical crm template
  apart from the three text values.
- R-6CH2-K2WH — composed-service test: `GET /static/tokens.css` answers 200
  with a CSS content type and the committed bytes; a vendored font serves
  likewise.
- R-67LH-0ZXP — nginx content test: the session landing block matches D6's
  stated shape (exact-match, `_session-authn` + `@login_bounce`, four owner
  headers each from its own service-prefixed variable, correlation forwarded).
- R-68TD-EROE — nginx content test: the static block is session-gated, proxies
  to `/static/`, forwards correlation, and forwards no identity header.
- Each id above appears verbatim as a tag in a `*_test.go` file, and the suite
  is green per design Conventions (`go build ./...`, `go vet ./...`,
  `go test ./...` all clean from `telemetry/`).
