# D06-space

Spaces are discovered from cloud tags and have an Elastic IP throughout their
lifetime. The platform is one root domain in one account: every `space`
subcommand that touches the cloud reads the root and its region from the
checkout's root file (D04), parses its `<space>` operand with `spaceref`
(D04), opens one `cloud.Session` (D03) whose first call is STS, and finds the
space through `cloud.LookupSpace`. Nothing is configured on the developer's
machine and no `--account` exists. Output names the full space domain even
when the label was typed; the one place a label appears is the advice line
that tells the developer which command to run next.

`space list` prints one line per space and marks the holder of the apex: the
space whose Elastic IP the root's `A` record points at. The holder is read
from the zone and the address tags, never from a host, by `FindApex`, which
`space destroy` and D14's `apex` command share. Stop and start preserve
records and the address; start runs the host's renewal check in the
foreground. Destroy deletes the root's record first when this space holds it,
has the host take its final backup with `opsctl retire`, then removes the
instance, the address, the space's two `A` records, and the role, resumably.
Secrets and backups are kept unless `--delete-secrets` or `--delete-backups`
says otherwise; each option governs only its own step. A stopped host cannot
take a backup, so destroy without `--no-backup` refuses a non-running instance
before it prints or changes anything, the apex record included.

What devctl makes for a space is named after the space: the role and its
instance profile are the space domain, the records are `<space domain>` and
`*.<space domain>`, and the backup prefix in the bucket named after the root
is the space's label followed by `/`. The role's one inline policy is built
here too: `PolicyDocument` derives it from the root, the zone id, the space,
and whether the space holds the apex, so `space create` (D07) and `apex` (D14)
regenerate the same document and can never disagree about what a space's role
may do. The wait helpers poll a cloud fact on
the shared interval until it holds: `WaitState` and `WaitChecks` watch an
instance, `WaitLaunchReady` watches a launch spec become acceptable, and
`WaitInsync` watches a Route 53 change, so a caller can hold a step open until
the resource it just made is genuinely usable. Every helper takes the one
client interface it needs, as D03's `Spaces` and D05's `Push` do; no session
or checkout type crosses into a helper.

Host execution lives in `internal/host`: `Host{Address, Deps}` runs a command
over ssh with every argument preserved as one literal word, `Sudo` returns the
captured `Output{Stdout, Stderr}` so a caller can use stdout (`opsctl status`
relays it; D10's `GetKey` reads it), and a non-zero exit is a `*CommandError`
whose `Detail()` quotes what the remote program wrote to stdout and then what
it wrote to stderr, each line prefixed `> `. A command's product is on its
stdout and its diagnostic on its stderr (the repository's command-line
convention), so a failing opsctl's report comes first and its reason after it;
neither is dropped because the other is present. devctl never asserts what
those bytes say. D07, D09, D10, D11, D12, D13,
and D14 issue every host command through this one shape.

## REQUIREMENTS

- R-RJ9Z-1OB0: Package `internal/space` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-SR5Y-JN2E: Package `internal/space` MUST export `Step(w io.Writer, name, detail string)`, which MUST write `<name>: ok (<detail>)` followed by one newline to `w` when `detail` is not empty and `<name>: ok` followed by one newline when `detail` is empty, verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 terminated)` and `init: ok`.

- R-U273-JQ19: Package `internal/space` MUST export `RoleName(domain string) string`, returning `domain` unchanged, so that a space's IAM role and its instance profile are both named exactly the space domain; `PolicyName = "space"`; `BackupPrefix(label string) string`, returning `label` followed by `/`; `RecordNames(domain string) []string`, returning exactly `domain` and `*.` followed by `domain`, in that order; and `RecordTTL = 60`; verified at least by `RoleName("sbx1.ikigenba.dev")` being `sbx1.ikigenba.dev`, `BackupPrefix("sbx1")` being `sbx1/`, and `RecordNames("sbx1.ikigenba.dev")` being `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev`.

- R-U3EZ-XHRY: Package `internal/space` MUST export `WaitState(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, id string, state cloud.InstanceState) (cloud.Instance, error)`, `WaitChecks(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, id string) error`, `WaitLaunchReady(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, spec cloud.LaunchSpec) error`, `WaitInsync(ctx context.Context, deps seam.Deps, route53 cloud.Route53, changeID string) error`, `PollInterval = 5 * time.Second`, and `PollAttempts = 60`.

- R-U4MW-B9IN: Package `internal/space` MUST export `ElasticIP(ctx context.Context, ec2 cloud.EC2, root, domain string) (cloud.Address, bool, error)` and `ReleaseElasticIP(ctx context.Context, ec2 cloud.EC2, addr cloud.Address) error`.

- R-U5US-P19C: Package `internal/space` MUST export `PutRecords(ctx context.Context, deps seam.Deps, route53 cloud.Route53, zone cloud.Zone, domain, address string) error` and `DeleteRecords(ctx context.Context, route53 cloud.Route53, zone cloud.Zone, domain string) ([]string, error)`.

- R-U72P-2T01: Package `internal/space` MUST export `DeleteRole(ctx context.Context, iam cloud.IAM, domain string) (bool, error)`.

- R-VPQM-VSH2: Package `internal/space` MUST export `PolicyDocument(root, zoneID string, sp spaceref.Space, apex bool) string`, returning an IAM policy document that is one JSON object whose `Version` is `2012-10-17`.

- R-VQYJ-9K7R: `PolicyDocument` MUST produce its result from its arguments alone, reading no file and embedding none, and MUST return identical text for identical arguments; the module MUST contain no `internal/space/templates` and no `internal/spacecreate/templates` directory, and `internal/spacecreate` MUST export neither a `PolicyDocument` nor an embedded policy template, so that `internal/space` alone owns the builder.

- R-9ANZ-12GL: The document `PolicyDocument` returns MUST grant `ssm:GetParameter` on exactly the resource `arn:aws:ssm:*:*:parameter/<sp.Domain>/*` and MUST grant no other `ssm:` action, so that the host reads each app's secrets object that create and `secrets push` write and nothing outside the space's prefix; verified at least by parsing the JSON for the space `sbx1` under `ikigenba.dev` and finding an `Allow` statement for `ssm:GetParameter` whose only resource is `arn:aws:ssm:*:*:parameter/sbx1.ikigenba.dev/*`, no action beginning `ssm:Put`, `ssm:Delete`, or `ssm:*`, and no resource naming `parameter/ikigenba/`.

- R-9D3R-SLXZ: The document `PolicyDocument` returns MUST grant `s3:GetObject` and `s3:PutObject` on exactly `arn:aws:s3:::<root>/<sp.Label>/*`, and `s3:ListBucket` on `arn:aws:s3:::<root>` only under a `StringLike` condition on `s3:prefix` whose values are exactly `<sp.Label>/` and `<sp.Label>/*`, and MUST grant no other `s3:` action and no `s3:` action on any other resource, so that the host reads `<sp.Label>/deploy/`, writes under `<sp.Label>/`, and gains no access to another space's prefix; verified at least by parsing the JSON for `sbx1` under `ikigenba.dev` and finding the object resource `arn:aws:s3:::ikigenba.dev/sbx1/*`, the bucket resource `arn:aws:s3:::ikigenba.dev` with the prefixes `sbx1/` and `sbx1/*`, and no resource or prefix naming `sbx2/`.

- R-VS6F-NBYG: The document `PolicyDocument` returns MUST grant exactly three Route 53 permissions and no other `route53:` action: `route53:ListResourceRecordSets` on `arn:aws:route53:::hostedzone/<zoneID>`, `route53:GetChange` on `arn:aws:route53:::change/*`, and `route53:ChangeResourceRecordSets` on `arn:aws:route53:::hostedzone/<zoneID>` under a `ForAllValues:StringEquals` condition requiring `route53:ChangeResourceRecordSetsRecordTypes` to equal `TXT` and a `ForAllValues:StringLike` condition whose `route53:ChangeResourceRecordSetsNormalizedRecordNames` values are exactly `<sp.Domain>` and `*.<sp.Domain>` when `apex` is false, and exactly those two and `_acme-challenge.<root>` when `apex` is true; verified at least by parsing the JSON for `sbx1` under `ikigenba.dev` with the zone id `Z09565073GHK8BYWQ1A78` and each value of `apex`, checking that the `route53:ChangeResourceRecordSetsRecordTypes` values are exactly `TXT`, that the `route53:ChangeResourceRecordSetsNormalizedRecordNames` values are exactly `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev` when `apex` is false, and those two and `_acme-challenge.ikigenba.dev` when `apex` is true, and that `route53:ListHostedZones` is absent.

- R-U8AL-GKQQ: Package `internal/space` MUST export an `ApexRecord` struct whose fields are exactly `Record cloud.Record`, `Found bool`, and `Holder string`, and `FindApex(ctx context.Context, route53 cloud.Route53, ec2 cloud.EC2, zoneID, root string) (ApexRecord, error)`.

- R-SZP9-8199: Package `internal/space` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage`, and `ExitCode() int`, returning 2.

- R-XQOJ-QF8R: Package `internal/space` MUST export a `NotRunningError` struct whose fields are exactly `Domain string` and `State cloud.InstanceState`, with the method `Error() string`, returning `'<Domain>' is <State>`, verified at least by reproducing `'sbx2.ikigenba.dev' is stopped`.

- R-T251-ZKQN: Package `internal/space` MUST export a `WaitError` struct whose fields are exactly `Subject string` and `Want string`, with the method `Error() string`, returning `timed out waiting for <Subject> to <Want>`, verified at least by reproducing `timed out waiting for i-0c9e94542d98846a8 to be running`.

- R-U9IH-UCHF: Package `internal/space` MUST export a `RetireStateError` struct whose fields are exactly `ID string`, `State cloud.InstanceState`, and `Label string`, with the methods `Error() string`, returning `retire: instance <ID> is <State>`, `ExitCode() int`, returning 1, and `Detail() string`, returning `run 'devctl space start <Label>' first, or pass --no-backup`, verified at least by reproducing `retire: instance i-0a1b2c3d4e5f60718 is stopped` and `run 'devctl space start staging' first, or pass --no-backup`.

- R-T3CY-DCHC: Package `internal/host` MUST export `User = "ec2-user"`, a `Host` struct whose fields are exactly `Address string` and `Deps seam.Deps`, and the method `Target() string` on `Host`, returning `User`, `@`, and `Address` joined, verified at least by the `Target()` of a `Host` whose `Address` is `3.19.79.227` being `ec2-user@3.19.79.227`.

- R-T5SR-4VYQ: Package `internal/host` MUST export `ProbeInterval = 5 * time.Second` and `ProbeAttempts = 12`.

- R-T88J-WFG4: Package `internal/host` MUST export an `UnreachableError` struct whose only field is `Address string`, with the method `Error() string`, returning `ssh ` followed by `ec2-user@<Address>` and `: connection timed out`, verified at least by reproducing `ssh ec2-user@3.145.72.19: connection timed out`.

- R-TD45-FIEW: `(host.Host).Wait` MUST pass to `Deps.Exec` the `seam.Cmd` that `Run` passes for the single argument `true`, MUST return a nil error as soon as one of those processes exits 0, MUST wait on `Deps.After(ProbeInterval)` between consecutive ones, MUST pass at most `ProbeAttempts` of them, and MUST return an `*UnreachableError` whose `Address` is `Address` when that many have not exited 0; verified with a fake `Deps.After` that fires immediately.

- R-UAQE-8484: `space.WaitState` MUST call `ec2.DescribeInstance` with `id` and return that `cloud.Instance` and a nil error as soon as its `State` equals `state` and — when `state` is `cloud.StateRunning` — its `Address` is not empty, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `be ` followed by `state` when that many calls have not satisfied it; verified with a fake `Deps.After` that fires immediately.

- R-UBYA-LVYT: `space.WaitChecks` MUST call `ec2.InstanceChecksPassed` with `id` and return a nil error as soon as it reports true, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `pass its status checks` when that many calls have not reported true.

- R-UD66-ZNPI: `space.WaitLaunchReady` MUST call `ec2.LaunchReady` with `spec` and return a nil error as soon as it reports true, MUST return its error unchanged as soon as it returns a non-nil one, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `spec.InstanceProfile` and whose `Want` is `become usable for launch` when that many calls have not reported true.

- R-UFLZ-R76W: `space.WaitInsync` MUST call `route53.ChangeStatus` with `changeID` and return a nil error as soon as it reports `cloud.ChangeInsync`, MUST return its error unchanged as soon as it returns a non-nil one, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `changeID` and whose `Want` is `reach INSYNC` when that many calls have not reported it; verified with a fake `Deps.After` that fires immediately.

- R-UGTW-4YXL: `space.ElasticIP` MUST call `ec2.ListSpaceAddresses` with `root` and return, with a true second result, the one `cloud.Address` whose `Space` equals `domain`; MUST return a false second result and a nil error when none does; and MUST return a non-nil error naming `domain` when two or more do.

- R-UI1S-IQOA: `space.ReleaseElasticIP` MUST call `ec2.DisassociateAddress` with `addr.AssociationID` exactly once when that field is not empty and not at all when it is, and MUST then call `ec2.ReleaseAddress` with `addr.AllocationID` exactly once.

- R-UJ9O-WIEZ: `space.PutRecords` MUST call `route53.ChangeRecords` for `zone.ID` exactly once, with one `cloud.RecordChange` per element of `RecordNames(domain)` in that order, each whose `Action` is `cloud.ChangeUpsert` and whose `Record` has that element as `Name`, `A` as `Type`, `RecordTTL` as `TTL`, and exactly `address` as `Values`; MUST then call `route53.ChangeStatus` with the change id that call returned until it reports `cloud.ChangeInsync`, waiting on `deps.After(PollInterval)` between consecutive calls and making at most `PollAttempts` of them; and MUST return a `*WaitError` whose `Subject` is that change id and whose `Want` is `reach INSYNC` when that many calls have not reported it.

- R-UKHL-AA5O: `space.DeleteRecords` MUST call `route53.ListRecords` for `zone.ID` and MUST pass to `route53.ChangeRecords` for `zone.ID`, in one call, one `cloud.RecordChange` whose `Action` is `cloud.ChangeDelete` for each returned `cloud.Record` whose `Type` is `A` and whose `Name` is `domain`, `*.` followed by `domain`, or `\052.` followed by `domain`, each carrying that `cloud.Record` exactly as `ListRecords` returned it; MUST return the names of the records so deleted as `RecordNames(domain)` spells them — `domain` for the bare record and `*.` followed by `domain` for the wildcard however `ListRecords` spelled it — in that order; and MUST return an empty result and call `ChangeRecords` not at all when there are none; verified at least with a `ListRecords` that returns the wildcard record as `\052.staging.ikigenba.dev` and a `ChangeRecords` that records the `cloud.Record` it was given, the result being exactly `staging.ikigenba.dev` and `*.staging.ikigenba.dev`.

- R-ULPH-O1WD: `space.DeleteRole` MUST call `iam.RoleExists` and `iam.InstanceProfileRoles` for `RoleName(domain)`, MUST return false and a nil error and call no other IAM operation when neither reports existence, and otherwise MUST call `DeleteRolePolicy` for that role and `PolicyName`, `RemoveRoleFromInstanceProfile` for that instance profile and each role it holds, `DeleteInstanceProfile`, and `DeleteRole`, in that order, and return true.

- R-UMXE-1TN2: `space.FindApex` MUST call `route53.FindRecord(ctx, zoneID, root, "A")` and, when it reports no record, return an `ApexRecord` whose `Found` is false and whose `Holder` is empty without calling `ec2`; otherwise it MUST call `ec2.ListSpaceAddresses(ctx, root)` and return an `ApexRecord` whose `Record` is the record `FindRecord` returned unchanged, whose `Found` is true, and whose `Holder` is the `Space` of the one `cloud.Address` whose `IP` is an element of that record's `Values`, or empty when no address's `IP` is; and it MUST return a non-nil error naming `root` when two or more addresses match; verified at least by `Holder` being `sbx1.ikigenba.dev` for a record holding `18.118.7.42` and an address with that `IP` and that `Space`, by `Holder` being empty for a record holding `203.0.113.9` that no address carries, and by `Found` being false with no `ListSpaceAddresses` call when the record is absent.

- R-JA8W-5LZ5: Package `internal/host` MUST export `CommandError` with exactly `Step string`, `Command []string`, `Status int`, `Stdout string`, and `Stderr string`; `Error() string` MUST return the space-joined command followed by `: exit status <Status>`, preceded by `<Step>: ` when nonempty; `Detail() string` MUST return `seam.QuoteOutput(Stdout)` followed by `seam.QuoteOutput(Stderr)`, each included only when that stream contains non-whitespace, the two separated by exactly one newline when both are included, and the empty string when neither is, so that what the remote program reported on its standard output precedes what it wrote to its standard error; `ExitCode() int` MUST return 1; verified at least by a `CommandError` whose `Stdout` is `a\nb\n` and whose `Stderr` is `c\n\n> d\n` giving a `Detail()` of exactly the five lines `> a`, `> b`, `> c`, `> `, and `> > d`, by one whose `Stderr` is empty and one whose `Stderr` is only whitespace each giving `seam.QuoteOutput(Stdout)` alone, and by one whose `Stdout` is empty giving `seam.QuoteOutput(Stderr)` alone.

- R-D6VV-A7PF: Package `internal/host` MUST export `Output` with exactly `Stdout string` and `Stderr string`, and methods `Run(ctx context.Context, step string, args ...string) (Output, error)`, `Sudo(ctx context.Context, step string, args ...string) (Output, error)`, `StreamSudo(ctx context.Context, stdout io.Writer, step string, args ...string) error`, and `Wait(ctx context.Context) error` on `Host`.

- R-D83R-NZG4: `Host.Run` MUST invoke `ssh` through `Deps.Exec` in `Deps.Dir` with `-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new`, `Target()`, and a remote command preserving each supplied argument as one literal shell word; `Host.Sudo` MUST prefix that remote argument vector with `sudo`. Tests MUST include spaces, quotes, wildcard characters and shell metacharacters to prove no argument becomes shell syntax.

- R-D9BO-1R6T: `Host.Run` and `Host.Sudo` MUST return unmodified stdout and stderr as Output and nil error on exit 0; on a nonzero exit they MUST return CommandError carrying step, status and both unmodified streams, with Command equal to `ssh`, Target(), and the logical remote argument vector, omitting transport flags and shell escaping. A process-start error MUST wrap the runner error, name ssh, and not masquerade as CommandError.

- R-DAJK-FIXI: `Host.StreamSudo` MUST execute the same SSH command as `Sudo` through `Deps.Stream`, stream stdout unchanged, and return nonzero process exits using `CommandError` with empty Stdout and the captured stderr; already-streamed output MUST NOT be repeated. Process-start, writer and context failures MUST remain distinguishable errors wrapping the runner error.

- R-VSDZ-20UO: Every line that `internal/space`, `internal/spacecreate`, `internal/spaceinit`, `internal/spaceapps`, `internal/deploy`, `internal/restore`, `internal/remove`, and `internal/apex` write to stdout to report a completed step MUST be written by `space.Step`, verified by a test over those packages' source that no non-test file of a package other than `internal/space` contains the string `: ok (`; and a command MUST write no step line for a step that did not complete.

- R-JBGS-JDPU: `devctl space --help` and `devctl space -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space <subcommand> [arguments]

  List, create, destroy, stop, start, initialise, and inspect spaces, and
  restart, disable, enable, or read the journal of one app on one. A space is
  one label under the root domain; <space> is that label or the full domain.
  The cloud's tags are the only registry.

  Subcommands:
    list                       one line per space
    create <space> [options]   create the space
    destroy <space> [options]  remove the space and everything it owned
    stop <space>               stop the instance; state is kept
    start <space>              start the instance; its address is unchanged
    init <space> [options]     set the host's keys again and run opsctl init
    status <space>             one line per app: version, service state, socket state, database journal mode
    restart <space> <app>      restart one app's service on the host
    disable <space> <app>      stop one app and keep it from starting until enabled
    enable <space> <app>       let a disabled app start again, and start it
    logs <space> <app>         print one app's journal from the host

  Options (create):
    --acme-email <address>  where the CA sends the space's expiry warnings; required

  Options (destroy):
    --no-backup             skip the final backup the host takes before it goes
    --delete-secrets        delete the space's secrets; they are kept otherwise
    --delete-backups        delete the space's backups; they are kept otherwise

  Options (init):
    --opsctl <version>      move the host to this opsctl release first
    --acme-email <address>  change where the CA sends the space's expiry warnings

  Options (logs):
    --follow                keep printing as the app writes, until interrupted
    --since <when>          start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00

  Run 'devctl space <subcommand> --help' for details.
  ```

- R-UPD6-TD4G: `devctl space list --help` and `devctl space list -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space list

  Print one line per space: the domain, the instance state, the public address
  or - when it has none, and apex on the one space that holds the root domain or
  - on every other. The lines are sorted by domain, and no spaces prints nothing.

  The cloud's tags are the only registry: a space is an instance tagged with the
  root domain and a Space tag naming its own. The holder of the root domain is
  the space whose Elastic IP the root's A record points at; no host is asked.
  What a space is running is 'devctl space status'.
  ```

- R-UQL3-74V5: `devctl space destroy --help` and `devctl space destroy -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space destroy <space> [--no-backup] [--delete-secrets] [--delete-backups]

  Delete the root domain's record first when this space holds it, have the host
  take its final backup with opsctl retire, then remove the instance, Elastic IP,
  records and role. Secrets and backups are kept unless an option says otherwise.
  Run again to finish a partial destroy.

  Options:
    --no-backup        skip the final backup the host takes before it goes
    --delete-secrets   delete the space's secrets; they are kept otherwise
    --delete-backups   delete the space's backups; they are kept otherwise
  ```

- R-URSZ-KWLU: `devctl space stop --help` and `devctl space stop -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space stop <space>

  Stop the instance and keep its disk, Elastic IP, records, secrets and backups.
  If the space holds the root domain, the root keeps pointing at it.
  ```

- R-UT0V-YOCJ: `devctl space start --help` and `devctl space start -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space start <space>

  Start the instance at its existing Elastic IP, wait for status checks and SSH,
  then run certbot renew. Records are unchanged; the last line is domain and address.
  ```

- R-JCOO-X5GJ: `devctl space status --help` and `devctl space status -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space status <space>

  Relay opsctl status from the running host: app, version, service state, socket
  state and database journal mode. A host with no apps prints nothing.
  ```

- R-UVGO-Q7TX: `devctl space` MUST write exactly the three lines `devctl: space needs <subcommand>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, call `Deps.Cloud` not at all, pass no `seam.Cmd` to `Deps.Exec`, and exit 2.

- R-TWMJ-JUA0: `space` invoked with a first argument that is none of its subcommands, `--help`, and `-h` and does not begin with `-` MUST write exactly the three lines `devctl: unknown subcommand '<name>'`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-UWOL-3ZKM: `space destroy`, `space stop`, `space start`, and `space status` invoked with no operand MUST each write exactly the three lines `devctl: space <subcommand> needs <space>` — where `<subcommand>` is the invoked subcommand's own name — an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, call `Deps.Cloud` not at all, pass no `seam.Cmd` to `Deps.Exec`, and exit 2, verified at least by reproducing `devctl: space destroy needs <space>`.

- R-UZ4D-VJ20: `space destroy`, `space stop`, `space start`, and `space status` invoked with more than one operand MUST each write `devctl: space <subcommand> takes only <space>`, and `space list` invoked with any operand MUST write `devctl: space list takes no arguments`, as the first of exactly three lines followed by an empty line and `see 'devctl space --help' for usage` on stderr, write nothing to stdout, call `Deps.Cloud` not at all, pass no `seam.Cmd` to `Deps.Exec`, and exit 2.

- R-JDWL-AX78: `cli.Run` MUST dispatch `space create` to `spacecreate.Run`, `space init` to `spaceinit.Run`, `space restart`, `space disable`, `space enable`, and `space logs` to `spaceapps.Run`, and every other `space` invocation to `space.Run`; create and init receive the arguments after their subcommand, spaceapps receives the subcommand and the arguments after it, and every dispatch receives `cli.Run`'s own `ctx`, `stdout`, and `deps` and nothing else, and returns 0 on a nil error.

- R-JF4H-OOXX: The `space` subcommands MUST be exactly `list`, `create`, `destroy`, `stop`, `start`, `init`, `status`, `restart`, `disable`, `enable`, and `logs`; `list` takes no operand, `destroy`, `stop`, `start`, and `status` take exactly one `<space>` operand, and `destroy` alone among those five accepts options, exactly `--no-backup`, `--delete-secrets`, and `--delete-backups`, in addition to help; the grammars of `create`, `init`, `restart`, `disable`, `enable`, and `logs` MUST be those their owning packages declare.

- R-JHKA-G8FB: An argument of `space` or of one of its subcommands other than `create`, `init`, `restart`, `disable`, `enable`, and `logs` that begins with `-` and is none of `--help`, `-h`, and — for `destroy` only — `--no-backup`, `--delete-secrets`, and `--delete-backups` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl space --help' for usage` to be written to stderr, nothing to stdout, and exit 2, with `Deps.Cloud` not called and no `seam.Cmd` passed to `Deps.Exec`; verified at least by `devctl space stop sbx1 --no-backup` and `devctl space destroy sbx1 --no-backup=true`.

- R-V3ZZ-EM0S: `space destroy` MUST accept each of `--no-backup`, `--delete-secrets`, and `--delete-backups` on either side of its `<space>` operand and in any order, with repeated occurrences of one option having the same effect as one, and each option MUST govern only its own step — `--no-backup` the `retire` step, `--delete-secrets` the `secrets` step, and `--delete-backups` the `backups` step; a value-bearing form such as `--no-backup=true` or `--delete-secrets=yes` MUST be an unknown option.

- R-V57V-SDRH: `space list`, `space destroy`, `space stop`, `space start`, and `space status` MUST each, after its usage errors are reported and before any other call, call `checkout.ReadRootFile(ctx, deps)`, then — for the four that take `<space>` — `spaceref.Parse` with the operand as typed and the `RootFile`'s `Domain` (D04 R-QW0J-KUFF), then `cloud.Connect(ctx, deps.Cloud, RootFile.Domain, RootFile.Region)` (D02 R-OO0I-HZUA), returning each call's error unchanged and making no later call when one fails; in the requirements below, `root` is that `Domain`, `space` is the `spaceref.Space` `Parse` returned, `clients` is the `Clients` of the `Session` `Connect` returned, and `zone` is what `clients.Route53.Zone(ctx, root)` returned; verified at least through `cli.Run` by `devctl space stop sbx1` in a temporary checkout whose root file holds `{"domain": "example.test", "region": "eu-west-1"}` leaving a recording fake `Deps.Cloud` with exactly one call whose profile is `example.test` and whose region is `eu-west-1`, and by `devctl space status crm.sbx1` leaving it with no call.

- R-V6FS-65I6: `space list` and `space stop` MUST pass no `seam.Cmd` to `deps.Exec` other than the single one `checkout.ReadRootFile` passes, and none to `deps.Stream`.

- R-V7NO-JX8V: `space status`, `space stop`, `space start`, and `space destroy` MUST each call `cloud.LookupSpace(ctx, clients.EC2, root, space.Domain)` before any call to `deps.Exec` other than `checkout.ReadRootFile`'s, any write to stdout, and any call to a method of `clients` other than those `cloud.Connect`, `cloud.LookupSpace`, and — for `destroy` — `clients.Route53.Zone` make; `status`, `stop`, and `start` MUST return its error unchanged, and `destroy` MUST treat a `*cloud.NoSpaceError` from it as the space having no instance and MUST NOT return it; verified at least through `cli.Run` by `devctl space stop gone`, `devctl space start gone`, and `devctl space status gone` each writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with empty stdout and exit 1.

- R-XO8Q-YVRD: `space list` MUST call `clients.Route53.Zone(ctx, root)` and return its error unchanged before calling `cloud.Spaces(ctx, clients.EC2, root)`, MUST call `FindApex(ctx, clients.Route53, clients.EC2, zone.ID, root)` exactly once when `Spaces` returned at least one space and not at all when it returned none, and MUST call no other method of `clients` beyond the `STS.CallerAccountID` call `cloud.Connect` makes; verified at least through `cli.Run` by a `Zone` that returns a `*cloud.NotFoundError` giving the single stderr line `devctl: no hosted zone 'ikigenba.dev'`, empty stdout, and exit 1.

- R-V8VK-XOZK: `space list` MUST write one line to stdout for each `cloud.Space` that `cloud.Spaces` returns, in the order it returns them, consisting of that `Space`'s `Domain`, one space, its `State`, one space, its `Address` or `-` when `Address` is empty, one space, and `apex` when the `ApexRecord` `FindApex` returned has a `Holder` equal to that `Domain` or `-` otherwise; MUST write nothing to stdout when `Spaces` returns none; and MUST exit 0; verified at least by reproducing the three lines `new.ikigenba.dev running 18.220.10.5 -`, `sbx1.ikigenba.dev running 18.118.7.42 apex`, and `sbx2.ikigenba.dev stopped - -` for a zone whose `A` record `ikigenba.dev` holds `18.118.7.42`, the `IP` of the address whose `Space` is `sbx1.ikigenba.dev`; by every line ending in ` -` both when that record is absent and when it holds an `IP` no address carries; and by reproducing empty output.

- R-VBBD-P8GY: `space status` MUST return a `*NotRunningError` whose `Domain` is `space.Domain` and whose `State` is the space's `State`, write nothing to stdout, and pass no `seam.Cmd` to `deps.Exec` other than `checkout.ReadRootFile`'s, when that state is not `cloud.StateRunning`, verified at least by reproducing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` with exit 1 for both `space status sbx2` and `space status sbx2.ikigenba.dev`.

- R-S9WX-LSSA: `space status` MUST obtain its answer from exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the space's `Address` and whose `Deps` is `deps`, with an empty `step` and exactly the arguments `opsctl` and `status`, and MUST write that call's `Output.Stdout` to stdout byte for byte and nothing else, neither parsing, reordering, reformatting, nor validating it; verified at least with a fake `Deps.Exec` that exits 0 with arbitrary multi-line standard output, with one whose standard output has no trailing newline, and with one whose standard output is empty, each reproduced on stdout unchanged.

- R-VCJA-307N: `space stop` MUST write the step `instance` first, whose detail MUST be `already stopped`, with `StopInstance` not called, when the space's `State` is `cloud.StateStopped`, and otherwise MUST call `clients.EC2.StopInstance` with the space's `ID` exactly once, then `WaitState` for `cloud.StateStopped`, and report the space's `ID` followed by ` stopped`; verified at least by reproducing `instance: ok (i-0a1b2c3d4e5f60718 stopped)` and `instance: ok (already stopped)`.

- R-VDR6-GRYC: `space start` MUST write the step `instance` first, MUST call `clients.EC2.StartInstance` with the space's `ID` exactly once and then `WaitState` for `cloud.StateRunning` when the space's `State` is not `cloud.StateRunning` and MUST call `StartInstance` not at all when it is, and MUST report the space's `ID`, ` running, `, and the running instance's `Address`; verified at least by reproducing `instance: ok (i-0a1b2c3d4e5f60718 running, 18.220.10.5)`.

- R-8XNM-MR8T: `space stop` MUST print only its instance step and MUST NOT change DNS records, release or disassociate the Elastic IP, modify storage, resource tags, secrets or IAM resources, or execute any host command.

- R-DP6D-0RTU: `space start` MUST write the step `host` second, with the detail `status checks passed`, after `WaitChecks` for the space's `ID` returned a nil error.

- R-B1XP-NI4K: `space start` MUST write the step `certificate` third, after exactly one successful call to `(host.Host).Wait` and then exactly one successful call to `(host.Host).Sudo` with the step `certificate` and exactly the arguments `certbot` and `renew`, both on a `host.Host` whose `Address` is the running instance's `Address` and whose `Deps` is `deps`; its detail MUST be `certbot renew`, verified by reproducing `certificate: ok (certbot renew)`. A zero exit is the whole of the step's success, whether or not certbot renewed anything; a failed call MUST produce no certificate step line and MUST return the host error unchanged.

- R-S6SW-5NHI: `space start` MUST perform only `instance`, `host`, and `certificate` steps in that order, followed on success by one stdout line of `space.Domain`, one space, and the running instance's `Address`; it MUST NOT change records or the Elastic IP. Failure MUST preserve completed steps and omit the final address line. A running instance MUST still receive status and renewal checks on a repeated start; verified at least by `devctl space start staging` reproducing the final line `staging.ikigenba.dev 18.220.10.5`.

- R-VEZ2-UJP1: `space destroy` MUST write, as its only stdout, the step lines `apex` — only when the space holds the apex — then `retire`, `instance`, `address`, `records`, `secrets`, `backups`, and `role`, in that order, MUST write no `account` or `domain` line, MUST stop at the first failure without undoing completed steps and without writing a line for the failed step, and MUST exit 0 only after every step has completed; verified at least by reproducing, in order, `apex: ok (ikigenba.dev record deleted)`, `retire: ok (opsctl retire)`, `instance: ok (i-0c9e94542d98846a8 terminated)`, `address: ok (elastic ip 18.118.7.42 released)`, `records: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)`, `secrets: ok (kept)`, `backups: ok (kept)`, and `role: ok (sbx1.ikigenba.dev deleted)` with exit 0, and by a `ChangeRecords` failure in the `records` step of `space destroy sbx1 --no-backup` leaving exactly `retire: skipped (--no-backup)`, `instance: ok (i-0c9e94542d98846a8 terminated)`, and `address: ok (elastic ip 18.118.7.42 released)` on stdout with the single stderr line `devctl: route53 ChangeResourceRecordSets: Throttling` and exit 1.

- R-VHEV-M36F: `space destroy` MUST call `clients.Route53.Zone(ctx, root)` and return its error unchanged, and MUST return a `*RetireStateError` whose `ID` and `State` are the space's `ID` and `State` and whose `Label` is `space.Label` when `--no-backup` is not given and `LookupSpace` found an instance whose `State` is not `cloud.StateRunning`, both before any write to stdout, any call to `deps.Exec` other than `checkout.ReadRootFile`'s, and any method of `clients` that creates, changes, terminates, releases, or deletes anything, so that a refused destroy changes nothing; verified at least by reproducing the stderr lines `devctl: retire: instance i-0a1b2c3d4e5f60718 is stopped`, an empty line, and `run 'devctl space start staging' first, or pass --no-backup` with empty stdout and exit 1 for both `space destroy staging` and `space destroy staging.ikigenba.dev`, including when the zone's `A` record at `root` points at that space's address.

- R-VIMR-ZUX4: The `apex` step of `space destroy` MUST call `FindApex(ctx, clients.Route53, clients.EC2, zone.ID, root)` after the checks R-VHEV-M36F states and before the `retire` step, and exactly when the returned `Holder` equals `space.Domain` MUST call `clients.Route53.ChangeRecords` for `zone.ID` exactly once with one `cloud.RecordChange` whose `Action` is `cloud.ChangeDelete` and whose `Record` is the `Record` `FindApex` returned, then write the step `apex` with the detail `root` followed by ` record deleted`; when `Holder` differs it MUST write no `apex` line and delete no record; and destroy MUST run no host command about the apex; verified at least by reproducing `apex: ok (ikigenba.dev record deleted)` as the first stdout line both without and with `--no-backup`, and by a destroy of a space that does not hold the apex writing no line beginning `apex`.

- R-DDFT-AXS8: When `LookupSpace` found an instance and `--no-backup` is not given, `space destroy` MUST run the `retire` step as exactly one call to `(host.Host).Sudo` with the step `retire` and the arguments `opsctl` and `retire`, on a `host.Host` whose `Address` is the space's `Address` and whose `Deps` is `deps`, before any method of `clients` that terminates, releases, deletes, or changes anything other than the `apex` step's record delete, and MUST return that call's error unchanged.

- R-JCCP-CNWX: After `sudo opsctl retire` exits 0, destroy MUST report `retire: ok (opsctl retire)`; a zero exit is the whole of the step's success, and devctl MUST NOT parse opsctl's output or read the backup bucket to describe what was backed up. Verified by reproducing `retire: ok (opsctl retire)`.

- R-VL2K-REEI: The `retire` step of `space destroy` MUST write exactly the line `retire: ok (already gone)` when `LookupSpace` found no instance, whether or not `--no-backup` is given, and exactly the line `retire: skipped (--no-backup)` when it found one and `--no-backup` is given, whatever that instance's `State`, in each case passing no `seam.Cmd` to `deps.Exec` other than `checkout.ReadRootFile`'s; verified at least by reproducing both lines and by `space destroy staging --no-backup` on a `stopped` instance continuing to `instance: ok (i-0a1b2c3d4e5f60718 terminated)`.

- R-VMAH-5657: The `instance` step of `space destroy` MUST report `already gone`, with no EC2 method other than those `LookupSpace` and `FindApex` make called before it, when `LookupSpace` returned a `*cloud.NoSpaceError`, and otherwise MUST call `clients.EC2.TerminateInstance` with the space's `ID` exactly once, then `WaitState` for `cloud.StateTerminated`, and report the space's `ID` followed by ` terminated`; verified at least by reproducing `instance: ok (i-0a1b2c3d4e5f60718 terminated)` and `instance: ok (already gone)`.

- R-UL0J-793W: The `address` step of `space destroy` MUST report `elastic ip `, the `cloud.Address`'s `IP`, and ` released` after calling `ReleaseElasticIP` for it when `ElasticIP` reports an Elastic IP for the domain; MUST report `no elastic ip` when it reports none and the `instance` step terminated an instance; and MUST report `already gone` when it reports none and the `instance` step reported `already gone`; verified at least by reproducing `address: ok (elastic ip 18.220.10.5 released)`, `address: ok (no elastic ip)`, and `address: ok (already gone)`.

- R-VNID-IXVW: The `records` step of `space destroy` MUST call `DeleteRecords(ctx, clients.Route53, zone, space.Domain)` exactly once and MUST report `deleted ` followed by the names it returned joined by `, ` when it returned at least one, and `already gone` when it returned none; verified at least by reproducing `records: ok (deleted staging.ikigenba.dev, *.staging.ikigenba.dev)`, `records: ok (deleted staging.ikigenba.dev)` for a zone that holds only the bare record, and `records: ok (already gone)`.

- R-S80S-JF87: The `secrets` step of `space destroy` MUST report `kept`, with no SSM method called, when `--delete-secrets` is not given, and otherwise MUST call `clients.SSM.ListParameters` with `secrets.Prefix(space.Domain)` and `clients.SSM.DeleteParameter` exactly once for each `cloud.Parameter` it returned, and report that count followed by ` parameters deleted` when the count is not 0 and `already gone` when it is 0; verified at least by reproducing `secrets: ok (kept)` both for a space whose instance was terminated and for one that was already gone, `secrets: ok (3 parameters deleted)`, and `secrets: ok (already gone)`.

- R-S98O-X6YW: The `backups` step of `space destroy` MUST report `kept`, with no S3 method called, when `--delete-backups` is not given, and otherwise MUST call `clients.S3.ListObjects` with `root` as the bucket and `BackupPrefix(space.Label)` as the prefix, then `clients.S3.DeleteObjects` with `root` and every key it returned exactly once when it returned at least one and not at all when it returned none, and report the number of those keys followed by ` objects deleted` when that number is not 0 and `already gone` when it is 0; verified at least by reproducing `backups: ok (kept)`, `backups: ok (12 objects deleted)`, and `backups: ok (already gone)`.

- R-VR62-O93Z: The `role` step of `space destroy` MUST call `DeleteRole(ctx, clients.IAM, space.Domain)` and report `RoleName(space.Domain)` followed by ` deleted` when it returned true and `already gone` when it returned false; verified at least by reproducing `role: ok (staging.ikigenba.dev deleted)` and `role: ok (already gone)`.

- R-8YVJ-0IZI: `space list` and `space status` MUST perform no cloud mutation and MUST leave local checkout files unchanged; status MUST execute no host command other than its single `sudo opsctl status` invocation. Tests MUST use cloud fakes that fail on mutation and verify the host command count for both populated and empty results.
