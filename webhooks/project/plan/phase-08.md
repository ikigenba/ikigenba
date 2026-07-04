# Phase 8 — Human landing page (web package + routes)

*Realizes design Decision 9 (human landing page). Depends on Phase 6 (composition root).*

Add the suite-standard human landing page to webhooks: a new self-contained
package `internal/web/` and two routes at the composition root. No domain code,
no MCP, no token logic — the browser-session gate is nginx's job (D7, Phase 9),
so in-process the routes are ungated.

Build `internal/web/`:

- `web.go` — `LandingHandler(service, version string) http.Handler` (200 with the
  service name + version at the exact root `/`, 404 for any other path) and
  `StaticHandler() http.Handler` (serves the embedded `static/` subtree).
- `embed.go` — `//go:embed landing.html` and `//go:embed static`; the `static`
  subtree via `fs.Sub`; `mime.AddExtensionType(".woff2", "font/woff2")` in
  `init`.
- `landing.html` — **byte-conformed to the cron canonical template**
  (`cron/internal/web/landing.html`): identical `<head>` (relative
  `href="static/tokens.css"`, the two `crossorigin` woff2 font preloads),
  identical CSS and `<main>` structure (top-left `Home` link, `eyebrow / h1 /
  description / dl(Service, Version, API)`), with only webhooks's per-service
  data differing — eyebrow `Inbound Webhooks`, the description line from D9, and
  the API cell `POST /mcp`.
- `static/tokens.css` + `static/fonts/*.woff2` — **verbatim copies of the cron
  canonical assets** (`tokens.css` md5 `63cb5c83f0db30a9646ea40ba5bb469e`; the
  shared woff2 set), so `font-display: optional` + self-served `src` come in with
  the copy.

Wire the routes inside the existing `Spec.Handlers` hook in
`cmd/webhooks/main.go`, **outside** `RequireIdentity`, beside the existing `/mcp`
and `/in/` mounts:

```go
rt.Handle("GET /{$}", web.LandingHandler(rt.Service(), rt.Version()))
rt.Handle("GET /static/", web.StaticHandler())
```

End state: `internal/web/web_test.go` exercises the handlers over `httptest` and
the embedded bytes; `cd webhooks && go build ./... && go vet ./... && go test
./...` is green; the new routes shadow none of `/mcp`, `/in/`, `/feed`, or the
PRM well-known.

**Done when:** the D9 Verification ids are each covered by a clearly-named test
and the suite is green —
- R-TMJH-V1NP — `GET /` → `200`, `text/html; charset=utf-8`, body contains
  `webhooks` and the injected version string;
- R-TNRE-8TEE — a non-root path (e.g. `GET /nope`) → `404`;
- R-TOZA-ML53 — `GET /static/tokens.css` → `200` non-empty, and a `.woff2` asset
  is served with `Content-Type: font/woff2`;
- R-TQ77-0CVS — the embedded `landing.html` carries the cron-canonical structure
  (relative `static/tokens.css` link, the `Home` link to `/`, and the
  `eyebrow / {{.Service}} / description / dl` markup with webhooks's eyebrow and
  description);
- R-TRF3-E4MH — the embedded assets eliminate the FOUT: `tokens.css` declares
  `font-display: optional` with self-served `src`, and `landing.html`'s `<head>`
  preloads the display + body fonts with `crossorigin`.
