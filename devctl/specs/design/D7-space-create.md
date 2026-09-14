# D7-space-create

`space create` makes a fresh, complete copy of the platform at a domain: a role
for the space, every app's secrets, an instance, its records, and `opsctl` on
the host, ready for a `deploy`. It is the heaviest command in the project —
seven AWS services, the developer's keyring, the Go toolchain, `scp`, and four
remote commands — which is why `internal/spacecreate` is its own package, as
D1's layout says, and why it is its own design.

Almost nothing here is new machinery. `seam.Deps`, `cli.Run` and `seam.Cmd` are
D1's; the grammar, the diagnostic shape, the four exit codes and the one
missing-`--account` rule are D2's; the account, its properties, the zone, the
delegation, the spaces and the whole AWS surface are D3's; the checkout, the
apps, `etc/manifest.toml` and the keyring are D4's; the secrets object and its
only writer are D5's; the step line, the space verbs — `RoleName`, `PolicyName`,
`RecordNames`, `RecordTTL`, `WaitState`, `WaitChecks`, `ElasticIP`,
`PutRecords` — the `UsageError`, and `host.Host` with its `ec2-user` identity,
its `sudo` prefix and its remote-exit rendering are D6's. This design composes
them. What it adds is the order, the ten steps' bytes, the refusals that keep a
space from being created where one must not be, the role document, and the
installation of `opsctl`.

**Ten steps, one line each.** The story fixes them and their order: `account`,
`domain`, `secrets`, `role`, `instance`, `records`, `host`, `opsctl` and `init`,
each naming itself and reporting `ok` with a short detail, and then a last line
that is the domain and the space's address. `--elastic-ip` inserts an eleventh
line, `address`, between `instance` and `records`.

The `address` step goes there because that is the order the work has to happen
in: the Elastic IP cannot be associated before there is an instance, and the
records must not be written before the address they point at is final. It is
also why the `instance` step reports the launch's own address while `records`
and the last line report the Elastic IP: the association replaces the instance's
public address, so from the `address` step onwards **the space's address is the Elastic IP**, and
that is the address `ssh` is given too.

**Create is not transactional, and that is a contract.** The part-way story
says so in its postconditions: the steps that printed `ok` hold, `space destroy`
removes what exists, and a second `create` is refused. So a failed step prints
nothing, the command stops there, and nothing already created is undone — no
role deleted, no parameter removed, no instance terminated, no address released.
A developer who reads four `ok` lines and a diagnostic knows exactly what is in
the account, and `destroy` — which is idempotent and resumable by design (D6) —
is the one way back. A rollback would be a second, untested half of this command
that runs only when something has already gone wrong.

**Everything that can refuse, refuses before anything is created.** Five of the
story's failures print nothing on stdout and exit 2, including the one where a
secret is missing from the keyring — which is after the account, the zone and
the apps have all been read successfully. So the whole preflight runs before the
first step line, in this order:

1. the arguments — `space create needs <domain>`, and no AWS call is made;
2. `checkout.Open` and `Apps()` — local and free, like `secrets push` (D5), so a
   broken manifest is refused before `Deps.Cloud` is called at all;
3. `account.Open` — the properties, and the clients in their region;
4. `<domain>` against `acct.Properties.Domain` — equal to it, or ending in a dot
   followed by it;
5. `acct.Zone` — the zone the records will be written in; a domain with no zone
   in the account is D3's `*NoZoneError`, rendered by D6's rule;
6. `acct.Delegation` — a domain the zone delegates away belongs to another
   account;
7. `acct.Spaces` — is there already a space at this domain, and is this domain
   where an app of some existing space answers;
8. `acct.Clients.IAM` — is the space's role already there, which is what makes
   the part-way story's "again is refused because the role exists" real;
9. `acct.CallerAccountID` — the last input the role needs. The policy itself is
   in the binary, so there is nothing to read for it.

Then, and only then, the steps run. The keyring is the one preflight input this
command does not read for itself: D5's `Push` gathers every value of every app
before it writes anything, so a missing value is already an all-or-nothing
refusal, and carrying a second copy of the gathering here would read the
keyring twice. The cost is that the `secrets` step is the first step to do
work, so **no byte reaches stdout until `secrets.Push` has returned**: the
`account` and `domain` lines report facts the preflight already holds and are
written when the secrets step completes, immediately before `secrets: ok`. The
developer sees the ten lines in the story's order either way; what they never
see is `account: ok` followed by a keyring refusal.

**Six refusals, six messages.** Five are the story's bytes; one is this
design's, for the failure the part-way story promises but shows no output for.
They cover a domain that does not end in the account's, a domain the zone
delegates away, a domain where an app of an existing space already answers, a
space that already exists at the domain, a role that already exists for it, and
a secret with no value in the keyring.

All six are exit 2 — the developer's own input was wrong and nothing was
attempted. The keyring one is D4's `*keyring.NoValueError`, rendered by D4's
rule; the other five are one `*RefusedError` whose message each requirement
below fixes, so `cli.Run` renders them through D5's `ExitCode()` mechanism and
this package owns no stderr.

Two of those deserve a note. The **delegation** refusal covers two stories with
one rule: `create sbx.ikigenba.dev` in the account that owns `ikigenba.dev` and
`create foo.sbx.ikigenba.dev` in the same account both find the zone
`ikigenba.dev` and a `NS` record that delegates `sbx.ikigenba.dev` away, so both
print the same sentence naming the zone devctl would otherwise have written in.
The **app answers** refusal is why this command needs the checkout before it
needs the account: `crm.foo.sbx.ikigenba.dev` is a free domain as far as the
tags are concerned, but it is where `crm` on the space `foo.sbx.ikigenba.dev`
answers, and a space there would take that name away from a deployed app. The
check is the cross product of the account's spaces and the checkout's apps,
which is why the apps are in hand before the refusal can be decided.

**The role's policy is devctl's own, and it is in the binary.** The space role
is created per space, at runtime, by this command — Terraform cannot own a role
it never makes — so the policy belongs to the thing that creates it. It is
hand-written as `internal/spacecreate/templates/space-role-policy.json` and
embedded with `go:embed`, which is why `PolicyTemplate` is a document and not a
path: nothing is read at run time, so a space can be created from an installed
binary with no checkout of devctl's own source, and there is no missing-file
failure to design for. The file is hand-maintained; only the four placeholders
it must carry are contract.

devctl substitutes those four into it: `<domain>`, `<zone_id>`, `<account_id>`,
and `<bucket>`. Every one appears more than once in the real file — `<domain>`
in the SSM parameter ARN, both S3 resources, the `s3:prefix` condition and the
two Route 53 record-name conditions — so the substitution is every occurrence,
not the first. What infra still owns is the cap on all of it:
`acct.Properties.PermissionsBoundaryARN`, which bounds whatever this policy
grants.

The trust policy is not in that file and is not in the checkout at all, because
nothing else needs it: it is the one document every EC2 instance role carries,
so this package declares it as a constant. The boundary is the account's
`permissions_boundary_arn`, and the instance profile has the same name as the
role, which is what `RunInstance` is given.

**opsctl is an installed tool, fetched from its own release.** The host's first
boot installs packages and nothing else (`infra/templates/space-first-boot.sh`),
so the host learns what it is from devctl. opsctl is not built here. It is a
separate project with its own release, and devctl consumes that release the way
it consumes `ssh`, `scp` and `git` — by name and by published interface, never
by reaching into its source tree. `space create` downloads opsctl's installer
for the version it requires onto the host, runs it as root, sets the six
configuration keys one `opsctl config set` at a time, and finishes with
`opsctl init`. Where the binary lands, what the installer verifies, and where it
leaves a copy of itself so the host can be moved forward later are opsctl's
business; devctl invokes `opsctl` by bare name afterwards, on whatever PATH the
installer arranged.

**The version is devctl's constant, not the host's answer.** devctl depends on
opsctl's grammar — the six keys below, `init`, and `install`, `restore` and
`status` in D6 and D9 — so the version those requirements assume belongs beside
them, pinned in this design the way a module version is pinned in `go.mod`.
Moving spaces to a newer opsctl is an edit here, visible in one place. That is
also why the `v0.1.0` in `opsctl: ok (v0.1.0 installed, 6 keys set)` is
`OpsctlVersion` read back rather than `opsctl version` parsed: reading another
project's output to learn something devctl already decided is a dependency
devctl does not need. Which release that is, is data — a one-line file beside
the policy template, embedded the same way — so moving every new space to a
newer opsctl is an edit to that file, not to this design.

`opsctl config set` and `opsctl init` are real designed commands, used here with
their real syntax: `set` takes one `KEY=VALUE` argument and splits it on the
first `=` (opsctl's D3), and `init` takes none (opsctl's D5). Both need root,
as every opsctl command but `--help` and `version` does, so both go through
`(host.Host).Sudo`. The installer needs root as well; fetching it does not.

The six keys are the story's postcondition, and their values are the account's
and the zone's:

| key | value |
|---|---|
| `host.name` | `<domain>` |
| `dns.provider` | `route53` |
| `dns.zones` | `<zone name>:<zone id>` — the `NAME:ID` pair opsctl's D4 parses |
| `backup.full_seconds` | `acct.Properties.BackupFullSeconds` |
| `backup.incremental_seconds` | `acct.Properties.BackupIncrementalSeconds` |
| `backup.wal_seconds` | `acct.Properties.BackupWALSeconds` |

`init: ok` has no detail because `opsctl init` has nothing devctl can add: it
prints its own preflight, one line per check, and devctl neither relays nor
parses it. A failed `init` is D6's `*host.CommandError` — `devctl: init: ssh
ec2-user@3.19.79.227 sudo opsctl init: exit status 2` — whose second block is
the remote *stderr*, which opsctl's design leaves empty for this failure;
`specs/issues/opsctl-init-reports-checks-on-stdout.md` records that.

**The waits are D6's, the clock is `Deps.After`.** `WaitState` for `running`,
`WaitChecks` for the status checks, `PutRecords`' own wait for `INSYNC`, and
`(host.Host).Wait` for ssh to answer. Nothing in this design polls for itself,
and `cloud-init status --wait` does the last of the waiting on the host's side
of the connection, inside one `ssh`. A test injects an `After` that fires
immediately and the whole command runs in microseconds with no clock, no AWS,
no host and no Go toolchain.

**The usage text.** `devctl space --help` promises
`devctl space <subcommand> --help` and D6 declares the other five texts, so
`create`'s is here. The story supplies none, so it is authored in the same
voice; it is declared byte for byte in the requirements below.

**The grammar, and its refusals.** `space create <domain> [--elastic-ip]`, one
operand and one option of its own. D6 fixes the shape of the other
subcommands' refusals and exempts `create` from its option rule, so the three
messages `space create needs <domain>`, `space create takes only <domain>` and
`unknown option '--force'` are this design's, in D6's shape: one
`*space.UsageError` whose `Help` is `devctl space --help`, never the
subcommand's own help.

**What is deliberately not here.** The other five subcommands, the step line
itself, the space verbs and the host are D6's; the secrets object is D5's; the
checkout, the manifest and the keyring are D4's; `<app>/dist/<app>-<tag>.tar.xz` and
`build`'s clean-tree rule are D8's; deploying an app onto the space this
command made is D9's.

## REQUIREMENTS

- R-XOJ7-MRN8: Package `internal/spacecreate` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.
- R-DKIB-S2JM: Package `internal/spacecreate` MUST export `PolicyTemplate string`, holding the contents of the file `internal/spacecreate/templates/space-role-policy.json` embedded into the binary at build time; `OpsctlVersion string`, holding the contents of the file `internal/spacecreate/templates/opsctl-version` embedded at build time with its trailing newlines removed; `OpsctlReleaseBase = "https://github.com/ikigenba/ikigenba/releases/download/opsctl"`; `OpsctlInstaller = "/tmp/opsctl-install"`; and `DNSProvider = "route53"`.
- R-XQZ0-EB4M: Package `internal/spacecreate` MUST export `AssumeRolePolicy`, whose value is exactly `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ec2.amazonaws.com"}}]}`.
- R-XS6W-S2VB: Package `internal/spacecreate` MUST export a `RefusedError` struct whose only field is `Message string`, with the methods `Error() string`, returning `Message`, and `ExitCode() int`, returning 2.
- R-TM5B-PJ2X: `devctl --account <name> space create --help` and `devctl --account <name> space create -h` MUST each print exactly this text, once, to stdout, write nothing to stderr, and exit 0, and MUST do the same with no `--account` and with a `<domain>` operand also present:

  ```
  Usage: devctl --account <name> space create <domain> [--elastic-ip]

  Create the space at <domain>: its secrets, its role, its instance, its records,
  and opsctl on the host, ready for a deploy. <domain> is the space's one
  identifier, typed in full; it must end in the account's domain property, or
  equal it for the apex space.

  Each line of output is one step; the last line is the domain and the address.
  Nothing is undone when a step fails: what the earlier steps did stays done, and
  'devctl space destroy <domain>' is how it is cleaned up.

  Arguments:
    <domain>   the domain the space answers at

  Options:
    --elastic-ip   allocate an Elastic IP and point the records at it, so the
                   address survives a stop
  ```
- R-XYAE-OXKS: `space create` MUST take exactly one `<domain>` operand and MUST accept no option other than `--elastic-ip`, `--help`, and `-h`; `--elastic-ip` MUST be accepted whether it precedes or follows the `<domain>` operand; and repeating it MUST have the same effect as giving it once.
- R-V1FO-SE25: `space create`, invoked with `--account` and with no operand, MUST write exactly the three lines `devctl: space create needs <domain>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, call `deps.Cloud` not at all, pass no `seam.Cmd` to `deps.Exec`, and exit 2.
- R-Y0Q7-GH26: `space create` invoked with more than one operand MUST write exactly the three lines `devctl: space create takes only <domain>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.
- R-Y1Y3-U8SV: An argument of `space create` that begins with `-` and is none of `--elastic-ip`, `--help`, and `-h` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl space --help' for usage` to be written to stderr, nothing to stdout, and exit 2.
- R-Y360-80JK: Each of the three refusals above MUST be reported by returning a `*space.UsageError` whose `Message` is that refusal's first line without its `devctl: ` prefix and whose `Help` is `devctl space --help`.
- R-Y4DW-LSA9: `space create` MUST call `checkout.Open` and then `(*Checkout).Apps` before it calls `account.Open`, and when either fails it MUST NOT call `deps.Cloud` at all.
- R-VVFF-8UQ5: `space create` MUST, after the checkout is open, call `account.Open`, then check `<domain>` against `acct.Properties.Domain`, then `(*Account).Zone`, then `(*Account).Delegation` for that zone, then `(*Account).Spaces`, then `acct.Clients.IAM.RoleExists` and `acct.Clients.IAM.InstanceProfileRoles` for `space.RoleName(domain)`, then `(*Account).CallerAccountID`, and MUST complete all of them before it writes any byte to stdout, calls `secrets.Push`, calls any of `PutSecureParameter`, `CreateRole`, `PutRolePolicy`, `CreateInstanceProfile`, `AddRoleToInstanceProfile`, `RunInstance`, `AllocateAddress`, `AssociateAddress`, and `ChangeRecords`, or passes to `deps.Exec` any `seam.Cmd` other than those `internal/checkout` passes.
- R-Y6TP-DBRN: `space create` MUST return a `*RefusedError` whose `Message` is `'<domain>' does not end in the account domain '<acct.Properties.Domain>'` when `<domain>` is neither equal to `acct.Properties.Domain` nor ends in a `.` followed by it, and MUST NOT refuse when it is equal to it, verified at least by reproducing the single stderr line `devctl: 'foo.example.com' does not end in the account domain 'sbx.ikigenba.dev'` with empty stdout and exit 2, and by a run for the domain `ikigenba.dev` in an account whose `domain` property is `ikigenba.dev` reaching its first step line.
- R-Y81L-R3IC: `space create` MUST return a `*RefusedError` whose `Message` is `'<domain>' is delegated away from this account's zone '<zone name>'` when `(*Account).Delegation` for the zone `(*Account).Zone` returned is not the empty string, where `<zone name>` is that zone's `Name`, verified at least by reproducing the single stderr line `devctl: 'foo.sbx.ikigenba.dev' is delegated away from this account's zone 'ikigenba.dev'` with empty stdout and exit 2 for the domain `foo.sbx.ikigenba.dev`, and the same line with `'sbx.ikigenba.dev'` in place of `'foo.sbx.ikigenba.dev'` for the domain `sbx.ikigenba.dev`, in an account whose only zone is `ikigenba.dev` and whose `NS` records there include `sbx.ikigenba.dev`.
- R-Y99I-4V91: `space create` MUST return a `*RefusedError` whose `Message` is `a space at '<domain>' already exists (<that space's ID>)` when one of the `account.Space` values `(*Account).Spaces` returned has `<domain>` as its `Domain`, verified at least by reproducing the single stderr line `devctl: a space at 'foo.sbx.ikigenba.dev' already exists (i-0c9e94542d98846a8)` with empty stdout and exit 2.
- R-YAHE-IMZQ: `space create` MUST return a `*RefusedError` whose `Message` is `'<domain>' is where app '<app>' of space '<space domain>' answers` when `<domain>` equals an app's `Name`, a `.`, and the `Domain` of a space that `(*Account).Spaces` returned — taking the first such pair in the order `Spaces` returned the spaces and `Apps` returned the apps — verified at least by reproducing the single stderr line `devctl: 'crm.foo.sbx.ikigenba.dev' is where app 'crm' of space 'foo.sbx.ikigenba.dev' answers` with empty stdout and exit 2.
- R-YBPA-WEQF: `space create` MUST return a `*RefusedError` whose `Message` is `a role for '<domain>' already exists (<space.RoleName(domain)>)` when `acct.Clients.IAM.RoleExists` or `acct.Clients.IAM.InstanceProfileRoles` for `space.RoleName(domain)` reports existence, verified at least by reproducing the single stderr line `devctl: a role for 'foo.sbx.ikigenba.dev' already exists (ikigenba-space-foo.sbx.ikigenba.dev)` with empty stdout and exit 2, for each of the two reporting existence alone.
- R-VWNB-MMGU: `space create` MUST produce the role's policy document from `PolicyTemplate` by replacing in it every occurrence of `<domain>` with `<domain>`, of `<zone_id>` with the `ID` of the zone `(*Account).Zone` returned, of `<account_id>` with what `(*Account).CallerAccountID` returned, and of `<bucket>` with `acct.Properties.BackupBucket`, and no other text, and MUST read no file to obtain it.
- R-YFD0-1PYI: `space create` MUST write no byte to stdout before `secrets.Push` — called with the apps `(*Checkout).Apps` returned, in the order it returned them — has returned a nil error, MUST return that call's error unchanged when it is not nil, and MUST then write exactly the three lines `account: ok (<acct.Properties.Domain>, <acct.Properties.Region>)`, `domain: ok (zone <zone name> <zone id>)`, and `secrets: ok (<n> apps)`, in that order, where `<n>` is the decimal count of the `secrets.Entry` values `Push` returned; verified at least by reproducing `account: ok (sbx.ikigenba.dev, us-east-2)`, `domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)` and `secrets: ok (3 apps)`, and by reproducing the single stderr line `devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment` with empty stdout, exit 2, and no call to `PutSecureParameter`.
- R-YGKW-FHP7: `space create` MUST write the step `role` fourth, after calling `acct.Clients.IAM.CreateRole` with a `cloud.RoleSpec` whose `Name` is `space.RoleName(domain)`, whose `AssumeRolePolicy` is `AssumeRolePolicy`, and whose `PermissionsBoundaryARN` is `acct.Properties.PermissionsBoundaryARN`, then `PutRolePolicy` with that role name, `space.PolicyName`, and the policy document, then `CreateInstanceProfile` with that same name, then `AddRoleToInstanceProfile` with that name as both the profile and the role, each exactly once and in that order, and its detail MUST be `space.RoleName(domain)`; verified at least by reproducing `role: ok (ikigenba-space-foo.sbx.ikigenba.dev)`.
- R-YHSS-T9FW: `space create` MUST write the step `instance` fifth, after calling `acct.Clients.EC2.RunInstance` exactly once with a `cloud.LaunchSpec` whose `LaunchTemplateID` is `acct.Properties.LaunchTemplateID`, whose `InstanceProfile` is `space.RoleName(domain)`, and whose `Space` is `<domain>`, and then `space.WaitState` for that instance's `ID` and `cloud.StateRunning`, and its detail MUST be that running instance's `ID`, ` running, `, and its `Address`; verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)`.
- R-YJ0P-716L: With `--elastic-ip`, `space create` MUST write the step `address` sixth, after calling `acct.Clients.EC2.AllocateAddress` with `<domain>` and then `AssociateAddress` with that `cloud.Address`'s `AllocationID` and the instance's `ID`, each exactly once, and its detail MUST be `elastic ip `, that address's `IP`, and ` associated`; without `--elastic-ip` it MUST write no `address` step line and MUST call neither `AllocateAddress` nor `AssociateAddress`; verified at least by reproducing `address: ok (elastic ip 18.220.10.5 associated)`.
- R-YK8L-KSXA: The space's address MUST be the `IP` of the `cloud.Address` the `address` step allocated when `--elastic-ip` was given and the running instance's `Address` when it was not, and every later use of an address in this command — the records, the `host.Host` it reaches the host with, and the last line of stdout — MUST use that one value; verified at least by an `--elastic-ip` run whose launched instance reports `3.15.44.201` and whose Elastic IP is `18.220.10.5` writing `instance: ok (i-0a1b2c3d4e5f60718 running, 3.15.44.201)` and then using `18.220.10.5` in its `records` line, in every `seam.Cmd` it passes to `deps.Exec` for `ssh` and `scp`, and in its last line.
- R-YLGH-YKNZ: `space create` MUST write the step `records` after the `instance` step and any `address` step, after calling `space.PutRecords` exactly once with the zone `(*Account).Zone` returned, `<domain>`, and the space's address, and its detail MUST be the elements of `space.RecordNames(domain)` joined by `, `, then ` -> `, the space's address, and `, INSYNC`; verified at least by reproducing `records: ok (foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.19.79.227, INSYNC)` and `records: ok (staging.ikigenba.dev, *.staging.ikigenba.dev -> 18.220.10.5, INSYNC)`.
- R-YMOE-CCEO: `space create` MUST write the step `host` after the `records` step, after `space.WaitChecks` for the instance's `ID` returned a nil error, then exactly one call to `(host.Host).Wait`, then exactly one call to `(host.Host).Sudo` with the step `host` and exactly the arguments `cloud-init`, `status`, and `--wait`, the last two on a `host.Host` whose `Address` is the space's address and whose `Deps` is `deps`, and its detail MUST be `status checks passed, cloud-init done`; verified at least by reproducing `host: ok (status checks passed, cloud-init done)`.
- R-DMY4-JM10: After the `host` step and before the `opsctl` step line, `space create` MUST call `(host.Host).Run` with the step `opsctl` and exactly the arguments `curl`, `-fsSL`, `-o`, `OpsctlInstaller`, and `OpsctlReleaseBase` followed by `/`, `OpsctlVersion`, and `/install.sh`; then `(host.Host).Sudo` with the step `opsctl` and exactly the arguments `bash`, `OpsctlInstaller`, and `OpsctlVersion`; each exactly once and in that order; MUST return either call's error unchanged when it is not nil; and MUST pass to `deps.Exec` no `seam.Cmd` whose `Path` is `go`; verified at least by the recorded `seam.Cmd` values reproducing both argument lists for the `OpsctlVersion` the binary under test was built with, and by reproducing a stderr first line of `devctl: opsctl: ssh ec2-user@3.19.79.227 sudo bash /tmp/opsctl-install ` followed by `OpsctlVersion` and `: exit status 1`, with exit 1.
- R-YQC3-HNMR: `space create` MUST then call `(host.Host).Sudo` with the step `opsctl` exactly six times, each with exactly the arguments `opsctl`, `config`, `set`, and one `<key>=<value>` argument, in this order of keys: `host.name` with `<domain>`, `dns.provider` with `DNSProvider`, `dns.zones` with the zone's `Name`, a `:`, and the zone's `ID`, `backup.full_seconds` with `acct.Properties.BackupFullSeconds`, `backup.incremental_seconds` with `acct.Properties.BackupIncrementalSeconds`, and `backup.wal_seconds` with `acct.Properties.BackupWALSeconds`, each period rendered in decimal; verified at least by the recorded `seam.Cmd` values holding `opsctl config set host.name=foo.sbx.ikigenba.dev` and `opsctl config set dns.zones=sbx.ikigenba.dev:Z02587302QXWONVKW632` after `sudo`.
- R-DO60-XDRP: `space create` MUST write the step `opsctl` after those six calls, and its detail MUST be `OpsctlVersion`, ` installed, `, the decimal count of those `config set` calls, and ` keys set`; MUST NOT invoke `opsctl version` on the host; and MUST NOT read the installed version from any process's output; verified at least by reproducing a line of `opsctl: ok (` followed by `OpsctlVersion` and ` installed, 6 keys set)`.
- R-YSRW-9745: `space create` MUST write the step `init` last and with no detail, after exactly one call to `(host.Host).Sudo` with the step `init` and exactly the arguments `opsctl` and `init`, and MUST return that call's error unchanged when it is not nil; verified at least by reproducing `init: ok` and by reproducing the stderr first line `devctl: init: ssh ec2-user@3.19.79.227 sudo opsctl init: exit status 2` with exit 1.
- R-YTZS-MYUU: `space create` MUST write, only when every step completed and as the last line of stdout, the `<domain>` operand, one space, and the space's address.
- R-YWFL-EIC8: `space create` MUST write the steps `account`, `domain`, `secrets`, `role`, `instance`, `address` — only with `--elastic-ip` — `records`, `host`, `opsctl`, and `init`, in that order and no others; MUST attempt no later step once one has failed; MUST exit 0 when every step completed; and MUST delete, release, terminate, or otherwise undo nothing it created when a step fails; verified at least by reproducing the ten stdout lines of the plain `space create` story ending in `foo.sbx.ikigenba.dev 3.19.79.227`, the eleven of the `--elastic-ip` story ending in `staging.ikigenba.dev 18.220.10.5`, and the ten of the apex story ending in `ikigenba.dev 3.18.9.77`, each with empty stderr and exit 0, and by reproducing the four stdout lines of the part-way story with the single stderr line `devctl: ec2 RunInstances: InsufficientInstanceCapacity`, exit 1, no further stdout line, and no call to `TerminateInstance`, `ReleaseAddress`, `DeleteParameter`, `DeleteRolePolicy`, `RemoveRoleFromInstanceProfile`, `DeleteInstanceProfile`, or `DeleteRole`.
