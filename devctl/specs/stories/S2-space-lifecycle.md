# Stories — space lifecycle

A space is one running copy of the platform: one EC2 instance in the one
account, named by its domain, found by its tags. A space is exactly one label
under the root domain: `sbx1.ikigenba.dev` is a space, and `crm.sbx1.ikigenba.dev`
is an app on it. There is no nesting, no grouping, and no space at the root
itself; the root is an A record `apex` points at one app on one space (see
`S7-apex.md`). `space` is the command that lists, creates, destroys, stops,
and starts spaces, sets one's host up again, asks one what it is running,
restarts, disables, or enables one of its apps, and reads that app's
journal. Nothing is kept on the developer's machine; the cloud is the
registry.

Every command here takes `<space>`, which is the space's label or its full
domain: `sbx1` and `sbx1.ikigenba.dev` name the same space. The root suffix
is stripped if present and what remains must be one valid DNS label. Output
always says the full domain. The commands that also name an app take it as a
second operand, `<space> <app>`, never as a hostname.

Everything Terraform made for the platform is named after the root and found
by that name: the hosted zone, the launch template, the permissions boundary,
the backup bucket, and the ssh key pair are all `ikigenba.dev`. What devctl
makes for a space is named after the space: the role and its instance profile
are `<space domain>`, and the instance, its volumes, and its Elastic IP are
tagged `Domain=ikigenba.dev` and `Space=<space domain>`. A space's instance
is the one tagged `Space=<space domain>` that is not terminated; there is at
most one.

## A developer asks what `space` can do

The top-level usage gains the line `  space     list, create, destroy, stop,
start, initialise, and inspect spaces` under `Commands:`.

Command:

```
$ devctl space --help
```

Output:

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

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer asks which spaces exist

A developer about to create, deploy to, or clean up a space checks what is
there first. One line per space, sorted by domain: the domain, the instance
state, the public address or `-`, and `apex` on the one space that holds the
root domain or `-` on every other. The holder is the space whose Elastic IP
the root's A record points at; no host is asked. No spaces prints nothing.
What a space is running is `space status`, asked of the host.

Command:

```
$ devctl space list
```

Output:

```
new.ikigenba.dev running 18.220.10.5 -
sbx1.ikigenba.dev running 18.118.7.42 apex
sbx2.ikigenba.dev stopped - -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev` and `us-east-2`.
- A live SSO session for the profile `ikigenba.dev`.
- Three instances tagged `Domain=ikigenba.dev` with a `Space` tag exist in
  `us-east-2` and are not terminated.
- The zone `ikigenba.dev` holds an `A` record `ikigenba.dev` whose value is
  `18.118.7.42`, the Elastic IP tagged `Space=sbx1.ikigenba.dev`.

Postconditions:

- Nothing has changed.
- Instances were found by `DescribeInstances` with filters
  `tag:Domain=ikigenba.dev` and `tag-key=Space`, in `us-east-2`. Had the
  root's `A` record been absent, or pointed at no space's address, every line
  would end in `-`.

## A developer names something that is not a space

The operand rule is the same for every subcommand here and for every other
command that takes `<space>`: strip `.ikigenba.dev` if the operand ends in
it, and what is left must be one label. An app's hostname, a name under some
other domain, the root itself, or a label with characters DNS does not allow
are all refused before anything is looked up.

Command:

```
$ devctl space status crm.sbx1
```

```
$ devctl space status crm.sbx1.ikigenba.dev
```

```
$ devctl space status foo.example.com
```

```
$ devctl space status ikigenba.dev
```

Output:

```
devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'
```

Exits 2. The line is on stderr, naming the operand as typed; stdout is empty.

Command:

```
$ devctl space status Foo_1
```

Output:

```
devctl: 'Foo_1' is not a valid label
```

Exits 2. The line is on stderr; stdout is empty. A label is lowercase
letters, digits, and hyphens, at most 63 characters, not starting or ending
with a hyphen: the rule build applies to app names.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev`.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer creates a space

A developer wants a fresh, complete copy of the platform at a label of their
choosing, ready for a deploy. Every space is given an Elastic IP, so its
address is fixed for the whole of its life: the records are written once,
here, and every later stop and start leaves them alone. Elastic IPs are an
account quota, five per region unless the account has asked for more, and
each space holds one; when the quota is exhausted, create fails at its
address step and relays the AWS error. Each line of output is one step; the
last line is the domain and the address.

The `account` step is what devctl established before it touched anything:
the root and region from the checkout's file, and the account id the profile
reached, asked of STS. The `domain` step finds the zone named after the root.

The host needs ten configuration keys before `opsctl init` will run, and
`create` is what sets all ten. Five derive from the root and the zone: the
space's domain is `host.name`, the zone is `dns.zones`, the provider is
`route53`, the region is `aws.region`, and the bucket with the space's label
is `backup.s3_uri`, `s3://ikigenba.dev/sbx1/`. Four are the backup periods,
which create sets to devctl's defaults: the host's own files and every
service's files daily (`86400`), a declared database snapshotted daily
(`86400`), and its committed changes shipped every five minutes (`300`). A
space that wants other periods sets them on the host with `opsctl config
set` and runs `space init`, which leaves them alone. The tenth, the address
the CA sends expiry warnings to, derives from nothing, so the developer
supplies it with `--acme-email`. It is required rather than defaulted: a
wrong address is only discovered when a certificate quietly expires.

The `secrets` step writes the same objects `secrets push` writes, one per app
in the checkout, and it is the one writer that does not require the space's
instance to exist, because create is what is about to launch it. The step
comes before the role and the instance so that a create refused for a
missing keyring value leaves nothing in the account.

The opsctl it installs is the newest release opsctl has published. The host
then stays on that version until someone explicitly moves it: `create` chooses
the first version, and `space init --opsctl` is the only later devctl command
that changes it, by fetching that release's installer and running it, just
as `create` does.

Backups are kept when a space is destroyed unless the developer says
otherwise, so a space may have lived before, and its certificate and store
are then in the bucket. The `restore` step, between `opsctl` and `init`,
looks under the space's own prefix for a host backup; the CA allows five
certificates for the same names in any seven days, and a space created and
destroyed that often would reach it without this. This is the first time, so
there is nothing to bring back and the line says so; the rebuild story below
shows the other case.

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

```
$ devctl space create sbx1.ikigenba.dev --acme-email ops@ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
secrets: ok (3 apps)
role: ok (sbx1.ikigenba.dev)
instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)
address: ok (elastic ip 18.118.7.42 associated)
records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)
host: ok (status checks passed, cloud-init done)
opsctl: ok (v0.3.0 installed, 10 keys set)
restore: ok (no host backup)
init: ok
sbx1.ikigenba.dev 18.118.7.42
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev` and `us-east-2`.
- A live SSO session for the profile `ikigenba.dev`.
- Terraform has been applied: the account holds the hosted zone, the launch
  template, the permissions boundary policy, the backup bucket, and the key
  pair, all named `ikigenba.dev`.
- No instance is tagged `Space=sbx1.ikigenba.dev`, and no role or instance
  profile named `sbx1.ikigenba.dev` exists.
- The zone holds no `NS` record for `sbx1.ikigenba.dev`.
- Every app in the checkout has the values its manifest's `secrets` array
  names in the developer's keyring (see `S3-secrets.md`).
- opsctl has a published release, and the host can reach it over the network.
- `--acme-email` names an address the CA will accept.
- The developer's ssh configuration can reach a new instance as `ec2-user`
  with the `ikigenba.dev` key pair.
- Nothing is under `sbx1/host/` in the bucket.

Postconditions:

- The role `sbx1.ikigenba.dev` exists with the `ikigenba.dev` permissions
  boundary attached and one inline policy named `space`, which allows the
  host to read parameters under `/sbx1.ikigenba.dev/`, to read, write, and
  list objects under `sbx1/` in the bucket and nothing outside it, and to
  write `TXT` records at `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev` in the
  zone and no other record. The instance profile of the same name holds the
  role.
- One instance is running, launched from the `ikigenba.dev` launch template
  with that profile, tagged `Domain=ikigenba.dev` and `Space=sbx1.ikigenba.dev`
  on the instance and its volume. It has passed its status checks and
  `cloud-init status --wait` has returned.
- An Elastic IP tagged `Domain=ikigenba.dev` and `Space=sbx1.ikigenba.dev` is
  allocated and associated with the instance.
- The space's records, the Route 53 `A` records `sbx1.ikigenba.dev` and
  `*.sbx1.ikigenba.dev` with TTL 60 in the zone, point at the Elastic IP and
  the change is `INSYNC`.
- Every app's secrets object is at `/sbx1.ikigenba.dev/<app>` (see
  `S3-secrets.md`).
- `opsctl` is installed on the host and on root's PATH, and its configuration
  store holds exactly the ten keys opsctl declares: `host.name=sbx1.ikigenba.dev`,
  `dns.provider=route53`, `dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78`,
  `aws.region=us-east-2`, `backup.s3_uri=s3://ikigenba.dev/sbx1/`,
  `acme.email` from `--acme-email`, `backup.host_files_seconds=86400`,
  `backup.service_files_seconds=86400`, `backup.service_db_seconds=86400`,
  and `backup.service_wal_seconds=300`. `host.apex` is not set.
- `sbx1/host/` was listed and found empty, so `opsctl host restore` was not
  run and the ten keys were set once.
- `sudo opsctl init` has exited 0 on the host, so the host holds its
  certificate, its generated nginx configuration, its litestream configuration
  and unit, its two backup timers, each enabled whose period is non-zero, and
  its certificate renewal timer, enabled.
- No apps are deployed; that is `deploy`.

## A developer rebuilds a space

Backups are kept by default, and a rebuild is how they earn their keep: the
host is gone, by choice or by accident, and a new one is to hold everything
the old one held. Every step is a command that already exists; what this
story adds is the order, and why it is that order.

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
3. `restore <space> <app>` for each app. The host has never run the app,
   so the restore lands its `etc/` and `state/` and its database with no
   binary and no unit, and litestream replicates the database from that
   moment.
4. `deploy <space> <file>` for each app, over the restored data. install
   replaces `bin/`, `etc/`, and `share/` and leaves `state/` alone, so the
   app starts over its own data.

Restore comes before deploy, not after. A deploy onto empty state starts the
app, and an app that finds no database makes one, migrates it, and seeds it
(see opsctl's `S7-apps.md`); a restore after that has to stop the app and
replace what it made. A restore first has nothing to stop, and the deploy
that follows is an ordinary deploy: the app finds its database, migrates it
forward, and seeds nothing.

A rebuilt space brings every app back enabled, including one that was
disabled on the old host. Whether an app is disabled lives only in the
host's own units, which no backup holds, just as a `remove` ends the
disabled state. An app that should stay offline is disabled again with
`space disable` after its deploy.

If the space held the apex, the destroy removed the root's record, and the
new host answers only at its own names until `apex set` points the root at
it again (see `S7-apex.md`). The backup's store carries the old host's
`host.apex`, and create removes it after the restore, so a rebuilt host never
holds the apex until `apex set` says so; `init` then reissues the certificate
without the apex name, since the restored one carries a name the store no
longer asks for.

Command:

```
$ devctl space destroy staging
$ devctl space create staging --acme-email ops@ikigenba.dev
$ devctl restore staging crm
$ devctl restore staging dashboard
$ devctl deploy staging crm/dist/crm-v0.1.0.tar.xz
$ devctl deploy staging dashboard/dist/dashboard-v0.0.9.tar.xz
$ devctl space status staging
```

Output, with the create's lines before `opsctl` and the deploys' lines
before `install` as in their own stories:

```
retire: ok (opsctl retire)
instance: ok (i-0f1e2d3c4b5a69788 terminated)
address: ok (elastic ip 18.117.42.9 released)
records: ok (deleted staging.ikigenba.dev, *.staging.ikigenba.dev)
secrets: ok (kept)
backups: ok (kept)
role: ok (staging.ikigenba.dev deleted)
...
opsctl: ok (v0.3.0 installed, 10 keys set)
restore: ok (host/2026-09-12T14:22:51Z.tar.zst, 10 keys set again)
init: ok
staging.ikigenba.dev 18.117.42.9
restore: ok (opsctl restore crm)
restore: ok (opsctl restore dashboard)
...
install: ok (opsctl installed crm)
...
install: ok (opsctl installed dashboard)
crm v0.1.0 active active wal
dashboard v0.0.9 active active -
```

Each command exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `crm` and `dashboard` are
  deployed on it; `crm` declares a database. It does not hold the apex.
- The developer's ssh configuration can reach the old instance and the new
  one as `ec2-user`.
- `crm/dist/crm-v0.1.0.tar.xz` and `dashboard/dist/dashboard-v0.0.9.tar.xz`
  exist, written by `build`.

Postconditions:

- After the destroy, `staging/host/`, `staging/crm/`, and
  `staging/dashboard/` in the bucket each hold an object from the retire,
  and the secrets under `/staging.ikigenba.dev/` are untouched.
- After the create, the new host holds the old certificate: `opsctl host
  restore` was run over ssh and exited 0 before the ten keys were set
  again, and `init`'s certificate step found it current and asked the CA
  for nothing. The store holds the ten keys as create derived them, plus
  any other key the backup carried except `host.apex`, which create removed
  after the restore whether or not the backup had it.
- After the restores, `/opt/crm/` and `/opt/dashboard/` hold the backups'
  `etc/` and `state/`, `crm`'s database is rebuilt to its last committed
  transaction and replicating, and neither has a binary or a unit.
- After the deploys, both apps answer from their binaries over the restored
  data, and `space status` shows them as above.
- The new instance has a new id and may have a new address; the records
  point at it. Nothing about the old instance remains in the account.

## A developer creates a space at a name delegated away from the zone

A name the zone hands to other name servers with an `NS` record is not the
zone's to answer for, so a space there would never resolve. Nothing in the
platform delegates any more; the guard is for a record left over from before
the zone was the only one.

Command:

```
$ devctl space create sbx --acme-email ops@ikigenba.dev
```

Output:

```
devctl: 'sbx.ikigenba.dev' is delegated away from 'ikigenba.dev'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone `ikigenba.dev` holds an `NS` record for `sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer creates a space that already exists

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
devctl: a space at 'sbx1.ikigenba.dev' already exists (i-0c9e94542d98846a8)
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- An instance tagged `Space=sbx1.ikigenba.dev` exists and is not terminated.

Postconditions:

- Nothing has changed.

## A developer creates a space whose role a failed create left behind

A create that failed after its `role` step left the role and the instance
profile in the account, and a second create would find them in its way. It
refuses rather than adopt them, and `space destroy` is what clears them.

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
devctl: a role for 'sbx1.ikigenba.dev' already exists
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance is tagged `Space=sbx1.ikigenba.dev`; a role or an instance
  profile named `sbx1.ikigenba.dev` exists.

Postconditions:

- Nothing has changed.

## A developer creates a space with a secret missing from the keyring

Command:

```
$ devctl space create new --acme-email ops@ikigenba.dev
```

Output:

```
devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- `crm/etc/manifest.toml` lists `CRM_API_KEY` in `secrets` and neither the
  keyring nor the environment has it.

Postconditions:

- Nothing has changed. No secrets object was written for any app.

## A developer creates a space before Terraform has been applied

The zone, the launch template, and the permissions boundary are looked up by
the root's name, and an account that has none of them is one Terraform has
not been applied to. That is a fact about the account, not about the
command, and it is found before any step runs.

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
devctl: no launch template 'ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty. A missing zone says
`devctl: no hosted zone 'ikigenba.dev'`, and a missing boundary
`devctl: no permissions boundary 'ikigenba.dev'`; the first missing one, in
that order, is the one reported. The bucket is used by name and never looked
up, so a missing bucket surfaces as the AWS error of the first step that
touches it.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The account has no launch template named `ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer runs `space create` without a space

Command:

```
$ devctl space create
```

Output:

```
devctl: space create needs <space>

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
arguments, before the checkout is read.

Command:

```
$ devctl space create sbx1
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
  `<space>`: a missing operand and a missing required option are both
  answered before the checkout is read.

## A developer's create fails part-way

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
secrets: ok (3 apps)
role: ok (sbx1.ikigenba.dev)
devctl: ec2 RunInstances: InsufficientInstanceCapacity
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- As for a create that succeeds, and EC2 has no capacity for the launch
  template's instance type.

Postconditions:

- The steps that printed `ok` hold: the secrets objects and the role exist.
  No instance was launched.
- `space destroy sbx1` removes what exists. `space create sbx1` again is
  refused because the role exists.

## A developer's SSO session has expired

The profile is named after the root and devctl hands the name to the AWS
SDK; what the SDK cannot do with it is the SDK's to say. The first call any
cloud command makes is to STS, so an expired or missing session fails there,
and the line is the AWS error as every other AWS failure is reported.

Command:

```
$ devctl space list
```

Output:

```
devctl: sts GetCallerIdentity: the SSO session has expired or is invalid
```

Exits 1. The line is on stderr; stdout is empty. The text after the colon
is the SDK's, and reads as the SDK phrases it.

Preconditions:

- The working directory is inside the checkout.
- No live SSO session for the profile `ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer sets a space's host up again

`create`'s `opsctl` and `init` steps, run again on a space that exists.
Everything the host generates is generated from its configuration store, and
`opsctl init` is what reads the store and makes the host match it; so when
something the store came from has changed, the way back to a host that
matches is to set the keys again and run `init` again. Terraform changed the
region; an operator ran `opsctl host restore` and the host now holds a store
that `init` has not acted on; an operator changed a backup period on the host
and wants its timer to follow; a manifest was restored that `init` has not
read. Nothing here needs ssh by hand.

The five keys `create` derives are derived the same way, from the root, the
zone, and the space, and set again; a value that has not changed is written
over with itself. The other five derive from nothing: `acme.email` is left as
it is unless `--acme-email` says otherwise, and the four backup periods are
left as they are, because `create`'s defaults were only defaults and an
operator may have changed them since. Any key an operator set by hand is
untouched, `host.apex` included. The `opsctl` line says which version the
host is on and whether this command put it there. Then `init` runs and its
report is not relayed: it succeeded.

Command:

```
$ devctl space init sbx1
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
opsctl: ok (v0.3.0 kept, 5 keys set)
init: ok
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- The host's configuration store holds the five derived keys at the values
  the root, the zone, and the space give now: `host.name`, `dns.provider`,
  `dns.zones`, `aws.region`, and `backup.s3_uri`. `acme.email`, the four
  backup periods, `host.apex`, and every other key are as they were.
- `/usr/local/bin/opsctl` is the version it was; no installer was fetched or
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

`--opsctl` names a release, and that release's installer is fetched onto the
host and run with that version as its operand before the keys are set and
`init` runs.
Installing the binary changes nothing on the host by itself: what a new
version changes is `init`'s to do, which is why the two are one command.

Command:

```
$ devctl space init staging --opsctl v0.3.0
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
instance: ok (i-0f1e2d3c4b5a69788 running, 18.117.42.9)
opsctl: ok (v0.3.0 installed, 5 keys set)
init: ok
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The release `opsctl/v0.3.0` exists and the host can reach it.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- `/usr/local/bin/opsctl` is `v0.3.0`.
- Everything the plain re-initialisation's postconditions say. `init` was
  the new version's, so whatever `v0.3.0` generates differently is on the
  host.
- Naming the version that is already installed writes the same bytes and
  reports `v0.3.0 installed` all the same: the installer ran, and the line
  says what it did. A version with no release fails at the `opsctl` step,
  before any key is set, with the installer's diagnostic relayed the way a
  failed `init` is below.

## A developer changes where the CA writes

The one key `create` could not derive is the one `space init` cannot either,
so it is the one option that changes it.

Command:

```
$ devctl space init sbx1 --acme-email alerts@ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
opsctl: ok (v0.3.0 kept, 6 keys set)
init: ok
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
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
$ devctl space init sbx1
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
opsctl: ok (v0.3.0 kept, 5 keys set)
devctl: init: ssh ec2-user@18.118.7.42 sudo opsctl init: exit status 2

> nginx: ok (/usr/sbin/nginx)
> certbot: failed: not found on PATH
> systemctl: ok (/usr/bin/systemctl)
> litestream: ok (/usr/bin/litestream)
> dns.provider: ok (route53)
> dns.zones: ok (ikigenba.dev)
> host.name: ok (sbx1.ikigenba.dev)
> timeouts: ok (drain 5s, stop 10s)
> zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
> host sbx1.ikigenba.dev: ok (zone ikigenba.dev)
> wildcard sbx1.ikigenba.dev: ok (18.118.7.42)
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `certbot` has been removed from the host.

Postconditions:

- The store holds the five keys as set. Nothing `init` generates was
  written: the host's certificate, nginx configuration, litestream
  configuration, and timers are as they were.
- Running `space init` again once the host is fixed finishes the job.

## A developer initialises a stopped space, or one that does not exist

`init` needs a host to talk to. A stopped space is refused the way `status`
refuses it, and a space that does not exist the way every subcommand refuses
one.

Command:

```
$ devctl space init sbx2
```

Output:

```
devctl: 'sbx2.ikigenba.dev' is stopped
```

Command:

```
$ devctl space init gone
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Each exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The instance tagged `Space=sbx2.ikigenba.dev` is `stopped`; no instance
  is tagged `Space=gone.ikigenba.dev`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer runs `space init` without a space

Command:

```
$ devctl space init
```

Output:

```
devctl: space init needs <space>

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
nothing costs money. Each line of output is one step. Two things a space
owned are kept unless the developer says otherwise: its secrets, because
they are the developer's values and cost nothing, and its backups, because
they are the point of a rebuild. Before the instance goes, devctl has the
host take its final backup with `sudo opsctl retire` over ssh, which stops
every service, lets litestream ship what it holds, and writes the service
and host backups one last time; the timers only copy on their own schedule,
and this is what makes a destroy lose nothing. A zero exit from retire is
the whole of the step's success; what retire does on the host, and what it
backs up, is opsctl's.

Command:

```
$ devctl space destroy staging
```

Output:

```
retire: ok (opsctl retire)
instance: ok (i-0a1b2c3d4e5f60718 terminated)
address: ok (elastic ip 18.220.10.5 released)
records: ok (deleted staging.ikigenba.dev, *.staging.ikigenba.dev)
secrets: ok (kept)
backups: ok (kept)
role: ok (staging.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it, with `crm` and `dashboard` deployed. It does not hold the apex.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- `sudo opsctl retire` has been run over ssh and exited 0, so
  `staging/crm/`, `staging/dashboard/`, and `staging/host/` in the bucket
  each hold a new object from that run, and `crm`'s database replica is
  complete to its last committed transaction.
- The space's instance is terminated and its Elastic IP is disassociated and
  released.
- The `A` records `staging.ikigenba.dev` and `*.staging.ikigenba.dev` are
  gone from the zone. The wildcard is read back first, because Route 53
  returns it as `\052.staging.ikigenba.dev` and the delete must match
  exactly.
- Every parameter under `/staging.ikigenba.dev/` and every object under
  `staging/` in the bucket are untouched, the new objects included.
- The inline policy, the instance profile, and the role are gone.

## A developer destroys a space and its secrets and backups with it

`--delete-secrets` and `--delete-backups` say the two kept things are not
wanted. Each governs its own step and nothing else: `retire` still runs,
because the developer has not said the loss is acceptable, only that the
copies are not wanted afterwards. A developer who wants neither the backup
taken nor the copies kept says both, `--no-backup --delete-backups`.

Command:

```
$ devctl space destroy sbx1 --delete-secrets --delete-backups
```

Output:

```
retire: ok (opsctl retire)
instance: ok (i-0c9e94542d98846a8 terminated)
address: ok (elastic ip 18.118.7.42 released)
records: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)
secrets: ok (3 parameters deleted)
backups: ok (12 objects deleted)
role: ok (sbx1.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it. It does not hold the apex.
- Three parameters exist under `/sbx1.ikigenba.dev/`, and after the retire
  twelve objects exist under `sbx1/` in the bucket.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- Everything a plain destroy's postconditions say, and every parameter under
  `/sbx1.ikigenba.dev/` and every object under `sbx1/` in the bucket, the
  retire's own objects and `deploy/` included, are deleted. The bucket's
  name has dots in it, so the deletes addressed it path-style.
- Either option alone deletes its one thing and the other line reads
  `kept`.

## A developer destroys the space that holds the apex

The root's `A` record points at this space's address, and an address about
to be released must not be pointed at. So the record goes first, as its own
step, before anything else; the apex then resolves to nothing until `apex
set` names another app (see `S7-apex.md`). Nothing is done on the host about
it: the host, its store, its certificate, and its role are all about to go.
A space that does not hold the apex has no `apex` line.

Command:

```
$ devctl space destroy sbx1
```

Output:

```
apex: ok (ikigenba.dev record deleted)
retire: ok (opsctl retire)
instance: ok (i-0c9e94542d98846a8 terminated)
address: ok (elastic ip 18.118.7.42 released)
records: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)
secrets: ok (kept)
backups: ok (kept)
role: ok (sbx1.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it.
- The zone's `A` record `ikigenba.dev` points at `18.118.7.42`, the space's
  Elastic IP.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- The zone holds no `A` record `ikigenba.dev`. `space list` shows `-` in the
  last column of every line, and `apex show` prints nothing.
- Everything a plain destroy's postconditions say. With `--no-backup` the
  `apex` line still comes first and `retire` is skipped as below.

## A developer destroys a space whose host cannot take a final backup

`retire` failing is a reason not to terminate: the whole point of the step
is that nothing is lost, and what opsctl reported is on the host, not gone.
opsctl's output follows the error line, each line quoted with `> `. The
instance is left as retire left it, with its services stopped, so the
developer can fix the host and run destroy again, or decide the loss is
acceptable and pass `--no-backup`.

Command:

```
$ devctl space destroy staging
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

- As for a plain destroy, and `opsctl retire` on the host exits non-zero.

Postconditions:

- Nothing in the account has changed: the instance is `running`, and its
  address, records, secrets, backups, and role are as they were.
- On the host, the units are whatever retire left; here, stopped.
- Running destroy again runs retire again.

## A developer destroys a stopped space

A stopped host cannot take a backup. It is refused the way `status` refuses
a stopped space, and the way out is to start it or to say the loss is
accepted.

Command:

```
$ devctl space destroy staging
```

Output:

```
devctl: retire: instance i-0a1b2c3d4e5f60718 is stopped

run 'devctl space start staging' first, or pass --no-backup
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The instance tagged `Space=staging.ikigenba.dev` is `stopped`.

Postconditions:

- Nothing has changed.

## A developer destroys a space without its final backup

`--no-backup` skips `retire`. What the timers had not copied since their
last run is lost with the instance: up to a day of service files, up to a
day of the host's own configuration, and up to five minutes of committed
database changes at the default periods. The line says the step was skipped
so the record of the destroy says so too.

Command:

```
$ devctl space destroy staging --no-backup
```

Output:

```
retire: skipped (--no-backup)
instance: ok (i-0a1b2c3d4e5f60718 terminated)
address: ok (elastic ip 18.220.10.5 released)
records: ok (deleted staging.ikigenba.dev, *.staging.ikigenba.dev)
secrets: ok (kept)
backups: ok (kept)
role: ok (staging.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists; its instance may be `running` or `stopped`, and the host
  need not be reachable.

Postconditions:

- No ssh connection was made. Nothing new is in the bucket.
- Otherwise as for a plain destroy.

## A developer destroys a space that is already gone

Command:

```
$ devctl space destroy sbx1
```

Output:

```
retire: ok (already gone)
instance: ok (already gone)
address: ok (already gone)
records: ok (already gone)
secrets: ok (already gone)
backups: ok (already gone)
role: ok (already gone)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- Nothing tagged or named for `sbx1.ikigenba.dev` exists in the account, and
  no parameter or object is under its prefixes.

Postconditions:

- Nothing has changed. No ssh connection was made.
- Without `--delete-secrets` or `--delete-backups`, `secrets` and `backups`
  say `kept` whether or not anything is there; `already gone` is what the
  options say when they find nothing to delete.

## A developer runs `space destroy` without a space

Command:

```
$ devctl space destroy
```

Output:

```
devctl: space destroy needs <space>

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
$ devctl space destroy sbx1 --no-backup
```

Output:

```
retire: skipped (--no-backup)
instance: ok (i-0c9e94542d98846a8 terminated)
address: ok (elastic ip 18.118.7.42 released)
devctl: route53 ChangeResourceRecordSets: Throttling
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists; Route 53 throttles the change.

Postconditions:

- The steps that printed `ok` hold; the remaining steps did not run.
- Running destroy again resumes from wherever things stand.

## A developer stops a space

A developer leaves a space for a while but wants its disk and its state back
later. The records stay, because the address does: a stop releases nothing.
If the space holds the apex, the root keeps pointing at it and answers
nothing until the space starts again; moving the apex is `apex set`.

Command:

```
$ devctl space stop staging
```

Output:

```
instance: ok (i-0a1b2c3d4e5f60718 stopped)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`.

Postconditions:

- The instance is `stopped`. Its volume, its tags, its role, its secrets, its
  backups, and its `A` records `<space domain>` and `*.<space domain>` are
  untouched.

## A developer stops a space that is already stopped

Command:

```
$ devctl space stop sbx2
```

Output:

```
instance: ok (already stopped)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
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
$ devctl space start staging
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

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `stopped`.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- The instance is `running` and has passed its status checks.
- The `A` records `staging.ikigenba.dev` and `*.staging.ikigenba.dev` still
  point at the Elastic IP.
- `sudo certbot renew` has been run on the host over ssh and exited 0.

## A developer asks what a space is running

The answer comes from the host, never from a record kept elsewhere: over ssh,
`opsctl status` lists the installed apps, asks each app's binary its version,
reads the state of each app's service unit and of its socket unit, and reads
the journal mode of the database each app declares. `space status` copies that
output byte for byte. One line per app, in name order: the app, the version,
the service state, the socket state, and the database journal mode (`-` for an
app that declares no database). A service that is `inactive` behind an
`active` socket is idle, not down: the next request starts it. A mode other
than `wal` means that database is no longer reaching S3.

Command:

```
$ devctl space status sbx1
```

Output:

```
crm v0.1.0 active active wal
dashboard v0.0.9 active active -
gmail v0.1.0 failed active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- Three apps are installed on the host, and every socket is listening.

Postconditions:

- Nothing has changed.

## A developer asks what a space with a disabled app is running

A disabled app's socket field reads `disabled`, whatever the socket unit's
state, so the line says why its service is not running: the app was taken
offline with `space disable`, not stopped by hand or failed. devctl copies the
line as opsctl wrote it, like every other.

Command:

```
$ devctl space status sbx1
```

Output:

```
crm v0.1.0 inactive disabled wal
dashboard v0.0.9 active active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed and disabled; `dashboard` is installed and enabled, and
  its service is `active`.

Postconditions:

- Nothing has changed. A disabled app is a fact about the host, reported in
  its line with exit 0, as a failed unit is.

## A developer asks what a space with no apps is running

Command:

```
$ devctl space status new
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it
  and no app has been deployed.

Postconditions:

- Nothing has changed.

## A developer asks what a stopped space is running

`space restart`, `space disable`, `space enable`, and `space logs` need the
same host and refuse a stopped space with the same line.

Command:

```
$ devctl space status sbx2
```

Output:

```
devctl: 'sbx2.ikigenba.dev' is stopped
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `stopped`.

Postconditions:

- Nothing has changed.

## A developer stops, starts, or asks about a space that does not exist

Command:

```
$ devctl space stop gone
```

```
$ devctl space start gone
```

```
$ devctl space status gone
```

```
$ devctl space restart gone crm
```

```
$ devctl space disable gone crm
```

```
$ devctl space enable gone crm
```

```
$ devctl space logs gone crm
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance is tagged `Space=gone.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer's start cannot reach the host

Command:

```
$ devctl space start sbx1
```

Output:

```
instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)
host: ok (status checks passed)
devctl: ssh ec2-user@18.118.7.42: connection timed out
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `stopped`.
- The developer's machine cannot open an ssh connection to the instance.

Postconditions:

- The instance is `running` and the `A` records `sbx1.ikigenba.dev` and
  `*.sbx1.ikigenba.dev` still point at its Elastic IP.
- No renewal check was run. Running start again on the running instance runs
  the check.

## A developer restarts an app on a space

Over ssh, `opsctl restart` restarts the app's service and reports it the way
install's last line does. devctl relays that report as opsctl wrote it, byte
for byte, the way `space status` relays opsctl's answer: opsctl's own line
says what state the app is in, which a fixed line of devctl's could not. A
restart changes nothing on the host's disk. In particular it does not
carry a pushed secret to the app: that is a deploy of the same file, and
`S3-secrets.md` says why.

Command:

```
$ devctl space restart sbx1 crm
```

Output:

```
service: ok (crm v0.1.0 active)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the host and enabled. Its service may be `active`,
  `inactive`, or `failed`.

Postconditions:

- `sudo opsctl restart crm` has been run on the host over ssh and exited 0,
  so `ikigenba-crm.service` is `active` under a new process. What restart
  does on the host is opsctl's; its socket kept listening throughout.
- Nothing on the host's disk changed, and no other app on the space has
  changed.

## A developer restarts a disabled app on a space

A disabled app stays disabled through everything but `space enable`, so
opsctl starts nothing and succeeds: the host is in the state the developer
chose. devctl relays opsctl's line, as for any restart, and that line says the
app is disabled rather than reporting a restart that did not happen.

Command:

```
$ devctl space restart sbx1 crm
```

Output:

```
service: ok (crm v0.1.0 disabled)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the host and disabled.

Postconditions:

- `sudo opsctl restart crm` has been run on the host over ssh and exited 0.
  Nothing has changed: neither of `crm`'s units was started or enabled, its
  names still answer `503`, and `space status` still shows
  `crm v0.1.0 inactive disabled wal`.

## A developer's restart fails on the host

`opsctl`'s output follows the error line: what it wrote to its stdout, then
what it wrote to its stderr, each line quoted with `> `, so the step that
failed and the journal the host relayed are in front of the developer.

Command:

```
$ devctl space restart sbx1 crm
```

Output:

```
devctl: restart: ssh ec2-user@18.118.7.42 sudo opsctl restart crm: exit status 1

> service: failed: crm: service failed to start
> opsctl: restart failed
> 
> > ikigenba-crm.service: Main process exited, code=exited, status=1/FAILURE
> > crm: open /opt/crm/state/crm.db: permission denied
```

Exits 1. The text is on stderr; stdout is empty. An app that is not on the
host is refused the same way, with `> service: failed: no service 'gmail'`
and `> opsctl: restart failed`, or `> service: failed: gmail is not
installed` and the same last line for one the host holds data for but never
installed.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `opsctl restart crm` on the host exits non-zero.

Postconditions:

- `crm` on the host is whatever `opsctl` left; `space status` reports it,
  here `crm v0.1.0 failed active wal`.
- No other app on the space has changed.

## A developer takes an app on a space offline

A developer wants an app to stop answering without taking it off the space:
its release, its data, and its units stay, and deploying it again later is
not needed to bring it back. Over ssh, `opsctl disable` stops the app's
socket and service and disables both, so neither starts at boot or on a
request, and regenerates nginx so the app's names answer `503`. devctl
relays opsctl's report byte for byte, as `space restart` does: the `stop`
line names the units it stopped and the `nginx` line every name the app
answers at.

The app stays disabled through `deploy`, `restore`, `space init`, and
`space restart`; only `space enable` brings it back. `remove` takes it off the
space altogether, and a later deploy installs it enabled.

Command:

```
$ devctl space disable sbx1 crm
```

Output:

```
stop: ok (ikigenba-crm.socket, ikigenba-crm.service stopped, disabled)
nginx: ok (crm.sbx1.ikigenba.dev disabled)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the host and enabled.

Postconditions:

- `sudo opsctl disable crm` has been run on the host over ssh and exited 0.
  What disable does on the host is opsctl's: `crm`'s socket and service are
  stopped and disabled, and `https://crm.sbx1.ikigenba.dev` answers `503`.
- `space status sbx1` shows `crm v0.1.0 inactive disabled wal`.
- Nothing under `/opt/crm/` changed, and no other app on the space has
  changed.

## A developer brings a disabled app back

Over ssh, `opsctl enable` enables and starts the app's socket, regenerates
nginx so the app's names reach it again, and starts its service. devctl
relays opsctl's report byte for byte, as `space restart` does.

Command:

```
$ devctl space enable sbx1 crm
```

Output:

```
enable: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx1.ikigenba.dev)
service: ok (crm v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the host and disabled.

Postconditions:

- `sudo opsctl enable crm` has been run on the host over ssh and exited 0.
  What enable does on the host is opsctl's: both of `crm`'s units are enabled
  and started, and `https://crm.sbx1.ikigenba.dev` reaches `crm` again.
- `space status sbx1` shows `crm v0.1.0 active active wal`.
- Nothing under `/opt/crm/` changed, and no other app on the space has
  changed.

## A developer's enable fails on the host

The app is enabled, but its service will not come up. opsctl's output follows
the error line, quoted with `> ` as a failed restart's is.

Command:

```
$ devctl space enable sbx1 crm
```

Output:

```
devctl: enable: ssh ec2-user@18.118.7.42 sudo opsctl enable crm: exit status 1

> enable: ok (ikigenba-crm.socket, ikigenba-crm.service)
> nginx: ok (crm.sbx1.ikigenba.dev)
> service: failed: crm: service failed to start
> opsctl: enable failed
> 
> > ikigenba-crm.service: Main process exited, code=exited, status=1/FAILURE
> > crm: open /opt/crm/state/crm.db: permission denied
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the host and disabled, and its binary exits at start.

Postconditions:

- `crm` is enabled: both its units are enabled, its socket is listening, and
  nginx routes its names to it. Its service is `failed`, and `space status`
  shows `crm v0.1.0 failed active wal`. Nothing was rolled back.
- No other app on the space has changed.

## A developer disables an app that is already disabled, or enables one that is already enabled

Both commands ask for the state the app is in afterwards, so opsctl succeeds
and changes nothing when the app is already in it. devctl relays opsctl's
report as for any success, so the lines say what was already so.

Command:

```
$ devctl space disable sbx1 crm
```

Output:

```
stop: ok (ikigenba-crm.socket, ikigenba-crm.service already inactive, disabled)
nginx: ok (unchanged)
```

Command:

```
$ devctl space enable sbx1 dashboard
```

Output:

```
enable: ok (ikigenba-dashboard.socket, ikigenba-dashboard.service already enabled)
nginx: ok (unchanged)
service: ok (dashboard v0.0.9 active)
```

Each exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed and already disabled; `dashboard` is installed, already
  enabled, and its service is `active`.

Postconditions:

- `sudo opsctl disable crm` and `sudo opsctl enable dashboard` have been run
  on the host over ssh and each exited 0.
- Nothing has changed: no unit was enabled, disabled, started, or stopped,
  and nginx was not reloaded.

## A developer tries to take the authenticator offline

`auth` is the app every other app's requests are checked against, and opsctl
never disables it, whatever else is on the space. Its refusal follows the
error line, quoted with `> ` as a failed restart's is.

Command:

```
$ devctl space disable sbx1 auth
```

Output:

```
devctl: disable: ssh ec2-user@18.118.7.42 sudo opsctl disable auth: exit status 1

> stop: failed: auth is the authenticator and cannot be disabled
> opsctl: disable failed
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `auth` is installed on the host.

Postconditions:

- Nothing has changed. `auth` answers as it did, and every other app is still
  checked against it. `remove` remains the only way to take the
  authenticator off the space.

## A developer disables or enables an app that is not on the space

opsctl's refusal follows the error line, quoted with `> ` as a failed
restart's is.

Command:

```
$ devctl space disable sbx1 gmail
```

Output:

```
devctl: disable: ssh ec2-user@18.118.7.42 sudo opsctl disable gmail: exit status 1

> stop: failed: no service 'gmail'
> opsctl: disable failed
```

Command:

```
$ devctl space enable sbx1 gmail
```

Output:

```
devctl: enable: ssh ec2-user@18.118.7.42 sudo opsctl enable gmail: exit status 1

> enable: failed: no service 'gmail'
> opsctl: enable failed
```

Each exits 1. The text is on stderr; stdout is empty. An app the host holds
data for but never installed is refused the same way, with `failed: gmail is
not installed` in place of `failed: no service 'gmail'`.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- Nothing under `/opt/gmail/` on the host.

Postconditions:

- Nothing has changed.

## A developer reads an app's journal

The journal is read from the host with `journalctl` and copied to the
developer's terminal byte for byte, the way `status` copies opsctl's report.
No opsctl command is involved: reading a journal changes nothing on the host,
and the unit's name, `ikigenba-<app>.service`, is what install wrote and
promised. Without options the last 100 lines are printed.

Command:

```
$ devctl space logs sbx1 crm
```

Output:

```
Sep 12 09:07:11 ip-10-0-1-23 systemd[1]: Starting ikigenba-crm.service...
Sep 12 09:07:11 ip-10-0-1-23 systemd[1]: Started ikigenba-crm.service.
Sep 12 09:07:40 ip-10-0-1-23 crm[1842]: crm: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: open /opt/crm/state/crm.db: database is locked
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
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
$ devctl space logs sbx1 crm --since -1h --follow
```

Output: every line `ikigenba-crm.service` wrote in the last hour, then each
new line as it is written.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
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
$ devctl space logs sbx1 gmail
```

Output:

```
devctl: no app 'gmail' on 'sbx1.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty. An app that was removed is
refused the same way: its unit went with it.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- No `ikigenba-gmail.service` on the host.

Postconditions:

- Nothing has changed.

## A developer runs `space restart`, `space disable`, `space enable`, or `space logs` without a space or an app

Command:

```
$ devctl space restart sbx1
```

Output:

```
devctl: space restart needs <space> and <app>

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `space disable`, `space
enable`, and `space logs` say the same with their own names: `devctl: space
disable needs <space> and <app>`, `devctl: space enable needs <space> and
<app>`, and `devctl: space logs needs <space> and <app>`. A `--since` with no
value gives `devctl: option '--since' requires a value`, also exit 2.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made and no ssh connection was opened.
