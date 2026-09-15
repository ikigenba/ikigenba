# Stories — deploy

Deploy puts one built file on one space. The file is what `build` wrote,
`<app>/dist/<app>-<tag>.tar.xz`, where the tag is `v` followed by a semver
version; devctl uploads it to the space's own `deploy/` prefix in the backup
bucket and has `opsctl` install it from there. The file never travels over
the ssh connection — the host fetches it with its own role, which can reach
that prefix and no other space's. Promotion is deploying the same file to a
different space. Any file build wrote can go to any space: a prerelease built
on a branch deploys the same way as a release, and no account restricts what
it accepts. What each space is running is read from the host with `space
status`, never recorded anywhere else.

## A developer asks what `deploy` can do

The top-level usage gains the line `  deploy    put a built app file on a
space` under `Commands:`.

Command:

```
$ devctl deploy --help
```

Output:

```
Usage: devctl --account <name> deploy <domain> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
it from there. The app and tag (v<semver>) are read from the file name.
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
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
secrets: ok (3 keys)
upload: ok (-> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/crm-v0.1.0.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it (`space create` did that).
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm/dist/crm-v0.1.0.tar.xz` exists, written
  by `build`.
- `/ikigenba/foo.sbx.ikigenba.dev/crm` holds every name the file's
  manifest lists in `secrets`.

Postconditions:

- `sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/crm-v0.1.0.tar.xz` holds the file,
  and `sudo opsctl install s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/crm-v0.1.0.tar.xz`
  has been run over ssh and exited 0, so `crm.foo.sbx.ikigenba.dev` answers
  from the new binary. `crm`'s `state/` directory is untouched. What install
  does on the host is opsctl's; devctl runs it and reports its exit.
- No other app on the space has changed.
- `space status foo.sbx.ikigenba.dev` shows `crm` at `v0.1.0`.

## A developer promotes a tested release

The same file, built once at the tagged commit and already deployed to a
sandbox space, against the space they are promoting to.

Command:

```
$ devctl --account 295229566359 deploy ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
secrets: ok (3 keys)
upload: ok (-> ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.1.0.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The apex space `ikigenba.dev` exists in the durable account, its instance
  is `running`, and `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm/dist/crm-v0.1.0.tar.xz` exists, written by `build` at the commit tagged
  `v0.1.0`.
- `/ikigenba/ikigenba.dev/crm` holds every name the file's manifest
  lists in `secrets`.

Postconditions:

- `ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.1.0.tar.xz` holds the file and
  `sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.1.0.tar.xz`
  has exited 0, so `crm.ikigenba.dev` answers from the new binary. `crm`'s
  `state/` is untouched.
- `space status ikigenba.dev` shows `crm` at `v0.1.0`.

## A developer deploys a prerelease to a sandbox

A file built at a prerelease tag on a branch. The tag is carried verbatim
through the file name, the object key, and the version the host reports.

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.2.0-rc.1.tar.xz
```

Output:

```
file: ok (crm v0.2.0-rc.1)
secrets: ok (3 keys)
upload: ok (-> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/crm-v0.2.0-rc.1.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `crm/dist/crm-v0.2.0-rc.1.tar.xz` exists, written by `build` at the commit
  tagged `v0.2.0-rc.1`.
- `/ikigenba/foo.sbx.ikigenba.dev/crm` holds every name the file's manifest
  lists in `secrets`.

Postconditions:

- `sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/crm-v0.2.0-rc.1.tar.xz`
  holds the file and `opsctl install` of it has exited 0, so
  `crm.foo.sbx.ikigenba.dev` answers from the new binary. `crm`'s `state/`
  is untouched.
- `space status foo.sbx.ikigenba.dev` shows `crm` at `v0.2.0-rc.1`.

## A developer deploys while the space lacks a secret the app declares

Keys the object has that the file's manifest no longer names are left
alone; only missing keys refuse the deploy.

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
devctl: crm: secrets missing CRM_ORG,CRM_WEBHOOK_SECRET

run 'devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev crm'
```

Exits 2. The `ok` line is on stdout; the rest is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists.
- The file exists and its manifest lists `CRM_ORG` and `CRM_WEBHOOK_SECRET`
  in `secrets`; `/ikigenba/foo.sbx.ikigenba.dev/crm` has neither key.

Postconditions:

- Nothing has changed. Nothing was uploaded.

## A developer deploys a file that does not exist

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.2.0.tar.xz
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
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev notes.tar.xz
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
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev
```

Output:

```
devctl: deploy needs <domain> and <file>

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
$ devctl --account 602773793009 deploy gone.sbx.ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
devctl: no space at 'gone.sbx.ikigenba.dev'
```

Exits 1. The `ok` line is on stdout; the last line is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- `crm/dist/crm-v0.1.0.tar.xz` exists.
- No instance in the account is tagged `Space=gone.sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer's deploy fails on the host

`opsctl`'s output follows the error line, each line quoted with `> ` so it is
plainly the other program's and not devctl's.

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev gmail/dist/gmail-v0.1.0.tar.xz
```

Output:

```
file: ok (gmail v0.1.0)
secrets: ok (2 keys)
upload: ok (-> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/gmail-v0.1.0.tar.xz)
devctl: install: ssh ec2-user@3.19.79.227 sudo opsctl install s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/gmail-v0.1.0.tar.xz: exit status 1

> opsctl: gmail: service failed to start
> 
> > ikigenba-gmail.service: Main process exited, code=exited, status=1/FAILURE
> > gmail: listen tcp 127.0.0.1:3300: bind: address already in use
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `gmail/dist/gmail-v0.1.0.tar.xz` exists and
  `/ikigenba/foo.sbx.ikigenba.dev/gmail` holds every name its manifest lists in `secrets`.
- `opsctl install` of the file on the host exits non-zero.

Postconditions:

- The file is under the space's `deploy/` prefix. `gmail` is whatever
  `opsctl` left; `space status` reports it.
- No other app on the space has changed.
