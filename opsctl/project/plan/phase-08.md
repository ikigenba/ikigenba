# Phase 08 — setup provisions the served `www` tree as `<app>:web`, setgid, via `ensureWWWPerms`

*Realizes design Decision 7 (`project/design/D07.md`) in full — both ids
(`R-AT2M-15HI`, `R-AUAI-EX87`) are unit-testable, no split. Introduces the shared
`ensureWWWPerms` helper that phases 09–10 reuse, and the `Chmod` seam method.
Touches `internal/opsctl/seam.go` (new `Chmod` seam method + `RealSystem` impl +
fake), a new `internal/opsctl/www.go` (the helper), `internal/opsctl/setup.go`
(the served-tree branch), and `setup_test.go`.*

Standardize the served `state/www` tree onto the `web` group with setgid
inheritance, defined once in a reusable helper. The observable end state:

- **Seam.** `System` gains `Chmod(ctx, path string, mode os.FileMode) error`
  (`RealSystem` runs `chmod`; the fake records `(path, mode)`), used to set the
  setgid bit `02750` on tier dirs.
- **Helper.** New `func (o *Opsctl) ensureWWWPerms(ctx, app string, l Layout)
  error` in `internal/opsctl/www.go`: returns nil when `l.WWWDir()` does not exist;
  otherwise records `ChownTree(app, "web", l.WWWDir())` then `Chmod(d, 02750)` for
  each existing tier dir (`WWWDir`, `WWWWorkingDir`, `WWWPublicDir`,
  `WWWPrivateDir`). Idempotent and self-guarding.
- **setup.** In the served-tree branch (`len(opts.WWWDirs) > 0`), create the dirs
  `0750` as today, then call `ensureWWWPerms` **in place of** the current
  world-readable `ChownTree(app, app, l.WWWRoot())`. Remove the now-dead
  `web`-group www block and its `EnsureSystemGroup("web")` from the no-fragment
  (worker) branch — group creation is init-box's job (D6/phase-07); setup assumes
  the group exists and fails loudly via the chown if it does not.
- Every non-served-tree path is unchanged: the DEFAULT-app branch (D5), the worker
  symlink, the fragment file + `nginx -t`/reload, and the user/unit steps.

Non-goals: no `web`-group creation or nginx-membership (phase-07 owns it), no
`deploy`/`restore` change (phases 09–10 reuse the helper), no change to the
DEFAULT or non-served branches, and no world-readable www mode anywhere.

**Done when** the suite is green — `GOWORK=off go build ./...` succeeds and
`GOWORK=off go test ./...` passes from `opsctl/` — and these ids are each covered
by a clearly-named test (temp `OPSCTL_ROOT` + the fake `System`, existing
setup-test substrate):

- **R-AT2M-15HI** — after `Setup` of a served-tree app (`WWWDirsFor` non-empty,
  i.e. `sites`), the fake records a `ChownTree(app, "web", WWWDir())` **and** a
  `Chmod(d, 02750)` for each existing tier dir, and records **no**
  `ChownTree(app, app, …)` over the www tree and no `0755` www mode. Fails against
  today's fragment branch (records `ChownTree(app, app, WWWRoot())`, no setgid).
- **R-AUAI-EX87** — after `Setup` of a no-served-tree app (`WWWDirsFor` empty),
  the fake records **no** `ChownTree(_, "web", _)` and **no** www `Chmod`, and no
  `state/www` tree is created; the worker/fragment nginx artifact behavior is
  unchanged.
