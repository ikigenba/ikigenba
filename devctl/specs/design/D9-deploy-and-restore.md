# D9-deploy-and-restore

Two commands finish devctl, and they are the same shape: check, copy, then run
one `opsctl` command over ssh and report its exit. `deploy` carries one built
file — `<app>/dist/<app>-<tag>.tar.xz`, the file `build` wrote (D8) — to `/tmp/` on
space's host and has `opsctl` install it. `restore` carries one app's backups
from one space's prefix to another's inside the backup bucket and has `opsctl`
on the target host restore from them. `internal/deploy` and `internal/restore`
own them, as D1's layout says.

Almost everything both commands need already exists. `seam.Deps`, `seam.Cmd`
and `cli.Run` are D1's; the grammar, the diagnostic shape and the four exit
codes are D2's; the account, the space lookup, the `*NoSpaceError` diagnostic,
the `SSM` and `S3` surfaces and the AWS failure text are D3's; `etc/manifest.toml`
and its one decoder are D4's; the secrets object and `secrets.Names` are D5's;
`space.Step`, `space.BackupPrefix`, `space.NotRunningError` and the whole of
`internal/host` — the `ssh` and `scp` command lines, the `ec2-user` identity,
the `sudo` prefix, and the rendering of a remote command that failed — are D6's.
What this design adds is two commands' grammar, their usage text, their steps,
and the handful of refusals the stories give them.

**The remote failure is rendered by D6, and it already produces every byte of
both stories' diagnostics.** Both stories end with opsctl failing on the host,
and both diagnostics are a `*host.CommandError` with nothing added:
`CommandError.Error()` is the step, the command with ssh's three options
removed, and the exit status; `Detail()` is the remote stderr; `ExitCode()` is
1. So `deploy`'s last step passes the step name `install` and `restore`'s passes
`restore`, and neither package renders anything. That detail is the remote
**stderr**, which is the same contract gap `space create` hit with `opsctl init`
(`specs/issues/opsctl-init-reports-checks-on-stdout.md`): if `opsctl install`
prints its failure to stdout, the developer sees an exit status and nothing
else. The issue this design files records that for both commands.

**`deploy` reads the name, then the file.** The app and the tag are in the file
name and the manifest is inside the file, so the `file` step is four questions
in one: whether a regular file is there, whether its name has the shape
`<something>-<something>.tar.xz`, whether the archive holds both
`etc/manifest.toml` and a `bin/` entry for the app, and whether the manifest
names an app that the file name begins with, leaving the tag as the rest.

The archive is opened with the system `tar`, not a Go library: D1 approves no xz
module and the house decided that archives are the system `tar` and `xz`, as
`build` already has it (D8's one `tar -c -J -f`). Two processes do it, and
neither writes a file: a `tar -t -J -f` over the archive lists its members, and
a `tar -x -J -O -f` naming `etc/manifest.toml` writes that one member's bytes to
standard output.

**Observed**, on this machine with GNU tar 1.35 and XZ Utils 5.8.1, over an
archive written exactly as D8 writes one (`tar -c -J -f … -C <app>/dist/.build bin
etc`): `tar -t -J -f` lists `bin/`, `bin/crm`, `etc/`, `etc/manifest.toml` — one
member per line, directories with a trailing `/`, so `etc/manifest.toml` and
`bin/crm` are exact line matches; `tar -x -J -O -f <file> etc/manifest.toml`
writes that member's bytes to standard output and exits 0; and the same command
for a member the archive does not hold exits 2 with its reason on standard
error, which is the `*ProcessError` path.

`-O` is why nothing is extracted to disk: the one member devctl wants comes back
on the process's standard output and goes straight into `checkout.DecodeManifest`,
which D4 made the module's only TOML reader precisely so that the manifest has
one reader wherever its bytes come from. D4's prose sketches this as a Go
`tar.NewReader` over an xz stream; that was illustration and it is not reachable
— nothing in the approved module set decompresses xz, and `seam.Cmd` has no
standard input to pipe `xz -dc` into `tar` with — so the contract is the two
`tar` command lines the requirements declare. No requirement of D4 changes: `DecodeManifest` takes an
`io.Reader` and does not care that the bytes arrived in a `seam.Result`.

**Which text is the app and which is the tag.** `build` wrote the name as
`<app>-<tag>.tar.xz` and both halves may hold a `-` — `v0.1.0-rc1` is a tag and
an app directory may be hyphenated — so the split cannot be guessed from the
name alone. The manifest settles it: the app is `Manifest.App`, the name must
begin with it and a `-`, and the tag is what is left. That is one rule for both
halves, it needs no convention about hyphens, and it refuses a renamed file
(`crm-v0.1.0.tar.xz` copied to `notes.tar.xz`) as the story asks. The one check
that happens before the archive is read is the purely syntactic one — a name
with no `-` at all is refused without running `tar`, which is what the story's
`notes.tar.xz` wants and what keeps devctl from reporting a tar failure for a
file that was never a build output.

The file operand is an ordinary path: relative to `Deps.Dir`, where the
developer was standing, exactly as `cat` and tab-completion would have it, and
absolute if they typed it that way. It is not resolved under the checkout root,
and `deploy` opens no checkout and runs no `git`: nothing else in the command
needs one, since the app and the tag are in the name and the manifest is in the
file. The cost is that `build`'s own output line, `crm/dist/crm-v0.1.0.tar.xz`, is
root-relative, so copying it into a `deploy` works from the root and not from
`devctl/` — the same as it would for any other program.

**`deploy`'s four steps, and the two exit codes they meet.** A run that
succeeds writes `file`, `secrets`, `copy`, and `install`, each with a detail of
its own: the app and the tag the archive gave up, the number of secret names the
manifest declares, the target and remote path the file was copied to, and the
app opsctl installed.

The `file` step is entirely local, which is why its two refusals say "No AWS
call was made": the name, the archive and the manifest are decided before
`account.Open`. Then the space is looked up — `devctl: no space at
'gone.sbx.ikigenba.dev'`, D3's rendering, exit 1, after the `file: ok` line has
already gone to stdout. Then the secrets comparison, which is D5's `Names`
against the manifest's `secrets`: a manifest name the object does not hold
refuses the run, naming the app and the absent keys, after the `file` step's
line has already been written.

Only **missing** keys refuse. A key the object holds that the manifest no longer
names is left exactly alone — devctl does not write the object here at all, and
`secrets push` is the only writer (D5) — so the comparison is one-directional
and the count in `secrets: ok (3 keys)` is the number of names the manifest
declares, not the number the object holds. The refusal is exit 2, a failed
preflight, and it carries the next command to run as D2's second block: the
profile in it is the value `--account` carried, so a developer who reads the
line can paste it.

A stopped space is the one failure no story covers, and it has to be answered
because the address is the thing ssh connects to: a stopped space's `Address` is
empty (D6's `space list` prints `-` for it), so copying to it would be `scp` to
`ec2-user@`. `deploy` and `restore` both refuse it with D6's
`*space.NotRunningError` — `devctl: 'bar.sbx.ikigenba.dev' is stopped`, exit 1 —
the same refusal `space status` gives, reported right after the space lookup and
before anything is copied. Neither command calls `(host.Host).Wait`: the host is
a running space the developer has deployed to before, not a machine being born,
and an ssh that cannot connect is already a bounded failure — `ConnectTimeout=10`
and a `*CommandError` carrying ssh's own stderr.

**`restore` is two accounts, one bucket-to-bucket copy, and one remote command.**
Its three steps are `source`, which names the source bucket and prefix and the
newest object's time; `copy`, which counts the objects it moved and names the
target bucket and prefix; and `restore`, the one remote command.

`--from-account` is the story that proves D3's seam: `cloud.Opener` takes a
profile, `account.Open` takes a profile, and nothing in devctl holds a
process-wide set of clients, so two accounts open at once is two `account.Open`
calls and no new machinery. When `--from-account` is absent, or names the same
profile as `--account` byte for byte, there is one account and one `Open`; the
source bucket is then the same bucket and the copy is prefix to prefix inside
it. When they differ there are two, and the source bucket is the *source*
account's `backup_bucket` property — which is why the story's two bucket names
are different and why the `source` step prints the one it read.

The copy is a streaming `GetObject` into a `PutObject`, never a server-side
copy, because no single S3 `CopyObject` can read one account's bucket and write
another's under the developer's identity alone, which is the reason D3's `S3`
interface has no `CopyObject` at all. The same code path serves the same-account
story: one `GetObject` from the source bucket and one `PutObject` into the
target bucket per object, with the object's size from the listing, and the key
rewritten by replacing the source prefix with the target prefix so the set
arrives object for object. The source prefix is provably untouched because the
source account's `S3` is only ever asked for `ListObjects` and `GetObject` —
there is no `PutObject` into the source bucket and no `DeleteObjects` anywhere
in this command — and a fake that records every call proves it in both the
same-account and the cross-account case.

**"The newest backup set" is every object under the source prefix.** The story
is explicit that the set's internal layout is opsctl's business and that devctl
copies it whole — "the newest full backup and every incremental and WAL segment
after it. The exact layout is opsctl's; devctl copies the set whole" — and
devctl has only two facts about each object: its key and its last-modified
time. Neither says which object is a full backup, so devctl cannot pick "the
newest full backup and everything after it" without knowing opsctl's naming,
which this design refuses to invent. What it can state, testably, is that every
object under `<from domain>/<app>/` is copied, and that the `newest` the
`source` step prints is the latest `Modified` among them. That is the newest
backup set exactly when opsctl's retention leaves nothing older under the
prefix; if it leaves several sets there, this copies all of them, which is a
superset the host can still restore from but not what the story's `9 objects`
describes. That gap is opsctl's half of the contract, and the issue this design
files records it beside the two missing commands.

**The usage texts.** `devctl deploy --help` and `devctl restore --help` are
declared byte for byte in the requirements below, from the story.

Neither command has subcommands, so neither promises a further `--help`.

**The grammars, and their refusals.** `deploy <domain> <file>`, two operands and
no options of its own. `restore <domain> <app> --from <from domain>
[--from-account <name>]`, two operands and two options that take a value, each
accepted as `--from <value>` and `--from=<value>` as `--account` is (D2), the
last occurrence winning. The stories fix two of the messages and the rest are in
D5's, D6's and D8's voice: each command refuses too few operands, too many, and
an option it does not know, and `restore` refuses a missing `--from` and an
option left without a value. The requirements declare every one of those lines.

All of them are exit 2, all of them are one `*UsageError` per package carrying
the message and a `Help` of `devctl deploy --help` or `devctl restore --help`,
so the blank line and the `see '...' for usage` line are written once, by
`cli.Run`. The one refusal with no second block is the one the story writes as a
single line, when `--from` names the very space being restored; it is the same
`*UsageError` with an empty `Help`, the form D8 gave it for `build`'s three git
refusals.

**What is deliberately not here.** The step line itself, the host, the waits,
`BackupPrefix` and `NotRunningError` are D6's; the secrets object, `Names` and
`push` are D5's; `etc/manifest.toml` and its decoder are D4's; the account, the
space lookup, the buckets and the AWS failure text are D3's; writing
`<app>/dist/<app>-<tag>.tar.xz` in the first place is D8's. What `opsctl install` and
`opsctl restore` do on the host is opsctl's, and neither is designed there yet:
`specs/issues/opsctl-install-and-restore-commands.md` records the three things
opsctl owes this design — the two command lines, where their failure output
goes, and what a backup set under a prefix looks like.

## REQUIREMENTS

- R-YOOC-0709: Package `internal/deploy` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.
- R-060J-6UCC: Package `internal/deploy` MUST export `TempDir = "/tmp"` and `RemotePath(file string) string`, returning `TempDir`, a `/`, and the last element of `file`'s slash-separated path, verified at least by `RemotePath("crm/dist/crm-v0.1.0.tar.xz")` being `/tmp/crm-v0.1.0.tar.xz`.
- R-YR44-RQHN: Package `internal/deploy` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.
- R-YSC1-5I8C: Package `internal/deploy` MUST export a `NoFileError` struct whose only field is `Path string`, with the methods `Error() string`, returning `no such file '<Path>'`, and `ExitCode() int`, returning 2.
- R-YURT-X1PQ: Package `internal/deploy` MUST export a `FileError` struct whose fields are exactly `Path string` and `Reason string`, with the methods `Error() string`, returning `'<Path>' is not a file build wrote: <Reason>`, and `ExitCode() int`, returning 2, verified at least by reproducing `'notes.tar.xz' is not a file build wrote: name is not <app>-<tag>.tar.xz`.
- R-YVZQ-ATGF: Package `internal/deploy` MUST export a `MissingSecretsError` struct whose fields are exactly `App string`, `Domain string`, `Profile string`, and `Names []string`, with the methods `Error() string`, returning `<App>: secrets missing ` followed by the elements of `Names` joined by `,` with no space, `Detail() string`, returning `run 'devctl --account <Profile> secrets push <Domain> <App>'`, and `ExitCode() int`, returning 2.
- R-YX7M-OL74: Package `internal/deploy` MUST export a `ProcessError` struct whose fields are exactly `Label string`, `Status int`, and `Stderr string`, with the methods `Error() string`, returning `<Label>: exit status <Status>`, `Detail() string`, returning `Stderr` with its trailing newlines removed, and `ExitCode() int`, returning 1.
- R-JPT3-EZK6: `devctl deploy --help` and `devctl deploy -h` MUST print exactly this text to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> deploy <domain> <file>

  Copy <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to /tmp/ on
  the space at <domain> and have opsctl install it. The app and tag are read
  from the file name.
  ```
- R-078F-KM31: When `--help` or `-h` appears anywhere among the arguments `deploy.Run` is given, `deploy.Run` MUST write the `deploy` usage text to `stdout`, return a nil error, call `deps.Cloud` not at all, and pass no `seam.Cmd` to `deps.Exec`, verified at least by `devctl deploy --help` and `devctl --account <name> deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.1.0.tar.xz --help` each printing that text to stdout with empty stderr and exit 0.
- R-Z0VB-TWF7: `deploy` MUST take exactly two operands, `<domain>` then `<file>`, in that order, and MUST accept no option other than `--help` and `-h`.
- R-V2NL-65SU: `deploy`, invoked with `--account` and with fewer than two operands, MUST write exactly the three lines `devctl: deploy needs <domain> and <file>`, an empty line, and `see 'devctl deploy --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2, by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`.
- R-Z3B4-LFWL: `deploy` invoked with more than two operands MUST write `devctl: deploy takes only <domain> and <file>`, and `deploy` invoked with an argument that begins with `-` and is neither `--help` nor `-h` MUST write `devctl: unknown option '<option>'`, as the first of exactly three lines followed by an empty line and `see 'devctl deploy --help' for usage` on stderr, write nothing to stdout, and exit 2, each by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl deploy --help`.
- R-Z4J0-Z7NA: `cli.Run` MUST dispatch the command `deploy` to `deploy.Run`, passing the arguments that follow `deploy`, the `stdout` writer `cli.Run` was given, `deps`, and the profile name `--account` carried, and MUST return 0 when `deploy.Run` returns a nil error.
- R-Z5QX-CZDZ: `deploy` MUST write the steps `file`, `secrets`, `copy`, and `install`, in that order and no others, MUST attempt no later step once one has failed, and MUST exit 0 with empty stderr when every step completed; verified at least by reproducing the four stdout lines `file: ok (crm v0.1.0)`, `secrets: ok (3 keys)`, `copy: ok (-> ec2-user@3.19.79.227:/tmp/crm-v0.1.0.tar.xz)`, and `install: ok (opsctl installed crm)`, and the same four with `3.18.9.77` in place of `3.19.79.227`.
- R-08GB-YDTQ: `deploy` MUST resolve the `<file>` operand under `Deps.Dir` when it is a relative path and use it unchanged when it is absolute, MUST return a `*NoFileError` whose `Path` is the operand exactly as given when no regular file exists there, and MUST call `deps.Cloud` not at all whenever the `file` step fails; verified at least by reproducing the single stderr line `devctl: no such file 'crm/dist/crm-v0.2.0.tar.xz'` with empty stdout, exit 2, and no call to a recording fake `Deps.Cloud`.
- R-Z86Q-4IVD: Before it passes any `seam.Cmd` to `deps.Exec`, `deploy` MUST return a `*FileError` whose `Path` is the `<file>` operand as given and whose `Reason` is `name is not <app>-<tag>.tar.xz` when the last element of that operand's slash-separated path does not end in `.tar.xz` or the rest of that element, with `.tar.xz` removed, does not hold a `-` with at least one character on each side of it; verified at least by reproducing the single stderr line `devctl: 'notes.tar.xz' is not a file build wrote: name is not <app>-<tag>.tar.xz` with empty stdout, exit 2, and no `seam.Cmd` passed to `deps.Exec`.
- R-Z9EM-IAM2: For the `file` step `deploy` MUST pass to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `tar`, whose `Args` are exactly `-t`, `-J`, `-f`, and the `<file>` operand as given, and whose `Dir` is `Deps.Dir`, taking the non-empty lines of its standard output as the archive's member names, and MUST then pass exactly one `seam.Cmd` whose `Path` is `tar`, whose `Args` are exactly `-x`, `-J`, `-O`, `-f`, the `<file>` operand as given, and `checkout.ManifestFile`, and whose `Dir` is `Deps.Dir`, whose standard output it MUST decode with `checkout.DecodeManifest`; and it MUST pass the second of those not at all when `checkout.ManifestFile` is not among those member names.
- R-ZAMI-W2CR: `deploy` MUST return a `*FileError` whose `Path` is the `<file>` operand as given and whose `Reason` is `no etc/manifest.toml in the archive` when `checkout.ManifestFile` is not among the member names, `etc/manifest.toml: ` followed by the message of `checkout.DecodeManifest`'s error when that decode fails, and `no bin/<app> in the archive` — where `<app>` is the decoded `Manifest.App` — when `bin/` followed by that app is not among the member names.
- R-ZBUF-9U3G: The app `deploy` deploys MUST be the decoded `Manifest.App`, and the tag MUST be what remains of the last element of the `<file>` operand's slash-separated path once `.tar.xz` is removed from its end and that app followed by a `-` is removed from its start; and `deploy` MUST return a `*FileError` whose `Path` is that operand as given and whose `Reason` is `name is not <app>-<tag>.tar.xz` when that element does not begin with that app followed by a `-` or nothing remains after the removal.
- R-7ZUE-0Q69: The `file` step MUST decide its refusals in this order, returning the first that applies and no later one: no regular file at the resolved `<file>` path, a name whose `.tar.xz`-stripped last element holds no `-` with a character on each side, no `checkout.ManifestFile` among the member names, a `checkout.ManifestFile` member that `checkout.DecodeManifest` refuses, no `bin/` followed by the decoded `Manifest.App` among the member names, and a name that does not begin with that app followed by a `-`; verified at least with a `<file>` that does not exist and whose name holds no `-`, which MUST be reported as the `*NoFileError`, and with an archive whose manifest names an app that is neither the name's first `-`-separated field nor present under `bin/`, which MUST be reported as the `*FileError` for the missing `bin/<app>`.
- R-ZEA8-1DKU: When either `tar` process exits non-zero, `deploy` MUST return a `*ProcessError` whose `Label` is that `seam.Cmd`'s `Path` followed by its `Args` joined by single spaces, whose `Status` is that exit status, and whose `Stderr` is that process's standard error; and when `deps.Exec` returns a non-nil error for either, `deploy` MUST return an error that `errors.As` does not match to a `*ProcessError` and whose message contains `tar`.
- R-ZFI4-F5BJ: `deploy` MUST write the `file` step with `space.Step` and with the app, one space, and the tag as its detail, verified at least by reproducing `file: ok (crm v0.1.0)` and `file: ok (gmail v0.1.0)`.
- R-ZGQ0-SX28: After the `file` step's line is written and before the `secrets` step, `deploy` MUST call `account.Open` with the profile `--account` carried and then `(*Account).Space(ctx, domain)`, MUST return that call's error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the `<domain>` operand and whose `State` is the space's `State` when that state is not `cloud.StateRunning`; and in each case it MUST pass no further `seam.Cmd` to `deps.Exec`; verified at least by reproducing the stdout line `file: ok (crm v0.1.0)` with the single stderr line `devctl: no space at 'gone.sbx.ikigenba.dev'` and exit 1, and by reproducing it with the single stderr line `devctl: 'bar.sbx.ikigenba.dev' is stopped` and exit 1.
- R-ZHXX-6OSX: `deploy` MUST call `secrets.Names` exactly once, with the `<domain>` operand and the app, and when every name of the decoded `Manifest.Secrets` is among the names it returned MUST write the `secrets` step with the decimal count of the distinct names of `Manifest.Secrets` followed by ` keys` as its detail, ignoring every name `Names` returned that `Manifest.Secrets` does not hold; verified at least by reproducing `secrets: ok (3 keys)` for a manifest of three names against an object holding those three and a fourth, and `secrets: ok (2 keys)` for a manifest of two.
- R-ZJ5T-KGJM: When a name of the decoded `Manifest.Secrets` is not among the names `secrets.Names` returned, `deploy` MUST return a `*MissingSecretsError` whose `App` is the app, whose `Domain` is the `<domain>` operand, whose `Profile` is the profile `--account` carried, and whose `Names` are those absent names sorted ascending, and MUST write no `secrets` step line and pass no `seam.Cmd` whose `Path` is `scp` or `ssh` to `deps.Exec`; verified at least by reproducing the stdout line `file: ok (crm v0.1.0)`, the stderr lines `devctl: crm: secrets missing CRM_ORG,CRM_WEBHOOK_SECRET`, an empty line, and `run 'devctl --account <profile> secrets push foo.sbx.ikigenba.dev crm'` — where `<profile>` is the value that invocation's `--account` carried — and exit 2.
- R-ZKDP-Y8AB: `deploy` MUST call `(host.Host).Copy` exactly once, on a `host.Host` whose `Address` is the space's `Address` and whose `Deps` is `deps`, with the step `copy`, the `<file>` operand as given as the local path, and `RemotePath` of that operand as the remote path, and MUST write the `copy` step with `-> `, that `host.Host`'s `Target()`, a `:`, and that remote path as its detail; verified at least by reproducing `copy: ok (-> ec2-user@3.19.79.227:/tmp/crm-v0.1.0.tar.xz)`.
- R-SB4T-ZKIZ: `deploy` MUST call `(host.Host).Sudo` exactly once, on that same `host.Host`, with the step `install` and exactly the arguments `opsctl`, `install`, and `RemotePath` of the `<file>` operand, MUST return that call's error unchanged, and MUST otherwise write the `install` step with `opsctl installed ` followed by the app as its detail; verified at least by reproducing `install: ok (opsctl installed crm)`, and by a failing remote call reproducing the three stdout lines of the failing-host story with the stderr line `devctl: install: ssh ec2-user@3.19.79.227 sudo opsctl install /tmp/gmail-v0.1.0.tar.xz: exit status 1`, an empty line, and that call's standard error with its trailing newlines removed and otherwise unaltered whatever its content, exit 1, and no further stdout line.
- R-ZMTI-PRRP: `deploy` MUST NOT call `checkout.Open` and every `seam.Cmd` it passes to `deps.Exec` MUST have `tar`, `scp`, or `ssh` as its `Path`.
- R-ZO1F-3JIE: Package `internal/restore` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.
- R-ZP9B-HB93: Package `internal/restore` MUST export `Prefix(domain, app string) string`, returning `space.BackupPrefix(domain)` followed by `app` and a `/`, verified at least by `Prefix("ikigenba.dev", "crm")` being `ikigenba.dev/crm/`.
- R-ZQH7-V2ZS: Package `internal/restore` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.
- R-ZRP4-8UQH: Package `internal/restore` MUST export a `NoBackupsError` struct whose fields are exactly `Bucket string` and `Prefix string`, with the method `Error() string`, returning `no backups under <Bucket>/<Prefix>`, verified at least by reproducing the single stderr line `devctl: no backups under sbx-ikigenba-dev-602773793009/bar.sbx.ikigenba.dev/crm/` with empty stdout and exit 1.
- R-JR0Z-SRAV: `devctl restore --help` and `devctl restore -h` MUST print exactly this text to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> restore <domain> <app> --from <from domain> [--from-account <name>]

  Copy <app>'s newest backup set from <from domain>'s prefix to <domain>'s prefix
  in the backup bucket, then have opsctl on <domain> restore <app> from it.

  Options:
    --from <from domain>     the space whose backups are copied; required
    --from-account <name>    the profile that account is read with; defaults to --account

  A restore across accounts reads the source bucket with --from-account and writes
  the target bucket with --account.
  ```
- R-ZU4X-0E7V: When `--help` or `-h` appears anywhere among the arguments `restore.Run` is given, `restore.Run` MUST write the `restore` usage text to `stdout`, return a nil error, call `deps.Cloud` not at all, and pass no `seam.Cmd` to `deps.Exec`, verified at least by `devctl restore --help` and `devctl --account <name> restore foo.sbx.ikigenba.dev crm --from ikigenba.dev --help` each printing that text to stdout with empty stderr and exit 0.
- R-ZWKP-RXP9: `restore` MUST take exactly two operands, `<domain>` then `<app>`, in that order; MUST accept no option other than `--from <value>`, `--from=<value>`, `--from-account <value>`, `--from-account=<value>`, `--help`, and `-h`; MUST accept each of its two value options whether it precedes or follows the operands; and when one of them appears more than once MUST take the last occurrence's value.
- R-V3VH-JXJJ: `restore`, invoked with `--account` and with fewer than two operands, MUST write `devctl: restore needs <domain> and <app>`; invoked with `--account` and with more than two operands, MUST write `devctl: restore takes only <domain> and <app>`; and invoked with `--account` and with an argument that begins with `-` and is none of its options, `--help`, and `-h`, MUST write `devctl: unknown option '<option>'`, as the first of exactly three lines followed by an empty line and `see 'devctl restore --help' for usage` on stderr, write nothing to stdout, call `deps.Cloud` not at all, and exit 2, each by returning a `*UsageError` whose `Message` is that first line without its `devctl: ` prefix and whose `Help` is `devctl restore --help`.
- R-V53D-XPA8: `restore`, invoked with `--account` and without `--from`, MUST write exactly the three lines `devctl: restore needs --from <from domain>`, an empty line, and `see 'devctl restore --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2.
- R-V6BA-BH0X: With `--account` given, `--from` or `--from-account` as the last argument, followed by an argument that begins with `-`, or spelled `--from=` or `--from-account=` with an empty value, MUST cause exactly the three lines `devctl: option '<option>' requires a value` — where `<option>` is `--from` or `--from-account` — an empty line, and `see 'devctl restore --help' for usage` to be written to stderr, nothing to stdout, and exit 2.
- R-01GB-B0O1: When the value of `--from` equals the `<domain>` operand, `restore` MUST write the single line `devctl: --from names the space being restored` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2, by returning a `*UsageError` whose `Message` is `--from names the space being restored` and whose `Help` is empty.
- R-02O7-OSEQ: `cli.Run` MUST dispatch the command `restore` to `restore.Run`, passing the arguments that follow `restore`, the `stdout` writer `cli.Run` was given, `deps`, and the profile name `--account` carried, and MUST return 0 when `restore.Run` returns a nil error.
- R-03W4-2K5F: `restore` MUST write the steps `source`, `copy`, and `restore`, in that order and no others, MUST attempt no later step once one has failed, and MUST exit 0 with empty stderr when every step completed; verified at least by reproducing the three stdout lines `source: ok (ikigenba-dev-295229566359/ikigenba.dev/crm/, newest 2026-09-12T03:00:04Z)`, `copy: ok (9 objects -> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/crm/)`, and `restore: ok (opsctl restore crm)`, and by reproducing the same three with `copy: ok (9 objects -> ikigenba-dev-295229566359/staging.ikigenba.dev/crm/)` as the second.
- R-0540-GBW4: `restore` MUST call `account.Open` with the profile `--account` carried as the target account, and MUST call it exactly once in total — using that same `*account.Account` as the source account — when `--from-account` is absent or its value equals that profile byte for byte, and exactly twice in total, the second with the value of `--from-account` as the profile and its result as the source account, when that value differs; verified with a fake `cloud.Opener` that records the profile of every call.
- R-06BW-U3MT: `restore` MUST call `(*Account).Space(ctx, domain)` on the target account for the `<domain>` operand and MUST return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is that operand and whose `State` is the space's `State` when that state is not `cloud.StateRunning`, in both cases before it opens the source account, writes any byte to stdout, calls any method of either account's `Clients.S3`, and passes any `seam.Cmd` to `deps.Exec`; verified at least by reproducing the single stderr line `devctl: no space at 'gone.sbx.ikigenba.dev'` with empty stdout, exit 1, and no call to `ListObjects`, `GetObject`, or `PutObject`.
- R-07JT-7VDI: `restore` MUST call the source account's `Clients.S3.ListObjects` exactly once, with that account's `Properties.BackupBucket` and `Prefix(<the value of --from>, <app>)`, and MUST return a `*NoBackupsError` whose `Bucket` is that bucket and whose `Prefix` is that prefix, writing no step line, when it returns no object.
- R-08RP-LN47: `restore` MUST write the `source` step with the source bucket, a `/`, the source prefix, `, newest `, and the latest `Modified` among the objects `ListObjects` returned as its detail, that time converted to UTC and formatted as four-digit year, `-`, two-digit month, `-`, two-digit day, `T`, two-digit hour, `:`, two-digit minute, `:`, two-digit second, and `Z`; verified at least by reproducing `source: ok (ikigenba-dev-295229566359/ikigenba.dev/crm/, newest 2026-09-12T03:00:04Z)` from a listing whose latest `Modified` is that instant.
- R-09ZL-ZEUW: For each object `ListObjects` returned, in the order it returned them, `restore` MUST call the source account's `Clients.S3.GetObject` exactly once with the source bucket and that object's `Key`, and MUST call the target account's `Clients.S3.PutObject` exactly once with the target account's `Properties.BackupBucket`, the key formed by replacing the leading source prefix of that `Key` with `Prefix(<the <domain> operand>, <app>)`, the body `GetObject` returned, and that object's `Size`; verified at least by a listing of `ikigenba.dev/crm/` objects whose recorded `PutObject` keys are the same objects under `foo.sbx.ikigenba.dev/crm/`.
- R-0B7I-D6LL: `restore` MUST write the `copy` step with the decimal count of the objects it copied, ` objects -> `, the target account's `Properties.BackupBucket`, a `/`, and the target prefix as its detail, verified at least by reproducing `copy: ok (9 objects -> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/crm/)` for a listing of nine objects.
- R-0CFE-QYCA: `restore` MUST call no method of the source account's `Clients.S3` other than `ListObjects` and `GetObject`, MUST call `DeleteObjects` on neither account's `Clients.S3`, and MUST call `PutObject` with no bucket other than the target account's `Properties.BackupBucket`; verified with recording fake `S3` clients for both a run whose `--from-account` differs from `--account` and a run with no `--from-account`.
- R-SCCQ-DC9O: `restore` MUST call `(host.Host).Sudo` exactly once, on a `host.Host` whose `Address` is the target space's `Address` and whose `Deps` is `deps`, with the step `restore` and exactly the arguments `opsctl`, `restore`, and the `<app>` operand, MUST return that call's error unchanged, and MUST otherwise write the `restore` step with `opsctl restore ` followed by that operand as its detail; verified at least by reproducing `restore: ok (opsctl restore crm)`, and by a failing remote call reproducing the two stdout lines of the failing-host story with the stderr line `devctl: restore: ssh ec2-user@3.19.79.227 sudo opsctl restore crm: exit status 1`, an empty line, and that call's standard error with its trailing newlines removed and otherwise unaltered whatever its content, exit 1, and no further stdout line.
- R-0G33-W9KD: `restore` MUST NOT call `checkout.Open` and every `seam.Cmd` it passes to `deps.Exec` MUST have `ssh` as its `Path`.

