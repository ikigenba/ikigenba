# Phase 34 — Version-plane schema and the repository name key

*Realizes design Decision 35 (git-backed script trees: schema + naming).*

`internal/db/migrations/` gains one **additive** timestamped migration (minted
with `bin/create-migration scripts version_plane`, never hand-numbered) adding
`scripts.name_key`, `scripts.repo_seeded_at`, `runs.repo_sha` and the partial
UNIQUE index on `scripts(name_key)`; no table is rebuilt and no row is dropped
(`scripts/state/` is live customer data). `internal/script/namekey.go` holds the
total slug rule and `deriveNameKey(ctx, excludeID, name)` with numeric
suffixing arbitrated by the real index, plus `RepoKey`. `Script` gains
`NameKey`/`RepoSeededAt` and `Run` gains `RepoSha` (wire key `repo_sha`), with
the store reading and writing them; no verb behavior changes yet.

**Done when:** the suite is green (`cd scripts && go build ./... && go vet ./...
&& gofmt -l . && go test ./...`) and each of these ids is covered by a genuine
test:

- R-20IL-OWGP — the migrated schema carries `name_key`, `repo_seeded_at`,
  `repo_sha` and the UNIQUE index while still carrying `body`/`source_path`, and
  every previously committed migration is byte-identical to its frozen body.
- R-21QI-2O7E — a pre-existing row survives the migration with its `body` and
  identity columns intact and its new columns NULL.
- R-22YE-GFY3 — `slugify` discriminates on punctuation, non-ASCII, the 48-byte
  cap, and the all-punctuation fallback; `RepoKey` prefixes `scripts/`.
- R-246A-U7OS — cross-owner name collisions yield `-2`/`-3` suffixes and the real
  SQLite UNIQUE index rejects a duplicate `name_key`.
