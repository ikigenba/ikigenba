# Phase 22 — Content-plane holder: `GET /run-content` + `content_url` on `run_fs_list`

*Realizes design Decision 25 (the content-plane holder). Depends on Phase 17.*

Add `Service.RunContentHandler() http.Handler` in `internal/script/content.go`:
`run_id` validated as a single clean path element, the file resolved through
the existing `resolveWithin` confinement, served with `http.ServeContent`;
every failure (invalid/unknown run id, absent path, directory, escape) a bare
404. Mount it loopback-only in `registerRoutes`
(`rt.HandleLoopback("GET /run-content", …)`). `FileEntry` gains
`ContentURL string \`json:"content_url,omitempty"\``, populated by the MCP
layer for non-directory `run_fs_list` entries from a `contentBase` threaded
through revised `Tools(svc, contentBase)` / `NewHandler(svc, contentBase, rt)`
signatures (`registry.BaseURL("scripts")` at the composition root); the
`run_fs_list` output schema and tool description grow the `content_url`
contract. Wire surface stays 18 tools.

**Done when:**

- R-IIQS-PDUK — a named test proves `GET /run-content` for an existing run
  file returns 200, byte-identical body, correct `Content-Length`, and an
  extension-derived `Content-Type`.
- R-IJYP-35L9 — a named test proves unknown run id, separator-bearing run id,
  absent path, directory path, and escaping path each return 404 with no
  path echo and never 500.
- R-IMEH-UP2N — a named test proves non-directory `run_fs_list` entries carry
  a `content_url` on the registry-resolved base whose query round-trips run id
  + relative path (subdirectory case included), directories carry none, and
  fetching a returned URL against the mounted handler yields the file's bytes.
- R-INME-8GTC — a named test proves `run_fs_list`'s declared `outputSchema`
  includes `content_url` on its entry items and the result still deep-equals
  its mirrored text block.
- The scripts suite is green per design Conventions.
