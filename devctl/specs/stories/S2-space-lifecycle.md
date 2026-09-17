# Stories — space lifecycle

A space is one running copy of the platform: one EC2 instance in one account,
named by its full domain, found by its tags. `space` is the command that lists,
creates, destroys, stops, and starts them, sets one's host up again, asks one
what it is running, restarts one of its apps, and reads that app's journal.
Nothing is kept on the developer's machine; the cloud is the registry.

## A developer asks what `space` can do

The top-level usage gains the line `  space     list, create, destroy, stop,
start, initialise, and inspect spaces` under `Commands:`.

Command:

```
$ devctl space --help
```

Output:

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
  `delete_secrets_on_destroy`, `delete_backups_on_destroy`,
  `backup_host_files_seconds`,
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
leaves them alone. Elastic IPs are an account quota, five per region unless
the account has asked for more, and each space holds one alongside whatever
else the account holds; when the quota is exhausted, create fails at its
address step and relays the AWS error. Each line of output is one step; the
last line is the domain and the address.

The host needs ten configuration keys before `opsctl init` will run, and
`create` is what sets all ten. Seven it already knows: the domain is
`host.name`, the zone it found is `dns.zones`, the provider is `route53`, and
the account's four backup periods are the four period keys. Two more it
reads from the account's properties: `region` becomes `aws.region`, and
`backup_bucket` with the domain becomes `backup.s3_uri`. The tenth, the address
the CA sends expiry warnings to, is in neither place, so the developer supplies
it with `--acme-email`. It is required rather than defaulted: a wrong address
is only discovered when a certificate quietly expires.

The `secrets` step writes the same objects `secrets push` writes, one per app
in the checkout, and it is the one writer that does not require the space's
instance to exist, because create is what is about to launch it. The step
comes before the role and the instance so that a create refused for a
missing keyring value leaves nothing in the account.

The opsctl it installs is the newest release opsctl has published. The host
then stays on that version until someone explicitly moves it: `create` chooses
the first version, and `space init --opsctl` is the only later devctl command
that changes it. The host holds its own copy of the installer, which is what
`space init` runs.

In an account that keeps backups there is one more step, `restore`, between
`opsctl` and `init`: the space may have lived before, and its certificate
and store are then in the bucket. This account deletes backups on destroy,
so the step is absent here and every create issues a new certificate; the
CA allows five for the same names in any seven days, which only a space
created and destroyed that often would reach. The apex create story shows
the other case.

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
  `delete_secrets_on_destroy`, `delete_backups_on_destroy`,
  `backup_host_files_seconds`,
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
  and unit, its two backup timers, each enabled whose period is non-zero, and
  its certificate renewal timer, enabled.
- No apps are deployed; that is `deploy`.

## A developer creates the apex space

`<domain>` may equal the account's `domain` property. This account keeps
backups, so create looks for a host backup under the space's own prefix
before `init` runs: a space that lived before has its certificate there,
and the CA rate-limits how often it will issue the same names. This is the
first time, so there is nothing to bring back and the line says so; the
rebuild story below shows the other case.

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
restore: ok (no host backup)
init: ok
ikigenba.dev 18.117.42.9
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties, the JSON object at Parameter Store `/ikigenba/account`,
  written by Terraform, with the keys `domain`, `backup_bucket`,
  `launch_template_id`, `permissions_boundary_arn`, `region`,
  `delete_secrets_on_destroy`, `delete_backups_on_destroy`,
  `backup_host_files_seconds`,
  `backup_service_files_seconds`, `backup_service_db_seconds`, and
  `backup_service_wal_seconds`.
- The account has a hosted zone whose name is a suffix of `<domain>`, and the
  launch template, the permissions boundary, and the backup bucket the
  properties name.
- `<domain>` equals the account's `domain` property, `ikigenba.dev`.
- No instance is tagged `Space=<domain>` in the account.
- The account's `delete_backups_on_destroy` is false, and nothing is under
  `ikigenba.dev/host/` in the backup bucket.
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
- `ikigenba.dev/host/` was listed and found empty, so `opsctl host restore`
  was not run and the ten keys were set once.
- `sudo opsctl init` has exited 0 on the host, so the host holds its
  certificate, its generated nginx configuration, its litestream configuration
  and unit, its two backup timers, each enabled whose period is non-zero, and
  its certificate renewal timer, enabled.
- No apps are deployed; that is `deploy`.

## A developer rebuilds a durable space

A durable space is one whose account keeps its backups, and a rebuild is
how those backups earn their keep: the host is gone, by choice or by
accident, and a new one is to hold everything the old one held. Every step
is a command that already exists; what this story adds is the order, and
why it is that order.

1. `space destroy`, so the old host takes its final backup with `retire`
   before it goes. When the host is already lost the backup cannot be taken,
   and `space destroy --no-backup` clears what remains: the records, the
   address, and the role. What the timers had not copied is lost with it.
2. `space create`. Its `restore` step finds the host backup and runs
   `sudo opsctl host restore`, which puts `/etc/letsencrypt/` and
   `/etc/ikigenba/` back; create then sets its ten keys again, so what it
   was told wins over the backup's copy of the store and any key an operator
   set by hand survives. `init` finds the certificate current and asks the
   CA for nothing.
3. `restore <domain> <app>` for each app. The host has never run the app,
   so the restore lands its `etc/` and `state/` and its database with no
   binary and no unit, and litestream replicates the database from that
   moment.
4. `deploy <domain> <file>` for each app, over the restored data. install
   replaces `bin/`, `etc/`, and `share/` and leaves `state/` alone, so the
   app starts over its own data.

Restore comes before deploy, not after. A deploy onto empty state starts the
app, and an app that finds no database makes one, migrates it, and seeds it
(see opsctl's `apps.md`); a restore after that has to stop the app and
replace what it made. A restore first has nothing to stop, and the deploy
that follows is an ordinary deploy: the app finds its database, migrates it
forward, and seeds nothing.

Command:

```
$ devctl --account 295229566359 space destroy ikigenba.dev
$ devctl --account 295229566359 space create ikigenba.dev --acme-email ops@ikigenba.dev
$ devctl --account 295229566359 restore ikigenba.dev crm
$ devctl --account 295229566359 restore ikigenba.dev dashboard
$ devctl --account 295229566359 deploy ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
$ devctl --account 295229566359 deploy ikigenba.dev dashboard/dist/dashboard-v0.0.9.tar.xz
$ devctl --account 295229566359 space status ikigenba.dev
```

Output, with the create's lines before `opsctl` and the deploys' lines
before `install` as in their own stories:

```
retire: ok (opsctl backed up crm, dashboard, host)
instance: ok (i-0f1e2d3c4b5a69788 terminated)
address: ok (elastic ip 18.117.42.9 released)
records: ok (deleted ikigenba.dev, *.ikigenba.dev)
secrets: ok (kept, delete_secrets_on_destroy=false)
backups: ok (kept, delete_backups_on_destroy=false)
role: ok (ikigenba-space-ikigenba.dev deleted)
...
opsctl: ok (v0.1.0 installed, 10 keys set)
restore: ok (host/2026-09-12T14:22:51Z.tar.zst, 10 keys set again)
init: ok
ikigenba.dev 18.117.42.9
restore: ok (opsctl restore crm)
restore: ok (opsctl restore dashboard)
...
install: ok (opsctl installed crm)
...
install: ok (opsctl installed dashboard)
crm v0.1.0 active wal
dashboard v0.0.9 active -
```

Each command exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account's `delete_secrets_on_destroy` and `delete_backups_on_destroy`
  are both false.
- The apex space exists, its instance is `running`, and `crm` and
  `dashboard` are deployed on it; `crm` declares a database.
- The developer's ssh configuration can reach the old instance and the new
  one as `ec2-user`.
- `crm/dist/crm-v0.1.0.tar.xz` and `dashboard/dist/dashboard-v0.0.9.tar.xz`
  exist, written by `build`.

Postconditions:

- After the destroy, `ikigenba.dev/host/`, `ikigenba.dev/crm/`, and
  `ikigenba.dev/dashboard/` each hold an object from the retire, and the
  secrets under `/ikigenba/ikigenba.dev/` are untouched.
- After the create, the new host holds the old certificate: `opsctl host
  restore` was run over ssh and exited 0 before the ten keys were set
  again, and `init`'s certificate step found it current and asked the CA
  for nothing. The store holds the ten keys as create derived them, plus
  any other key the backup carried.
- After the restores, `/opt/crm/` and `/opt/dashboard/` hold the backups'
  `etc/` and `state/`, `crm`'s database is rebuilt to its last committed
  transaction and replicating, and neither has a binary or a unit.
- After the deploys, both apps answer from their binaries over the restored
  data, and `space status` shows them as above.
- The new instance has a new id and may have a new address; the records
  point at it. Nothing about the old instance remains in the account.

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

## A developer creates a space under an existing space

A space's wildcard record, wildcard certificate, and nginx catch-all answer
for every name beneath its domain: its apps today, and any app deployed to
it later. So no space may lie under another, and no list of apps is
consulted, from the checkout or from the host. The account domain is the
one exception, since every space in the account lies under it by
definition. The check reads only the account's instances, which create
already lists.

Command:

```
$ devctl --account 602773793009 space create crm.foo.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
```

Output:

```
devctl: 'crm.foo.sbx.ikigenba.dev' lies under space 'foo.sbx.ikigenba.dev'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- An instance tagged `Space=foo.sbx.ikigenba.dev` exists in the account.

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
  `delete_secrets_on_destroy`, `delete_backups_on_destroy`,
  `backup_host_files_seconds`,
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

## A developer sets a space's host up again

`create`'s `opsctl` and `init` steps, run again on a space that exists.
Everything the host generates is generated from its configuration store, and `opsctl init`
is what reads the store and makes the host match it; so when something the
store came from has changed, the way back to a host that matches is to set
the keys again and run `init` again. Terraform changed a backup period; an
operator ran `opsctl host restore` and the host now holds a store that `init`
has not acted on; a manifest was restored that `init` has not read. Nothing
here needs ssh by hand.

The nine keys `create` derives are derived the same way, from the account's
properties and the zone, and set again; a value that has not changed is
written over with itself. The tenth, `acme.email`, is derived from nothing,
so it is left as it is unless `--acme-email` says otherwise. Any key an
operator set by hand is not one of the ten and is untouched. The `opsctl`
line says which version the host is on and whether this command put it
there. Then `init` runs and its report is not relayed: it succeeded.

Command:

```
$ devctl --account 602773793009 space init foo.sbx.ikigenba.dev
```

Output:

```
account: ok (sbx.ikigenba.dev, us-east-2)
domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
opsctl: ok (v0.1.0 kept, 9 keys set)
init: ok
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account has its properties at `/ikigenba/account`, with the keys
  `create` reads.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- The host's configuration store holds the nine derived keys at the values
  the account's properties and the zone give now: `host.name`,
  `dns.provider`, `dns.zones`, `aws.region`, `backup.s3_uri`, and the four
  backup periods. `acme.email` and every other key are as they were.
- `/usr/local/bin/opsctl` is the version it was; the saved installer was not
  run.
- `sudo opsctl init` has exited 0 on the host, so the host holds its
  certificate, its generated nginx configuration, its litestream configuration
  and unit, its two backup timers, each enabled whose period is non-zero, and
  its certificate renewal timer, enabled, all regenerated from what the store
  and `/opt` hold now.
- No app was deployed, restarted, or stopped, and no record or secret was
  touched. A change to a period reaches its timer; nothing else on the space
  is different unless the store was.

## A developer moves a space to a newer opsctl

`--opsctl` names a release, and the host's own saved copy of the installer is
run with that version as its operand before the keys are set and `init` runs.
Installing the binary changes nothing on the host by itself: what a new
version changes is `init`'s to do, which is why the two are one command.

Command:

```
$ devctl --account 295229566359 space init ikigenba.dev --opsctl v0.2.0
```

Output:

```
account: ok (ikigenba.dev, us-east-2)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
instance: ok (i-0f1e2d3c4b5a69788 running, 18.117.42.9)
opsctl: ok (v0.2.0 installed, 9 keys set)
init: ok
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it with its saved installer at
  `/usr/local/share/ikigenba/opsctl-install.sh`.
- The release `opsctl/v0.2.0` exists and the host can reach it.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- `/usr/local/bin/opsctl` is `v0.2.0` and the saved installer is `v0.2.0`'s.
- Everything the plain re-initialisation's postconditions say. `init` was
  the new version's, so whatever `v0.2.0` generates differently is on the
  host.
- Naming the version that is already installed writes the same bytes and
  reports `v0.2.0 installed` all the same: the installer ran, and the line
  says what it did. A version with no release fails at the `opsctl` step,
  before any key is set, with the installer's diagnostic relayed the way a
  failed `init` is below.

## A developer changes where the CA writes

The one key `create` could not derive is the one `space init` cannot either,
so it is the one option that changes it.

Command:

```
$ devctl --account 602773793009 space init foo.sbx.ikigenba.dev --acme-email alerts@ikigenba.dev
```

Output:

```
account: ok (sbx.ikigenba.dev, us-east-2)
domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
opsctl: ok (v0.1.0 kept, 10 keys set)
init: ok
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- Everything the plain re-initialisation's postconditions say, and
  `acme.email` is `alerts@ikigenba.dev`.
- The certificate the host holds is the one it held: `init`'s certificate
  step renews when renewal is due, and a changed address is not that. The CA
  learns the new address at the next renewal.

## A developer's `space init` finds the host not ready

`opsctl init` checks everything before it runs anything, and a failed check is
on its stdout with exit 2. devctl relays the whole report after its own error
line, each line quoted with `> `, so the developer sees exactly what the host
said about itself. The keys were set before `init` ran and stay set; the
sequence did not run.

Command:

```
$ devctl --account 602773793009 space init foo.sbx.ikigenba.dev
```

Output:

```
account: ok (sbx.ikigenba.dev, us-east-2)
domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
opsctl: ok (v0.1.0 kept, 9 keys set)
devctl: init: ssh ec2-user@18.118.7.42 sudo opsctl init: exit status 2

> nginx: ok (/usr/sbin/nginx)
> certbot: failed: not found on PATH
> systemctl: ok (/usr/bin/systemctl)
> litestream: ok (/usr/bin/litestream)
> dns.provider: ok (route53)
> dns.zones: ok (sbx.ikigenba.dev)
> host.name: ok (foo.sbx.ikigenba.dev)
> zone sbx.ikigenba.dev: ok (route53 Z02587302QXWONVKW632, 4 nameservers delegated)
> host foo.sbx.ikigenba.dev: ok (zone sbx.ikigenba.dev)
> wildcard foo.sbx.ikigenba.dev: ok (18.118.7.42)
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `certbot` has been removed from the host.

Postconditions:

- The store holds the nine keys as set. Nothing `init` generates was
  written: the host's certificate, nginx configuration, litestream
  configuration, and timers are as they were.
- Running `space init` again once the host is fixed finishes the job.

## A developer initialises a stopped space, or one that does not exist

`init` needs a host to talk to. A stopped space is refused the way `status`
refuses it, and a space that does not exist the way every subcommand refuses
one.

Command:

```
$ devctl --account 602773793009 space init bar.sbx.ikigenba.dev
```

Output:

```
devctl: 'bar.sbx.ikigenba.dev' is stopped
```

Command:

```
$ devctl --account 602773793009 space init gone.sbx.ikigenba.dev
```

Output:

```
devctl: no space at 'gone.sbx.ikigenba.dev'
```

Each exits 1. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The instance tagged `Space=bar.sbx.ikigenba.dev` is `stopped`; no instance
  is tagged `Space=gone.sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer runs `space init` without a domain

Command:

```
$ devctl --account 602773793009 space init
```

Output:

```
devctl: space init needs <domain>

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An `--opsctl` or
`--acme-email` with no value gives `devctl: option '--opsctl' requires a
value` or `devctl: option '--acme-email' requires a value`, also exit 2.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer destroys a space

A developer wants everything a space owned gone, so nothing lingers and
nothing costs money. Each line of output is one step. This account deletes
its backups on destroy, so there is nothing to take a final backup for and
no `retire` step: the instance is simply terminated.

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

The backups are the point of keeping them, and the timers only copy on their
own schedule: service files daily, the host weekly, committed database
changes every fifteen minutes. So before the instance goes, devctl has the
host take its final backup with `sudo opsctl retire` over ssh, which stops
every service, lets litestream ship what it holds, and writes the service
and host backups one last time. The `retire` line summarises what opsctl
backed up; what retire does on the host is opsctl's.

Command:

```
$ devctl --account 295229566359 space destroy staging.ikigenba.dev
```

Output:

```
retire: ok (opsctl backed up crm, dashboard, host)
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
- The space exists and its instance is `running`; `opsctl` is installed on
  it, with `crm` and `dashboard` deployed.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- `sudo opsctl retire` has been run over ssh and exited 0, so
  `staging.ikigenba.dev/crm/`, `staging.ikigenba.dev/dashboard/`, and
  `staging.ikigenba.dev/host/` in the backup bucket each hold a new object
  from that run, and `crm`'s database replica is complete to its last
  committed transaction.
- The instance is terminated; the Elastic IP is disassociated and released;
  the `A` records `<domain>` and `*.<domain>`, and the role, profile, and
  policy are gone.
- Every parameter under `/ikigenba/staging.ikigenba.dev/` and every object
  under `staging.ikigenba.dev/` in the backup bucket are untouched, the new
  objects included.

## A developer destroys a space whose host cannot take a final backup

`retire` failing is a reason not to terminate: the whole point of the step
is that nothing is lost, and what opsctl reported is on the host, not gone.
opsctl's output follows the error line, each line quoted with `> `. The
instance is left as retire left it, with its services stopped, so the
developer can fix the host and run destroy again, or decide the loss is
acceptable and pass `--no-backup`.

Command:

```
$ devctl --account 295229566359 space destroy staging.ikigenba.dev
```

Output:

```
devctl: retire: ssh ec2-user@18.220.10.5 sudo opsctl retire: exit status 1

> services: ok (crm, dashboard stopped)
> litestream: ok (stopped, crm.db synced)
> crm: failed: /opt/crm/state/outbox: permission denied
> dashboard: ok (2026-09-12T14:22:51Z.tar.zst, 1.1 MiB)
> host: ok (2026-09-12T14:22:51Z.tar.zst, 48.2 KiB)
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- As for the previous story, and `opsctl retire` on the host exits non-zero.

Postconditions:

- Nothing in the account has changed: the instance is `running`, and its
  address, records, secrets, backups, and role are as they were.
- On the host, the units are whatever retire left; here, stopped.
- Running destroy again runs retire again.

## A developer destroys a stopped space whose account keeps backups

A stopped host cannot take a backup. It is refused the way `status` refuses
a stopped space, and the way out is to start it or to say the loss is
accepted.

Command:

```
$ devctl --account 295229566359 space destroy staging.ikigenba.dev
```

Output:

```
devctl: retire: instance i-0a1b2c3d4e5f60718 is stopped

run 'devctl --account 295229566359 space start staging.ikigenba.dev' first, or pass --no-backup
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The account's `delete_backups_on_destroy` is false.
- The instance tagged `Space=staging.ikigenba.dev` is `stopped`.

Postconditions:

- Nothing has changed.

## A developer destroys a space without its final backup

`--no-backup` skips `retire`. What the timers had not copied since their
last run is lost with the instance: up to a day of service files, up to a
week of the host's own configuration, and up to fifteen minutes of committed
database changes. The line says the step was skipped so the record of the
destroy says so too.

Command:

```
$ devctl --account 295229566359 space destroy staging.ikigenba.dev --no-backup
```

Output:

```
retire: skipped (--no-backup)
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
- The space exists; its instance may be `running` or `stopped`, and the host
  need not be reachable.

Postconditions:

- No ssh connection was made. Nothing new is in the backup bucket.
- Otherwise as for a destroy whose account keeps secrets and backups. In an
  account that deletes backups on destroy the option is accepted and changes
  nothing, and the `retire` line is not printed.

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

- Nothing has changed. No ssh connection was made.
- In an account that keeps backups the first line is
  `retire: ok (already gone)`: there is no host to back up.

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
decides. The host's own renewal timer is persistent and would run the same
check at boot; start runs it in the foreground so the developer sees the
answer now rather than in a journal. The last line is the domain and the
address.

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

`space restart` and `space logs` need the same host and refuse a stopped
space with the same line.

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

```
$ devctl --account 602773793009 space restart gone.sbx.ikigenba.dev crm
```

```
$ devctl --account 602773793009 space logs gone.sbx.ikigenba.dev crm
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

## A developer restarts an app on a space

Over ssh, `opsctl restart` restarts the app's unit and reports it the way
install's last line does; devctl reports opsctl's exit, the shape `restore`
uses. A restart changes nothing on the host's disk. In particular it does not
carry a pushed secret to the app: that is a deploy of the same file, and
`secrets.md` says why.

Command:

```
$ devctl --account 602773793009 space restart foo.sbx.ikigenba.dev crm
```

Output:

```
restart: ok (opsctl restarted crm)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the host. Its unit may be `active`, `inactive`, or
  `failed`.

Postconditions:

- `sudo opsctl restart crm` has been run on the host over ssh and exited 0,
  so `ikigenba-crm.service` is `active` under a new process. What restart
  does on the host is opsctl's.
- Nothing on the host's disk changed, and no other app on the space has
  changed.

## A developer's restart fails on the host

`opsctl`'s output follows the error line, each line quoted with `> `, so the
journal the host relayed is in front of the developer.

Command:

```
$ devctl --account 602773793009 space restart foo.sbx.ikigenba.dev crm
```

Output:

```
devctl: restart: ssh ec2-user@18.118.7.42 sudo opsctl restart crm: exit status 1

> opsctl: crm: service failed to start
> 
> > ikigenba-crm.service: Main process exited, code=exited, status=1/FAILURE
> > crm: open /opt/crm/state/crm.db: permission denied
```

Exits 1. The text is on stderr; stdout is empty. An app that is not on the
host is refused the same way, with `> opsctl: no service 'gmail'`, or
`> opsctl: gmail is not installed` for one the host holds data for but never
installed.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `opsctl restart crm` on the host exits non-zero.

Postconditions:

- `crm` on the host is whatever `opsctl` left; `space status` reports it,
  here `crm v0.1.0 failed wal`.
- No other app on the space has changed.

## A developer reads an app's journal

The journal is read from the host with `journalctl` and copied to the
developer's terminal byte for byte, the way `status` copies opsctl's report.
No opsctl command is involved: reading a journal changes nothing on the host,
and the unit's name, `ikigenba-<app>.service`, is what install wrote and
promised. Without options the last 100 lines are printed.

Command:

```
$ devctl --account 602773793009 space logs foo.sbx.ikigenba.dev crm
```

Output:

```
Sep 12 09:07:11 ip-10-0-1-23 systemd[1]: Started ikigenba-crm.service.
Sep 12 09:07:11 ip-10-0-1-23 crm[1842]: crm v0.1.0 listening on 127.0.0.1:3100
Sep 12 09:07:40 ip-10-0-1-23 crm[1842]: GET /contacts 200 12ms
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `ikigenba-crm.service` exists on the host and its journal holds three
  lines.

Postconditions:

- Nothing has changed. `sudo journalctl -u ikigenba-crm.service -n 100
  --no-pager` was run on the host over ssh and its stdout was copied
  unchanged; more than 100 lines would have been cut to the newest 100, the
  way journalctl cuts them.

## A developer follows an app's journal, or reads it from a moment

`--follow` keeps the connection open and prints each line as the app writes
it, until the developer interrupts the command; the interrupt is the exit.
`--since` names the moment to start from and is handed to journalctl as it
was typed, so journalctl's own syntax is what is accepted; with `--since`
every line from that moment is printed, not the newest 100. The two combine.

Command:

```
$ devctl --account 602773793009 space logs foo.sbx.ikigenba.dev crm --since -1h --follow
```

Output: every line `ikigenba-crm.service` wrote in the last hour, then each
new line as it is written.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `ikigenba-crm.service` exists on the host.

Postconditions:

- Nothing has changed. `sudo journalctl -u ikigenba-crm.service --since -1h
  -f --no-pager` was run on the host over ssh. A `--since` journalctl cannot
  parse is journalctl's to refuse: its diagnostic is relayed after
  `devctl: logs: ssh ec2-user@18.118.7.42 sudo journalctl ...: exit status
  1`, quoted with `> `, and the exit is 1.

## A developer asks for the journal of an app that is not on the space

journalctl would answer a name it has never seen with an empty journal, which
is what a typo looks like too. The host is asked whether the unit exists
first, and a name with no unit is refused.

Command:

```
$ devctl --account 602773793009 space logs foo.sbx.ikigenba.dev gmail
```

Output:

```
devctl: no app 'gmail' on 'foo.sbx.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty. An app that was removed is
refused the same way: its unit went with it.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space's instance exists and is `running`.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- No `ikigenba-gmail.service` on the host.

Postconditions:

- Nothing has changed.

## A developer runs `space restart` or `space logs` without a domain or an app

Command:

```
$ devctl --account 602773793009 space restart foo.sbx.ikigenba.dev
```

Output:

```
devctl: space restart needs <domain> and <app>

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `space logs` says `devctl:
space logs needs <domain> and <app>`, and a `--since` with no value gives
`devctl: option '--since' requires a value`, also exit 2.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made and no ssh connection was opened.
