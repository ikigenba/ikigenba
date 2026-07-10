# Phase 18 — The filesystem write API: Service methods + loopback routes

*Realizes design Decision 16 (all ids) and Decision 18's write-origin slice
(R-KO9V-DF5Z). Depends on phase 12 (`WriteFrom`/`Open`), phase 13 (directories),
phase 14 (`origin` field), and phase 15 (enqueue).*

Observable end state:

- `internal/dropbox/service.go` gains the mutating surface:
  `Write(ctx, path, src io.Reader, clientID) (FileRow, error)`,
  `Mkdir(ctx, path, clientID) error`,
  `Delete(ctx, path, clientID) (removed int, err error)`,
  `Move(ctx, from, to, clientID) error`. `Write` streams to the mirror via
  `WriteFrom`, then in **one tx** upserts the `files` row, appends the lifecycle
  event (`file.created` for a new path, `file.modified` for an existing one) with
  `origin` = the caller's client id, and enqueues the coalescing upload row;
  `Ring()` fires after commit. `Mkdir`/`Delete`/`Move` likewise mutate mirror +
  index and enqueue the matching op (`mkdir`/`delete`/`move`); a cross-path move
  emits `file.deleted(from)` + `file.created(to)` but enqueues a single `move`.
- `cmd/dropbox/main.go` mounts the new loopback routes through `Spec.Handlers`
  beside `/content`/`/list`, self-guarded by the identity-header check exactly
  like `/content`: `PUT /content` (streaming write), `DELETE /content`,
  `POST /mkdir`, `POST /move`, `GET /stat`. The write path reads `origin` from the
  `X-Client-Id` header. Every path is confined to the mirror root; an escaping
  path is rejected with a validation error and nothing is written.

**Done when:** the suite is green (design Conventions commands, from `dropbox/`)
and:

- R-K4RH-93AV — `PUT /content` streams a body into the mirror + index and a later
  `GET /content` returns the exact bytes with the reported size/hash.
- R-K5ZD-MV1K — a write whose resolved path escapes the mirror root is rejected
  and nothing is created inside or outside the root.
- R-K77A-0MS9 — `DELETE /content` removes a file and is idempotent (deleting an
  absent path succeeds, emits no spurious event).
- R-K8F6-EEIY — `POST /move` relocates a file in one call (`from` absent, `to`
  present) and emits `file.deleted(from)` + `file.created(to)`.
- R-K9N2-S69N — `GET /stat` returns metadata for a file and a directory, and
  `not_found` for an unknown path.
- R-KAUZ-5Y0C — a `PUT` to a new path emits `file.created` and a `PUT`
  overwriting an existing path emits `file.modified`.
- R-KO9V-DF5Z — a service write emits an event whose `origin` equals the caller's
  `X-Client-Id`.
