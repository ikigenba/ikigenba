# github — Design Conventions

Shared facts every Decision leans on.

- **Language / module.** Go (`go 1.26`); module path `github`, a new module at
  `github/` under the repo root. It is built on the shared `appkit` chassis and,
  like every deployable service, is release-versioned (`github/VERSION`). It wires
  `appkit` (and, only where a shared type is needed, `eventplane`) via committed
  `replace` directives, exactly as `gmail` does: `replace appkit => ../appkit`.
- **Package layout.** `cmd/github/main.go` is the composition root (`appkit.Main`
  over the Spec); domain packages live under `internal/`:
  `internal/githubapp` (the appkit Spec), `internal/gh` (the GitHub auth + REST
  client), `internal/mcp` (the domain tool registrations over the shared
  `appkit/mcp` transport — D8), `internal/db` (the embedded migration set),
  `internal/web` (the landing page + embedded assets). The loopback PR route lives
  in `internal/gh`.
- **Non-secret config, read at the composition root.** `internal/githubapp.Spec`'s
  `Handlers` hook reads `IKIGENBA_APP_ID`, `IKIGENBA_GITHUB_ORG`, and
  `IKIGENBA_APP_PRIVATE_KEY` from the environment once, at the boundary, and passes
  them into the client. The private key value is **never logged**. No package below
  the composition root reads the environment. (`IKIGENBA_APP_CLIENT_SECRET` exists
  but is unused in v1.)
- **Zero new third-party dependencies.** The GitHub client and the RS256 app-JWT
  signing use only the Go standard library (`crypto/rsa`, `crypto/x509`,
  `crypto/sha256`, `encoding/pem`, `encoding/base64`, `encoding/json`,
  `net/http`); the JSON-RPC transport is the shared `appkit/mcp` (D8), not
  hand-rolled here, and the outbound `*http.Client` is the shared
  `appkit/httpclient` (D11). No `go-github`, no JWT library, no `x/oauth2`. The
  module's dependency set matches the chassis's existing closure (sqlite via
  appkit) and adds nothing — `appkit` is a committed in-repo replace-sibling,
  so consuming more of it adds no third-party dependency.
- **Build / typecheck command.** `GOWORK=off go build ./...` from the module root
  (`github/`). Forcing `GOWORK=off` matches the deterministic production build and
  proves the module resolves standalone via its `replace` directives.
- **Test command.** `GOWORK=off go test ./...` from the module root. Every
  automated test is fully offline: the GitHub client is exercised against an
  injected `http.RoundTripper` stub (the same technique `gmail`'s client tests
  use), and the nginx fragment against a parse/grep test. No test in the gate
  performs live network I/O.
- **Test layers.** The suite's testing vocabulary — the hermetic / composed /
  live / manual layers, what each may touch, the single `//go:build live`
  mechanism, the ban on `t.Skip` outside live-tagged files, and the rule that a
  manual-layer check lives in a committed runbook with a positive and a negative
  check — is the contract `root project/design/D23.md`, cited and not restated
  here. github's own layer facts are recorded in D14: **hermetic** and
  **composed** in the default gate (the composed member being the install-layout
  boot smoke in `cmd/github/main_test.go`), a **manual** layer whose runbook is
  `project/github-verification.md`, and no live layer. `GOWORK=off` is this
  tree's declared GOWORK mode; the `go` binary on `PATH` at test time is its one
  environmental precondition beyond the Go toolchain.
- **Test-file glob.** Requirement-id tags live in `*_test.go` files, each id as a
  `// R-XXXX-XXXX` comment on the test that asserts it.
- **"The suite is green"** means: `GOWORK=off go build ./...` succeeds **and**
  `GOWORK=off go test ./...` passes with no failures and no `SKIP`, from `github/`,
  and `gofmt -l .` is empty and `go vet ./...` is clean.
- **The real-substrate proof is `health`, and it is a manual-layer check.**
  Correctness of the app→installation authentication cannot be proven by a stub
  (a stub accepts any JWT). The design therefore designates the `health` verb as
  that proof: against the deployed service with real credentials it drives a
  *real* authenticated GitHub call, and its success is the observable that the
  credentials actually work. The id that hinges on the real GitHub contract
  (`R-DMUT-QF4A`, D2) is a **manual-layer** id in the sense of
  `root project/design/D23.md`: it is proven by the operator in this tree's
  committed runbook, `project/github-verification.md` — which states the
  positive check, the bad-key negative check that proves the positive one
  exercised real GitHub, and where the result is recorded — and it is
  deliberately out of gate and unscheduled by any plan phase. The offline suite
  proves request *construction*; the runbook proves the request is *accepted*.
  See D14.
- **Bot-only attribution.** Write paths pass no owner-identifying author,
  committer, or body marker to GitHub; the only owner record is a structured log
  line (`X-Owner-Email` + verb) emitted at MCP dispatch. This is asserted directly
  on the request the client builds (the outbound body/headers carry no owner PII)
  and on the emitted log line.
