# Phase 66 — `sync` skips non-file mirror entries

*Realizes design Decision 34 (`sync` reconciles as one batch commit).*

The `sync` enumeration reads the `kind` discriminator dropbox's loopback `/list`
returns on every entry and includes **only file entries** in the desired set. The
mirror wire type (`internal/sites`, the `listPage`/`MirrorFile` structs) gains the
`kind` field it was dropping, and the `syncDesired` enumeration
(`internal/mcp/sync.go`) skips every non-file entry so a directory is never
`Fetch`ed. This removes the failure where `sync` fetched a directory entry (a
sub-directory, or the queried prefix row dropbox emits for the path itself) and
`/content` answered `404`, surfacing spuriously as `source_unavailable`.

**Done when:**

- R-G118-GMFK — with the fake `MirrorClient` returning a `kind:"dir"` entry (path
  equal to `source_path`) plus two file entries under it, `sync` builds a desired
  set of exactly the two files, makes **no** `Fetch` for the directory entry, and
  returns without a `source_unavailable` error.
- `cd sites && go build ./... && go vet ./... && gofmt -l . && go test ./...` is
  green.
