# Phase 139 — Mount-compose the brand-mark logo src

*Realizes design Decision 80 (page shell — the R-2MYX-Y4PN slice only).*

The layout's brand-mark image (`share/www/layout.tmpl`) changes its `src`
from the base-relative `static/logo.png` to the mount-composed
`{{.Mount}}static/logo.png`, exactly as the stylesheet href already composes —
so the image resolves to the root `static/` route through the front door
instead of 404ing under the tier+scope `<base>`. The test tagged R-2MYX-Y4PN
is updated to the rewritten assertion: the `src` attribute equals the
configured mount + `static/logo.png` (a handler with mount `/srv/zzz/`
renders `src="/srv/zzz/static/logo.png"`), never the bare base-relative form.

**Done when:**

- The test tagged R-2MYX-Y4PN asserts the mount-composed `src` (threaded, not
  hardcoded) and passes.
- `grep -c 'src="static/logo.png"' share/www/layout.tmpl` returns 0.
- The suite is green: `go test ./...` from `wiki/` exits 0.
