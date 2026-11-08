# Stories — space lifecycle

A space is one running copy of the platform: one EC2 instance in the one
account, named by its domain, found by its tags. A space is exactly one label
under the root domain: `sbx1.ikigenba.dev` is a space, and `crm.sbx1.ikigenba.dev`
is an app on it. There is no nesting, no grouping, and no space at the root
itself; the root is an A record `apex` points at one app on one space (see
`S7-apex.md`). `space` is the command that lists, creates, moves, destroys,
stops, and starts spaces, asks one what it is running, restarts, disables, or
enables one of its apps, and reads that app's journal. Nothing is kept on the developer's machine; the cloud is the
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
is the one tagged `Space=<space domain>` that is neither terminated nor
retired; there is at most one. `space move` puts a space on a new instance
and retires the one it left: that instance is stopped and tagged
`Retired=<RFC3339 timestamp>` beside its `Domain` and `Space` tags, and its
Elastic IP stays associated with it and is tagged the same. A retired
instance and its address stay in the account until `space destroy` removes
them, or a person does. Every command that acts on a space's instance acts
on this one and never on a retired one; only `space create`, which refuses
while any instance carries the label, and `space destroy`, which removes
every one, look at retired instances at all.

## A developer asks what `space` can do

The top-level usage gains the line `  space     list, create, move, destroy,
stop, start, and inspect spaces` under `Commands:`.

Command:

```
$ devctl space --help
```

Output:

```
Usage: devctl space <subcommand> [arguments]

List, create, move, destroy, stop, start, and inspect spaces, and restart,
disable, enable, or read the journal of one app on one. A space is one label
under the root domain; <space> is that label or the full domain. The cloud's
tags are the only registry.

Subcommands:
  list                       one line per space
  create <space> [options]   create the space and deploy a release to it
  move <space> [options]     move the space to a new instance and restore it there
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

Options (move):
  --release <sha|tag>     the release to deploy; the newest release-<N> tag otherwise

Options (destroy):
  --no-backup             skip the final backup the host takes before it goes
  --delete-secrets        delete the space's secrets; they are kept otherwise
  --delete-backups        delete the space's backups; they are kept otherwise

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
the root's A record points at; no host is asked. Only active instances and
their addresses count: an instance or Elastic IP tagged `Retired` is
skipped, so a moved space is listed once, at its new address, and a root
record left pointing at a retired address marks no space. No spaces prints nothing.
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

Command:

```
$ devctl space create golden --acme-email ops@ikigenba.dev
```

Output:

```
devctl: 'golden' is not a usable space label: golden/ holds the golden sets
```

Exits 2. The line is on stderr; stdout is empty. Every command that takes
`<space>` refuses `golden` and `golden.ikigenba.dev` the same way, because
`s3://ikigenba.dev/golden/` holds the golden sets (see `S8-seed.md`) and a
space's prefix is its label.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev`.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer creates a space

A developer wants a fresh, complete copy of the platform at a label of their
choosing, running a release. Every space is given an Elastic IP, so its
address is fixed for the whole of its life: the records are written once,
here, and every later stop and start leaves them alone. Elastic IPs are an
account quota, five per region unless the account has asked for more, and
each space holds one; when the quota is exhausted, create fails at its
address step and relays the AWS error. Each line of output is one step; the
last line is the domain and the address.

The `account` step is what devctl established before it touched anything:
the root and region from the checkout's file, and the account id the profile
reached, asked of STS. The `domain` step finds the zone named after the root.

Create deploys a release, and without `--release` it is the newest release
tag in the local repository: of the tags named exactly `release-<N>`, `N` a
number, the one with the highest `N`, so `release-100` is newer than
`release-99`, and a release candidate such as `release-88-rc1` is never
chosen. The older `r<N>` tags are not consulted at all, whatever their
numbers. Nothing is fetched. The tag is resolved to its commit and the
release is built there, as `devctl build` builds it (see `S4-build.md`),
before anything is created in the account, so a create that cannot build
leaves nothing behind. The `build` line names the
tag it chose and the file it wrote, and the tag is the release's label on the
host.

The host needs ten configuration keys before `opsctl init` will run, and
`create` is what sets all ten. Five derive from the root and the zone: the
space's domain is `host.name`, the zone is `dns.zones`, the provider is
`route53`, the region is `aws.region`, and the bucket with the space's label
is `backup.s3_uri`, `s3://ikigenba.dev/sbx1/`. Four are the backup periods,
which create sets to devctl's defaults: the host's own files and every
service's files daily (`86400`), a declared database snapshotted daily
(`86400`), and its committed changes shipped every five minutes (`300`). A
space that wants other periods sets them on the host with `opsctl config
set`. The tenth, the address
the CA sends expiry warnings to, derives from nothing, so the developer
supplies it with `--acme-email`. It is required rather than defaulted: a
wrong address is only discovered when a certificate quietly expires.

The `secrets` step writes the same objects `secrets push` writes, one per app
in the release, the apps whose manifests the built tarball holds, reading
each value from the developer's keyring as `secrets push` does. It is the one
writer that does not require the space's instance to exist, because create
is what is about to launch it. The step comes before the role and the
instance so that a create refused for a missing keyring value leaves nothing
in the account.

A new host has no opsctl until the release reaches it. Once the `host` step
has seen the instance's first boot finish, the tarball is copied to the host
and unpacked into `/opt/ikigenba/releases/<sha>/`, as `deploy` does (see
`S5-deploy.md`), and everything create then asks of the host is asked of that
release's own opsctl, `/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl`: it
sets the ten keys, restores the host backup if there is one, runs `init`, puts
each app's data back when a host backup was found (see the rebuild story), and
last activates the release, with the tag as its label. The host's opsctl is
from then on the one in the release it runs: activate points
`/usr/local/bin/opsctl` at it, and every later activation points it at the new
release's own. Every command devctl runs on a host as root runs as its own
transient system unit, outside the ssh session's user slice, so the host's
per-user memory limit does not throttle it.

Backups are kept when a space is destroyed unless the developer says
otherwise, so a space may have lived before, and its certificate and store
are then in the bucket. The `restore` step, between `opsctl` and `init`,
looks under the space's own prefix for a host backup; the CA allows five
certificates for the same names in any seven days, and a space created and
destroyed that often would reach it without this. This is the first time, so
there is nothing to bring back and the line says so; the rebuild story below
shows the other case.

The lines from `preflight` on are opsctl's, copied as it writes them, as a
deploy's are; they are shown here for illustration, and what they say is
opsctl's.

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
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps)
role: ok (sbx1.ikigenba.dev)
instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)
address: ok (elastic ip 18.118.7.42 associated)
records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)
host: ok (status checks passed, cloud-init done)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
opsctl: ok (10 keys set)
restore: ok (no host backup)
init: ok
preflight: ok (8 apps)
label: ok (release-87)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (release-87 (4b22285))
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
- The local repository holds the tags `release-86`, `release-87` and
  `release-88-rc1`, and no other tag named `release-<N>`, as well as the
  older tags `r1` through `r7`; `release-87` points at the commit
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, where the suite builds as
  `S4-build.md` says and holds the apps `auth`, `dummy`, `events`, `mcp`,
  `repos`, `scripts`, `sites` and `telemetry`.
- Every app in the release has the values its manifest's `secrets` array
  names in the developer's keyring (see `S3-secrets.md`).
- `--acme-email` names an address the CA will accept.
- The developer's ssh configuration can reach a new instance as `ec2-user`
  with the `ikigenba.dev` key pair.
- Nothing is under `sbx1/host/` in the bucket.

Postconditions:

- `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` is in the checkout,
  as `devctl build release-87` writes it; no tag, branch or worktree was created or
  moved.
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
- Every app in the release has its secrets object at
  `/sbx1.ikigenba.dev/<app>` (see `S3-secrets.md`).
- `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/` on the
  host holds the release as it was built, and no copy of the tarball is left
  in the host's temporary directory.
- The configuration store holds exactly the ten keys opsctl declares, set by
  the release's opsctl: `host.name=sbx1.ikigenba.dev`,
  `dns.provider=route53`, `dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78`,
  `aws.region=us-east-2`, `backup.s3_uri=s3://ikigenba.dev/sbx1/`,
  `acme.email` from `--acme-email`, `backup.host_files_seconds=86400`,
  `backup.service_files_seconds=86400`, `backup.service_db_seconds=86400`,
  and `backup.service_wal_seconds=300`. `host.apex` is not set.
- `sbx1/host/` was listed and found empty, so `opsctl host restore` was not
  run and the ten keys were set once.
- `sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl init`
  has exited 0 on the host, so the host holds its certificate, its generated
  nginx configuration, its litestream configuration and unit, its two backup
  timers, each enabled whose period is non-zero, and its certificate renewal
  timer, enabled.
- `sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a release-87`
  has then exited 0, so the space runs the release labelled `release-87`, every app
  in it answers at `<app>.sbx1.ikigenba.dev`, and `opsctl` on root's PATH is
  the release's. What activate did on the host is opsctl's. The space has no
  previous release, so `rollback` is refused until the next deploy.
- With no host backup, the key the new host launched with is its host key.
  The developer's `known_hosts` holds that key for `18.118.7.42`, and a
  later command that reaches the space over ssh, such as `space status
  sbx1`, reaches it with no complaint about the host key.

## A developer creates a space running a release they name

`--release` names the release to deploy instead of the newest
`release-<N>`, in any form `deploy` takes: a tag, such as a release
candidate `release-88-rc1`, which is then the label, or a sha, full or
short, which gives no label. It is resolved in the local repository before
any AWS call, with nothing fetched.

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev --release 9e1c7a3
```

Options (create):

- `--release <sha|tag>` names the release the space runs; without it, the
  newest `release-<N>` tag.

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (dist/9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c.tar.xz)
secrets: ok (8 apps)
role: ok (sbx1.ikigenba.dev)
instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)
address: ok (elastic ip 18.118.7.42 associated)
records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)
host: ok (status checks passed, cloud-init done)
copy: ok (9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c)
opsctl: ok (10 keys set)
restore: ok (no host backup)
init: ok
preflight: ok (8 apps)
env: ok (8 apps)
units: ok (8 apps)
current: ok (9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (9e1c7a3)
sbx1.ikigenba.dev 18.118.7.42
```

Exits 0. The lines are on stdout; stderr is empty. The `build` line names a
tag only when a tag chose the release. With `--release release-88-rc1` it reads
`build: ok (release-88-rc1, dist/<sha>.tar.xz)` and the release is labelled
`release-88-rc1`. The lines from `preflight` on are opsctl's, as above.

Preconditions:

- As for a create without `--release`, with `9e1c7a3` resolving to
  `9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c` in the local repository, where
  the suite builds. Whether any `release-<N>` tag exists does not matter.

Postconditions:

- As for a create without `--release`, for the release
  `9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c`, which was activated with no
  label: `opsctl activate 9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c`.

## A developer creates a space with a release that is not a commit

The release is resolved before anything else is looked up, and refused as
`deploy` refuses it (see `S5-deploy.md`).

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev --release main
```

Output:

```
devctl: 'main' is not a commit
```

Exits 2. The line is on stderr; stdout is empty. A tag the local repository
does not hold, `HEAD`, and a sha no commit has fail the same way.

Preconditions:

- The working directory is inside the checkout; `main` is a branch, not a
  tag.

Postconditions:

- Nothing has changed. Nothing was built and no AWS call was made.

## A developer creates a space when the checkout has no release tag

Without `--release`, create deploys the newest `release-<N>` tag, and when
the local repository holds none there is nothing to deploy. Release
candidates such as `release-1-rc1` are not counted, and neither are the
older `r<N>` tags: a checkout that holds only `r1` through `r7` has no
release tag. The refusal comes before any AWS call, and names the option
that would let the create go ahead. `space move` without `--release` looks
for the same tag and refuses with the same line.

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
devctl: no release-<N> tag in this checkout; name one with --release <sha|tag>
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout.
- The local repository holds no tag named exactly `release-<N>`; it may hold
  release candidates such as `release-1-rc1`, the older tags `r1` through
  `r7`, and tags such as `feature/x`.

Postconditions:

- Nothing has changed. Nothing was built and no AWS call was made.

## A developer creates a space with a release that does not build

The build comes before anything is created, so a failed build leaves the
account as it was. It fails as `devctl build` fails at that commit (see
`S4-build.md`).

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
devctl: build dashboard: exit status 1

> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard
> cmd/dashboard/main.go:41:2: undefined: render
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr. A refusal the
build makes before compiling gives its own line from `S4-build.md` and exits
2.

Preconditions:

- As for a create that succeeds, except that at the commit the newest `release-<N>`
  names, `dashboard/` is an app that does not compile for `linux/arm64`.

Postconditions:

- Nothing has changed in the account: no secrets object, role, instance,
  address or record was made. Nothing under `dist/` has changed, and no
  worktree is left behind.

## A developer rebuilds a space

Backups are kept by default, and a rebuild is how they earn their keep: the
host is gone, by choice or by accident, and a new one is to hold everything
the old one held. It takes two commands.

1. `space destroy`, so the old host takes its final backup with `retire`
   before it goes. When the host is already lost the backup cannot be taken,
   and `space destroy --no-backup` clears what remains: the records, the
   address, and the role. What the timers had not copied is lost with it.
2. `space create`. Its `restore` step finds the host backup and has the
   release's opsctl run `host restore`, which puts `/etc/letsencrypt/`,
   `/etc/ikigenba/`, and the old host's sshd host keys back and restarts
   sshd; create then sets its ten keys again, so what it was told wins over
   the backup's copy of the store and any key an operator set by hand
   survives. `init` finds the certificate current and asks the
   CA for nothing. Because a host backup was found, create then puts each
   app's data back before any app runs: for each app in the release, in name
   order, whose prefix `<label>/<app>/` in the bucket holds a backup, the
   release's opsctl runs `restore <app>`, one `restore` line each, as `devctl
   restore` reports it (see `S6-restore.md`); an app with no backup there is
   skipped with a line that says so. Only then does it activate the release.

The data goes back before activate, not after. Activate starts every app, and
an app that finds no database makes one, migrates it, and seeds it (see
opsctl's `S7-apps.md`), and its replication would begin from that empty
database. Restoring first means each app starts over its own data: it finds
its database, migrates it forward, and seeds nothing. When no host backup is
found the space is new, and no app's data is looked for.

A rebuilt space brings every app back enabled, including one that was
disabled on the old host. Whether an app is disabled lives only in the
host's own units, which no backup holds. An app that should stay offline is disabled again with
`space disable` after the create.

If the space held the apex, the destroy removed the root's record, and the
new host answers only at its own names until `apex set` points the root at
it again (see `S7-apex.md`). The backup's store carries the old host's
`host.apex`, and create removes it after the restore, so a rebuilt host never
holds the apex until `apex set` says so; `init` then reissues the certificate
without the apex name, since the restored one carries a name the store no
longer asks for.

The new host presents the old host's sshd host key from its host restore
on. devctl's ssh to it keeps working through the rest of the create, and
afterwards every command that reaches the space over ssh reaches the new
address with no complaint about the host key.

Command:

```
$ devctl space destroy staging
$ devctl space create staging --acme-email ops@ikigenba.dev
$ devctl space status staging
```

Output, with the create's lines before `opsctl` and activate's lines between
`preflight` and `activate` as in the create story above, and opsctl's lines
from `preflight` on shown for illustration:

```
retire: ok (opsctl retire)
instance: ok (i-0f1e2d3c4b5a69788 terminated)
address: ok (elastic ip 18.117.42.9 released)
records: ok (deleted staging.ikigenba.dev, *.staging.ikigenba.dev)
secrets: ok (kept)
backups: ok (kept)
role: ok (staging.ikigenba.dev deleted)
...
opsctl: ok (10 keys set)
restore: ok (host/2026-09-12T14:22:51Z.tar.zst, 10 keys set again)
init: ok
restore: ok (opsctl restore auth)
restore: ok (opsctl restore dummy)
restore: ok (opsctl restore events)
restore: ok (opsctl restore mcp)
restore: ok (opsctl restore repos)
restore: ok (opsctl restore scripts)
restore: ok (no backup of sites)
restore: ok (opsctl restore telemetry)
preflight: ok (8 apps)
...
activate: ok (release-87 (4b22285))
staging.ikigenba.dev 18.117.42.9
auth 4b22285 release-87 active active wal
dummy 4b22285 release-87 active active -
events 4b22285 release-87 active active wal
mcp 4b22285 release-87 active active -
repos 4b22285 release-87 active active wal
scripts 4b22285 release-87 active active wal
sites 4b22285 release-87 active active wal
telemetry 4b22285 release-87 active active wal
```

Each command exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and it runs a release of
  seven apps, all of `release-87`'s but `sites`, which it has never run. It does not
  hold the apex.
- The local repository's newest `release-<N>` tag is `release-87`, at
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, where the suite builds and holds
  the eight apps.
- The developer's ssh configuration can reach the old instance and the new
  one as `ec2-user`.

Postconditions:

- After the destroy, `staging/host/` and `staging/<app>/` for each of the
  seven apps the old host ran each hold an object from the retire, and the
  secrets under `/staging.ikigenba.dev/` are untouched.
- After the create, the new host holds the old certificate: `opsctl host
  restore` was run by the release's opsctl over ssh and exited 0 before the
  ten keys were set again, and `init`'s certificate step found it current and
  asked the CA for nothing. The store holds the ten keys as create derived
  them, plus any other key the backup carried except `host.apex`, which
  create removed after the restore whether or not the backup had it.
- `sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl restore <app>`
  was run and exited 0 for each of the seven apps, after `init` and before
  activate, so each of them started over the backup's `state/` and database;
  what restore does on the host is opsctl's. `sites` had no backup, so no
  restore was run for it and it started over empty state.
- `sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a release-87`
  then exited 0, and `space status` shows every app as above.
- The new instance has a new id and may have a new address; the records
  point at it. Nothing about the old instance remains in the account.
- The new host holds the old host's sshd host keys, put back by `host
  restore`, and sshd was restarted with them. No ssh connection devctl made
  to the new address after the host restore complained of the host key, and
  `space status staging` afterwards reached it the same way.
- The developer's `known_hosts` holds no entry for the key the new host had
  at launch, before its host restore; for the new address it holds the old
  host's key, the one the new host presents.

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

Any instance carrying the label that is not terminated is in the way, a
retired one included: a space that has been moved is refused for its active
instance, and one whose active instance is gone is refused for the retired
one (see the next story). The line names the active instance when there is
one.

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

## A developer creates a space while a retired instance remains

A move that failed after it retired the old instance and before it launched
the new one leaves the label with no active instance, only a retired one,
stopped, and its Elastic IP. Create does not take that as room for a new
space: the retired instance holds the space's last state, and `space
destroy` is what removes it. The line marks the instance it names as
retired.

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
devctl: a space at 'sbx1.ikigenba.dev' already exists (i-0c9e94542d98846a8 retired)
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The only instance tagged `Space=sbx1.ikigenba.dev` that is not terminated
  is `i-0c9e94542d98846a8`, `stopped` and tagged
  `Retired=2026-11-08T18:30:00Z`.

Postconditions:

- Nothing has changed. Nothing was built.

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

The names come from the manifests in the built release, so the keyring is
read once the build has succeeded, and every value is found before any
object is written.

Command:

```
$ devctl space create new --acme-email ops@ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
devctl: auth: no value for 'GOOGLE_CLIENT_ID' in the keyring or the environment
```

Exits 2. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The release create chose builds, and its `auth/etc/manifest.toml` lists
  `GOOGLE_CLIENT_ID` in `secrets`; neither the keyring nor the environment
  has it.

Postconditions:

- Nothing has changed in the account. No secrets object was written for any
  app. `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` is in the
  checkout.

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
value gives `devctl: option '--acme-email' requires a value`, and a
`--release` with no value `devctl: option '--release' requires a value`,
each also exit 2.

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
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps)
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

## A developer's create cannot activate its release

Everything before `activate` held, and the release's opsctl failed to
activate it. As in a deploy, opsctl's stdout has already been copied up to
the step that failed, and its stderr follows devctl's error line, each line
quoted with `> `. The lines from `preflight` on are opsctl's, shown for
illustration. No last line naming the domain and address is written.

Command:

```
$ devctl space create sbx1 --acme-email ops@ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps)
role: ok (sbx1.ikigenba.dev)
instance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)
address: ok (elastic ip 18.118.7.42 associated)
records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)
host: ok (status checks passed, cloud-init done)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
opsctl: ok (10 keys set)
restore: ok (no host backup)
init: ok
preflight: ok (8 apps)
label: ok (release-87)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
nginx: ok (8 apps)
restart: failed: events: service failed to start
devctl: activate: ssh ec2-user@18.118.7.42 sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a release-87: exit status 1

> opsctl: activate failed
> 
> > ikigenba-events.service: Main process exited, code=exited, status=1/FAILURE
```

Exits 1. The lines up to the failed step are on stdout; the rest is on
stderr.

Preconditions:

- As for a create that succeeds, and `events` in the release exits as soon
  as it starts.

Postconditions:

- Every step that printed `ok` holds: the space exists, its host is set up
  and holds the release, and the space is whatever activate left it; `space
  status` reports it.
- `deploy` of a release that starts puts the space right; `space destroy`
  removes it.

## A developer moves a space to a new instance

A developer wants a space on a new host launched from the current launch
template, with its architecture and instance type, and everything the old
host held carried across: every app's data, the host's own configuration,
its users, groups, authorized keys, homes, `/srv`, and its sshd host keys.
The space keeps its names; only its instance and its address change. Each
line of output is one step; the last line is the domain, the new instance,
and its address.

Before any step runs, move checks what create checks of the account, the
hosted zone, the launch template, and the permissions boundary, and that the
label has exactly one active instance, `running` or `stopped`. The `account`
and `domain` steps are create's. The release is chosen and built as create
chooses and builds it, before anything is changed, so a move that cannot
build leaves the space where it was.

Then the old host goes. Over ssh,
`sudo systemd-run --quiet --wait --pipe --collect opsctl retire` takes its
final backup, as destroy has it do; it runs on every move, because running
it again is safe. The opsctl on the old host is whatever release it runs,
and that may be older than the one being moved to, whose host restore needs
what an older host backup lacks, such as the host's accounts. So the
`backup` step then has the release being moved to take one more host backup:
devctl asks the old host its architecture with `uname -m` (`x86_64` is
`amd64`, `aarch64` is `arm64`), compiles that release's opsctl for it on the
developer's machine, copies it to a temporary file under `/tmp` on the old
host, runs `host backup` with it as root, and removes the file. That backup
is the newest in the bucket and the one the new host is restored from. The
copy is not part of the release, which stays arm64 only, and the binary
never lands in `dist/`. The `stop` step stops the old instance, and the
`tag` step tags it and its Elastic IP `Retired=<RFC3339 timestamp>`. The old
instance and its address are not removed: they stay, stopped and retired,
until `space destroy` removes them, or a person does, and until then they
are the way back.

The new instance is launched from the launch template with the space's
existing role and instance profile, and tagged `Domain=ikigenba.dev` and
`Space=<space domain>` on the instance and its volume, as create tags it.
It is given a new Elastic IP of its own, tagged the same. The `instance`,
`address`, `host`, `copy`, and `unpack` steps are create's. No secrets are
written: the space's secrets objects are already in Parameter Store.

On the new host, the release's own opsctl sets the five keys derived from
the root, the zone, and the space, `host.name`, `dns.provider`, `dns.zones`,
`aws.region`, and `backup.s3_uri`; the space has no `--acme-email` here, and
its address for the CA and its backup periods come from the old host. The
`restore` step then has it run `host restore` over the newest host backup in
the bucket, the one the `backup` step has just written, which brings back
the old host's store, `acme.email`, the backup periods and `host.apex` among
it, with its certificate, its users and groups, their authorized keys and
homes, `/srv`, and the old host's sshd host keys, and restarts sshd; and
then sets the five keys again, so devctl's values win. Unlike create, move
keeps `host.apex`: a space that held the apex still holds it. Only after the
host restore does the `records` step point the space's records at the new
address and wait for `INSYNC`, so that the new host answers at the space's
names with the old host's identity. Then `init`, each app's data put back as
the rebuild story does it, and the release activated, as create does.

The new host presents the old host's sshd host key from its host restore
on. devctl's ssh to it keeps working through the rest of the move, and
afterwards every command that reaches the space over ssh reaches the new
address with no complaint about the host key.

The lines from `preflight` on are opsctl's, copied as it writes them, as a
deploy's are; they are shown here for illustration.

Command:

```
$ devctl space move staging
```

```
$ devctl space move staging.ikigenba.dev
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
retire: ok (opsctl retire)
backup: ok (opsctl host backup, amd64)
stop: ok (i-0a1b2c3d4e5f60718 stopped)
tag: ok (i-0a1b2c3d4e5f60718 and elastic ip 18.220.10.5 Retired=2026-11-08T18:30:00Z)
instance: ok (i-0e5d4c3b2a1f09876 running, 3.22.140.61)
address: ok (elastic ip 3.141.27.86 associated)
host: ok (status checks passed, cloud-init done)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 3.141.27.86)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
opsctl: ok (5 keys set)
restore: ok (host/2026-11-08T18:29:41Z.tar.zst, 5 keys set again)
records: ok (updated staging.ikigenba.dev, *.staging.ikigenba.dev -> 3.141.27.86, INSYNC)
init: ok
restore: ok (opsctl restore auth)
restore: ok (opsctl restore dummy)
restore: ok (opsctl restore events)
restore: ok (opsctl restore mcp)
restore: ok (opsctl restore repos)
restore: ok (opsctl restore scripts)
restore: ok (opsctl restore sites)
restore: ok (opsctl restore telemetry)
preflight: ok (8 apps)
label: ok (release-87)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (release-87 (4b22285))
staging.ikigenba.dev i-0e5d4c3b2a1f09876 3.141.27.86
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev` and `us-east-2`, and a live SSO session for the profile
  `ikigenba.dev`.
- Terraform has been applied: the account holds the hosted zone, the launch
  template, the permissions boundary policy, the backup bucket, and the key
  pair, all named `ikigenba.dev`.
- The space's instance `i-0a1b2c3d4e5f60718` is `running` with the Elastic
  IP `18.220.10.5`, and it is the only instance tagged
  `Space=staging.ikigenba.dev` that is not terminated. It runs the eight
  apps of an earlier release on `x86_64`, with that release's `opsctl`, and
  it does not hold the apex. The role and instance profile
  `staging.ikigenba.dev` exist.
- The local repository's newest `release-<N>` tag is `release-87`, at
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, where the suite builds and
  holds the apps `auth`, `dummy`, `events`, `mcp`, `repos`, `scripts`,
  `sites` and `telemetry`.
- The developer's ssh configuration can reach the old instance and a new
  one as `ec2-user` with the `ikigenba.dev` key pair.

Postconditions:

- `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` is in the checkout.
- `sudo systemd-run --quiet --wait --pipe --collect opsctl retire` has been
  run on the old host over ssh and exited 0, so a new object under
  `staging/<app>/` for each of the eight apps and a host backup of its own
  are in the bucket.
- Then `uname -m` on the old host printed `x86_64`; the opsctl of
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, compiled for `linux/amd64`,
  was copied to a temporary file under `/tmp` there, run as root with
  `host backup`, which exited 0, and the file was removed. So
  `staging/host/2026-11-08T18:29:41Z.tar.zst`, with the accounts archive
  beside it, is the newest host backup in the bucket. The developer's
  machine keeps no copy of that binary; it never landed in `dist/`, which
  holds only the release.
- `i-0a1b2c3d4e5f60718` is `stopped` and tagged
  `Retired=2026-11-08T18:30:00Z` beside its `Domain` and `Space` tags; the
  Elastic IP `18.220.10.5` is still associated with it and tagged the same.
  Both stay until `space destroy staging` removes them.
- `i-0e5d4c3b2a1f09876` is running, launched from the `ikigenba.dev` launch
  template with the instance profile `staging.ikigenba.dev`, tagged
  `Domain=ikigenba.dev` and `Space=staging.ikigenba.dev` on the instance and
  its volume, and has passed its status checks and `cloud-init status
  --wait`. A new Elastic IP, `3.141.27.86`, tagged the same, is associated
  with it. The role, its policy, and the secrets objects are as they were.
- On the new host, `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/`
  holds the release. The release's opsctl set `host.name`, `dns.provider`,
  `dns.zones`, `aws.region`, and `backup.s3_uri`, then ran `host restore`
  over `staging/host/2026-11-08T18:29:41Z.tar.zst` and exited 0, then set
  the same five keys again. The store holds the old host's `acme.email` and
  backup periods; no `config del host.apex` was run. The host holds the old
  host's users, groups, authorized keys, homes, `/srv`, certificate, and
  sshd host keys, and sshd was restarted with them.
- After that host restore and before `init`, the `A` records
  `staging.ikigenba.dev` and `*.staging.ikigenba.dev`, TTL 60, were upserted
  to `3.141.27.86` and the change is `INSYNC`.
- `sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl init`, then `sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl restore <app>` for each of the eight
  apps in name order, then `sudo systemd-run --quiet --wait --pipe --collect /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a release-87`
  each exited 0 on the new host, so every app answers at
  `<app>.staging.ikigenba.dev` over the data the old host held.
- The new host presents the old host's sshd host key: no ssh connection
  devctl made to `3.141.27.86` after the host restore complained of the
  host key, and `space status staging` afterwards reaches it the same way.
- The developer's `known_hosts` holds no entry for the key the new host had
  at launch, before its host restore; devctl recorded none.

## A developer moves a stopped space

A stopped host cannot take its final backup, so move starts it first, as
`space start` would, and the `start` step reports it; then retire runs on
it, then the `backup` step, and the move goes on as for a running space. The
instance comes back at its Elastic IP, so retire reaches it at the address
it always had.

Command:

```
$ devctl space move sbx2
```

Output, with the lines after `stop` as in the story above, for this space:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
start: ok (i-0b2c3d4e5f6071829 running, status checks passed)
retire: ok (opsctl retire)
backup: ok (opsctl host backup, amd64)
stop: ok (i-0b2c3d4e5f6071829 stopped)
...
sbx2.ikigenba.dev i-0e5d4c3b2a1f09876 3.141.27.86
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for moving a running space, except that the space is `sbx2`, whose
  only instance not terminated, `i-0b2c3d4e5f6071829`, is `stopped`, with
  the Elastic IP `18.191.20.7`.

Postconditions:

- `i-0b2c3d4e5f6071829` was started and passed its status checks before
  `sudo systemd-run --quiet --wait --pipe --collect opsctl retire`, and then
  the `backup` step's `host backup`, were run on it over ssh. It is then
  `stopped` and retired, as in the story above.
- Otherwise as for moving a running space.

## A developer moves a space to a release they name

`--release` names the release to deploy on the new host instead of the
newest `release-<N>`, in any form create's `--release` takes: a tag, which
is then the label, or a sha, full or short, which gives no label. It is
resolved in the local repository before any AWS call, with nothing fetched.
A move is how a space goes to a new instance, not how it changes release, but
the new host has to be given a release, and this is the developer's say in
which.

Command:

```
$ devctl space move staging --release 9e1c7a3
```

Options (move):

- `--release <sha|tag>` names the release the new host runs; without it, the
  newest `release-<N>` tag.

Output, with the lines between `tag` and `opsctl`, and between `init` and
`preflight`, as in the story of moving a running space:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (dist/9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c.tar.xz)
retire: ok (opsctl retire)
backup: ok (opsctl host backup, amd64)
stop: ok (i-0a1b2c3d4e5f60718 stopped)
tag: ok (i-0a1b2c3d4e5f60718 and elastic ip 18.220.10.5 Retired=2026-11-08T18:30:00Z)
...
copy: ok (9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c.tar.xz -> 3.141.27.86)
unpack: ok (/opt/ikigenba/releases/9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c)
opsctl: ok (5 keys set)
restore: ok (host/2026-11-08T18:29:41Z.tar.zst, 5 keys set again)
records: ok (updated staging.ikigenba.dev, *.staging.ikigenba.dev -> 3.141.27.86, INSYNC)
init: ok
...
preflight: ok (8 apps)
env: ok (8 apps)
units: ok (8 apps)
current: ok (9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (9e1c7a3)
staging.ikigenba.dev i-0e5d4c3b2a1f09876 3.141.27.86
```

Exits 0. The lines are on stdout; stderr is empty. The `build` line names a
tag only when a tag chose the release; with `--release release-88-rc1` it
reads `build: ok (release-88-rc1, dist/<sha>.tar.xz)` and the release is
labelled `release-88-rc1`. The lines from `preflight` on are opsctl's.

Preconditions:

- As for moving a running space, with `9e1c7a3` resolving to
  `9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c` in the local repository, where
  the suite builds. Whether any `release-<N>` tag exists does not matter.

Postconditions:

- As for moving a running space, for the release
  `9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c`, which was activated on the new
  host with no label: `opsctl activate 9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c`.

## A developer moves the space that holds the apex

The root's `A` record points at the old instance's Elastic IP, which the
move keeps but leaves on a stopped host. Once the release is active on the
new host, the `apex` step upserts the root's record to the new Elastic IP
and waits for `INSYNC`, so the root follows the space. Nothing else about
the apex is done: the restored store still holds `host.apex`, the restored
certificate already carries the root, and the role already allows the
root's challenge record. Until the step, the root answers at the old,
stopped address. A space that does not hold the apex has no `apex` line.

Command:

```
$ devctl space move sbx1
```

Output, with the lines between `tag` and `activate` as in the story of
moving a running space, for this space:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
retire: ok (opsctl retire)
backup: ok (opsctl host backup, amd64)
stop: ok (i-0c9e94542d98846a8 stopped)
tag: ok (i-0c9e94542d98846a8 and elastic ip 18.118.7.42 Retired=2026-11-08T18:30:00Z)
...
activate: ok (release-87 (4b22285))
apex: ok (ikigenba.dev -> 3.141.27.86, INSYNC)
sbx1.ikigenba.dev i-0e5d4c3b2a1f09876 3.141.27.86
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for moving a running space, except that the space is `sbx1`, whose
  only instance not terminated, `i-0c9e94542d98846a8`, is `running` with the
  Elastic IP `18.118.7.42`, and the zone's `A` record `ikigenba.dev` points
  at `18.118.7.42`. On the old host `host.apex` is `crm`, and its
  certificate carries the root.

Postconditions:

- Everything moving a running space's postconditions say, for `sbx1`.
- On the new host `host.apex` is `crm`, as the restored store held it, and
  the certificate covers `sbx1.ikigenba.dev`, `*.sbx1.ikigenba.dev`, and
  `ikigenba.dev`.
- After the release was activated, the zone's `A` record `ikigenba.dev`,
  TTL 60, was upserted to `3.141.27.86` and the change is `INSYNC`. `space
  list` marks `sbx1.ikigenba.dev` with `apex`, and no line for the retired
  instance.

## A developer moves a space that does not exist

A move needs a host to take the final backup from and a space to move, so a
label with no active instance is refused as `space stop` and `space status`
refuse it, before any step runs. A label whose only instance is retired has
no active instance and is refused the same way.

Command:

```
$ devctl space move gone
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance tagged `Space=gone.ikigenba.dev` is active: none exists, or
  every one is terminated or tagged `Retired`.

Postconditions:

- Nothing has changed. Nothing was built and no ssh connection was made.

## A developer moves a space with more than one active instance

A space has at most one active instance, and a label that carries two, made
by hand or by something other than devctl, leaves no telling which one holds
the space. Move refuses rather than pick, naming both, before any step runs;
the developer retires or removes the stray one by hand.

Command:

```
$ devctl space move sbx1
```

Output:

```
devctl: 'sbx1.ikigenba.dev' has more than one active instance (i-0c9e94542d98846a8, i-0f1e2d3c4b5a69788)
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- Two instances tagged `Space=sbx1.ikigenba.dev`, `i-0c9e94542d98846a8` and
  `i-0f1e2d3c4b5a69788`, are neither terminated nor tagged `Retired`.

Postconditions:

- Nothing has changed. Nothing was built and no ssh connection was made.

## A developer moves a space before Terraform has been applied

The new instance is launched from the launch template and holds the zone's
records, so move looks up the zone, the launch template, and the
permissions boundary by the root's name as create does, before any step
runs, and refuses with create's lines.

Command:

```
$ devctl space move sbx1
```

Output:

```
devctl: no launch template 'ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty. A missing zone says
`devctl: no hosted zone 'ikigenba.dev'`, and a missing boundary
`devctl: no permissions boundary 'ikigenba.dev'`; the first missing one, in
that order, is the one reported.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The account has no launch template named `ikigenba.dev`.

Postconditions:

- Nothing has changed. The space's instance was not touched.

## A developer moves a space with a release that does not build

The build is the first step that does anything, so a failed build leaves
the space where it was: the current host has not been asked for its final
backup, and it keeps running. It fails as `devctl build` fails at that
commit (see `S4-build.md`). A release that is not a commit, and a checkout
with no `release-<N>` tag when `--release` is not given, are refused before
any AWS call with create's lines.

Command:

```
$ devctl space move sbx1
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
devctl: build dashboard: exit status 1

> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard
> cmd/dashboard/main.go:41:2: undefined: render
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr. A refusal the
build makes before compiling gives its own line from `S4-build.md` and exits
2.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance `i-0c9e94542d98846a8` is `running`, and it is the
  only instance tagged `Space=sbx1.ikigenba.dev` that is not terminated.
- At the commit the newest `release-<N>` names, `dashboard/` is an app that
  does not compile for `linux/arm64`.

Postconditions:

- Nothing has changed in the account or on the host: no ssh connection was
  made, the instance is `running` and untagged `Retired`, and no instance or
  address was made. Nothing under `dist/` has changed, and no worktree is
  left behind.

## A developer runs `space move` without a space

Command:

```
$ devctl space move
```

Output:

```
devctl: space move needs <space>

see 'devctl space --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. A `--release` with no
value gives `devctl: option '--release' requires a value`, also exit 2.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer's move cannot take the host backup

The `backup` step comes after retire and before the old host is stopped, so
when it fails the old host is still running, as retire left it, and no new
instance has been launched. The step fails when the old host's `uname -m`
names an architecture other than `x86_64` or `aarch64`, when the release's
opsctl does not compile for it, or when copying, running, or removing the
uploaded binary fails; here the release's `host backup` itself exits 1, and
what it wrote to its standard error is quoted. The uploaded binary is
removed from the host even so. A failure to remove it fails the step too.

Command:

```
$ devctl space move staging
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
retire: ok (opsctl retire)
devctl: backup: ssh ec2-user@18.220.10.5 sudo systemd-run --quiet --wait --pipe --collect /tmp/tmp.Ab12Cd34Ef host backup: exit status 1

> opsctl: host backup: upload failed
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr. The
temporary file's name is the one the old host's `mktemp` gave. An
architecture devctl does not know gives the single line `devctl: backup:
host architecture 'riscv64' is neither x86_64 nor aarch64` after `retire`
and compiles nothing.

Preconditions:

- As for moving a running space: the space's instance
  `i-0a1b2c3d4e5f60718` is `running` with the Elastic IP `18.220.10.5`, and
  `uname -m` there prints `x86_64`.
- The release's `host backup` on the old host fails, writing `opsctl: host
  backup: upload failed` to its standard error.

Postconditions:

- `sudo systemd-run --quiet --wait --pipe --collect opsctl retire` has been
  run on the old host over ssh and exited 0: the host is retired as opsctl
  leaves it, and its backups are in the bucket.
- The release's opsctl was compiled for `linux/amd64`, copied to
  `/tmp/tmp.Ab12Cd34Ef` on the old host, and run there as root with `host
  backup`; then the file was removed from the host.
- `i-0a1b2c3d4e5f60718` is still `running`; it was not stopped, and neither
  it nor its Elastic IP is tagged `Retired`. No instance was launched and no
  address allocated; the records, the secrets, and the role are untouched.
- Nothing undoes the retire. Running `space move staging` again, once the
  cause is fixed, runs retire again, which is safe, and goes on from there.

## A developer's move fails part-way

A move stops at the first step that fails, as create and destroy do, and
undoes nothing: what each completed step did holds. Here the old host has
taken its final backup and been stopped and retired, and no new instance
could be launched. The space's records still point at the retired
instance's Elastic IP, so the space answers nothing until it is brought
back.

Command:

```
$ devctl space move sbx1
```

Output:

```
account: ok (ikigenba.dev, us-east-2, 295229566359)
domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)
build: ok (release-87, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
retire: ok (opsctl retire)
backup: ok (opsctl host backup, amd64)
stop: ok (i-0c9e94542d98846a8 stopped)
tag: ok (i-0c9e94542d98846a8 and elastic ip 18.118.7.42 Retired=2026-11-08T18:30:00Z)
devctl: ec2 RunInstances: InsufficientInstanceCapacity
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr. No last
line naming the domain, the instance and the address is written.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance `i-0c9e94542d98846a8` is `running` with the Elastic
  IP `18.118.7.42`, and it is the only instance tagged
  `Space=sbx1.ikigenba.dev` that is not terminated; `opsctl` is installed on
  it, and `uname -m` there prints `x86_64`.
- The local repository's newest `release-<N>` tag is `release-87`, at
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, where the suite builds.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- EC2 has no capacity for the launch template's instance type.

Postconditions:

- `sudo systemd-run --quiet --wait --pipe --collect opsctl retire` and then
  the release's `host backup` have been run on the old host over ssh and
  exited 0, so its final backups are in the bucket under `sbx1/`, and the
  uploaded binary was removed from the host.
- `i-0c9e94542d98846a8` is `stopped` and tagged
  `Retired=2026-11-08T18:30:00Z` beside its `Domain` and `Space` tags; its
  Elastic IP is still associated with it and tagged the same. No instance
  was launched and no address allocated.
- The records `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev`, the apex record
  if the space held it, the secrets, the backups, and the role are untouched.
- The old instance is intact: stopped, with its volume, its data as retire
  left it, and its Elastic IP. There is no resume and nothing undoes the
  move; the way back is by hand: start `i-0c9e94542d98846a8`, remove the
  `Retired` tag from it and from its Elastic IP, and point the records
  `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev` at `18.118.7.42` if a later
  failure had already moved them. If the space held the apex, the root's
  record still points at `18.118.7.42`, which now holds nothing, so re-point
  or clear the apex with `devctl apex set` or `devctl apex clear` too; a
  destroy before that would leave the root record pointing at a released
  address. Then the space is its old instance again.
- Until then the label has no active instance, so `space list` shows no
  line for it, and `space move sbx1` and `space status sbx1` say `devctl: no
  space at 'sbx1.ikigenba.dev'`, as "A developer moves a space that does not
  exist" shows. `space create sbx1` is refused for the retired
  instance. `space destroy sbx1` removes the retired instance and its
  address with the rest; with no active instance its first line is
  `retire: ok (already gone)`, and no ssh connection is made.

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

## A developer destroys a space

A developer wants everything a space owned gone, so nothing lingers and
nothing costs money. Each line of output is one step. Two things a space
owned are kept unless the developer says otherwise: its secrets, because
they are the developer's values and cost nothing, and its backups, because
they are the point of a rebuild. Before the instance goes, devctl has the
host take its final backup with
`sudo systemd-run --quiet --wait --pipe --collect opsctl retire` over ssh,
which stops every service, lets litestream ship what it holds, and writes
the service and host backups one last time; the timers only copy on their
own schedule, and this is what makes a destroy lose nothing. A zero exit
from retire is the whole of the step's success; what retire does on the
host, and what it backs up, is opsctl's.

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

- `sudo systemd-run --quiet --wait --pipe --collect opsctl retire` has been
  run over ssh and exited 0, so `staging/crm/`, `staging/dashboard/`, and
  `staging/host/` in the bucket each hold a new object from that run, and
  `crm`'s database replica is complete to its last committed transaction.
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
  retire's own objects, `deploy/`, `snapshots/`, and `seed/` included, are
  deleted, so no later seed can take this space's snapshots. The bucket's
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

## A developer destroys a space that has a retired instance

A space that has been moved still has the instance it was moved from,
stopped and retired, with its Elastic IP. Destroy removes the label's
every instance and every address, the retired ones with the active one, so
nothing the space ever had lingers. The final backup is the active host's
alone: `retire` runs on it and never on a retired instance, which already
took its final backup before the move left it. The `instance` and
`address` lines name the active instance and its address first, then each
retired one.

Command:

```
$ devctl space destroy sbx1
```

Output:

```
retire: ok (opsctl retire)
instance: ok (i-0e5d4c3b2a1f09876 terminated, i-0c9e94542d98846a8 terminated)
address: ok (elastic ip 3.141.27.86 released, elastic ip 18.118.7.42 released)
records: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)
secrets: ok (kept)
backups: ok (kept)
role: ok (sbx1.ikigenba.dev deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- `space move sbx1` has run to the end: the active instance
  `i-0e5d4c3b2a1f09876` is `running` with the Elastic IP `3.141.27.86`, and
  `i-0c9e94542d98846a8`, the instance it moved from, is `stopped` and
  tagged `Retired=2026-11-08T18:30:00Z`, its Elastic IP `18.118.7.42` still
  associated with it and tagged the same. The space does not hold the apex.
- The developer's ssh configuration can reach the active instance as
  `ec2-user`.

Postconditions:

- `sudo systemd-run --quiet --wait --pipe --collect opsctl retire` has been
  run over ssh on `i-0e5d4c3b2a1f09876` and exited 0. No ssh connection was
  made to the retired instance, and it was not started.
- Both instances are terminated, and both Elastic IPs are disassociated and
  released. Nothing tagged `Space=sbx1.ikigenba.dev` remains.
- Otherwise as for a plain destroy. With `--no-backup` the `retire` line
  says `skipped (--no-backup)` and the rest is the same.
- Had the label held only the retired instance, as a move that failed after
  its `tag` step leaves it, the `retire` line would say `ok (already gone)`,
  no ssh connection would be made, and the `instance` and `address` lines
  would name only `i-0c9e94542d98846a8` and `18.118.7.42`.

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
devctl: retire: ssh ec2-user@18.220.10.5 sudo systemd-run --quiet --wait --pipe --collect opsctl retire: exit status 1

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
was stopped is renewed. It is
`sudo systemd-run --quiet --wait --pipe --collect certbot renew`, never
forced; certbot decides. The host's own renewal timer is persistent and
would run the same check at boot; start runs it in the foreground so the
developer sees the answer now rather than in a journal. The last line is the
domain and the address.

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
- `sudo systemd-run --quiet --wait --pipe --collect certbot renew` has been
  run on the host over ssh and exited 0.

## A developer asks what a space is running

The answer comes from the host, never from a record kept elsewhere: over ssh,
`opsctl status` lists the installed apps, says which release each runs,
reads the state of each app's service unit and of its socket unit, and reads
the journal mode of the database each app declares. `space status` copies that
output byte for byte. One line per app, in name order: the app, the short sha
of the commit it runs, the release's label or `-` when it has none, the
service state, the socket state, and the database journal mode (`-` for an app
that declares no database). What the commit and label columns hold is
opsctl's. A service that is `inactive` behind an
`active` socket is idle, not down: the next request starts it. A mode other
than `wal` means that database is no longer reaching S3.

Command:

```
$ devctl space status sbx1
```

Output:

```
crm 4b22285 release-87 active active wal
dashboard 4b22285 release-87 active active -
gmail 4b22285 release-87 failed active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space's instance exists and is `running`; `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- Three apps are installed on the host, and every socket is listening. The
  host runs the release `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, labelled
  `release-87`; deployed by its sha, the third column would be `-`.

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
crm 4b22285 release-87 inactive disabled wal
dashboard 4b22285 release-87 active active -
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

## A developer asks about a space after it has moved

After a move the space is its new instance. `space list` shows one line for
it, the active instance's state and the new address, and never a line for
the retired instance it left; the space still holds the apex if it held it
before, because the move pointed the root's record at the new address.
`space status`, `stop`, `start`, `restart`, `disable`, `enable`, and `logs`
act on the new instance. The new host presents the host key the old one
did, so the developer's ssh reaches the new address with no complaint about
a changed host key.

Command:

```
$ devctl space list
```

Output:

```
sbx1.ikigenba.dev running 3.141.27.86 apex
```

Exits 0. The line is on stdout; stderr is empty.

Command:

```
$ devctl space status sbx1
```

Output:

```
auth 4b22285 release-87 active active wal
dummy 4b22285 release-87 active active -
events 4b22285 release-87 active active wal
mcp 4b22285 release-87 active active -
repos 4b22285 release-87 active active wal
scripts 4b22285 release-87 active active wal
sites 4b22285 release-87 active active wal
telemetry 4b22285 release-87 active active wal
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- `space move sbx1` has run to the end: the active instance
  `i-0e5d4c3b2a1f09876` is `running` with the Elastic IP `3.141.27.86` and
  runs `release-87`, and `i-0c9e94542d98846a8`, the instance it moved from,
  is `stopped` and tagged `Retired=2026-11-08T18:30:00Z`, its Elastic IP
  `18.118.7.42` still associated with it. The zone's `A` records
  `sbx1.ikigenba.dev`, `*.sbx1.ikigenba.dev` and `ikigenba.dev` point at
  `3.141.27.86`. No other space exists.
- Before the move, the developer's ssh had reached the old host as
  `ec2-user`, and the developer's ssh configuration can reach the new one.

Postconditions:

- Nothing has changed. The retired instance was not started or asked.
- `space status` reached `3.141.27.86` over ssh, the first ordinary command
  to do so since the move, and nothing on stderr complained of a changed or
  unknown host key. The developer's `known_hosts` holds no entry for the key
  the new host had at launch.

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

Exits 1. The line is on stderr; stdout is empty. A label whose only
instances are retired has no space to act on and gets the same line; a
retired instance is never started, stopped, or asked.

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

Over ssh, `opsctl restart` restarts the app's service and reports it.
devctl relays that report as opsctl wrote it, byte
for byte, the way `space status` relays opsctl's answer: opsctl's own line
says what state the app is in, which a fixed line of devctl's could not. A
restart changes nothing on the host's disk. In particular it does not
carry a pushed secret to the app: that is a deploy of the release the space
runs, and `S3-secrets.md` says why.

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

- `sudo systemd-run --quiet --wait --pipe --collect opsctl restart crm` has
  been run on the host over ssh and exited 0, so `ikigenba-crm.service` is
  `active` under a new process. What restart does on the host is opsctl's;
  its socket kept listening throughout.
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

- `sudo systemd-run --quiet --wait --pipe --collect opsctl restart crm` has
  been run on the host over ssh and exited 0. Nothing has changed: neither
  of `crm`'s units was started or enabled, its names still answer `503`, and
  `space status` still shows `crm 4b22285 release-87 inactive disabled wal`.

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
devctl: restart: ssh ec2-user@18.118.7.42 sudo systemd-run --quiet --wait --pipe --collect opsctl restart crm: exit status 1

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
  here `crm 4b22285 release-87 failed active wal`.
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

The app stays disabled through `deploy`, `restore`, and `space restart`;
only `space enable` brings it back.

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

- `sudo systemd-run --quiet --wait --pipe --collect opsctl disable crm` has
  been run on the host over ssh and exited 0. What disable does on the host
  is opsctl's: `crm`'s socket and service are stopped and disabled, and
  `https://crm.sbx1.ikigenba.dev` answers `503`.
- `space status sbx1` shows `crm 4b22285 release-87 inactive disabled wal`.
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

- `sudo systemd-run --quiet --wait --pipe --collect opsctl enable crm` has
  been run on the host over ssh and exited 0. What enable does on the host
  is opsctl's: both of `crm`'s units are enabled and started, and
  `https://crm.sbx1.ikigenba.dev` reaches `crm` again.
- `space status sbx1` shows `crm 4b22285 release-87 active active wal`.
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
devctl: enable: ssh ec2-user@18.118.7.42 sudo systemd-run --quiet --wait --pipe --collect opsctl enable crm: exit status 1

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
  shows `crm 4b22285 release-87 failed active wal`. Nothing was rolled back.
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

- `sudo systemd-run --quiet --wait --pipe --collect opsctl disable crm` and
  `sudo systemd-run --quiet --wait --pipe --collect opsctl enable dashboard`
  have been run on the host over ssh and each exited 0.
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
devctl: disable: ssh ec2-user@18.118.7.42 sudo systemd-run --quiet --wait --pipe --collect opsctl disable auth: exit status 1

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
  checked against it. Deploying a release that does not hold `auth` remains
  the only way to take the authenticator off the space.

## A developer disables or enables an app that is not on the space

opsctl's refusal follows the error line, quoted with `> ` as a failed
restart's is.

Command:

```
$ devctl space disable sbx1 gmail
```

Output:

```
devctl: disable: ssh ec2-user@18.118.7.42 sudo systemd-run --quiet --wait --pipe --collect opsctl disable gmail: exit status 1

> stop: failed: no service 'gmail'
> opsctl: disable failed
```

Command:

```
$ devctl space enable sbx1 gmail
```

Output:

```
devctl: enable: ssh ec2-user@18.118.7.42 sudo systemd-run --quiet --wait --pipe --collect opsctl enable gmail: exit status 1

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
and the unit's name, `ikigenba-<app>.service`, is what activate wrote and
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

- Nothing has changed. `sudo systemd-run --quiet --wait --pipe --collect
  journalctl -u ikigenba-crm.service -n 100 --no-pager` was run on the host
  over ssh and its stdout was copied unchanged; more than 100 lines would
  have been cut to the newest 100, the way journalctl cuts them.

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

- Nothing has changed. `sudo systemd-run --quiet --wait --pipe --collect
  journalctl -u ikigenba-crm.service --since -1h -f --no-pager` was run on
  the host over ssh. A `--since` journalctl cannot parse is journalctl's to
  refuse: its diagnostic is relayed after `devctl: logs: ssh
  ec2-user@18.118.7.42 sudo systemd-run --quiet --wait --pipe --collect
  journalctl ...: exit status 1`, quoted with `> `, and the exit is 1.

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

Exits 1. The line is on stderr; stdout is empty. An app a later release dropped
is refused the same way: its unit went with it.

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
