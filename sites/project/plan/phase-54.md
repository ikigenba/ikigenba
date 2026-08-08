# Phase 54 — Seeding pre-plane sites into the version plane

*Realizes design Decision 37 (seeding). Depends on Phase 49 and Phase 48.*

`internal/sites/seed.go` gains `Seed(ctx, store, layout, vc) (int, error)`: it
takes `Store.ListUnseeded` in slug order and, per site, calls `vc.Create` with
the row's own owner (the only identity this callerless pass has, D32), walks
`SiteDir(site.Visibility, slug)` for its regular files, issues **one**
`vc.Commit` of the whole tree (skipping the call entirely when the tree is
empty), records the sha, and calls `Store.MarkSeeded`. It stops at the first
failure and returns it, leaving already-seeded rows seeded and the failed row at
`repo_seeded = 0`.

The pass writes nothing under `SITES_ROOT` and changes no column but `repo_sha`
and `repo_seeded`. Starting it at boot is Phase 55's.

**Done when:**

- R-FFN9-X9GL — two unseeded rows (one with `index.html` + `css/app.css`, one
  empty) yield a return of 2, a `Create` per slug, exactly one `Commit` for the
  first whose change set is those two files at their site-relative paths with
  their real bytes and no deletes, **no** `Commit` for the empty site, and
  `repo_seeded = 1` on both with the first carrying the commit's sha.
- R-FGV6-B17A — a second `Seed` returns 0 and produces zero repos requests; a
  site created normally afterwards is never revisited.
- R-FI32-OSXZ — snapshots of every path and its bytes under `SITES_ROOT` and of
  every row's slug/name/visibility/owner fields/`created_at`, taken before and
  after a `Seed` (including a run where the second site's `Commit` fails), are
  identical except for `repo_sha`/`repo_seeded`, and the failed site is still at
  `repo_seeded = 0`.
- The suite is green.
