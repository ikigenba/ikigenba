# Phase 63 — The write path commits: create, update, import, rename, delete

*Realizes design Decision 53 (write path through the version plane).
Depends on Phase 62.*

`prompt.Service` gains the `Version version.Client` field, injected at the
composition root over `registry.BaseURL("repos")`. `create` creates the
repository and batch-commits the initial layout; `update` commits only the
changed files and renames the repository when the name changes; `import`
batch-commits the fetched body as `prompt.md`; `delete` archives. `Get` reads
the live definition from the plane; `List` reads none. The content columns are
still written and read alongside — they are retired in Phase 69, not here.

**Done when:**

- `R-RLZ0-72UW` — create with a system prompt commits exactly
  `prompt.md`/`config.json`/`system.md`; create without one commits exactly two
  paths.
- `R-ROES-YMCA` — a config-only update commits exactly `["config.json"]`; a
  cleared system prompt commits a `Delete` entry for `system.md`; a name-only
  update commits nothing and calls `Rename` before the row write.
- `R-RPMP-CE2Z` — a first import commits `prompt.md` + the seeded
  `config.json` after a `Create`; a re-import commits exactly `["prompt.md"]`
  with no `Create` and leaves an owner-edited config uncommitted.
- `R-RQUL-Q5TO` — delete issues exactly one `Archive`, removes the row and its
  triggers, and leaves the prompt's runs present and readable.
- `R-RS2I-3XKD` — a failing `Commit` on create leaves no prompt row and returns
  an error naming the version plane; on update it leaves the row unchanged.
- `R-SGGH-RCE9` — `List` over three prompts issues zero `Read` calls and
  returns no definition content; `Get` issues exactly one `Read(nameKey,
  "main")` and returns the definition.
- `go test ./...` from `prompts/` is green; `gofmt -l .` is empty.
