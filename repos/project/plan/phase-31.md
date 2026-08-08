# Phase 31 — The loopback commit API: put, delete, batch

*Realizes the write slice of design Decision 18 (R-JQWG-D4IU, R-JS4C-QW9J,
R-JTC9-4O08, R-JUK5-IFQX, R-JVS1-W7HM, R-JY7U-NQZ0) and the commit slice of
Decision 23 (R-JFXC-X6UL, R-JID5-OQBZ). Depends on Phase 30.*

`internal/repos/` gains the worktree-free commit path — `hash-object`,
a temporary `GIT_INDEX_FILE`, `read-tree`/`update-index`/`write-tree`,
`commit-tree`, then `ApplyRefUpdate` — behind `PUT /content`,
`DELETE /content`, and `POST /commit`, all committing to `main` only, all
attributing the author to the supplied `actor`, all bounded by
`REPOS_MAX_COMMIT_BYTES`, and all idempotent when the tree would not change.
With the write path in place the `push` event's payload and its trip over the
real `/feed` become provable, which is why D23's two commit-driven ids land
here.

**Done when:** the suite is green and these ids are each covered by a
clearly-named test —

- R-JQWG-D4IU — one `PUT` is exactly one commit with the right parent, author,
  committer, and message, and the bytes read back.
- R-JS4C-QW9J — a stale `rev` on `PUT` is 409 and `main` does not move.
- R-JTC9-4O08 — `DELETE` of a present path commits once; of an absent path
  returns 200, commits nothing, and publishes nothing.
- R-JUK5-IFQX — a batch of three puts and one delete is exactly one commit; a
  no-op batch commits nothing.
- R-JVS1-W7HM — the first write to an empty repository creates a parentless
  initial commit and `refs/heads/main`.
- R-JY7U-NQZ0 — a body over `REPOS_MAX_COMMIT_BYTES` is 413 and writes no blob
  or commit; one byte under succeeds.
- R-JFXC-X6UL — a commit publishes exactly one `push` event whose branch, sha,
  old sha, and actor match real git and the request.
- R-JID5-OQBZ — a subscriber on the real `/feed` receives that event with source
  `repos`, key `repos:push/code/demo`, and a non-empty correlation id.
