# Phase 65 — `file_delete` and `rmdir` tools

*Realizes design Decision 41 (per-file and per-directory deletion). Depends on the
surface facts in Decisions 13, 20, 21, 25, and 38, which this phase brings into
conformance with the two new tools.*

Two new **mutating** MCP tools in `internal/mcp`, over `internal/sites`:

- `file_delete(site, path)` — removes one regular file: confine `path`
  (`validation` on escape), require an existing **file** (`validation` if it is a
  directory, `not_found` if absent), commit one `FileChange{Path, Delete:true}`
  through the plane, then remove the file from the served copy. Commit-then-apply
  and repos error mapping are D33's. Result `{deleted, site}`.
- `rmdir(site, path)` — removes a directory and every file under it: confine
  `path`, require an existing **directory** (`validation` on a file, `not_found`
  if absent), commit **one** batch of `Delete` changes covering every file under
  the prefix (a locally-only empty directory commits nothing), then `RemoveAll`
  the subtree. Result `{removed, site, files}`.

On a site with a non-empty publish root both tools' committed paths carry the
root prefix (D38). Both are **structured** tools (D25) with the output schemas
`{deleted:string, site:string}` and `{removed:string, site:string,
files:integer}`, and their failures speak the closed vocabulary. The surface
facts that reference the tool set are updated with them: the `tools/list` count
(D13, now 19), the file-tool listing (D20), the guide/model text and its "19
tools" line (D21), and the structured-tool tables and error-mapping (D25).

**Done when:**

- R-FOU8-MX0M — `file_delete` commits one deletion, removes the file, records the
  sha (checked by reading the directory, not the tool result).
- R-FQ25-0ORB — `file_delete` on a directory path is `validation` with zero repos
  calls; R-FRA1-EGI0 — on a missing file is `not_found` with zero repos calls.
- R-FSHX-S88P — a failed `file_delete` commit leaves the file present and the sha
  unchanged; R-FTPU-5ZZE — repos unreachable → `source_unavailable`, a `400` →
  `internal`.
- R-FUXQ-JRQ3 — `rmdir` removes a whole subtree in one commit (change set = every
  file under the prefix, `files` count correct, other files untouched);
  R-FW5M-XJGS — `rmdir` on a file path is `validation` with zero repos calls;
  R-FXDJ-BB7H — a failed `rmdir` commit leaves the subtree in place.
- R-FYLF-P2Y6 — on a site with `path "public"`, `file_delete`/`rmdir` commit their
  deletions at the root-prefixed path while removing locally at the unprefixed
  served path.
- The updated surface pins pass: R-Z8DD-BL71 asserts **19** tools including
  `file_delete` and `rmdir`; R-CXDB-6TRC / R-CYL7-KLI1 include both new tools with
  their `outputSchema`s; the guide/model reflects them.
- `cd sites && go build ./... && go vet ./... && gofmt -l . && go test ./...` is
  green.
