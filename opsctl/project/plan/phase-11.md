# Phase 11 — retire the `working/` segment from opsctl's served-tree model

*Realizes design Decision 7 (`project/design/D07.md`) — the two new ids
(`R-QFXB-VARQ`, `R-QEPF-HJ11`) are unit-testable, no split. Depends on phase 08
(the `ensureWWWPerms` helper + `Chmod` seam, unchanged here). Touches
`internal/opsctl/layout.go` (remove `WWWWorkingDir`), `internal/opsctl/www.go`
(the helper's tier-dir loop), `internal/opsctl/setup.go` (`WWWDirsFor` +
comments), and the affected tests in `provision_test.go` / `setup_test.go` /
`deploy_test.go`.*

Bring opsctl's on-box view of the sites `state/www` tree in line with the
service's two-tier (`public`/`private`) model by removing the retired `working/`
segment everywhere it is still enumerated. This is what unblocks
`opsctl deploy sites`: today `ensureWWWPerms` chmods `state/www/working`, which no
longer exists on the box, and the deploy aborts. The observable end state:

- **Layout.** `internal/opsctl/layout.go` no longer defines `WWWWorkingDir()`;
  `WWWDir`, `WWWPublicDir`, `WWWPrivateDir` remain, and the `WWWPublicDir` doc
  comment no longer describes "publish symlinks → working trees".
- **Helper.** `ensureWWWPerms` (`www.go`) setgids exactly `{WWWDir, WWWPublicDir,
  WWWPrivateDir}` — the `WWWWorkingDir` entry is gone — and self-guards each dir on
  existence so a legacy `working/` (or any absent tier) is skipped, never chmod'd,
  never an error.
- **setup.** `WWWDirsFor(root, "sites")` returns exactly `{WWWDir, WWWPublicDir,
  WWWPrivateDir}` (no `working` path); the setup comments describing the tree drop
  the `working/` step. Non-sites apps still get `nil` (unchanged).
- **Tests.** The provision/setup/deploy tests that assert the old three-tier shape
  are updated to the two-tier shape (no `working` dir created, no `working` chmod
  recorded).

Non-goals: no change to the `web` group / setgid mechanism itself (retained per
D7), no change to D8/D9's re-assert calls (they reuse the helper and its tier set
follows automatically), no touch of the DEFAULT or worker branches, and no removal
of the dead `stateWWWFragment` alias generator (out of scope — a separate
cleanup).

**Done when** the suite is green — `GOWORK=off go build ./...` succeeds and
`GOWORK=off go test ./...` passes from `opsctl/` — and these ids are each covered
by a clearly-named test:

- **R-QFXB-VARQ** — `WWWDirsFor("/opt", "sites")` returns exactly the three
  parents `{WWWDir(), WWWPublicDir(), WWWPrivateDir()}` (none ending in `/working`),
  and a `project/`-excluded grep over `internal/` for `WWWWorkingDir` and
  `www/working` returns no matches. Fails against today's `WWWDirsFor(sites)`
  (returns a `.../www/working` entry) and `layout.go` (defines `WWWWorkingDir()`).
  *(unit: direct `WWWDirsFor` call + a scoped grep)*
- **R-QEPF-HJ11** — after `Setup` of a served-tree app (`WWWDirsFor` non-empty,
  i.e. `sites`), the fake `System` records `ChownTree(app, "web", WWWDir())` **and**
  a `Chmod(d, 02750)` for each of `WWWDir`, `WWWPublicDir`, `WWWPrivateDir`, and
  records **no** `Chmod` of any path ending in `/working`, no
  `ChownTree(app, app, …)` over the www tree, and no `0755` www mode. Fails against
  today's helper (records `Chmod(WWWWorkingDir(), 02750)`). *(unit: temp
  `OPSCTL_ROOT` + fake `System`)*

The real-substrate proof that the deploy now completes on the box — a real
`opsctl deploy sites <version>` followed by an anonymous
`GET https://int.ikigenba.com/srv/sites/public/<published-site>/` returning 200
(the existing D8 live-box id `R-AXY7-K8GA`) — is operator-verified out-of-loop,
consistent with phases 09–10.
