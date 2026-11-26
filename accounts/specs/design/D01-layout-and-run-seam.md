# D01-layout-and-run-seam

accounts runs the SSH hosts of the business. It keeps EC2 hosts it creates through the AWS SDK for Go v2, the accounts (customer businesses) it places on them, and the users of each account. A second binary, `accountsd`, runs as root on every host and reconciles it against the desired state it pulls from accounts. accounts' own SQLite database, `state/accounts.db`, is the source of truth (outcome 8). accounts records every change and every host state change in telemetry's trail, offers its MCP tools (`D07-tools`) at `/mcp` as the only API for the space's callers, offers the box tools (`D19-box-tools`) to the users of its hosts, whose daemons forward them to `/accountsd/v1/box`, serves read-only pages to a signed-in user, and serves one page with a form to anyone holding an invite link, the enrollment page at `/join?token=<token>` (`D18-enrollment`). Outcomes cited as outcome N are those of /home/mgreenly/Documents/ikigenba-ssh-hosts.md. Log entries are those of /home/mgreenly/Documents/ikigenba-ssh-hosts-decisions.md. Self-service outcome N is outcome N of /home/mgreenly/Documents/ikigenba-self-service-accounts.md.

This document lays the structural ground the others stand on:

- where the code lives and what each package of both binaries is for
- the seam through which the process is run, so every part can be tested without a real process
- the settings and how they are read
- how the cloud is chosen (real, sandbox or none)
- the database path, the daemon binary's path, and the accounts URL and binding
- where the identity and the display string come from
- the binary's wiring
- the rule that accounts calls no sibling

AWS is outside the suite, not a sibling, so that rule still holds.

The module is `github.com/ikigenba/ikigenba/accounts`, rooted at the sub-project directory, with `go.mod` beside `specs/`. `devctl build` finds an app as a `package main` in `cmd/<app>`, so `cmd/accounts` holds accounts' `main`. It also ships every other `cmd/<x>` as `<app>/bin/<x>` (log D5, D11), so `cmd/accountsd` holds the daemon's `main` (`D14-accountsd`). Both are wiring only.

## The dependencies

Besides the standard library, accounts depends on two modules:

- appkit, at a published release. accounts uses its `db`, `version`, `services`, `page`, `paging`, `identity`, `mcp` and `telemetry` packages. `db` brings the pure-Go SQLite driver.
- the AWS SDK for Go v2 (approved, log P3), for EC2, Route 53 and STS (`D05-cloud`).

Neither module has a `replace` directive, and no `go.work` exists. The release build of both binaries stays cgo-free.

## Packages

The root package, named `accounts`, holds what the module embeds:

- `Assets()`: the six templates `about.html`, `host.html`, `join.html`, `landing.html`, `notfound.html` and `tools.html`.
- `Etc()`: `manifest.toml` and `nginx.conf`.
- `Migrations()`: `0001_accounts.sql`, kept byte for byte, `0002_hosts.sql`, which adds the hosts' tables, `0003_bans.sql`, which adds the allowlist and unban tables, `0004_invites.sql`, which adds the admin flags' and the invites' tables, `0005_host_records.sql`, which adds the `host_records` table, each host's record change id, record reason and, once deleted, whether it is settled, and `0006_kept_volumes.sql`, which adds the `delete_choices` and `kept_volumes` tables, each delete's choice about the host's data and the data volumes kept from deleted hosts (`D04-store`).

`share/icon.svg` is packed beside `bin/` by `devctl build` and is not embedded (`D09-on-a-space`).

Each internal package holds one concern. The document named declares that package's shapes and behaviour. They are listed bottom-up: a package imports only packages listed before it. That is guidance, not contract.

- `internal/valid`: names, reserved lists, keys, sizes and caps (`D17-names-keys-sizes`).
- `internal/settings`: the settings and the cloud mode they select, `SandboxHosts`, and the accounts URL and binding (this document).
- `internal/cloud`: the cloud seam, its types, errors and tag constants (`D05-cloud`).
- `internal/protocol`: the daemon's wire types and limits (`D11-protocol`).
- `internal/trail`: the trail events (`D08-trail`).
- `internal/cloud/awssdk`: the AWS SDK adapter that accounts uses (`D05-cloud`).
- `internal/cloud/awshost`: STS presign and IMDS, used only by `accountsd` (`D05-cloud`).
- `internal/cloud/memory`: the in-memory cloud for the sandbox and tests (`D16-sandbox`).
- `internal/hostauth`: the presigned-identity header and its verification (`D12-host-identity`).
- `internal/machine`: the `Machine` interface and the in-memory machine (`D15-machine`).
- `internal/linux`: the real host (`D15-machine`).
- `internal/store`: the record and the migration (`D04-store`).
- `internal/ops`: the operations engine (`D10-operations`).
- `internal/daemonapi`: `/accountsd` and `/accountsd/v1/*` (`D13-daemon-endpoints`).
- `internal/daemon`: `accountsd`'s loop and self-update (`D14-accountsd`).
- `internal/sim`: the simulated daemons in the sandbox (`D16-sandbox`).
- `internal/tools`: the MCP tools (`D07-tools`).
- `internal/box`: the box tools, which a host's users reach through their host's daemon (`D19-box-tools`).
- `internal/boxmcp`: the stdio MCP server that a host's users run as `accountsd-mcp`, the box tools' other end (`D20-box-mcp`).
- `internal/pages`: the templates' data and the page routes (`D06-pages`), and `pages.ServiceName` (this document).
- `internal/join`: the enrollment page at `/join?token=<token>` and its rate limiter (`D18-enrollment`).
- `internal/server`: a listener's life (`D03-serve`).
- `internal/web`: the route table (`D03-serve`).
- `internal/cli`, split by document:
  - the run seam, `cli.Process` and `cli.Run` (this document)
  - the commands, exit codes, `cli.Manifest` and `cli.Usage` (`D02-cli`)
  - the serve path (`D03-serve`)
  - `cli.NginxConf` (`D09-on-a-space`)

`cmd/accounts` links every package above but `awshost` and `linux`; `daemon` and `machine` reach it only through `sim`. `cmd/accountsd` links `daemon`, `linux` and `awshost`, and never EC2 or Route 53. The old `internal/host`, the opsctl seam, is gone, and with it `ACCOUNTS_HOST_COMMAND` and `host.DefaultCommand`. accounts runs no program.

## Where the environment is read

`main` makes the one read of the environment that is not `Run`'s own. It calls appkit's `version.Read` exactly once, first, and holds the result, the **identity**, whose `String()` is the **display string**. The identity goes to `page.New`, whose banner carries its `Release` and `Commit` to every page's footer. The display string goes to the `mcp.ServerConfig` that `Process.MCP` builds, and to `Run` as `Process.Version`. `Process.MCP` builds every MCP server the run serves, three in all: the one at `/mcp` and the box path's two tiers (`D03-serve`). `Run` never calls `mcp.NewServer` itself, because it reads `IKIGENBA_SERVICES` from the process environment.

`Run` reads every variable through `Process.LookupEnv`. At start, through `settings.Read`, it reads `DRAIN_SECONDS`, `IKIGENBA_SANDBOX`, `AWS_REGION` and the four `IKIGENBA_HOSTS_*` keys. For the socket and readiness it reads `LISTEN_PID`, `LISTEN_FDS` and `NOTIFY_SOCKET`. It reads `IKIGENBA_SERVICES` once, when it starts to serve. That is the whole set of eleven.

Two readers sit outside that set:

- appkit's constructors read `IKIGENBA_SERVICES` from the process environment.
- In AWS mode, `awssdk.Open` loads the SDK's default credential chain, which reads the process environment and the instance's metadata service (IMDS).

Both are recorded exceptions. Tests never reach the second, because they hand in `Process.Cloud`.

## The run seam

The run seam is `cli.Run`. It takes a context and a `cli.Process` carrying everything the program would otherwise take from the `os` package, and returns the exit code. These are its fields:

- `Now` is the clock behind every time accounts stamps: created times, step times, report times, and every event's time.
- `Sleep` is the pause appkit's writer waits with between delivery attempts.
- `After` is the timer source of the operations engine and the simulated hosts. nil means `time.After`.
- `Rand` is the source of every request id, every operation id, and, in the sandbox, every fake resource id. `Run` serializes its reads.
- `Executable` names the running binary, so the daemon binary beside it can be found. nil means `os.Executable`.
- `Cloud` opens the real cloud in AWS mode. nil means `awssdk.Open`. Tests always hand a fake or a memory cloud here.
- `Inherit`, `Now` and `Rand` must be given by the caller whenever `Args` is empty. `cmd/accounts` chooses them once, at the process boundary: the process's own descriptor 3, `time.Now` and `crypto/rand.Reader`.

## Settings and the cloud mode

accounts reads seven values at start:

- `DRAIN_SECONDS` is read exactly as before.
- `IKIGENBA_SANDBOX` is set by the sandbox and never in production. accounts asks only whether it is present.
- The five hosts keys are `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` and `IKIGENBA_HOSTS_INSTANCE_PROFILE`. opsctl writes them from its configuration (log D9) and judges none of them, so accounts takes them verbatim and checks nothing more about them. They land in `settings.Hosts`.

`Settings.Cloud()` picks the mode:

- **AWS** when all five hosts values are non-empty, sandbox or not.
- **sandbox** when none of the five is set and `IKIGENBA_SANDBOX` is present. The hosts settings in effect are then `settings.SandboxHosts`, which names no space.
- **none** otherwise, so a partly set space, sandbox or not, is unconfigured.

`Settings.HostsInUse()` answers the hosts settings in effect. Consumers judge **configured** as `Cloud() != ModeNone`. Unconfigured, the host, account and user tools refuse, the landing page says so, and the engine does not run. So the tools work only where opsctl's configuration names the hosts, and nothing in the code names a space (outcome 4).

In AWS mode the cloud is what `Process.Cloud` opens for the region. In sandbox mode it is `D16-sandbox`'s memory cloud, kept in `state/sandbox-cloud.json` so that it survives a restart. In mode none there is no cloud. How the start handles a cloud that fails to open is `D03-serve`'s.

## Paths

These paths resolve against `Process.Dir`, or the process's working directory when `Dir` is empty:

- The database is `state/accounts.db`, opened with `db.Open`, the root package's `Migrations()` and `Process.Now`. `state/` is created when absent.
- In sandbox mode the memory cloud's file is `state/sandbox-cloud.json`.
- In sandbox mode the simulated hosts' box sockets are under `state/box`, which the simulation creates only while a host holds a user (`D16-sandbox`).

accounts writes nothing else under `Dir` and nothing outside it, except that in sandbox mode it removes `state/sandbox-bans`, the file a person drops for the simulated hosts (`D16-sandbox`).

The daemon binary accounts serves at `/accountsd` (`D13-daemon-endpoints`) is the file `accountsd` beside the running executable. `Run` resolves that path once, at start, with `filepath.EvalSymlinks`. The bytes served and their digest then stay those of the release that started, even when `/opt/ikigenba/current` is switched to another release. On a space that file is `/opt/ikigenba/current/accounts/bin/accountsd`. In a sandbox it is `<data>/bin/accountsd` (log D11).

## The accounts URL and the binding

Two values come from the services file's entry `accounts`. Both are read afresh on every call, so a changed services file reaches the next operation and the next verification. No other services-file read is added.

- The **accounts URL** is the entry's URL with its trailing `/` removed. It is tagged on every instance as `AccountsURL` (log tag contract), and user data hands it to the daemon.
- The **binding** is that URL's host, with its port if any, lowercased. Every daemon signs the binding into its identity header, and the verifier (`D12-host-identity`) expects it.

When there is no usable entry:

- In sandbox mode the binding is `settings.SandboxBinding` and the accounts URL is `http://` followed by it.
- Otherwise both refuse with `settings.ErrNoAccountsURL`. A create step then fails with `ops.ReasonNoAccountsURL` (`D10-operations`), and identity answers `hostauth.ErrUnavailable` (`D12-host-identity`).

## What reaches the consumers

`Run` builds one store, one writer and, outside mode none, one cloud.

- **Tools** take the store, the cloud, the hosts in use, whether the run is configured, the engine's `Wake`, the writer and the accounts URL source, which an invite's link is built on (`tools.Config`, `D07-tools`).
- **Pages** take the store, the banner, the hosts domain in use and whether the run is configured (`pages.Config`, `D06-pages`).
- **The enrollment page** takes the template set, the store, whether the run is configured, the hosts domain in use, a rate limiter over `Now` and the writer (`join.Config`, `D18-enrollment`).
- **The engine** takes the store, the cloud, the hosts in use, the accounts URL source, the writer, `Now` and `After` (`ops.Config`, `D10-operations`).
- **The verifier** takes the region in use, the instance profile in use as the role, the binding source, the cloud's STS and `Now` (`hostauth.VerifierConfig`, `D12-host-identity`).
- **The daemon endpoints** take the store, the verifier, the resolved daemon binary path, the writer, `Now` and whether the run is configured (`daemonapi.Config`, `D13-daemon-endpoints`).
- **The simulated hosts**, in sandbox mode only, take the memory cloud, the handler, the binding, the served digest, `Now`, a `Sleep` and `After` (`sim.Config`, `D16-sandbox`).

The order is `D03-serve`'s. The requirements below fix only what reaches the consumers, as observed at `Run`.

## Siblings and the outside

accounts calls no sibling. Telemetry is reached through appkit's sink, as every app reaches it. Outside the suite it reaches only AWS, in AWS mode, through the cloud `Process.Cloud` opened. In the sandbox the simulated daemons call accounts' own handler in process, never over a socket. It runs no program: `opsctl` and `sudo` are no longer reached (outcome 27, expand half).

Three seams make this observable:

- `cli.Process` holds no HTTP client and no sibling address.
- `Run` looks up no variable but the eleven above.
- A `Run`-level test names every sibling in the services file, each on a listener that records every connection. It exercises every tool and page, the enrollment page's form included, and proves none is reached.

The binary-level test composes `PATH` with recording `sudo` and `opsctl` stubs, and proves that neither runs.

## Recorded decisions

- **The cloud is chosen by `Settings.Cloud()`, and the sandbox only by `IKIGENBA_SANDBOX` with no hosts value set.**
  - Precedent: accounts' previous `Settings.HostFields`, where a non-blank `ACCOUNTS_HOST_COMMAND` won over the sandbox. Here a complete hosts configuration wins over the sandbox in the same way.
  - Any partial configuration is mode none, sandbox or not. A half-configured space then refuses rather than half-working, and a sandbox never mixes a real setting into a fake cloud.
- **Hosts values are verbatim.** Precedent: opsctl D08, which passes them on unjudged and leaves it to the app that operates hosts to judge them. A value of only white space counts as set. AWS refuses a bad value when it is first used, and the failure lands as the operation's reason. accounts adds no shape check, so an operator never meets two judges of one value.
- **`settings.SandboxHosts` is a package variable**. It holds the region `sandbox`, the domain `hosts.test` (an RFC 6761 test name), zone `ZSANDBOX`, launch template `sandbox-hosts` and instance profile `sandbox-hosts`. `D16-sandbox`'s memory cloud answers ARNs for that profile, and its STS host for that region.
- **`HostsInUse()`** is added so that one method answers "which hosts settings apply", and the tools, pages, engine and verifier cannot disagree. It answers `SandboxHosts` in sandbox mode, the read values in AWS mode, and zero in mode none.
- **The accounts URL and binding** are methods on `Settings` taking the services path.
  - They read the file afresh, as the pages read `auth` afresh (`D06-pages`).
  - An entry is usable only when its URL parses as an absolute `http` or `https` URL with a host. Anything else is "no entry", because a binding must be a host.
  - Every trailing `/` is removed, so the tagged URL never ends in one.
  - The sandbox fallback applies to any unusable entry, so a sandbox with no `accounts` entry still verifies its simulated daemons.
- **`Process.Cloud`, `Process.After` and `Process.Executable`** are new fields at the end of `Process`. Today's fields keep their order. Their nil defaults are stated in the field's declaration, as `Sleep`'s is. No behavioural requirement states the nil `Cloud` default: observing it would reach real AWS, which no permitted test may do. `cmd/accounts` passes `awssdk.Open` explicitly, and the finish line proves the real chain.
- **The daemon binary path is resolved once, at start, with `filepath.EvalSymlinks`** (log D11). Re-resolving per request would serve a newer release's binary while the digest computed at start named the old one. A daemon would then install bytes whose digest it never saw. When `Executable` fails, or the resolved file is absent, the daemon endpoints get a path to no file. `D13-daemon-endpoints` then answers `/accountsd` 404, and the start does not fail.
- **The sandbox memory cloud lives in `state/`**, beside the database. Fake state must survive an accounts restart for resume to be observable, and `sandbox wipe` removes it with the app directory. The precedent is accounts' previous in-memory host, which was empty at each start: a departure, for that reason.
- **Mode none rather than a start failure for an unconfigured space.** accounts ships on every space (outcome 4), and a space that does not operate hosts must still serve.
- **The binary-level `PATH` stub** (R-ETO0-MM7J) proves outcome 27's "stops calling opsctl". It runs in sandbox mode, where every changing tool does its work, so a build that still shells out to `sudo opsctl` from any tool is caught; in mode none the tools refuse before any work and would prove nothing. It relies on accounts/AGENTS.md's exec'ing test keeping a composed-`PATH` case with recording `sudo` and `opsctl` stubs that must never run. accounts/AGENTS.md keeps no stub case that expects a run.
- **Tests these requirements need**, each permitted by `accounts/AGENTS.md`, Test discipline:
  - `Run`-level tests drive the sandbox mode with `IKIGENBA_SANDBOX` and a temp `Dir`, and the AWS mode with a recording fake `cloud.Cloud` handed as `Process.Cloud`.
  - The binary-serving test points `Executable` at a temp file through a symlink.
  - The exec'ing test uses `IKIGENBA_SANDBOX`.
- **`pages.ServiceName` stays declared here**: `main`, the banner kit, the MCP server, the database and the manifest all need the name.

## Consumer usage

`main` (wiring): `id := version.Read()`, `v := id.String()`, then `os.Exit(cli.Run(ctx, cli.Process{...}))`. The `Process` it builds is:

- `Args: os.Args[1:]`, `LookupEnv: os.LookupEnv`, `Unsetenv: os.Unsetenv`, `Pid: os.Getpid()`
- `Stdout: os.Stdout`, `Stderr: os.Stderr`, `Version: v`
- `Inherit` taking descriptor 3, `Now: time.Now`, `Rand: rand.Reader`
- `Banner: page.New(pages.ServiceName, id).Banner`
- `MCP` returning `mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})`, a new server at each of `Run`'s three calls (`D03-serve`)
- `After: time.After`, `Executable: os.Executable`, `Cloud: awssdk.Open`

`ctx` is cancelled on `SIGTERM` or `SIGINT`, with a cause whose `Error` is the signal's name.

`Run`, serving:

1. It reads `s, err := settings.Read(p.LookupEnv)`. A refusal writes `accounts: ` and the error and returns `ExitUsage`.
2. It takes `hosts := s.HostsInUse()` and switches on `s.Cloud()`:
   - `settings.ModeAWS`: `c, err := p.Cloud(ctx, hosts.Region)`.
   - `settings.ModeSandbox`: `memory.New(memory.Config{Path: filepath.Join(p.Dir, "state", "sandbox-cloud.json"), Now: p.Now, Rand: rnd, Hosts: settings.SandboxHosts})`.
   - `settings.ModeNone`: no cloud.
3. It resolves the daemon binary once: `exe, _ := p.Executable()`, then `bin, _ := filepath.EvalSymlinks(filepath.Join(filepath.Dir(exe), "accountsd"))`.
4. It hands each consumer what it needs:
   - The engine gets `ops.Config.AccountsURL = func() (string, error) { return s.AccountsURL(servicesPath) }`.
   - The verifier gets `hostauth.VerifierConfig.Binding = func() (string, error) { return s.Binding(servicesPath) }`, `Region: hosts.Region` and `Role: hosts.InstanceProfile`.
   - The tools get `tools.Config.AccountsURL` from the same `s.AccountsURL(servicesPath)` (`D03-serve`).
   - The tools, pages, enrollment page and daemon endpoints get `Configured = s.Cloud() != settings.ModeNone`, and the tools, pages and enrollment page get `hosts` or its `Domain`.

A test of settings reads `s, _ := settings.Read(lookup)` over a map holding only `IKIGENBA_SANDBOX` (empty value). It then expects:

- `s.Cloud() == settings.ModeSandbox`
- `s.HostsInUse() == settings.SandboxHosts`
- `s.AccountsURL("")` to answer `"http://accounts.sandbox.test"`
- `s.Binding("")` to answer `settings.SandboxBinding`

Adding `AWS_REGION` alone to that map makes `s.Cloud()` answer `settings.ModeNone`. A test of the run seam serves `cli.Run` with:

- a temporary `Dir`
- `IKIGENBA_SANDBOX` present
- a `Now`, an `After` and a `Rand` it controls
- an `Executable` pointing into its temp tree
- a `*telemetry.Capture` as `Sink`

It calls `create_host` and `host` through `mcp.Client`, cancels, and loads `state/sandbox-cloud.json` with `memory.New`. For AWS mode it sets the five hosts variables and hands a recording fake as `Process.Cloud`.

## REQUIREMENTS

- R-7QD1-Z5PQ: The module's root package, imported from the path `github.com/ikigenba/ikigenba/accounts` with the package name `accounts`, MUST export `func Assets() fs.FS`, where `fs` is the standard library's `io/fs`, returning, on every call and whatever the working directory, a file system whose root directory holds exactly the regular files `about.html`, `host.html`, `join.html`, `landing.html`, `notfound.html` and `tools.html` and no other entry.
- R-C7K6-723O: The root package MUST export `func Etc() fs.FS`, returning, on every call and whatever the working directory, a file system whose root directory holds exactly the regular files `manifest.toml` and `nginx.conf` and no other entry.
- R-7SFE-Z3X0: The root package MUST export `func Migrations() fs.FS`, returning, on every call and whatever the working directory, a file system whose root directory holds exactly the regular files `0001_accounts.sql`, `0002_hosts.sql`, `0003_bans.sql`, `0004_invites.sql`, `0005_host_records.sql` and `0006_kept_volumes.sql`, whose bytes `D04-store` states, and no other entry.
- R-22LW-2NEW: The `internal/cli` package, imported from the path `github.com/ikigenba/ikigenba/accounts/internal/cli` with the package name `cli`, MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Version string; Inherit func(fd uintptr) (net.Listener, error); Now func() time.Time; Sleep func(ctx context.Context, d time.Duration); Rand io.Reader; Dir string; Sink telemetry.Sink; Banner func(u page.User) page.Banner; MCP func(w *telemetry.Writer) *mcp.Server; After func(d time.Duration) <-chan time.Time; Executable func() (string, error); Cloud cloud.Opener }`, with exactly these fields in this order, where `page`, `mcp` and `telemetry` are the packages `github.com/ikigenba/ikigenba/appkit/page`, `github.com/ikigenba/ikigenba/appkit/mcp` and `github.com/ikigenba/ikigenba/appkit/telemetry` and `cloud` is accounts' `internal/cloud` (`D05-cloud`); `Args` are the program's arguments without the program name; `LookupEnv` is the environment every variable `Run` reads is looked up in, and `Unsetenv` removes a variable from it; `Pid` is the id of the process `Run` speaks for; `Stdout` and `Stderr` are its output streams; `Version` is the display string `Run` writes for `--version` (`D02-cli`) and gives the writer it builds as its version; `Inherit` takes an inherited descriptor as a listener; `Now` is the clock; `Sleep` is the pause the writer `Run` builds waits with between delivery attempts, nil meaning a pause of real time that ends early when its context is done; `Rand` is the source the request ids, operation ids and sandbox resource ids of the run are drawn from; `Dir` is the directory the database path resolves against; `Sink` is the sink the writer `Run` builds delivers to; `Banner` is the banner source of the handler `Run` serves; `MCP` makes, from the writer it is given, a server at each call, `Run` calling it once for each MCP server the run serves (`D03-serve`): the one that handler mounts at `/mcp` and the two box servers; `After` is the timer source of the run's operations engine and simulated hosts, nil meaning `time.After`; `Executable` answers the path of the running binary, nil meaning `os.Executable`; and `Cloud` opens the cloud `Run` uses in AWS mode, nil meaning `awssdk.Open` (`D05-cloud`).
- R-CB7V-CDBR: The `internal/cli` package MUST export `func Run(ctx context.Context, p Process) int`.
- R-Y6WW-T7RD: `Run` MUST treat a nil `Unsetenv` as removing no variable, an empty `Dir` as the process's working directory and a nil `Sink` as the sink `telemetry.NewSocketSink` returns; and every time `Run` records MUST be read from `Now`, so that, serving with `IKIGENBA_SANDBOX` present and none of `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` and `IKIGENBA_HOSTS_INSTANCE_PROFILE` set, a host a `create_host` call (`D07-tools`) created while `Now` returned 2026-11-17 14:03:07 UTC is answered by a later `host` call with `created` `2026-11-17T14:03:07Z`, and an account a `create_account` call created on that host while `Now` returned 2026-11-17 14:05:00 UTC is answered by a later `account` call with `created` `2026-11-17T14:05:00Z`.
- R-CEVK-HOJU: When `Run` serves with a `Now` that returns one fixed instant with a zero sub-microsecond part on every call, the `Time` of every event `Run`'s writer hands to `Sink` MUST be that instant.
- R-CG3G-VGAJ: `Run` MUST never call `Rand`'s `Read` from two goroutines at the same time, so that a `Rand` that is not safe for concurrent use, such as a `*bytes.Reader`, may be handed in to a run serving concurrent requests and tool calls.
- R-PE8F-6SDX: The request ids `Run` mints MUST be drawn from `Rand` alone, so that two runs each given a `Rand` that yields the byte `0x5a` for every byte read, one given a `Now` that always returns 2026-10-29 14:00:00 UTC and the other one that always returns 2026-10-30 09:30:00 UTC, hand `Sink` a `request.started` event with the same non-empty `RequestID` for the first request each answers that carries no `X-Request-Id`, and a run given a `Rand` that yields the byte `0x00` for every byte read hands it one with a different `RequestID` for that request.
- R-ESG4-8UGU: When `Run` serves, the database it opens MUST be the SQLite database at `state/accounts.db` resolved against `Dir`, so that, with `IKIGENBA_SANDBOX` present and no hosts variable set, a host a `create_host` call (`D07-tools`) created in one run is answered by a `host` call naming it in a later run given the same `Dir`, with the first run's `created` time, and is answered not found (`D07-tools`) by a `host` call naming it in a later run given another `Dir` that held no entry.
- R-Y9CP-KR8R: When `Run` serves with a `Dir` that holds no entry, with its services file outside `Dir`, it MUST, by the time it has sent `READY=1`, have created under `Dir` the directory `state` holding the regular file `accounts.db`; and when it sends `READY=1`, and after `Run` has returned having answered a `create_host` call (`D07-tools`), `Dir` MUST hold no entry but `state`, and `state` no entry but files whose names begin `accounts.db` and, only when the settings `Run` read select `settings.ModeSandbox`, the regular file `sandbox-cloud.json`.
- R-YAKL-YIZG: Every key `Run` passes to `LookupEnv` MUST be one of `DRAIN_SECONDS`, `IKIGENBA_SANDBOX`, `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE`, `IKIGENBA_HOSTS_INSTANCE_PROFILE`, `IKIGENBA_SERVICES`, `LISTEN_PID`, `LISTEN_FDS` and `NOTIFY_SOCKET`, whatever its `Args` and whatever tools and pages it serves; so that no other variable, `IKIGENBA_COMMIT`, `IKIGENBA_RELEASE`, `ACCOUNTS_HOST_COMMAND`, any other whose name begins `ACCOUNTS_` and any whose name begins `AWS_` other than `AWS_REGION` included, is ever read through it.
- R-YBSI-CAQ5: When `Run` serves with a non-nil `Sink`, every event the writer `Run` builds delivers MUST be handed to that `Sink`'s `Deliver`, so that once `Run` has returned, having started over a `Dir` that held no entry with `IKIGENBA_SANDBOX` present and no hosts variable set, answered a `create_host` call (`D07-tools`), and stopped with no request cut off, the `*telemetry.Capture` holds `service.started` first and `service.stopping` last.
- R-7SSU-QP74: When `Run` serves with a non-nil `Sink`, `IKIGENBA_SANDBOX` present, no hosts variable set, and `IKIGENBA_SERVICES` naming a services file that holds an entry for each of `auth`, `telemetry`, `mcp`, `repos`, `sites`, `scripts`, `events`, `cron`, `home`, `webhooks`, `prompts`, `artifacts`, `connections`, `gdrive`, `gmail`, `scrapes`, `candidates`, `campaigns`, `ledger`, `sessions` and `dummy`, each naming a Unix socket on which a listener records every connection it accepts, then while `Run` answers at least one call of each tool `D07-tools` declares, a signed-in `GET` of `/`, of `/tools`, of `/about` and of `/hosts/host01`, a `GET /accountsd`, and a guest's `GET` and form `POST` of the path of a `link` a `create_invite` call answered, the `POST` creating a user (`D18-enrollment`), and until `Run` has returned, no listener MUST accept a connection; so that accounts calls no sibling.
- R-YE8B-3U7J: The tools and the pages `Run` serves MUST read one store, so that when `Run` serves with `IKIGENBA_SANDBOX` present and no hosts variable set, once a `create_host` call (`D07-tools`) has answered the host `host01` and a `create_account` call naming the account `acme` and that host has answered a success result, the body of the answer to a signed-in `GET /` holds `host01` and `acme`.
- R-YFG7-HLY8: The `internal/settings` package, imported from the path `github.com/ikigenba/ikigenba/accounts/internal/settings` with the package name `settings`, MUST export `type Settings struct { DrainSeconds int64; Sandbox bool; Hosts Hosts }`, with exactly these fields in this order, `DrainSeconds` holding the value of `DRAIN_SECONDS`, `Sandbox` whether `IKIGENBA_SANDBOX` is present and `Hosts` the five hosts values.
- R-YGO3-VDOX: The `internal/settings` package MUST export `type Hosts struct { Region, Domain, ZoneID, LaunchTemplate, InstanceProfile string }`, with exactly these fields in this order, holding the values of `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` and `IKIGENBA_HOSTS_INSTANCE_PROFILE` respectively, with the method `func (h Hosts) Configured() bool`.
- R-YHW0-95FM: The `internal/settings` package MUST export `type Mode int` and the constants `ModeNone`, `ModeAWS` and `ModeSandbox` of type `Mode`, `ModeNone` being the zero value and the three distinct, and the method `func (s Settings) Cloud() Mode`.
- R-YJ3W-MX6B: The `internal/settings` package MUST export `var SandboxHosts = Hosts{Region: "sandbox", Domain: "hosts.test", ZoneID: "ZSANDBOX", LaunchTemplate: "sandbox-hosts", InstanceProfile: "sandbox-hosts"}` and `const SandboxBinding = "accounts.sandbox.test"`.
- R-YKBT-0OX0: The `internal/settings` package MUST export the method `func (s Settings) HostsInUse() Hosts`.
- R-YLJP-EGNP: The `internal/settings` package MUST export the methods `func (s Settings) AccountsURL(servicesPath string) (string, error)` and `func (s Settings) Binding(servicesPath string) (string, error)`, and the variable `ErrNoAccountsURL`, a non-nil `error` whose text is copy.
- R-CTID-2XG6: The `internal/settings` package MUST export `func Defaults() Settings`.
- R-CUQ9-GP6V: The `internal/settings` package MUST export `func Read(lookup func(key string) (string, bool)) (Settings, error)`.
- R-CX62-88O9: The `internal/settings` package MUST export `type Error struct { Name, Value, Want string }`, with exactly these fields in this order, with the method `func (e *Error) Error() string`.
- R-CYDY-M0EY: The `internal/settings` package MUST export the method `func (s Settings) Drain() time.Duration`.
- R-CZ10-4RU5: The `internal/settings` package MUST export the string constant `WantSeconds`, a non-empty copy constant holding no newline: what `DRAIN_SECONDS` must be, said after `not ` in a refusal.
- R-YNZI-6053: `settings.Defaults` MUST return, on every call, a `Settings` whose `DrainSeconds` is 5, whose `Sandbox` is false and whose `Hosts` is the zero `Hosts`, so that its `Cloud()` is `settings.ModeNone`.
- R-D39K-53DQ: `settings.Read` MUST accept the value `lookup` returns for `DRAIN_SECONDS` if and only if `lookup` returns false for it, returns an empty value, or returns one or more ASCII decimal digits the first of which is not `0` and no other characters; so that `0`, `00`, `-1`, `+5`, `2.5`, `5s`, `1e9`, `05`, ` 5`, `5 ` and `abc` are refused, and no value is refused for being large.
- R-D4HG-IV4F: When `settings.Read` accepts the value for `DRAIN_SECONDS` it MUST return a nil error, whatever `lookup` returns for any other key, and a `Settings` whose `DrainSeconds` is that of `settings.Defaults()` when the variable is unset or empty, and otherwise the integer its digits denote, or `math.MaxInt64` when that integer exceeds `math.MaxInt64`; so that `30` reads as 30 and `99999999999999999999` as `math.MaxInt64`.
- R-YP7E-JRVS: Whether `settings.Read` returns a nil error, and the `*settings.Error` it returns when it does not, MUST NOT depend on what `lookup` returns for `IKIGENBA_SANDBOX`, `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` or `IKIGENBA_HOSTS_INSTANCE_PROFILE`; and when it returns a nil error, the `Settings` it returns MUST have as `Sandbox` true exactly when `lookup` returns true for `IKIGENBA_SANDBOX`, whatever the value, the empty string included, and as `Hosts.Region`, `Hosts.Domain`, `Hosts.ZoneID`, `Hosts.LaunchTemplate` and `Hosts.InstanceProfile` the values `lookup` returns for `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` and `IKIGENBA_HOSTS_INSTANCE_PROFILE` respectively, each verbatim, white space included, or the empty string when `lookup` returns false for it.
- R-D6X9-AELT: When `settings.Read` does not accept the value for `DRAIN_SECONDS` it MUST return a non-nil error that `errors.As` finds to be a `*settings.Error` whose `Name` is `DRAIN_SECONDS`, whose `Value` is the value `lookup` returned for it, verbatim, and whose `Want` is `settings.WantSeconds`.
- R-D855-O6CI: `(*settings.Error).Error` MUST return exactly `Name + " is '" + Value + "', not " + Want`, so that the error `settings.Read` returns for `DRAIN_SECONDS` set to `05` reads `"DRAIN_SECONDS is '05', not " + settings.WantSeconds`.
- R-D9D2-1Y37: `Settings.Drain` MUST return `DrainSeconds` seconds as a `time.Duration`, and the largest `time.Duration` when that number of nanoseconds exceeds it.
- R-YQFA-XJMH: `Hosts.Configured` MUST return true exactly when each of `Region`, `Domain`, `ZoneID`, `LaunchTemplate` and `InstanceProfile` is not the empty string, a value of only white space counting as not empty; so that a `Hosts` with any one of the five empty answers false.
- R-YRN7-BBD6: `Settings.Cloud` MUST return `settings.ModeAWS` when `Hosts.Configured()` is true, whatever `Sandbox` is; `settings.ModeSandbox` when each of the five fields of `Hosts` is the empty string and `Sandbox` is true; and `settings.ModeNone` otherwise; so that a `Settings` whose `Sandbox` is true and whose `Hosts` holds only a non-empty `Region` answers `settings.ModeNone`.
- R-YSV3-P33V: `Settings.HostsInUse` MUST return `Hosts` when `Cloud()` is `settings.ModeAWS`, a value equal to `settings.SandboxHosts` when it is `settings.ModeSandbox`, and the zero `Hosts` when it is `settings.ModeNone`.
- R-YU30-2UUK: `Settings.AccountsURL(servicesPath)` MUST read the services file at `servicesPath` afresh on every call with appkit's `services.Read`; when that read succeeds and the list holds an entry named `accounts` whose `URL` parses with `net/url`'s `Parse` as a URL whose `Scheme` is `http` or `https` and whose `Host` is not empty, it MUST return that `URL` with every trailing `/` removed and a nil error; otherwise, when `Cloud()` is `settings.ModeSandbox`, it MUST return `"http://" + settings.SandboxBinding` and a nil error; and otherwise it MUST return the empty string and an error for which `errors.Is(err, settings.ErrNoAccountsURL)` is true; so that, the file's entry `accounts` naming `https://accounts.one.test/`, a call answers `https://accounts.one.test`, and after the file is rewritten to name `https://accounts.two.test:8443`, the next call answers `https://accounts.two.test:8443`, and an empty `servicesPath` answers `settings.ErrNoAccountsURL` outside sandbox mode.
- R-YVAW-GML9: `Settings.Binding(servicesPath)` MUST return, when `AccountsURL(servicesPath)` returns a nil error, the `Host` of that URL as `net/url`'s `Parse` reads it, port included when present, with every ASCII letter lowercased, and a nil error; and otherwise the empty string and the error `AccountsURL` returned; so that an entry `accounts` naming `https://Accounts.Example.TEST:8443/` binds `accounts.example.test:8443`, and with no entry in sandbox mode it answers `settings.SandboxBinding`.
- R-YWIS-UEBY: When `Args` is empty and `settings.Read(p.LookupEnv)` returns a non-nil error `err`, `Run` MUST write exactly `"accounts: " + err.Error() + "\n"` to `Stderr` in one call to `Stderr.Write`, write nothing to `Stdout`, and return `ExitUsage` (`D02-cli`), without calling `LookupEnv` for `LISTEN_PID`, `LISTEN_FDS` or `IKIGENBA_SERVICES`, without calling `Inherit`, `Unsetenv`, `Executable` or `Cloud`, and without creating anything under `Dir`.
- R-YXQP-862N: When `Run` serves and the settings it read select `settings.ModeAWS`, it MUST call `Cloud` exactly once, with the value of `AWS_REGION` verbatim as the region, and every call the run makes to a cloud while it serves MUST be made on the `cloud.Cloud` that call returned; so that, `Cloud` returning a fake that records every call and answers `FindRecords` with no record, a `create_host` call (`D07-tools`) and then a `create_account` call naming `acme` and the host the first answered lead the fake to receive a `FindRecords` call whose zone id is the value of `IKIGENBA_HOSTS_ZONE_ID` and whose name is `acme` followed by `.` and the value of `IKIGENBA_HOSTS_DOMAIN`.
- R-YYYL-LXTC: When `Run` serves and the settings it read select `settings.ModeSandbox`, it MUST NOT call `Cloud`, and the cloud its tools, engine, verifier and simulated hosts reach MUST be one `memory.New` (`D16-sandbox`) returned for a `memory.Config` whose `Path` is `state/sandbox-cloud.json` resolved against `Dir`, whose `Now` is `Now`, whose `Rand` draws from `Rand` and whose `Hosts` is `settings.SandboxHosts`; so that once a `host` call answers a host a `create_host` call (`D07-tools`) created with a non-empty `instance_id` and `Run` has returned, `memory.New` over that path loads a cloud whose `FindInstances` for the tags `{Kind: host}` yields an instance with that id whose tag `Host` is that host's name, and so that a `create_account` call naming `acme` and that host answers a `dns_name` of `acme.` followed by `settings.SandboxHosts.Domain`.
- R-Z06H-ZPK1: When `Run` serves and the settings it read select `settings.ModeNone`, it MUST NOT call `Cloud` and MUST create no file `state/sandbox-cloud.json`, and every host, account and user tool it serves MUST refuse as `D07-tools` states for an unconfigured run, whatever `IKIGENBA_SANDBOX` holds; so that with `AWS_REGION` alone set and `IKIGENBA_SANDBOX` present a `create_host` call answers a `Result` whose `IsError()` is true and whose text holds `tools.NotConfigured`.
- R-Z1EE-DHAQ: When `Run` serves in a mode other than `settings.ModeNone`, `Executable` returning a path `e`, the daemon binary its daemon endpoints serve (`D13-daemon-endpoints`) MUST be the file that `filepath.Join(filepath.Dir(e), "accountsd")` resolved to, symbolic links followed, when `Run` started, and `Run` MUST NOT call `Executable` more than once; so that, with `e` naming `bin/accounts` in a temporary directory and `bin/accountsd` there a symbolic link to a file `a`, after `READY=1` the link is repointed at a file `b` holding other bytes, the body of the answer to `GET /accountsd` is `a`'s bytes; and when `Executable` returns an error, or no file exists at that path, `GET /accountsd` is answered 404 and `Run` still sends `READY=1`.
- R-Z2MA-R91F: Every instance the run's engine launches (`D10-operations`) MUST carry the tag `cloud.TagAccountsURL` (`D05-cloud`) whose value is what `Settings.AccountsURL` of the settings `Run` read returns, at the time of that launch, for the services file `IKIGENBA_SERVICES` names (empty when unset); so that, in sandbox mode, with the services file's entry `accounts` naming `https://accounts.one.test/` the instance of a host a `create_host` call created is tagged `https://accounts.one.test`, and with `IKIGENBA_SERVICES` unset it is tagged `http://accounts.sandbox.test`; and so that, in AWS mode with a services file holding no entry `accounts`, the host a `create_host` call created is answered by a later `host` call in state `failed` with reason `ops.ReasonNoAccountsURL` (`D10-operations`).
- R-Z3U7-50S4: When the `accounts` binary starts with no arguments, `DRAIN_SECONDS` unset, `IKIGENBA_SANDBOX` set, none of `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` and `IKIGENBA_HOSTS_INSTANCE_PROFILE` set, a listening socket passed as file descriptor 3 by the socket-activation protocol and `NOTIFY_SOCKET` naming a Unix datagram socket, from a working directory that holds no entry, it MUST, by the time it has sent `READY=1`, have created there `state/accounts.db`; and a host a `create_host` call (`D07-tools`) over the socket created MUST be answered by a `hosts` call made to a later process of the binary started the same way from the same working directory.
- R-Z523-ISIT: When the `accounts` binary is serving with `IKIGENBA_SANDBOX` set and no hosts variable set, a `CallTool` call for the tool `hosts` (`D07-tools`) made by an appkit `mcp.Client` over its socket to `/mcp` on behalf of a caller with a non-empty `UserID` MUST return a `Result` whose `_meta` member `io.modelcontextprotocol/serverInfo` is exactly `{"name":<n>,"version":<v>}`, `<n>` being `pages.ServiceName` and `<v>` the display string of that run (R-DE8N-L11Z).
- R-ETO0-MM7J: When the `accounts` binary is serving with `IKIGENBA_SANDBOX` set, none of `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` and `IKIGENBA_HOSTS_INSTANCE_PROFILE` set, and `PATH` naming only a directory that holds stub programs named `sudo` and `opsctl` that each record every run, neither stub MUST run while the binary answers `create_host`, `create_account`, `add_user`, `add_key`, `remove_key`, `lock_user`, `unlock_user`, `delete_user`, `remove_account` and `delete_host` calls (`D07-tools`) made over its socket and a signed-in `GET /`, nor until it has exited; so that accounts reaches no host command.
- R-DD0R-79BA: accounts' design defines the **identity** of a run of the `accounts` binary, and every requirement of accounts' design that names the identity of a run MUST denote that value, as the `version.Identity` that appkit's `version.Read`, from the package `github.com/ikigenba/ikigenba/appkit/version`, returns when called in a process in whose environment each of the variables `version.CommitVariable` and `version.ReleaseVariable` name is set to the same value as in the environment that run of the binary was started with, or is unset when it was unset there.
- R-DE8N-L11Z: accounts' design defines the **display string** of a run of the `accounts` binary as the string that the `String` method of the identity of that run (R-DD0R-79BA) returns; every requirement of accounts' design that names the display string MUST denote this.
- R-DGOG-CKJD: The `accounts` binary MUST behave as `cli.Run` does when given the binary's arguments after the program name, the process's environment, its process id, its standard output and standard error, and as `Version` the display string of that run, with an empty `Dir` and a nil `Sink`, and MUST exit with the value `Run` returns; and when serving and sent `SIGTERM` or `SIGINT` it MUST stop as `Run` does when its context is cancelled with a cause whose `Error` method returns `SIGTERM` or `SIGINT` respectively.
- R-PHW4-C3M0: When the `accounts` binary is serving with `IKIGENBA_SERVICES` unset, the body of its answer to a `GET /about` request carrying `Host: accounts.example.test`, `X-Forwarded-Proto: https`, a non-empty `X-User-Id` and `X-User-Email: mg@example.com` MUST be exactly the body the about screen `D06-pages` defines for that request has when its banner is drawn by the `Banner` method of `page.New(pages.ServiceName, id)`, `id` being the identity of that run of the binary (R-DD0R-79BA), called while `services.Variable` names no file in the calling process's environment; so that the `Release` and `Commit` of the identity of the run reach the about screen.
- R-DLK1-VNI5: When the `accounts` binary is serving with `IKIGENBA_SERVICES` naming a services file whose entry `telemetry` names a Unix socket on which a listener answers every `POST /ingest` with 204, the first event the `telemetry` listener receives from the binary MUST be `service.started` with `attrs` exactly `{"version":<v>}`, `<v>` the display string; and when the binary is then sent `SIGTERM` with no request in progress, it MUST exit 0, having written nothing to its standard output or standard error, the last event the `telemetry` listener receives being `service.stopping` with `attrs` exactly `{"reason":"SIGTERM"}`.
- R-DMRY-9F8U: The `internal/pages` package, imported from the path `github.com/ikigenba/ikigenba/accounts/internal/pages` with the package name `pages`, MUST export `const ServiceName = "accounts"`.
