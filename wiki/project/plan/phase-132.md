# Phase 132 — Mount-prefix the landing and selector redirect targets

*Realizes design Decision 76 (scoped web surface) — the slice R-HN4G-06FQ and R-HOCC-DY6F.*

The web handler's two emitted redirects — the bare-root landing 303 and the
selector 303 — currently target service-relative paths (`/private/…`,
`/{tier}/{to}/`). nginx never rewrites a path-only `Location` header, so at the
front door those redirects escape `/srv/wiki/` and 404 on the apex. End state:
both `Location`s are composed from the handler's configured mount
(`{mount}private/{scope}/` and `{mount}{tier}/{to}/`), matching how the cookie
path and `<base href>` already use it. Route patterns registered on the mux stay
service-relative; only the emitted `Location` values change. The work is
confined to `internal/web` and its tests.

**Done when:**

- R-HN4G-06FQ — `GET /` with cookie `wiki_scope=team-x` (a known scope) 303s to
  `{mount}private/team-x/` (with the configured mount,
  `/srv/wiki/private/team-x/`); with no cookie, or a cookie naming an unknown
  scope, it 303s to `{mount}private/default/` — never a bare `/private/…` that
  would escape the mount at the front door.
- R-HOCC-DY6F — `GET /private/s1/select?to=team-x` responds 303 to
  `{mount}private/team-x/` (with the configured mount,
  `/srv/wiki/private/team-x/`) with a `Set-Cookie` for `wiki_scope=team-x`; the
  public-tier selector action redirects to `{mount}public/{to}/`; and a plain
  scope-home or subject GET sets **no** cookie — only the selector action does.
- The suite is green per design's Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` empty, `go test ./...` from `wiki/`).
