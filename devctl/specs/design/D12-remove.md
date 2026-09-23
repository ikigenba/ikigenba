# D12-remove

Remove is deploy's inverse: it has `opsctl` on one space take one app off it,
stopping short of the app's data, so a later deploy of the app lands over what
it left. devctl's part is small and deliberately so. It reads the root file to
learn the root and its region, resolves the `<space>` operand through
`spaceref.Parse`, opens one cloud session with `cloud.Connect`, finds the space
by its tags with `cloud.LookupSpace`, refuses a space that is not running, and
then runs exactly one command over ssh: `sudo opsctl uninstall <app>`. What
uninstall does on the host — which unit it stops, which directories it removes,
which it keeps, how nginx and litestream are regenerated — is opsctl's, reached
only through its published grammar; devctl never describes or checks it.

The checkout is used for one thing: the root file. Remove never looks the app
up in the checkout, never reads a manifest, and never inspects a git ref beyond
the one `checkout.ReadRootFile` needs to find the checkout root. The app is
named on the command line and the host is asked, so an app deployed from an
older checkout and since dropped from it can still be taken off. There is no
checkout precondition beyond the root file being present and well formed.

Nothing in the account changes. Remove makes no call to SSM, S3, Route 53, or
IAM, and no EC2 call beyond the lookup: the secrets parameter stays where
`secrets push` put it, every object under the space's prefix stays in the
bucket (the deployed file included), and the apex record is not touched. If the
removed app was the space's apex app, what the root answers is opsctl's
business (S7, D14); remove neither moves nor clears the apex.

The command reports opsctl's exit the way `restore` does: success is one
`space.Step` line, `remove: ok (opsctl uninstalled <app>)`; failure is the
`*host.CommandError` returned unchanged, which `cli.Run` prints as one
diagnostic line with opsctl's standard output and then its standard error
quoted under it (D06). devctl never
asserts what those quoted bytes say — a missing service and an app the host
holds data for but never installed are both simply whatever opsctl wrote.

Ordering is the shared rule and is not restated here: usage errors first, then
the root file, then the operand, then the first cloud call (D04 R-N1LD-IX1I and
R-QW0J-KUFF). The cli mapping of a `*cloud.NoSpaceError` to
`devctl: no space at '<domain>'`, exit 1, is D03's (R-R10I-DEAA); of the
checkout and root-file errors to exit 2, D04's (R-QEXY-821P); of every error
carrying `ExitCode()` and `Detail()`, D05's (R-D4G2-IO81); of everything else,
D05's fallback (R-0D99-FN33), which is how a `*space.NotRunningError` becomes
`devctl: '<domain>' is stopped`, exit 1. The host runner is D06's
`internal/host`: `host.Host{Address, Deps}`, `(host.Host).Sudo`, and
`*host.CommandError` (R-T3CY-DCHC, R-D6VV-A7PF, R-JA8W-5LZ5, R-D9BO-1R6T).

## REQUIREMENTS

- R-8AJV-4VEY: Package `internal/remove` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-GTD4-7O1Q: Package `internal/remove` MUST export `UsageError` with exactly `Message string` and `Help string`, methods `Error() string` returning Message, `ExitCode() int` returning 2, and `Detail() string` returning `see '<Help>' for usage`.

- R-JZUS-6SJQ: `devctl remove --help` and `devctl remove -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0, whenever `--help` or `-h` appears anywhere among the arguments; subject to the superuser refusal, help MUST call `deps.Cloud` not at all and pass no `seam.Cmd` to `deps.Exec`, so that it neither finds a checkout nor reads the root file, verified at least by `devctl remove --help` and `devctl remove sbx1 crm --help` each printing that text with `Deps.Dir` set to a directory that is not inside a git checkout:

  ```
  Usage: devctl remove <space> <app>

  Have opsctl on the space take <app> off it: stop and remove its socket and
  service, remove its binary and configuration, and stop routing its name. Its
  state/ is kept on the host and its secrets are kept in the account, so a later
  deploy of <app> lands over its data. What remove does on the host is opsctl's.
  ```

- R-HRU7-10TZ: `remove` MUST take exactly two operands, `<space>` then `<app>`, in that order, and MUST accept no option other than `--help` and `-h`; fewer than two operands MUST return a `*UsageError` whose `Message` is `remove needs <space> and <app>`, more than two operands one whose `Message` is `remove takes only <space> and <app>`, and an argument that begins with `-` and is neither `--help` nor `-h` one whose `Message` is `unknown option '<option>'`, each with `Help` equal to `devctl remove --help`; these refusals MUST call `deps.Cloud` not at all and pass no `seam.Cmd` to `deps.Exec`, so that no checkout is found, no root file is read, and no ssh connection is opened; verified at least by `devctl remove sbx1` writing exactly the three lines `devctl: remove needs <space> and <app>`, an empty line, and `see 'devctl remove --help' for usage` to stderr with empty stdout and exit 2 with `Deps.Dir` set to a directory that is not inside a git checkout.

- R-HT23-ESKO: `cli.Run` MUST dispatch the command `remove` to `remove.Run`, passing the arguments that follow `remove`, the `stdout` writer `cli.Run` was given, and `deps`, and MUST return 0 when `remove.Run` returns a nil error.

- R-HU9Z-SKBD: After its arguments are accepted, `remove` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the space by `spaceref.Parse` as R-QW0J-KUFF requires, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain` and return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST write nothing to stdout and pass no `seam.Cmd` whose `Path` is `ssh` to `deps.Exec`; verified at least, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by `devctl remove gone crm` writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with a fake `EC2` listing no instance tagged `Space=gone.ikigenba.dev`, and by `devctl remove sbx2 crm` writing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` with a fake `EC2` whose instance tagged `Space=sbx2.ikigenba.dev` is `stopped`, each with empty stdout, exit 1, and no `ssh` `seam.Cmd` passed to `deps.Exec`.

- R-MTKM-W619: `remove` MUST obtain nothing from the checkout but the root file: it MUST NOT call `(*Checkout).Apps` or `(*Checkout).App`, MUST NOT read any file under the checkout root other than the root file, MUST NOT require the `<app>` operand to name a directory or manifest in the checkout, and in a successful run MUST pass to `deps.Exec` no `seam.Cmd` beyond those `checkout.ReadRootFile` passes and the one `(host.Host).Sudo` call of the `remove` step, so that an app absent from the checkout is removed the same way; verified at least by a successful `devctl remove sbx1 crm` in a temporary checkout that holds a root file and no `crm` directory.

- R-HWPS-K3SR: `remove` MUST call no method of the session's `Clients.SSM`, `Clients.S3`, `Clients.Route53`, or `Clients.IAM`, and no method of `Clients.EC2` other than those `cloud.LookupSpace` itself makes, whether the `remove` step succeeds or fails, so that the space's secrets parameters, every object under the space's prefix in the bucket, and the apex record are left as they were; verified with recording fake clients left with no such call after `devctl remove sbx1 crm` with a fake `ssh` process that exits 0 and again after one that exits 1.

- R-GZGM-4IR7: Remove MUST invoke `Host.Sudo` with step `remove` and arguments `opsctl`, `uninstall`, and app; success MUST discard remote output and report exactly `remove: ok (opsctl uninstalled <app>)`, while failure MUST return the host error with no success line. Host-side data retention MUST remain the installed tool’s responsibility.

- R-JXEZ-F92C: The `remove` step MUST run on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, its success line MUST be written with `space.Step`, and when its process exits non-zero `cli.Run` MUST write nothing to stdout and write to stderr `devctl: remove: ssh ec2-user@<address> sudo opsctl uninstall <app>: exit status <status>`, an empty line, and that process's standard output followed by its standard error, quoted as `(*host.CommandError).Detail` quotes them, and return 1; verified at least through `cli.Run` by `devctl remove sbx1 crm` reproducing the single stdout line `remove: ok (opsctl uninstalled crm)` with empty stderr and exit 0 for a fake `ssh` process that exits 0 with arbitrary output and a recorded remote argument vector of exactly `sudo`, `opsctl`, `uninstall`, and `crm`, and by `devctl remove sbx1 gmail` reproducing the stderr first line `devctl: remove: ssh ec2-user@18.118.7.42 sudo opsctl uninstall gmail: exit status 1` for a space at `18.118.7.42` and a fake `ssh` process that exits 1 with arbitrary multi-line standard output and arbitrary multi-line standard error, each of whose lines appears once on stderr with one `> ` prefix added, every standard output line before every standard error line.
