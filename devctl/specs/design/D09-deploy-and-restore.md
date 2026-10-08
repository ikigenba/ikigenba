# D09-deploy-and-restore

Deploy puts one release of the whole suite on one space; the requirements
call this the release form. Its argument after `<space>` is a commit, a full
sha, a shorter sha or a tag, whatever it holds: a tag such as `auth/v0.18.2`
carries a slash, and a path such as `dist/<sha>.tar.xz` is resolved like any
other argument and refused as no commit. It works in a fixed order: resolve,
find the space, build, check secrets, copy, unpack, activate. It resolves the
argument in the local repository with `release.Resolve` (D16), before
anything else is looked up, so a branch, `HEAD` or an unknown tag is refused
without a cloud call. It then reads the root file, parses the space and finds
it running, so a deploy that could not reach a host builds nothing. It builds
the release with `build.Suite` (D08), the same build `devctl build <sha>`
runs, into `dist/<sha>.tar.xz`. It checks the space's secrets object of every
app the release holds against the names its manifest declares, in name
order, before anything reaches the host. Then `release.Put` copies the
tarball to the host over ssh and unpacks it into
`/opt/ikigenba/releases/<sha>/`, and `release.Activate` has that release's
own opsctl activate it, with the tag as typed as the label and none for a sha;
opsctl's stdout is copied as it is written. Nothing is uploaded to the
bucket, and no store of built releases exists. Promotion is the same command
against a different space; nothing about the target restricts what is
accepted.

Restore drives `opsctl restore <app> [--at <timestamp>]` on the space over the
same kind of session: root file, operand, `cloud.Connect`, `cloud.LookupSpace`,
running check, one `sudo` over ssh. Nothing moves through devctl and no bucket
object is read or written by it. The backups it puts back are the space's own,
which only opsctl on the host writes and reads. They are not all that sits
under the space's prefix: opsctl's snapshots sit there too, which `golden
capture` and `seed` read, and the snapshots `seed` copies in (D15). Restore reads none of those; `seed` is what puts a
snapshot back. Both commands report the host's exit the same
way: success is one `space.Step` line, failure is the host error unchanged,
which `cli.Run` prints with opsctl's output quoted under it.

The ordering rules that every space-taking command shares — usage errors
first, then the root file, then the operand, then the first cloud call, all
through the one parser — are D04's (R-N1LD-IX1I, R-ST4K-APZN) and are only
referenced here. The cli mapping of a `*cloud.NoSpaceError` to
`devctl: no space at '<domain>'`, exit 1, is D03's (R-RLKQ-JQXG); of the
checkout and root-file errors to exit 2, D04's (R-EX3T-CTOO); of every error
carrying `ExitCode()`, D05's (R-D4G2-IO81).

## REQUIREMENTS

- R-YR44-RQHN: Package `internal/deploy` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.

- R-O1SV-WGPG: Package `internal/deploy` MUST export a `MissingSecretsError` struct whose fields are exactly `App string`, `Space string`, and `Names []string`, with the methods `Error() string`, returning `<App>: secrets missing ` followed by the elements of `Names` joined by `,` with no space, `Detail() string`, returning `run 'devctl secrets push <Space> <App>'`, and `ExitCode() int`, returning 2.

- R-R9DQ-Q1II: When `--help` or `-h` appears anywhere among the arguments `deploy.Run` is given, `deploy.Run` MUST write the `deploy` usage text to `stdout`, return a nil error, call `deps.Cloud` not at all, and pass no `seam.Cmd` to `deps.Exec`, so that it neither finds a checkout nor reads the root file, verified at least by `devctl deploy --help` and `devctl deploy sbx1 r1 --help` each printing that text to stdout with empty stderr and exit 0 with `Deps.Dir` set to a directory that is not inside a git checkout.

- R-ONR2-SC1Y: Package `internal/restore` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-FMOM-TP3F: Package `internal/restore` MUST export `UsageError` with exactly `Message string` and `Help string`, and methods `Error() string` returning Message, `ExitCode() int` returning 2, and `Detail() string` returning `see '<Help>' for usage` when Help is nonempty and an empty string otherwise.

- R-JW73-1HBN: `devctl restore --help` and `devctl restore -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl restore <space> <app> [--at <timestamp>]

  Have opsctl on the space put <app> back from the space's own backups. The
  app's etc/ and state/ come from the newest tarball, and its database, when it
  declares one, from litestream. <app>'s socket and service are stopped for the
  restore and started again after it, unless <app> is disabled.

  Options:
    --at <timestamp>   restore the app as it was at this RFC 3339 moment

  --at governs both halves: the files come from the newest tarball written at or
  before that moment, and the database is rebuilt to the moment itself.
  ```

- R-OQ6V-JVJC: Restore MUST accept exactly the operands `<space>` and `<app>`, in that order, the option `--at <timestamp>` or `--at=<timestamp>` before or after them, and `--help` and `-h`, and no other option; repeated `--at` MUST use the last value.

- R-ORER-XNA1: Restore with fewer than two operands MUST return a `*UsageError` whose `Message` is `restore needs <space> and <app>` and whose `Help` is `devctl restore --help`; more than two operands MUST give the `Message` `restore takes only <space> and <app>`, an argument beginning with `-` that is none of its options `unknown option '<option>'`, and a missing or empty `--at` value `option '--at' requires a value`, each with the same `Help`; these refusals MUST call `deps.Cloud` not at all and pass no `seam.Cmd` to `deps.Exec`, so that no checkout is found and no root file is read; verified at least by `devctl restore sbx1` writing exactly the three lines `devctl: restore needs <space> and <app>`, an empty line, and `see 'devctl restore --help' for usage` to stderr with empty stdout and exit 2 with `Deps.Dir` set to a directory that is not inside a git checkout.

- R-FRK8-CS27: Restore MUST reject an at value that `time.Parse(time.RFC3339, value)` rejects before external access, with a `UsageError` message `--at takes an RFC 3339 timestamp` and empty help; a valid timestamp MUST be passed to opsctl byte for byte, including its original offset and fractional seconds.

- R-OSMO-BF0Q: `cli.Run` MUST dispatch the command `restore` to `restore.Run`, passing the arguments that follow `restore`, the `stdout` writer `cli.Run` was given, and `deps`, and MUST return 0 when `restore.Run` returns a nil error.

- R-2B3I-HPXS: After its arguments, `--at` value included, are accepted, `restore` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the space by `spaceref.Parse` as R-ST4K-APZN requires, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain` and return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST write nothing to stdout and pass no `seam.Cmd` whose `Path` is `ssh` to `deps.Exec`; and `restore` MUST call no method of `Clients.S3` in any case; verified at least by `devctl restore gone crm` writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with empty stdout and exit 1 in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`.

- R-9R4L-UV1E: `restore` MUST obtain nothing from the checkout but the root file: it MUST NOT call `(*Checkout).Apps` or `(*Checkout).App`, MUST NOT read any file under the checkout root other than the root file, and MUST pass to `deps.Exec` no `seam.Cmd` beyond those `checkout.ReadRootFile` passes and the one `(host.Host).Sudo` call of the `restore` step, so that an app absent from the checkout is restored the same way; verified at least by a successful `devctl restore sbx1 crm` in a temporary checkout that holds a root file and no `crm` directory.

- R-FV7X-I3AA: Restore MUST invoke `Host.Sudo` at the resolved address with step `restore` and exactly `opsctl restore <app>`, appending `--at <value>` when supplied; success MUST discard remote output and report exactly `restore: ok (opsctl restore <app>[ --at <value>])`, and failure MUST return the host error without a success line. It MUST require no locally known or already-installed app and transfer no backup objects.

- R-OWAD-GQ8T: The `restore` step MUST run on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, and its line MUST be written with `space.Step`; verified at least by `devctl restore sbx1 crm` reproducing the single stdout line `restore: ok (opsctl restore crm)` with a fake `ssh` process that exits 0, and by `devctl restore sbx1 crm --at 2026-09-11T18:00:00Z` reproducing `restore: ok (opsctl restore crm --at 2026-09-11T18:00:00Z)` with a recorded remote argument vector of exactly `sudo`, `opsctl`, `restore`, `crm`, `--at`, and `2026-09-11T18:00:00Z`, each with empty stderr and exit 0.

- R-VOSP-G8A5: Package `internal/deploy` MUST export `Run(ctx context.Context, args []string, version string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-VQ0L-U00U: `cli.Run` MUST dispatch the command `deploy` to `deploy.Run`, passing the arguments that follow `deploy`, as `version` the version string that `devctl --version` prints without its newline, the `stdout` writer `cli.Run` was given, and `deps`, and MUST return 0 when `deploy.Run` returns a nil error.

- R-RALN-3T97: `deploy` MUST take exactly two operands, `<space>` and then `<sha|tag>`, and MUST accept no option other than `--help` and `-h`; the second operand MUST be passed to `release.Resolve` as typed whatever it holds, a `/` or a `.tar.xz` suffix included; verified at least through `cli.Run` by `devctl deploy sbx1 auth/v0.18.2` and `devctl deploy sbx1 4b22285` each resolving its operand as typed, and by `devctl deploy sbx1 dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`, with that file present under `Deps.Dir` and a fake `git` holding no such tag, writing the single stderr line `devctl: 'dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz' is not a commit` with empty stdout, exit 2, and a recording fake `Deps.Cloud` left with no call.

- R-RBTJ-HKZW: `deploy` invoked with fewer than two operands MUST write exactly the three lines `devctl: deploy needs <space> and <sha|tag>`, an empty line, and `see 'devctl deploy --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2, by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`, verified at least by `devctl deploy sbx1` and `devctl deploy`.

- R-RD1F-VCQL: `deploy` invoked with more than two operands MUST write `devctl: deploy takes only <space> and <sha|tag>`, and `deploy` invoked with an argument that begins with `-` and is neither `--help` nor `-h` MUST write `devctl: unknown option '<option>'`, as the first of exactly three lines followed by an empty line and `see 'devctl deploy --help' for usage` on stderr, write nothing to stdout, pass no `seam.Cmd` to `deps.Exec`, and exit 2, each by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`.

- R-RE9C-94HA: `devctl deploy --help` and `devctl deploy -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl deploy <space> <sha|tag>

  Build the suite at <sha|tag> as build does, check that the space holds every
  secret the release's manifests declare, copy dist/<sha>.tar.xz to the space's
  host, unpack it into /opt/ikigenba/releases/<sha>/, and have that release's
  opsctl activate it. A tag is the release's label, exactly as typed; a sha
  gives none.
  ```

- R-W8B3-KK59: After its arguments are accepted, the release form of `deploy` MUST call `checkout.Open(ctx, deps)` and return its error unchanged; MUST then call `release.Resolve` with that checkout and the second operand as typed and return its error unchanged; MUST then call `(*Checkout).ReadRootFile` on that checkout and return its error unchanged; MUST then obtain the space by `spaceref.Parse` as R-ST4K-APZN requires, call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read, call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain`, returning each one's error unchanged, and return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; when any of these fails it MUST write nothing to stdout, call `build.Suite` not at all, call no method of the session's `SSM` or `S3`, and pass no further `seam.Cmd` to `deps.Exec` or `deps.Stream`, and when `release.Resolve` fails it MUST call `deps.Cloud` not at all; verified at least through `cli.Run`, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by `devctl deploy sbx1 r9` with a fake `git` holding no tag `r9` and `devctl deploy sbx1 main` with one that resolves `main` as a branch writing the single stderr lines `devctl: 'r9' is not a commit` and `devctl: 'main' is not a commit` with empty stdout, exit 2, and a recording fake `Deps.Cloud` left with no call; and by `devctl deploy gone r1` writing `devctl: no space at 'gone.ikigenba.dev'` and `devctl deploy sbx2 r1`, whose instance is `stopped`, writing `devctl: 'sbx2.ikigenba.dev' is stopped`, each with empty stdout, exit 1, no `git worktree add`, no `go` and no `ssh` or `scp` `seam.Cmd`.

- R-W9IZ-YBVY: Once the space is found running, the release form of `deploy` MUST call `build.Suite(ctx, c, sha, version)` exactly once, with that checkout, the sha `release.Resolve` returned, and the `version` `Run` was given; when it returns an error `deploy` MUST return that error unchanged, write nothing to stdout, call no method of the session's `SSM`, and pass no `ssh` or `scp` `seam.Cmd`; on success it MUST write, through `space.Step`, `build: ok (<label>, <File>)` when the label `release.Resolve` returned is not empty and `build: ok (<File>)` when it is, `<File>` being the returned `Release`'s `File`; verified at least by reproducing `build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)` for `devctl deploy sbx1 r1` and `build: ok (dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)` for `devctl deploy sbx1 4b22285`, and through `cli.Run` by an app `dashboard` whose fake compile exits 1 with the two stderr lines `# github.com/ikigenba/ikigenba/dashboard/cmd/dashboard` and `cmd/dashboard/main.go:41:2: undefined: render` writing nothing to stdout and to stderr exactly `devctl: build dashboard: exit status 1`, an empty line, and those two lines each prefixed `> `, with exit 1.

- R-WAQW-C3MN: After the `build` line, the release form of `deploy` MUST check the space's secrets against the returned `Release`'s `Manifests`, in the order given: for each manifest whose `Secrets` hold at least one name, exactly one call to `secrets.Names` with the `SSM` of the session's `Clients`, the space's `Domain`, and that manifest's `App`, and no call for a manifest with none; at the first manifest one of whose names `Names` did not return, it MUST return a `*MissingSecretsError` whose `App` is that manifest's `App`, whose `Space` is the space's `Label`, and whose `Names` are its absent names sorted ascending, making no later `Names` call, writing no `secrets` line, and passing no `ssh` or `scp` `seam.Cmd`; when every name is present it MUST write, through `space.Step`, `secrets: ok (<n> apps, <k> keys)`, `<n>` being the decimal count of the manifests and `<k>` the sum over them of the counts of their distinct names, ignoring every name an object holds that no manifest declares; verified at least by reproducing `secrets: ok (8 apps, 2 keys)` for eight apps of which only `auth` declares `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` against an object holding those and a third, with exactly one `Names` call; and through `cli.Run` by `/sbx1.ikigenba.dev/auth` holding only `GOOGLE_CLIENT_ID` giving the stdout line `build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)` and the stderr lines `devctl: auth: secrets missing GOOGLE_CLIENT_SECRET`, an empty line, and `run 'devctl secrets push sbx1 auth'`, with exit 2; and by two apps each missing a name reporting the first in name order.

- R-WBYS-PVDC: After the `secrets` line, the release form of `deploy` MUST call `release.Put(ctx, h, stdout, file, sha)` exactly once, where `h` is a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps` and `file` is the checkout's `Path` of the returned `Release`'s `File`, and then, only when `Put` returned nil, `release.Activate(ctx, h, stdout, sha, label)` exactly once with the label `release.Resolve` returned; it MUST return either call's error unchanged and write nothing to stdout of its own after the `secrets` line; verified at least through `cli.Run` by `devctl deploy sbx1 r1` reproducing the lines `build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)`, `secrets: ok (8 apps, 2 keys)`, `copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)`, and `unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)` followed by exactly the arbitrary standard output of the fake `activate` process, with empty stderr and exit 0, and by `devctl deploy sbx1 4b22285` running `activate` with the sha alone.

- R-WD6P-3N41: The release form of `deploy` MUST call no method of the session's `S3`, MUST call no method of the session's `SSM` other than the `GetParameter` calls `secrets.Names` makes, MUST change nothing in the account, and MUST pass to `deps.Exec` and `deps.Stream` no `seam.Cmd` other than those of `checkout.Open`, `release.Resolve`, `build.Suite`, `release.Put` and `release.Activate`, so that no file is uploaded to the bucket and no tag, branch, `HEAD` or working-tree file of the checkout outside `dist/` changes; verified at least by a successful `devctl deploy sbx1 r1` with a fake `S3` that fails on every method.
