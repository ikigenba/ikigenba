# repos — Design

**Authority: shape and its proof.** This directory owns *how* the repos
service is built and *how each behavior is proven* — seams, interfaces, types,
naming, and the test plan. Product (`project/product/README.md`) owns the why
and the promises; design states the exact, checkable form of those promises and
never re-declares the why. Design uses the product's contractual constants (the
kind set `sites`/`scripts`/`prompts`/`code`, the live branch `main`, the
`ikigenba/` automation branch namespace, port 3007, mount `/srv/repos/`,
starting version `v0.1.0`) by value but does not own them. This is the single
current statement of the architecture, rewritten in place; construction history
lives in git.

repos implements the suite's **version plane** contract,
`root project/design/D24.md`. That contract is cited, never restated: it owns
the plane's rules (ambient versioning, `<kind>/<name>` custody, `main` is live,
one git door, merge posture, archive-not-delete), and this design owns the
seams, shapes, and proofs that realize them inside `repos/`.

## Requirement ids

Each Decision ends with a **Verification** list — the concrete behaviors that
Decision requires. Every item carries a minted `R-XXXX-XXXX` id: a stable,
unique handle for that one behavior. The ids live inline in these lists and
nowhere else (no separate requirements document). Design's responsibility for
ids ends at minting them — how coverage is measured and when the work is
"done" are downstream's concern and are not specified here.

## Conventions

- **Language / module:** Go (`go 1.26`); module path `repos`, a standalone
  module at `repos/`, on the `appkit` chassis over SQLite
  (`modernc.org/sqlite`, pure-Go, no cgo). In-repo libraries via committed
  `replace` directives (`appkit => ../appkit`, `eventplane => ../eventplane`)
  plus `require registry` (same pattern). **No other module dependency**: repos
  drives no agent engine and speaks to no external API, so `go.mod` requires
  neither `github.com/ikigenba/agentkit` nor any provider SDK.
- **Build / typecheck:** `cd repos && go build ./...` and `go vet ./...`.
  Production binary via `bin/ship repos` (`CGO_ENABLED=0 GOOS=linux
  GOARCH=amd64 GOWORK=off`).
- **Test:** `cd repos && go test ./...`. **"The suite is green" means:**
  `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0 with no
  failures, and `gofmt -l .` prints nothing — all from `repos/`. Requirement-id
  tags live in the test-file glob `*_test.go`.
- **git is the substrate, never a mock.** repos' entire domain is what real git
  does, so **every git claim is proven against the real `git` binary**: bare
  repositories created with `git init --bare` under `t.TempDir()`, real
  `git clone`/`fetch`/`push` from a temporary client working copy against the
  service's own smart-HTTP handler mounted on an `httptest` server, and real
  plumbing (`hash-object`, `read-tree`, `write-tree`, `commit-tree`,
  `update-ref`, `merge-tree`, `merge-base`, `archive`) for the commit paths.
  There is no `Git` fake in the gate: a mocked git accepts whatever it is
  handed and can falsify nothing this service claims. `file://` remotes are used
  only where the claim is about custody on disk rather than about the door.
- **Other test substrates:** real temp-file SQLite through the embedded
  migration set; the service's own HTTP handlers through `net/http/httptest`; a
  deterministic injected clock; no non-loopback network I/O anywhere in the
  gate, and in particular **no request to github.com from any test or any
  non-test source path**.
- **Test layers.** The suite's testing vocabulary — the hermetic / composed /
  live / manual layers, what each may touch, the single `//go:build live`
  mechanism, and the ban on `t.Skip` outside live-tagged files — is the contract
  `root project/design/D23.md`, cited and not restated here. repos' own layer
  facts are recorded in D16: **hermetic** and **composed** only, both in the
  default gate, with no live layer and no tree-local manual runbook. No gate
  item runs against `bin/start`; the assembled-stack check is the suite's
  manual-layer item. The two environmental preconditions beyond the Go
  toolchain — the real `git` binary and the `go` binary, each on `PATH` at test
  time — are hard failures when absent, never skips.
- **DB / migrations:** ordered, immutable SQL in `internal/db/migrations/`,
  embedded, applied forward-only by the appkit runner. New migrations only via
  `bin/create-migration repos <name>`; numbers never hand-picked, committed
  migrations never edited.
- **Config:** env only, prefix `REPOS_`, read at the composition root, never
  below it. The whole set: `REPOS_STATE_DIR` (dev-only override; unset in
  production, where the state root composes in-binary from `IKIGENBA_ROOT` —
  resolution order in D15), `REPOS_MAX_COMMIT_BYTES` (default `67108864`), and
  `REPOS_GIT_BIN` (default `git`). There are no credentials in repos'
  environment: it authenticates nobody itself and calls no external service.
  Run-token lifetime is not configured here — it arrives per request (D20),
  bounded at the source by `root project/design/D26.md`.
- **Peers by name, addresses from the registry:** repos has **no service
  peers** — it makes no outbound call to another service. It asks `registry`
  only for its own address (`registry.MustPort("repos")`,
  `registry.BaseURL("repos")` when rendering a `content_url`). No
  `127.0.0.1:30xx` literal in source.
- **Time / IO:** time enters through a `Clock` seam; the DB handle is the
  appkit single-writer `*sql.DB` (`rt.DB()`), shared with the producer outbox.
  Every git invocation goes through the `Git` seam (D17) so the binary path,
  environment, and streaming are set in exactly one place.
- **Identity and owner keying** follow `root project/design/D17.md`:
  `X-Owner-Id` is the sole scoping key, `owner_email` a write-once display
  snapshot, `X-Client-Id` the acting actor and the commit attribution source.
  Identity arrives as nginx-injected or loopback-asserted headers and is
  trusted blindly (suite convention).
- **The MCP surface** conforms to `root project/design/D20.md` (structured
  results with declared output schemas, the closed error vocabulary, the three
  discovery tiers); **the loopback byte surface** conforms to
  `root project/design/D19.md` (holder contract: streaming, loopback-private by
  handler guard, `rev`-pinnable with 409/404, addressed by repos' own key);
  **the event surface** conforms to `root project/design/D18.md`.
- **Correlation and telemetry are the chassis's** (`root project/design/D14.md`).
  Reading-or-minting `X-Correlation-Id` on every inbound request, recording
  inbound MCP and plain HTTP traffic, `publish` hops, and `lifecycle` records
  arrive with `appkit`/`eventplane` and are proven by their ids, never
  re-proven here. repos owns exactly one correlation seam: its nginx fragment
  hands the process a trustworthy id (D13). **Out of recording scope,
  deliberately:** every `git` subprocess repos spawns — including
  `git http-backend` serving a clone or a push. The boundary (the MCP call, the
  loopback request, the door request and its outcome) is recorded; git's own
  chatter is not.

## Layout

`INDEX.md` is the manifest; `DNN.md` is one self-contained file per Decision
(zero-padded; referenced in prose and the plan as `D<N>`); this README holds
only the spine. Design is rewritten in place: a changed Decision is rewritten
in its `DNN.md` and `INDEX.md` is regenerated; a new Decision adds a `DNN.md`
and an INDEX entry. Retired Decision numbers are listed in `INDEX.md` and are
never reused.
