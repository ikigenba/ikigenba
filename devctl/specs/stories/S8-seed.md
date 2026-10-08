# Stories — golden sets and seeds

A space starts empty, and a developer often wants it full: of a known set of
data kept for the purpose, or of what another space holds. Both come from
snapshots, the one artifact that carries an app's data whole: `opsctl
snapshot` on a host writes each app's `etc/` but for `etc/env`, its `state/`,
and a consistent copy of its database to
`s3://ikigenba.dev/<label>/snapshots/<app>/<timestamp>.tar.zst`, under the
space's own prefix, and `opsctl restore <app> --from <uri>` puts one back (see
opsctl's `S8-backup.md`). A **golden set** is a named directory of snapshots,
one per app, at `s3://ikigenba.dev/golden/<set>/<app>/`, outside every space's
prefix. `golden capture` makes one from a space; `seed` gives a space the data
of a golden set or of another space. Creating a space and filling it are two
acts, so `space create` never seeds, and a space can be seeded as often as
the developer likes.

A host's role reaches only its own space's prefix, and that does not change
here. devctl does the copying with the developer's own credentials, which
reach the whole bucket: into a golden set for a capture, and into the target
space's own `seed/` prefix for a seed, from which that host restores. No
secret is ever in a snapshot or a golden set. A seeded app's `etc/env` is
written from the target space's own secrets, as activate writes it, and a
database's rows, token hashes among them, travel as data. Nothing here knows
about tokens: on a seeded space the developer signs in, as on any new space,
and mints a token there.

`<space>` is the space's label or its full domain, as everywhere (see
`S2-space-lifecycle.md`); no space is labelled `golden`, because that prefix
holds the golden sets. A golden set's name is one label by the same rule.
Each line of output is one step, and every bucket path is addressed
path-style, because the bucket's name has dots in it. An app is what opsctl
calls a service.

## A developer asks what `golden` can do

The top-level usage gains the line `  golden    capture a space's data as a
named golden set` under `Commands:`.

Command:

```
$ devctl golden --help
```

Output:

```
Usage: devctl golden <subcommand> [arguments]

Keep a space's data as a named golden set, one snapshot per app under
golden/<set>/ in the backup bucket, for 'devctl seed' to give to any space.
A golden set carries no secrets.

Subcommands:
  capture <space> <set>   snapshot every app on <space> and make the snapshots golden set <set>

Run 'devctl golden <subcommand> --help' for details.
```

Exits 0. The text is on stdout; stderr is empty. `devctl golden capture
--help` prints the same text, since `capture` is the one subcommand.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer captures a space's data as a golden set

`capture` has opsctl snapshot every app on the space in one run, so every
snapshot carries the same timestamp and the apps' data agree with one another
as closely as one run can make them. It then copies each snapshot into the
set, keeping its name. A set holds what one capture wrote and nothing else:
the `prune` step deletes every object under `golden/<set>/` that this capture
did not write. Here the set is new, so there is nothing to delete.

Command:

```
$ devctl golden capture sbx1 demo
```

Output:

```
snapshot: ok (opsctl snapshot, 2 apps)
copy: ok (crm -> ikigenba.dev/golden/demo/crm/2026-09-12T14:22:51Z.tar.zst)
copy: ok (dashboard -> ikigenba.dev/golden/demo/dashboard/2026-09-12T14:22:51Z.tar.zst)
prune: ok (nothing to delete)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it, with `crm` and `dashboard` installed. `crm` declares a `[database]` that
  litestream has replicated.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- Nothing is under `golden/demo/` in the bucket.

Postconditions:

- `sudo opsctl snapshot` has been run on the host over ssh and exited 0, so
  `sbx1/snapshots/crm/2026-09-12T14:22:51Z.tar.zst` and
  `sbx1/snapshots/dashboard/2026-09-12T14:22:51Z.tar.zst` are in the bucket.
  What a snapshot holds is opsctl's.
- `golden/demo/crm/2026-09-12T14:22:51Z.tar.zst` and
  `golden/demo/dashboard/2026-09-12T14:22:51Z.tar.zst` are copies of those two
  objects, and nothing else is under `golden/demo/`.
- The snapshots under `sbx1/snapshots/` stay where opsctl wrote them.
- No app on the space was stopped or changed, and no secret was read.

## A developer captures over a golden set that already exists

Capturing to a name that is taken replaces the set: the new snapshots are
copied first, and only then is the old set's every object deleted, an app the
space no longer runs included. A capture that succeeds leaves the set whole
from its one run.

Command:

```
$ devctl golden capture sbx1 demo
```

Output:

```
snapshot: ok (opsctl snapshot, 2 apps)
copy: ok (crm -> ikigenba.dev/golden/demo/crm/2026-09-14T09:10:02Z.tar.zst)
copy: ok (dashboard -> ikigenba.dev/golden/demo/dashboard/2026-09-14T09:10:02Z.tar.zst)
prune: ok (3 objects deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for the previous story, but `golden/demo/` holds the earlier capture:
  `crm/2026-09-12T14:22:51Z.tar.zst`, `dashboard/2026-09-12T14:22:51Z.tar.zst`,
  and `gmail/2026-09-12T14:22:51Z.tar.zst`. `gmail` is no longer on the space.

Postconditions:

- `golden/demo/` holds exactly `crm/2026-09-14T09:10:02Z.tar.zst` and
  `dashboard/2026-09-14T09:10:02Z.tar.zst`. The three earlier objects are gone,
  `gmail`'s with them.

## A developer captures a space whose host cannot snapshot every app

A golden set is the whole of a space or nothing. When opsctl reports any app
failed, nothing is copied and the set is left as it was. opsctl's output
follows the error line, what it wrote to its stdout and then to its stderr,
each line quoted with `> `.

Command:

```
$ devctl golden capture sbx1 demo
```

Output:

```
devctl: snapshot: ssh ec2-user@18.118.7.42 sudo opsctl snapshot: exit status 1

> crm: failed: no replica under the prefix
> dashboard: ok (s3://ikigenba.dev/sbx1/snapshots/dashboard/2026-09-12T14:22:51Z.tar.zst, 1.1 MiB)
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- As for a capture that succeeds, but nothing litestream wrote is under
  `sbx1/crm/`, so `opsctl snapshot` on the host exits non-zero.

Postconditions:

- Nothing under `golden/demo/` was written or deleted.
- The snapshots opsctl did write stay under `sbx1/snapshots/`; they are
  opsctl's.

## A developer captures a space that has no apps

A set of no snapshots would only empty an existing set, so when the host
reports no apps installed, capture refuses before it snapshots anything.

Command:

```
$ devctl golden capture sbx1 demo
```

Output:

```
devctl: 'sbx1.ikigenba.dev' has no apps to capture
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it with no apps installed.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- Nothing under `golden/demo/` was written or deleted, and nothing was
  written under `sbx1/snapshots/`.

## A developer's capture fails part-way

Command:

```
$ devctl golden capture sbx1 demo
```

Output:

```
snapshot: ok (opsctl snapshot, 2 apps)
copy: ok (crm -> ikigenba.dev/golden/demo/crm/2026-09-14T09:10:02Z.tar.zst)
devctl: s3 CopyObject: SlowDown
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- As for a capture over a golden set that already exists, and S3 throttles
  the second copy.

Postconditions:

- The steps that printed `ok` hold. The prune did not run, so the earlier
  set's objects are all still there, beside the one new copy. Until a capture
  succeeds the set is mixed: a seed from it takes `crm` from the new run and
  `dashboard` from the old.
- Running capture again takes a new snapshot run and leaves the set whole
  from it.

## A developer captures a space that does not exist, or one that is stopped

Command:

```
$ devctl golden capture gone demo
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Command:

```
$ devctl golden capture sbx2 demo
```

Output:

```
devctl: 'sbx2.ikigenba.dev' is stopped
```

Each exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance is tagged `Space=gone.ikigenba.dev`; the instance tagged
  `Space=sbx2.ikigenba.dev` is `stopped`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer runs `golden capture` without a space or a set, or with a set name that is not a label

Command:

```
$ devctl golden capture sbx1
```

Output:

```
devctl: golden capture needs <space> and <set>

see 'devctl golden --help' for usage
```

Command:

```
$ devctl golden capture sbx1 Demo_1
```

Output:

```
devctl: 'Demo_1' is not a valid label
```

Each exits 2. The text is on stderr; stdout is empty. A `<space>` that is not
a space is refused as `S2-space-lifecycle.md` shows.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made and no ssh connection was opened.

## A developer asks what `seed` can do

The top-level usage gains the line `  seed      give a space a golden set's
or another space's data` under `Commands:`.

Command:

```
$ devctl seed --help
```

Output:

```
Usage: devctl seed <space> <source>

Give <space> the data of <source>, a golden set or another space. Each app's
newest snapshot in <source> is copied under <space>'s own seed/ prefix and
put back there with 'opsctl restore <app> --from <uri>', which replaces the
app's etc/, state/ and database, and writes its etc/env from <space>'s own
secrets. An app on <space> that <source> holds no snapshot of is left alone.

<source> is a golden set when one has that name, and otherwise the space with
that label. A space's full domain always names the space.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer seeds a space from a golden set

The `source` step says which kind of source the name found and how many apps
it holds a snapshot of. Every snapshot is copied before any app is touched,
then each app is restored in name order, and once every restore has
succeeded the copies are deleted from `seed/`. An app the source holds need
not be installed on the target: opsctl restores its data, and the deploy that
brings its binary later finds it there. The app brings an older snapshot's
schema forward itself when it starts, as after any restore.

Command:

```
$ devctl seed sbx2 demo
```

Output:

```
source: ok (golden set demo, 2 apps)
copy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)
copy: ok (dashboard -> ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)
restore: ok (opsctl restore crm --from s3://ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)
restore: ok (opsctl restore dashboard --from s3://ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)
clean: ok (2 objects deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space `sbx2` exists, its instance is `running`, and `opsctl` is
  installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `golden/demo/` holds `crm/2026-09-12T14:22:51Z.tar.zst` and
  `dashboard/2026-09-12T14:22:51Z.tar.zst`.
- `/sbx2.ikigenba.dev/crm` holds every name `crm`'s manifest lists in
  `secrets`; `dashboard`'s manifest lists none.

Postconditions:

- `sudo opsctl restore crm --from s3://ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst`
  and the same for `dashboard` have been run on the host over ssh, in that
  order, and each exited 0. What a restore does on the host is opsctl's.
- Nothing is under `sbx2/seed/` in the bucket. `golden/demo/` is as it was.
- Any other app on `sbx2` has not changed.
- No secret was read or written by devctl, and no other space was touched.

## A developer seeds a space from another space

With a space as the source, the newest snapshot of each app under that
space's `snapshots/` prefix is taken, app by app; seed takes no snapshot of
its own. The source is read from the bucket alone, so it need not be running,
or exist at all once destroyed with its backups kept. A developer who wants a
space's data as it is now captures it to a golden set first. The source may
be the target itself: its newest snapshots are then put back.

Command:

```
$ devctl seed sbx2 sbx1
```

```
$ devctl seed sbx2 sbx1.ikigenba.dev
```

Output:

```
source: ok (space sbx1.ikigenba.dev, 2 apps)
copy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-14T09:10:02Z.tar.zst)
copy: ok (dashboard -> ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)
restore: ok (opsctl restore crm --from s3://ikigenba.dev/sbx2/seed/crm/2026-09-14T09:10:02Z.tar.zst)
restore: ok (opsctl restore dashboard --from s3://ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)
clean: ok (2 objects deleted)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for seeding from a golden set, but no golden set is named `sbx1`.
- `sbx1/snapshots/crm/` holds `2026-09-12T14:22:51Z.tar.zst` and
  `2026-09-14T09:10:02Z.tar.zst`; `sbx1/snapshots/dashboard/` holds only
  `2026-09-12T14:22:51Z.tar.zst`.

Postconditions:

- `crm` on `sbx2` is its `2026-09-14T09:10:02Z` snapshot, and `dashboard` its
  `2026-09-12T14:22:51Z` one.
- Nothing under `sbx1/` in the bucket was written or deleted, and no ssh
  connection was opened to `sbx1`'s host.
- Otherwise as for seeding from a golden set.

## A developer seeds from a name that is both a golden set and a space

A bare name is looked up as a golden set first, and the `source` line says
what it found. The full domain is how to name the space instead.

Command:

```
$ devctl seed sbx2 demo
```

Output:

Only the first line is shown; the rest is as for seeding from a golden set.

```
source: ok (golden set demo, 2 apps)
```

Command:

```
$ devctl seed sbx2 demo.ikigenba.dev
```

Output:

Only the first line is shown; the rest is as for seeding from another space.

```
source: ok (space demo.ikigenba.dev, 2 apps)
```

Each exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for seeding from a golden set, and both `golden/demo/` and
  `demo/snapshots/` hold a snapshot of `crm` and of `dashboard`.

Postconditions:

- The first restored `sbx2` from the golden set and read nothing under
  `demo/`; the second from the space and read nothing under `golden/`.

## A developer seeds from a source that does not exist

A bare name with no golden set and no snapshots under the space of that
label, or a full domain with none under its prefix, is refused before
anything is copied.

Command:

```
$ devctl seed sbx2 gone
```

Output:

```
devctl: no golden set or space snapshots for 'gone'
```

Exits 1. The line is on stderr, naming the source as typed; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space `sbx2` exists and its instance is `running`.
- Nothing is under `golden/gone/` or `gone/snapshots/` in the bucket.

Postconditions:

- Nothing has changed. No object was copied and no ssh connection was
  opened.

## A developer seeds a space that does not exist, or one that is stopped

The target is checked before the source is looked up.

Command:

```
$ devctl seed gone demo
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Command:

```
$ devctl seed sbx2 demo
```

Output:

```
devctl: 'sbx2.ikigenba.dev' is stopped
```

Each exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance is tagged `Space=gone.ikigenba.dev`; the instance tagged
  `Space=sbx2.ikigenba.dev` is `stopped`.

Postconditions:

- Nothing has changed. No object was copied and no ssh connection was
  opened.

## A developer's seed fails on the host

A restore that fails stops the seed: the apps already restored stay
restored, the rest are not touched, and the copies stay under `seed/`.
opsctl's output follows the error line, each line quoted with `> `. Running
seed again copies and restores every app from the start.

Command:

```
$ devctl seed sbx2 demo
```

Output:

```
source: ok (golden set demo, 3 apps)
copy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)
copy: ok (dashboard -> ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)
copy: ok (gmail -> ikigenba.dev/sbx2/seed/gmail/2026-09-12T14:22:51Z.tar.zst)
restore: ok (opsctl restore crm --from s3://ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)
restore: ok (opsctl restore dashboard --from s3://ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)
devctl: restore: ssh ec2-user@18.224.31.9 sudo opsctl restore gmail --from s3://ikigenba.dev/sbx2/seed/gmail/2026-09-12T14:22:51Z.tar.zst: exit status 1

> source: ok (s3://ikigenba.dev/sbx2/seed/gmail/2026-09-12T14:22:51Z.tar.zst, 6.1 MiB)
> secrets: failed: gmail: no value for 'GMAIL_CLIENT_SECRET' in /sbx2.ikigenba.dev/gmail
> opsctl: restore gmail failed at secrets
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- As for seeding from a golden set, and `sbx2`'s instance is at
  `18.224.31.9`. `golden/demo/` also holds
  `gmail/2026-09-12T14:22:51Z.tar.zst`.
- `/sbx2.ikigenba.dev/gmail` does not hold `GMAIL_CLIENT_SECRET`.

Postconditions:

- `crm` and `dashboard` on `sbx2` are restored from the set. `gmail` is
  whatever opsctl left; here, untouched.
- The three copies are still under `sbx2/seed/`; the clean step did not run.
- Pushing the secret (`devctl secrets push sbx2 gmail`) and running seed again
  completes it.

## A developer's seed fails part-way

A copy that fails stops the seed before any app is touched. A failure at the
`clean` step, after every restore has succeeded, reports the same way: every
`restore` line holds, the copies not yet deleted stay under `seed/`, and seed
exits 1.

Command:

```
$ devctl seed sbx2 demo
```

Output:

```
source: ok (golden set demo, 2 apps)
copy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)
devctl: s3 CopyObject: SlowDown
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- As for seeding from a golden set, and S3 throttles the second copy.

Postconditions:

- The steps that printed `ok` hold: `crm`'s copy is under `sbx2/seed/`. No
  ssh connection was opened, and no app on `sbx2` has changed.
- Running seed again copies and restores every app from the start.

## A developer runs `seed` without a space or a source

Command:

```
$ devctl seed sbx2
```

Output:

```
devctl: seed needs <space> and <source>

see 'devctl seed --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. A `<space>` that is not a
space is refused as `S2-space-lifecycle.md` shows. Run inside the checkout,
whose root file names the suffix, a `<source>` that is neither one label nor
one label under `ikigenba.dev`, `crm.sbx1` say, gives
`devctl: 'crm.sbx1' is not a golden set or a space`, exit 2, before any AWS
call; `golden.ikigenba.dev` is refused as a `<space>` is.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made and no ssh connection was opened.
