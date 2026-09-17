# Stories — config

The host configuration store is one file, `/etc/ikigenba/config.json`, holding
a flat map of string keys to string values. It is the first thing a bootstrap
touches: everything else opsctl does reads its inputs from here, and every
later group declares the keys it reads. It is also the first thing backed up.

The top-level usage gains the line `  config    read and write the host
configuration store` under `Commands:`.

Keys match `^[a-z0-9_.-]+$`, so a `list` line never needs quoting and dotted
names such as `dns.zones` give later groups a namespace. Values are strings
with no newline in them, so every value fits on one line. The empty string is
a legal value and is not the same as the key being absent.

## An agent asks what `config` can do

Command:

```
$ opsctl config --help
```

```
$ opsctl config -h
```

Output:

```
Usage: opsctl config <subcommand> [arguments]

Read and write the host configuration store (/etc/ikigenba/config.json).

Subcommands:
  get KEY        print the value of KEY; exit 1 if KEY is not set
  set KEY=VALUE  set KEY to VALUE, creating or replacing it
  del KEY        remove KEY; succeeds whether or not KEY is set
  list           print every KEY=VALUE, one per line, sorted by key

Keys match ^[a-z0-9_.-]+$. Values may not contain newlines.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An agent configures a fresh host

`devctl space create` has just brought a host up and drives one `config set`
per key over ssh; `devctl space init` drives the same sets again on a live
host whenever what a key came from has changed. Nothing is printed: the answer
to "did it work" is the exit code.

These ten are every key opsctl's own groups declare, and between them they are
what `init` needs to bring the host to the state the store describes. A host
that has them all and nothing else is a configured host.

Command:

```
$ sudo opsctl config set host.name=foo.sbx.ikigenba.dev
$ sudo opsctl config set aws.region=us-east-2
$ sudo opsctl config set acme.email=ops@ikigenba.dev
$ sudo opsctl config set dns.provider=route53
$ sudo opsctl config set dns.zones=sbx.ikigenba.dev:Z02587302QXWONVKW632
$ sudo opsctl config set backup.s3_uri=s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/
$ sudo opsctl config set backup.host_files_seconds=86400
$ sudo opsctl config set backup.service_files_seconds=86400
$ sudo opsctl config set backup.service_db_seconds=86400
$ sudo opsctl config set backup.service_wal_seconds=300
```

Output:

```
```

Each exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is installed on the host and is running as root.
- `/etc/ikigenba/` may or may not exist.

Postconditions:

- `/etc/ikigenba/` exists with mode `0700` and `/etc/ikigenba/config.json`
  with mode `0600`, holding a JSON object of string values with its keys in
  sorted order, two-space indentation, and a trailing newline.
- The ten keys hold exactly the values given. Any other key is untouched.
- A reader at any moment saw a complete file: each write went to a temporary
  file in the same directory and was renamed over `config.json` under an
  exclusive lock on `/etc/ikigenba/config.lock`.

## An operator reads one value back

Command:

```
$ sudo opsctl config get dns.zones
```

Output:

```
sbx.ikigenba.dev:Z02587302QXWONVKW632
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `dns.zones` is set to that value.

Postconditions:

- Nothing has changed.

## An operator reads a value that was never set

The key being absent is an operation failure, not a usage error: the command
was well-formed and the store answered it.

Command:

```
$ sudo opsctl config get backup.s3_uri
```

Output:

```
opsctl: key not set: backup.s3_uri
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `backup.s3_uri` is not in the store, or `config.json` does not exist.

Postconditions:

- Nothing has changed. A missing `config.json` was not created.

## An operator reads the whole store

One `KEY=VALUE` line per entry, sorted by key, so the output diffs cleanly
between two hosts.

Command:

```
$ sudo opsctl config list
```

Output:

```
acme.email=ops@ikigenba.dev
dns.provider=route53
dns.zones=sbx.ikigenba.dev:Z02587302QXWONVKW632
host.name=foo.sbx.ikigenba.dev
```

Exits 0. The lines are on stdout; stderr is empty. An empty store prints
nothing and still exits 0.

Preconditions:

- The store holds those four keys.

Postconditions:

- Nothing has changed.

## An operator removes a key, twice

`del` is about the state the key is in afterwards, not about whether it did
any work, so removing a key that is not there succeeds.

Command:

```
$ sudo opsctl config del acme.email
$ sudo opsctl config del acme.email; echo "exit $?"
```

Output:

```
exit 0
```

Both exit 0. Nothing is on stdout or stderr but the shell's own `echo`.

Preconditions:

- `acme.email` is set before the first command.

Postconditions:

- `acme.email` is not in the store. Every other key is untouched, and
  `config.json` is a complete file at every moment.
- `del` against a missing `config.json` returns 0 and does not create it.

## An agent sets a value that contains an `=`

`set` splits its argument on the first `=` only, so a value may hold as many
more as it likes — an S3 URI with a query, a base64 value with padding.

Command:

```
$ sudo opsctl config set backup.s3_uri=s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/
$ sudo opsctl config set app.flags=--verbose=true
$ sudo opsctl config get app.flags
```

Output:

```
--verbose=true
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- `app.flags` holds `--verbose=true`.
- Characters a JSON encoder likes to escape — `<`, `>`, `&` — are written
  into `config.json` literally, so a value reads the same in the file as it
  does from `get`.

## An agent sets a malformed key or value

Three ways to get it wrong, all of them the agent's mistake rather than the
host's, so all of them are usage errors and none of them touches the store.

Command:

```
$ sudo opsctl config set dns.zones
```

Output:

```
opsctl: config set needs KEY=VALUE

see 'opsctl config --help' for usage
```

Command:

```
$ sudo opsctl config set DNS.Zones=ikigenba.dev
```

Output:

```
opsctl: invalid key: DNS.Zones

see 'opsctl config --help' for usage
```

Command:

```
$ sudo opsctl config set motd="line one
line two"
```

Output:

```
opsctl: invalid value: newline in value for 'motd'

see 'opsctl config --help' for usage
```

Each exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- The store is byte for byte as it was. No key was created or changed.

## An operator runs `config` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl config
```

Output:

```
opsctl: no config subcommand given

see 'opsctl config --help' for usage
```

Command:

```
$ sudo opsctl config unset dns.zones
```

Output:

```
opsctl: unknown config subcommand 'unset'

see 'opsctl config --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator works against a corrupt config file

A half-written file, a hand-edit that broke the JSON, a value that is not a
string: every one of them is an error every subcommand reports. It is never
treated as an empty store, because silently starting over is how a re-run of
a bootstrap destroys a host's real configuration.

Command:

```
$ sudo opsctl config get dns.zones
```

```
$ sudo opsctl config list
```

```
$ sudo opsctl config set dns.provider=route53
```

Output:

```
opsctl: /etc/ikigenba/config.json is corrupt
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `/etc/ikigenba/config.json` exists and is not a JSON object whose values
  are all strings.

Postconditions:

- The corrupt file is exactly as it was. `set` and `del` wrote nothing.

## Two agents write different keys at the same moment

`devctl space create` sets the ten keys a host needs, one after another, while
an operator at a terminal sets a key of their own. Neither loses the other's
work: the read-modify-write of a `set` or a `del` holds an exclusive lock for
its whole span.

Command:

```
$ sudo opsctl config set backup.service_wal_seconds=60 &
$ sudo opsctl config set app.flags=--verbose=true &
$ wait
```

Output:

```
```

Both exit 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Both keys are in the store with the values given. Neither write was lost,
  and `config.json` was a complete, parseable file at every moment.
