# D13-space-app-operations

`space restart` and `space logs` act on one app of one space. Both take
`<space> <app>`: the space is the shared operand grammar's (`spaceref.Parse`
against the root file's domain), the app is the usable-name rule build and
deploy already apply. Neither command needs anything from the checkout beyond
the root file: it names the root and the region, and from those the command
connects (STS first, like every cloud command), finds the space by its tags,
and refuses a missing or non-running one before any ssh.

Restart hands the work to the installed host tool over ssh, `sudo opsctl
restart <app>`, and reports opsctl's exit in the step shape the other host
steps use; a failure quotes opsctl's own report. Logs never involves opsctl:
it asks systemd whether the unit exists, so a typo is refused instead of
answered with an empty journal, then streams `journalctl` through the run
seam's streaming runner so `--follow` is useful before the remote command
exits, and an interrupt is the exit.

## REQUIREMENTS

- R-0O97-MDUA: Package `internal/spaceapps` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`; args MUST begin with `restart` or `logs`, and `Run` MUST take no writer other than `stdout`.

- R-H1WE-W28L: Package `internal/spaceapps` MUST export `NoAppError` with exactly `App string` and `Domain string`, implementing `Error() string` as `no app '<App>' on '<Domain>'`.

- R-H34B-9TZA: Restart and logs MUST each require domain and app; restart accepts only help, while logs additionally accepts `--follow` and `--since <when>`/`--since=<when>` before or after operands, with repeated follow idempotent and last since winning. Since MUST consume a following nonempty value even when it begins with `-`, including `-1h`, except a recognized logs option which indicates a missing value.

- R-0PH4-05KZ: Space app operation syntax failures MUST return a `*space.UsageError` whose `Help` is `devctl space --help` and whose `Message` is `space <subcommand> needs <space> and <app>` for missing operands, `space <subcommand> takes only <space> and <app>` for extra operands, `unknown option '<option>'` for an unknown option, or `option '--since' requires a value` for a `--since` with no value; on these failures `Run` MUST call neither `checkout.ReadRootFile` nor `checkout.Open`, MUST pass no `seam.Cmd` to `deps.Exec` or `deps.Stream`, and MUST call `deps.Cloud` not at all; verified at least through `cli.Run` by `devctl space restart sbx1` writing exactly the three lines `devctl: space restart needs <space> and <app>`, an empty line, and `see 'devctl space --help' for usage` to stderr with nothing on stdout and exit 2, by `devctl space logs sbx1` writing `devctl: space logs needs <space> and <app>` as its first line with exit 2, and by `devctl space logs sbx1 crm --since` writing `devctl: option '--since' requires a value` as its first line with exit 2.

- R-0QP0-DXBO: `devctl space restart --help` and `devctl space restart -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout, without a root file, and before any external operation:

  ```
  Usage: devctl space restart <space> <app>

  Have opsctl restart one app's service. Deploy the existing file to apply pushed
  secrets; a restart uses the environment already installed on the host.
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

- R-0T4T-5GT2: After their syntax checks and before any other action, restart and logs MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged; MUST then obtain the space from the `<space>` operand under R-QW0J-KUFF; MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `checkout.RootFile` read, and `cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, space.Domain)` with that `Domain` and the parsed `spaceref.Space`'s `Domain`, returning each call's error unchanged so that a `*cloud.NoSpaceError` reaches `cli.Run` as D03 R-R10I-DEAA describes; and when the returned `cloud.Space`'s `State` is not `cloud.StateRunning` MUST return a `*space.NotRunningError` whose `Domain` is that `Space`'s `Domain` and whose `State` is its `State`; every one of these MUST happen before any byte is written to `stdout` and before any `seam.Cmd` other than the one `checkout.Open` passes reaches `deps.Exec` or `deps.Stream`; verified at least through `cli.Run`, in a temporary checkout whose root file names `ikigenba.dev`, by `devctl space restart gone crm` and `devctl space logs gone crm` each writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with exit 1, and by `devctl space restart sbx2 crm` and `devctl space logs sbx2 crm` against a fake `EC2` whose `sbx2.ikigenba.dev` instance is `stopped` each writing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` with exit 1, none of the four passing an `ssh` `seam.Cmd` to `deps.Exec` or `deps.Stream`.

- R-0UCP-J8JR: Restart and logs MUST read nothing from the checkout other than the root file, calling neither `(*Checkout).Apps` nor `(*Checkout).App`; MUST make no cloud call other than the `STS.CallerAccountID` call `cloud.Connect` makes and the `EC2.ListSpaceInstances` call `cloud.LookupSpace` makes; and MUST mutate no cloud resource, parameter, or local file; verified at least with cloud fakes that fail on any other method, and by both commands reaching their host command in a temporary checkout that holds no file other than the root file.

- R-HAFP-KGFG: Restart MUST run `sudo opsctl restart <app>` with step `restart`, discard successful output and report `restart: ok (opsctl restarted <app>)` only for exit 0; any failure MUST return the host error without a success line.

- R-HBNL-Y865: Before running journalctl, logs MUST query the host with `sudo systemctl show --property=LoadState --value ikigenba-<app>.service`; a returned `not-found` load state MUST yield `NoAppError` without journal output, and query execution failures MUST surface as host errors rather than an absent app.

- R-HCVI-BZWU: Logs MUST run `sudo journalctl -u ikigenba-<app>.service -n 100 --no-pager` by default; when since is supplied, it MUST replace `-n 100` with `--since <unchanged value>`, and when follow is supplied it MUST insert `-f` before `--no-pager`. It MUST neither parse the since value nor invoke opsctl for this operation.

- R-HE3E-PRNJ: Logs MUST stream journal stdout byte for byte through `Host.StreamSudo` with step `logs`, without status decoration or buffering until exit; journal failure MUST preserve already-delivered stdout and quote stderr in a host diagnostic with exit 1. An interrupt MUST cancel the stream and return 0 without a diagnostic when cancellation is the only failure.

- R-HFBB-3JE8: Before a logs unit query, logs MUST reject an app for which `appref.ValidName` is false with `space.UsageError` saying `'<app>' is not a usable app name` and Help `devctl space --help`, without SSH; unit names MUST be formed only from the validated app.
