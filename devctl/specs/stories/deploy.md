# Stories — deploy

Deploy puts one built file on one space. The file is what `build` wrote,
`dist/<app>-<version>.tar.xz`; devctl copies it to the host's `/tmp/` and has
`opsctl` install it from there. Promotion is deploying the same file to a
different space. What each space is running is read from the host with
`space status`, never recorded anywhere else.

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

Copy <file>, a dist/<app>-<version>.tar.xz written by build, to /tmp/ on the
space at <domain> and have opsctl install it. The app and version are read
from the file name.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer deploys an app they just built

The app and version come from the file name. Before anything is copied, the
`etc/env.list` inside the file is compared with the space's secrets object
for that app. Each line of output is one step.

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz
```

Output:

```
file: ok (crm 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
secrets: ok (3 keys)
copy: ok (-> ec2-user@3.19.79.227:/tmp/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it (`space create` did that).
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` exists, written
  by `build`.
- `/ikigenba/foo.sbx.ikigenba.dev/crm` holds every name the file's
  `etc/env.list` declares.

Postconditions:

- `/tmp/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` is on the host
  and `sudo opsctl install /tmp/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`
  has been run over ssh and exited 0, so `crm.foo.sbx.ikigenba.dev` answers
  from the new binary. `crm`'s `state/` directory is untouched. What install
  does on the host is opsctl's; devctl runs it and reports its exit.
- No other app on the space has changed.
- `space status foo.sbx.ikigenba.dev` shows `crm` at
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`.

## A developer promotes a tested release

The same file, built once at the tagged commit and already deployed to a
sandbox space, against the space they are promoting to.

Command:

```
$ devctl --account 295229566359 deploy ikigenba.dev dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
secrets: ok (3 keys)
copy: ok (-> ec2-user@3.18.9.77:/tmp/crm-v0.1.0.tar.xz)
install: ok (opsctl installed crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The apex space `ikigenba.dev` exists in the durable account, its instance
  is `running`, and `opsctl` is installed on it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `dist/crm-v0.1.0.tar.xz` exists, written by `build` at the commit tagged
  `v0.1.0`.
- `/ikigenba/ikigenba.dev/crm` holds every name the file's `etc/env.list`
  declares.

Postconditions:

- `/tmp/crm-v0.1.0.tar.xz` is on the host and
  `sudo opsctl install /tmp/crm-v0.1.0.tar.xz` has exited 0, so
  `crm.ikigenba.dev` answers from the new binary. `crm`'s `state/` is
  untouched.
- `space status ikigenba.dev` shows `crm` at `v0.1.0`.

## A developer deploys while the space lacks a secret the app declares

Keys the object has that the file's `etc/env.list` no longer names are left
alone; only missing keys refuse the deploy.

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz
```

Output:

```
file: ok (crm 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
devctl: crm: secrets missing CRM_ORG,CRM_WEBHOOK_SECRET

run 'devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev crm'
```

Exits 2. The `ok` line is on stdout; the rest is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists.
- The file exists and its `etc/env.list` names `CRM_ORG` and
  `CRM_WEBHOOK_SECRET`; `/ikigenba/foo.sbx.ikigenba.dev/crm` has neither key.

Postconditions:

- Nothing has changed. Nothing was copied.

## A developer deploys a file that does not exist

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev dist/crm-v0.2.0.tar.xz
```

Output:

```
devctl: no such file 'dist/crm-v0.2.0.tar.xz'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- No file at `dist/crm-v0.2.0.tar.xz`.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer deploys a file that build did not write

The file name must be `<app>-<version>.tar.xz` and the file must hold
`bin/<app>` and `etc/env.list`.

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev notes.tar.xz
```

Output:

```
devctl: 'notes.tar.xz' is not a file build wrote: name is not <app>-<version>.tar.xz
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
$ devctl --account 602773793009 deploy gone.sbx.ikigenba.dev dist/crm-v0.1.0.tar.xz
```

Output:

```
file: ok (crm v0.1.0)
devctl: no space at 'gone.sbx.ikigenba.dev'
```

Exits 1. The `ok` line is on stdout; the last line is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- `dist/crm-v0.1.0.tar.xz` exists.
- No instance in the account is tagged `Space=gone.sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer's deploy fails on the host

`opsctl`'s output follows the error line.

Command:

```
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev dist/gmail-v0.1.0.tar.xz
```

Output:

```
file: ok (gmail v0.1.0)
secrets: ok (2 keys)
copy: ok (-> ec2-user@3.19.79.227:/tmp/gmail-v0.1.0.tar.xz)
devctl: install: ssh ec2-user@3.19.79.227 sudo opsctl install /tmp/gmail-v0.1.0.tar.xz: exit status 1

opsctl: gmail: service failed to start
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists and its instance is `running`; `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `dist/gmail-v0.1.0.tar.xz` exists and
  `/ikigenba/foo.sbx.ikigenba.dev/gmail` holds every name it declares.
- `opsctl install` of the file on the host exits non-zero.

Postconditions:

- The file is in `/tmp/` on the host. `gmail` is whatever `opsctl` left;
  `space status` reports it.
- No other app on the space has changed.
