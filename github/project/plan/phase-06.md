# Phase 6 — The landing page and nginx fragment

*Realizes design Decision 6 (landing page + nginx fragment). Depends on Phase 1
(the Spec `Handlers` hook and `etc/manifest.env`) and Phase 5 (the `/pr` route the
fragment 404s at the public mount).*

## What gets built

`internal/web/` — the landing seam copied from `gmail/internal/web`, minus any
consent: `embed.go` (`//go:embed landing.html` + `static`), `web.go`
(`LandingHandler(service, version)` and `StaticHandler()`), `landing.html`, and
`static/` (the Carbon `tokens.css` and the woff2 fonts). Wired in `Handlers`:
`GET /{$}` → landing, `GET /static/` → assets.

`etc/nginx.conf` — the path-routed location fragment (D6), literal
`MOUNT=/srv/github/` / port `3203`: open PRM well-known; `= /srv/github/`
session-gated landing; `/srv/github/static/` session-gated; `/srv/github/` prefix
bearer-gated (covers `/mcp`) with the identity-hygiene + 429 `error_page` block;
`= /srv/github/pr` → `return 404`; and **no** `/srv/github/feed` block.
`internal/web/nginx_test.go` asserts the fragment's shape over the shipped file.

Observable end state: a browser at the mount root sees the name+version page; the
fragment gates and routes as specified and exposes no feed and no public `/pr`.

## Done when

All hold on identical repo state, from `github/`:

- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0; `gofmt -l .`
  empty; `go vet ./...` clean.
- Clearly-named offline tests cover and pass for `R-EVZ3-VXJZ` (`GET /` → 200 HTML
  containing `github` and the running version), `R-EX70-9PAO` (`/static/tokens.css`
  → `text/css`; a `.woff2` → `font/woff2`; traversal/unknown → 404), and
  `R-EYEW-NH1D` (the `nginx_test.go` fragment assertions: session-gated root,
  bearer-gated prefix, `= /srv/github/pr` returns 404, PRM open, and **no**
  `/srv/github/feed` block) — each id named in a test.
- The nginx fragment check is scoped to the shipped `etc/nginx.conf` (not the
  `project/` docs): `grep -c 'srv/github/feed' github/etc/nginx.conf` returns `0`.
