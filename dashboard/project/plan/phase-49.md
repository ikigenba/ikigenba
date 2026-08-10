# Phase 49 — Static foundation of the wiki shell: system fonts, shell CSS, logo

*Realizes design Decision 8 (system font stacks) and the CSS slice of Decision 10 (the shared shell) — ids R-VKP5-N8HN, R-VLX2-108C, R-VS0J-XUXT.*

The embedded static layer becomes the wiki shell's foundation, ahead of the
template rewrite (Phase 50):

- `ui/static/tokens.css` re-points `--font-display`/`--font-body` to the
  `system-ui` stack and `--font-mono` to the `ui-monospace` stack, and loses
  its four `@font-face` blocks; no `url(` reference remains in it.
- `ui/static/fonts/` and its `.woff2` files are deleted; every
  `<link rel="preload" as="font" …>` line is removed from the template heads
  (`index.html`, `profile.html`, `metrics.html`, `oauth_authorize.html`).
- The wiki's logo asset is vendored in as `ui/static/logo.png` (copied from
  the wiki tree's served logo), reachable through the existing
  `staticHandler`.
- `ui/static/app.css` is rewritten to the shell idiom of D10: the shared
  bounding column on `header`/`main`/`footer` (`--layout-max-width`,
  `--layout-gutter`), `scrollbar-gutter: stable` on `html`, the header/nav/
  identity styles, the `.rows` list idiom, snippet boxes, bordered-neutral
  small buttons, the footer style, and the metrics panel grid. The sign-in
  wall rules (D7) are preserved; the avatar hover rule keeps
  `var(--accent-700)` (R-VVY7-ADWO); dead chrome rules (`.banner`,
  `.wordmark`, `.section-head`, tile and `.list`/`.row` surface chrome) are
  deleted.
- `internal/server/font_loading_test.go` is deleted — its ids
  (R-P97M-GIJ1, R-PAFI-UA9Q, R-PBNF-820F) left the design with D8's rewrite.

Existing templates still render with the old markup this phase; tests that
assert markup (not fonts) stay green. Visual regressions of old pages against
the new CSS are acceptable and short-lived — Phase 50 rewrites the templates.

**Done when:**
- R-VKP5-N8HN — served `GET /static/tokens.css` defines the system font
  stacks and contains no `@font-face` and no `url(` — covered by a tagged
  test.
- R-VLX2-108C — the embedded `ui/static` tree contains no `.woff2` and no
  served template head contains a font preload — covered by a tagged test.
- R-VS0J-XUXT — served `GET /static/app.css` sizes `header`/`main`/`footer`
  with `var(--layout-max-width)`/`var(--layout-gutter)` and sets
  `scrollbar-gutter: stable` on `html` — covered by a tagged test.
- `internal/server/font_loading_test.go` no longer exists;
  `grep -rn 'R-P97M-GIJ1\|R-PAFI-UA9Q\|R-PBNF-820F' --include='*_test.go' .`
  from `dashboard/` returns nothing.
- The dashboard suite is green per design Conventions (`go build ./...`,
  `go vet ./...`, silent `gofmt -l .`, `go test ./...` from `dashboard/`).
