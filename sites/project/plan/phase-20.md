# Phase 20 — Landing layout rewrite + slug links

*Realizes design Decision 6 (landing page layout — sites-specific) and 19 (slug-as-link delta). Depends on Phase 18 (landing lists sites from `store.List`) and Phase 16 (the `siteURL` / `baseURL` machinery the link mirrors).*

Reshape the landing page to the sites-specific layout and make each slug a link
to its site. Markup + inline CSS in `share/www/landing.html` and the landing
handler in `cmd/sites/main.go`; no schema, no MCP, no nginx change.

- **`sites/share/www/landing.html`** — collapse to the top-to-bottom stack: home
  link, eyebrow `Static website host`, a single heading line carrying the service
  name and the version inline (`<h1 id="page-title">{{.Service}} <span
  class="version">{{.Version}}</span></h1>`), the reworded lead `Hosts file-backed
  static websites and serves them through the suite gateway.`, then the listing
  table with **no** `<h2>Sites</h2>`. Remove the big-`<h1>` scale (heading drops to
  the `28px`/`1.15` the old `.sites h2` used), the `Service / Version / API` `<dl>`
  (including the `POST /mcp` value), and the now-dead CSS (`dl`/`dl > div`/`dd` +
  responsive, `.sites h2`, the `dt` member of the shared label selector). `.version`
  gains an inline left margin and keeps its mono/uppercase/muted treatment.
  Each row's slug renders as `<a href="{{.URL}}">{{.Slug}}</a>`.
- **`cmd/sites/main.go`** — lift the `baseURL := strings.TrimSuffix(rt.ResourceID(),
  "mcp")` computation so both the MCP wiring and the landing handler share the one
  value (behavior-preserving for MCP). Add `URL` to the landing `siteRow` and fill
  it per row as `baseURL + <public|private> + "/" + slug + "/"` (segments
  `sites.PublicSeg`/`sites.PrivateSeg` per `Public`) — the same string
  `internal/mcp`'s `siteURL` produces for the `url` field.
- **Tests** (`cmd/sites`, rendering over the repo-real `share/www` via `appkit/web`,
  handler driven with `httptest`).

**Done when:** the sites suite is green (`cd sites && go build ./...`, `go vet
./...`, `gofmt -l .` prints nothing, `go test ./...`),
AND R-WKGI-FVFJ: a test renders `landing.html` and asserts the version string
appears **inside the same `<h1>` element** as the service name (one heading line)
and the rendered body contains **no** `POST /mcp` substring and no old
`Service`/`Version`/`API` `<dl>` field label;
AND R-WLOE-TN68: the rendered body contains `Static website host` and exactly
`Hosts file-backed static websites and serves them through the suite gateway.`,
and does **not** contain the old lead beginning `Sites hosts file-backed`;
AND R-WMWB-7EWX: a test drives `GET /{$}` (handler built with a fixed `baseURL`)
against a store seeded with a public site `X` and a private site `Y` and asserts
`X`'s slug is wrapped in an anchor with `href="<baseURL>public/X/"` and `Y`'s slug
in an anchor with `href="<baseURL>private/Y/"` (distinct per row).
