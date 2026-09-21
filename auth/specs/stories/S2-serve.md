# Stories — serve

The bare binary serves. The host's service unit runs it with no arguments and
an environment file that sets `PORT` and the app's secrets and `[env]` values;
nothing else is passed. auth listens on `127.0.0.1:$PORT` only, because nginx
on the host terminates TLS and proxies to loopback. A healthy server prints
nothing, so under systemd the journal holds only trouble. The actor in these
stories is the host, whether that is systemd or a developer at a terminal
standing in for it. The environment auth reads is `PORT`, the two Google
secrets `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, and `WORKSPACE_DOMAIN`;
it opens a SQLite database at `state/auth.db`, relative to its working
directory `/opt/auth`. Starting touches no network. auth reads its
environment, opens the database, and listens; it does not contact Google.
The Google settings are read and required at startup — a missing one refuses
the start (below) — but Google itself is reached only when a human signs in
(`S3-sign-in.md`). So auth serves even while Google is unreachable, and
`/check` and `/me` keep answering from the local database (`S4-check.md`).

## The host starts auth

Command:

```
$ PORT=3001 GOOGLE_CLIENT_ID=<client-id> GOOGLE_CLIENT_SECRET=<client-secret> WORKSPACE_DOMAIN=michaelgreenly.dev auth
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/auth` exists.
- Nothing is listening on `127.0.0.1:3001`.
- `state/auth.db` exists, from an earlier start.

Postconditions:

- auth is listening on `127.0.0.1:3001` and on no other address.
- It keeps running until it is signalled.
- `state/auth.db` is the database it opened; it existed already.
- No network call to Google was made; the Google settings were read from the
  environment, not checked against Google.

## The host starts auth for the first time

The database file does not yet exist. auth creates `state/auth.db` and its
schema on this first start, then serves. This is the one difference from the
ordinary start, where the database already exists and is only opened.

Command:

```
$ PORT=3001 GOOGLE_CLIENT_ID=<client-id> GOOGLE_CLIENT_SECRET=<client-secret> WORKSPACE_DOMAIN=michaelgreenly.dev auth
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/auth` exists.
- Nothing is listening on `127.0.0.1:3001`.
- `state/auth.db` does not exist.

Postconditions:

- `state/auth.db` now exists, with its schema, created by this start.
- auth is listening on `127.0.0.1:3001` and on no other address.
- It keeps running until it is signalled.

## The host stops auth

`systemctl stop` and `systemctl restart` send `SIGTERM`. auth finishes the
requests it has accepted, closes the listener, and exits. `SIGINT` is handled
the same way.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

The server exits 0. Nothing is on stdout or stderr.

Preconditions:

- auth is running with `PORT=3001` as process `<pid>`.

Postconditions:

- Every request accepted before the signal received its full response.
- Nothing is listening on `127.0.0.1:3001`.

## The host starts auth without a port

Command:

```
$ auth
```

Output:

```
auth: PORT is not set
```

Exits 2. The line is on stderr; stdout is empty. A value that is not an
integer from 1 to 65535 fails the same way with `auth: PORT is 'abc', not a
port number`.

Preconditions:

- `bin/auth` exists.
- `PORT` is unset or empty in the environment.

Postconditions:

- Nothing has changed. Nothing is listening.

## The host starts auth without a Google setting

`PORT` is set, but a required Google setting is not. auth reports the missing
name and refuses to start, in the same shape as a missing `PORT`.

Command:

```
$ PORT=3001 auth
```

Output:

```
auth: GOOGLE_CLIENT_ID is not set
```

Exits 2. The line is on stderr; stdout is empty. `GOOGLE_CLIENT_SECRET` and
`WORKSPACE_DOMAIN` are each required the same way and fail identically with
their own name.

Preconditions:

- `bin/auth` exists.
- `PORT` is set to a valid port.
- `GOOGLE_CLIENT_ID` is unset or empty in the environment.

Postconditions:

- Nothing has changed. Nothing is listening.

## The host starts auth on a port already in use

The line is the listener's own complaint, in the shape opsctl's install story
relays for a service that will not start.

Command:

```
$ PORT=3001 GOOGLE_CLIENT_ID=<client-id> GOOGLE_CLIENT_SECRET=<client-secret> WORKSPACE_DOMAIN=michaelgreenly.dev auth
```

Output:

```
auth: listen tcp 127.0.0.1:3001: bind: address already in use
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `bin/auth` exists.
- `state/auth.db` exists and can be opened.
- Another process is listening on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed. The other process still holds the port.

## The host starts auth with a database it cannot open

`state/auth.db` exists but auth cannot open it — the file is not writable by
the service user, or its contents are not a valid SQLite database. auth writes
a diagnostic naming the database problem and refuses to start. A missing file
is not this error: an absent `state/auth.db` is created on first start (see
`The host starts auth for the first time`).

Command:

```
$ PORT=3001 GOOGLE_CLIENT_ID=<client-id> GOOGLE_CLIENT_SECRET=<client-secret> WORKSPACE_DOMAIN=michaelgreenly.dev auth
```

Output:

```
auth: cannot open database state/auth.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying
open failure.

Preconditions:

- `bin/auth` exists.
- `PORT` and the Google settings are set.
- `state/auth.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. Nothing is listening.
