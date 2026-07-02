# Phase 47 — ship emits the versioned-tier bundle opsctl stage expects

*Realizes design Decision 4 (version production: bundle) — the corrected,
versioned-tier bundle layout, id `R-201H-8XX0`. Supersedes the flat-bundle layout
built in Phase 32 (which realized the now-retired `R-P4CO-FY2L`); Phase 32 stays
as history. Depends on Phase 32.*

## What gets built

`bin/ship` (and its test `bin/ship.test.sh`).

Today `ship` assembles a **flat** bundle — the built binary as `<svc>`,
`nginx.conf`, `manifest.env`, and optional `share/…` at the tar root — but opsctl
`stage` requires the **versioned-tier** layout (`requireBundlePaths`:
`libexec/<svc>-v<full>`, `etc/v<full>/{nginx.conf,manifest.env}`, `share/v<full>/`),
so the produced bundle cannot be staged. This phase makes `ship` emit the tiered
layout D02/D04 now specify, so a shipped bundle round-trips through `stage`.

After this phase, `ship`'s `tar.gz` (still named `<svc>-v<full>.tar.gz`, still the
same binary stamp and scp/box-command output) contains, for the full version
`v<full>`:

- `libexec/<svc>-v<full>` — the built static binary as a file (ship already builds
  the artifact at this path; it stops copying it flat to `<svc>`).
- `etc/v<full>/nginx.conf` — the service's `<svc>/etc/nginx.conf`, byte-for-byte.
- `etc/v<full>/manifest.env` — the service's `<svc>/etc/manifest.env`, byte-for-byte
  (verbatim, not generated/stamped).
- `share/v<full>/…` — the contents of `<svc>/share/` when present, and an **empty**
  `share/v<full>/` directory when the service has no `<svc>/share/` (the tier is
  always present because opsctl `stage` requires it).

Nothing else about `ship` changes: the worktree build, version read/validation,
`+<sha>` stamping, bundle filename, early existence check, dry-run, scp, and the
printed `opsctl stage`/`deploy` commands are all unchanged. `bin/ship.test.sh` is
updated to assert the tiered layout (retagged from `R-P4CO-FY2L` to
`R-201H-8XX0`), including the empty-`share/v<full>/` case for a share-less fixture.

## Done when

All of the following hold on identical repo state, from the repo root:

- `bin/ship.test.sh` tagged `R-201H-8XX0` builds a bundle from fixture service
  trees (one **with** `<svc>/share/`, one **without**) and asserts, by
  listing/extracting the archive, that it contains: `libexec/<svc>-v<full>` (a
  file), `etc/v<full>/nginx.conf` and `etc/v<full>/manifest.env` byte-for-byte
  against the fixture sources, `share/v<full>/…` with the fixture's resources for
  the share-ful service, and a **present but empty** `share/v<full>/` for the
  share-less service — and that **no** flat `<svc>`/`nginx.conf`/`manifest.env`
  entry exists at the tar root. `grep -n 'R-201H-8XX0' bin/ship.test.sh` returns
  ≥ 1 line.
- `bin/ship.test.sh` exits 0.
- `bin/test` exits 0.
