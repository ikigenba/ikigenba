# Phase 12 — Prove no loopback-port literal survives, and guard the deploy artifacts against registry drift

*Realizes design Decision 16 (source-scan guard + deploy-artifact drift guard).
Depends on phase 11 (the `registry` adoption must already be wired, and `go.mod`
must already require `registry`). Covers `R-X1CA-0373`, `R-X2K6-DUXS`. **Read D16
for the exact guard shapes and rationale.***

Phase 11 removed crm's sole non-test loopback literal (`Spec.Port`); this phase
**enforces** that no `127.0.0.1:30xx` literal returns and turns the two static
deploy artifacts that must still carry the literal port (`etc/manifest.env`,
`etc/nginx.conf`) into drift-caught, not drift-prone — their crm tests now derive
the expected port from `registry`. For crm the remaining Go-source
`127.0.0.1:3100` literals live entirely in `cmd/crm/main_test.go` (the nginx and
manifest assertions); this phase converts them.

**What gets changed (tests only — all inside `crm/`):**

- **Source-scan guard (`R-X1CA-0373`)** — add a genuinely-asserting test (a small
  guard file under `cmd/crm` or a dedicated `internal/` guard package) tagged
  `// R-X1CA-0373` that:
  - walks every `*.go` file under the `crm/` module root and fails if any file's
    source contains a bare loopback-address literal of the form `127.0.0.1:30` +
    two digits;
  - **assembles the forbidden needle at runtime** (e.g. `"127.0.0.1:" + "30"`)
    rather than embedding the full literal, and **skips its own filename**, so the
    guard can never match itself;
  - passes cleanly after the manifest/nginx re-pointing below (zero
    `127.0.0.1:30xx` literals remain) and would go red if a hardcoded upstream like
    `"http://127.0.0.1:3100/"` were reintroduced. The pre-existing `127.0.0.1:0`
    (`net.Listen` free-port) and `127.0.0.1:%d` (`waitForHealth` URL) literals are
    not the `30xx` form and must stay green.
- **Manifest drift guard (`R-X2K6-DUXS`, part 1)** — in `cmd/crm/main_test.go`,
  change `TestManifestLibraryByteEqualsCommittedFile`'s emitted field from
  `Port: 3100` to `Port: registry.MustPort("crm")`. Keep every other `Fields`
  value (App, Mount, Default, MCP, Feed, the `OUTBOX_RETENTION_*` extras) and the
  byte-equality assertion unchanged; the emitted `PORT=3100` still byte-matches the
  committed `etc/manifest.env` today. This concretely proves D15's R-X04D-MBGE (the
  emitted port derives from `registry.MustPort("crm")`).
- **nginx drift guard (`R-X2K6-DUXS`, part 2)** — in `cmd/crm/main_test.go`, replace
  the three hardcoded `proxy_pass` upstream assertions with ones built from
  `registry.BaseURL("crm")` (== `"http://127.0.0.1:3100"`):
  - the landing test (`R-NGNX-6M9N`): assert the block contains
    `"proxy_pass " + registry.BaseURL("crm") + "/;"`;
  - the PRM-survival test (`R-NGNX-8P1Q`): assert the PRM block contains
    `"proxy_pass " + registry.BaseURL("crm") + "/.well-known/oauth-protected-resource;"`;
  - the static test (`R-SWNU-U5QA`): assert the block contains
    `"proxy_pass " + registry.BaseURL("crm") + "/static/;"`.

  Keep the exact-match vs prefix distinction, the session/bearer `auth_request`
  gate assertions, the `X-Owner-Email` forwarding checks, the feed `return 404`
  survival, and the PRM-ungated check exactly as they are — only the port value
  they compare against becomes a `registry` call.
- Touch nothing else. Do **not** edit `etc/manifest.env` or `etc/nginx.conf`
  themselves — their literal `3100` stays; these tests now police it. **No schema
  change — no migration.**

**Done when:** the suite is green — `cd crm && go build ./...`,
`cd crm && go vet ./...`, `cd crm && gofmt -l .` (no output),
`cd crm && go test ./...`, and `bin/check-migrations crm` all succeed with zero
failures — and:

- R-X1CA-0373 — a guard test walks crm's `*.go` files (skipping itself, needle
  assembled at runtime) and asserts no bare `127.0.0.1:30xx` loopback-address
  literal remains; it is green after the re-pointing and goes red if one is
  reintroduced.
- R-X2K6-DUXS — the manifest byte-equality test emits with
  `registry.MustPort("crm")` and matches the committed `etc/manifest.env`, and the
  landing/PRM/static nginx tests assert their `proxy_pass` targets against
  `registry.BaseURL("crm")`; a `registry` value differing from the committed `3100`
  would fail them (the intended drift alarm).
- R-NGNX-6M9N, R-NGNX-8P1Q, R-SWNU-U5QA keep their existing behavioral assertions,
  now with the port derived from `registry`.
- `grep -rn "127.0.0.1:3100" crm --include=*.go` returns no matches (excluding
  `crm/project/`, where this plan quotes the pattern), and no `Port: 3100` integer
  literal remains in crm's Go source.
