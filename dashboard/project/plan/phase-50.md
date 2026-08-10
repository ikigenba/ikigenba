# Phase 50 — The shell templates, the home directory, and the install page

*Realizes design Decision 1 (topology — the `/install` slice), Decision 5 (home composition), and Decision 40 (install page) — ids R-VH1G-HX9K, R-VI9C-VP09, R-VJH9-9GQY, R-VVO9-365W, R-VWW5-GXWL. Depends on Phase 49.*

The shell becomes real markup and the first two pages adopt it:

- A shell partial (`ui/html/partials/shell.tmpl` defining `shell_head`,
  `shell_header`, `shell_footer` per D10) joins the `template.ParseFS` set.
  The header takes the current page's identity (for `aria-current`) and the
  owner (identity controls render only when present); the footer takes the
  version string (D9).
- `ui/html/index.html`'s logged-in branch is rewritten to the D5 home: the
  shell frame around one `MCP Services` section of `.rows` service rows
  (name → mount link; mono MCP URL + bordered Copy button). The logged-out
  branch keeps the sign-in wall unchanged. `handleIndex` stops populating any
  connect-agent data the home no longer shows and gains the version field the
  footer needs.
- `GET /install` is registered (`routes.go`) with `handleInstall`
  (session-gated, `303 → /` when signed out) rendering the new
  `ui/html/install.html`: the D40 composition (lede with `IKIGENBA_TOKEN`
  inline code; stacked Claude Code / Codex subsections with snippet boxes and
  flush copy controls).
- Tests whose ids left the design are deleted with their behaviors:
  R-DB02-LND7 (in `index_test.go`), R-DB15-INST
  (`landing_composition_test.go`), R-VTIE-IUFA / R-VUQA-WM5Z
  (`banner_chrome_test.go`), and R-X506-GK1V (the landing metrics-tile
  test). Retained-id tests whose markup assertions changed (R-OF1Q-VEDC,
  R-OHHJ-MXUQ, R-XO4W-LKAI, R-DB14-SOUT, R-DB12-LINK, R-OG9N-9641,
  R-X682-UBSK) are updated against the new markup, keeping their ids.

The profile, metrics, authorize, and show-once pages still wear their old
markup after this phase; they adopt the shell in Phase 51.

**Done when:**
- R-VH1G-HX9K, R-VI9C-VP09 — `/install` renders with a session and redirects
  `303 → /` without one — each covered by a tagged test.
- R-VJH9-9GQY — the logged-in home contains the `MCP Services` heading and no
  install snippet — covered by a tagged test.
- R-VVO9-365W, R-VWW5-GXWL — the install page's composition and PAT lede —
  each covered by a tagged test.
- `grep -rn 'R-DB02-LND7\|R-DB15-INST\|R-VTIE-IUFA\|R-VUQA-WM5Z\|R-X506-GK1V' --include='*_test.go' .`
  from `dashboard/` returns nothing.
- The dashboard suite is green per design Conventions.
