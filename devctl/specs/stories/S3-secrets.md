# Stories — secrets

An app's secrets are one Parameter Store SecureString per app per space,
`/<space domain>/<app>`, holding a flat JSON object whose keys are the names
the app's manifest declares. The developer's machine is the only source of the
values and devctl is the only writer. The host reads the object through its
instance role when a release is activated, and writes
the values into the app's environment file; a value pushed after that reaches
the app at its next deploy and at no other moment. `<space>` is the space's
label or its full domain, as everywhere (see `S2-space-lifecycle.md`); the
parameter path always uses the full domain.

## A developer asks what `secrets` can do

The top-level usage gains the line `  secrets   push and list an app's
secrets for a space` under `Commands:`.

Command:

```
$ devctl secrets --help
```

Output:

```
Usage: devctl secrets <subcommand> <space> [<app>]

Push the values an app's manifest names from this machine's keyring to the
space's Parameter Store entry, or list which names a space holds. Values are
never printed.

Subcommands:
  push <space> [<app>]   write /<space domain>/<app> for one app, or every app
  list <space> [<app>]   print the key names held for one app, or every app

Run 'devctl secrets <subcommand> --help' for details.
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
reads from it here — the default flag, the `[env]` table, and the
`[database]` table are the host's business, not the developer's machine's:

```toml
app = "crm"
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
$ devctl secrets push sbx1 crm
```

Output:

```
crm: ok (3 keys)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists (an instance tagged `Space=sbx1.ikigenba.dev`).
- `crm/etc/manifest.toml` is in the checkout.
- Every name in its `secrets` array has a value in the keyring or the
  environment.

Postconditions:

- `/sbx1.ikigenba.dev/crm` is a SecureString whose value is a JSON object
  with exactly the `secrets` names as keys and the keyring values as values,
  overwriting whatever was there.
- No other parameter has changed.

## A developer pushes every app's secrets to a space

With `<app>` omitted, every app in the checkout is pushed, in name order. An
app whose `secrets` array is empty or absent gets the object `{}`.

Command:

```
$ devctl secrets push sbx1
```

Output:

```
crm: ok (3 keys)
dashboard: ok (0 keys)
gmail: ok (2 keys)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists.
- Every name in every app's `secrets` array has a value in the keyring or
  the environment.

Postconditions:

- `/sbx1.ikigenba.dev/<app>` is written for every app, as above. An app
  with no `secret` lines gets `{}`.

## A developer pushes with a value missing from the keyring

Command:

```
$ devctl secrets push sbx1
```

Output:

```
devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists.
- `crm/etc/manifest.toml` lists `CRM_API_KEY` in `secrets` and neither the
  keyring nor the environment has it.

Postconditions:

- Nothing is written for any app, including the ones whose values were all
  present.

## A developer pushes an app that is not in the checkout

Command:

```
$ devctl secrets push sbx1 bogus
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
$ devctl secrets push gone
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

## A developer rotates a secret

A value has to change: it leaked, it expired, the provider issued a new one.
The developer puts the new value in their keyring, or the environment, and
pushes the one app. That changes the parameter and nothing on the host: the
host read the parameter when the release was activated and wrote what it
found into the app's environment, and the running app has that. What carries
the new value to the host is a deploy of the release the space already runs.
Activating it again reads every app's parameter afresh, rewrites each app's
environment, and restarts the apps, so the old value is gone from the host
from that restart on. `space restart` alone would not do it: a restart
re-reads the environment the last activate wrote. The deploy's lines are
those of any deploy of a release the host already holds (see
`S5-deploy.md`); the lines from `preflight` on are opsctl's, shown here for
illustration.

Command:

```
$ devctl secrets push sbx1 auth
$ devctl deploy sbx1 r1
```

Output:

```
auth: ok (2 keys)
build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps, 2 keys)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a already present, kept)
preflight: ok (8 apps)
label: ok (r1)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (r1 (4b22285))
```

Each command exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and it runs the release `r1`,
  the commit `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, whose `auth` manifest
  lists `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in `secrets` and whose
  other apps list none; `auth` is not disabled.
- The keyring or the environment holds the new value for
  `GOOGLE_CLIENT_SECRET`, and a value for `GOOGLE_CLIENT_ID`.

Postconditions:

- `/sbx1.ikigenba.dev/auth` holds the new value under `GOOGLE_CLIENT_SECRET`;
  the other key was written over with itself.
- `auth`'s environment on the host holds the new value, and `auth` has been
  restarted under it. The old value is nowhere on the host.
- The host runs the same release as before: the deploy changed a value, not
  the release.
- Between the two commands the parameter and the host disagreed, and the app
  ran on the old value. A rotation that cannot tolerate that window deploys
  first and revokes the old value at the provider afterwards.

## A developer asks which secret names a space holds

One line per object under `/<space domain>/`, in app order: the app, then
its keys sorted and comma-separated, or `-` for an empty object. Values never
appear.

Command:

```
$ devctl secrets list sbx1
```

Output:

```
crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG
dashboard -
gmail GMAIL_CLIENT_ID,GMAIL_CLIENT_SECRET
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- Three objects exist under `/sbx1.ikigenba.dev/`.

Postconditions:

- Nothing has changed.

## A developer asks which secret names a space holds for one app

Command:

```
$ devctl secrets list sbx1 crm
```

Output:

```
crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- `/sbx1.ikigenba.dev/crm` exists.

Postconditions:

- Nothing has changed.

## A developer lists a space that holds no secrets

Command:

```
$ devctl secrets list empty
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No parameter exists under `/empty.ikigenba.dev/`.

Postconditions:

- Nothing has changed.
