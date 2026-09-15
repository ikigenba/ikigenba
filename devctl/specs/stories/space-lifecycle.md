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
  list                       one line per space in the account
  create <domain> [options]  create the space at <domain>
  destroy <domain>           remove the space and everything it owned
  stop <domain>              stop the instance; state is kept
  start <domain>             start the instance; its address is unchanged
  status <domain>            one line per app: version, service state, database journal mode

Options (create):
  --acme-email <address>  where the CA sends the space's expiry warnings; required

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
foo.sbx.ikigenba.dev running 18.118.7.42
new.sbx.ikigenba.dev running 18.220.10.5
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties: the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_host_files_seconds`,
  `backup_service_files_seconds`, `backup_service_db_seconds`, and
  `backup_service_wal_seconds`.
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
Every space is given an Elastic IP, so its address is fixed for the whole of
its life: the records are written once, here, and every later stop and start
leaves them alone. Each line of output is one step; the last line is the
domain and the address.

The host needs ten configuration keys before `opsctl init` will run, and
`create` is what sets all ten. Seven it already knows: the domain is
`host.name`, the zone it found is `dns.zones`, the provider is `route53`, and
the account's four backup periods are the four period keys. Two more it
reads from the account's properties: `region` becomes `aws.region`, and
`backup_bucket` with the domain becomes `backup.s3_uri`. The tenth, the address
the CA sends expiry warnings to, is in neither place, so the developer supplies
it with `--acme-email`. It is required rather than defaulted: a wrong address
is only discovered when a certificate quietly expires.

The opsctl it installs is the newest release opsctl has published. The host
then stays on that version until someone explicitly updates it: `create` is
the only thing that chooses a version, and no later devctl command changes it.
The host holds its own copy of the installer, which is what an update runs.

Command:

```
$ devctl --account 602773793009 space create foo.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
```

Output:

```
account: ok (sbx.ikigenba.dev, us-east-2)
domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)
secrets: ok (3 apps)
role: ok (ikigenba-space-foo.sbx.ikigenba.dev)
instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)
address: ok (elastic ip 18.118.7.42 associated)
records: ok (created foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 18.118.7.42, INSYNC)
host: ok (status checks passed, cloud-init done)
opsctl: ok (v0.1.0 installed, 10 keys set)
init: ok
foo.sbx.ikigenba.dev 18.118.7.42
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties, the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_host_files_seconds`,
  `backup_service_files_seconds`, `backup_service_db_seconds`, and
  `backup_service_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` ends in the account's `domain` property.
- No instance is tagged `Space=<domain>` in the account.
- Every app in the checkout has the values its manifest's `secrets` array
  names in the developer's keyring (see `secrets.md`).
- opsctl has a published release, and the host can reach it over the network.
- `--acme-email` names an address the CA will accept.
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
- An Elastic IP tagged `Project=ikigenba` and `Space=<domain>` is allocated
  and associated with the instance.
- The space's records, the Route 53 `A` records `<domain>` and `*.<domain>`
  with TTL 60 in the account's hosted zone whose name is the longest suffix
  of `<domain>`, point at the Elastic IP and the change is `INSYNC`.
- Every app's secrets object is at `/ikigenba/<domain>/<app>` (see
  `secrets.md`).
- `opsctl` is installed on the host and on root's PATH, and its configuration
  store holds exactly the ten keys opsctl declares: `host.name=<domain>`,
  `dns.provider=route53`, `dns.zones=<zone name>:<zone id>`, `aws.region` and
  `backup.s3_uri` from the account's `region` and `backup_bucket` properties,
  `acme.email` from `--acme-email`, and `backup.host_files_seconds`,
  `backup.service_files_seconds`, `backup.service_db_seconds`, and
  `backup.service_wal_seconds` set to the account's four periods.
- `sudo opsctl init` has exited 0 on the host, so the host holds its
  certificate, its generated nginx configuration, its litestream configuration
  and unit, and its two backup timers, each enabled whose period is non-zero.
- No apps are deployed; that is `deploy`.

## A developer creates the apex space

`<domain>` may equal the account's `domain` property.

Command:

```
$ devctl --account 295229566359 space create ikigenba.dev --acme-email ops@ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
secrets: ok (3 apps)
role: ok (ikigenba-space-ikigenba.dev)
instance: ok (i-0f1e2d3c4b5a69788 running, 3.18.9.77)
address: ok (elastic ip 18.117.42.9 associated)
records: ok (created ikigenba.dev, *.ikigenba.dev -> 18.117.42.9, INSYNC)
host: ok (status checks passed, cloud-init done)
opsctl: ok (v0.1.0 installed, 10 keys set)
init: ok
ikigenba.dev 18.117.42.9
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties, the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `deploy_from_main_only`, `delete_secrets_on_destroy`,
  `delete_backups_on_destroy`, `backup_host_files_seconds`,
  `backup_service_files_seconds`, `backup_service_db_seconds`, and
  `backup_service_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` equals the account's `domain` property, `ikigenba.dev`.
- No instance is tagged `Space=<domain>` in the account.
- Every app in the checkout has the values its manifest's `secrets` array
  names in the developer's keyring (see `secrets.md`).
- opsctl has a published release, and the host can reach it over the network.
- `--acme-email` names an address the CA will accept.
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
- An Elastic IP tagged `Project=ikigenba` and `Space=<domain>` is allocated
  and associated with the instance.
- The space's records, the Route 53 `A` records `<domain>` and `*.<domain>`
  with TTL 60 in the account's hosted zone whose name is the longest suffix
  of `<domain>`, point at the Elastic IP and the change is `INSYNC`.
- Every app's secrets object is at `/ikigenba/<domain>/<app>` (see
  `secrets.md`).
- `opsctl` is installed on the host and on root's PATH, and its configuration
  store holds exactly the ten keys opsctl declares: `host.name=<domain>`,
  `dns.provider=route53`, `dns.zones=<zone name>:<zone id>`, `aws.region` and
  `backup.s3_uri` from the account's `region` and `backup_bucket` properties,
  `acme.email` from `--acme-email`, and `backup.host_files_seconds`,
  `backup.service_files_seconds`, `backup.service_db_seconds`, and
  `backup.service_wal_seconds` set to the account's four periods.
- `sudo opsctl init` has exited 0 on the host, so the host holds its
  certificate, its generated nginx configuration, its litestream configuration
  and unit, and its two backup timers, each enabled whose period is non-zero.
- No apps are deployed; that is `deploy`.

## A developer creates a space outside the account's domain

Command:

```
$ devctl --account 602773793009 space create foo.example.com --acme-email ops@ikigenba.dev
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
$ devctl --account 295229566359 space create foo.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
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
$ devctl --account 295229566359 space create sbx.ikigenba.dev --acme-email ops@ikigenba.dev
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
$ devctl --account 602773793009 space create crm.foo.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
```

Output:

```
devctl: 'crm.foo.sbx.ikigenba.dev' is where app 'crm' of space 'foo.sbx.ikigenba.dev' answers
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- An instance tagged `Space=foo.sbx.ikigenba.dev` exists in the account.
- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.

Postconditions:

- Nothing has changed.

## A developer creates a space that already exists

Command:

```
$ devctl --account 602773793009 space create foo.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
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
$ devctl --account 602773793009 space create new.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
```

Output:

```
devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- `crm/etc/manifest.toml` lists `CRM_API_KEY` in `secrets` and neither the
  keyring nor the environment has it.

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

## A developer runs `space create` without an address for the CA

There is no default and nothing to fall back on: the address is the developer's
to choose, and a space created without one would only say so months later, when
a certificate it could not warn anyone about expired. The refusal comes with the
arguments, before the account is read.

Command:

```
$ devctl --account 602773793009 space create foo.sbx.ikigenba.dev
```

Output:

```
devctl: space create needs --acme-email <address>

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. A `--acme-email` with no
value gives `devctl: option '--acme-email' requires a value`, also exit 2.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made, and nothing was checked about
  `<domain>`: a missing operand and a missing required option are both
  answered before the account is read.

## A developer's create fails part-way

Command:

```
$ devctl --account 602773793009 space create foo.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
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
  `delete_backups_on_destroy`, `backup_host_files_seconds`,
  `backup_service_files_seconds`, `backup_service_db_seconds`, and
  `backup_service_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` ends in the account's `domain` property.
- No instance is tagged `Space=<domain>` in the account.
- Every app in the checkout has the values its manifest's `secrets` array
  names in the developer's keyring (see `secrets.md`).
- opsctl has a published release, and the host can reach it over the network.
- `--acme-email` names an address the CA will accept.
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
address: ok (elastic ip 18.118.7.42 released)
records: ok (deleted foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev)
secrets: ok (3 parameters deleted)
backups: ok (0 objects deleted)
role: ok (ikigenba-space-foo.sbx.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties at `/ikigenba/account`, with
  `delete_secrets_on_destroy` and `delete_backups_on_destroy` both true.
- The space exists.

Postconditions:

- The space's instance is terminated and its Elastic IP is disassociated and
  released.
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
records: ok (deleted staging.ikigenba.dev, *.staging.ikigenba.dev)
secrets: ok (kept, delete_secrets_on_destroy=false)
backups: ok (kept, delete_backups_on_destroy=false)
role: ok (ikigenba-space-staging.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account's `delete_secrets_on_destroy` and `delete_backups_on_destroy`
  are both false.
- The space exists.

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
address: ok (elastic ip 18.118.7.42 released)
devctl: route53 ChangeResourceRecordSets: Throttling
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists; Route 53 throttles the change.

Postconditions:

- The steps that printed `ok` hold; the remaining steps did not run.
- Running destroy again resumes from wherever things stand.

## A developer stops a space

A developer leaves a space for a while but wants its disk and its state back
later. The records stay, because the address does: a stop releases nothing.

Command:

```
$ devctl --account 295229566359 space stop staging.ikigenba.dev
```

Output:

```
instance: ok (i-0a1b2c3d4e5f60718 stopped)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`.

Postconditions:

- The instance is `stopped`. Its volume, its tags, its role, its secrets, its
  backups, and its `A` records `<domain>` and `*.<domain>` are untouched.

## A developer stops a space that is already stopped

Command:

```
$ devctl --account 602773793009 space stop foo.sbx.ikigenba.dev
```

Output:

```
instance: ok (already stopped)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance is `stopped`.

Postconditions:

- Nothing has changed.

## A developer starts a stopped space

The instance comes back at the address it had, so no record changes, and a
renewal check runs on the host so a certificate that expired while the space
was stopped is renewed. It is `sudo certbot renew`, never forced; certbot
decides. The last line is the domain and the address.

Command:

```
$ devctl --account 295229566359 space start staging.ikigenba.dev
```

Output:

```
instance: ok (i-0a1b2c3d4e5f60718 running, 18.220.10.5)
host: ok (status checks passed)
certificate: ok (certbot renew: renewed)
staging.ikigenba.dev 18.220.10.5
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `stopped`.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- The instance is `running` and has passed its status checks.
- The `A` records `<domain>` and `*.<domain>` still point at the Elastic IP.
- `sudo certbot renew` has been run on the host over ssh and exited 0.

## A developer asks what a space is running

The answer comes from the host, never from a record kept elsewhere: over ssh,
`opsctl` lists the installed apps, asks each app's binary its version, reads
each app's systemd unit state, and reads the journal mode of the database each
app declares. `space status` copies that output byte for byte. One line per app,
in name order: the app, the version, the unit state, and the database journal
mode (`-` for an app that declares no database). A mode other than `wal` means
that database is no longer reaching S3.

Command:

```
$ devctl --account 602773793009 space status foo.sbx.ikigenba.dev
```

Output:

```
crm v0.1.0 active wal
dashboard v0.0.9 active -
gmail v0.1.0 failed -
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
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
host: ok (status checks passed)
devctl: ssh ec2-user@18.118.7.42: connection timed out
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `stopped`.
- The developer's machine cannot open an ssh connection to the instance.

Postconditions:

- The instance is `running` and the `A` records `<domain>` and `*.<domain>`
  still point at its Elastic IP.
- No renewal check was run. Running start again on the running instance runs
  the check.
