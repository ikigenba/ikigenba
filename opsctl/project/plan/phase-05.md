# Phase 05 — Setup provisions the DEFAULT app without a locations fragment

*Realizes design Decision 5 (`project/design/D05.md`) in full — all four ids are
unit-testable, no partial-Decision split. Touches `internal/opsctl/setup.go` and
its test, and `cmd/opsctl/main.go` (the `runSetup` flag wiring). Reuses
`mkdirAll755`, the `System` seam, and the existing setup-test substrate
unchanged.*

Give `Setup` a DEFAULT-app path so the apex app (dashboard) can be provisioned
without the box-fatal locations symlink. The observable end state:

- `SetupOptions` gains an `IsDefault bool`. `runSetup` (`cmd/opsctl/main.go`)
  gains a `--default` bool flag passed through to it (mirroring `init-box`'s
  `--default-app`). The cutover command is `opsctl setup dashboard --default`.
- **Contradiction guard, early:** if `IsDefault` is true and `Fragment` is
  non-empty, `Setup` returns a non-nil error naming both `--default` and
  `--fragment` and does nothing further (no tree, no unit, no nginx artifact).
- **Tree (step 2):** when `IsDefault` is true, create the same versioned tree a
  fragment service gets — `AppDir`, `BinDir`, `EtcDir`, `LibexecDir`, `CacheDir`,
  `BackupsDir`, and `StateDir` (0750) — and **no** `state/www` world-readable
  tree and **no** `web` group. (Dashboard serves over loopback; it shares no files
  with nginx, and it needs `backups/` for the deploy's pre-migration snapshot.)
- **nginx artifact (step 4):** when `IsDefault` is true, write **nothing** under
  `conf.d/` — no rendered fragment, no `conf.d/locations/<app>.conf` symlink. The
  apex block at `l.ApexBlockPath()` is owned by `init-box` (first render + cert)
  and re-rendered on each deploy (D4); setup must not touch it. Log a line noting
  the apex block is owned elsewhere.
- **Both non-DEFAULT paths are unchanged:** the worker path (empty `Fragment`,
  `IsDefault=false`) still creates the `conf.d/locations/<app>.conf` →
  `etc/current/nginx.conf` symlink; the fragment path still renders and writes the
  fragment (and runs `nginx -t`/reload unless `--defer-nginx`).

Structure the `IsDefault` case as a new first arm of the step-2 tree switch and
the step-4 nginx switch, ahead of the existing `Fragment == ""` / `Fragment != ""`
arms, so the two existing arms keep their exact current behavior.

Non-goals for this phase: no change to the worker or fragment provisioning, the
user/unit steps, the apex block render (init-box or D4's deploy render), or any
deploy/rollback/prune verb. No manifest reading in setup (the discriminator is the
flag — setup runs before any release exists, so there is no `etc/current/manifest.env`
to read).

**Done when** the suite is green — `GOWORK=off go build ./...` succeeds and
`GOWORK=off go test ./...` passes from `opsctl/` — and these ids are each covered
by a clearly-named test (temp `OPSCTL_ROOT` + the fake `System`, the existing
setup-test substrate):

- **R-CIUC-KW66** — setup with `IsDefault=true` (no `Fragment`) leaves the
  `/opt/<app>` tree present (`AppDir`, `BinDir`, `EtcDir`, `LibexecDir`,
  `CacheDir`, `BackupsDir`, `StateDir`) and the unit written + enabled
  (`System.EnableUnit(app+".service", false)` recorded), with **no** file or
  symlink at `l.FragmentPath()` **and no** file at `l.ApexBlockPath()`.
- **R-CK28-YNWV** — setup with `IsDefault=false` and empty `Fragment` leaves
  `l.FragmentPath()` a **symlink** targeting `l.ActiveNginxConf()` (the D01 worker
  behavior, unchanged).
- **R-CLA5-CFNK** — setup with a non-empty `Fragment` (and `IsDefault=false`)
  leaves `l.FragmentPath()` a **regular file** equal to `renderFragment(src,
  Port)`, and invokes the fake `System.NginxTest`/`NginxReload` (unless
  `DeferNginx`) — unchanged.
- **R-CMI1-Q7E9** — setup with `IsDefault=true` **and** a non-empty `Fragment`
  returns an error naming both `--default` and `--fragment`, with no file/symlink
  at `l.FragmentPath()` and none at `l.ApexBlockPath()`.
