# D07-space-create

Create validates the domain and prerequisites, writes secrets, provisions a host
with an Elastic IP, installs the newest published opsctl, and sets ten host
keys. Accounts retaining backups restore the host before initialization and
reapply explicit configuration. Failures leave completed work for destroy to
clean up.

The host's role writes TXT records under its own domain and nothing else in
Route 53. That is what proving ownership for its certificate takes, and what an
operator's `opsctl dns add`/`remove` probe of the seam takes. Create writes the
space's A records itself, with the developer's identity, in the `records` step,
so the host never needs an A-record write, and it never needs to enumerate
zones, since opsctl carries the zone id in `dns.zones`.

## REQUIREMENTS

- R-XOJ7-MRN8: Package `internal/spacecreate` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.

- R-XQZ0-EB4M: Package `internal/spacecreate` MUST export `AssumeRolePolicy`, whose value is exactly `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ec2.amazonaws.com"}}]}`.

- R-XS6W-S2VB: Package `internal/spacecreate` MUST export a `RefusedError` struct whose only field is `Message string`, with the methods `Error() string`, returning `Message`, and `ExitCode() int`, returning 2.

- R-V1FO-SE25: `space create`, invoked with `--account` and with no operand, MUST write exactly the three lines `devctl: space create needs <domain>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2.

- R-Y0Q7-GH26: `space create` invoked with more than one operand MUST write exactly the three lines `devctl: space create takes only <domain>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-3M5T-DZ38: The missing-domain and extra-operand refusals above MUST be reported by returning a `*space.UsageError` whose `Message` is that refusal's first line without its `devctl: ` prefix and whose `Help` is `devctl space --help`.

- R-Y4DW-LSA9: `space create` MUST call `checkout.Open` and then `(*Checkout).Apps` before it calls `account.Open`, and when either fails it MUST NOT call `deps.Cloud` at all.

- R-VVFF-8UQ5: `space create` MUST, after the checkout is open, call `account.Open`, then check `<domain>` against `acct.Properties.Domain`, then `(*Account).Zone`, then `(*Account).Delegation` for that zone, then `(*Account).Spaces`, then `acct.Clients.IAM.RoleExists` and `acct.Clients.IAM.InstanceProfileRoles` for `space.RoleName(domain)`, then `(*Account).CallerAccountID`, and MUST complete all of them before it writes any byte to stdout, calls `secrets.Push`, calls any of `PutSecureParameter`, `CreateRole`, `PutRolePolicy`, `CreateInstanceProfile`, `AddRoleToInstanceProfile`, `RunInstance`, `AllocateAddress`, `AssociateAddress`, and `ChangeRecords`, or passes to `deps.Exec` any `seam.Cmd` other than those `internal/checkout` passes.

- R-Y6TP-DBRN: `space create` MUST return a `*RefusedError` whose `Message` is `'<domain>' does not end in the account domain '<acct.Properties.Domain>'` when `<domain>` is neither equal to `acct.Properties.Domain` nor ends in a `.` followed by it, and MUST NOT refuse when it is equal to it, verified at least by reproducing the single stderr line `devctl: 'foo.example.com' does not end in the account domain 'sbx.ikigenba.dev'` with empty stdout and exit 2, and by a run for the domain `ikigenba.dev` in an account whose `domain` property is `ikigenba.dev` reaching its first step line.

- R-Y81L-R3IC: `space create` MUST return a `*RefusedError` whose `Message` is `'<domain>' is delegated away from this account's zone '<zone name>'` when `(*Account).Delegation` for the zone `(*Account).Zone` returned is not the empty string, where `<zone name>` is that zone's `Name`, verified at least by reproducing the single stderr line `devctl: 'foo.sbx.ikigenba.dev' is delegated away from this account's zone 'ikigenba.dev'` with empty stdout and exit 2 for the domain `foo.sbx.ikigenba.dev`, and the same line with `'sbx.ikigenba.dev'` in place of `'foo.sbx.ikigenba.dev'` for the domain `sbx.ikigenba.dev`, in an account whose only zone is `ikigenba.dev` and whose `NS` records there include `sbx.ikigenba.dev`.

- R-Y99I-4V91: `space create` MUST return a `*RefusedError` whose `Message` is `a space at '<domain>' already exists (<that space's ID>)` when one of the `account.Space` values `(*Account).Spaces` returned has `<domain>` as its `Domain`, verified at least by reproducing the single stderr line `devctl: a space at 'foo.sbx.ikigenba.dev' already exists (i-0c9e94542d98846a8)` with empty stdout and exit 2.

- R-3NDP-RQTX: `space create` MUST reject nested space domains using only the non-terminated spaces returned by `(*Account).Spaces`, without consulting checkout or host app names for this check. If `<domain>` ends in `.` plus an existing space's domain other than the account domain, it MUST return `RefusedError` with Message `'<domain>' lies under space '<space domain>'`; if an existing space's domain ends in `.` plus `<domain>` and `<domain>` is not the account domain, it MUST instead return Message `'<domain>' would contain space '<space domain>'`. The first conflicting space in returned order MUST decide the diagnostic. Both refusals MUST have empty stdout, exit 2 and no mutation; tests MUST cover unknown app labels, multiple descendant labels, both creation orders, the account-domain exception, and a name that is only a textual suffix without a label boundary.

- R-YBPA-WEQF: `space create` MUST return a `*RefusedError` whose `Message` is `a role for '<domain>' already exists (<space.RoleName(domain)>)` when `acct.Clients.IAM.RoleExists` or `acct.Clients.IAM.InstanceProfileRoles` for `space.RoleName(domain)` reports existence, verified at least by reproducing the single stderr line `devctl: a role for 'foo.sbx.ikigenba.dev' already exists (ikigenba-space-foo.sbx.ikigenba.dev)` with empty stdout and exit 2, for each of the two reporting existence alone.

- R-VWNB-MMGU: `space create` MUST produce the role's policy document from `PolicyTemplate` by replacing in it every occurrence of `<domain>` with `<domain>`, of `<zone_id>` with the `ID` of the zone `(*Account).Zone` returned, of `<account_id>` with what `(*Account).CallerAccountID` returned, and of `<bucket>` with `acct.Properties.BackupBucket`, and no other text, and MUST read no file to obtain it.

- R-YFD0-1PYI: `space create` MUST write no byte to stdout before `secrets.Push` — called with the apps `(*Checkout).Apps` returned, in the order it returned them — has returned a nil error, MUST return that call's error unchanged when it is not nil, and MUST then write exactly the three lines `account: ok (<acct.Properties.Domain>, <acct.Properties.Region>)`, `domain: ok (zone <zone name> <zone id>)`, and `secrets: ok (<n> apps)`, in that order, where `<n>` is the decimal count of the `secrets.Entry` values `Push` returned; verified at least by reproducing `account: ok (sbx.ikigenba.dev, us-east-2)`, `domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)` and `secrets: ok (3 apps)`, and by reproducing the single stderr line `devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment` with empty stdout, exit 2, and no call to `PutSecureParameter`.

- R-YGKW-FHP7: `space create` MUST write the step `role` fourth, after calling `acct.Clients.IAM.CreateRole` with a `cloud.RoleSpec` whose `Name` is `space.RoleName(domain)`, whose `AssumeRolePolicy` is `AssumeRolePolicy`, and whose `PermissionsBoundaryARN` is `acct.Properties.PermissionsBoundaryARN`, then `PutRolePolicy` with that role name, `space.PolicyName`, and the policy document, then `CreateInstanceProfile` with that same name, then `AddRoleToInstanceProfile` with that name as both the profile and the role, each exactly once and in that order, and its detail MUST be `space.RoleName(domain)`; verified at least by reproducing `role: ok (ikigenba-space-foo.sbx.ikigenba.dev)`.

- R-YHSS-T9FW: `space create` MUST write the step `instance` fifth, after calling `acct.Clients.EC2.RunInstance` exactly once with a `cloud.LaunchSpec` whose `LaunchTemplateID` is `acct.Properties.LaunchTemplateID`, whose `InstanceProfile` is `space.RoleName(domain)`, and whose `Space` is `<domain>`, and then `space.WaitState` for that instance's `ID` and `cloud.StateRunning`, and its detail MUST be that running instance's `ID`, ` running, `, and its `Address`; verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)`.

- R-YMOE-CCEO: `space create` MUST write the step `host` after the `records` step, after `space.WaitChecks` for the instance's `ID` returned a nil error, then exactly one call to `(host.Host).Wait`, then exactly one call to `(host.Host).Sudo` with the step `host` and exactly the arguments `cloud-init`, `status`, and `--wait`, the last two on a `host.Host` whose `Address` is the space's address and whose `Deps` is `deps`, and its detail MUST be `status checks passed, cloud-init done`; verified at least by reproducing `host: ok (status checks passed, cloud-init done)`.

- R-YSRW-9745: `space create` MUST write the step `init` last and with no detail, after exactly one call to `(host.Host).Sudo` with the step `init` and exactly the arguments `opsctl` and `init`, and MUST return that call's error unchanged when it is not nil; verified at least by reproducing `init: ok` and by reproducing the stderr first line `devctl: init: ssh ec2-user@3.19.79.227 sudo opsctl init: exit status 2` with exit 1.

- R-YTZS-MYUU: `space create` MUST write, only when every step completed and as the last line of stdout, the `<domain>` operand, one space, and the space's address.

- R-E2L9-88ZH: Package `internal/spacecreate` MUST export `PolicyTemplate string`, embedding `internal/spacecreate/templates/space-role-policy.json`.

- R-3OLM-5IKM: `devctl space create --help` and `devctl space create -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space create <domain> --acme-email <address>

  Create the space with secrets, a role, an instance, an Elastic IP and DNS records.
  Install the newest published opsctl, set its host configuration and run init.
  When the account keeps backups, restore its own host backup before init.
  Completed steps remain on failure; use space destroy to clean up.

  Options:
    --acme-email <address>   ACME contact address stored on the host; required
  ```

- R-E511-ZSGV: `space create` MUST take exactly one domain and required `--acme-email <address>` or `--acme-email=<address>`, accepting the option before or after the operand and taking the last occurrence; only help is otherwise accepted, and `--elastic-ip` MUST be refused as unknown.

- R-E7GU-RBY9: An unknown option of create MUST produce `space.UsageError` with Message `unknown option '<option>'` and Help `devctl space --help`, empty stdout and exit 2; a consumed acme-email value MUST not be treated as an option.

- R-E8OR-53OY: After operand arity is checked, create without `--acme-email` MUST return a `space.UsageError` with message `space create needs --acme-email <address>` and help `devctl space --help`; a missing or empty option value MUST instead say `option '--acme-email' requires a value`. These failures MUST precede all account, checkout and process access.

- R-E9WN-IVFN: Every successful create MUST allocate exactly one tagged Elastic IP and associate it with the new instance, then write the address step `address: ok (elastic ip <IP> associated)`; allocation or association failure MUST stop subsequent steps and preserve earlier resources.

- R-EB4J-WN6C: After the address step, every address create uses for records, host connections and its final output MUST be the allocated Elastic IP, even when the launch response carried a different public address.

- R-ECCG-AEX1: `space create` MUST write the step `records` after the `address` step, after calling `space.PutRecords` exactly once with the zone `(*Account).Zone` returned, `<domain>`, and the space's address, and its detail MUST be `created ` followed by the elements of `space.RecordNames(domain)` joined by `, `, then ` -> `, the space's address, and `, INSYNC`; verified at least by reproducing `records: ok (created foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.19.79.227, INSYNC)` and `records: ok (created staging.ikigenba.dev, *.staging.ikigenba.dev -> 18.220.10.5, INSYNC)`.

- R-EDKC-O6NQ: After host readiness, create MUST call `hostsetup.InstallLatest`, set all ten keys through `hostsetup.Configure`, and report `opsctl: ok (<version> installed, 10 keys set)` using the selected release version; no pinned version or build of opsctl from this checkout MUST be used.

- R-EES9-1YEF: In an account with `DeleteBackupsOnDestroy` false, create MUST list only `<domain>/host/` in the account backup bucket after configuring opsctl and before init; an empty listing MUST produce `restore: ok (no host backup)` and no host restore call.

- R-93SF-DMH9: When that host-backup listing is nonempty, create MUST run `sudo opsctl host restore` with step `restore`, then set the same ten keys again through `hostsetup.Configure`, preserving all other host keys; it MUST report `restore: ok (<host-relative backup key>, 10 keys set again)` only after both operations succeed. The reported backup key MUST identify the backup the published restore interface selected, verified at least by reproducing `restore: ok (host/2026-09-12T14:22:51Z.tar.zst, 10 keys set again)` when that was the selected backup; the listing alone MUST NOT be treated as evidence of which backup was restored.

- R-EH81-THVT: In an account with `DeleteBackupsOnDestroy` true, create MUST omit the restore step and MUST NOT list or restore host backups.

- R-EIFY-79MI: Create MUST emit `account`, `domain`, `secrets`, `role`, `instance`, `address`, `records`, `host`, `opsctl`, optional retention-dependent `restore`, and `init` steps in that order, then its domain/address line; failure MUST stop later work without deleting, releasing, terminating, or otherwise undoing completed effects.

- R-EJNU-L1D7: The policy created from `PolicyTemplate` MUST allow host object reads and writes only below its own `<domain>/` prefix in the named bucket and restrict bucket listing to that prefix; in particular install MUST be able to read `<domain>/deploy/` and MUST gain no access to another space’s prefix.

- R-EKVQ-YT3W: The current target MUST contain no embedded opsctl-version pin and no `internal/spacecreate/templates/opsctl-version` file; create’s version MUST be selected from published release data at invocation time.

- R-F3DQ-ZZV8: The policy produced from `PolicyTemplate` MUST grant `ssm:GetParameter` on `arn:aws:ssm:*:<account_id>:parameter/ikigenba/<domain>/*`, allowing the host to read each app's secrets object that create and secrets push write. It MUST grant no parameter reads outside that space prefix and no parameter writes.

- R-CAAK-IWJI: The policy produced from `PolicyTemplate` MUST grant exactly three Route 53 permissions and no other `route53:` action: `route53:ListResourceRecordSets` on `arn:aws:route53:::hostedzone/<zone_id>`, `route53:GetChange` on `arn:aws:route53:::change/*`, and `route53:ChangeResourceRecordSets` on `arn:aws:route53:::hostedzone/<zone_id>` under a `ForAllValues:StringEquals` condition requiring `route53:ChangeResourceRecordSetsRecordTypes` to equal `TXT` and a `ForAllValues:StringLike` condition requiring `route53:ChangeResourceRecordSetsNormalizedRecordNames` to match `<domain>` or `*.<domain>`. Tests MUST inspect the substituted JSON policy and verify that a TXT record at `_acme-challenge.<domain>` is writable, that the `<domain>` and `*.<domain>` A records are not, and that `route53:ListHostedZones` is absent.
