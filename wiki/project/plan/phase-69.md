# Phase 69 — Web read surface: foundation + the home page

*Realizes design Decision 42 (web foundation — the slice: package shape, route
table, `<base href>`, injected seams, shared layout — covering `R-WDA6-B2C8`)
and Decision 43 (the home / reset page). Also keeps Decision 39's `R-LAND-*` ids
green by re-pointing them at the new home page. Rewrites `internal/web`; touches
the composition root `cmd/wiki/main.go`. No migration, no LLM. Depends on Phase 68
(`Service.Orphans` for the OrphanLister adapter) and Phase 63 (the existing
`internal/web` package + embedded Carbon assets). `internal/web` is one package
built across Phases 69–71 (foundation+home, then ask, then subject).*

The placeholder landing card (Phases 63/66) becomes the **home / reset page**: a
search box, the orphan index, and a name+version footer. This phase builds the
read-surface **foundation** every later page reuses, plus the home page itself.

In **`wiki/internal/web`**:

- `NewHandler(service, version, mount string, opts ...Option) http.Handler` —
  builds a `*http.ServeMux` and returns it. Registers `GET /{$}` (home) and
  `GET /static/` (embedded assets, unchanged from Phase 63). The `/subject/…` and
  ask `?q` routes are added in Phases 71/70.
- The injected seam interfaces (`Asker`, `PageFinder`, `OrphanLister`,
  `Mentioner`) with `With…` options, and the `Ref{Href, Name}` and `SubjectView`
  types (consumed in 70/71). The handler reaches **only** `OrphanLister` this
  phase.
- A shared **layout** template emitting `<!doctype>` + `<head>` with
  `<base href="{{.Mount}}">` (the injected mount, e.g. `/srv/wiki/`), the
  `<base>`-relative `static/tokens.css` link, and a `{{block "main"}}` the page
  templates fill. All intra-surface links are written **relative, no leading
  slash**.
- **home page** (`home.tmpl`): a `<form action="" method="get" role="search">`
  with one text input named **`q`** and a submit; below it the orphan index —
  `{{if .Orphans}}` a list of `<a href="{{.Href}}">{{.Name}}</a>`, **omitted
  whole when empty**; a footer with the service **name** + **version**.

In **`cmd/wiki/main.go`** (`spec.Handlers`), replace the two bare
`web.LandingHandler` / `web.StaticHandler` mount lines with a single
`rt.Handle("/", web.NewHandler(rt.Service(), rt.Version(), wiki.Mount,
web.WithOrphanLister(orphanAdapter{svc}), web.WithAsker(asker), …))` — mounted
**ungated** (no `RequireIdentity`). Add the `orphanAdapter` mapping
`svc.Orphans` → `[]web.Ref{Href:"subject/"+wiki.Path(s), Name:s.Name}`.

This **supersedes** the Phase 63/66 landing card; `web_test.go`'s `R-LAND-*`
tests are re-pointed at the home page (name+version now in the footer).

**Done when:** the suite is green (per design *Conventions*) and these ids are
covered by clearly-named tests in `internal/web` via `net/http/httptest` with
stub seams (no DB/LLM/identity):

- **R-WDA6-B2C8** — a handler built with a **non-default** mount (`/srv/zzz/`)
  renders `<base href="/srv/zzz/">` on the home page — proving the base is the
  injected mount, not a hard-coded literal.
- **R-OMRY-L9O8** — the home page (`GET /`, no `q`) contains a `<form
  method="get">` whose action resolves to the base and whose single text input
  is named exactly `q`.
- **R-ONZU-Z1EX** — an `OrphanLister` stub returning two Refs renders both as
  `<a href="subject/…">Name</a>` in order — the home page calls `Orphans` and
  emits mount-relative subject links.
- **R-OP7R-CT5M** — an `OrphanLister` stub returning an empty slice renders the
  search form and **no** orphan `<nav>`/`<ul>` markup — the section is omitted
  whole.
- **D39 `R-LAND-PG01`, `R-LAND-NMVR`, `R-LAND-CARB`, `R-LAND-ROOT`,
  `R-LAND-UNGT`** remain covered against the new home page: 200 `text/html` at
  exact root; name+version present (footer), with a non-default version proving
  it renders `Version()`; embedded `tokens.css` referenced + served 200; `{$}`
  matches exact root only (no shadow of `/mcp`/`/health`/`/nope`); served ungated
  in-process with no identity header.
