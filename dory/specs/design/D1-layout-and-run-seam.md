# D1-layout-and-run-seam

`dory` is a Go CLI that runs one pass of a supervisor/worker agent tree over
`github.com/ikigenba/ikigenba/agentkit`, with the six local tools from
`github.com/ikigenba/ikigenba/toolkit` given to workers, and a per-session
SQLite store the supervisors search. Module path
`github.com/ikigenba/ikigenba/dory`, targeting the Go version pinned in
`go.mod`.

**Direct dependencies are an approved list**, as in agent-repl. The module's
direct requirements are exactly agentkit, toolkit, `github.com/google/uuid`,
and `modernc.org/sqlite`; everything else in `go.mod` is `// indirect`. A test
compares `go.mod`'s direct requirements against that list, so the build run
cannot promote a module without a human first replacing the requirement that
names the set. Versions are not pinned here; adopting a release is a
dependency edit, not a design change.

`modernc.org/sqlite` is the pure-Go transpile of SQLite, chosen so the binary
needs no cgo. That it ships FTS5 with `bm25()` was proven by a probe against
v1.58.0 (SQLite 3.53.4): an external-content FTS5 table over a plain table,
populated and queried with `MATCH`, ordered by `bm25()`, returned the expected
hit.

```
dory/                               (this sub-project; go.mod lives here)
├── cmd/dory/main.go                thin: os.Args/stdio/real deps → cli.Run(); os.Exit
├── internal/cli/                   orchestration, exit codes, version string
├── internal/options/               flag grammar, usage text, pre-I/O validation
├── internal/model/                 per-role provider resolution → conversation factory
├── internal/store/                 the session database: entries, search, fetch, log writer
├── internal/agent/                 roles, addresses, tools, delegate, one pass
└── internal/render/                the address-prefixed trace and the summary
```

- **`internal/options`** — the flag surface, the usage text, and every check
  that needs no I/O (D2, D3).
- **`internal/model`** — turns one role's validated options plus the machine
  (home directory, environment) into a factory that builds fresh agentkit
  conversations on demand: catalog lookup, credential resolution, endpoint,
  settings, limits (D3).
- **`internal/store`** — the SQLite session file: create and open, the entry
  table, full-text search, fetch, the writer that turns agentkit log records
  into transcript entries, and the session metadata (D4).
- **`internal/agent`** — what an agent is: its address, role, tools, and
  prompt; the `delegate` tool that runs a child to completion; and `RunPass`,
  which runs a root supervisor and returns its report (D5).
- **`internal/render`** — the decorated trace a person reads, every line
  prefixed with the address of the agent that produced it, and the summary
  block (D6).
- **`internal/cli`** — the composition root's behavior behind one entry point:
  the order of operations, the store, the pass, and exit codes (D2, D5).

  ```go
  package cli

  // Run executes the CLI. args are the program arguments without the program
  // name; all I/O flows through the injected streams and every environmental
  // dependency through deps. Run never terminates the process; it returns the
  // process exit code.
  func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int

  // Deps carries what a pass cannot be deterministic about.
  type Deps struct {
      Home       string                 // the user's home directory (~/.dory lives here)
      Getenv     func(string) string    // environment lookup (API keys)
      Now        func() time.Time       // clock (log timestamps, entry timestamps)
      SessionID  string                 // the id a new session is created under
      Root       string                 // working directory the worker tools are rooted at
      Interrupts <-chan struct{}        // one receive per SIGINT (D5)
  }
  ```

- **`cmd/dory`** — `main` wires the real values: `os.Args[1:]`, `os.Stdin`,
  `os.Stdout`, `os.Stderr`, `os.UserHomeDir()`, `os.Getenv`, `time.Now`,
  `uuid.NewString()`, `os.Getwd()`, and a channel fed by `signal.Notify` for
  `SIGINT`. It contains no other logic.

**The session id is minted once, in `main`.** A new session's id is a version
4 UUID from `github.com/google/uuid`, minted by `main` and passed in as
`Deps.SessionID`; `Run` uses it only when no `--resume` is given. A random
source inside `Run` would make the store's path and the summary's session line
impossible to compare in a test. The `uuid` import lives in `cmd/dory` alone.

Dependencies point one way: `cmd/dory` → `internal/cli` → `{internal/options,
internal/model, internal/store, internal/agent, internal/render}`, and
`internal/agent` → `{internal/model, internal/store}`. `internal/options`,
`internal/model`, `internal/store`, and `internal/render` import nothing else
in this module. `internal/agent` must not import `internal/render`: the trace
is an interface the agent package declares (D5) and the render package
satisfies, so the tree can be tested with a recording fake.

**The network is not injectable.** agentkit sends every request through Go's
default HTTP client. Every test that needs a provider stands up an `httptest`
server and passes `-c <role>.base_url=` (D3), as agent-repl does.

**Wiring proof at the binary level.** One test builds the real binary, runs
it as a subprocess with a piped prompt, a temporary `HOME`, an API key in the
environment, and `-c` pairs pointing both roles at an `httptest` provider that
answers with a text reply, and checks its output and the session file it
wrote, including that the file's name and the summary's session line carry
the same well-formed version 4 UUID, the only place `main`'s minting can be
proven. This requirement depends on D2–D6 having landed and belongs in a final
phase.

**Version.** A single `var version` in `internal/cli` carries the version
string in source, never injected at build time. Its value is release data.

## REQUIREMENTS

- R-VSTJ-F5OX: The module MUST be `github.com/ikigenba/ikigenba/dory` with its own `go.mod` that specifies a Go version, and the set of `require` entries in that `go.mod` not marked `// indirect` MUST be exactly `github.com/ikigenba/ikigenba/agentkit`, `github.com/ikigenba/ikigenba/toolkit`, `github.com/google/uuid`, and `modernc.org/sqlite`, verified by a test that reads `go.mod` and consults neither `go.sum` nor the module cache.
- R-VU1F-SXFM: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-HCLS-94F3: Package `internal/cli` MUST export a `Deps` struct whose fields are exactly `Home string`, `Getenv func(string) string`, `Now func() time.Time`, `SessionID string`, `Root string`, and `Interrupts <-chan struct{}`.
- R-HDTO-MW5S: A pass run through `Run` MUST obtain its home directory, its environment lookups, its clock, its new-session id, and its tool root from the corresponding `Deps` fields, verified by injected values each of which is observable (the session file path, the credential used, the timestamps of log records in the store, the session id in the summary, and a worker tool's resolved root).
- R-HF1L-0NWH: Packages `internal/options`, `internal/model`, `internal/store`, and `internal/render` MUST import no other package of this module, and `internal/agent` MUST import no package of this module other than `internal/model` and `internal/store`.
- R-HG9H-EFN6: Package `internal/cli` MUST declare a package-level `var version string` whose value matches `^v[0-9]+\.[0-9]+\.[0-9]+$`, and `-V` MUST print exactly that value followed by a newline to stdout and exit 0.
- R-HHHD-S7DV: The binary built from `./cmd/dory`, run with a piped prompt on stdin, a temporary `HOME`, `OPENAI_API_KEY` set, and `-c` pairs setting both roles' `provider=openai`, `auth=api_key`, and `base_url=` to an `httptest` provider that answers with a text reply, MUST exit 0, MUST write a line beginning `1 assistant › ` followed by that reply to stdout, MUST write a summary whose `session` line carries a lowercase RFC 4122 version 4 UUID, and MUST leave exactly one file under `$HOME/.dory/sessions/` named that UUID followed by `.db`.
