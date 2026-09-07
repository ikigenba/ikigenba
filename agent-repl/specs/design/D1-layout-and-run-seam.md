# D1-layout-and-run-seam

`agent-repl` is a Go CLI that runs an interactive conversation with a model
through `github.com/ikigenba/ikigenba/agentkit`, with the six local tools from
`github.com/ikigenba/ikigenba/toolkit` registered. It is deliberately thin: the
whole configuration is `-c key=value` strings, the help screen is generated
from agentkit's catalog, and every request option except the handful that pick
the provider and credentials is handed to agentkit unchecked. Module path
`github.com/ikigenba/ikigenba/agent-repl`, targeting the Go version pinned
in `go.mod`.

**Direct dependencies are an approved list.** The module's direct
requirements are exactly agentkit, toolkit, and `github.com/google/uuid`;
everything else in `go.mod` is `// indirect`. That list is the record of
human approval: a test compares `go.mod`'s direct requirements against it, so
the build run cannot promote a new module without a human first replacing the
requirement that names the set. Versions are deliberately not pinned here —
adopting a release is a dependency edit, not a design change. `go.sum` and the
module cache are not consulted; indirect entries move without anyone approving
a dependency.

```
agent-repl/                         (this sub-project; go.mod lives here)
├── cmd/agent-repl/main.go          thin: os.Args/stdio/real deps → cli.Run(); os.Exit
├── internal/cli/                   orchestration, streams, exit codes, version string
├── internal/options/               flag grammar, usage text, pre-I/O validation
├── internal/help/                  the catalog text, rendered from agentkit.Catalog()
├── internal/session/               offering, credentials, endpoint, tools, conversation
└── internal/render/                decorated and raw output
```

- **`internal/help`** — pure text: the providers block and the per-provider
  model rows, computed from the catalog alone (D4).
- **`internal/options`** — the flag surface, the usage text, and every check
  that needs no I/O (D2, D3).
- **`internal/session`** — turns validated options plus the machine (home
  directory, environment, working directory) into a live `agentkit`
  conversation: catalog lookup, credential resolution, endpoint, tools, log (D3).
- **`internal/render`** — the two output styles: the decorated transcript a
  person reads, and the raw log-record stream a script reads (D6).
- **`internal/cli`** — the composition root's behavior behind one entry point:
  the order of operations, the input loop, streams, and exit codes (D5).

  ```go
  package cli

  // Run executes the CLI. args are the program arguments without the program
  // name; all I/O flows through the injected streams and every environmental
  // dependency through deps. Run never terminates the process; it returns the
  // process exit code.
  func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int

  // Deps carries what a session cannot be deterministic about.
  type Deps struct {
      Home       string                 // the user's home directory (~/.agent-repl lives here)
      Getenv     func(string) string    // environment lookup (API keys)
      Now        func() time.Time       // clock (log file name, log timestamps)
      LogID      string                 // identity stamped on every log record (D5)
      Root       string                 // working directory the tools are rooted at
      Interrupts <-chan struct{}        // one receive per SIGINT (D5)
  }
  ```

- **`cmd/agent-repl`** — `main` wires the real values: `os.Args[1:]`,
  `os.Stdin`, `os.Stdout`, `os.Stderr`, `os.UserHomeDir()`, `os.Getenv`,
  `time.Now`, `uuid.NewString()`, `os.Getwd()`, and a channel fed by
  `signal.Notify` for `SIGINT`. It contains no other logic.

**The log id is minted once, in `main`.** agentkit stamps the `id` handed to
`NewLog` on every record of the log (its D15), so a store that merges many
sessions' logs can filter one session by id. agent-repl's id is a version 4
UUID from `github.com/google/uuid`, minted by `main` and passed in as
`Deps.LogID` like every other environmental value — a random source inside
`Run` would make the log file and the raw-mode stream impossible to compare
byte for byte in a test. `Run` trusts the value as it trusts `Home`; the `uuid`
import lives in `cmd/agent-repl` alone, so the internal packages stay testable
with any string.

Dependencies point one way: `cmd/agent-repl` → `internal/cli` →
`{internal/options, internal/session, internal/render}`, and
`internal/options` → `internal/help`. `internal/help`, `internal/session`, and
`internal/render` import nothing else in this module, so each is testable with
no knowledge that a CLI exists.

**The network is not injectable.** agentkit sends every request through Go's
default HTTP client and offers no seam for a fake. What it does offer is the
endpoint's base URL, so every test that needs a provider stands up an
`httptest` server and passes `-c base_url=` (D3). That is also why `base_url`
is a user-facing key rather than a test-only hook: a proxy or a compatible
local server is the same case.

**Wiring proof at the binary level.** Every in-process test injects its own
streams and `Deps`, so none can prove `main` wired the real ones. One test
builds the real binary, runs it as a subprocess with a piped prompt, a
temporary `HOME`, an API key in the environment, and `-c base_url=` pointing at
an `httptest` provider, and checks its output and the log file it wrote —
including that every record carries an `id` that is a well-formed version 4
UUID, the only place `main`'s minting can be proven. Like oauth's D01, this
requirement depends on D2–D6 having landed and belongs in a final phase.

**Version.** A single `var version` in `internal/cli` carries the version
string in source, never injected at build time (D4). It is the successor of
the previously installed `agentrepl` binary; that value is release data,
edited directly.

## REQUIREMENTS

- R-OWY5-60C0: The module MUST be `github.com/ikigenba/ikigenba/agent-repl` with its own `go.mod` that specifies a Go version, and the set of `require` entries in that `go.mod` not marked `// indirect` MUST be exactly `github.com/ikigenba/ikigenba/agentkit`, `github.com/ikigenba/ikigenba/toolkit`, and `github.com/google/uuid`, verified by a test that reads `go.mod` and consults neither `go.sum` nor the module cache.
- R-U01R-JPKI: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-OY61-JS2P: Package `internal/cli` MUST export a `Deps` struct whose fields are exactly `Home string`, `Getenv func(string) string`, `Now func() time.Time`, `LogID string`, `Root string`, and `Interrupts <-chan struct{}`.
- R-OZDX-XJTE: A session run through `Run` MUST obtain its home directory, its environment lookups, its clock, its log id, and its tool root from the corresponding `Deps` fields, verified by injected values each of which is observable in the session's behavior (the log file path, the credential used, the log timestamps, the `id` field of every log record, and a tool's resolved root).
- R-U3PG-P0SL: Packages `internal/help`, `internal/session`, and `internal/render` MUST import no other package of this module, and `internal/options` MUST import no package of this module other than `internal/help`.
- R-P0LU-BBK3: The binary built from `./cmd/agent-repl`, run with a piped prompt on stdin, a temporary `HOME`, `OPENAI_API_KEY` set, and `-c provider=openai -c auth=api_key -c base_url=` naming an `httptest` provider that answers with a text reply, MUST exit 0, MUST write a line beginning `assistant › ` followed by that reply to stdout, and MUST leave exactly one `.jsonl` file under `$HOME/.agent-repl/logs/` in which every record's `id` field is the same lowercase RFC 4122 version 4 UUID string.
