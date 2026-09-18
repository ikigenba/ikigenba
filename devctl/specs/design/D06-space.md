# D06-space

Spaces are discovered from cloud tags and have an Elastic IP throughout their
lifetime. Stop and start preserve records and the address. Destroy retires a
host first when backups are retained, then removes resources resumably. Host
execution handles quoting and diagnostics; status relays opaque host output.

## REQUIREMENTS

- R-SPY2-5VBP: Package `internal/space` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.

- R-SR5Y-JN2E: Package `internal/space` MUST export `Step(w io.Writer, name, detail string)`, which MUST write `<name>: ok (<detail>)` followed by one newline to `w` when `detail` is not empty and `<name>: ok` followed by one newline when `detail` is empty, verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 terminated)` and `init: ok`.

- R-SSDU-XET3: Package `internal/space` MUST export `RoleName(domain string) string`, returning `ikigenba-space-` followed by `domain`; `PolicyName = "space"`; `BackupPrefix(domain string) string`, returning `domain` followed by `/`; `RecordNames(domain string) []string`, returning exactly `domain` and `*.` followed by `domain`, in that order; and `RecordTTL = 60`; verified at least by `RoleName("foo.sbx.ikigenba.dev")` being `ikigenba-space-foo.sbx.ikigenba.dev` and `BackupPrefix("foo.sbx.ikigenba.dev")` being `foo.sbx.ikigenba.dev/`.

- R-STLR-B6JS: Package `internal/space` MUST export `WaitState(ctx context.Context, deps seam.Deps, acct *account.Account, id string, state cloud.InstanceState) (cloud.Instance, error)`, `WaitChecks(ctx context.Context, deps seam.Deps, acct *account.Account, id string) error`, `PollInterval = 5 * time.Second`, and `PollAttempts = 60`.

- R-SUTN-OYAH: Package `internal/space` MUST export `ElasticIP(ctx context.Context, acct *account.Account, domain string) (cloud.Address, bool, error)` and `ReleaseElasticIP(ctx context.Context, acct *account.Account, addr cloud.Address) error`.

- R-SW1K-2Q16: Package `internal/space` MUST export `PutRecords(ctx context.Context, deps seam.Deps, acct *account.Account, zone cloud.Zone, domain, address string) error` and `DeleteRecords(ctx context.Context, acct *account.Account, zone cloud.Zone, domain string) (int, error)`.

- R-SX9G-GHRV: Package `internal/space` MUST export `DeleteRole(ctx context.Context, acct *account.Account, domain string) (bool, error)`.

- R-SZP9-8199: Package `internal/space` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage`, and `ExitCode() int`, returning 2.

- R-T0X5-LSZY: Package `internal/space` MUST export a `NotRunningError` struct whose fields are exactly `Domain string` and `State cloud.InstanceState`, with the method `Error() string`, returning `'<Domain>' is <State>`, verified at least by reproducing `'bar.sbx.ikigenba.dev' is stopped`.

- R-T251-ZKQN: Package `internal/space` MUST export a `WaitError` struct whose fields are exactly `Subject string` and `Want string`, with the method `Error() string`, returning `timed out waiting for <Subject> to <Want>`, verified at least by reproducing `timed out waiting for i-0c9e94542d98846a8 to be running`.

- R-T3CY-DCHC: Package `internal/host` MUST export `User = "ec2-user"`, a `Host` struct whose fields are exactly `Address string` and `Deps seam.Deps`, and the method `Target() string` on `Host`, returning `User`, `@`, and `Address` joined, verified at least by the `Target()` of a `Host` whose `Address` is `3.19.79.227` being `ec2-user@3.19.79.227`.

- R-T5SR-4VYQ: Package `internal/host` MUST export `ProbeInterval = 5 * time.Second` and `ProbeAttempts = 12`.

- R-T88J-WFG4: Package `internal/host` MUST export an `UnreachableError` struct whose only field is `Address string`, with the method `Error() string`, returning `ssh ` followed by `ec2-user@<Address>` and `: connection timed out`, verified at least by reproducing `ssh ec2-user@3.145.72.19: connection timed out`.

- R-TD45-FIEW: `(host.Host).Wait` MUST pass to `Deps.Exec` the `seam.Cmd` that `Run` passes for the single argument `true`, MUST return a nil error as soon as one of those processes exits 0, MUST wait on `Deps.After(ProbeInterval)` between consecutive ones, MUST pass at most `ProbeAttempts` of them, and MUST return an `*UnreachableError` whose `Address` is `Address` when that many have not exited 0; verified with a fake `Deps.After` that fires immediately.

- R-TEC1-TA5L: `space.WaitState` MUST call `acct.Clients.EC2.DescribeInstance` with `id` and return that `cloud.Instance` and a nil error as soon as its `State` equals `state` and — when `state` is `cloud.StateRunning` — its `Address` is not empty, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `be ` followed by `state` when that many calls have not satisfied it; verified with a fake `Deps.After` that fires immediately.

- R-TFJY-71WA: `space.WaitChecks` MUST call `acct.Clients.EC2.InstanceChecksPassed` with `id` and return a nil error as soon as it reports true, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `pass its status checks` when that many calls have not reported true.

- R-THZQ-YLDO: `space.ElasticIP` MUST call `acct.Clients.EC2.ListSpaceAddresses` and return, with a true second result, the one `cloud.Address` whose `Space` equals `domain`; MUST return a false second result and a nil error when none does; and MUST return a non-nil error naming `domain` when two or more do.

- R-TJ7N-CD4D: `space.ReleaseElasticIP` MUST call `acct.Clients.EC2.DisassociateAddress` with `addr.AssociationID` exactly once when that field is not empty and not at all when it is, and MUST then call `acct.Clients.EC2.ReleaseAddress` with `addr.AllocationID` exactly once.

- R-TKFJ-Q4V2: `space.PutRecords` MUST call `acct.Clients.Route53.ChangeRecords` for `zone.ID` exactly once, with one `cloud.RecordChange` per element of `RecordNames(domain)` in that order, each whose `Action` is `cloud.ChangeUpsert` and whose `Record` has that element as `Name`, `A` as `Type`, `RecordTTL` as `TTL`, and exactly `address` as `Values`; MUST then call `acct.Clients.Route53.ChangeStatus` with the change id that call returned until it reports `cloud.ChangeInsync`, waiting on `deps.After(PollInterval)` between consecutive calls and making at most `PollAttempts` of them; and MUST return a `*WaitError` whose `Subject` is that change id and whose `Want` is `reach INSYNC` when that many calls have not reported it.

- R-TLNG-3WLR: `space.DeleteRecords` MUST call `acct.Clients.Route53.ListRecords` for `zone.ID` and MUST pass to `acct.Clients.Route53.ChangeRecords` for `zone.ID` one `cloud.RecordChange` whose `Action` is `cloud.ChangeDelete` for each returned `cloud.Record` whose `Type` is `A` and whose `Name` is `domain`, `*.` followed by `domain`, or `\052.` followed by `domain`, each carrying that `cloud.Record` exactly as `ListRecords` returned it, MUST return the number of those changes, and MUST return 0 and call `ChangeRecords` not at all when there are none; verified at least with a `ListRecords` that returns the wildcard record as `\052.foo.sbx.ikigenba.dev` and a `ChangeRecords` that records the `cloud.Record` it was given.

- R-TMVC-HOCG: `space.DeleteRole` MUST call `acct.Clients.IAM.RoleExists` and `acct.Clients.IAM.InstanceProfileRoles` for `RoleName(domain)`, MUST return false and a nil error and call no other IAM operation when neither reports existence, and otherwise MUST call `DeleteRolePolicy` for that role and `PolicyName`, `RemoveRoleFromInstanceProfile` for that instance profile and each role it holds, `DeleteInstanceProfile`, and `DeleteRole`, in that order, and return true.

- R-TO38-VG35: When a command's `Run` returns a non-nil error that `errors.As` matches to a `*account.NoZoneError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line of stderr, write nothing further to stdout, and return 1.

- R-TPB5-97TU: Every line that `internal/space`, `internal/spacecreate`, `internal/deploy`, and `internal/restore` write to stdout to report a completed step MUST be written by `space.Step`, verified by a test over those packages' source that no non-test file of a package other than `internal/space` contains the string `: ok (`; and a command MUST write no step line for a step that did not complete.

- R-TG1T-SODG: `devctl --account <name> space list --help` and `devctl --account <name> space list -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> space list

  Print one line per space in the account: the domain, the instance state, and the
  public address, or - when it has none. The lines are sorted by domain, and an
  account with no spaces prints nothing.

  The cloud's tags are the only registry: a space is an instance tagged
  Project=ikigenba with a Space tag naming its domain. What a space is running is
  'devctl space status'.
  ```

- R-TVEN-62JB: `devctl --account <name> space` MUST write exactly the three lines `devctl: space needs <subcommand>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-TWMJ-JUA0: `space` invoked with a first argument that is none of its subcommands, `--help`, and `-h` and does not begin with `-` MUST write exactly the three lines `devctl: unknown subcommand '<name>'`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-TXUF-XM0P: `space destroy`, `space stop`, `space start`, and `space status` invoked with no operand MUST each write exactly the three lines `devctl: space <subcommand> needs <domain>` — where `<subcommand>` is the invoked subcommand's own name — an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2, verified at least by reproducing `devctl: space destroy needs <domain>`.

- R-U0A8-P5I3: `space destroy`, `space stop`, `space start`, and `space status` invoked with more than one operand MUST each write `devctl: space <subcommand> takes only <domain>`, and `space list` invoked with any operand MUST write `devctl: space list takes no arguments`, as the first of exactly three lines followed by an empty line and `see 'devctl space --help' for usage` on stderr, write nothing to stdout, and exit 2.

- R-U3XX-UGQ6: `space list` MUST write one line to stdout for each `account.Space` that `(*Account).Spaces` returns, in the order it returns them, consisting of that `Space`'s `Domain`, one space, its `State`, one space, and either its `Address` or `-` when `Address` is empty; MUST write nothing to stdout when `Spaces` returns none; and MUST exit 0; verified at least by reproducing the three lines `bar.sbx.ikigenba.dev stopped -`, `foo.sbx.ikigenba.dev running 3.19.79.227`, and `new.sbx.ikigenba.dev running 18.220.10.5`, and by reproducing empty output.

- R-U55U-88GV: `space status`, `space stop`, and `space start` MUST each call `(*Account).Space(ctx, domain)` and return its error unchanged, before any call to `deps.Exec`, any write to stdout, and any call to a method of `Account.Clients` other than those `Space` itself makes; and `space destroy` MUST treat a `*account.NoSpaceError` from that call as the space having no instance and MUST NOT return it.

- R-U6DQ-M07K: `space status` MUST return a `*NotRunningError` whose `Domain` is the `<domain>` operand and whose `State` is the space's `State`, write nothing to stdout, and pass no `seam.Cmd` to `deps.Exec`, when that state is not `cloud.StateRunning`, verified at least by reproducing the single stderr line `devctl: 'bar.sbx.ikigenba.dev' is stopped` with exit 1.

- R-S9WX-LSSA: `space status` MUST obtain its answer from exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the space's `Address` and whose `Deps` is `deps`, with an empty `step` and exactly the arguments `opsctl` and `status`, and MUST write that call's `Output.Stdout` to stdout byte for byte and nothing else, neither parsing, reordering, reformatting, nor validating it; verified at least with a fake `Deps.Exec` that exits 0 with arbitrary multi-line standard output, with one whose standard output has no trailing newline, and with one whose standard output is empty, each reproduced on stdout unchanged.

- R-U8TJ-DJOY: `space stop` MUST write the step `instance` first, whose detail MUST be `already stopped`, with `StopInstance` not called, when the space's `State` is `cloud.StateStopped`, and otherwise MUST call `acct.Clients.EC2.StopInstance` with the space's `ID` exactly once, then `WaitState` for `cloud.StateStopped`, and report the space's `ID` followed by ` stopped`; verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 stopped)` and `instance: ok (already stopped)`.

- R-UB9C-536C: `space start` MUST write the step `instance` first, MUST call `acct.Clients.EC2.StartInstance` with the space's `ID` exactly once and then `WaitState` for `cloud.StateRunning` when the space's `State` is not `cloud.StateRunning` and MUST call `StartInstance` not at all when it is, and MUST report the space's `ID`, ` running, `, and the running instance's `Address`; verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 running, 3.145.72.19)`.

- R-UIKQ-FPMI: The `instance` step of `space destroy` MUST report `already gone`, with no EC2 operation other than the space lookup called, when `(*Account).Space` returned a `*account.NoSpaceError`, and otherwise MUST call `acct.Clients.EC2.TerminateInstance` with the space's `ID` exactly once, then `WaitState` for `cloud.StateTerminated`, and report the space's `ID` followed by ` terminated`.

- R-UL0J-793W: The `address` step of `space destroy` MUST report `elastic ip `, the `cloud.Address`'s `IP`, and ` released` after calling `ReleaseElasticIP` for it when `ElasticIP` reports an Elastic IP for the domain; MUST report `no elastic ip` when it reports none and the `instance` step terminated an instance; and MUST report `already gone` when it reports none and the `instance` step reported `already gone`; verified at least by reproducing `address: ok (elastic ip 18.220.10.5 released)`, `address: ok (no elastic ip)`, and `address: ok (already gone)`.

- R-UNGB-YSLA: The `secrets` step of `space destroy` MUST report `kept, delete_secrets_on_destroy=false`, with no SSM operation called, when `acct.Properties.DeleteSecretsOnDestroy` is false, and MUST otherwise call `acct.Clients.SSM.ListParameters` with `secrets.Prefix(domain)` and `acct.Clients.SSM.DeleteParameter` exactly once for each `cloud.Parameter` it returned, and report that count followed by ` parameters deleted` when the count is not 0 or the `instance` step terminated an instance and `already gone` otherwise; verified at least by reproducing `secrets: ok (3 parameters deleted)`, `secrets: ok (kept, delete_secrets_on_destroy=false)`, and `secrets: ok (already gone)`.

- R-UOO8-CKBZ: The `backups` step of `space destroy` MUST report `kept, delete_backups_on_destroy=false`, with no S3 operation called, when `acct.Properties.DeleteBackupsOnDestroy` is false, and MUST otherwise call `acct.Clients.S3.ListObjects` with `acct.Properties.BackupBucket` and `BackupPrefix(domain)` and then `acct.Clients.S3.DeleteObjects` with every key it returned, and report the number of those keys followed by ` objects deleted` when that number is not 0 or the `instance` step terminated an instance and `already gone` otherwise; verified at least by reproducing `backups: ok (0 objects deleted)`, `backups: ok (kept, delete_backups_on_destroy=false)`, and `backups: ok (already gone)`.

- R-UPW4-QC2O: The `role` step of `space destroy` MUST report `RoleName(domain)` followed by ` deleted` when `DeleteRole` returned true and `already gone` when it returned false; verified at least by reproducing `role: ok (ikigenba-space-foo.sbx.ikigenba.dev deleted)` and `role: ok (already gone)`.

- R-D5NY-WFYQ: Package `internal/host` MUST export `CommandError` with exactly `Step string`, `Command []string`, `Status int`, `Stdout string`, and `Stderr string`; `Error() string` MUST return the space-joined command followed by `: exit status <Status>`, preceded by `<Step>: ` when nonempty; `Detail() string` MUST return `seam.QuoteOutput(Stderr)` when stderr contains non-whitespace and otherwise `seam.QuoteOutput(Stdout)`; `ExitCode() int` MUST return 1.

- R-D6VV-A7PF: Package `internal/host` MUST export `Output` with exactly `Stdout string` and `Stderr string`, and methods `Run(ctx context.Context, step string, args ...string) (Output, error)`, `Sudo(ctx context.Context, step string, args ...string) (Output, error)`, `StreamSudo(ctx context.Context, stdout io.Writer, step string, args ...string) error`, and `Wait(ctx context.Context) error` on `Host`.

- R-D83R-NZG4: `Host.Run` MUST invoke `ssh` through `Deps.Exec` in `Deps.Dir` with `-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new`, `Target()`, and a remote command preserving each supplied argument as one literal shell word; `Host.Sudo` MUST prefix that remote argument vector with `sudo`. Tests MUST include spaces, quotes, wildcard characters and shell metacharacters to prove no argument becomes shell syntax.

- R-D9BO-1R6T: `Host.Run` and `Host.Sudo` MUST return unmodified stdout and stderr as Output and nil error on exit 0; on a nonzero exit they MUST return CommandError carrying step, status and both unmodified streams, with Command equal to `ssh`, Target(), and the logical remote argument vector, omitting transport flags and shell escaping. A process-start error MUST wrap the runner error, name ssh, and not masquerade as CommandError.

- R-DAJK-FIXI: `Host.StreamSudo` MUST execute the same SSH command as `Sudo` through `Deps.Stream`, stream stdout unchanged, and return nonzero process exits using `CommandError` with empty Stdout and the captured stderr; already-streamed output MUST NOT be repeated. Process-start, writer and context failures MUST remain distinguishable errors wrapping the runner error.

- R-8WFQ-8ZI4: `devctl space --help` and `devctl space -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space <subcommand> [arguments]

  List, create, destroy, stop, start, initialise, and inspect spaces in one
  account, and restart or read the journal of one app on one. A space is one
  instance named by its full domain; the cloud's tags are the only registry.

  Subcommands:
    list                       one line per space in the account
    create <domain> [options]  create the space at <domain>
    destroy <domain> [options] remove the space and everything it owned
    stop <domain>              stop the instance; state is kept
    start <domain>             start the instance; its address is unchanged
    init <domain> [options]    set the host's keys again and run opsctl init
    status <domain>            one line per app: version, service state, database journal mode
    restart <domain> <app>     restart one app's service on the host
    logs <domain> <app>        print one app's journal from the host

  Options (create):
    --acme-email <address>  where the CA sends the space's expiry warnings; required

  Options (destroy):
    --no-backup             skip the final backup an account that keeps backups takes

  Options (init):
    --opsctl <version>      move the host to this opsctl release first
    --acme-email <address>  change where the CA sends the space's expiry warnings

  Options (logs):
    --follow                keep printing as the app writes, until interrupted
    --since <when>          start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00

  Every subcommand needs --account. Run 'devctl space <subcommand> --help' for details.
  ```

- R-DCZD-72EW: `devctl space destroy --help` and `devctl space destroy -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space destroy <domain> [--no-backup]

  Retire the host first when the account keeps backups, then remove the instance,
  Elastic IP, records and role. Account retention settings decide whether secrets
  and backups are deleted. Run again to finish a partial destroy.

  Options:
    --no-backup   skip the final backup
  ```

- R-DE79-KU5L: `devctl space stop --help` and `devctl space stop -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space stop <domain>

  Stop the instance and keep its disk, Elastic IP, records, secrets and backups.
  ```

- R-DFF5-YLWA: `devctl space start --help` and `devctl space start -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space start <domain>

  Start the instance at its existing Elastic IP, wait for status checks and SSH,
  then run certbot renew. Records are unchanged; the last line is domain and address.
  ```

- R-DGN2-CDMZ: `devctl space status --help` and `devctl space status -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space status <domain>

  Relay opsctl status from the running host: app, version, service state and
  database journal mode. A host with no apps prints nothing.
  ```

- R-DHUY-Q5DO: `cli.Run` MUST dispatch `space create` to `spacecreate.Run`, `space init` to `spaceinit.Run`, `space restart` and `space logs` to `spaceapps.Run`, and other `space` invocations to `space.Run`; create/init receive arguments after their subcommand, spaceapps receives the subcommand and following arguments, and every dispatch receives the original stdout, deps and profile and returns 0 on nil error.

- R-DJ2V-3X4D: The `space` subcommands MUST be exactly `list`, `create`, `destroy`, `stop`, `start`, `init`, `status`, `restart`, and `logs`; list takes no operand, destroy/stop/start/status take exactly one domain, and destroy alone among those five accepts `--no-backup`, in addition to help. New subcommand grammars MUST be those declared by their owning packages.

- R-DLIN-VGLR: An argument of `space` or of one of its subcommands other than `create`, `init`, `restart`, and `logs` that begins with `-` and is neither `--help` nor `-h` nor destroy’s `--no-backup` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl space --help' for usage` to be written to stderr, nothing to stdout, and exit 2.

- R-DMQK-98CG: `space.Run` MUST NOT open a checkout; `space list` and `space stop` MUST execute no external process.

- R-8XNM-MR8T: `space stop` MUST print only its instance step and MUST NOT change DNS records, release or disassociate the Elastic IP, modify storage, resource tags, secrets or IAM resources, or execute any host command.

- R-DP6D-0RTU: `space start` MUST write the step `host` second, with the detail `status checks passed`, after `WaitChecks` for the space's `ID` returned a nil error.

- R-B1XP-NI4K: `space start` MUST write the step `certificate` third, after exactly one successful call to `(host.Host).Wait` and then exactly one successful call to `(host.Host).Sudo` with the step `certificate` and exactly the arguments `certbot` and `renew`, both on a `host.Host` whose `Address` is the running instance's `Address` and whose `Deps` is `deps`; its detail MUST be `certbot renew`, verified by reproducing `certificate: ok (certbot renew)`. A zero exit is the whole of the step's success, whether or not certbot renewed anything; a failed call MUST produce no certificate step line and MUST return the host error unchanged.

- R-DRM5-SBB8: `space start` MUST perform only `instance`, `host`, and `certificate` steps in that order, followed on success by `<domain> <address>`; it MUST NOT change records or the Elastic IP. Failure MUST preserve completed steps and omit the final address line. A running instance MUST still receive status and renewal checks on a repeated start.

- R-DSU2-631X: `space destroy` MUST perform `retire` only when backups are retained, then `instance`, `address`, `records`, `secrets`, `backups`, and `role`, in that order, stopping at the first failure without undoing completed steps and exiting 0 only after all applicable steps complete.

- R-DU1Y-JUSM: The records step of destroy MUST delete only the space’s apex and wildcard A records using their returned record sets, report `records: ok (deleted <domain>, *.<domain>)` when both existed, list only the deleted name when one existed, and report `records: ok (already gone)` when neither record or no zone exists.

- R-3KXX-07CJ: When backups are retained and the instance exists, destroy without `--no-backup` MUST run `sudo opsctl retire` with step `retire` before any direct cloud mutation by devctl; a remote failure MUST leave the instance, address, records, secrets and IAM resources untouched, preserve any backup writes already made by opsctl and omit all step lines, while quoting the remote report in its diagnostic.

- R-DWHR-BEA0: When retained-backup destroy finds no instance, it MUST report `retire: ok (already gone)` without SSH; with an existing instance and `--no-backup` it MUST instead print exactly `retire: skipped (--no-backup)` and perform no SSH. When backups are deleted on destroy, `--no-backup` MUST be accepted with no retire line and no host call.

- R-DXPN-P60P: Package `internal/space` MUST export `RetireStateError` with exactly `ID string`, `State cloud.InstanceState`, `Domain string`, and `Profile string`, implementing `Error() string` as `retire: instance <ID> is <State>`, `ExitCode() int` as 1, and `Detail() string` as `run 'devctl --account <Profile> space start <Domain>' first, or pass --no-backup`.

- R-DYXK-2XRE: Retained-backup destroy without `--no-backup` MUST refuse an existing non-running instance with `RetireStateError`, empty stdout and no mutations or SSH.

- R-JCCP-CNWX: After `sudo opsctl retire` exits 0, destroy MUST report `retire: ok (opsctl retire)`; a zero exit is the whole of the step's success, and devctl MUST NOT parse opsctl's output or read the backup bucket to describe what was backed up. Verified by reproducing `retire: ok (opsctl retire)`.

- R-E1DC-UH8S: Destroy MUST accept `--no-backup` on either side of its domain operand, with repeated occurrences having the same effect as one; value-bearing forms such as `--no-backup=true` MUST be unknown options.

- R-8YVJ-0IZI: `space list` and `space status` MUST perform no cloud mutation and MUST leave local checkout files unchanged; status MUST execute no host command other than its single `sudo opsctl status` invocation. Tests MUST use cloud fakes that fail on mutation and verify the host command count for both populated and empty results.
