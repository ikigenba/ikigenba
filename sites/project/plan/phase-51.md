# Phase 51 — `sync` reconciles as one batch commit

*Realizes design Decision 34 (`sync` as one batch commit). Depends on Phase 50.*

`internal/mcp`'s `sync` verb keeps every step it has — argument validation, slug
derivation, the `not_found` refusal, the `source_path` stamp, the mirror
enumeration and fetches, the local walk — and changes only its reconcile: it
builds one change set (a write per desired file, a delete per existing path the
mirror no longer has), issues **one** `version.Commit`, records the sha, and then
applies the same set locally through the unchanged `sites.Reconcile`. An empty
change set makes no commit call. The returned `{slug, written, deleted}` counts
come from the change set.

**Done when:**

- R-EYKO-KH2V — three mirrored files against a directory holding two (one
  stale) yields exactly one `Commit` whose change set is three writes with the
  mirror's bytes plus one delete of the stale path, and no other repos call.
- R-EZSK-Y8TK — after that pass the directory holds exactly the three mirrored
  files with the mirror's bytes, the stale file is gone, and the row carries the
  commit's sha; a no-op re-sync records zero `Commit` calls and returns
  `{written: 0, deleted: 0}`.
- R-F28D-PSAY — with the commit made to fail, `sync` returns an error envelope,
  the directory's file set and every file's bytes are byte-identical to before
  (the stale file still present), and the row's sha is unchanged.
- The suite is green.
