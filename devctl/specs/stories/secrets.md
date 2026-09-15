# Stories — secrets

An app's secrets are one Parameter Store SecureString per app per space,
`/ikigenba/<domain>/<app>`, holding a flat JSON object whose keys are the names
the app's manifest declares. The developer's machine is the only source
of the values and devctl is the only writer. The host reads the object through
its instance role when an app is installed, and writes the values into the
app's environment file; a value pushed after that reaches the app at its next
deploy and at no other moment.

## A developer asks what `secrets` can do

The top-level usage gains the line `  secrets   push and list an app's
secrets for a space` under `Commands:`.

Command:

```
$ devctl secrets --help
```

Output:

```
Usage: devctl --account <name> secrets <subcommand> <domain> [<app>]

Push the values an app's manifest names from this machine's keyring to the
space's Parameter Store entry, or list which names a space holds. Values are
never printed.

Subcommands:
  push <domain> [<app>]   write /ikigenba/<domain>/<app> for one app, or every app
  list <domain> [<app>]   print the key names held for one app, or every app

Every subcommand needs --account. Run 'devctl secrets <subcommand> --help' for details.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer pushes one app's secrets to a space

An app has gained a secret name in its manifest and the space's object lacks
the key, so a deploy would be refused. The developer puts the value in their
keyring and pushes that one app, named by its directory.

An app is a sub-project of the checkout that has a `main` package and a
committed `etc/manifest.toml`; its name is the directory name. The manifest's
`secrets` array lists the names the app needs, and that array is all devctl
reads from it here — the port, the default flag, the `[env]` table, and the
`[database]` table an app with one declares are all the host's business:

```toml
app = "crm"
port = 3100
default = false
secrets = ["CRM_API_KEY", "CRM_API_SECRET", "CRM_ORG"]

[env]
OUTBOX_RETENTION_DAYS = "7"

[database]
engine = "sqlite"
path = "state/crm.db"
```

Each value comes from the developer's login keyring, read with
`secret-tool lookup name <NAME>`; an environment variable named `<NAME>`
overrides the keyring. A value is kept byte for byte except for trailing
newlines, must be non-empty, is never printed, and never appears on a command
line.

Command:

```
$ devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev crm
```

Output:

```
crm: ok (3 keys)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists in the account (an instance tagged `Space=<domain>`).
- `crm/etc/manifest.toml` is in the checkout.
- Every name in its `secrets` array has a value in the keyring or the
  environment.

Postconditions:

- `/ikigenba/foo.sbx.ikigenba.dev/crm` is a SecureString whose value is a
  JSON object with exactly the `secrets` names as keys and the keyring values
  as values, overwriting whatever was there.
- No other parameter has changed.

## A developer pushes every app's secrets to a space

With `<app>` omitted, every app in the checkout is pushed, in name order. An
app whose `secrets` array is empty or absent gets the object `{}`.

Command:

```
$ devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev
```

Output:

```
crm: ok (3 keys)
dashboard: ok (0 keys)
gmail: ok (2 keys)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists in the account.
- Every name in every app's `secrets` array has a value in the keyring or
  the environment.

Postconditions:

- `/ikigenba/foo.sbx.ikigenba.dev/<app>` is written for every app, as above.
  An app with no `secret` lines gets `{}`.

## A developer pushes with a value missing from the keyring

Command:

```
$ devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev
```

Output:

```
devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists in the account.
- `crm/etc/manifest.toml` lists `CRM_API_KEY` in `secrets` and neither the
  keyring nor the environment has it.

Postconditions:

- Nothing is written for any app, including the ones whose values were all
  present.

## A developer pushes an app that is not in the checkout

Command:

```
$ devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev bogus
```

Output:

```
devctl: no app 'bogus' in the checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- No sub-project `bogus` with a `main` package and `etc/manifest.toml` in the
  checkout.

Postconditions:

- Nothing has changed.

## A developer pushes to a space that does not exist

A push to a space that is not there would leave an object nothing will ever
read, so push checks. `space create` is the one path that writes the objects
before the instance exists, and it is about to launch it.

Command:

```
$ devctl --account 602773793009 secrets push gone.sbx.ikigenba.dev
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

## A developer rotates a secret

A value has to change: it leaked, it expired, the provider issued a new one.
The developer puts the new value in their keyring, or the environment, and
pushes the one app. That changes the parameter and nothing on the host: the
host read the parameter when the app was installed and wrote what it found
into `etc/env`, and the running app has that. What carries the new value to
the host is a deploy of the file the space already runs. `install` reads the
parameter again, rewrites `etc/env`, and restarts the unit, so the old value
is gone from the host from that restart on. `space restart` alone would not
do it: a restart re-reads the environment file, which only an install writes.

Command:

```
$ devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev crm
$ devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
```

Output:

```
crm: ok (3 keys)
file: ok (crm v0.1.0)
secrets: ok (3 keys)
upload: ok (-> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/deploy/crm-v0.1.0.tar.xz)
install: ok (opsctl installed crm)
```

Each command exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The space exists, its instance is `running`, and `crm` at `v0.1.0` is
  deployed on it.
- The keyring or the environment holds the new value for `CRM_API_KEY`, and
  values for every other name in `crm`'s `secrets` array.
- `crm/dist/crm-v0.1.0.tar.xz` exists, written by `build`.

Postconditions:

- `/ikigenba/foo.sbx.ikigenba.dev/crm` holds the new value under
  `CRM_API_KEY`; the other keys were written over with themselves.
- `/opt/crm/etc/env` on the host holds the new value, and
  `ikigenba-crm.service` has been restarted under it. The old value is nowhere
  on the host.
- `space status` shows `crm` at `v0.1.0`, as before: the deploy changed a
  value, not a version.
- Between the two commands the parameter and the host disagreed, and the app
  ran on the old value. A rotation that cannot tolerate that window deploys
  first and revokes the old value at the provider afterwards.

## A developer asks which secret names a space holds

One line per object under `/ikigenba/<domain>/`, in app order: the app, then
its keys sorted and comma-separated, or `-` for an empty object. Values never
appear.

Command:

```
$ devctl --account 602773793009 secrets list foo.sbx.ikigenba.dev
```

Output:

```
crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG
dashboard -
gmail GMAIL_CLIENT_ID,GMAIL_CLIENT_SECRET
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- Three objects exist under `/ikigenba/foo.sbx.ikigenba.dev/`.

Postconditions:

- Nothing has changed.

## A developer asks which secret names a space holds for one app

Command:

```
$ devctl --account 602773793009 secrets list foo.sbx.ikigenba.dev crm
```

Output:

```
crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- `/ikigenba/foo.sbx.ikigenba.dev/crm` exists.

Postconditions:

- Nothing has changed.

## A developer lists a space that holds no secrets

Command:

```
$ devctl --account 602773793009 secrets list empty.sbx.ikigenba.dev
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- No parameter exists under `/ikigenba/empty.sbx.ikigenba.dev/`.

Postconditions:

- Nothing has changed.
