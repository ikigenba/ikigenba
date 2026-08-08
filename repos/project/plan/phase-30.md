# Phase 30 — The loopback read API: content, list, stat, archive

*Realizes the read slice of design Decision 18 (R-JJL2-2I2O, R-JKSY-G9TD,
R-JM0U-U1K2, R-JN8R-7TAR, R-JOGN-LL1G, R-JPOJ-ZCS5). Depends on Phase 29.*

`internal/repos/` gains the four read handlers and their service methods, and
the composition root mounts them through the shared chassis `rt.HandleLoopback`
guard: `GET /content` (streaming, `rev`-pinnable, `X-Repos-Rev`/`X-Repos-Blob`),
`GET /list` (recursive JSON listing over `git ls-tree`), `GET /stat` (metadata
plus a `content_url` assembled from `registry.BaseURL("repos")`), and
`GET /archive` (a tar of the tree at a ref, no VCS metadata). The error mapping
D18 states — 400/404/409/413/500 with a plain-text detail line — lands with them.

**Done when:** the suite is green and these ids are each covered by a
clearly-named test, all driving real git and real HTTP through `httptest` —

- R-JJL2-2I2O — exact bytes and a correct `X-Repos-Rev`; distinct 404s for an
  unknown path and an unknown ref.
- R-JKSY-G9TD — a stale `rev` yields 409 with no bytes; the current `rev` yields
  200.
- R-JM0U-U1K2 — recursive listing with sizes and blob shas, a `path` prefix
  filter, and an empty repository listing zero entries without error.
- R-JN8R-7TAR — `stat` metadata and a `content_url` that is byte-equal to the
  composed URL and dereferences to the same bytes; 404 when absent.
- R-JOGN-LL1G — the tar's member set is exactly the tree's files with identical
  bytes and no `.git` member; an older ref yields the older tree; an empty
  repository is 404.
- R-JPOJ-ZCS5 — every route 404s a request carrying `X-Forwarded-Proto`.
