# Phase 14 — Origin-tagged file events (pull + reflection)

*Realizes design Decision 18, the pull + reflection slice (R-KPHR-R6WO,
R-KQPO-4YND). The write-emits-origin behavior is realized in phase 18 with the
write path. Depends on phase 11; independent of phases 12–13.*

Observable end state:

- `internal/dropbox/events.go`: `FileEvent` gains an `Origin string` field and
  `filePayload` gains an `origin` JSON field. The `Events` registry `Sample`
  filePayload sets `origin`, so the reflection tool's emitted JSON Schema **and**
  worked example both carry it (they share the one Sample). The three event
  descriptions note the field and its two forms (a writing client id, or the
  sentinel `dropbox`).
- The download apply path (`internal/dropbox/service.go` / `sync.go` —
  `applyUpsert`/`applyRename`/`applyDelete`) sets `FileEvent.Origin` to the
  sentinel **`dropbox`** for every pulled change.
- `origin` is purely additive; no migration; existing consumers ignore it.

**Done when:** the suite is green (design Conventions commands, from `dropbox/`)
and:

- R-KPHR-R6WO is covered by a test asserting a pulled `list_folder` change,
  applied by the download engine, emits an event whose `origin` is `dropbox`
  (captured on the recording sink).
- R-KQPO-4YND is covered by a test asserting the `reflection` tool's `file.*`
  type detail includes `origin` in both the JSON Schema and the worked example.
