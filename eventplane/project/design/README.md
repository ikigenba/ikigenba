# eventplane — Design

**Authority: shape and its proof.** This document set owns *how* the current
work is built and *how each behavior is proven* — seams, interfaces, types,
naming, and the test strategy. Product owns the why and the promises; design
states their exact, checkable form and never re-declares the why. It uses
product's contractual constants (the envelope fields, the canonical key form,
the glob dialect, the correlation-id format and its header and envelope field
names) by value but does not own them. This is the single current statement of
the design, rewritten in place; construction history lives in git.

**Scope note — revision over a baseline.** This spec covers the routing
revision (D1–D4), the feed-guard removal (D5), and the correlation and
observation work (D6–D9). The as-built library — outbox atomicity, the SSE
transport and control frames, cursors and the epoch token, all four resync
reasons, reconnect backoff, retention, and the handler-return cursor gate
(nil/ErrSkip/stall) — is the baseline described in `eventplane/CLAUDE.md` and
verified in `project/research/research.md`. Decisions reference that baseline;
they never respecify it, and no Verification id below re-proves it.

## Requirement ids

Each Decision ends with a **Verification** list — the concrete behaviors that
decision requires. Every item carries a minted requirement id (`idgen` prefix
`R`, shape `R-` + two four-character groups): a stable,
unique handle for that one behavior. The ids live inline in those lists and
nowhere else — there is no separate requirements document. Design's
responsibility for ids ends at minting them; how coverage is measured and when
the work is "done" are downstream's concern and are not specified here.

## Conventions

- **Language/module:** Go 1.26, module `eventplane` (packages `outbox`,
  `consumer`, `routing`, and the new `correlation` and `observe`). Sole direct
  dependency stays `modernc.org/sqlite` — the matcher and the ULID minter are
  hand-rolled; **no new `require` may appear in `go.mod`**.
- **Leaf packages:** `routing`, `correlation` and `observe` are leaves that
  anything downstream of this module may import. `correlation` is stdlib-only;
  `observe` imports only stdlib plus `routing`. Neither may import `outbox` or
  `consumer`, and nothing in this module may import `appkit` (which requires
  `eventplane` — the reverse is an import cycle).
- **Build/vet:** `go vet ./...` run from `eventplane/`; code is `gofmt`-clean
  (`gofmt -l .` prints nothing). Local dev runs in workspace mode via the
  repo-root `go.work` (do not set `GOWORK=off`).
- **Test command:** `go test ./...` run from `eventplane/`.
- **"The suite is green" means:** `go test ./...` from `eventplane/` exits 0
  with every package passing, and `go vet ./...` exits 0.
- **Test substrate rule:** any claim that depends on a real substrate is
  proven on that substrate — DDL claims apply the schema to a real SQLite
  database (`modernc.org/sqlite`); wire claims run the real
  `outbox.FeedHandler()` in an `httptest.Server` with a real HTTP client or
  `consumer.Run` on the other end (the existing `consumer_test.go` pattern).
- **Test naming:** each Verification id is covered by a test that cites the id
  in its name or an adjacent comment, so grepping for the id finds the proof.
  Requirement-id tags live in Go test files, glob `*_test.go`.

## Layout

`INDEX.md` is the manifest: every Decision and every Verification id, with id
lookup a grep away. Each `DNN.md` is one self-contained Decision, referenced
in prose and the plan as `D<N>`. This README holds only the spine. Design is
rewritten in place: a changed Decision is rewritten in its `DNN.md` and
`INDEX.md` is regenerated; a new Decision adds a `DNN.md` and an INDEX entry.

Current Decisions:

- **D1** — Envelope and wire cutover: `kind` + `subject` replace `type`.
- **D2** — The `routing` package: canonical key rendering and the hand-rolled
  matcher.
- **D3** — Producer families: registry, reflection, and filter validation.
- **D4** — Consumer surface: routing fields on `consumer.Event`.
- **D5** — Feed guard ownership moves to the chassis: `FeedHandler` checks no
  headers itself.
- **D6** — `eventplane/correlation`: the suite's correlation-id leaf package
  (header, Crockford ULID minter, validity, context accessors).
- **D7** — Correlation on the producer path: outbox column, envelope field,
  ctx-bearing `Append`.
- **D8** — Correlation on the consumer path: the chain enters the handler's
  context, minting a root when the event carries none.
- **D9** — `eventplane/observe`: an injectable hook on the publish and consume
  paths.
