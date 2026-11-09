# D06-space

Spaces are discovered from cloud tags and have an Elastic IP throughout their
lifetime. The platform is one root domain in one account: every `space`
subcommand that touches the cloud reads the root and its region from the
checkout's root file (D04), parses its `<space>` operand with `spaceref`
(D04), opens one `cloud.Session` (D03) whose first call is STS, and finds the
space through D03's lookups: `cloud.LookupInstance` for stop, start, and
status, which need a host, and `cloud.LookupSpace` for destroy, which acts on
whatever the label carries. Nothing is configured on the developer's
machine and no `--account` exists. Output names the full space domain even
when the label was typed; the one place a label appears is the advice line
that tells the developer which command to run next.

A label carries at most one instance and at most one Elastic IP, and a space
exists while either carries it (D03): `cloud.LookupSpace` answers the one
`Space`, whose `ElasticIP` is the space's address for its whole life. `space
create` (D07) allocates it and writes the space's two `A` records, `space
destroy` releases it and deletes them, and `space replace` (D18) assigns it to
the new instance; no other command allocates, releases, or rewrites them.
`space list` prints one line per space, a space left with only its address
among them with `-` for its state, and marks the holder of the apex: the space whose Elastic IP the
root's `A` record points at, whether or not that address is associated with
an instance. The holder is read from the zone and the address tags, never from
a host, by `FindApex`, which `space destroy` and D14's `apex` command share; a
root record pointing at an address no space carries marks no holder. Stop and
start preserve records and the address; start runs the host's renewal check
in the foreground. A space that has only its address has no host, so stop,
start, and status refuse it with D03's `NoInstanceError` before they touch the
instance or the host; replace or destroy is the way on. A label carrying two
Elastic IPs is refused by every lookup with D03's `DuplicateAddressError`,
destroy's included, before anything changes. Destroy deletes the root's record first when
this space holds it, has the host take its final backup with `opsctl retire`,
then removes the instance, the address, the space's two `A` records, and the
role, resumably. The `instance` and `address` steps act on the one instance
and the one address the label carries; a space with no instance, as a replace
that could not launch leaves it, has no host to back up, so `retire` and
`instance` say `already gone`, no host is reached, and its address is
released as any other. The `address` step looks the space up again once the
instance is terminated, because terminating it disassociates the address.
Secrets and backups are kept unless `--delete-secrets` or `--delete-backups`
says otherwise; each option governs only its own step. A stopped host cannot
take a backup, so destroy without `--no-backup` refuses a non-running instance
before it prints or changes anything, the apex record included.

What devctl makes for a space is named after the space: the role and its
instance profile are the space domain, the records are `<space domain>` and
`*.<space domain>`, and the backup prefix in the bucket named after the root
is the space's label followed by `/`. Under that prefix opsctl writes the
backups, and its snapshots under `snapshots/`; earlier per-app deploys left
files under `deploy/`; and seed (D15) copies snapshots into `seed/`. `SnapshotPrefix` and
`SeedPrefix` name those two, beside `BackupPrefix`. A host backup is
`host/<ts>.tar.zst` under it, `<ts>` an RFC3339 UTC time to the nanosecond as
opsctl writes it, beside a `<ts>.accounts.tar.zst` that is not one;
`HostBackup` picks the newest by that time, folding D03's paged listing one
page at a time into the newest seen so far, so a caller holds one page and one
key however many host backups the bucket keeps, and `space create` (D07) and
`space replace` (D18) choose the same backup. `--delete-backups` deletes
every object under the whole prefix, snapshots and seed copies included, so a
destroyed space leaves nothing a later seed could take; it deletes each page of
the listing as it arrives, one `DeleteObjects` of at most `ObjectPageSize` keys,
holding one page and a count, never every key at once. The role's one inline policy is built
here too: `PolicyDocument` derives it from the root, the zone id, the space,
and whether the space holds the apex, so `space create` (D07) and `apex` (D14)
regenerate the same document and can never disagree about what a space's role
may do. The policy also lets the host's SSM agent register and carry a
Session Manager session. The wait helpers poll a cloud fact on
the shared interval until it holds: `WaitState` and `WaitChecks` watch an
instance, `WaitLaunchReady` watches a launch spec become acceptable, and
`WaitInsync` watches a Route 53 change, so a caller can hold a step open until
the resource it just made is genuinely usable. An instance not yet visible to
EC2 is one that has not reached the state yet, not a failure: `WaitState`
keeps polling through the zero `Instance` D03's `DescribeInstance` returns for
an id EC2 does not know yet. `WaitAssociate` is the same wait for an Elastic IP:
a new allocation id can briefly be unknown to `AssociateAddress`, which EC2
reports as the error code `InvalidAllocationID.NotFound`, so `WaitAssociate`
retries the association while that is the answer and relays any other error
at once. `WaitResolves` watches the developer's own resolver rather than the
cloud: create (D07) and replace (D18) hold their `resolve` step until
`Deps.LookupHost`, the system resolver ssh itself uses, answers the space
domain with exactly the space's address, so the first connection by name
reaches the space's host. A lookup error, an empty answer, another address,
or several addresses are all not yet; running out of attempts is a
`ResolveError`, the DNS failure that names the domain and the address, so the
developer looks at name resolution rather than at ssh. `WaitResolves` runs
nothing itself; that no ssh follows its failure is its caller's, which returns
the error at once. Each attempt holds one answer, which the resolver bounds by
the records of one name, and drops it before the next. Every cloud helper
takes the one client interface it needs, as D03's `Spaces` and D05's `Push`
do; no session or checkout type crosses into a helper.

Host execution lives in `internal/host`: `Host{Domain, Deps, KnownHosts}` runs a command
over ssh on `ec2-user@<Domain>`, the space's domain, with every argument
preserved as one literal word. devctl reaches a host only by that name: it
passes no key, no `-i`, no `HostName`, and no address, and the developer's ssh
configuration for the root domain supplies the key, as it does for an
operator. `Sudo` returns the
captured `Output{Stdout, Stderr}` so a caller can use stdout (`opsctl status`
relays it; D10's `GetKey` reads it), and a non-zero exit is a `*CommandError`
whose `Detail()` quotes what the remote program wrote to stdout and then what
it wrote to stderr, each line prefixed `> `. A command's product is on its
stdout and its diagnostic on its stderr (the repository's command-line
convention), so a failing opsctl's report comes first and its reason after it;
neither is dropped because the other is present. devctl never asserts what
those bytes say. D07, D09, D10, D13, D14, D16, D17 and D18 issue every
host command through this one shape. Every root command runs as a transient
system unit, `sudo systemd-run --quiet --wait --pipe --collect` before it, because
opsctl's hardening caps the memory of every user slice and a command started
from an ssh session stays in that slice even through sudo; the unit runs
outside it, waits, connects the streams and passes back the exit status.
`Copy` is the one way a file reaches a
host from the developer's machine: `scp` with the same options and target as
`ssh`, failing with the same `*CommandError`, so a failed copy is reported
the way a failed remote command is. ssh and scp accept a host key they have
not seen and record it, under the name, in the developer's own known hosts; a
`Host` whose `KnownHosts` names a file uses that file instead, which is how
create (D07) and replace (D18) keep the key a new host has before its host
restore out of the developer's file; after a host restore, which D10's `Restore` issues for
both, the host is reached through the known hosts `Restore` is given. The
path is one `-o` value, so it holds no whitespace; an empty
`KnownHosts` leaves the command exactly as it was. `space status` relays `opsctl status`
whole, so its columns, the short sha of the commit each app runs and the
release's label among them, are opsctl's and never parsed here.

## REQUIREMENTS

- R-RJ9Z-1OB0: Package `internal/space` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-SR5Y-JN2E: Package `internal/space` MUST export `Step(w io.Writer, name, detail string)`, which MUST write `<name>: ok (<detail>)` followed by one newline to `w` when `detail` is not empty and `<name>: ok` followed by one newline when `detail` is empty, verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 terminated)` and `init: ok`.

- R-U273-JQ19: Package `internal/space` MUST export `RoleName(domain string) string`, returning `domain` unchanged, so that a space's IAM role and its instance profile are both named exactly the space domain; `PolicyName = "space"`; `BackupPrefix(label string) string`, returning `label` followed by `/`; `RecordNames(domain string) []string`, returning exactly `domain` and `*.` followed by `domain`, in that order; and `RecordTTL = 60`; verified at least by `RoleName("sbx1.ikigenba.dev")` being `sbx1.ikigenba.dev`, `BackupPrefix("sbx1")` being `sbx1/`, and `RecordNames("sbx1.ikigenba.dev")` being `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev`.

- R-SCI4-HK5P: Package `internal/space` MUST export `SnapshotPrefix(label string) string`, returning `BackupPrefix(label)` followed by `snapshots/`, and `SeedPrefix(label string) string`, returning `BackupPrefix(label)` followed by `seed/`; verified at least by `SnapshotPrefix("sbx1")` being `sbx1/snapshots/` and `SeedPrefix("sbx2")` being `sbx2/seed/`.

- R-U2BO-LPNG: Package `internal/space` MUST export `HostBackup(newest cloud.Object, found bool, page []cloud.Object, prefix string) (cloud.Object, bool)`, which folds one page of a listing into the newest host backup seen so far: `newest` and `found` are what the call for the page before returned, the zero `cloud.Object` and false before the first page.

- R-U3JK-ZHE5: `space.HostBackup` MUST treat as a host backup exactly each `cloud.Object` in `page` whose `Key` is `prefix` followed by `<ts>.tar.zst`, where `<ts>` is an RFC3339 UTC timestamp `YYYY-MM-DDThh:mm:ssZ`, optionally with a fractional second of one to nine digits before the `Z`, that parses as a valid time, and MUST ignore every other object, and MUST return `newest` and `found` unchanged when `page` holds no host backup; verified at least by, for `prefix` `us/host/` and the zero `newest` with `found` false, a page holding only `us/host/2026-11-01T00:00:00Z.tar.zst` returning that object, its `Size` and `Modified` unchanged, and true; by a page holding `us/host/2026-11-08T16:12:01.865067548Z.tar.zst` beside `us/host/2026-11-08T16:12:01.865067548Z.accounts.tar.zst`, `us/host/2026-11-08T17:43:26.222933531Z.accounts.tar.zst`, `us/host/old/2026-11-09T00:00:00Z.tar.zst`, `us/host/latest.tar.zst`, `us/host/2026-11-09T00:00:00+00:00.tar.zst`, `us/host/2026-02-30T00:00:00Z.tar.zst`, `us/host/2026-11-09T00:00:00Z.tar.gz`, and `them/host/2026-11-09T00:00:00Z.tar.zst`, each of these others given a later `Modified`, returning `us/host/2026-11-08T16:12:01.865067548Z.tar.zst` and true; by a nil page and a page holding only the eight ignored objects above each returning the zero `cloud.Object` and false; and by that page of eight ignored objects with `newest` `us/host/2026-11-01T00:00:00Z.tar.zst` and `found` true returning that object and true.

- R-U4RH-D94U: When `found` is true, `space.HostBackup` MUST take `newest` as a host backup whose `<ts>` is its `Key` with `prefix` removed from its front and `.tar.zst` from its end, and of `newest` and every host backup in `page` MUST return the one whose parsed `<ts>` is the latest instant, whatever the objects' `Modified` and order in `page`, and of two that parse to the same instant the one with the greater `Key`, with true; verified at least by, for `prefix` `us/host/`, each pair below given as one page in both orders with the zero `newest` and `found` false, and given as `newest` with `found` true and a page holding only the other, in both assignments: `us/host/2026-11-08T16:12:01.5Z.tar.zst` winning over `us/host/2026-11-08T16:12:01Z.tar.zst` whose `Modified` is later, `us/host/2026-11-07T17:09:48.74850415Z.tar.zst` winning over `us/host/2026-11-07T17:09:48.7485041Z.tar.zst` whose `Modified` is later, `us/host/2026-11-08T17:43:26.222933531Z.tar.zst` winning over `us/host/2026-11-08T16:12:01.865067548Z.tar.zst` whose `Modified` is later, and `us/host/2026-11-08T16:12:01.5Z.tar.zst` winning over `us/host/2026-11-08T16:12:01.50Z.tar.zst`.

- R-U5ZD-R0VJ: `space.HostBackup` MUST hold no state between calls: its result MUST depend only on `newest`, `found`, `page`, and `prefix`, so that folding a listing page by page from the zero `cloud.Object` and false gives the same result however the listing is split into pages; verified at least by the nine objects of the R-U3JK-ZHE5 page beside `us/host/2026-11-08T17:43:26.222933531Z.tar.zst`, folded as one page, as ten pages of one in both orders, and as pages of three, three, and four, each returning `us/host/2026-11-08T17:43:26.222933531Z.tar.zst` and true, and by a call with the zero `newest`, `found` false, and a page holding only `us/host/2026-11-01T00:00:00Z.tar.zst`, made right after a fold that returned a later backup, returning `us/host/2026-11-01T00:00:00Z.tar.zst`.

- R-U3EZ-XHRY: Package `internal/space` MUST export `WaitState(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, id string, state cloud.InstanceState) (cloud.Instance, error)`, `WaitChecks(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, id string) error`, `WaitLaunchReady(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, spec cloud.LaunchSpec) error`, `WaitInsync(ctx context.Context, deps seam.Deps, route53 cloud.Route53, changeID string) error`, `PollInterval = 5 * time.Second`, and `PollAttempts = 60`.

- R-BW20-J3IW: Package `internal/space` MUST export `ReleaseElasticIP(ctx context.Context, ec2 cloud.EC2, addr cloud.Address) error`.

- R-U5US-P19C: Package `internal/space` MUST export `PutRecords(ctx context.Context, deps seam.Deps, route53 cloud.Route53, zone cloud.Zone, domain, address string) error` and `DeleteRecords(ctx context.Context, route53 cloud.Route53, zone cloud.Zone, domain string) ([]string, error)`.

- R-U72P-2T01: Package `internal/space` MUST export `DeleteRole(ctx context.Context, iam cloud.IAM, domain string) (bool, error)`.

- R-VPQM-VSH2: Package `internal/space` MUST export `PolicyDocument(root, zoneID string, sp spaceref.Space, apex bool) string`, returning an IAM policy document that is one JSON object whose `Version` is `2012-10-17`.

- R-SUCG-OHQC: `PolicyDocument` MUST return identical text for identical arguments.

- R-U4WP-JND0: The document `PolicyDocument` returns MUST grant `ssm:GetParameter` on exactly the resource `arn:aws:ssm:*:*:parameter/<sp.Domain>/*` and `ssm:UpdateInstanceInformation` on exactly the resource `arn:aws:ec2:*:*:instance/*`, and MUST grant no other `ssm:` action, so that the host reads each app's secrets object that create and `secrets push` write and nothing outside the space's prefix, and its SSM agent registers the instance for Session Manager; verified at least by parsing the JSON for the space `sbx1` under `ikigenba.dev` and finding an `Allow` statement for `ssm:GetParameter` whose only resource is `arn:aws:ssm:*:*:parameter/sbx1.ikigenba.dev/*`, an `Allow` statement for `ssm:UpdateInstanceInformation` whose only resource is `arn:aws:ec2:*:*:instance/*`, no action beginning `ssm:Put`, `ssm:Delete`, or `ssm:*`, no `ssm:GetParameters`, and no resource naming `parameter/ikigenba/`.

- R-U64L-XF3P: The document `PolicyDocument` returns MUST grant `ssmmessages:CreateControlChannel`, `ssmmessages:CreateDataChannel`, `ssmmessages:OpenControlChannel`, `ssmmessages:OpenDataChannel`, `ec2messages:AcknowledgeMessage`, `ec2messages:DeleteMessage`, `ec2messages:FailMessage`, `ec2messages:GetEndpoint`, `ec2messages:GetMessages`, and `ec2messages:SendReply` on exactly the resource `*`, and MUST grant no other `ssmmessages:` or `ec2messages:` action, so that the host's SSM agent carries a Session Manager session and an operator reaches the host without sshd; verified at least by parsing the JSON for `sbx1` under `ikigenba.dev` with each value of `apex` and finding each of the ten actions in an `Allow` statement whose only resource is `*`, exactly ten distinct actions beginning `ssmmessages:` or `ec2messages:`, and no action `ssmmessages:*` or `ec2messages:*`.

- R-2RDF-YMG9: The document `PolicyDocument` returns MUST grant `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, and `s3:AbortMultipartUpload` on exactly `arn:aws:s3:::<root>/<sp.Label>/*`, and `s3:ListBucket` on `arn:aws:s3:::<root>` only under a `StringLike` condition on `s3:prefix` whose values are exactly `<sp.Label>/` and `<sp.Label>/*`, and MUST grant no other `s3:` action and no `s3:` action on any other resource, so that the host reads, writes, and deletes under `<sp.Label>/`, aborts a multipart upload it started there, and gains no access to another space's prefix; verified at least by parsing the JSON for `sbx1` under `ikigenba.dev` and finding the object resource `arn:aws:s3:::ikigenba.dev/sbx1/*` carrying `s3:DeleteObject` and `s3:AbortMultipartUpload`, the bucket resource `arn:aws:s3:::ikigenba.dev` with the prefixes `sbx1/` and `sbx1/*` and neither `s3:DeleteObject` nor `s3:AbortMultipartUpload`, and no resource or prefix naming `sbx2/`.

- R-VS6F-NBYG: The document `PolicyDocument` returns MUST grant exactly three Route 53 permissions and no other `route53:` action: `route53:ListResourceRecordSets` on `arn:aws:route53:::hostedzone/<zoneID>`, `route53:GetChange` on `arn:aws:route53:::change/*`, and `route53:ChangeResourceRecordSets` on `arn:aws:route53:::hostedzone/<zoneID>` under a `ForAllValues:StringEquals` condition requiring `route53:ChangeResourceRecordSetsRecordTypes` to equal `TXT` and a `ForAllValues:StringLike` condition whose `route53:ChangeResourceRecordSetsNormalizedRecordNames` values are exactly `<sp.Domain>` and `*.<sp.Domain>` when `apex` is false, and exactly those two and `_acme-challenge.<root>` when `apex` is true; verified at least by parsing the JSON for `sbx1` under `ikigenba.dev` with the zone id `Z09565073GHK8BYWQ1A78` and each value of `apex`, checking that the `route53:ChangeResourceRecordSetsRecordTypes` values are exactly `TXT`, that the `route53:ChangeResourceRecordSetsNormalizedRecordNames` values are exactly `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev` when `apex` is false, and those two and `_acme-challenge.ikigenba.dev` when `apex` is true, and that `route53:ListHostedZones` is absent.

- R-U8AL-GKQQ: Package `internal/space` MUST export an `ApexRecord` struct whose fields are exactly `Record cloud.Record`, `Found bool`, and `Holder string`, and `FindApex(ctx context.Context, route53 cloud.Route53, ec2 cloud.EC2, zoneID, root string) (ApexRecord, error)`.

- R-SZP9-8199: Package `internal/space` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage`, and `ExitCode() int`, returning 2.

- R-XQOJ-QF8R: Package `internal/space` MUST export a `NotRunningError` struct whose fields are exactly `Domain string` and `State cloud.InstanceState`, with the method `Error() string`, returning `'<Domain>' is <State>`, verified at least by reproducing `'sbx2.ikigenba.dev' is stopped`.

- R-T251-ZKQN: Package `internal/space` MUST export a `WaitError` struct whose fields are exactly `Subject string` and `Want string`, with the method `Error() string`, returning `timed out waiting for <Subject> to <Want>`, verified at least by reproducing `timed out waiting for i-0c9e94542d98846a8 to be running`.

- R-U9IH-UCHF: Package `internal/space` MUST export a `RetireStateError` struct whose fields are exactly `ID string`, `State cloud.InstanceState`, and `Label string`, with the methods `Error() string`, returning `retire: instance <ID> is <State>`, `ExitCode() int`, returning 1, and `Detail() string`, returning `run 'devctl space start <Label>' first, or pass --no-backup`, verified at least by reproducing `retire: instance i-0a1b2c3d4e5f60718 is stopped` and `run 'devctl space start staging' first, or pass --no-backup`.

- R-C5T7-L9GG: Package `internal/host` MUST export `User = "ec2-user"`, a `Host` struct whose fields are exactly `Domain string`, `Deps seam.Deps`, and `KnownHosts string`, and the method `Target() string` on `Host`, returning `User`, `@`, and `Domain` joined, verified at least by the `Target()` of a `Host` whose `Domain` is `sbx1.ikigenba.dev` being `ec2-user@sbx1.ikigenba.dev`.

- R-T5SR-4VYQ: Package `internal/host` MUST export `ProbeInterval = 5 * time.Second` and `ProbeAttempts = 12`.

- R-C713-Z175: Package `internal/host` MUST export an `UnreachableError` struct whose only field is `Domain string`, with the method `Error() string`, returning `ssh ` followed by `ec2-user@<Domain>` and `: connection timed out`, verified at least by reproducing `ssh ec2-user@sbx1.ikigenba.dev: connection timed out`.

- R-C890-CSXU: `(host.Host).Wait` MUST pass to `Deps.Exec` the `seam.Cmd` that `Run` passes for the single argument `true`, MUST return a nil error as soon as one of those processes exits 0, MUST wait on `Deps.After(ProbeInterval)` between consecutive ones, MUST pass at most `ProbeAttempts` of them, and MUST return an `*UnreachableError` whose `Domain` is `Domain` when that many have not exited 0; verified with a fake `Deps.After` that fires immediately.

- R-UAQE-8484: `space.WaitState` MUST call `ec2.DescribeInstance` with `id` and return that `cloud.Instance` and a nil error as soon as its `State` equals `state` and — when `state` is `cloud.StateRunning` — its `Address` is not empty, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `be ` followed by `state` when that many calls have not satisfied it; verified with a fake `Deps.After` that fires immediately.

- R-3N3S-I439: `space.WaitState` MUST treat a zero `cloud.Instance` returned by `ec2.DescribeInstance` with a nil error as an instance that has not yet reached `state`, waiting and calling again within the same `PollAttempts` bound; verified with a fake `Deps.After` that fires immediately, at least by a fake `EC2` whose `DescribeInstance` returns the zero `cloud.Instance` on its first two calls and then a `running` instance with an `Address` making `WaitState` for `cloud.StateRunning` return that instance and a nil error after exactly three calls, and by one that returns the zero `cloud.Instance` on every call making it return a `*WaitError` whose `Subject` is `id` and whose `Want` is `be running` after exactly `PollAttempts` calls.

- R-TRRP-P2TM: Package `internal/space` MUST export `WaitAssociate(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, allocationID, instanceID string) error`.

- R-RUIM-IAS8: `space.WaitAssociate` MUST call `ec2.AssociateAddress` with `allocationID` and `instanceID` and return a nil error as soon as it returns a nil error; while it returns an error that `errors.As` matches to a `*cloud.Error` whose `Code` is `InvalidAllocationID.NotFound` it MUST wait on `deps.After(PollInterval)` and call again, making at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `allocationID` and whose `Want` is `become associable` when that many calls have all returned that error; any other non-nil error MUST be returned unchanged at once with no further call; verified with a fake `Deps.After` that fires immediately, at least by a fake `EC2` whose `AssociateAddress` fails with that code on its first two calls and then succeeds giving a nil error after exactly three calls, by one that always fails with that code giving that `*WaitError` after exactly `PollAttempts` calls, and by one whose first call fails with a `*cloud.Error` whose `Code` is `Gateway.NotAttached` giving that error unchanged after exactly one call.

- R-UBYA-LVYT: `space.WaitChecks` MUST call `ec2.InstanceChecksPassed` with `id` and return a nil error as soon as it reports true, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `pass its status checks` when that many calls have not reported true.

- R-UD66-ZNPI: `space.WaitLaunchReady` MUST call `ec2.LaunchReady` with `spec` and return a nil error as soon as it reports true, MUST return its error unchanged as soon as it returns a non-nil one, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `spec.InstanceProfile` and whose `Want` is `become usable for launch` when that many calls have not reported true.

- R-UFLZ-R76W: `space.WaitInsync` MUST call `route53.ChangeStatus` with `changeID` and return a nil error as soon as it reports `cloud.ChangeInsync`, MUST return its error unchanged as soon as it returns a non-nil one, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `changeID` and whose `Want` is `reach INSYNC` when that many calls have not reported it; verified with a fake `Deps.After` that fires immediately.

- R-BX9W-WV9L: Package `internal/space` MUST export `WaitResolves(ctx context.Context, deps seam.Deps, domain, address string) error`.

- R-BYHT-AN0A: Package `internal/space` MUST export a `ResolveError` struct whose fields are exactly `Domain string` and `Address string`, with the methods `Error() string`, returning `resolve: <Domain> does not resolve to <Address>`, and `ExitCode() int`, returning 1, verified at least by reproducing `resolve: sbx1.ikigenba.dev does not resolve to 18.118.7.42`.

- R-BZPP-OEQZ: `space.WaitResolves` MUST call `deps.LookupHost` with `domain` and return a nil error as soon as a call returns a nil error and an answer of exactly one element equal to `address`, MUST wait on `deps.After(PollInterval)` between consecutive calls and not before the first, MUST make at most `PollAttempts` calls, and MUST return a `*ResolveError` whose `Domain` is `domain` and whose `Address` is `address` when that many calls have not given that answer; verified with a fake `Deps.After` that fires immediately and a fake `Deps.LookupHost`, for `domain` `sbx1.ikigenba.dev` and `address` `18.118.7.42`, at least by one answering `18.118.7.42` from its first call giving a nil error after exactly one call and no `Deps.After` call, by one giving an empty answer on its first two calls and then `18.118.7.42` giving a nil error after exactly three calls, and by one never answering `18.118.7.42` giving that `*ResolveError` after exactly `PollAttempts` calls.

- R-C0XM-26HO: `space.WaitResolves` MUST treat each of these results of a `deps.LookupHost` call as a name that does not resolve yet, waiting and calling again within the same `PollAttempts` bound, and MUST never return the lookup's error: a non-nil error, whatever answer accompanies it; an empty answer; an answer of one address other than `address`; and an answer of two or more addresses, `address` among them or not; verified with a fake `Deps.After` that fires immediately, for `domain` `sbx1.ikigenba.dev` and `address` `18.118.7.42`, at least by a first call failing with a `*net.DNSError` whose `IsNotFound` is true, by a first answer `3.19.79.227`, and by a first answer `18.118.7.42` and `3.19.79.227`, each followed by the answer `18.118.7.42` alone, giving a nil error after exactly two calls, and by a lookup failing on every call giving the `*ResolveError` R-BZPP-OEQZ states, not the lookup's error, after exactly `PollAttempts` calls.

- R-C3DE-TPZ2: `space.WaitResolves` MUST use no field of `deps` other than `LookupHost` and `After`, so that it passes no `seam.Cmd` to `deps.Exec` or `deps.Stream` whatever it returns; verified with a `Deps` whose `Exec` and `Stream` fail the test when called, over each case R-BZPP-OEQZ and R-C0XM-26HO name.

- R-UI1S-IQOA: `space.ReleaseElasticIP` MUST call `ec2.DisassociateAddress` with `addr.AssociationID` exactly once when that field is not empty and not at all when it is, and MUST then call `ec2.ReleaseAddress` with `addr.AllocationID` exactly once.

- R-UJ9O-WIEZ: `space.PutRecords` MUST call `route53.ChangeRecords` for `zone.ID` exactly once, with one `cloud.RecordChange` per element of `RecordNames(domain)` in that order, each whose `Action` is `cloud.ChangeUpsert` and whose `Record` has that element as `Name`, `A` as `Type`, `RecordTTL` as `TTL`, and exactly `address` as `Values`; MUST then call `route53.ChangeStatus` with the change id that call returned until it reports `cloud.ChangeInsync`, waiting on `deps.After(PollInterval)` between consecutive calls and making at most `PollAttempts` of them; and MUST return a `*WaitError` whose `Subject` is that change id and whose `Want` is `reach INSYNC` when that many calls have not reported it.

- R-UKHL-AA5O: `space.DeleteRecords` MUST call `route53.ListRecords` for `zone.ID` and MUST pass to `route53.ChangeRecords` for `zone.ID`, in one call, one `cloud.RecordChange` whose `Action` is `cloud.ChangeDelete` for each returned `cloud.Record` whose `Type` is `A` and whose `Name` is `domain`, `*.` followed by `domain`, or `\052.` followed by `domain`, each carrying that `cloud.Record` exactly as `ListRecords` returned it; MUST return the names of the records so deleted as `RecordNames(domain)` spells them — `domain` for the bare record and `*.` followed by `domain` for the wildcard however `ListRecords` spelled it — in that order; and MUST return an empty result and call `ChangeRecords` not at all when there are none; verified at least with a `ListRecords` that returns the wildcard record as `\052.staging.ikigenba.dev` and a `ChangeRecords` that records the `cloud.Record` it was given, the result being exactly `staging.ikigenba.dev` and `*.staging.ikigenba.dev`.

- R-ULPH-O1WD: `space.DeleteRole` MUST call `iam.RoleExists` and `iam.InstanceProfileRoles` for `RoleName(domain)`, MUST return false and a nil error and call no other IAM operation when neither reports existence, and otherwise MUST call `DeleteRolePolicy` for that role and `PolicyName`, `RemoveRoleFromInstanceProfile` for that instance profile and each role it holds, `DeleteInstanceProfile`, and `DeleteRole`, in that order, and return true.

- R-C4LB-7HPR: `space.FindApex` MUST call `route53.FindRecord(ctx, zoneID, root, "A")` and, when it reports no record, return an `ApexRecord` whose `Found` is false and whose `Holder` is empty without calling `ec2`; otherwise it MUST call `ec2.ListSpaceAddresses(ctx, root)` and return an `ApexRecord` whose `Record` is the record `FindRecord` returned unchanged, whose `Found` is true, and whose `Holder` is the `Space` of the one `cloud.Address` whose `IP` is an element of that record's `Values`, whether or not that address is associated with an instance, or empty when no address's `IP` is; and it MUST return a non-nil error naming `root` when two or more addresses match; verified at least by `Holder` being `sbx1.ikigenba.dev` for a record holding `18.118.7.42` and an address with that `IP`, that `Space`, and the association id `eipassoc-01`, by the same with an empty `AssociationID`, by `Holder` being empty with `Found` true for a record holding `203.0.113.9` that no address carries, and by `Found` being false with no `ListSpaceAddresses` call when the record is absent.

- R-JA8W-5LZ5: Package `internal/host` MUST export `CommandError` with exactly `Step string`, `Command []string`, `Status int`, `Stdout string`, and `Stderr string`; `Error() string` MUST return the space-joined command followed by `: exit status <Status>`, preceded by `<Step>: ` when nonempty; `Detail() string` MUST return `seam.QuoteOutput(Stdout)` followed by `seam.QuoteOutput(Stderr)`, each included only when that stream contains non-whitespace, the two separated by exactly one newline when both are included, and the empty string when neither is, so that what the remote program reported on its standard output precedes what it wrote to its standard error; `ExitCode() int` MUST return 1; verified at least by a `CommandError` whose `Stdout` is `a\nb\n` and whose `Stderr` is `c\n\n> d\n` giving a `Detail()` of exactly the five lines `> a`, `> b`, `> c`, `> `, and `> > d`, by one whose `Stderr` is empty and one whose `Stderr` is only whitespace each giving `seam.QuoteOutput(Stdout)` alone, and by one whose `Stdout` is empty giving `seam.QuoteOutput(Stderr)` alone.

- R-D6VV-A7PF: Package `internal/host` MUST export `Output` with exactly `Stdout string` and `Stderr string`, and methods `Run(ctx context.Context, step string, args ...string) (Output, error)`, `Sudo(ctx context.Context, step string, args ...string) (Output, error)`, `StreamSudo(ctx context.Context, stdout io.Writer, step string, args ...string) error`, and `Wait(ctx context.Context) error` on `Host`.

- R-C9GW-QKOJ: `Host.Run` MUST invoke `ssh` through `Deps.Exec` in `Deps.Dir` with `-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new`, followed, only when `KnownHosts` is not empty, by `-o` and `UserKnownHostsFile=` joined to `KnownHosts`, then `Target()`, and a remote command preserving each supplied argument as one literal shell word; `Host.Sudo` MUST prefix that remote argument vector with exactly `sudo`, `systemd-run`, `--quiet`, `--wait`, `--pipe`, and `--collect`, in that order; when `KnownHosts` is empty no argument MUST name `UserKnownHostsFile`. Tests MUST include spaces, quotes, wildcard characters and shell metacharacters to prove no argument becomes shell syntax, and MUST show a `Host` whose `Domain` is `staging.ikigenba.dev` and whose `KnownHosts` is `/tmp/create.Ab12/known_hosts` passing, before the remote command, exactly the arguments `-o`, `BatchMode=yes`, `-o`, `ConnectTimeout=10`, `-o`, `StrictHostKeyChecking=accept-new`, `-o`, `UserKnownHostsFile=/tmp/create.Ab12/known_hosts`, and `ec2-user@staging.ikigenba.dev`, and the same `Host` with an empty `KnownHosts` passing exactly those arguments without the last `-o` and its value.

- R-D9BO-1R6T: `Host.Run` and `Host.Sudo` MUST return unmodified stdout and stderr as Output and nil error on exit 0; on a nonzero exit they MUST return CommandError carrying step, status and both unmodified streams, with Command equal to `ssh`, Target(), and the logical remote argument vector, omitting transport flags and shell escaping. A process-start error MUST wrap the runner error, name ssh, and not masquerade as CommandError.

- R-DAJK-FIXI: `Host.StreamSudo` MUST execute the same SSH command as `Sudo` through `Deps.Stream`, stream stdout unchanged, and return nonzero process exits using `CommandError` with empty Stdout and the captured stderr; already-streamed output MUST NOT be repeated. Process-start, writer and context failures MUST remain distinguishable errors wrapping the runner error.

- R-SVKD-29H1: A command MUST write no step line for a step that did not complete, verified at least through `cli.Run` by one command whose fake fails a step leaving stdout without a line naming that step.

- R-CO3P-BTKV: `devctl space list --help` and `devctl space list -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space list

  Print one line per space: the domain, the instance state, the public address
  or - when it has none, and apex on the one space that holds the root domain or
  - on every other. The lines are sorted by domain, and no spaces prints nothing.

  The cloud's tags are the only registry: a space is an instance or an Elastic
  IP tagged with the root domain and a Space tag naming its own. The holder of
  the root domain is the space whose Elastic IP the root's A record points at;
  no host is asked. What a space is running is 'devctl space status'.
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

- R-UVGO-Q7TX: `devctl space` MUST write exactly the three lines `devctl: space needs <subcommand>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, call `Deps.Cloud` not at all, pass no `seam.Cmd` to `Deps.Exec`, and exit 2.

- R-TWMJ-JUA0: `space` invoked with a first argument that is none of its subcommands, `--help`, and `-h` and does not begin with `-` MUST write exactly the three lines `devctl: unknown subcommand '<name>'`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-UWOL-3ZKM: `space destroy`, `space stop`, `space start`, and `space status` invoked with no operand MUST each write exactly the three lines `devctl: space <subcommand> needs <space>` — where `<subcommand>` is the invoked subcommand's own name — an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, call `Deps.Cloud` not at all, pass no `seam.Cmd` to `Deps.Exec`, and exit 2, verified at least by reproducing `devctl: space destroy needs <space>`.

- R-UZ4D-VJ20: `space destroy`, `space stop`, `space start`, and `space status` invoked with more than one operand MUST each write `devctl: space <subcommand> takes only <space>`, and `space list` invoked with any operand MUST write `devctl: space list takes no arguments`, as the first of exactly three lines followed by an empty line and `see 'devctl space --help' for usage` on stderr, write nothing to stdout, call `Deps.Cloud` not at all, pass no `seam.Cmd` to `Deps.Exec`, and exit 2.

- R-04BN-SV5G: The `space` subcommands MUST be exactly `list`, `create`, `replace`, `destroy`, `stop`, `start`, `status`, `restart`, `disable`, `enable`, and `logs`; `list` takes no operand, `destroy`, `stop`, `start`, and `status` take exactly one `<space>` operand, and `destroy` alone among those five accepts options, exactly `--no-backup`, `--delete-secrets`, and `--delete-backups`, in addition to help; the grammars of `create`, `replace`, `restart`, `disable`, `enable`, and `logs` MUST be those their owning packages declare; verified at least through `cli.Run` by `devctl space move sbx1` writing exactly the three lines `devctl: unknown subcommand 'move'`, an empty line, and `see 'devctl space --help' for usage` to stderr, nothing to stdout, calling `Deps.Cloud` not at all, and exiting 2.

- R-CRRE-H4SY: An argument of `space` or of one of its subcommands other than `create`, `replace`, `restart`, `disable`, `enable`, and `logs` that begins with `-` and is none of `--help`, `-h`, and — for `destroy` only — `--no-backup`, `--delete-secrets`, and `--delete-backups` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl space --help' for usage` to be written to stderr, nothing to stdout, and exit 2, with `Deps.Cloud` not called and no `seam.Cmd` passed to `Deps.Exec`; verified at least by `devctl space stop sbx1 --no-backup` and `devctl space destroy sbx1 --no-backup=true`.

- R-V3ZZ-EM0S: `space destroy` MUST accept each of `--no-backup`, `--delete-secrets`, and `--delete-backups` on either side of its `<space>` operand and in any order, with repeated occurrences of one option having the same effect as one, and each option MUST govern only its own step — `--no-backup` the `retire` step, `--delete-secrets` the `secrets` step, and `--delete-backups` the `backups` step; a value-bearing form such as `--no-backup=true` or `--delete-secrets=yes` MUST be an unknown option.

- R-P5O5-A9HX: `space list`, `space destroy`, `space stop`, `space start`, and `space status` MUST each, after its usage errors are reported and before any other call, call `checkout.ReadRootFile(ctx, deps)`, then — for the four that take `<space>` — `spaceref.Parse` with the operand as typed and the `RootFile`'s `Domain` (D04 R-ST4K-APZN), then `cloud.Connect(ctx, deps.Cloud, RootFile.Domain, RootFile.Region)` (D02 R-YPS2-X32R), returning each call's error unchanged and making no later call when one fails; in the requirements below, `root` is that `Domain`, `space` is the `spaceref.Space` `Parse` returned, `clients` is the `Clients` of the `Session` `Connect` returned, and `zone` is what `clients.Route53.Zone(ctx, root)` returned; verified at least through `cli.Run` by `devctl space stop sbx1` in a temporary checkout whose root file holds `{"domain": "example.test", "region": "eu-west-1"}` leaving a recording fake `Deps.Cloud` with exactly one call whose profile is `example.test` and whose region is `eu-west-1`, and by `devctl space status crm.sbx1` leaving it with no call.

- R-V6FS-65I6: `space list` and `space stop` MUST pass no `seam.Cmd` to `deps.Exec` other than the single one `checkout.ReadRootFile` passes, and none to `deps.Stream`.

- R-RHLF-T86V: `space status`, `space stop`, and `space start` MUST each call D03's `cloud.LookupInstance(ctx, clients.EC2, root, space.Domain)`, and `space destroy` MUST call `cloud.LookupSpace(ctx, clients.EC2, root, space.Domain)`, before any call to `deps.Exec` other than `checkout.ReadRootFile`'s, any write to stdout, and any call to a method of `clients` other than those `cloud.Connect`, that lookup, and — for `destroy` — `clients.Route53.Zone` make; `status`, `stop`, and `start` MUST return its error unchanged, and `destroy` MUST treat a `*cloud.NoSpaceError` from it as the space having no instance and MUST NOT return it; verified at least through `cli.Run` by `devctl space stop gone`, `devctl space start gone`, and `devctl space status gone` each writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with empty stdout and exit 1.

- R-RITC-6ZXK: When `cloud.LookupInstance` returns a `*cloud.NoInstanceError`, as it does for a label carrying only its Elastic IP, `space stop`, `space start`, and `space status` MUST each return it unchanged, write nothing to stdout, call neither `clients.EC2.StopInstance` nor `clients.EC2.StartInstance`, pass no `seam.Cmd` to `deps.Exec` other than `checkout.ReadRootFile`'s, and pass none to `deps.Stream`; verified at least through `cli.Run`, for the label `sbx1` carrying only the Elastic IP `18.118.7.42` with no association id beside a terminated instance, by `devctl space stop sbx1`, `devctl space start sbx1`, and `devctl space status sbx1` each writing the single stderr line `devctl: 'sbx1.ikigenba.dev' has no instance` with empty stdout and exit 1.

- R-RK18-KRO9: `space destroy` MUST return a `*cloud.DuplicateAddressError` from `cloud.LookupSpace` unchanged before any write to stdout, any call to `deps.Exec` other than `checkout.ReadRootFile`'s, and any method of `clients` that creates, changes, terminates, releases, or deletes anything, the apex record included; verified at least through `cli.Run` by `devctl space destroy sbx1`, for a label carrying the two Elastic IPs `18.118.7.42` and `18.190.33.61`, writing the single stderr line `devctl: 'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)` with empty stdout and exit 2.

- R-XO8Q-YVRD: `space list` MUST call `clients.Route53.Zone(ctx, root)` and return its error unchanged before calling `cloud.Spaces(ctx, clients.EC2, root)`, MUST call `FindApex(ctx, clients.Route53, clients.EC2, zone.ID, root)` exactly once when `Spaces` returned at least one space and not at all when it returned none, and MUST call no other method of `clients` beyond the `STS.CallerAccountID` call `cloud.Connect` makes; verified at least through `cli.Run` by a `Zone` that returns a `*cloud.NotFoundError` giving the single stderr line `devctl: no hosted zone 'ikigenba.dev'`, empty stdout, and exit 1.

- R-XMWC-TW4J: `space list` MUST return any error from `cloud.Spaces` unchanged and write nothing to stdout; verified at least through `cli.Run` by `devctl space list` with a fake `EC2` reporting the two Elastic IPs `18.118.7.42` and `18.190.33.61` on the label `sbx1.ikigenba.dev`, so that `Spaces` returns a `*cloud.DuplicateAddressError`, writing the single stderr line `devctl: 'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)` with empty stdout and exit 2.

- R-9IAV-GV18: `space list` MUST write one line to stdout for each `cloud.Space` that `cloud.Spaces` returns, in the order it returns them, consisting of that `Space`'s `Domain`, one space, its `State` or `-` when `State` is empty, one space, its `Address` or `-` when `Address` is empty, one space, and `apex` when the `ApexRecord` `FindApex` returned has a `Holder` equal to that `Domain` or `-` otherwise; MUST write nothing to stdout when `Spaces` returns none; and MUST exit 0; verified at least by reproducing the three lines `new.ikigenba.dev running 18.220.10.5 -`, `sbx1.ikigenba.dev running 18.118.7.42 apex`, and `sbx2.ikigenba.dev stopped - -` for a zone whose `A` record `ikigenba.dev` holds `18.118.7.42`, the `IP` of the address whose `Space` is `sbx1.ikigenba.dev`; by every line ending in ` -` both when that record is absent and when it holds an `IP` no address carries; by reproducing the two lines `sbx1.ikigenba.dev - 18.118.7.42 -` and `sbx2.ikigenba.dev running 18.220.10.5 -` for a label `sbx1` carrying only the unassociated Elastic IP `18.118.7.42` beside a terminated instance, and a zone holding no `A` record `ikigenba.dev`; and by reproducing empty output.

- R-RNOX-Q2WC: `space status` MUST return a `*NotRunningError` whose `Domain` is `space.Domain` and whose `State` is the `State` of the `Space` `cloud.LookupInstance` returned, write nothing to stdout, and pass no `seam.Cmd` to `deps.Exec` other than `checkout.ReadRootFile`'s, when that state is not `cloud.StateRunning`, verified at least by reproducing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` with exit 1 for both `space status sbx2` and `space status sbx2.ikigenba.dev`.

- R-CBWP-I45X: `space status` MUST obtain its answer from exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Domain` is `space.Domain`, whose `Deps` is `deps`, and whose `KnownHosts` is empty, with an empty `step` and exactly the arguments `opsctl` and `status`, and MUST write that call's `Output.Stdout` to stdout byte for byte and nothing else, neither parsing, reordering, reformatting, nor validating it; verified at least with a fake `Deps.Exec` that exits 0 with arbitrary multi-line standard output, with one whose standard output has no trailing newline, and with one whose standard output is empty, each reproduced on stdout unchanged.

- R-RL94-YJEY: `space stop` MUST write the step `instance` first, whose detail MUST be `already stopped`, with `StopInstance` not called, when the `State` of the `Space` `cloud.LookupInstance` returned is `cloud.StateStopped`, and otherwise MUST call `clients.EC2.StopInstance` with that `Space`'s `ID` exactly once, then `WaitState` for `cloud.StateStopped`, and report that `ID` followed by ` stopped`; verified at least by reproducing `instance: ok (i-0a1b2c3d4e5f60718 stopped)` and `instance: ok (already stopped)`.

- R-RMH1-CB5N: `space start` MUST write the step `instance` first, MUST call `clients.EC2.StartInstance` exactly once with the `ID` of the `Space` `cloud.LookupInstance` returned and then `WaitState` for `cloud.StateRunning` when that `Space`'s `State` is not `cloud.StateRunning` and MUST call `StartInstance` not at all when it is, and MUST report that `ID`, ` running, `, and the running instance's `Address`; verified at least by reproducing `instance: ok (i-0a1b2c3d4e5f60718 running, 18.220.10.5)`.

- R-8XNM-MR8T: `space stop` MUST print only its instance step and MUST NOT change DNS records, release or disassociate the Elastic IP, modify storage, resource tags, secrets or IAM resources, or execute any host command.

- R-DP6D-0RTU: `space start` MUST write the step `host` second, with the detail `status checks passed`, after `WaitChecks` for the space's `ID` returned a nil error.

- R-CD4L-VVWM: `space start` MUST write the step `certificate` third, after exactly one successful call to `(host.Host).Wait` and then exactly one successful call to `(host.Host).Sudo` with the step `certificate` and exactly the arguments `certbot` and `renew`, both on a `host.Host` whose `Domain` is `space.Domain`, whose `Deps` is `deps`, and whose `KnownHosts` is empty; its detail MUST be `certbot renew`, verified by reproducing `certificate: ok (certbot renew)`. A zero exit is the whole of the step's success, whether or not certbot renewed anything; a failed call MUST produce no certificate step line and MUST return the host error unchanged.

- R-S6SW-5NHI: `space start` MUST perform only `instance`, `host`, and `certificate` steps in that order, followed on success by one stdout line of `space.Domain`, one space, and the running instance's `Address`; it MUST NOT change records or the Elastic IP. Failure MUST preserve completed steps and omit the final address line. A running instance MUST still receive status and renewal checks on a repeated start; verified at least by `devctl space start staging` reproducing the final line `staging.ikigenba.dev 18.220.10.5`.

- R-VEZ2-UJP1: `space destroy` MUST write, as its only stdout, the step lines `apex` — only when the space holds the apex — then `retire`, `instance`, `address`, `records`, `secrets`, `backups`, and `role`, in that order, MUST write no `account` or `domain` line, MUST stop at the first failure without undoing completed steps and without writing a line for the failed step, and MUST exit 0 only after every step has completed; verified at least by reproducing, in order, `apex: ok (ikigenba.dev record deleted)`, `retire: ok (opsctl retire)`, `instance: ok (i-0c9e94542d98846a8 terminated)`, `address: ok (elastic ip 18.118.7.42 released)`, `records: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)`, `secrets: ok (kept)`, `backups: ok (kept)`, and `role: ok (sbx1.ikigenba.dev deleted)` with exit 0, and by a `ChangeRecords` failure in the `records` step of `space destroy sbx1 --no-backup` leaving exactly `retire: skipped (--no-backup)`, `instance: ok (i-0c9e94542d98846a8 terminated)`, and `address: ok (elastic ip 18.118.7.42 released)` on stdout with the single stderr line `devctl: route53 ChangeResourceRecordSets: Throttling` and exit 1.

- R-CECI-9NNB: `space destroy` MUST call `clients.Route53.Zone(ctx, root)` and return its error unchanged, and MUST return a `*RetireStateError` whose `ID` and `State` are those of the `Space` `LookupSpace` returned and whose `Label` is `space.Label` when `--no-backup` is not given and that `Space`'s `ID` is not empty and its `State` is not `cloud.StateRunning`, both before any write to stdout, any call to `deps.Exec` other than `checkout.ReadRootFile`'s, and any method of `clients` that creates, changes, terminates, releases, or deletes anything, so that a refused destroy changes nothing; verified at least by reproducing the stderr lines `devctl: retire: instance i-0a1b2c3d4e5f60718 is stopped`, an empty line, and `run 'devctl space start staging' first, or pass --no-backup` with empty stdout and exit 1 for both `space destroy staging` and `space destroy staging.ikigenba.dev`, including when the zone's `A` record at `root` points at that space's address, and by a `Space` whose `ID` is empty, as `LookupSpace` returns for a label carrying only its Elastic IP, never being refused.

- R-CFKE-NFE0: The `apex` step of `space destroy` MUST call `FindApex(ctx, clients.Route53, clients.EC2, zone.ID, root)` after the checks R-CECI-9NNB states and before the `retire` step, and exactly when the returned `Holder` equals `space.Domain` MUST call `clients.Route53.ChangeRecords` for `zone.ID` exactly once with one `cloud.RecordChange` whose `Action` is `cloud.ChangeDelete` and whose `Record` is the `Record` `FindApex` returned, then write the step `apex` with the detail `root` followed by ` record deleted`; when `Holder` differs it MUST write no `apex` line and delete no record; and destroy MUST run no host command about the apex; verified at least by reproducing `apex: ok (ikigenba.dev record deleted)` as the first stdout line both without and with `--no-backup`, and by a destroy of a space that does not hold the apex writing no line beginning `apex`.

- R-CGSB-174P: When `LookupSpace` returned a `Space` whose `ID` is not empty and `--no-backup` is not given, `space destroy` MUST run the `retire` step as exactly one call to `(host.Host).Sudo` with the step `retire` and the arguments `opsctl` and `retire`, on a `host.Host` whose `Domain` is `space.Domain`, whose `Deps` is `deps`, and whose `KnownHosts` is empty, before any method of `clients` that terminates, releases, deletes, or changes anything other than the `apex` step's record delete, and MUST return that call's error unchanged; verified at least through `cli.Run` by `space destroy staging` with a fake `ssh` exiting 1 writing the first stderr line `devctl: retire: ssh ec2-user@staging.ikigenba.dev sudo systemd-run --quiet --wait --pipe --collect opsctl retire: exit status 1`, with empty stdout, exit 1, and `TerminateInstance` not called.

- R-YAGF-GEFY: After `sudo systemd-run --quiet --wait --pipe --collect opsctl retire` exits 0, destroy MUST report `retire: ok (opsctl retire)`; a zero exit is the whole of the step's success, and devctl MUST NOT parse opsctl's output or read the backup bucket to describe what was backed up. Verified by reproducing `retire: ok (opsctl retire)`.

- R-CI07-EYVE: The `retire` step of `space destroy` MUST write exactly the line `retire: ok (already gone)` when `LookupSpace` returned a `*cloud.NoSpaceError` or a `Space` whose `ID` is empty, whether or not `--no-backup` is given, and exactly the line `retire: skipped (--no-backup)` when it returned a `Space` whose `ID` is not empty and `--no-backup` is given, whatever that `Space`'s `State`, in each case passing no `seam.Cmd` to `deps.Exec` other than `checkout.ReadRootFile`'s; verified at least by reproducing both lines and by `space destroy staging --no-backup` on a `stopped` instance continuing to `instance: ok (i-0a1b2c3d4e5f60718 terminated)`.

- R-CJ83-SQM3: The `instance` step of `space destroy` MUST, when `LookupSpace` returned a `Space` whose `ID` is not empty, call `clients.EC2.TerminateInstance` exactly once with that `ID` and with no other id, call `StartInstance` not at all, and call `WaitState` for `cloud.StateTerminated` for that `ID`, returning each error unchanged, and then report that `ID` followed by ` terminated`; when `LookupSpace` returned a `*cloud.NoSpaceError` or a `Space` whose `ID` is empty it MUST report `already gone` and call `TerminateInstance` not at all; verified at least by reproducing `instance: ok (i-0a1b2c3d4e5f60718 terminated)` and `instance: ok (already gone)`.

- R-CLNW-KA3H: The `address` step of `space destroy` MUST call `cloud.LookupSpace(ctx, clients.EC2, root, space.Domain)` again after the `instance` step, so that it acts on the address as it stands once the instance is terminated; when that call returns a `Space` whose `ElasticIP.AllocationID` is not empty it MUST call `ReleaseElasticIP(ctx, clients.EC2, <that Space's ElasticIP>)` exactly once and report `elastic ip `, that `ElasticIP`'s `IP`, and ` released`; when it returns a `*cloud.NoSpaceError` or a `Space` whose `ElasticIP.AllocationID` is empty it MUST call no address method and report `no elastic ip` when the `instance` step terminated an instance and `already gone` when it reported `already gone`; and it MUST return any other error unchanged; verified at least by reproducing `address: ok (elastic ip 18.220.10.5 released)`, `address: ok (no elastic ip)`, and `address: ok (already gone)`, and by a fake `EC2` that reports the address `18.220.10.5` with the association id `eipassoc-01` until `TerminateInstance` is called and with none after it giving no `DisassociateAddress` call.

- R-CMVS-Y1U6: `space destroy sbx1`, for a label carrying only the Elastic IP `18.118.7.42` with the allocation id `eipalloc-01` and no association id, beside a terminated `i-0c9e94542d98846a8`, the space's two records, and its role, and not holding the apex, MUST write to stdout exactly the lines `retire: ok (already gone)`, `instance: ok (already gone)`, `address: ok (elastic ip 18.118.7.42 released)`, `records: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)`, `secrets: ok (kept)`, `backups: ok (kept)`, and `role: ok (sbx1.ikigenba.dev deleted)` and exit 0, MUST call `ReleaseAddress` with `eipalloc-01` exactly once and `DisassociateAddress` and `TerminateInstance` not at all, and MUST pass no `ssh` or `scp` `seam.Cmd` to `deps.Exec` or `deps.Stream`; verified through `cli.Run`.

- R-VNID-IXVW: The `records` step of `space destroy` MUST call `DeleteRecords(ctx, clients.Route53, zone, space.Domain)` exactly once and MUST report `deleted ` followed by the names it returned joined by `, ` when it returned at least one, and `already gone` when it returned none; verified at least by reproducing `records: ok (deleted staging.ikigenba.dev, *.staging.ikigenba.dev)`, `records: ok (deleted staging.ikigenba.dev)` for a zone that holds only the bare record, and `records: ok (already gone)`.

- R-S80S-JF87: The `secrets` step of `space destroy` MUST report `kept`, with no SSM method called, when `--delete-secrets` is not given, and otherwise MUST call `clients.SSM.ListParameters` with `secrets.Prefix(space.Domain)` and `clients.SSM.DeleteParameter` exactly once for each `cloud.Parameter` it returned, and report that count followed by ` parameters deleted` when the count is not 0 and `already gone` when it is 0; verified at least by reproducing `secrets: ok (kept)` both for a space whose instance was terminated and for one that was already gone, `secrets: ok (3 parameters deleted)`, and `secrets: ok (already gone)`.

- R-U77A-4SM8: The `backups` step of `space destroy` MUST report `kept`, with no S3 method called, when `--delete-backups` is not given, and otherwise MUST call `clients.S3.ListObjects` exactly once with `root` as the bucket and `BackupPrefix(space.Label)` as the prefix and, for each page that call hands to `each`, call `clients.S3.DeleteObjects` with `root` and exactly that page's keys, once, before `each` returns true, holding only that page and a running count of keys deleted, never a key from an earlier page; it MUST call `DeleteObjects` not at all when the listing hands no page, and report that count followed by ` objects deleted` when it is not 0 and `already gone` when it is 0; verified at least by reproducing `backups: ok (kept)`, `backups: ok (12 objects deleted)`, and `backups: ok (already gone)`, and by a fake `S3` that lists 2500 objects under `sbx1/` in pages of 1000, 1000, and 500, records with each `DeleteObjects` call how many pages it has handed out, and fails any `DeleteObjects` call with more than `cloud.ObjectPageSize` keys, giving exactly three `DeleteObjects` calls with those pages' keys in order, made when one, two, and three pages had been handed out, and `backups: ok (2500 objects deleted)`.

- R-U8F6-IKCX: When a `DeleteObjects` call of the `backups` step of `space destroy` returns an error, `each` MUST return false, the step MUST call `DeleteObjects` no more and write no `backups` line, and `space destroy` MUST return that error unchanged, as it MUST any error `ListObjects` returns; verified at least by a fake `S3` that lists three pages under `sbx1/` and fails the second `DeleteObjects` call, giving exactly two `DeleteObjects` calls, no third page handed out, no `backups` or `role` line, and that error returned, and by a fake whose `ListObjects` fails after handing one page, giving exactly one `DeleteObjects` call and that error returned.

- R-VR62-O93Z: The `role` step of `space destroy` MUST call `DeleteRole(ctx, clients.IAM, space.Domain)` and report `RoleName(space.Domain)` followed by ` deleted` when it returned true and `already gone` when it returned false; verified at least by reproducing `role: ok (staging.ikigenba.dev deleted)` and `role: ok (already gone)`.

- R-YBOB-U66N: `space list` and `space status` MUST perform no cloud mutation and MUST leave local checkout files unchanged; status MUST execute no host command other than its single `sudo systemd-run --quiet --wait --pipe --collect opsctl status` invocation. Tests MUST use cloud fakes that fail on mutation and verify the host command count for both populated and empty results.

- R-CPBL-PLBK: `devctl space --help` and `devctl space -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space <subcommand> [arguments]

  List, create, replace, destroy, stop, start, and inspect spaces, and restart,
  disable, enable, or read the journal of one app on one. A space is one label
  under the root domain; <space> is that label or the full domain. The cloud's
  tags are the only registry.

  Subcommands:
    list                       one line per space
    create <space> [options]   create the space and deploy a release to it
    replace <space> [options]  put the space on a new instance at its address and restore it there
    destroy <space> [options]  remove the space and everything it owned
    stop <space>               stop the instance; state is kept
    start <space>              start the instance; its address is unchanged
    status <space>             one line per app: commit, label, service state, socket state, database journal mode
    restart <space> <app>      restart one app's service on the host
    disable <space> <app>      stop one app and keep it from starting until enabled
    enable <space> <app>       let a disabled app start again, and start it
    logs <space> <app>         print one app's journal from the host

  Options (create):
    --acme-email <address>  where the CA sends the space's expiry warnings; required
    --release <sha|tag>     the release to deploy; the newest release-<N> tag otherwise
    --apex <app>            after the restore, set host.apex to <app>, an app in the release
    --no-apex               after the restore, delete host.apex whatever the backup held

  Options (replace):
    --release <sha|tag>     the release to deploy; the newest release-<N> tag otherwise
    --no-backup             skip the final backup; restore from the newest backups already kept

  Options (destroy):
    --no-backup             skip the final backup the host takes before it goes
    --delete-secrets        delete the space's secrets; they are kept otherwise
    --delete-backups        delete the space's backups; they are kept otherwise

  Options (logs):
    --follow                keep printing as the app writes, until interrupted
    --since <when>          start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00

  Run 'devctl space <subcommand> --help' for details.
  ```

- R-UPFM-CVS0: `devctl space status --help` and `devctl space status -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout and before any external operation:

  ```
  Usage: devctl space status <space>

  Relay opsctl status from the running host: app, commit, label, service state,
  socket state and database journal mode. A host with no apps prints nothing.
  ```

- R-CSZA-UWJN: `cli.Run` MUST dispatch `space create` to `spacecreate.Run`, `space replace` to `spacereplace.Run`, `space restart`, `space disable`, `space enable`, and `space logs` to `spaceapps.Run`, and every other `space` invocation to `space.Run`; create and replace each receive the arguments after its subcommand and, as `version`, the version string that `devctl --version` prints without its newline, spaceapps receives the subcommand and the arguments after it, and every dispatch receives `cli.Run`'s own `ctx`, `stdout`, and `deps` and nothing else, and returns 0 on a nil error.

- R-URVF-4F9E: Package `internal/host` MUST export the method `Copy(ctx context.Context, step, local, remote string) error` on `Host`.

- R-CAOT-4CF8: `(host.Host).Copy` MUST pass to `Deps.Exec` exactly one `seam.Cmd`, whose `Path` is `scp`, whose `Dir` is `Deps.Dir`, and whose `Args` are exactly `-o`, `BatchMode=yes`, `-o`, `ConnectTimeout=10`, `-o`, `StrictHostKeyChecking=accept-new`, then — only when `KnownHosts` is not empty — `-o` and `UserKnownHostsFile=` joined to `KnownHosts`, then `local`, and `Target()` followed by `:` and `remote`, so that the file at `local` on the developer's machine is copied to `remote` on the host as `User`; it MUST return nil when that process exits 0; on a non-zero exit it MUST return a `*CommandError` whose `Step` is `step`, whose `Command` is exactly `scp`, `local`, and `Target()` followed by `:` and `remote`, omitting the transport options, and which carries the exit status and both unmodified streams; and when `Deps.Exec` returns a non-nil error it MUST return an error that wraps it, whose message contains `scp`, and that `errors.As` does not match to a `*CommandError`; verified at least by a `Host` whose `Domain` is `sbx1.ikigenba.dev` copying `/w/dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` to `/tmp/tmp.Ab12Cd34Ef` with the step `copy` and a fake `scp` exiting 1 with the standard error `No space left on device`, giving an `Error()` of `copy: scp /w/dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz ec2-user@sbx1.ikigenba.dev:/tmp/tmp.Ab12Cd34Ef: exit status 1` and a `Detail()` of `> No space left on device`, and by the same copy from a `Host` whose `KnownHosts` is `/tmp/create.Ab12/known_hosts` passing exactly the `Args` `-o`, `BatchMode=yes`, `-o`, `ConnectTimeout=10`, `-o`, `StrictHostKeyChecking=accept-new`, `-o`, `UserKnownHostsFile=/tmp/create.Ab12/known_hosts`, `/w/dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`, and `ec2-user@sbx1.ikigenba.dev:/tmp/tmp.Ab12Cd34Ef`, with the same `Error()`.
