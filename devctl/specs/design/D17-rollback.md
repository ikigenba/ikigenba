# D17-rollback

`devctl rollback <space>` puts a space back on the release it ran before the
current one. The work is opsctl's: devctl finds the space the way every
space-taking command does (root file, operand, one cloud session, the space by
its tags, the running check) and runs `sudo opsctl rollback` on the host over
ssh. `opsctl` there is the `/usr/local/bin/opsctl` link, current's opsctl,
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

- R-X58D-WD60: After its arguments are accepted, `rollback` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the space by `spaceref.Parse` as R-ST4K-APZN requires, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain` and return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST write nothing to stdout and pass no `seam.Cmd` whose `Path` is `ssh` to `deps.Exec` or `deps.Stream`; verified at least through `cli.Run`, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by `devctl rollback gone` writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` and by `devctl rollback sbx2`, whose instance is `stopped`, writing `devctl: 'sbx2.ikigenba.dev' is stopped`, each with empty stdout and exit 1.

- R-X6GA-A4WP: `rollback` MUST make exactly one call to `(host.Host).StreamSudo` on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, with `stdout`, the step `rollback`, and exactly the arguments `opsctl` and `rollback`, so that opsctl's standard output reaches stdout as it is written and unchanged; it MUST write nothing to stdout of its own and return that call's error unchanged; verified at least through `cli.Run`, for a space at `18.118.7.42`, by a fake `ssh` writing arbitrary multi-line standard output and exiting 0 reproducing exactly those bytes on stdout with empty stderr and exit 0, and by one exiting 1 with empty standard output and the standard error `opsctl: no previous release to roll back to` writing nothing to stdout and to stderr exactly `devctl: rollback: ssh ec2-user@18.118.7.42 sudo opsctl rollback: exit status 1`, an empty line, and `> opsctl: no previous release to roll back to`, with exit 1.

- R-X7O6-NWNE: `rollback` MUST read nothing from the checkout other than the root file, calling neither `(*Checkout).Apps` nor `(*Checkout).App` nor any method that runs `git` beyond what `checkout.ReadRootFile` runs; MUST make no cloud call other than the `STS.CallerAccountID` call `cloud.Connect` makes and the `EC2.ListSpaceInstances` call `cloud.LookupSpace` makes; and MUST pass to `deps.Exec` and `deps.Stream` no `seam.Cmd` other than those of `checkout.ReadRootFile` and the one `StreamSudo` call; verified at least with cloud fakes that fail on any other method and a successful `devctl rollback sbx1` in a temporary checkout that holds no file other than the root file.
