# Phase 17 — Adopt the shared instrumented outbound client

*Realizes design Decision 11 (instrumented outbound client).*

**External dependency — build this phase after appkit.** The Decision consumes
seams that do not exist yet: `appkit/httpclient`
(`New`/`NewTransport`/`Options`) and the runtime accessor that yields the
`*telemetry.Recorder` inside `Spec.Handlers`, both owned by the appkit
workspace. The suite's execution order builds appkit before any service adopts
it. Until it lands, this phase cannot compile — that is the intended signal,
not a defect.

`github` is neither an event-plane producer nor a consumer, so eventplane's
`Append`-takes-a-context change does not touch this module and imposes no
ordering here.

## What gets built

Two touched packages inside `github/`:

- **`internal/githubapp/spec.go`** — the `Handlers` hook builds the outbound
  client with `httpclient.New` (recorder from the runtime,
  `Timeout: 30 * time.Second`) and passes it to `newGitHubClient(cfg, hc)` in
  place of today's `nil`. The `Config` hook keeps passing `nil`: it constructs
  a client only to validate that `IKIGENBA_APP_PRIVATE_KEY` parses, it issues
  no request, and it runs before the recorder exists.
- **`internal/gh/client.go` and `internal/gh/token.go`** — both `client()`
  fallbacks stop returning `http.DefaultClient` and return
  `httpclient.New(httpclient.Options{Timeout: 30 * time.Second})` instead. The
  `httpClient *http.Client` parameter on `gh.NewClient`, and the fact that the
  one client it receives is shared by the REST `Client` and the embedded
  `tokenSource`, are unchanged — so every existing offline stub-`RoundTripper`
  test and the `newGitHubClient` indirection keep working as they are.

Observable end state: `github`'s behavior toward GitHub is unchanged (same
endpoints, same app-JWT/installation-token flow, same error mapping) except
that outbound calls now leave through the recorded transport and are bounded by
a 30-second timeout instead of running untimed.

## Done when

The suite is green — from `github/`: `GOWORK=off go build ./...` succeeds,
`GOWORK=off go test ./...` passes with no failures and no `SKIP`,
`gofmt -l .` is empty, and `go vet ./...` is clean — and:

- **D11's ids are covered** by clearly-named tests:
  - `R-01NE-H6K8` — the `*http.Client` a `gh.NewClient(cfg, nil)` client
    resolves for a REST call has a `Transport` of the type `appkit/httpclient`
    builds, and is neither `http.DefaultClient` nor a client with a nil
    `Transport`.
  - `R-02VA-UYAX` — that client and the one its embedded `tokenSource`
    resolves for the app-JWT and installation-token exchanges are the same
    `*http.Client` value (pointer identity).
  - `R-05B3-MHSB` — a non-nil injected client whose `Transport` is a test
    `RoundTripper` is used for both a REST request and the installation-token
    exchange; the stub observes both and nothing reaches the network.
  - `R-06J0-09J0` — the client resolved for the nil case has `Timeout` exactly
    `30 * time.Second`.
- `grep -rn 'http\.DefaultClient' --include='*.go' --exclude-dir=project
  github/` returns **no matches** (exit status 1).

D2's live-substrate GitHub auth id (see `project/design/D02.md`) is **not**
carried by this phase and is not a gating condition for it — per
`project/plan/README.md` it is verified out of loop by the operator following
`project/github-verification.md`. It is worth re-running once this phase lands,
since it is what proves a real authenticated GitHub call still succeeds through
the instrumented transport, but the loop does not wait on it. Its id is
deliberately not written here, so the coverage check does not read it as
assigned to this phase.
