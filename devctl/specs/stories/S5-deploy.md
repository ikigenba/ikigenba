# Stories — deploy

Deploy puts one built file on one space. The file is what `build` wrote,
`<app>/dist/<app>-<tag>.tar.xz`, where the tag is `v` followed by a semver
version; devctl uploads it to the space's own `deploy/` prefix in the bucket
and has `opsctl` install it from there. The bucket is the one named after the
root, `ikigenba.dev`, and the space's prefix is its label: the file lands at
`s3://ikigenba.dev/sbx1/deploy/<file>`. The bucket's name has dots in it, so
devctl addresses it path-style, as opsctl on the host does. The file never
travels over the ssh connection — the host fetches it with its own role,
which can reach that prefix and no other space's. Promotion is deploying the
same file to a different space. Any file build wrote can go to any space: a
prerelease built on a branch deploys the same way as a release, and no space
restricts what it accepts. What each space is running is read from the host
with `space status`, never recorded anywhere else. `<space>` is the space's
label or its full domain, as everywhere (see `S2-space-lifecycle.md`).

`remove` is deploy's inverse: it has `opsctl` take one app off one space,
stopping short of the app's data, so a later deploy of the app lands over
what it left. Nothing in the account changes: the secrets stay where `secrets
push` put them, and the file stays under `deploy/`.

## A developer asks what `deploy` can do

The top-level usage gains the line `  deploy    put a built app file on a
space` under `Commands:`.

Command:

```
$ devctl deploy --help
```

Output:

```
Usage: devctl deploy <space> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
space's deploy/ prefix in the bucket and have opsctl on the space install it
from there. The app and tag (v<semver>) are read from the file name.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer deploys an app they just built

The app and tag come from the file name: the tag is the `v<semver>` the name
ends in before `.tar.xz`, and the app is everything before the `-` that
precedes it. Before anything is uploaded, the `secrets` array in the
`etc/manifest.toml` inside the file is compared with the space's secrets
object for that app. Each line of output is one step.

Command:

```
$ devctl deploy sbx1 crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
secrets: ok (3 keys)
upload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.1.0.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it (`space create` did that).
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm/dist/crm-v0.1.0.tar.xz` exists, written by `build`.
- `/sbx1.ikigenba.dev/crm` holds every name the file's manifest lists in
  `secrets`.
- `crm` is not disabled on the space.

Postconditions:

- `ikigenba.dev/sbx1/deploy/crm-v0.1.0.tar.xz` holds the file, and
  `sudo opsctl install s3://ikigenba.dev/sbx1/deploy/crm-v0.1.0.tar.xz`
  has been run over ssh and exited 0, so `crm.sbx1.ikigenba.dev` answers
  from the new binary. `crm`'s `state/` directory is untouched. What install
  does on the host is opsctl's; devctl runs it and reports its exit.
- No other app on the space has changed.
- `space status sbx1` shows `crm` at `v0.1.0`.

## A developer promotes a tested release

The same file, built once at the tagged commit and already deployed to a
sandbox space, against the space they are promoting to.

Command:

```
$ devctl deploy staging crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
secrets: ok (3 keys)
upload: ok (-> ikigenba.dev/staging/deploy/crm-v0.1.0.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space `staging.ikigenba.dev` exists, its instance is `running`, and
  `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm/dist/crm-v0.1.0.tar.xz` exists, written by `build` at the commit tagged
  `v0.1.0`.
- `/staging.ikigenba.dev/crm` holds every name the file's manifest lists in
  `secrets`.
- `crm` is not disabled on the space.

Postconditions:

- `ikigenba.dev/staging/deploy/crm-v0.1.0.tar.xz` holds the file and
  `sudo opsctl install s3://ikigenba.dev/staging/deploy/crm-v0.1.0.tar.xz`
  has exited 0, so `crm.staging.ikigenba.dev` answers from the new binary.
  `crm`'s `state/` is untouched. If `staging` holds the apex and `crm` is
  its apex app, `ikigenba.dev` answers from it too (see `S7-apex.md`).
- `space status staging` shows `crm` at `v0.1.0`.

## A developer deploys a prerelease to a sandbox

A file built at a prerelease tag on a branch. The tag is carried verbatim
through the file name, the object key, and the version the host reports.

Command:

```
$ devctl deploy sbx1 crm/dist/crm-v0.2.0-rc.1.tar.xz
```

Output:

```
file: ok (crm v0.2.0-rc.1)
secrets: ok (3 keys)
upload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm/dist/crm-v0.2.0-rc.1.tar.xz` exists, written by `build` at the commit
  tagged `v0.2.0-rc.1`.
- `/sbx1.ikigenba.dev/crm` holds every name the file's manifest lists in
  `secrets`.
- `crm` is not disabled on the space.

Postconditions:

- `ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz` holds the file and
  `opsctl install` of it has exited 0, so `crm.sbx1.ikigenba.dev` answers
  from the new binary. `crm`'s `state/` is untouched.
- `space status sbx1` shows `crm` at `v0.2.0-rc.1`.

## A developer deploys to a space where the app is disabled

The app was taken offline with `space disable`, and a deploy does not undo
that: opsctl puts the new release in place and starts nothing, and the app
stays disabled until `space enable`. devctl's lines are those of any deploy;
the install step reports opsctl's exit, and opsctl succeeded.

Command:

```
$ devctl deploy sbx1 crm/dist/crm-v0.2.0-rc.1.tar.xz
```

Output:

```
file: ok (crm v0.2.0-rc.1)
secrets: ok (3 keys)
upload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the space and disabled.
- `crm/dist/crm-v0.2.0-rc.1.tar.xz` exists, written by `build`, and
  `/sbx1.ikigenba.dev/crm` holds every name its manifest lists in `secrets`.

Postconditions:

- `ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz` holds the file and
  `opsctl install` of it has exited 0. `crm`'s `state/` is untouched.
- `crm` is still disabled: nothing of it was started, its names still answer
  `503`, and `space status sbx1` shows `crm v0.2.0-rc.1 inactive disabled
  wal`. Whether the release starts is known once `space enable sbx1 crm`
  starts it.

## A developer deploys while the space lacks a secret the app declares

Keys the object has that the file's manifest no longer names are left
alone; only missing keys refuse the deploy.

Command:

```
$ devctl deploy sbx1 crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
devctl: crm: secrets missing CRM_ORG,CRM_WEBHOOK_SECRET

run 'devctl secrets push sbx1 crm'
```

Exits 2. The `ok` line is on stdout; the rest is on stderr.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists.
- The file exists and its manifest lists `CRM_ORG` and `CRM_WEBHOOK_SECRET`
  in `secrets`; `/sbx1.ikigenba.dev/crm` has neither key.

Postconditions:

- Nothing has changed. Nothing was uploaded.

## A developer deploys a file that does not exist

Command:

```
$ devctl deploy sbx1 crm/dist/crm-v0.2.0.tar.xz
```

Output:

```
devctl: no such file 'crm/dist/crm-v0.2.0.tar.xz'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- No file at `crm/dist/crm-v0.2.0.tar.xz`.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer deploys a file that build did not write

The file name must be `<app>-v<semver>.tar.xz` and the file must hold
`bin/<app>` and `etc/manifest.toml`. A name whose tag is not a semver
version, `crm-latest.tar.xz` say, fails the same way.

Command:

```
$ devctl deploy sbx1 notes.tar.xz
```

Output:

```
devctl: 'notes.tar.xz' is not a file build wrote: name is not <app>-v<semver>.tar.xz
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `notes.tar.xz` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer runs `deploy` without a file

Command:

```
$ devctl deploy sbx1
```

Output:

```
devctl: deploy needs <space> and <file>

see 'devctl deploy --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer deploys to a space that does not exist

Command:

```
$ devctl deploy gone crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
devctl: no space at 'gone.ikigenba.dev'
```

Exits 1. The `ok` line is on stdout; the last line is on stderr.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- `crm/dist/crm-v0.1.0.tar.xz` exists.
- No instance is tagged `Space=gone.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer's deploy fails on the host

`opsctl`'s output follows the error line — what it wrote to its stdout, then
what it wrote to its stderr — each line quoted with `> ` so it is plainly the
other program's and not devctl's.

Command:

```
$ devctl deploy sbx1 gmail/dist/gmail-v0.1.0.tar.xz
```

Output:

```
file: ok (gmail v0.1.0)
secrets: ok (2 keys)
upload: ok (-> ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz)
devctl: install: ssh ec2-user@18.118.7.42 sudo opsctl install s3://ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz: exit status 1

> fetch: ok (gmail-v0.1.0.tar.xz, 6.1 MiB)
> file: ok (gmail)
> secrets: ok (2 keys)
> unpack: ok (/opt/gmail)
> unit: ok (ikigenba-gmail.socket, ikigenba-gmail.service)
> nginx: ok (gmail.sbx1.ikigenba.dev)
> litestream: ok (unchanged)
> service: failed: gmail: service failed to start
> opsctl: install failed
> 
> > ikigenba-gmail.service: Main process exited, code=exited, status=1/FAILURE
> > gmail: open /opt/gmail/etc/labels.json: no such file or directory
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `gmail/dist/gmail-v0.1.0.tar.xz` exists and `/sbx1.ikigenba.dev/gmail`
  holds every name its manifest lists in `secrets`.
- `opsctl install` of the file on the host exits non-zero.

Postconditions:

- The file is under the space's `deploy/` prefix. `gmail` is whatever
  `opsctl` left; `space status` reports it.
- No other app on the space has changed.

## A developer asks what `remove` can do

The top-level usage gains the line `  remove    take an app off a space`
under `Commands:`.

Command:

```
$ devctl remove --help
```

Output:

```
Usage: devctl remove <space> <app>

Have opsctl on the space take <app> off it: stop and remove its socket and
service, remove its binary and configuration, and stop routing its name. Its
state/ is kept on the host and its secrets are kept in the account, so a later
deploy of <app> lands over its data. What remove does on the host is opsctl's.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer takes an app off a space

The one line of output is opsctl's exit, the same shape `restore` uses. The
app is named, and the host is asked: the checkout is not read, so an app
deployed from an older checkout and since dropped from it can still be taken
off.

Command:

```
$ devctl remove sbx1 crm
```

Output:

```
remove: ok (opsctl uninstalled crm)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm` is installed on the host.

Postconditions:

- `sudo opsctl uninstall crm` has been run on the host over ssh and exited 0,
  so `crm.sbx1.ikigenba.dev` answers 404 and `crm`'s service, binary, and
  configuration are gone from the host. `/opt/crm/state/` is kept, and a
  database `crm` declared was shipped in full before replication of it
  stopped. What uninstall does on the host is opsctl's. If `crm` was the
  space's apex app, the root answers 404 from this host too until `crm` is
  deployed again; the apex itself is not moved (see `S7-apex.md`).
- `/sbx1.ikigenba.dev/crm` and every object under the space's prefix in the
  bucket are untouched, `deploy/crm-v0.1.0.tar.xz` included.
- `space status sbx1` shows `crm - - - -`. Deploying
  `crm/dist/crm-v0.1.0.tar.xz` again puts `crm` back over its data.
- No other app on the space has changed.

## A developer removes an app that is not on the space

`opsctl`'s refusal follows the error line, quoted with `> `, as a failed
deploy's is.

Command:

```
$ devctl remove sbx1 gmail
```

Output:

```
devctl: remove: ssh ec2-user@18.118.7.42 sudo opsctl uninstall gmail: exit status 1

> stop: failed: no service 'gmail'
> opsctl: uninstall failed
```

Exits 1. The text is on stderr; stdout is empty. An app the host holds data
for but never installed is refused the same way, with
`> stop: failed: gmail is not installed` and the same last line.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- Nothing under `/opt/gmail/` on the host.

Postconditions:

- Nothing has changed.

## A developer removes from a space that does not exist, or one that is stopped

Command:

```
$ devctl remove gone crm
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Command:

```
$ devctl remove sbx2 crm
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

## A developer runs `remove` without a space or an app

Command:

```
$ devctl remove sbx1
```

Output:

```
devctl: remove needs <space> and <app>

see 'devctl remove --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made and no ssh connection was opened.
