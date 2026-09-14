# D1-layout-and-run-seam

`devctl` is a Go CLI built and run on the developer's own machine, as an
ordinary user, under the developer's own AWS identity. Module path
`github.com/ikigenba/ikigenba/devctl`, targeting the Go version pinned in
`go.mod`. Its direct dependencies are the AWS SDK modules, the Smithy runtime
the SDK's errors are read with, and one TOML decoder — each approved by a
human and pinned to an exact version in the dependency requirement below; a
gate test keeps `go.mod` honest against it, so the build run can never promote
a module a human has not approved.

devctl does five jobs — manage spaces, push secrets, build an app, deploy one,
restore one — and every one of them reaches out: to six AWS services, to a host
over `ssh` and `scp`, to `git`, `go`, `tar`, `xz`, and `secret-tool`. All of
that enters the program through one seam so the gates can run offline, under an
ordinary identity, with no AWS credentials and no host to talk to.

The sub-project is a single Go module rooted at `devctl/`, where `go.mod` lives.
Above it sits one command, `cmd/devctl`, a thin `main` that turns the process's
arguments and streams into a call and the result into an exit status. Below it
sit the internal packages named in the layout requirement: the run seam, the
CLI, the cloud boundary and the one package that implements it over the AWS
SDK, the three packages a command reads its world from, one package per
command, and the host a space is reached on.

Each package is one concern, sized so a reader or a build agent holds it
whole. A later design declares its exported names into the package named here;
it never invents a package of its own, because a new or split package is a
re-mint of the layout requirement below.

- **`internal/seam`** — the run seam: the `Deps` bundle every package reads its
  environment from, and external process execution. It is the one package the
  whole tree may import, so the bundle has exactly one home and no design
  invents a second.

  `Deps` carries what a command cannot be deterministic about — the working
  directory relative paths resolve under, the effective user id, the process
  environment, the opener that turns a profile name into that account's clients
  (D3), the runner external processes go through, and the two clock functions
  polling needs — and a test passes a fake for every one of them. Process
  execution adds three more names beside it — a description of one process to
  run (`ssh`, `scp`, `tar`, `xz`, `git`, `secret-tool` or `go`), the output and
  exit status it produced, and the function type the real executor satisfies and
  a test substitutes for.

- **`internal/cli`** — everything between the process and the packages that do
  work: the command grammar and usage text, the exit-code taxonomy, the root
  check, the version string, the diagnostic shape, and the dispatch to each
  command (D2).

  Its `Run` takes the program arguments without the program name, the three
  standard streams, and the `Deps` bundle; all I/O flows through the injected
  streams and every environmental dependency through `Deps`. It never
  terminates the process — it returns the exit code and lets `main` do that.

- **`internal/cloud`** and **`internal/cloud/awssdk`** — the cloud behind one
  seam, as `opsctl` puts Route 53 behind its DNS provider seam. `internal/cloud`
  declares `Clients`, the narrow per-service interfaces it holds, the `Opener`
  that turns a profile name into `Clients`, and the shape of an AWS failure
  (service, operation, code) that every diagnostic is rendered from (D3). The
  interfaces speak devctl's vocabulary, not the SDK's, which is what keeps the
  SDK inside one package: `internal/cloud/awssdk` implements them over the real
  services and is the only package in this module that imports the SDK. Above
  the seam every test fakes the interfaces; `awssdk`'s own tests run against a
  fake HTTP endpoint. Nothing in the gates reaches AWS.

- **`internal/account`**, **`internal/checkout`**, **`internal/keyring`** — the
  two worlds a command reads before it changes anything: the account (its
  properties at `/ikigenba/account`, the caller's account id, the hosted zone a
  domain belongs to, and the spaces the account's tags name) and the developer's
  machine (the checkout root, its apps and their `etc/manifest.toml`, the git
  facts `build` insists on, and a secret value from the login keyring). D3 and
  D4 declare them. `internal/checkout` is the only package that decodes TOML,
  so the manifest has one reader wherever its bytes come from — a file in the
  checkout or a member of a tarball (D9).

- **`internal/secrets`**, **`internal/space`**, **`internal/spacecreate`**,
  **`internal/build`**, **`internal/deploy`**, **`internal/restore`** — one
  package per command, each owning its own subcommand grammar, its own usage
  text, and its step output (D5–D9). `space create` is its own package because
  it is its own design and its own weight; it uses the instance, address, record
  and role verbs `internal/space` declares for `list`, `status`, `stop`, `start`
  and `destroy`.

- **`internal/host`** — a space's host seen from this machine: the `ssh` and
  `scp` command lines, the `ec2-user` identity, the `sudo` prefix on privileged
  ones, how a remote exit status and a remote command's output are reported, and
  the waits for a host to become reachable. It runs everything through
  `Deps.Exec`, so the developer's own `~/.ssh/config`, agent and keys apply and
  no Go ssh library is needed. D6 declares it; D7 and D9 use it.

- **`cmd/devctl`** — `main` wires `context.Background()`, `os.Args[1:]`,
  `os.Stdin`, `os.Stdout`, `os.Stderr`, and one `seam.Deps` — `Dir` from
  `os.Getwd()`, `EUID: os.Geteuid()`, `Getenv: os.Getenv`, `Cloud: awssdk.Open`,
  `Exec: seam.Exec`, `Now: time.Now`, `After: time.After` — into `cli.Run` and
  passes the result to `os.Exit`. It contains no other logic.

**Why an opener and not clients.** `--account` names the profile a command acts
in, and `restore` acts in two accounts at once (`--from-account`), so the thing
the seam can carry is not a set of clients but the means to get one:
`Deps.Cloud(ctx, profile, region)` — a `cloud.Opener`, whose exact signature D3
declares. A fake opener returns fakes and records which profiles and regions
were asked for, which is how a two-account restore is tested with no
credentials anywhere.

**Why `Deps.Dir` is why the gates need no home directory.** Every path devctl
reads or writes is inside the checkout — an app's `<app>/dist/`, its `etc/` —
and every one is resolved under `Deps.Dir`, which is also the working
directory of every process the seam runs. Tests pass a temporary directory, so
the whole surface is exercised in-process without touching the developer's
files. Nothing in devctl reads the home directory: `~/.ssh` is `ssh`'s own
business and the keyring is `secret-tool`'s.

**The nil-means-the-real-thing convention**, as `opsctl`'s `Deps` has it: a nil
`Getenv` reads as an empty environment, a nil `Exec` as `seam.Exec`, a nil `Now`
as `time.Now`, a nil `After` as `time.After`. `Cloud` has no default on purpose
— a nil that quietly meant "load the developer's real AWS configuration" is the
one default this project must not have.

**A non-zero exit is data, not an error.** Three stories print an external
command's exit status and then its output — `devctl: install: ssh
ec2-user@3.19.79.227 sudo opsctl install /tmp/gmail-v0.1.0.tar.xz: exit status
1`, followed by opsctl's own stderr. So `Runner` hands back a `Result` with the
status and both streams, and reserves `error` for a process that never ran. A
`Cmd` with no `Path` or no `Dir` is that kind of failure: the seam refuses it
rather than inheriting the test runner's working directory.

**Dependencies, observed.** Ten direct modules, proved on this machine to
resolve and build together: `go mod tidy` over a module requiring exactly these
ten settles on one AWS core, and a program importing all ten compiles. Which
release of each satisfies that is data, and it lives in `go.mod`; the design
says only which modules devctl depends on and why.

| module | why |
|---|---|
| `github.com/aws/aws-sdk-go-v2` | the SDK core |
| `github.com/aws/aws-sdk-go-v2/config` | the shared-config loader `--account` names a profile in |
| `github.com/aws/aws-sdk-go-v2/service/ec2` | instances, tags, volumes, Elastic IPs |
| `github.com/aws/aws-sdk-go-v2/service/ssm` | Parameter Store: account properties and secrets objects |
| `github.com/aws/aws-sdk-go-v2/service/route53` | the space's records and the zone it belongs to |
| `github.com/aws/aws-sdk-go-v2/service/s3` | the backup bucket `destroy` empties and `restore` copies |
| `github.com/aws/aws-sdk-go-v2/service/iam` | the space's role, inline policy, and instance profile |
| `github.com/aws/aws-sdk-go-v2/service/sts` | the caller's account id, substituted into the policy |
| `github.com/aws/smithy-go` | the API error code a diagnostic quotes (`ParameterNotFound`) |
| `github.com/BurntSushi/toml` | decodes `etc/manifest.toml` |

**Wiring proof at the binary level.** Every in-process test injects its own
streams and `Deps`, so none can prove `main` wired the real ones. One test
builds the real binary and runs it as a subprocess with `--help`. Nothing is
exempt from the root check (D2 refuses a zero uid before it reads the
arguments at all), so the proof is valid only for the ordinary user the gates
run as — which is the identity devctl is built for, and the one `main` has to
wire `os.Geteuid` correctly for. A second test parses
`main.go` and fails if the `seam.Deps` literal there has stopped naming every
field of the struct, which is what keeps a later design's new field from
reaching production unwired.

**Dependencies point one way.** `cmd/devctl` imports `internal/cli`,
`internal/seam`, and `internal/cloud/awssdk` — the last two only to wire the
real seams. `internal/cli` is imported by nothing else: it is the top of the
tree, and every command package sits below it. `internal/cloud` imports nothing
of this module and `internal/seam` only `internal/cloud`, so the seam is a leaf
every package can take. Outside the standard library and this module, only
`internal/cloud/awssdk` (the AWS and Smithy modules) and `internal/checkout`
(the TOML module) import anything at all — that is what turns "the SDK lives in
one package" and "TOML has one reader" from a hope into a test.

## REQUIREMENTS

- R-UNLP-X6MH: The module MUST be `github.com/ikigenba/ikigenba/devctl` with its own `go.mod` that specifies a Go version, and the module paths of the direct (non-`// indirect`) `require` entries of that `go.mod` MUST be exactly `github.com/BurntSushi/toml`, `github.com/aws/aws-sdk-go-v2`, `github.com/aws/aws-sdk-go-v2/config`, `github.com/aws/aws-sdk-go-v2/service/ec2`, `github.com/aws/aws-sdk-go-v2/service/iam`, `github.com/aws/aws-sdk-go-v2/service/route53`, `github.com/aws/aws-sdk-go-v2/service/s3`, `github.com/aws/aws-sdk-go-v2/service/ssm`, `github.com/aws/aws-sdk-go-v2/service/sts`, and `github.com/aws/smithy-go`, verified by a test that reads `go.mod`.
- R-9XGV-PDET: The module MUST contain no package other than `cmd/devctl`, `internal/seam`, `internal/cli`, `internal/cloud`, `internal/cloud/awssdk`, `internal/account`, `internal/checkout`, `internal/keyring`, `internal/secrets`, `internal/space`, `internal/spacecreate`, `internal/host`, `internal/build`, `internal/deploy`, and `internal/restore`, verified by a test that lists the module's packages.
- R-U72C-BSYD: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-9YOS-355I: Package `internal/seam` MUST export a `Deps` struct whose fields are exactly `Dir string`, `EUID int`, `Getenv func(key string) string`, `Cloud cloud.Opener`, `Exec Runner`, `Now func() time.Time`, and `After func(d time.Duration) <-chan time.Time`; a nil `Getenv` MUST read as an empty environment, a nil `Exec` MUST read as `seam.Exec`, a nil `Now` MUST read as `time.Now`, and a nil `After` MUST read as `time.After`.
- R-9ZWO-GWW7: Package `internal/seam` MUST export a `Cmd` struct whose fields are exactly `Path string`, `Args []string`, `Dir string`, and `Env []string`; a `Result` struct whose fields are exactly `Stdout []byte`, `Stderr []byte`, and `ExitCode int`; the type `Runner func(ctx context.Context, cmd Cmd) (Result, error)`; and `Exec(ctx context.Context, cmd Cmd) (Result, error)`, which MUST satisfy `Runner`.
- R-A14K-UOMW: `seam.Exec` MUST report a process that exited non-zero as a `Result` carrying that exit status in `ExitCode` with a nil error, MUST return a non-nil error only when the process could not be started or `ctx` ended first, and MUST return a non-nil error without starting any process when `Cmd.Path` or `Cmd.Dir` is empty.
- R-A2CH-8GDL: `seam.Exec` MUST run the program named by `Cmd.Path`, found on `PATH`, with `Cmd.Args` as its arguments and `Cmd.Dir` as its working directory, MUST give it the environment the devctl process was started with plus `Cmd.Env`, whose entries override inherited entries of the same name, and MUST capture its standard output and standard error into separate `Result` fields.
- R-EG57-YV7O: Every filesystem path a command reads or writes MUST be resolved under `Deps.Dir` or under the checkout root that `internal/checkout` derives from `Deps.Dir`, and never under the developer's home directory or any other absolute location; every `seam.Cmd` a command passes to `Deps.Exec` MUST carry a `Dir` equal to one of those two directories or a path under either; verified by running a command through `Run` with a temporary directory as `Deps.Dir`, and a fake `Exec` that reports that same directory as the checkout root, and observing that every file written appears under that directory and that every `Cmd.Dir` recorded is within it.
- R-A4S9-ZZUZ: Within this module, `cmd/devctl` MUST import only `internal/cli`, `internal/seam`, and `internal/cloud/awssdk`; `internal/cli` MUST be imported by no other package of this module; `internal/cloud` MUST import no other package of this module and `internal/seam` only `internal/cloud`; and no package MUST import a package outside the standard library and this module, except `internal/cloud/awssdk`, which MAY import the `github.com/aws/` modules the dependency requirement names, and `internal/checkout`, which MAY import `github.com/BurntSushi/toml`; verified by a test over the packages' import lists.
- R-VW1U-BL0I: No package of this module other than `cmd/devctl` MUST reference `os.Getenv`, `os.Getwd`, `os.UserHomeDir`, or `os.Geteuid`; no package other than `cmd/devctl` and `internal/seam` MUST reference `os.Environ`; no package other than `cmd/devctl` and `internal/cli` MUST reference `time.Now`, `time.Since`, `time.Sleep`, `time.After`, or `time.Tick`; and no package other than `internal/seam` MUST import `os/exec`; verified by a test over the packages' syntax trees.
- R-A782-RJCD: `cmd/devctl` MUST consist of a `main` that builds one `seam.Deps` composite literal naming every field of `seam.Deps` and passes the result of `cli.Run` to `os.Exit`, verified by a test that parses `cmd/devctl/main.go` and compares the literal's field names with the fields of `seam.Deps` obtained by reflection.
- R-F59F-A9GW: The binary built from `./cmd/devctl`, run with the single argument `--help` as a non-root user, MUST print the top-level usage text of D2 to stdout, write nothing to stderr, and exit 0.
