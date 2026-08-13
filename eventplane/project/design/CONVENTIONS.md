# eventplane — Design Conventions

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
- **Test layers.** The suite's testing vocabulary — the hermetic / composed /
  live / manual layers and what each may touch, the single `//go:build live`
  mechanism, and the ban on `t.Skip` outside live-tagged files — is the contract
  `root project/design/D23.md`, cited and not restated here. Every test in this
  tree is **hermetic**: the substrates named above are real local ones (temp-dir
  SQLite, `httptest` loopback listeners, a local `go list` subprocess), and this
  tree has no composed, live, or manual layer. D10 records eventplane's full
  declaration, including its GOWORK mode and environmental preconditions.
- **Test naming:** each Verification id is covered by a test that cites the id
  in its name or an adjacent comment, so grepping for the id finds the proof.
  Requirement-id tags live in Go test files, glob `*_test.go`.
- **Suite-contract proofs carried here.** Some `*_test.go` files in this module
  are tagged with requirement ids minted by the **umbrella** project (the repo
  root's `project/design/`) rather than by a Decision in this directory: the
  umbrella marks those ids `[proof: eventplane]`, naming eventplane as the one
  tree that carries the tagged test for a suite-wide contract it owns — the
  event plane's own plumbing is where that behavior is observable, so it is
  proven once, here. Those tags are correct and expected; this design neither
  owns nor restates the contracts behind them, so a tree-local sweep that reads
  only `eventplane/project/design/` will not find their home, and that is not a
  defect. The converse also holds: an umbrella id marked `[proof: per-service]`
  does **not** belong on a test here. eventplane is a library, not a service, so
  it is never itself the adopter of a per-service contract; each service carries
  that proof in its own tree.
- **Scope — revision over a baseline.** This spec covers the routing revision
  (D1–D4), the feed-guard removal (D5), and the correlation and observation work
  (D6–D9). The as-built library — outbox atomicity, the SSE transport and
  control frames, cursors and the epoch token, all four resync reasons, reconnect
  backoff, retention, and the handler-return cursor gate (nil/ErrSkip/stall) — is
  the baseline described in `eventplane/CLAUDE.md` and verified in
  `project/research/research.md`. Decisions reference that baseline; they never
  respecify it, and no Verification id re-proves it.
