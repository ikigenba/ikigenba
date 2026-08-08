# Phase 36 — The authoring verbs become commits

*Realizes design Decision 37 (the write path). Depends on Phase 35.*

`internal/script/service.go` grows the version-plane leg on every authoring
verb: `create` derives the key, inserts the row, creates the repository, commits
`main.py`, stamps `repo_seeded_at`, and deletes the row again if the plane
fails; `update` commits a supplied body and renames the repository when the
derived key changes (and does neither when nothing changed); `import` commits
the fetched mirror bytes, creating the repository only for a new row; `delete`
asks the plane to archive and never blocks on its failure; `get` reads `main.py`
at `main` for the returned `body` while `list` makes zero plane calls and
returns empty bodies. The `body` column is still written (Decision 40 retires
it) and is never read to answer `get`.

**Done when:** the suite is green and each of these ids is covered by a genuine
test:

- R-2BHP-4U4Y — `create` issues `Create` then `Commit` on the derived key with
  exactly `{"main.py": body}` and the `scripts:<id>` attribution, and stamps the
  row.
- R-2CPL-ILVN — a plane failure during `create` yields the `source_unavailable`
  structured error and leaves zero rows.
- R-2DXH-WDMC — `update` issues `Commit` only for a changed body, `Rename` only
  for a changed slug, and neither when neither changed.
- R-2F5E-A5D1 — `import` creates once, commits the fetched bytes, and re-import
  upserts the one row with no second `Create`.
- R-2GDA-NX3Q — `delete` archives through the plane and still succeeds (row
  gone) when the plane fails.
- R-2HL7-1OUF — `get` returns the plane's `main.py` content, not the stored
  column.
- R-2IT3-FGL4 — `list` makes zero plane calls and returns empty bodies with all
  other fields present.
