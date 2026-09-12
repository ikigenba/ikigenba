# Stories — space lifecycle

A space is one running copy of the platform: one EC2 instance in one account,
named by its full domain, found by its tags. `space` is the command that lists,
creates, destroys, stops, and starts them, and asks one what it is running. Nothing is kept on the developer's
machine; the cloud is the registry.

## A developer asks what `space` can do

The top-level usage gains the line `  space     list, create, destroy, stop,
start, and inspect spaces` under `Commands:`.

Command:

```
$ devctl space --help
```

Output:

```
Usage: devctl --account <name> space <subcommand> [arguments]

List, create, destroy, stop, start, and inspect spaces in one account. A space
is one instance named by its full domain; the cloud's tags are the only
registry.

Subcommands:
  list                            one line per space in the account
  create <domain> [--elastic-ip]  create the space at <domain>
  destroy <domain>                remove the space and everything it owned
  stop <domain>                   stop the instance; state is kept
  start <domain>                  start the instance and point its records at it
  status <domain>                 one line per app on the space: version and service state

Every subcommand needs --account. Run 'devctl space <subcommand> --help' for details.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer asks which spaces exist in an account

A developer about to create, deploy to, or clean up a space checks what is
there first. One line per space, sorted by domain: the domain, the instance
state, the public address or `-`. An account with no spaces prints nothing.
What a space is running is `space status`, asked of the host.

Command:

```
$ devctl --account 602773793009 space list
```

Output:

```
bar.sbx.ikigenba.dev stopped -
foo.sbx.ikigenba.dev running 3.19.79.227
new.sbx.ikigenba.dev running 18.220.10.5
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties: the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_full_seconds`,
  `backup_incremental_seconds`, and `backup_wal_seconds`.
- Three instances tagged `Project=ikigenba` with a `Space` tag exist in the
  account's region and are not terminated. A space's instance is the one
  tagged `Space=<domain>` that is not terminated; there is at most one.

Postconditions:

- Nothing has changed.
- Instances were found by `DescribeInstances` with filters
  `tag:Project=ikigenba` and `tag-key=Space`, in the region the account's
  properties name.

## A developer runs a `space` subcommand without naming the account

Command:

```
$ devctl space list
```

Output:

```
devctl: --account is required

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer lists an account that has no properties

Command:

```
$ devctl --account 602773793009 space list
```

Output:

```
devctl: ssm GetParameter /ikigenba/account: ParameterNotFound
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- No parameter `/ikigenba/account` in the account.

Postconditions:

- Nothing has changed.

## A developer creates a space

A developer wants a fresh, complete copy of the platform at a domain of their
choosing, ready for a deploy. `<domain>` is the space's one identifier, its
full domain, typed in full every time: `foo.sbx.ikigenba.dev`,
`staging.ikigenba.dev`, or the account domain itself for the apex space.
Without `--elastic-ip` the address is whatever the launch assigned; `start`
re-points the records after a stop. Each line of output is one step; the last
line is the domain and the address.

Command:

```
$ devctl --account 602773793009 space create foo.sbx.ikigenba.dev
```

Output:

```
account: ok (sbx.ikigenba.dev, us-east-2)
domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)
secrets: ok (3 apps)
role: ok (ikigenba-space-foo.sbx.ikigenba.dev)
instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)
records: ok (foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.19.79.227, INSYNC)
host: ok (status checks passed, cloud-init done)
opsctl: ok (v0.1.0 installed, 6 keys set)
init: ok
foo.sbx.ikigenba.dev 3.19.79.227
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties, the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_full_seconds`,
  `backup_incremental_seconds`, and `backup_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` ends in the account's `domain` property.
- No instance is tagged `Space=<domain>` in the account.
- `infra/templates/space-role-policy.json` is in the checkout.
- Every app in the checkout has the values its `etc/env.list` names in the
  developer's keyring (see `secrets.md`).
- `opsctl/` is in the checkout and the Go toolchain can cross-compile it for
  `linux/amd64`.
- The developer's ssh configuration can reach a new instance as `ec2-user`
  with the account's `ikigenba` key pair.

Postconditions:

- The role `ikigenba-space-<domain>` exists with the account's permissions
  boundary attached and one inline policy named `space`, the template with
  `<domain>`, `<zone_id>`, `<account_id>`, and `<bucket>` substituted. The
  instance profile of the same name holds the role.
- One instance is running, launched from the account's launch template with
  that profile, tagged `Project=ikigenba` and `Space=<domain>` on the instance
  and its volume. It has passed its status checks and `cloud-init status
  --wait` has returned.
- The space's records, the Route 53 `A` records `<domain>` and `*.<domain>`
  with TTL 60 in the account's hosted zone whose name is the longest suffix
  of `<domain>`, point at the instance's public address and the change is
  `INSYNC`.
- Every app's secrets object is at `/ikigenba/<domain>/<app>` (see
  `secrets.md`).
- `opsctl` is installed on the host at `/usr/local/bin/opsctl`, its config
  store holds `host.name=<domain>`, `dns.provider=route53`,
  `dns.zones=<zone name>:<zone id>`, `backup.full_seconds`,
  `backup.incremental_seconds`, and `backup.wal_seconds` set to the account's
  three periods, and `sudo opsctl init` has exited 0 on the host.
- No apps are deployed; that is `deploy`.

## A developer creates a space with a fixed address

`--elastic-ip` allocates an Elastic IP tagged `Project=ikigenba` and
`Space=<domain>`, associates it with the instance, and writes the records to
it, so the address survives stop and start. There is no account default.

Command:

```
$ devctl --account 295229566359 space create staging.ikigenba.dev --elastic-ip
```

Output:

```
account: ok (ikigenba.dev, us-east-2)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
secrets: ok (3 apps)
role: ok (ikigenba-space-staging.ikigenba.dev)
instance: ok (i-0a1b2c3d4e5f60718 running, 3.15.44.201)
address: ok (elastic ip 18.220.10.5 associated)
records: ok (staging.ikigenba.dev, *.staging.ikigenba.dev -> 18.220.10.5, INSYNC)
host: ok (status checks passed, cloud-init done)
opsctl: ok (v0.1.0 installed, 6 keys set)
init: ok
staging.ikigenba.dev 18.220.10.5
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties, the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_full_seconds`,
  `backup_incremental_seconds`, and `backup_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` ends in the account's `domain` property, `ikigenba.dev`.
- No instance is tagged `Space=<domain>` in the account.
- `infra/templates/space-role-policy.json` is in the checkout.
- Every app in the checkout has the values its `etc/env.list` names in the
  developer's keyring (see `secrets.md`).
- `opsctl/` is in the checkout and the Go toolchain can cross-compile it for
  `linux/amd64`.
- The developer's ssh configuration can reach a new instance as `ec2-user`
  with the account's `ikigenba` key pair.

Postconditions:

- An Elastic IP tagged `Project=ikigenba` and `Space=<domain>` is allocated
  and associated with the instance.
- The role `ikigenba-space-<domain>` exists with the account's permissions
  boundary attached and one inline policy named `space`, the template with
  `<domain>`, `<zone_id>`, `<account_id>`, and `<bucket>` substituted. The
  instance profile of the same name holds the role.
- One instance is running, launched from the account's launch template with
  that profile, tagged `Project=ikigenba` and `Space=<domain>` on the instance
  and its volume. It has passed its status checks and `cloud-init status
  --wait` has returned.
- The space's records, the Route 53 `A` records `<domain>` and `*.<domain>`
  with TTL 60 in the account's hosted zone whose name is the longest suffix
  of `<domain>`, point at the Elastic IP and the change is `INSYNC`.
- Every app's secrets object is at `/ikigenba/<domain>/<app>` (see
  `secrets.md`).
- `opsctl` is installed on the host at `/usr/local/bin/opsctl`, its config
  store holds `host.name=<domain>`, `dns.provider=route53`,
  `dns.zones=<zone name>:<zone id>`, `backup.full_seconds`,
  `backup.incremental_seconds`, and `backup.wal_seconds` set to the account's
  three periods, and `sudo opsctl init` has exited 0 on the host.
- No apps are deployed; that is `deploy`.

## A developer creates the apex space

`<domain>` may equal the account's `domain` property.

Command:

```
$ devctl --account 295229566359 space create ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
secrets: ok (3 apps)
role: ok (ikigenba-space-ikigenba.dev)
instance: ok (i-0f1e2d3c4b5a69788 running, 3.18.9.77)
records: ok (ikigenba.dev, *.ikigenba.dev -> 3.18.9.77, INSYNC)
host: ok (status checks passed, cloud-init done)
opsctl: ok (v0.1.0 installed, 6 keys set)
init: ok
ikigenba.dev 3.18.9.77
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties, the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_full_seconds`,
  `backup_incremental_seconds`, and `backup_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` equals the account's `domain` property, `ikigenba.dev`.
- No instance is tagged `Space=<domain>` in the account.
- `infra/templates/space-role-policy.json` is in the checkout.
- Every app in the checkout has the values its `etc/env.list` names in the
  developer's keyring (see `secrets.md`).
- `opsctl/` is in the checkout and the Go toolchain can cross-compile it for
  `linux/amd64`.
- The developer's ssh configuration can reach a new instance as `ec2-user`
  with the account's `ikigenba` key pair.

Postconditions:

- The role `ikigenba-space-<domain>` exists with the account's permissions
  boundary attached and one inline policy named `space`, the template with
  `<domain>`, `<zone_id>`, `<account_id>`, and `<bucket>` substituted. The
  instance profile of the same name holds the role.
- One instance is running, launched from the account's launch template with
  that profile, tagged `Project=ikigenba` and `Space=<domain>` on the instance
  and its volume. It has passed its status checks and `cloud-init status
  --wait` has returned.
- The space's records, the Route 53 `A` records `<domain>` and `*.<domain>`
  with TTL 60 in the account's hosted zone whose name is the longest suffix
  of `<domain>`, point at the instance's public address and the change is
  `INSYNC`.
- Every app's secrets object is at `/ikigenba/<domain>/<app>` (see
  `secrets.md`).
- `opsctl` is installed on the host at `/usr/local/bin/opsctl`, its config
  store holds `host.name=<domain>`, `dns.provider=route53`,
  `dns.zones=<zone name>:<zone id>`, `backup.full_seconds`,
  `backup.incremental_seconds`, and `backup.wal_seconds` set to the account's
  three periods, and `sudo opsctl init` has exited 0 on the host.
- No apps are deployed; that is `deploy`.

## A developer creates a space outside the account's domain

Command:

```
$ devctl --account 602773793009 space create foo.example.com
```

Output:

```
devctl: 'foo.example.com' does not end in the account domain 'sbx.ikigenba.dev'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account's `domain` property is `sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer creates a space under a subdomain delegated to another account

Command:

```
$ devctl --account 295229566359 space create foo.sbx.ikigenba.dev
```

Output:

```
devctl: 'foo.sbx.ikigenba.dev' is delegated away from this account's zone 'ikigenba.dev'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The zone `ikigenba.dev` holds an `NS` record for `sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer creates a space at a delegated subdomain itself

Command:

```
$ devctl --account 295229566359 space create sbx.ikigenba.dev
```

Output:

```
devctl: 'sbx.ikigenba.dev' is delegated away from this account's zone 'ikigenba.dev'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The zone `ikigenba.dev` holds an `NS` record for `sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer creates a space where an existing space's app answers

Command:

```
$ devctl --account 602773793009 space create crm.foo.sbx.ikigenba.dev
```

Output:

```
devctl: 'crm.foo.sbx.ikigenba.dev' is where app 'crm' of space 'foo.sbx.ikigenba.dev' answers
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- An instance tagged `Space=foo.sbx.ikigenba.dev` exists in the account.
- `crm/etc/env.list` is in the checkout.

Postconditions:

- Nothing has changed.

## A developer creates a space that already exists

Command:

```
$ devctl --account 602773793009 space create foo.sbx.ikigenba.dev
```

Output:

```
devctl: a space at 'foo.sbx.ikigenba.dev' already exists (i-0c9e94542d98846a8)
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- An instance tagged `Space=foo.sbx.ikigenba.dev` exists in the account and
  is not terminated.

Postconditions:

- Nothing has changed.

## A developer creates a space with a secret missing from the keyring

Command:

```
$ devctl --account 602773793009 space create new.sbx.ikigenba.dev
```

Output:

```
devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- `crm/etc/env.list` names `CRM_API_KEY` and neither the keyring nor the
  environment has it.

Postconditions:

- Nothing has changed. No secrets object was written for any app.

## A developer runs `space create` without a domain

Command:

```
$ devctl --account 602773793009 space create
```

Output:

```
devctl: space create needs <domain>

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer's create fails part-way

Command:

```
$ devctl --account 602773793009 space create foo.sbx.ikigenba.dev
```

Output:

```
account: ok (sbx.ikigenba.dev, us-east-2)
domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)
secrets: ok (3 apps)
role: ok (ikigenba-space-foo.sbx.ikigenba.dev)
devctl: ec2 RunInstances: InsufficientInstanceCapacity
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties, the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_full_seconds`,
  `backup_incremental_seconds`, and `backup_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` ends in the account's `domain` property.
- No instance is tagged `Space=<domain>` in the account.
- `infra/templates/space-role-policy.json` is in the checkout.
- Every app in the checkout has the values its `etc/env.list` names in the
  developer's keyring (see `secrets.md`).
- `opsctl/` is in the checkout and the Go toolchain can cross-compile it for
  `linux/amd64`.
- The developer's ssh configuration can reach a new instance as `ec2-user`
  with the account's `ikigenba` key pair.
- EC2 has no capacity for the launch template's instance type.

Postconditions:

- The steps that printed `ok` hold: the secrets objects and the role exist.
  No instance was launched.
- `space destroy foo.sbx.ikigenba.dev` removes what exists. `space create
  foo.sbx.ikigenba.dev` again is refused because the role exists.

## A developer destroys a space

A developer wants everything a space owned gone, so nothing lingers and
nothing costs money. Each line of output is one step.

Command:

```
$ devctl --account 602773793009 space destroy foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (i-0c9e94542d98846a8 terminated)
address: ok (no elastic ip)
records: ok (2 deleted)
secrets: ok (3 parameters deleted)
backups: ok (0 objects deleted)
role: ok (ikigenba-space-foo.sbx.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties at `/ikigenba/account`, with
  `delete_secrets_on_destroy` and `delete_backups_on_destroy` both true.
- The space exists without an Elastic IP.

Postconditions:

- The space's instance is terminated.
- The `A` records `<domain>` and `*.<domain>` are gone from the zone. The
  wildcard is read back first,
  because Route 53 returns it as `\052.<domain>` and the delete must match
  exactly.
- Every parameter under `/ikigenba/<domain>/` is deleted.
- Every object under `<domain>/` in the account's backup bucket is deleted.
- The inline policy, the instance profile, and the role are gone.

## A developer destroys a space whose account keeps secrets and backups

Command:

```
$ devctl --account 295229566359 space destroy staging.ikigenba.dev
```

Output:

```
instance: ok (i-0a1b2c3d4e5f60718 terminated)
address: ok (elastic ip 18.220.10.5 released)
records: ok (2 deleted)
secrets: ok (kept, delete_secrets_on_destroy=false)
backups: ok (kept, delete_backups_on_destroy=false)
role: ok (ikigenba-space-staging.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account's `delete_secrets_on_destroy` and `delete_backups_on_destroy`
  are both false.
- The space exists with an Elastic IP tagged `Space=staging.ikigenba.dev`.

Postconditions:

- The instance is terminated; the Elastic IP is disassociated and released;
  the `A` records `<domain>` and `*.<domain>`, and the role, profile, and
  policy are gone.
- Every parameter under `/ikigenba/staging.ikigenba.dev/` and every object
  under `staging.ikigenba.dev/` in the backup bucket are untouched.

## A developer destroys a space that is already gone

Command:

```
$ devctl --account 602773793009 space destroy foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (already gone)
address: ok (already gone)
records: ok (already gone)
secrets: ok (already gone)
backups: ok (already gone)
role: ok (already gone)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- Nothing tagged or named for `foo.sbx.ikigenba.dev` exists in the account.

Postconditions:

- Nothing has changed.

## A developer runs `space destroy` without a domain

Command:

```
$ devctl --account 602773793009 space destroy
```

Output:

```
devctl: space destroy needs <domain>

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer's destroy fails part-way

Command:

```
$ devctl --account 602773793009 space destroy foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (i-0c9e94542d98846a8 terminated)
address: ok (no elastic ip)
devctl: route53 ChangeResourceRecordSets: Throttling
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists; Route 53 throttles the change.

Postconditions:

- The steps that printed `ok` hold; the remaining steps did not run.
- Running destroy again resumes from wherever things stand.

## A developer stops a space that has a fixed address

A developer leaves a space for a while but wants its disk and its state back
later. With an Elastic IP the records stay, because the address does.

Command:

```
$ devctl --account 295229566359 space stop staging.ikigenba.dev
```

Output:

```
instance: ok (i-0a1b2c3d4e5f60718 stopped)
records: ok (kept, elastic ip)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`, with an Elastic IP.

Postconditions:

- The instance is `stopped`. Its volume, its tags, its role, its secrets, its
  backups, and its `A` records `<domain>` and `*.<domain>` are untouched.

## A developer stops a space that has no fixed address

Without an Elastic IP the `A` records `<domain>` and `*.<domain>` are deleted, because the address is
released with the stop and the name must not point at a stranger.

Command:

```
$ devctl --account 602773793009 space stop foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (i-0c9e94542d98846a8 stopped)
records: ok (2 deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`, without an Elastic IP.

Postconditions:

- The instance is `stopped`. Its volume, its tags, its role, its secrets, and
  its backups are untouched.
- The `A` records `<domain>` and `*.<domain>` are gone from the zone.

## A developer stops a space that is already stopped

Command:

```
$ devctl --account 602773793009 space stop foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (already stopped)
records: ok (already gone)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance is `stopped`, without an Elastic IP, and its records
  are gone.

Postconditions:

- Nothing has changed.

## A developer starts a stopped space that has no fixed address

The instance comes up on a new address, the records are written to it, and a
renewal check runs on the host so a certificate that expired while the space
was stopped is renewed. It is `sudo certbot renew`, never forced; certbot
decides. The last line is the domain and the address.

Command:

```
$ devctl --account 602773793009 space start foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (i-0c9e94542d98846a8 running, 3.145.72.19)
records: ok (foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.145.72.19, INSYNC)
host: ok (status checks passed)
certificate: ok (certbot renew: not yet due)
foo.sbx.ikigenba.dev 3.145.72.19
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `stopped`, without an Elastic IP.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- The instance is `running` and has passed its status checks.
- The `A` records `<domain>` and `*.<domain>` point at the instance's new
  public address and the change
  is `INSYNC`.
- `sudo certbot renew` has been run on the host over ssh and exited 0.

## A developer starts a stopped space that has a fixed address

Command:

```
$ devctl --account 295229566359 space start staging.ikigenba.dev
```

Output:

```
instance: ok (i-0a1b2c3d4e5f60718 running, 18.220.10.5)
records: ok (unchanged, 18.220.10.5)
host: ok (status checks passed)
certificate: ok (certbot renew: renewed)
staging.ikigenba.dev 18.220.10.5
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `stopped`, with an Elastic IP.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- The instance is `running` and has passed its status checks.
- The `A` records `<domain>` and `*.<domain>` still point at the Elastic IP.
- `sudo certbot renew` has been run on the host over ssh and exited 0.

## A developer asks what a space is running

The answer comes from the host, never from a record kept elsewhere: over ssh,
`opsctl` lists the installed apps, asks each app's binary its version, and
reads each app's systemd unit state. One line per app, in name order: the
app, the version, the unit state.

Command:

```
$ devctl --account 602773793009 space status foo.sbx.ikigenba.dev
```

Output:

```
crm v0.1.0 active
dashboard 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a active
gmail v0.1.0 failed
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- Three apps are installed on the host.

Postconditions:

- Nothing has changed.

## A developer asks what a space with no apps is running

Command:

```
$ devctl --account 602773793009 space status new.sbx.ikigenba.dev
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`; `opsctl` is installed on it
  and no app has been deployed.

Postconditions:

- Nothing has changed.

## A developer asks what a stopped space is running

Command:

```
$ devctl --account 602773793009 space status bar.sbx.ikigenba.dev
```

Output:

```
devctl: 'bar.sbx.ikigenba.dev' is stopped
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `stopped`.

Postconditions:

- Nothing has changed.

## A developer stops, starts, or asks about a space that does not exist

Command:

```
$ devctl --account 602773793009 space stop gone.sbx.ikigenba.dev
```

```
$ devctl --account 602773793009 space start gone.sbx.ikigenba.dev
```

```
$ devctl --account 602773793009 space status gone.sbx.ikigenba.dev
```

Output:

```
devctl: no space at 'gone.sbx.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- No instance in the account is tagged `Space=gone.sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer's start cannot reach the host

Command:

```
$ devctl --account 602773793009 space start foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (i-0c9e94542d98846a8 running, 3.145.72.19)
records: ok (foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.145.72.19, INSYNC)
host: ok (status checks passed)
devctl: ssh ec2-user@3.145.72.19: connection timed out
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `stopped`.
- The developer's machine cannot open an ssh connection to the instance.

Postconditions:

- The instance is `running` and the `A` records `<domain>` and `*.<domain>`
  point at it.
- No renewal check was run. Running start again on the running instance
  re-points the records and runs the check.
