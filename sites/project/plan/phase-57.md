# Phase 57 — Publish root, surface half: `create(path?)`, the `set_path` tool, the mapped write path, projection, and guide

*Realizes design Decision 38 (the publish root — the MCP-surface slice),
Decision 21 (the guide's generator example), and the rewritten pins in
Decisions 13, 20, and 25. Depends on Phase 56.*

The MCP surface speaks the publish root. `create` gains the optional `path`
argument (validated by `ValidatePath`, normalized value stored at birth); the
new small `set_path(slug, path)` tool re-materializes export-first per D38; the
mutating file tools and `sync` prefix every committed `FileChange.Path` with
the site's root; `renderSite` and every site `outputSchema` gain the `path`
field; and the embedded `guide.md` gains the static-site-generator worked
example. The tool table grows to 15 domain / 17 total (D13), so the existing
tagged tests for the rewritten pins are updated to the new expected surface:
R-Z8DD-BL71 (the 17-tool partition, D13), R-CW5E-T20N / R-CXDB-6TRC /
R-CYL7-KLI1 / R-0A69-6H6K (the projection keys, the structured-tool table with
`set_path`, and schema conformance, D25).

**Done when** the suite is green (design Conventions), the rewritten pins above
pass against their updated expectations, and each id below is covered by a
clearly-named, genuinely-asserting test:

- R-3V4I-867Z — `create` persists the normalized `path` (default `""`), returns
  it in the projection, and refuses an invalid path with `validation` creating
  nothing.
- R-3WCE-LXYO — `set_path` re-materializes export-first: exactly one `Export`
  call, the row's `path` and `repo_sha` updated, the served copy holding
  exactly the prefix-stripped subtree.
- R-3XKA-ZPPD — a `set_path` whose export fails changes nothing
  (`source_unavailable` for unreachable, `internal` for a repos-side
  rejection).
- R-3YS7-DHG2 — `set_path` refuses an invalid path (`validation`), an unknown
  slug (`not_found`), and an omitted `path` argument (`validation`) with zero
  repos calls.
- R-4003-R96R — `file_write`/`file_edit` on a rooted site commit
  `<path>/<site-relative>` while applying locally unprefixed; `mkdir` still
  makes no repos call.
- R-4180-50XG — `sync` on a rooted site batches root-prefixed writes and
  deletes into its one commit.
- R-43NS-WKEU — `set_path` onto a subtree absent from `main` succeeds and
  serves empty until a push lands content there.
- R-44VP-AC5J — the embedded guide carries the publish-root anchors
  (`set_path`, a `path:"public"` example, the whole-repository default, and
  the folder-not-in-URL statement).
