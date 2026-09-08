# D1-layout-and-run-seam

`opsctl` is a Go CLI. Module path `github.com/ikigenba/ikigenba/opsctl`,
targeting the Go version pinned in `go.mod`. Its direct dependencies are the
AWS SDK modules named in the dependency requirement below, each approved by a
human and pinned to an exact version; every other package is standard
library, and the SDK is imported by exactly one package.

```
opsctl/                              (this sub-project; go.mod lives here)
├── cmd/opsctl/main.go               thin: os.Args/stdio/real deps → cli.Run(); os.Exit
├── internal/cli/                    grammar, usage, dispatch, exit codes, version, root check
├── internal/config/                 the host configuration store
├── internal/dns/                    DNS vocabulary, zone mapping, verbs; the provider seam
└── internal/dns/route53/            the Route 53 provider — the only package importing the SDK
```

- **`internal/config`** — the store at `/etc/ikigenba/config.json`: open,
  get, set, del, list, with the locking and atomic-write guarantees (D3). It
  knows nothing about flags or output.
- **`internal/cli`** — everything between the process and the packages that
  do work: the command grammar and usage text, the exit-code taxonomy, the
  root check, the version string, and the dispatch to each command (D2).

  ```go
  package cli

  // Run executes the CLI. args are the program arguments without the program
  // name; all I/O flows through the injected streams and every environmental
  // dependency through deps. Run never terminates the process; it returns
  // the process exit code.
  func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int

  // Deps carries what a command cannot be deterministic about.
  type Deps struct {
      Root   string                  // filesystem root every host path is resolved under ("/" in production)
      EUID   int                     // effective user id of the process
      Getenv func(key string) string // process environment; nil reads as empty
      DNS    dns.Env                 // provider registry and resolver (D4)
  }
  ```

- **`internal/dns`** and **`internal/dns/route53`** — DNS records behind a
  provider seam (D4).
- **`cmd/opsctl`** — `main` wires `os.Args[1:]`, `os.Stdin`, `os.Stdout`,
  `os.Stderr`, `Root: "/"`, `EUID: os.Geteuid()`, `Getenv: os.Getenv`, and
  `DNS: dns.Env{Open: route53.Open}` into `cli.Run` and passes the result to
  `os.Exit`. It contains no other logic.

**`Deps.Root` is why the gates need no root.** Every host path in every design
is stated as an absolute path such as `/etc/ikigenba/config.json`, and every
package resolves it under `Deps.Root`. Tests pass a temporary directory as
`Root` and `EUID: 0`, so the whole surface is exercised in-process by an
ordinary user with no privileged file touched. Later designs extend `Deps`
with a command runner and cloud clients as they need them; each extension is
a re-mint of the `Deps` requirement.

Dependencies point one way. `cmd/opsctl` imports `internal/cli`,
`internal/dns`, and `internal/dns/route53` (the last only to wire the
registry). `internal/cli` imports `internal/config` and `internal/dns`.
`internal/dns` imports `internal/config`. `internal/dns/route53` imports
`internal/dns` and the SDK. `internal/config` imports nothing else in this
module. No package but `internal/dns/route53` imports anything outside the
standard library and this module.

**Wiring proof at the binary level.** One test builds the real binary and
runs it as a subprocess with `--help`, which is exempt from the root check,
so the proof runs as any user.

## REQUIREMENTS

- R-LV0Z-17L3: The module MUST be `github.com/ikigenba/ikigenba/opsctl` with its own `go.mod` that specifies a Go version, and the direct (non-`// indirect`) `require` entries of that `go.mod` MUST be exactly `github.com/aws/aws-sdk-go-v2 v1.46.0`, `github.com/aws/aws-sdk-go-v2/config v1.33.3`, and `github.com/aws/aws-sdk-go-v2/service/route53 v1.69.0`, verified by a test that reads `go.mod`.
- R-MUPN-JCBU: Package `internal/cli` MUST export `Run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-LXGR-SR2H: Package `internal/cli` MUST export a `Deps` struct whose fields are exactly `Root string`, `EUID int`, `Getenv func(key string) string`, and `DNS dns.Env`, and a nil `Getenv` MUST read as an empty environment.
- R-MYDC-ONJX: Every host path a command reads or writes MUST be resolved under `Deps.Root`, verified by running `config set` and `config get` through `Run` with a temporary directory as `Root` and observing the file appear under that directory and nowhere else.
- R-LYOO-6IT6: Within this module, `cmd/opsctl` MUST import only `internal/cli`, `internal/dns`, and `internal/dns/route53`; `internal/cli` only `internal/config` and `internal/dns`; `internal/dns` only `internal/config`; `internal/dns/route53` only `internal/dns`; `internal/config` nothing; and no package other than `internal/dns/route53` MUST import a package outside the standard library and this module, verified by a test over the packages' import lists.
- R-N0T5-G71B: The binary built from `./cmd/opsctl`, run with the single argument `--help`, MUST print the usage text of D2 to stdout, write nothing to stderr, and exit 0.
