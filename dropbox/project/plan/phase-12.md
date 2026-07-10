# Phase 12 — Streaming byte I/O in the mirror + streaming read route

*Realizes design Decision 14 (streaming byte primitives + `ServeContent` read
route). Depends on phase 11 for the settled post-conversion tree; otherwise
mechanically independent.*

Observable end state:

- `internal/dropbox/mirror.go` exposes `WriteFrom(rel string, src io.Reader)
  (contentHash string, size int64, err error)` — streams `src` to a temp file in
  the destination dir through a fixed copy buffer (`io.Copy` over an
  `io.TeeReader` into the Dropbox block-SHA256 hasher), `fsync`s, `chmod`s to the
  private file mode, and atomically renames into place. The whole file is never
  resident. The prior `Write([]byte)` is removed (or reduced to a thin
  test-only shim over `WriteFrom(bytes.NewReader(...))`).
- `internal/dropbox/mirror.go` exposes `Open(rel string) (*os.File, os.FileInfo,
  error)` returning a seekable handle for the read route; `Mirror.Read([]byte)`
  is no longer used by the serve path.
- The `GET /content` handler (`internal/dropbox/content.go`) resolves the display
  path through the index (unchanged), then serves the mirror file with
  `http.ServeContent`, honoring `Range` (206 partial) and setting
  content-type/length. The private-guard behavior (404 on nginx identity headers,
  path confinement → 400) is unchanged.
- The MCP `get` tool's buffered, 25 MiB-capped base64 path is untouched.

**Done when:** the suite is green (design Conventions commands, from `dropbox/`)
and:

- R-JV0A-6XDB is covered by a test asserting `WriteFrom` on a `> 8 MiB`
  multi-block reader yields byte-identical content, the correct streaming
  `ContentHash`, and the right size, with no partial file observable (atomic
  rename).
- R-JW86-KP40 is covered by a test driving `GET /content` with a `Range: bytes=`
  header, asserting 206 and the exact requested slice, and a full GET asserting
  200 and the whole file.
- R-JXG2-YGUP is covered by a test round-tripping a `64 MiB` file
  (`WriteFrom` → full `GET /content`) with matching content hashes; the
  `WriteFrom` signature takes an `io.Reader`.
