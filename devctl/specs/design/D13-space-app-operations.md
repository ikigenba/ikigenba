# D13-space-app-operations

`space restart`, `space disable`, `space enable`, and `space logs` act on one
app of one space. All four take `<space> <app>`: the space is the shared
operand grammar's (`spaceref.Parse` against the root file's domain); the app
goes to opsctl as typed, and only logs checks it against the usable-name rule,
because logs alone forms a unit name from it. None of them needs
anything from the checkout beyond the root file: it names the root and the
region, and from those the command connects (STS first, like every cloud
command), finds the space by its tags, and refuses a missing or non-running
one before any ssh.

Restart, disable, and enable hand the work to the installed host tool over
ssh — `sudo opsctl restart <app>`, `sudo opsctl disable <app>`, `sudo opsctl
enable <app>` — and on success relay what opsctl wrote to its stdout, byte
for byte and nothing else, the way `space status` relays `opsctl status`
(D06). devctl adds no step line of its own: opsctl's lines already say what
happened, including outcomes such as restarting a disabled app or disabling
one already disabled, which a fixed line of devctl's would hide. What those
lines say is opsctl's published interface, never restated here. A failure is the
`*host.CommandError` unchanged, which `cli.Run` prints as one diagnostic line
with opsctl's stdout and then its stderr quoted under it (D06). Whether an app
is disabled is the host's fact alone: devctl keeps no record of it, never asks
for it before acting, and leaves every other command's host call as it was, so
a disabled app stays disabled through deploy, restore, init, and restart
because opsctl keeps it so. Which apps may be disabled — opsctl refuses the
authenticator — is opsctl's rule, relayed like any other refusal.

Logs never involves opsctl: it asks systemd whether the unit exists, so a typo
is refused instead of answered with an empty journal, then streams
`journalctl` through the run seam's streaming runner so `--follow` is useful
before the remote command exits, and an interrupt is the exit.

## REQUIREMENTS

- R-JIS6-U060: Package `internal/spaceapps` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`; args MUST begin with `restart`, `disable`, `enable`, or `logs`, and `Run` MUST take no writer other than `stdout`.

- R-H1WE-W28L: Package `internal/spaceapps` MUST export `NoAppError` with exactly `App string` and `Domain string`, implementing `Error() string` as `no app '<App>' on '<Domain>'`.

- R-JK03-7RWP: Restart, disable, enable, and logs MUST each require `<space>` and `<app>`; restart, disable, and enable accept only help, while logs additionally accepts `--follow` and `--since <when>`/`--since=<when>` before or after operands, with repeated follow idempotent and last since winning. Since MUST consume a following nonempty value even when it begins with `-`, including `-1h`, except a recognized logs option which indicates a missing value.

- R-JL7Z-LJNE: Space app operation syntax failures MUST return a `*space.UsageError` whose `Help` is `devctl space --help` and whose `Message` is `space <subcommand> needs <space> and <app>` for missing operands, `space <subcommand> takes only <space> and <app>` for extra operands, `unknown option '<option>'` for an unknown option, or `option '--since' requires a value` for a `--since` with no value; on these failures `Run` MUST call neither `checkout.ReadRootFile` nor `checkout.Open`, MUST pass no `seam.Cmd` to `deps.Exec` or `deps.Stream`, and MUST call `deps.Cloud` not at all; verified at least through `cli.Run` by `devctl space restart sbx1` writing exactly the three lines `devctl: space restart needs <space> and <app>`, an empty line, and `see 'devctl space --help' for usage` to stderr with nothing on stdout and exit 2, by `devctl space disable sbx1`, `devctl space enable sbx1`, and `devctl space logs sbx1` writing `devctl: space disable needs <space> and <app>`, `devctl: space enable needs <space> and <app>`, and `devctl: space logs needs <space> and <app>` as their first lines with exit 2, by `devctl space disable sbx1 crm extra` writing `devctl: space disable takes only <space> and <app>` and `devctl space enable sbx1 crm --now` writing `devctl: unknown option '--now'` as their first lines with exit 2, and by `devctl space logs sbx1 crm --since` writing `devctl: option '--since' requires a value` as its first line with exit 2.

- R-0QP0-DXBO: `devctl space restart --help` and `devctl space restart -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout, without a root file, and before any external operation:

  ```
  Usage: devctl space restart <space> <app>

  Have opsctl restart one app's service. Deploy the existing file to apply pushed
  secrets; a restart uses the environment already installed on the host.
  ```

- R-JRBH-IECV: `devctl space disable --help` and `devctl space disable -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout, without a root file, and before any external operation:

  ```
  Usage: devctl space disable <space> <app>

  Have opsctl take one app offline: stop its socket and service and keep both
  from starting until 'devctl space enable'. Its release, data and units stay
  on the host, and deploy, restore, init and restart leave it disabled.
  ```

- R-JSJD-W63K: `devctl space enable --help` and `devctl space enable -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout, without a root file, and before any external operation:

  ```
  Usage: devctl space enable <space> <app>

  Have opsctl bring a disabled app back: enable and start its socket and
  service. Enabling an app that is already enabled changes nothing.
  ```

- R-0RWW-RP2D: `devctl space logs --help` and `devctl space logs -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout, without a root file, and before any external operation:

  ```
  Usage: devctl space logs <space> <app> [--follow] [--since <when>]

  Print the last 100 journal lines of one app's service on the space's host. With
  --since, print every line from that moment instead; with --follow, keep printing
  until interrupted. The two combine.

  Options:
    --follow         keep printing as the app writes, until interrupted
    --since <when>   start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00
  ```

- R-JMFV-ZBE3: After their syntax checks and before any other action, restart, disable, enable, and logs MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged; MUST then obtain the space from the `<space>` operand under R-QW0J-KUFF; MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `checkout.RootFile` read, and `cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, space.Domain)` with that `Domain` and the parsed `spaceref.Space`'s `Domain`, returning each call's error unchanged so that a `*cloud.NoSpaceError` reaches `cli.Run` as D03 R-R10I-DEAA describes; and when the returned `cloud.Space`'s `State` is not `cloud.StateRunning` MUST return a `*space.NotRunningError` whose `Domain` is that `Space`'s `Domain` and whose `State` is its `State`; every one of these MUST happen before any byte is written to `stdout` and before any `seam.Cmd` other than the one `checkout.Open` passes reaches `deps.Exec` or `deps.Stream`; verified at least through `cli.Run`, in a temporary checkout whose root file names `ikigenba.dev`, by `devctl space restart gone crm`, `devctl space disable gone crm`, `devctl space enable gone crm`, and `devctl space logs gone crm` each writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with exit 1, and by `devctl space restart sbx2 crm`, `devctl space disable sbx2 crm`, `devctl space enable sbx2 crm`, and `devctl space logs sbx2 crm` against a fake `EC2` whose `sbx2.ikigenba.dev` instance is `stopped` each writing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` with exit 1, none of the eight passing an `ssh` `seam.Cmd` to `deps.Exec` or `deps.Stream`.

- R-JNNS-D34S: Restart, disable, enable, and logs MUST read nothing from the checkout other than the root file, calling neither `(*Checkout).Apps` nor `(*Checkout).App`; MUST make no cloud call other than the `STS.CallerAccountID` call `cloud.Connect` makes and the `EC2.ListSpaceInstances` call `cloud.LookupSpace` makes; and MUST mutate no cloud resource, parameter, or local file; verified at least with cloud fakes that fail on any other method, and by all four commands reaching their host command in a temporary checkout that holds no file other than the root file.

- R-SBQG-TBGD: Restart MUST make exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, with the step `restart` and exactly the arguments `opsctl`, `restart`, and `<app>` as typed; when it exits 0 restart MUST write that call's `Output.Stdout` to stdout byte for byte and nothing else, neither parsing, reformatting, nor adding a step line of its own, and MUST write nothing of `Output.Stderr`; on any failure it MUST return that call's error unchanged and write nothing to stdout; verified at least through `cli.Run`, for a space at `18.118.7.42`, by `devctl space restart sbx1 crm` for a fake `ssh` process that exits 0, writes to its standard output exactly the line `a` and a newline, and writes to its standard error exactly the line `b` and a newline, reproducing exactly that one line on stdout with empty stderr and exit 0 and leaving a recorded remote argument vector of exactly `sudo`, `opsctl`, `restart`, and `crm`; by one that exits 0 with standard output `a` and no trailing newline reproducing that standard output unchanged; and by one that exits 1 with arbitrary multi-line standard output and arbitrary multi-line standard error writing nothing to stdout and to stderr exactly `devctl: restart: ssh ec2-user@18.118.7.42 sudo opsctl restart crm: exit status 1`, an empty line, and `(*host.CommandError).Detail` of those streams, with exit 1.

- R-SGM2-CEF5: Disable MUST make exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, with the step `disable` and exactly the arguments `opsctl`, `disable`, and `<app>` as typed; when it exits 0 disable MUST write that call's `Output.Stdout` to stdout byte for byte and nothing else, neither parsing, reformatting, nor adding a step line of its own, and MUST write nothing of `Output.Stderr`; on any failure it MUST return that call's error unchanged and write nothing to stdout; verified at least through `cli.Run`, for a space at `18.118.7.42`, by `devctl space disable sbx1 crm` for a fake `ssh` process that exits 0, writes to its standard output exactly the two lines `a` and `b`, each followed by a newline, and writes to its standard error exactly the line `c` and a newline, reproducing exactly those standard output lines on stdout with empty stderr and exit 0 and leaving a recorded remote argument vector of exactly `sudo`, `opsctl`, `disable`, and `crm`; by one that exits 0 with standard output `a` and no trailing newline reproducing that standard output unchanged; and by one that exits 1 with arbitrary multi-line standard output and arbitrary multi-line standard error writing nothing to stdout and to stderr exactly `devctl: disable: ssh ec2-user@18.118.7.42 sudo opsctl disable crm: exit status 1`, an empty line, and `(*host.CommandError).Detail` of those streams, with exit 1.

- R-SK9R-HPN8: Enable MUST make exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, with the step `enable` and exactly the arguments `opsctl`, `enable`, and `<app>` as typed; when it exits 0 enable MUST write that call's `Output.Stdout` to stdout byte for byte and nothing else, neither parsing, reformatting, nor adding a step line of its own, and MUST write nothing of `Output.Stderr`; on any failure it MUST return that call's error unchanged and write nothing to stdout; verified at least through `cli.Run`, for a space at `18.118.7.42`, by `devctl space enable sbx1 crm` for a fake `ssh` process that exits 0, writes to its standard output exactly the three lines `a`, `b`, and `c`, each followed by a newline, and writes to its standard error exactly the line `d` and a newline, reproducing exactly those standard output lines on stdout with empty stderr and exit 0 and leaving a recorded remote argument vector of exactly `sudo`, `opsctl`, `enable`, and `crm`; by one that exits 0 with standard output `a` and no trailing newline reproducing that standard output unchanged; and by one that exits 1 with arbitrary multi-line standard output and arbitrary multi-line standard error writing nothing to stdout and to stderr exactly `devctl: enable: ssh ec2-user@18.118.7.42 sudo opsctl enable crm: exit status 1`, an empty line, and `(*host.CommandError).Detail` of those streams, with exit 1.

- R-HBNL-Y865: Before running journalctl, logs MUST query the host with `sudo systemctl show --property=LoadState --value ikigenba-<app>.service`; a returned `not-found` load state MUST yield `NoAppError` without journal output, and query execution failures MUST surface as host errors rather than an absent app.

- R-HCVI-BZWU: Logs MUST run `sudo journalctl -u ikigenba-<app>.service -n 100 --no-pager` by default; when since is supplied, it MUST replace `-n 100` with `--since <unchanged value>`, and when follow is supplied it MUST insert `-f` before `--no-pager`. It MUST neither parse the since value nor invoke opsctl for this operation.

- R-HE3E-PRNJ: Logs MUST stream journal stdout byte for byte through `Host.StreamSudo` with step `logs`, without status decoration or buffering until exit; journal failure MUST preserve already-delivered stdout and quote stderr in a host diagnostic with exit 1. An interrupt MUST cancel the stream and return 0 without a diagnostic when cancellation is the only failure.

- R-HFBB-3JE8: Before a logs unit query, logs MUST reject an app for which `appref.ValidName` is false with `space.UsageError` saying `'<app>' is not a usable app name` and Help `devctl space --help`, without SSH; unit names MUST be formed only from the validated app.
