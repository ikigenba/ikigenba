# Phase 50 — The write path: mutating file tools commit, then apply

*Realizes design Decision 33 (the write path) and Decision 32 (the context/attribution slice: R-EQ1D-W2W0). Depends on Phase 49.*

`internal/mcp` takes a `sites.VersionClient` alongside the store, layout, and
mirror client (a constructor parameter on `Tools`/`NewHandler`, exactly as the
mirror client became one in D13). `file_write` and `file_edit` build their single
`FileChange`, call `Commit` with the handler's live `ctx`, record the returned
sha via `Store.SetRepoSha`, and only then write the bytes to
`SiteDir(v, slug)`. `mkdir` makes no plane call at all. `ErrVersionUnavailable`
maps to `source_unavailable`; a repos-side rejection maps to `internal`.

This phase also updates the existing `internal/mcp` test harness to construct the
conforming repos server from Phase 49 (D20's ids keep pinning the surface
behaviors through the new seam and are not re-tagged).

**Done when:**

- R-EQ1D-W2W0 — driving a mutating MCP tool with `X-Client-Id: cli-alice` and a
  correlation id on the request context, the recording transport observes that
  correlation id on the `Context()` of **every** outgoing repos request and
  `cli-alice` as the call's attribution.
- R-ESH6-NMDE — `file_write` produces exactly one `Commit` carrying one change
  at `index.html` with the written bytes, **and** the file on disk with those
  bytes, **and** the returned sha on the row.
- R-ETP3-1E43 — `file_edit` commits the post-edit whole-file bytes (`beta`, not
  `alpha`, not a patch body) and the file on disk reads `beta`.
- R-EUWZ-F5US — `mkdir` produces zero repos requests and creates the directory;
  a following `file_write` into it commits the full site-relative path
  (`assets/app.css`).
- R-EW4V-SXLH — with the repos server failing the commit, `file_write` returns
  an error envelope, the new path does not exist, an overwritten file's bytes
  are byte-identical to before, and the row's sha is unchanged.
- R-EXCS-6PC6 — a closed repos server yields `source_unavailable`; a `400` to a
  well-formed commit yields `internal`.
- The suite is green.
