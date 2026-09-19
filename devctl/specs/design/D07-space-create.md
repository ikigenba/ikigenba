# D07-space-create

`space create` makes one complete space at one label under the root. It
follows the session every space-taking command follows: usage errors first,
then the checkout and its apps, then the root file, then the `<space>` operand
through the one parser (D04), then one cloud session whose first call is STS
(D03). Before it prints or changes anything it establishes that the platform
is there and the name is free: the hosted zone, the launch template, and the
permissions boundary named after the root exist, no `NS` record delegates the
space's name away, no instance is tagged for the space, and no role or
instance profile of the space's name was left behind. Then it pushes the
apps' secrets, makes the role, launches the instance, gives it an Elastic IP,
writes its two `A` records, installs the newest published opsctl, sets the
ten host keys, restores the space's own host backup when the bucket holds
one, and runs `opsctl init`. Each line of output is one step; the last line is
the space domain and the address. Failures leave completed work for destroy
to clean up.

A freshly created instance profile is not visible to EC2 the instant IAM
returns it, so the `role` step does not finish until the profile is provably
usable: after attaching the role it waits, through `space.WaitLaunchReady`,
for a dry-run launch to be accepted, and only then writes `role: ok`. The
`instance` step's real launch therefore never races the profile's
propagation.

The role and its instance profile are named exactly the space domain
(`space.RoleName`). IAM documents a `RoleName` of at most 64 characters, and a
63-byte label under the root would exceed it, so create refuses such a name
before it connects. The role's one inline policy, `space.PolicyName`, is the
document `space.PolicyDocument` (D06) builds from the root, the zone id, and
the space; what it grants is D06's contract, and create only writes it with
`apex` false, so the fresh role reads the space's parameters, works under the
space's label in the bucket named after the root, and writes `TXT` records at
the space's own two names in the zone and nothing else in Route 53. That is
what proving ownership for its certificate takes. Create writes the
space's `A` records itself, with the developer's identity, so the host never
needs an `A`-record write, and it never enumerates zones, since opsctl
carries the zone id in `dns.zones`. `apex set` and `apex clear` (D14)
regenerate the same document with `apex` true or false, which adds or removes
the one name outside the space's subtree, `_acme-challenge.<root>`, that only
the holder of the apex may prove.

The `restore` step runs on every create. It lists `<label>/host/` in the
bucket; when nothing is there the line says so, and when a backup is there
`opsctl host restore` puts the old host's certificate and store back, the
ten keys are set again so what create was told wins over the backup's copy,
and `host.apex` is deleted so a rebuilt host never holds the apex until `apex
set` says so. The restore's documented selection is the newest backup, so the
key the line reports is the newest object devctl listed; nothing opsctl
prints is parsed.

## REQUIREMENTS

- R-Z2ND-8MJF: Package `internal/spacecreate` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout` and no profile name.

- R-XQZ0-EB4M: Package `internal/spacecreate` MUST export `AssumeRolePolicy`, whose value is exactly `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ec2.amazonaws.com"}}]}`.

- R-XS6W-S2VB: Package `internal/spacecreate` MUST export a `RefusedError` struct whose only field is `Message string`, with the methods `Error() string`, returning `Message`, and `ExitCode() int`, returning 2.

- R-8TLD-OA2V: `space create` MUST take exactly one `<space>` operand and the required option `--acme-email <address>` or `--acme-email=<address>`, accepting the option before or after the operand and taking the last occurrence, and `--help`/`-h`; no other option is accepted, and `--elastic-ip` MUST be refused as unknown.

- R-8UTA-21TK: `space create` invoked with no operand MUST write exactly the three lines `devctl: space create needs <space>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec` or `deps.Stream`, and exit 2.

- R-8W16-FTK9: `space create` invoked with more than one operand MUST write exactly the three lines `devctl: space create takes only <space>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-3M5T-DZ38: The missing-domain and extra-operand refusals above MUST be reported by returning a `*space.UsageError` whose `Message` is that refusal's first line without its `devctl: ` prefix and whose `Help` is `devctl space --help`.

- R-E7GU-RBY9: An unknown option of create MUST produce `space.UsageError` with Message `unknown option '<option>'` and Help `devctl space --help`, empty stdout and exit 2; a consumed acme-email value MUST not be treated as an option.

- R-8X92-TLAY: After operand arity is checked, create without `--acme-email` MUST return a `*space.UsageError` whose `Message` is `space create needs --acme-email <address>` and whose `Help` is `devctl space --help`; a `--acme-email` whose value is missing or empty MUST instead give the `Message` `option '--acme-email' requires a value`; on these failures and on every other usage failure `Run` MUST call neither `checkout.Open` nor `checkout.ReadRootFile`, MUST pass no `seam.Cmd` to `deps.Exec` or `deps.Stream`, and MUST call `deps.Cloud` not at all, so that nothing is checked about `<space>`; verified at least through `cli.Run`, with `Deps.Dir` set to a directory that is not inside a git checkout, by `devctl space create sbx1` writing exactly the three lines `devctl: space create needs --acme-email <address>`, an empty line, and `see 'devctl space --help' for usage` to stderr with nothing on stdout and exit 2, and by `devctl space create sbx1 --acme-email` writing `devctl: option '--acme-email' requires a value` as its first line with exit 2.

- R-8YGZ-7D1N: `devctl space create --help` and `devctl space create -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout, without a root file, and before any external operation, calling `deps.Cloud` not at all and passing no `seam.Cmd` to `deps.Exec` or `deps.Stream`, verified at least with `Deps.Dir` set to a directory that is not inside a git checkout:

  ```
  Usage: devctl space create <space> --acme-email <address>

  Create the space: push its secrets, make its role, launch its instance with an
  Elastic IP, write its records, install the newest published opsctl, set its
  ten host keys, restore its own host backup when the bucket holds one, and run
  opsctl init. Completed steps remain on failure; use space destroy to clean up.

  Options:
    --acme-email <address>  where the CA sends the space's expiry warnings; required
  ```

- R-8ZOV-L4SC: After its arguments are accepted, `space create` MUST call `checkout.Open(ctx, deps)`, then `(*Checkout).Apps`, then `(*Checkout).ReadRootFile`, returning each one's error unchanged; MUST then obtain the space — the parsed `spaceref.Space`, called `sp` in this design — from the `<space>` operand under R-QW0J-KUFF, with the `Domain` of the `checkout.RootFile` read as `root`; and MUST then, after the check R-90WR-YWJ1 states, call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` exactly once with that `RootFile`'s `Domain` and `Region`, as D02 R-OO0I-HZUA binds, returning its error unchanged; when any call before `Connect` fails it MUST call `deps.Cloud` not at all; verified at least through `cli.Run`, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by a recording fake `Opener` receiving `ikigenba.dev` and `us-east-2` from both `devctl space create sbx1 --acme-email ops@ikigenba.dev` and `devctl space create sbx1.ikigenba.dev --acme-email ops@ikigenba.dev`, by `devctl space create crm.sbx1 --acme-email ops@ikigenba.dev` writing the single stderr line `devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'` with empty stdout, exit 2, and that fake left with no call, and by a fake `STS` whose `CallerAccountID` fails with an empty `Code` and an `Err` whose message is `<m>` giving the single stderr line `devctl: sts GetCallerIdentity: <m>` with empty stdout, exit 1, and no other client method called.

- R-90WR-YWJ1: After `spaceref.Parse` succeeds and before `cloud.Connect`, `space create` MUST return a `*RefusedError` whose `Message` is `'<sp.Domain>' is too long: a space's role name is at most 64 characters` when `sp.Domain` is longer than 64 bytes, since the role and the instance profile are named `space.RoleName(sp.Domain)` and IAM documents a `RoleName` of at most 64 characters, and MUST NOT refuse when it is exactly 64 bytes; verified at least, under the root `ikigenba.dev`, by a 52-byte label writing that single stderr line with empty stdout, exit 2, and a recording fake `Deps.Cloud` left with no call, and by a 51-byte label reaching `cloud.Connect`.

- R-924O-CO9Q: After `cloud.Connect` returned a `Session`, `space create` MUST call, in this order, `session.Clients.Route53.Zone(ctx, root.Domain)`, `session.Clients.EC2.LaunchTemplate(ctx, root.Domain)`, `session.Clients.IAM.PermissionsBoundary(ctx, root.Domain)`, `session.Clients.Route53.FindRecord(ctx, zone.ID, sp.Domain, "NS")` with the `ID` of the zone returned, `cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, sp.Domain)`, and `session.Clients.IAM.RoleExists` and `session.Clients.IAM.InstanceProfileRoles` for `space.RoleName(sp.Domain)`, returning the first non-nil error of `Zone`, `LaunchTemplate`, `PermissionsBoundary`, `FindRecord`, `RoleExists`, and `InstanceProfileRoles` unchanged and a `LookupSpace` error other than a `*cloud.NoSpaceError` unchanged, and MUST complete all of them, and the refusals R-93CK-QG0F, R-94KH-47R4, and R-95SD-HZHT state, before it writes any byte to `stdout`, calls `secrets.Push`, calls any of `PutSecureParameter`, `CreateRole`, `PutRolePolicy`, `CreateInstanceProfile`, `AddRoleToInstanceProfile`, `LaunchReady`, `RunInstance`, `AllocateAddress`, `AssociateAddress`, `ChangeRecords`, `ListObjects`, and `PutObject`, or passes to `deps.Exec` or `deps.Stream` any `seam.Cmd` other than the one `checkout.Open` passes; verified at least through `cli.Run`, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by fakes with no launch template named `ikigenba.dev` writing the single stderr line `devctl: no launch template 'ikigenba.dev'` with empty stdout and exit 1, by fakes with neither the zone nor the launch template writing `devctl: no hosted zone 'ikigenba.dev'`, and by fakes with the zone and the launch template but no permissions boundary writing `devctl: no permissions boundary 'ikigenba.dev'`, each with every recording fake left with none of the calls named above.

- R-93CK-QG0F: `space create` MUST return a `*RefusedError` whose `Message` is `'<sp.Domain>' is delegated away from '<zone.Name>'`, where `zone` is the `cloud.Zone` `Route53.Zone` returned, when `FindRecord` for `sp.Domain` and `NS` reports a record found; verified at least by reproducing the single stderr line `devctl: 'sbx.ikigenba.dev' is delegated away from 'ikigenba.dev'` with empty stdout and exit 2 for both the operands `sbx` and `sbx.ikigenba.dev` against a fake `Route53` whose zone `ikigenba.dev` holds an `NS` record named `sbx.ikigenba.dev`, and by the operand `sbx1` against the same fake reaching `secrets.Push`.

- R-94KH-47R4: `space create` MUST return a `*RefusedError` whose `Message` is `a space at '<sp.Domain>' already exists (<ID>)`, where `<ID>` is the `ID` of the `cloud.Space` `cloud.LookupSpace` returned, when `LookupSpace` returns a nil error, and MUST proceed when it returns a `*cloud.NoSpaceError`; verified at least by reproducing the single stderr line `devctl: a space at 'sbx1.ikigenba.dev' already exists (i-0c9e94542d98846a8)` with empty stdout and exit 2 against a fake `EC2` whose non-terminated instance tagged `Space=sbx1.ikigenba.dev` has that id, and by a fake whose only instance for that space is `terminated` reaching `secrets.Push`.

- R-95SD-HZHT: `space create` MUST return a `*RefusedError` whose `Message` is `a role for '<sp.Domain>' already exists` when `RoleExists` or `InstanceProfileRoles` for `space.RoleName(sp.Domain)` reports existence, adopting neither; verified at least by reproducing the single stderr line `devctl: a role for 'sbx1.ikigenba.dev' already exists` with empty stdout and exit 2 for each of the two reporting existence alone, with no `CreateRole`, `PutRolePolicy`, `CreateInstanceProfile`, or `AddRoleToInstanceProfile` call.

- R-9709-VR8I: `space create` MUST write no byte to stdout before `secrets.Push(ctx, deps, session.Clients.SSM, sp.Domain, apps)` — called exactly once with the apps `(*Checkout).Apps` returned, in the order it returned them — has returned a nil error, MUST return that call's error unchanged when it is not nil, and MUST then write, through `space.Step`, exactly the three lines `account: ok (<root.Domain>, <root.Region>, <session.AccountID>)`, `domain: ok (zone <zone.Name> <zone.ID>)`, and `secrets: ok (<n> apps)`, in that order, where `<n>` is the decimal count of the `secrets.Entry` values `Push` returned; verified at least by reproducing `account: ok (ikigenba.dev, us-east-2, 295229566359)`, `domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)`, and `secrets: ok (3 apps)`, and by reproducing the single stderr line `devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment` with empty stdout, exit 2, and no call to `PutSecureParameter`, `CreateRole`, or `RunInstance`.

- R-VTEC-13P5: `space create` MUST write the step `role` fourth, after calling `session.Clients.IAM.CreateRole` with a `cloud.RoleSpec` whose `Name` is `space.RoleName(sp.Domain)`, whose `AssumeRolePolicy` is `AssumeRolePolicy`, and whose `PermissionsBoundaryARN` is the ARN `PermissionsBoundary` returned, then `PutRolePolicy` with that role name, `space.PolicyName`, and exactly `space.PolicyDocument(root.Domain, zone.ID, sp, false)`, then `CreateInstanceProfile` with that same name, then `AddRoleToInstanceProfile` with that name as both the profile and the role, then `space.WaitLaunchReady(ctx, deps, session.Clients.EC2, spec)` with a `cloud.LaunchSpec` equal to the one the `instance` step passes to `RunInstance` — its `LaunchTemplateID` the id `LaunchTemplate` returned, its `InstanceProfile` `space.RoleName(sp.Domain)`, its `Domain` `root.Domain`, and its `Space` `sp.Domain` — each exactly once and in that order; it MUST return `WaitLaunchReady`'s error unchanged when it is not nil, MUST NOT write the `role` line until `WaitLaunchReady` has returned a nil error, and its detail MUST be `space.RoleName(sp.Domain)`; verified at least by reproducing `role: ok (sbx1.ikigenba.dev)` and by a recording fake `IAM` receiving, for the operand `sbx1` under `ikigenba.dev` and the zone id `Z09565073GHK8BYWQ1A78`, a document equal to `space.PolicyDocument("ikigenba.dev", "Z09565073GHK8BYWQ1A78", sp, false)`.

- R-9GRG-XX62: `space create` MUST write the step `instance` fifth, after calling `session.Clients.EC2.RunInstance` exactly once with that same `cloud.LaunchSpec` and then `space.WaitState(ctx, deps, session.Clients.EC2, id, cloud.StateRunning)` for the launched instance's `ID`, and its detail MUST be the running instance's `ID`, ` running, `, and its `Address`; it MUST return `RunInstance`'s error unchanged when it is not nil, writing no `instance` line; verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)`, and through `cli.Run` by a fake `EC2` whose `RunInstance` fails with a `*cloud.Error` for `ec2` `RunInstances` with the code `InsufficientInstanceCapacity` leaving exactly the four lines `account: ok (ikigenba.dev, us-east-2, 295229566359)`, `domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)`, `secrets: ok (3 apps)`, and `role: ok (sbx1.ikigenba.dev)` on stdout, the single line `devctl: ec2 RunInstances: InsufficientInstanceCapacity` on stderr, exit 1, and no `AllocateAddress`, `TerminateInstance`, `DeleteRolePolicy`, `DeleteRole`, or `DeleteParameter` call.

- R-9HZD-BOWR: `space create` MUST write the step `address` sixth, after calling `session.Clients.EC2.AllocateAddress(ctx, root.Domain, sp.Domain)` exactly once and then `AssociateAddress` exactly once with the returned `cloud.Address`'s `AllocationID` and the running instance's `ID`, and its detail MUST be `elastic ip `, that address's `IP`, and ` associated`; when either call returns a non-nil error create MUST return it unchanged, write no `address` line, and call `ReleaseAddress`, `TerminateInstance`, and `DeleteRole` not at all; verified at least by reproducing `address: ok (elastic ip 18.118.7.42 associated)`, and by a fake `EC2` whose `AllocateAddress` fails with a `*cloud.Error` for `ec2` `AllocateAddress` with the code `AddressLimitExceeded` giving the single stderr line `devctl: ec2 AllocateAddress: AddressLimitExceeded`, exit 1, and no later call.

- R-EB4J-WN6C: After the address step, every address create uses for records, host connections and its final output MUST be the allocated Elastic IP, even when the launch response carried a different public address.

- R-9J79-PGNG: `space create` MUST write the step `records` seventh, after calling `space.PutRecords(ctx, deps, session.Clients.Route53, zone, sp.Domain, address)` exactly once with the `cloud.Zone` `Route53.Zone` returned and the Elastic IP's `IP` as `address`, and its detail MUST be `created ` followed by the elements of `space.RecordNames(sp.Domain)` joined by `, `, then ` -> `, that `IP`, and `, INSYNC`; verified at least by reproducing `records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)` and `records: ok (created staging.ikigenba.dev, *.staging.ikigenba.dev -> 18.117.42.9, INSYNC)`.

- R-YMOE-CCEO: `space create` MUST write the step `host` after the `records` step, after `space.WaitChecks` for the instance's `ID` returned a nil error, then exactly one call to `(host.Host).Wait`, then exactly one call to `(host.Host).Sudo` with the step `host` and exactly the arguments `cloud-init`, `status`, and `--wait`, the last two on a `host.Host` whose `Address` is the space's address and whose `Deps` is `deps`, and its detail MUST be `status checks passed, cloud-init done`; verified at least by reproducing `host: ok (status checks passed, cloud-init done)`.

- R-VUM8-EVFU: After the `host` line, `space create` MUST, on the same `host.Host`, call `hostsetup.InstallLatest(ctx, h)` exactly once and then `hostsetup.Configure(ctx, h, "opsctl", cfg)` exactly once with a `hostsetup.Config` whose `Root` is `root.Domain`, `Region` is `root.Region`, `ZoneID` is `zone.ID`, `Space` is `sp`, `Email` is the `--acme-email` value verbatim, and `Periods` points at `hostsetup.DefaultBackupPeriods()`, returning either call's error unchanged and writing no `opsctl` line when it is not nil, and MUST then write the step `opsctl` with the detail `<version> installed, <count> keys set`, where `<version>` is the string `InstallLatest` returned, unparsed, and `<count>` is the count `Configure` returned; verified at least by reproducing `10 keys set` with exactly the ten remote argument vectors R-5PA7-AFGB orders for that `Config`, in that order, for the operand `sbx1` with `--acme-email ops@ikigenba.dev` in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}` and a zone whose `ID` is `Z09565073GHK8BYWQ1A78`.

- R-EKVQ-YT3W: The current target MUST contain no embedded opsctl-version pin and no `internal/spacecreate/templates/opsctl-version` file; create’s version MUST be selected from published release data at invocation time.

- R-9LN2-H04U: After the `opsctl` line, `space create` MUST write the step `restore` on every create, after calling `session.Clients.S3.ListObjects(ctx, root.Domain, prefix)` exactly once with `space.BackupPrefix(sp.Label)` followed by `host/` as `prefix`; when the listing is empty its detail MUST be `no host backup` and create MUST pass to `deps.Exec` no `seam.Cmd` whose remote argument vector names `host.apex` or contains `host` followed by `restore`; when the listing is not empty create MUST, on the same `host.Host` and in this order, call `(host.Host).Sudo` with the step `restore` and exactly the arguments `opsctl`, `host`, and `restore`, then `hostsetup.Configure(ctx, h, "restore", cfg)` with a `Config` equal to the `opsctl` step's, then `hostsetup.DelKey(ctx, h, "restore", hostsetup.KeyHostApex)`, returning the first non-nil error unchanged and writing no `restore` line, and its detail MUST be `<key>, <count> keys set again`, where `<key>` is the `Key` of the listed `cloud.Object` with the latest `Modified` — the greater `Key` when two share it — with `space.BackupPrefix(sp.Label)` removed from its front, and `<count>` is the count that second `Configure` returned, the deletion of `host.apex` not being counted; verified at least by reproducing `restore: ok (no host backup)` for an empty listing, and `restore: ok (host/2026-09-12T14:22:51Z.tar.zst, 10 keys set again)` for the operand `staging` against a fake `S3` listing `staging/host/2026-09-11T14:22:51Z.tar.zst` and, with a later `Modified`, `staging/host/2026-09-12T14:22:51Z.tar.zst` under the bucket `ikigenba.dev`, with a recording `Deps.Exec` whose remote argument vectors after the `opsctl` step's ten `sudo opsctl config set` vectors are `sudo opsctl host restore`, the same ten vectors again in the same order, `sudo opsctl config del host.apex`, and then `sudo opsctl init`, and whose `sudo opsctl host restore` process exits 0 with arbitrary standard output that appears nowhere on stdout or stderr.

- R-YSRW-9745: `space create` MUST write the step `init` last and with no detail, after exactly one call to `(host.Host).Sudo` with the step `init` and exactly the arguments `opsctl` and `init`, and MUST return that call's error unchanged when it is not nil; verified at least by reproducing `init: ok` and by reproducing the stderr first line `devctl: init: ssh ec2-user@3.19.79.227 sudo opsctl init: exit status 2` with exit 1.

- R-9MUY-URVJ: Create MUST emit the steps `account`, `domain`, `secrets`, `role`, `instance`, `address`, `records`, `host`, `opsctl`, `restore`, and `init`, in that order and every one of them on every successful create, then, only when every step completed and as the last line of stdout, `sp.Domain`, one space, and the Elastic IP's `IP`, so that the last line names the full space domain whether the label or the domain was typed; a failure MUST stop later work without deleting, releasing, terminating, or otherwise undoing completed effects, and MUST leave every step line already written on stdout; verified at least by reproducing the eleven step lines and the final line `sbx1.ikigenba.dev 18.118.7.42` for both `devctl space create sbx1 --acme-email ops@ikigenba.dev` and `devctl space create sbx1.ikigenba.dev --acme-email ops@ikigenba.dev`, each with exit 0 and empty stderr.
