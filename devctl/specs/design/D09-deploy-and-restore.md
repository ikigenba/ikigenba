# D09-deploy-and-restore

Deploy puts one file that `build` wrote on one space. It validates the file
locally first (the name is `<app>-v<semver>.tar.xz`, the archive holds
`bin/<app>` and `etc/manifest.toml`), reads the root file to learn the root
and its region, resolves the `<space>` operand through `spaceref.Parse`, opens
one cloud session with `cloud.Connect`, finds the space by its tags, compares
the manifest's `secrets` with the space's secrets object, uploads the file to
`s3://<root>/<label>/deploy/<file>` in the bucket named after the root, and
has `opsctl install` fetch it from there over ssh. The bucket's name has dots,
so the S3 adapter addresses it path-style; that is D03's obligation
(R-QIQ0-MU5V) and is not restated here. Promotion is the same command against
a different space: any file build wrote goes to any space, a prerelease as
readily as a release, and nothing about the target or the session restricts
what is accepted.

The checkout is used for one thing: the root file. Deploy never looks the app
up in the checkout, never reads a manifest from it, and never inspects a git
ref beyond the one `checkout.ReadRootFile` needs to find the checkout root;
the file operand resolves against the working directory, not the checkout
root (D04 R-QG5U-LTSE). Because the file is validated before the root file is
read, a missing or malformed file is refused without any process being run,
which is what the stories' "no AWS call was made" preconditions ask for.

Restore drives `opsctl restore <app> [--at <timestamp>]` on the space over the
same kind of session: root file, operand, `cloud.Connect`, `cloud.LookupSpace`,
running check, one `sudo` over ssh. Nothing moves through devctl and no bucket
object is read or written by it. Both commands report the host's exit the same
way: success is one `space.Step` line, failure is the host error unchanged,
which `cli.Run` prints with opsctl's output quoted under it.

The ordering rules that every space-taking command shares — usage errors
first, then the root file, then the operand, then the first cloud call, all
through the one parser — are D04's (R-N1LD-IX1I, R-QW0J-KUFF) and are only
referenced here. The cli mapping of a `*cloud.NoSpaceError` to
`devctl: no space at '<domain>'`, exit 1, is D03's (R-R10I-DEAA); of the
checkout and root-file errors to exit 2, D04's (R-QEXY-821P); of every error
carrying `ExitCode()`, D05's (R-D4G2-IO81).

## REQUIREMENTS

- R-O0KZ-IOYR: Package `internal/deploy` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-YR44-RQHN: Package `internal/deploy` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.

- R-YSC1-5I8C: Package `internal/deploy` MUST export a `NoFileError` struct whose only field is `Path string`, with the methods `Error() string`, returning `no such file '<Path>'`, and `ExitCode() int`, returning 2.

- R-O1SV-WGPG: Package `internal/deploy` MUST export a `MissingSecretsError` struct whose fields are exactly `App string`, `Space string`, and `Names []string`, with the methods `Error() string`, returning `<App>: secrets missing ` followed by the elements of `Names` joined by `,` with no space, `Detail() string`, returning `run 'devctl secrets push <Space> <App>'`, and `ExitCode() int`, returning 2.

- R-O48O-O06U: When `--help` or `-h` appears anywhere among the arguments `deploy.Run` is given, `deploy.Run` MUST write the `deploy` usage text to `stdout`, return a nil error, call `deps.Cloud` not at all, and pass no `seam.Cmd` to `deps.Exec`, so that it neither finds a checkout nor reads the root file, verified at least by `devctl deploy --help` and `devctl deploy sbx1 crm/dist/crm-v0.1.0.tar.xz --help` each printing that text to stdout with empty stderr and exit 0 with `Deps.Dir` set to a directory that is not inside a git checkout.

- R-O5GL-1RXJ: `deploy` MUST take exactly two operands, `<space>` then `<file>`, in that order, and MUST accept no option other than `--help` and `-h`.

- R-O6OH-FJO8: `deploy` invoked with fewer than two operands MUST write exactly the three lines `devctl: deploy needs <space> and <file>`, an empty line, and `see 'devctl deploy --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2, by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`, verified at least by `devctl deploy sbx1` and `devctl deploy`.

- R-O7WD-TBEX: `deploy` invoked with more than two operands MUST write `devctl: deploy takes only <space> and <file>`, and `deploy` invoked with an argument that begins with `-` and is neither `--help` nor `-h` MUST write `devctl: unknown option '<option>'`, as the first of exactly three lines followed by an empty line and `see 'devctl deploy --help' for usage` on stderr, write nothing to stdout, pass no `seam.Cmd` to `deps.Exec`, and exit 2, each by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`.

- R-O94A-735M: `cli.Run` MUST dispatch the command `deploy` to `deploy.Run`, passing the arguments that follow `deploy`, the `stdout` writer `cli.Run` was given, and `deps`, and MUST return 0 when `deploy.Run` returns a nil error.

- R-08GB-YDTQ: `deploy` MUST resolve the `<file>` operand under `Deps.Dir` when it is a relative path and use it unchanged when it is absolute, MUST return a `*NoFileError` whose `Path` is the operand exactly as given when no regular file exists there, and MUST call `deps.Cloud` not at all whenever the `file` step fails; verified at least by reproducing the single stderr line `devctl: no such file 'crm/dist/crm-v0.2.0.tar.xz'` with empty stdout, exit 2, and no call to a recording fake `Deps.Cloud`.

- R-Z9EM-IAM2: For the `file` step `deploy` MUST pass to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `tar`, whose `Args` are exactly `-t`, `-J`, `-f`, and the `<file>` operand as given, and whose `Dir` is `Deps.Dir`, taking the non-empty lines of its standard output as the archive's member names, and MUST then pass exactly one `seam.Cmd` whose `Path` is `tar`, whose `Args` are exactly `-x`, `-J`, `-O`, `-f`, the `<file>` operand as given, and `checkout.ManifestFile`, and whose `Dir` is `Deps.Dir`, whose standard output it MUST decode with `checkout.DecodeManifest`; and it MUST pass the second of those not at all when `checkout.ManifestFile` is not among those member names.

- R-ZAMI-W2CR: `deploy` MUST return a `*FileError` whose `Path` is the `<file>` operand as given and whose `Reason` is `no etc/manifest.toml in the archive` when `checkout.ManifestFile` is not among the member names, `etc/manifest.toml: ` followed by the message of `checkout.DecodeManifest`'s error when that decode fails, and `no bin/<app> in the archive` — where `<app>` is the decoded `Manifest.App` — when `bin/` followed by that app is not among the member names.

- R-ZEA8-1DKU: When either `tar` process exits non-zero, `deploy` MUST return a `*ProcessError` whose `Label` is that `seam.Cmd`'s `Path` followed by its `Args` joined by single spaces, whose `Status` is that exit status, and whose `Stderr` is that process's standard error; and when `deps.Exec` returns a non-nil error for either, `deploy` MUST return an error that `errors.As` does not match to a `*ProcessError` and whose message contains `tar`.

- R-ZFI4-F5BJ: `deploy` MUST write the `file` step with `space.Step` and with the app, one space, and the tag as its detail, verified at least by reproducing `file: ok (crm v0.1.0)` and `file: ok (gmail v0.1.0)`.

- R-F81U-8G73: Package `internal/deploy` MUST export a `ProcessError` struct whose fields are exactly `Label string`, `Status int`, and `Stderr string`, with the methods `Error() string`, returning `<Label>: exit status <Status>`, `Detail() string`, returning `seam.QuoteOutput(Stderr)`, and `ExitCode() int`, returning 1.

- R-OAC6-KUWB: `devctl deploy --help` and `devctl deploy -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl deploy <space> <file>

  Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
  space's deploy/ prefix in the bucket and have opsctl on the space install it
  from there. The app and tag (v<semver>) are read from the file name.
  ```

- R-FAHM-ZZOH: Deploy MUST perform and report exactly `file`, `secrets`, `upload`, and `install` in that order, stop on the first failure, and leave completed uploads in place on install failure.

- R-FBPJ-DRF6: Deploy MUST validate existence of a regular file before parsing its basename with `appref.ParseFile`; invalid names MUST yield `FileError` with reason `name is not <app>-v<semver>.tar.xz`, empty stdout, no archive process or cloud call, and exit 2.

- R-OBK2-YMN0: The app and version deployed MUST come from the file's basename; the archive MUST contain `bin/<app>` and a manifest whose app matches that app, a mismatch producing a `*FileError` whose `Reason` is `manifest app does not match file name`; and every check of the `file` step MUST complete, and its line be written, before `deploy` calls `checkout.ReadRootFile` or `deps.Cloud`, verified at least by `devctl deploy sbx1 crm/dist/crm-v0.2.0.tar.xz` with no such file and `Deps.Dir` set to a directory that is not inside a git checkout writing the single stderr line `devctl: no such file 'crm/dist/crm-v0.2.0.tar.xz'`, exiting 2, and passing no `seam.Cmd` to `deps.Exec`, and by `devctl deploy sbx1 notes.tar.xz` under the same `Deps.Dir` writing the single stderr line `devctl: 'notes.tar.xz' is not a file build wrote: name is not <app>-v<semver>.tar.xz`, exiting 2, and passing no `seam.Cmd` to `deps.Exec`.

- R-OCRZ-CEDP: After the `file` step's line is written and before the `secrets` step, `deploy` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the space by `spaceref.Parse` as R-QW0J-KUFF requires, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain` and return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST pass no further `seam.Cmd` to `deps.Exec` and call no S3 method; verified at least by reproducing the stdout line `file: ok (crm v0.1.0)` with the single stderr line `devctl: no space at 'gone.ikigenba.dev'` and exit 1 for the operand `gone`, and with the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` and exit 1 for the operand `sbx2` whose instance is `stopped`, each in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`.

- R-9PWP-H3AP: `deploy` MUST obtain nothing from the checkout but the root file: it MUST NOT call `(*Checkout).Apps` or `(*Checkout).App`, MUST NOT read any file under the checkout root other than the root file, and MUST pass to `deps.Exec` no `seam.Cmd` beyond the two `tar` commands of R-Z9EM-IAM2, those `checkout.ReadRootFile` passes, and the one `(host.Host).Sudo` call of the `install` step; verified at least by a successful `devctl deploy sbx1 crm/dist/crm-v0.1.0.tar.xz` in a temporary checkout that holds a root file and no `crm` directory.

- R-OF7S-3XV3: `deploy` MUST inspect no git ref other than the one `checkout.ReadRootFile` needs to find the checkout root, MUST enforce no restriction based on the target space, the file's version, or the session's `AccountID`, so that the same file deploys to any space; and a prerelease or metadata version MUST be preserved byte for byte in the `file` step's line, the object key, and the `s3://` URI, verified at least by `crm/dist/crm-v0.2.0-rc.1.tar.xz` deployed to `sbx1` reproducing `file: ok (crm v0.2.0-rc.1)`, `upload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz)`, and the remote argument `s3://ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz`, and by `crm/dist/crm-v0.1.0.tar.xz` deployed to `sbx1` and then to `staging` from the same working directory reproducing `upload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.1.0.tar.xz)` and then `upload: ok (-> ikigenba.dev/staging/deploy/crm-v0.1.0.tar.xz)`.

- R-0HFK-QF61: `deploy` MUST call `secrets.Names` exactly once, with the `SSM` of the session's `Clients`, the space's `Domain`, and the app, and when every name of the decoded `Manifest.Secrets` is among the names it returned MUST write the `secrets` step with the decimal count of the distinct names of `Manifest.Secrets` followed by ` keys` as its detail, ignoring every name `Names` returned that `Manifest.Secrets` does not hold; verified at least by reproducing `secrets: ok (3 keys)` for a manifest of three names against an object holding those three and a fourth, and `secrets: ok (2 keys)` for a manifest of two.

- R-OHNK-VHCH: When a name of the decoded `Manifest.Secrets` is not among the names `secrets.Names` returned, `deploy` MUST return a `*MissingSecretsError` whose `App` is the app, whose `Space` is the space's `Label`, and whose `Names` are those absent names sorted ascending, and MUST write no `secrets` step line and perform no upload or host operation; verified at least by reproducing the stdout line `file: ok (crm v0.1.0)`, the stderr lines `devctl: crm: secrets missing CRM_ORG,CRM_WEBHOOK_SECRET`, an empty line, and `run 'devctl secrets push sbx1 crm'`, and exit 2, both for the operand `sbx1` and for the operand `sbx1.ikigenba.dev`.

- R-OIVH-9936: Package `internal/deploy` MUST export `ObjectKey(label, filename string) string`, returning `<label>/deploy/<filename>`, verified at least by `ObjectKey("sbx1", "crm-v0.1.0.tar.xz")` being `sbx1/deploy/crm-v0.1.0.tar.xz`.

- R-OK3D-N0TV: After the `secrets` step, `deploy` MUST upload the file's complete bytes through the `PutObject` of the session's `Clients.S3` exactly once, with the root file's `Domain` as the bucket, `ObjectKey` of the space's `Label` and the file's basename as the key, and the file's byte length as the size, and MUST then write the `upload` step with `-> <bucket>/<key>` as its detail; no byte of the file MUST travel over ssh; verified at least by reproducing `upload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.1.0.tar.xz)` with a fake `S3` that records the bucket `ikigenba.dev`, the key `sbx1/deploy/crm-v0.1.0.tar.xz`, and a body equal to the file.

- R-FHT1-AM4N: After upload, deploy MUST invoke `Host.Sudo` with step `install` and arguments `opsctl`, `install`, and `s3://<bucket>/<key>`; success MUST report `install: ok (opsctl installed <app>)`, discard successful remote output, and exit 0; failure MUST return the host error unchanged.

- R-D95E-F95N: The `install` step MUST run on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, and when its process exits non-zero `cli.Run` MUST leave the `file`, `secrets`, and `upload` lines on stdout and write to stderr `devctl: install: ssh ec2-user@<address> sudo opsctl install s3://<bucket>/<key>: exit status <status>`, an empty line, and that process's standard output followed by its standard error, quoted as `(*host.CommandError).Detail` quotes them, and return 1; verified at least through `cli.Run` by reproducing the stdout lines `file: ok (gmail v0.1.0)`, `secrets: ok (2 keys)`, and `upload: ok (-> ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz)` and the stderr first line `devctl: install: ssh ec2-user@18.118.7.42 sudo opsctl install s3://ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz: exit status 1` for a space at `18.118.7.42` and a fake `ssh` process that exits 1 with arbitrary multi-line standard output and arbitrary multi-line standard error, each of whose lines appears once on stderr with one `> ` prefix added, every standard output line before every standard error line.

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

- R-OTUK-P6RF: After its arguments, `--at` value included, are accepted, `restore` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the space by `spaceref.Parse` as R-QW0J-KUFF requires, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain` and return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST write nothing to stdout and pass no `seam.Cmd` whose `Path` is `ssh` to `deps.Exec`; and `restore` MUST call no method of `Clients.S3` in any case; verified at least by `devctl restore gone crm` writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with empty stdout and exit 1 in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`.

- R-9R4L-UV1E: `restore` MUST obtain nothing from the checkout but the root file: it MUST NOT call `(*Checkout).Apps` or `(*Checkout).App`, MUST NOT read any file under the checkout root other than the root file, and MUST pass to `deps.Exec` no `seam.Cmd` beyond those `checkout.ReadRootFile` passes and the one `(host.Host).Sudo` call of the `restore` step, so that an app absent from the checkout is restored the same way; verified at least by a successful `devctl restore sbx1 crm` in a temporary checkout that holds a root file and no `crm` directory.

- R-FV7X-I3AA: Restore MUST invoke `Host.Sudo` at the resolved address with step `restore` and exactly `opsctl restore <app>`, appending `--at <value>` when supplied; success MUST discard remote output and report exactly `restore: ok (opsctl restore <app>[ --at <value>])`, and failure MUST return the host error without a success line. It MUST require no locally known or already-installed app and transfer no backup objects.

- R-OWAD-GQ8T: The `restore` step MUST run on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, and its line MUST be written with `space.Step`; verified at least by `devctl restore sbx1 crm` reproducing the single stdout line `restore: ok (opsctl restore crm)` with a fake `ssh` process that exits 0, and by `devctl restore sbx1 crm --at 2026-09-11T18:00:00Z` reproducing `restore: ok (opsctl restore crm --at 2026-09-11T18:00:00Z)` with a recorded remote argument vector of exactly `sudo`, `opsctl`, `restore`, `crm`, `--at`, and `2026-09-11T18:00:00Z`, each with empty stderr and exit 0.

- R-DE0Z-YC4F: When the `restore` step's process exits non-zero, `cli.Run` MUST write nothing to stdout and write to stderr `devctl: restore: ssh ec2-user@<address> sudo opsctl restore <app>[ --at <value>]: exit status <status>`, an empty line, and that process's standard output followed by its standard error, quoted as `(*host.CommandError).Detail` quotes them, and return 1; verified at least through `cli.Run` by reproducing the stderr first line `devctl: restore: ssh ec2-user@18.118.7.42 sudo opsctl restore crm: exit status 1` for a space at `18.118.7.42` and a fake `ssh` process that exits 1 with arbitrary standard output and arbitrary standard error, each of whose lines appears once on stderr with one `> ` prefix added, every standard output line before every standard error line.

- R-FWFT-VV0Z: Package `internal/deploy` MUST export a `FileError` struct whose fields are exactly `Path string` and `Reason string`, with the methods `Error() string`, returning `'<Path>' is not a file build wrote: <Reason>`, and `ExitCode() int`, returning 2, verified at least by reproducing `'notes.tar.xz' is not a file build wrote: name is not <app>-v<semver>.tar.xz`.
