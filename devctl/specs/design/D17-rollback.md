# D17-rollback

`devctl rollback <space>` puts a space back on the release it ran before the
current one. The work is opsctl's: devctl finds the space the way every
command that needs a host does (root file, operand, one cloud session, the
space by its tags with `cloud.LookupInstance`, D03, then the running check)
and runs `sudo systemd-run --quiet --wait --pipe --collect opsctl rollback` on
the host, reached by the space's domain, over ssh. A space that has only its
Elastic IP has no host to roll back and is refused with D03's
`NoInstanceError` before any state check. `opsctl` there is the
`/usr/local/bin/opsctl` link, current's opsctl,
which hands the rollback to previous's own; that hand-off, the refusal when
there is no previous release, and every line the rollback writes are opsctl's.
devctl copies opsctl's stdout to its own as it is written and adds no line of
its own, the way `activate` is relayed (D16). Nothing is built, no commit is
read, and nothing in the account changes.

It lives in `internal/rollback`, beside `restore`, and follows the shared
session rules: usage errors first, then the root file, then the operand (D04
R-N1LD-IX1I, R-ST4K-APZN). A failure on the host is the `*host.CommandError`
unchanged, printed as one line with opsctl's stderr quoted under it.

## REQUIREMENTS

- R-X0CS-DA78: Package `internal/rollback` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`; and a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string otherwise, and `ExitCode() int`, returning 2.

- R-X1KO-R1XX: `cli.Run` MUST dispatch the command `rollback` to `rollback.Run`, passing the arguments that follow `rollback`, the `stdout` writer `cli.Run` was given, and `deps`, and MUST return 0 when `rollback.Run` returns a nil error.

- R-X2SL-4TOM: `rollback` MUST take exactly one `<space>` operand and accept no option other than `--help` and `-h`; with no operand it MUST return a `*UsageError` whose `Message` is `rollback needs <space>`, with more than one `rollback takes only <space>`, and with an argument beginning with `-` that is neither `--help` nor `-h` `unknown option '<option>'`, each with the `Help` `devctl rollback --help`; these refusals MUST call neither `checkout.Open` nor `checkout.ReadRootFile`, pass no `seam.Cmd` to `deps.Exec` or `deps.Stream`, and call `deps.Cloud` not at all; verified at least through `cli.Run`, with `Deps.Dir` set to a directory that is not inside a git checkout, by `devctl rollback` writing exactly the three lines `devctl: rollback needs <space>`, an empty line, and `see 'devctl rollback --help' for usage` to stderr with empty stdout and exit 2, and by `devctl rollback sbx1 sbx2` and `devctl rollback sbx1 --now` writing `devctl: rollback takes only <space>` and `devctl: unknown option '--now'` as their first lines with exit 2.

- R-X40H-ILFB: `devctl rollback --help` and `devctl rollback -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation, calling `deps.Cloud` not at all and passing no `seam.Cmd` to `deps.Exec` or `deps.Stream`:

  ```
  Usage: devctl rollback <space>

  Have opsctl on the space activate the release it ran before the current one
  again: current points at previous, previous is removed, and every app is
  restarted. With no previous release opsctl refuses, so a second rollback is
  refused until the next deploy. What rollback does on the host is opsctl's.
  ```

- R-5I1H-KHKK: After its arguments are accepted, `rollback` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the space by `spaceref.Parse` as R-ST4K-APZN requires, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupInstance` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain` and return its error unchanged, and only after that lookup succeeds MUST return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST write nothing to stdout and pass no `seam.Cmd` whose `Path` is `ssh` to `deps.Exec` or `deps.Stream`; verified at least through `cli.Run`, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by `devctl rollback gone`, with no instance that is not terminated and no Elastic IP carrying `gone.ikigenba.dev`, writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'`, by `devctl rollback sbx2`, whose instance is `stopped`, writing `devctl: 'sbx2.ikigenba.dev' is stopped`, and by `devctl rollback sbx1`, while the only resource carrying `sbx1.ikigenba.dev` is the address `18.118.7.42` associated with nothing, writing `devctl: 'sbx1.ikigenba.dev' has no instance`, each with empty stdout and exit 1, and by `devctl rollback sbx1`, while the addresses `18.118.7.42` and `18.190.33.61` and one running instance carry `sbx1.ikigenba.dev`, writing the single stderr line `devctl: 'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)` with empty stdout and exit 2.

- R-BV3Q-UOGF: `rollback` MUST make exactly one call to `(host.Host).StreamSudo` on a `host.Host` whose `Domain` is the found `cloud.Space`'s `Domain` and whose `Deps` is `deps`, with `stdout`, the step `rollback`, and exactly the arguments `opsctl` and `rollback`, so that opsctl's standard output reaches stdout as it is written and unchanged; it MUST write nothing to stdout of its own and return that call's error unchanged; verified at least through `cli.Run`, for the space `sbx1.ikigenba.dev`, by a fake `ssh` writing arbitrary multi-line standard output and exiting 0 reproducing exactly those bytes on stdout with empty stderr and exit 0, and by one exiting 1 with empty standard output and the standard error `opsctl: no previous release to roll back to` writing nothing to stdout and to stderr exactly `devctl: rollback: ssh ec2-user@sbx1.ikigenba.dev sudo systemd-run --quiet --wait --pipe --collect opsctl rollback: exit status 1`, an empty line, and `> opsctl: no previous release to roll back to`, with exit 1.

- R-5J9D-Y9B9: `rollback` MUST read nothing from the checkout other than the root file, calling neither `(*Checkout).Apps` nor `(*Checkout).App` nor any method that runs `git` beyond what `checkout.ReadRootFile` runs; MUST make no cloud call other than the `STS.CallerAccountID` call `cloud.Connect` makes and the `EC2.ListSpaceInstances` and `EC2.ListSpaceAddresses` calls `cloud.LookupInstance` makes; and MUST pass to `deps.Exec` and `deps.Stream` no `seam.Cmd` other than those of `checkout.ReadRootFile` and the one `StreamSudo` call; verified at least with cloud fakes that fail on any other method and a successful `devctl rollback sbx1` in a temporary checkout that holds no file other than the root file.
