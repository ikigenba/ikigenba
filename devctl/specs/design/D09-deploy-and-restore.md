# D09-deploy-and-restore

Deploy uploads a validated artifact to the target space’s deploy prefix and
invokes opsctl install with its S3 URI. Restore invokes opsctl against the same
space’s own backups, optionally passing a timestamp. Both use the one selected
account, and remote failures preserve completed local steps.

## REQUIREMENTS

- R-YOOC-0709: Package `internal/deploy` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.

- R-YR44-RQHN: Package `internal/deploy` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.

- R-YSC1-5I8C: Package `internal/deploy` MUST export a `NoFileError` struct whose only field is `Path string`, with the methods `Error() string`, returning `no such file '<Path>'`, and `ExitCode() int`, returning 2.

- R-YVZQ-ATGF: Package `internal/deploy` MUST export a `MissingSecretsError` struct whose fields are exactly `App string`, `Domain string`, `Profile string`, and `Names []string`, with the methods `Error() string`, returning `<App>: secrets missing ` followed by the elements of `Names` joined by `,` with no space, `Detail() string`, returning `run 'devctl --account <Profile> secrets push <Domain> <App>'`, and `ExitCode() int`, returning 2.

- R-078F-KM31: When `--help` or `-h` appears anywhere among the arguments `deploy.Run` is given, `deploy.Run` MUST write the `deploy` usage text to `stdout`, return a nil error, call `deps.Cloud` not at all, and pass no `seam.Cmd` to `deps.Exec`, verified at least by `devctl deploy --help` and `devctl --account <name> deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.1.0.tar.xz --help` each printing that text to stdout with empty stderr and exit 0.

- R-Z0VB-TWF7: `deploy` MUST take exactly two operands, `<domain>` then `<file>`, in that order, and MUST accept no option other than `--help` and `-h`.

- R-V2NL-65SU: `deploy`, invoked with `--account` and with fewer than two operands, MUST write exactly the three lines `devctl: deploy needs <domain> and <file>`, an empty line, and `see 'devctl deploy --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2, by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`.

- R-Z3B4-LFWL: `deploy` invoked with more than two operands MUST write `devctl: deploy takes only <domain> and <file>`, and `deploy` invoked with an argument that begins with `-` and is neither `--help` nor `-h` MUST write `devctl: unknown option '<option>'`, as the first of exactly three lines followed by an empty line and `see 'devctl deploy --help' for usage` on stderr, write nothing to stdout, and exit 2, each by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`.

- R-Z4J0-Z7NA: `cli.Run` MUST dispatch the command `deploy` to `deploy.Run`, passing the arguments that follow `deploy`, the `stdout` writer `cli.Run` was given, `deps`, and the profile name `--account` carried, and MUST return 0 when `deploy.Run` returns a nil error.

- R-08GB-YDTQ: `deploy` MUST resolve the `<file>` operand under `Deps.Dir` when it is a relative path and use it unchanged when it is absolute, MUST return a `*NoFileError` whose `Path` is the operand exactly as given when no regular file exists there, and MUST call `deps.Cloud` not at all whenever the `file` step fails; verified at least by reproducing the single stderr line `devctl: no such file 'crm/dist/crm-v0.2.0.tar.xz'` with empty stdout, exit 2, and no call to a recording fake `Deps.Cloud`.

- R-Z9EM-IAM2: For the `file` step `deploy` MUST pass to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `tar`, whose `Args` are exactly `-t`, `-J`, `-f`, and the `<file>` operand as given, and whose `Dir` is `Deps.Dir`, taking the non-empty lines of its standard output as the archive's member names, and MUST then pass exactly one `seam.Cmd` whose `Path` is `tar`, whose `Args` are exactly `-x`, `-J`, `-O`, `-f`, the `<file>` operand as given, and `checkout.ManifestFile`, and whose `Dir` is `Deps.Dir`, whose standard output it MUST decode with `checkout.DecodeManifest`; and it MUST pass the second of those not at all when `checkout.ManifestFile` is not among those member names.

- R-ZAMI-W2CR: `deploy` MUST return a `*FileError` whose `Path` is the `<file>` operand as given and whose `Reason` is `no etc/manifest.toml in the archive` when `checkout.ManifestFile` is not among the member names, `etc/manifest.toml: ` followed by the message of `checkout.DecodeManifest`'s error when that decode fails, and `no bin/<app> in the archive` — where `<app>` is the decoded `Manifest.App` — when `bin/` followed by that app is not among the member names.

- R-ZEA8-1DKU: When either `tar` process exits non-zero, `deploy` MUST return a `*ProcessError` whose `Label` is that `seam.Cmd`'s `Path` followed by its `Args` joined by single spaces, whose `Status` is that exit status, and whose `Stderr` is that process's standard error; and when `deps.Exec` returns a non-nil error for either, `deploy` MUST return an error that `errors.As` does not match to a `*ProcessError` and whose message contains `tar`.

- R-ZFI4-F5BJ: `deploy` MUST write the `file` step with `space.Step` and with the app, one space, and the tag as its detail, verified at least by reproducing `file: ok (crm v0.1.0)` and `file: ok (gmail v0.1.0)`.

- R-ZGQ0-SX28: After the `file` step's line is written and before the `secrets` step, `deploy` MUST call `account.Open` with the profile `--account` carried and then `(*Account).Space(ctx, domain)`, MUST return that call's error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the `<domain>` operand and whose `State` is the space's `State` when that state is not `cloud.StateRunning`; and in each case it MUST pass no further `seam.Cmd` to `deps.Exec`; verified at least by reproducing the stdout line `file: ok (crm v0.1.0)` with the single stderr line `devctl: no space at 'gone.sbx.ikigenba.dev'` and exit 1, and by reproducing it with the single stderr line `devctl: 'bar.sbx.ikigenba.dev' is stopped` and exit 1.

- R-ZHXX-6OSX: `deploy` MUST call `secrets.Names` exactly once, with the `<domain>` operand and the app, and when every name of the decoded `Manifest.Secrets` is among the names it returned MUST write the `secrets` step with the decimal count of the distinct names of `Manifest.Secrets` followed by ` keys` as its detail, ignoring every name `Names` returned that `Manifest.Secrets` does not hold; verified at least by reproducing `secrets: ok (3 keys)` for a manifest of three names against an object holding those three and a fourth, and `secrets: ok (2 keys)` for a manifest of two.

- R-F81U-8G73: Package `internal/deploy` MUST export a `ProcessError` struct whose fields are exactly `Label string`, `Status int`, and `Stderr string`, with the methods `Error() string`, returning `<Label>: exit status <Status>`, `Detail() string`, returning `seam.QuoteOutput(Stderr)`, and `ExitCode() int`, returning 1.

- R-F99Q-M7XS: `devctl deploy --help` and `devctl deploy -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> deploy <domain> <file>

  Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
  deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
  it from there. The app and tag (v<semver>) are read from the file name.
  ```

- R-FAHM-ZZOH: Deploy MUST perform and report exactly `file`, `secrets`, `upload`, and `install` in that order, stop on the first failure, and leave completed uploads in place on install failure.

- R-FBPJ-DRF6: Deploy MUST validate existence of a regular file before parsing its basename with `appref.ParseFile`; invalid names MUST yield `FileError` with reason `name is not <app>-v<semver>.tar.xz`, empty stdout, no archive process or cloud call, and exit 2.

- R-FE5C-5AWK: The app and version deployed MUST come from the artifact basename; the archive MUST contain `bin/<app>` and a manifest whose app matches that app. A mismatch MUST produce `FileError` with reason `manifest app does not match file name`; a missing binary MUST produce reason `no bin/<app> in the archive`. All file validation MUST precede any account access.

- R-FFD8-J2N9: Package `internal/deploy` MUST export `ObjectKey(domain, filename string) string`, returning `<domain>/deploy/<filename>` for an artifact basename.

- R-FGL4-WUDY: After secret validation, deploy MUST upload the file’s complete bytes through the selected account’s `Clients.S3.PutObject`, with its byte length, `Properties.BackupBucket`, and `ObjectKey(domain, basename)`; success MUST report `upload: ok (-> <bucket>/<key>)`. No scp or archive bytes over SSH MUST be used.

- R-FHT1-AM4N: After upload, deploy MUST invoke `Host.Sudo` with step `install` and arguments `opsctl`, `install`, and `s3://<bucket>/<key>`; success MUST report `install: ok (opsctl installed <app>)`, discard successful remote output, and exit 0; failure MUST return the host error unchanged.

- R-FJ0X-ODVC: Deploy MUST open no checkout, inspect no git ref, enforce no branch or account release restriction, and execute only archive inspection and the remote install; a prerelease or metadata version MUST be preserved in output, object key and URI.

- R-FK8U-25M1: When a name of the decoded `Manifest.Secrets` is not among the names `secrets.Names` returned, `deploy` MUST return a `*MissingSecretsError` whose `App` is the app, whose `Domain` is the `<domain>` operand, whose `Profile` is the profile `--account` carried, and whose `Names` are those absent names sorted ascending, and MUST write no `secrets` step line and perform no upload or host operation; verified at least by reproducing the stdout line `file: ok (crm v0.1.0)`, the stderr lines `devctl: crm: secrets missing CRM_ORG,CRM_WEBHOOK_SECRET`, an empty line, and `run 'devctl --account <profile> secrets push foo.sbx.ikigenba.dev crm'` — where `<profile>` is the value that invocation's `--account` carried — and exit 2.

- R-FLGQ-FXCQ: Package `internal/restore` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`.

- R-FMOM-TP3F: Package `internal/restore` MUST export `UsageError` with exactly `Message string` and `Help string`, and methods `Error() string` returning Message, `ExitCode() int` returning 2, and `Detail() string` returning `see '<Help>' for usage` when Help is nonempty and an empty string otherwise.

- R-FHIL-ORT8: `devctl restore --help` and `devctl restore -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> restore <domain> <app> [--at <timestamp>]

  Have opsctl on <domain> put <app> back from <domain>'s own backups. The app's
  etc/ and state/ come from the newest tarball, and its database, when it
  declares one, from litestream. <app>'s unit is stopped for the restore and
  started again after it.

  Options:
    --at <timestamp>   restore the app as it was at this RFC 3339 moment

  --at governs both halves: the files come from the newest tarball written at or
  before that moment, and the database is rebuilt to the moment itself.
  ```

- R-FP4F-L8KT: Restore MUST accept exactly domain and app operands, optional `--at <timestamp>` or `--at=<timestamp>` before or after them, and help; repeated `--at` MUST use the last value. `--from` and `--from-account` MUST be unknown options.

- R-FQCB-Z0BI: Restore with missing operands MUST return `UsageError` with message `restore needs <domain> and <app>` and help `devctl restore --help`; extra operands MUST say `restore takes only <domain> and <app>`, unknown options `unknown option '<option>'`, and a missing or empty at value `option '--at' requires a value`, with the same help; these refusals MUST perform no external access.

- R-FRK8-CS27: Restore MUST reject an at value that `time.Parse(time.RFC3339, value)` rejects before external access, with a `UsageError` message `--at takes an RFC 3339 timestamp` and empty help; a valid timestamp MUST be passed to opsctl byte for byte, including its original offset and fractional seconds.

- R-FSS4-QJSW: `cli.Run` MUST dispatch restore to `restore.Run` with its arguments, stdout, deps and selected profile, and return 0 on nil error.

- R-FU01-4BJL: Restore MUST open only the selected account, resolve the target space, and refuse absent or non-running instances with the existing account/space errors, empty stdout and no SSH; it MUST open no checkout and call no S3 method.

- R-FV7X-I3AA: Restore MUST invoke `Host.Sudo` at the resolved address with step `restore` and exactly `opsctl restore <app>`, appending `--at <value>` when supplied; success MUST discard remote output and report exactly `restore: ok (opsctl restore <app>[ --at <value>])`, and failure MUST return the host error without a success line. It MUST require no locally known or already-installed app and transfer no backup objects.

- R-FWFT-VV0Z: Package `internal/deploy` MUST export a `FileError` struct whose fields are exactly `Path string` and `Reason string`, with the methods `Error() string`, returning `'<Path>' is not a file build wrote: <Reason>`, and `ExitCode() int`, returning 2, verified at least by reproducing `'notes.tar.xz' is not a file build wrote: name is not <app>-v<semver>.tar.xz`.
