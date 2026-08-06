# Phase 35 — Resolve the mirror root through `appkit/config`

*Realizes design Decision 28 (state paths from `appkit/config`).*

`cmd/dropbox` resolves dropbox's durable paths once at startup with
`config.Resolve("dropbox", "/srv/dropbox/", registry.MustPort("dropbox"),
os.Getenv)` and derives the mirror root from `cfg.DBPath`. `defaultMirrorPath`
takes the resolved DB path as an argument instead of reading `DROPBOX_DB_PATH`
itself, keeps `DROPBOX_MIRROR_PATH` as the explicit override, and no longer
contains a literal `tmp` fallback. The sync engine, uploader, write API, and
event surface are untouched and receive the same absolute root they receive
today.

The resolution is a pure function of an injected `getenv`, so its tests drive it
directly with a map environment: no temp dirs, no filesystem, no running suite.

**Done when:**

- R-L7TF-ZAFX is covered by a test asserting that with `IKIGENBA_ROOT=/opt` and
  no `DROPBOX_DB_PATH` or `DROPBOX_MIRROR_PATH`, the resolved mirror root is
  exactly `/opt/dropbox/state/mirror`.
- R-L91C-D26M is covered by a test asserting that an explicit
  `DROPBOX_MIRROR_PATH` is returned verbatim even when `IKIGENBA_ROOT=/opt` is
  also set.
- R-LA98-QTXB is covered by a test asserting that with `IKIGENBA_ROOT` absent
  and no overrides, the resolved mirror root is `tmp`-rooted, preserving the dev
  default.
- `cd dropbox && grep -rn '"\./tmp' cmd/ internal/` prints nothing.
- The suite is green: `cd dropbox && go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), and `go test ./...` all succeed.
