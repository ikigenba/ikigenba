# D01-layout-and-run-seam

`oauth` is a Go CLI that performs one job: the OAuth 2.0 authorization-code +
PKCE login flow against any protocol-compliant service described entirely by
its flags, emitting the token endpoint's response verbatim on stdout. Module
path `github.com/ikigenba/ikigenba/oauth`, Go 1.26, standard library only; no
third-party modules.

```
oauth/                          (this sub-project; go.mod lives here)
├── cmd/oauth/main.go           thin: os.Args/stdio/real deps → cli.Run(); os.Exit
├── internal/cli/               orchestration, streams, exit codes, version string
├── internal/options/           flag grammar, usage text, all pre-I/O validation
├── internal/oauth/             pure protocol: session, PKCE, authorize URL, exchange
├── internal/callback/          loopback listener and callback HTTP protocol
└── internal/browser/           platform browser launcher (build-tagged)
```

- **`internal/oauth`** — the pure protocol core: per-login secrets, the PKCE
  challenge, authorize-URL construction, and the token exchange. Contracts in
  D02, D03, D04.
- **`internal/callback`** — binds the loopback listeners and serves the one
  redirect that completes a login. Contracts in D05, D06.
- **`internal/browser`** — opens a URL in the user's browser, per platform.
  Contract in D07.
- **`internal/options`** — the entire flag surface, the usage text, and every
  check that can be made before anything observable happens. Contracts in D08,
  D09, D10.
- **`internal/cli`** — the composition root's behavior: the order of
  operations, stream discipline, and exit codes, behind one exported entry
  point. Contract in D11.

  ```go
  package cli

  // Run executes the CLI: args are the program arguments (without the program
  // name), all I/O flows through the injected streams, and every
  // non-deterministic dependency flows through deps. Run reads no stdin. Run
  // never terminates the process; it returns the process exit code.
  func Run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int

  // Deps carries the four dependencies a login cannot be deterministic about.
  // Every field is required; Run does not substitute a default for a nil
  // field, so a missing dependency is a programming error at the composition
  // root rather than a silent fallback to real entropy or the real network.
  type Deps struct {
      Launcher   browser.Launcher   // opens the authorize URL (D07)
      Entropy    io.Reader          // source of state and code verifier (D02)
      HTTPClient *http.Client       // performs the token exchange (D04)
      Listen     callback.ListenFunc // binds the loopback listeners (D05)
  }
  ```

- **`cmd/oauth`** — `main` wires the real values (`os.Args[1:]`, `os.Stdout`,
  `os.Stderr`, `browser.New()`, `crypto/rand.Reader`, `http.DefaultClient`,
  `net.Listen`) into `cli.Run` and passes the result to `os.Exit`. It contains
  no other logic.

Dependencies point one way: `cmd/oauth` → `internal/cli` →
`{internal/options, internal/callback, internal/browser}`, with
`internal/options` → `internal/oauth` and `internal/cli` → `internal/oauth`.
`internal/oauth`, `internal/callback`, and `internal/browser` import nothing
else in this module, so each is testable with no knowledge that a CLI exists.

**Why `Deps` is a struct rather than four more parameters.** The dependency
set is the part of this program most likely to grow, and a named field per fake
is what a test reads. idgen passes its one `Clock` positionally because it has
exactly one; four positional interfaces would be four chances to transpose two
arguments of compatible type.

**Why the whole CLI lives below `cmd/`.** Everything above — flags, validation,
wiring, streams, exit codes — is behavior a test must be able to drive
in-process. A `main` that owns any of it can only be tested by spawning a
subprocess, and a package that can only be tested by subprocess accretes
logic until it is untestable in practice.

**What injected dependencies structurally cannot reach.** Every test that
enters through `Run` supplies its own launcher, entropy, HTTP client, and
listen function — which is exactly why no such test can prove that `main`
wired the *real* ones. A `main` that passed a zero `io.Reader` as entropy, or
that never called `net.Listen`, would satisfy every `Run` test in the suite.
So this design also requires a wiring proof at the binary level: the real
built binary, run as a subprocess with `--no-browser`, against an
`httptest.Server` standing in for the provider. The test reads the authorize
URL from the binary's stderr, performs the authorize GET itself, and lets the
fake provider redirect to the binary's real loopback listener. That exercises
`net.Listen`, `crypto/rand.Reader`, and `http.DefaultClient` as `main` actually
wired them, and it is fully deterministic — no human, no real provider, no
network beyond loopback — so it runs in the ordinary gates rather than behind a
build tag.

The one dependency this cannot cover is `browser.New()`: under `--no-browser`
the launcher is constructed but never invoked, and invoking a real browser is
not something a test may do. That residue is accepted deliberately; D07 pins
the launcher's contract against an injected command factory instead.

**Phase ordering note.** The binary-level requirements below are not
satisfiable until D02–D11 have landed — there is no working login to drive
until the protocol, callback, options, and orchestration contracts exist. They
belong to this document because the seam is what they prove, but `$seal-spec`
must order them into a **final** phase rather than an early one. idgen has the
same tension in its D1, whose binary smoke ("the built binary prints an id")
depends on its D2–D4; this is that situation, stated here so the plan does not
have to rediscover it. That sibling requirement is deliberately described rather
than cited by id: the gap is computed by grepping every id-shaped literal in
`specs/design/`, so quoting another project's id here would enter this project's
gap and send the loop hunting for a test that does not belong to it.

## REQUIREMENTS

- R-E5L7-OSOC: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-E6T4-2KF1: Package `internal/cli` MUST export a `Deps` struct whose fields are exactly `Launcher browser.Launcher`, `Entropy io.Reader`, `HTTPClient *http.Client`, and `Listen callback.ListenFunc`.
- R-E810-GC5Q: A successful `Run` MUST obtain its browser launch, its random bytes, its token-exchange HTTP round trip, and its loopback listener from the corresponding `Deps` fields, verified by four injected fakes each of which records that it was used.
- R-E98W-U3WF: Packages `internal/oauth`, `internal/callback`, and `internal/browser` MUST import no other package of this module, and `internal/options` MUST import no package of this module other than `internal/oauth`.
- R-EAGT-7VN4: The binary built from `./cmd/oauth`, run with `--no-browser` against an `httptest` provider that redirects to the binary's own loopback listener, MUST exit 0 and write exactly the provider's token response bytes to stdout.
- R-EBOP-LNDT: Two successive runs of the binary built from `./cmd/oauth` against an `httptest` provider MUST present that provider with different `state` values and different `code_challenge` values, proving `main` wired a real entropy source rather than a fixed or empty one.
