# Stories — secrets

An app's secrets are one Parameter Store SecureString per app per space,
`/ikigenba/<domain>/<app>`, holding a flat JSON object whose keys are the names
the app's `etc/env.list` declares. The developer's machine is the only source
of the values and devctl is the only writer; the host reads the object through
its instance role at app start.

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

Push the values an app's etc/env.list names from this machine's keyring to the
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

An app has gained a `secret <NAME>` line and the space's object lacks the key,
so a deploy would be refused. The developer puts the value in their keyring
and pushes that one app, named by its directory.

An app is a top-level directory of the checkout holding `etc/env.list`; its
name is the directory name. `etc/env.list` holds one declaration per line, and
devctl reads exactly the lines of the form `secret <NAME>`; every other line
(comments, `rotating`, `config`) is the app's own business and is ignored.

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
- `crm/etc/env.list` is in the checkout.
- Every `secret <NAME>` in it has a value in the keyring or the environment.

Postconditions:

- `/ikigenba/foo.sbx.ikigenba.dev/crm` is a SecureString whose value is a
  JSON object with exactly the `secret` names as keys and the keyring values
  as values, overwriting whatever was there.
- No other parameter has changed.

## A developer pushes every app's secrets to a space

With `<app>` omitted, every app in the checkout is pushed, in name order. An
app whose `etc/env.list` has no `secret` lines gets the object `{}`.

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
- Every `secret <NAME>` in every app's `etc/env.list` has a value in the
  keyring or the environment.

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
- `crm/etc/env.list` names `CRM_API_KEY` and neither the keyring nor the
  environment has it.

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

- No directory `bogus/etc/env.list` in the checkout.

Postconditions:

- Nothing has changed.

## A developer pushes to a space that does not exist

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
