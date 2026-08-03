# repos — Design

**Authority: shape and its proof.** This directory owns *how* the repos
service is built and *how each behavior is proven* — seams, interfaces, types,
naming, and the test plan. Product (`project/product/README.md`) owns the why
and the promises; design states the exact, checkable form of those promises
and never re-declares the why. Design uses the product's contractual constants
(bot identity `@ikigenba`, label set `execute`/`executing`/`failed`/`discuss`,
port 3007, mount `/srv/repos/`, starting version `v0.1.0`) by value but does
not own them. This is the single current statement of the architecture,
rewritten in place; construction history lives in git.

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
  plus `require registry` (same pattern); the agent engine via the pinned
  tagged module `github.com/ikigenba/agentkit` (v0.2.0 line, matching
  prompts).
- **Build / typecheck:** `cd repos && go build ./...` and `go vet ./...`.
  Production binary via `bin/ship repos` (`CGO_ENABLED=0 GOOS=linux
  GOARCH=amd64 GOWORK=off`).
- **Test:** `cd repos && go test ./...`. **"The suite is green" means:**
  `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0 with no
  failures, and `gofmt -l .` prints nothing — all from `repos/`. Requirement-id
  tags live in the test-file glob `*_test.go`.
- **Test substrates:** real temp-file SQLite through the embedded migration
  set; the **real `git` binary** against local fixture remotes
  (`git init --bare` in `t.TempDir()`, `file://` URLs) — never a mocked git;
  suite peers (github, webhooks) as `httptest` stubs that record requests; a
  deterministic injected clock; no live network I/O in unit tests. The live
  end-to-end proof runs against `bin/start`.
- **DB / migrations:** ordered, immutable SQL in `internal/db/migrations/`,
  embedded, applied forward-only by the appkit runner. New migrations only via
  `bin/create-migration repos <name>`; numbers never hand-picked, committed
  migrations never edited.
- **Config:** env only, prefix `REPOS_`, read at the composition root, never
  below it. The set: `REPOS_PROVIDER` (default `anthropic`), `REPOS_MODEL`
  (default `claude-opus-4-8`), `REPOS_SESSION_TTL` (default `30m`),
  `REPOS_MAX_SESSIONS` (default `2`), `REPOS_GITHUB_HOOK` (default `github`),
  `REPOS_BOT_LOGIN` (default `ikigenba[bot]`), `REPOS_GITHUB_ORG` (default
  `ikigenba`), `REPOS_WORKTREE_TTL_DAYS` (default `14`), `REPOS_STATE_DIR`
  (default `state`, resolved to an absolute path at the composition root —
  D9/D4), plus the provider API keys (`ANTHROPIC_API_KEY` et al.) that agentkit
  providers need.
- **Peers by name, addresses from the registry:** the service names its peers
  in code (`webhooks` as event source, `github` as the GitHub actor) and asks
  `registry` where they live (`registry.MustPort("repos")`,
  `registry.BaseURL("github")`). No `127.0.0.1:30xx` literal in source.
- **Time / IO:** time enters through a `Clock` seam; the DB handle is the
  appkit single-writer `*sql.DB` (`rt.DB()`), shared with the producer outbox.
- **Correlation and telemetry are the chassis's, with two repos-owned seams.**
  Reading-or-minting the `X-Correlation-Id` on every inbound request, recording
  inbound MCP and plain HTTP traffic, `publish`/`consume` hops, and `lifecycle`
  records at boot and graceful shutdown all arrive with the `appkit`/`eventplane`
  rebuild and are proven by their ids, never re-proven here. repos owns exactly
  three things: its two direct HTTP peers use the shared **instrumented outbound
  HTTP client** and thread a live context to it (D11); a session carries its
  correlation id in its row so the chain survives the runner's deliberately
  detached contexts and a restart (D12); and its nginx fragment hands the process
  a trustworthy id (D13). **Out of recording scope, deliberately:** anything
  happening inside a spawned subprocess or a dispatched agent session — git
  invocations, the agent's own tool calls and provider traffic. The boundary
  (the MCP call or consumed event that dispatched the work, and its outcome) is
  recorded; the inside lives in the session transcript under `state/`.
- **Outbound HTTP is proven at the injected client, not by re-asserting the
  chassis.** The instrumented client comes off the Router (`rt.HTTPClient(…)`)
  and is injected into both peers at the composition root, so tests supply a
  `*http.Client` whose `Transport` is a recording `RoundTripper` and assert the
  two things that are repos': the request reaches the wire through that client,
  and it carries the call's live context (the transport reads the correlation id
  off `req.Context()`). Setting the header and emitting the `outbound` record are
  appkit's behaviors with appkit's ids, never re-proven here — and no test needs
  to stand up a Router.
- **Fragment ids are scoped to what a content read shows.** `cmd/repos/nginx_test.go`
  already pins the fragment's routing and identity shape under D10's ids; D13's
  correlation lines extend that same test under their own. Whether a genuine
  minted id arrives, un-injectable by an anonymous caller, is not claimable
  there — it needs a live nginx plus dashboard introspection, outside `repos/`.
- **Trust posture:** identity arrives as nginx-injected (or loopback-asserted)
  owner headers — `X-Owner-Id` (the stable scoping key; the appkit gate
  refuses on its absence), plus the `X-Owner-Email`/`X-Owner-Name`/
  `X-Owner-Picture` display headers and `X-Client-Id`; the service accepts them
  blindly (suite convention) and keys **all** scoping and provenance on
  `X-Owner-Id` (appkit `Identity.OwnerID`), treating `owner_email` as a
  write-once display snapshot. The session agent's isolation is
  worktree-cwd + path-confined file tools, the same single-owner-box posture
  as prompts/scripts — not a security sandbox.

## Layout

`INDEX.md` is the manifest; `DNN.md` is one self-contained file per Decision
(zero-padded; referenced in prose and the plan as `D<N>`); this README holds
only the spine. Design is rewritten in place: a changed Decision is rewritten
in its `DNN.md` and `INDEX.md` is regenerated; a new Decision adds a `DNN.md`
and an INDEX entry.
