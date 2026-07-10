# Phase 16 — Dropbox write client (upload / folder / delete / move)

*Realizes design Decision 17, the client slice (R-KJE9-UC77 hermetic;
R-KEIO-B98F, R-KFQK-P0Z4, R-KGYH-2SPT LIVE). Depends on phase 12 (streaming
`Open` for upload source). Prerequisite for the uploader (17).*

Observable end state:

- `internal/dropbox/client.go` gains the write half of the Dropbox API, on the
  same rpc/content hosts with the bearer:
  - `Upload(ctx, path, src io.Reader, size int64) (rev string, err error)` —
    `mode:overwrite`; a single `files/upload` under 150 MiB, switching to
    `upload_session/start|append_v2|finish` above it, streaming fixed chunks from
    `src` (never resident).
  - `CreateFolder(ctx, path) error` (`files/create_folder_v2`),
    `DeletePath(ctx, path) error` (`files/delete_v2`),
    `Move(ctx, from, to) error` (`files/move_v2`).
- A hermetic `client_test.go` addition drives these against an `httptest` fake
  (the existing pattern), asserting request shape.
- A `-tags live` smoke test (`//go:build live`) exercises the real Dropbox app
  folder with the suite refresh token (`DROPBOX_*` from `.envrc`), asserting the
  observable outcome via a follow-up `list_folder`. It is **not** part of the
  hermetic green suite.

**Done when:** the hermetic suite is green (design Conventions commands, from
`dropbox/`) **and** the live smoke passes (`go test -tags live ./internal/dropbox/`
with the `DROPBOX_*` secrets present), and:

- R-KJE9-UC77 (hermetic) — a test asserting `Upload` sends `mode:overwrite` for a
  small file and chunks a `> 150 MiB` source into `upload_session` calls (request
  shape against the fake; no network).
- R-KEIO-B98F (LIVE) — the smoke asserts `Upload(overwrite)` to the real app
  folder returns a `rev` and a follow-up `list_folder` shows the file with that
  rev.
- R-KFQK-P0Z4 (LIVE) — the smoke asserts a `> 150 MiB` file uploads via
  `upload_session` and is retrievable whole (byte count / content hash match).
- R-KGYH-2SPT (LIVE) — the smoke asserts `CreateFolder`, `DeletePath`, and `Move`
  each produce the observable folder / removal / relocation via `list_folder`.
