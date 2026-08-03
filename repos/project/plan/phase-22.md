# Phase 22 — A session carries its correlation id, so the outcome event stays on the chain

*Realizes design Decision 12 (session correlation continuity). Depends on Phase
21 only in the sense that both touch `internal/repos`; it may build before or
after it. It cannot build until the suite-level `eventplane` work has landed the
`correlation_id` outbox column and the new `Append` signature — that signature
change is what makes this phase's event call site fail to compile until it is
updated. The operator runs the appkit and eventplane loops first.*

The session becomes chain-aware end to end:

- **Schema.** A new migration created with `bin/create-migration repos <name>`
  adds `correlation_id TEXT NOT NULL DEFAULT ''` to `sessions` with an
  `ALTER TABLE … ADD COLUMN` — additive, existing rows preserved, no table
  rebuild. Every committed migration stays frozen.
- **Type and store.** `repos.Session` gains `CorrelationID string`;
  `Store.InsertSession` persists it and `scanSession` reads it back.
- **Capture.** `Runner.Enqueue` reads the ambient correlation id off its `ctx`
  (via the accessor appkit's correlation package exposes) and sets it on the
  `Session` it inserts. `SessionRequest` gains no field. Both paths into
  `Enqueue` — the webhooks consumer in `internal/repos/intake.go` and the MCP
  `start` tool in `internal/tools` — must hand it a live context.
- **Use — the run context.** The context a dispatched session executes under
  carries that session's stored `CorrelationID`, so the GitHub peer calls the run
  makes (queued comment, label transitions, token fetch, `pr_create`) go out on
  the chain rather than bare. This is what makes Phase 21's instrumented client
  useful for repos' largest source of outbound traffic.
- **Use — the completion contexts.** `AppendOutcome`'s closure stops discarding
  its `context.Context` and passes it to the outbox `Append`. Every place the
  runner completes work on a fresh background context — the cancel path, the
  completion path, the reaper success call, and `Recover` — derives that context
  carrying the session's stored `CorrelationID` instead of a bare
  `context.Background()`. The detachment itself stays: a cancelled caller still
  must not kill a running session.

Not in this phase: no chain root is minted anywhere. The reaper's periodic sweep
is the only self-started cycle and it does no recordable work (D12), so
`rec.StartRoot` is deliberately not called.

**Done when** (all commands from `repos/`):

- `R-BVPG-H7Y9` is covered by a test over a real temp-file SQLite with the full
  embedded migration set applied: a session inserted with a correlation id reads
  back with that exact id, and one inserted without reads back as the empty
  string rather than failing to scan.
- `R-BWXC-UZOY` is covered by tests driving **both** entry paths — the webhooks
  consumer handler and the MCP `start` tool — with a correlation id on the
  inbound context, asserting the stored session row carries exactly that id, and
  the empty string when the context carries none.
- `R-LM2I-ORUI` is covered by a test driving a dispatched run whose session row
  holds a known correlation id, with the GitHub peer's injected client recording
  each request: every request the run causes carries that id on its `Context()`,
  and a runner using a bare dispatcher/background context fails.
- `R-BY59-8RFN` is covered by a test driving a session to a terminal status
  through the runner's real completion path (real SQLite, real outbox, the
  runner's own detached background context), asserting the resulting outbox row's
  correlation id equals the session row's.
- `R-BZD5-MJ6C` is covered by a test invoking `Runner.Recover` on a boot context
  carrying no correlation id, over a database holding a `running` session with a
  stored id, asserting the outcome event carries that stored id — not empty, not
  a fresh one.
- The new migration is additive: `grep -lE 'ALTER TABLE sessions ADD COLUMN correlation_id' internal/db/migrations/*.sql` names exactly one file, and `grep -c 'DROP TABLE' ` over that same file prints `0`.
- The suite is green as design's *Conventions* define it: `go build ./...`, `go vet ./...`, `go test ./...` all clean and `gofmt -l .` empty.
