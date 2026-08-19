# Phase 63 — Type-change-safe in-place reconcile

*Realizes design Decision 42 (the shared in-place reconcile is type-change-safe).*

`sites.Reconcile` — the helper every inbound door reuses (`sync` D34, the
`repos:push` materializer D35, `set_path` D38) — applies a desired file set to a
served directory so that, on success, the directory holds exactly the desired
paths and nothing else, **without failing on a file/directory type change**.
Deletes run before writes; a file whose removal empties its parent directory
prunes that directory; and a write clears any wrong-typed node sitting at its own
path or an ancestor before creating parents and writing. A former directory that
must become a file (and the reverse) reconciles cleanly instead of erroring with
`open <path>: is a directory` / `not a directory`. Reconcile stays **in place**
(no build-and-swap, per D35); a genuine filesystem error is returned unchanged so
each door keeps its own failure semantics.

**Done when:**

- R-FJYN-3U1U — directory→file transition reconciles, driven end-to-end through
  the assembled `set_path` MCP handler (a served `qr/index.html` becoming a
  regular file `qr` with the exported bytes, `qr/` gone, no error).
- R-FL6J-HLSJ — file→directory transition reconciles (a served regular file `qr`
  becoming `qr/index.html`, the file gone, no error), over a real `t.TempDir()`
  tree.
- R-FMEF-VDJ8 — deletes precede writes and emptied directories are pruned: a
  desired set that both empties an existing directory and writes a file at that
  directory's path completes without an `is a directory` error and leaves exactly
  the desired paths.
- `cd sites && go build ./... && go vet ./... && gofmt -l . && go test ./...` is
  green.
