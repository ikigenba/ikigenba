# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits:
auth never opens one of its own. It takes the socket the way systemd socket
activation passes it — `LISTEN_PID` names auth's own process, `LISTEN_FDS` is
`1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*`
variables from its environment once it has taken it. On a host, opsctl
publishes `ikigenba-auth.socket`, which holds the Unix socket
`/run/ikigenba/auth.sock`, beside `ikigenba-auth.service`, which runs
`/opt/auth/bin/auth` with no arguments as the `ikigenba` user, with
`/opt/auth` as its working directory and `/opt/auth/etc/env` as its
environment file; nginx proxies auth's own hostname to
`http://unix:/run/ikigenba/auth.sock:` and makes its identity subrequest for
every other app to `http://unix:/run/ikigenba/auth.sock:/check`. The service
is `Type=notify`: auth tells systemd it is ready, by sending `READY=1` to
`$NOTIFY_SOCKET`, once it is serving. A developer stands in for the host with
`systemd-socket-activate`, which passes a socket on the same terms. The actor
in these stories is the host, whether that is systemd or a developer at a
terminal standing in for it.

The environment auth reads is the two Google secrets `GOOGLE_CLIENT_ID` and
`GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, and `DRAIN_SECONDS`. The first
three are required. `DRAIN_SECONDS` is how long auth drains when stopped, a
positive whole number of seconds, and 5 when it is unset or empty. On a host,
opsctl owns that value and the service unit's stop timeout: both are
space-wide settings in opsctl's configuration, opsctl writes the drain into
every app's `etc/env` and the stop timeout (10 seconds by default, always
longer than the drain) into every service unit, and an app's manifest never
sets either. auth checks its environment first — `GOOGLE_CLIENT_ID`,
`GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, then `DRAIN_SECONDS` — then looks
for its socket, and only then opens its SQLite database at `state/auth.db`,
relative to its working directory. So a start refused as a usage error has
touched nothing, not even the database. Starting touches no network: the
Google settings are read and required at startup, but Google itself is
reached only when a human signs in (`S3-sign-in.md`), so auth serves even
while Google is unreachable, and `/check` and `/me` keep answering from the
local database (`S4-check.md`). In the laptop stories below the developer's
shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
`WORKSPACE_DOMAIN=michaelgreenly.dev` unless a story says otherwise, and
`systemd-socket-activate` passes its environment on to auth.

A healthy auth prints nothing, so under systemd the journal holds only
trouble. A diagnostic auth writes about a request names that request by its
`X-Request-Id`, as `auth: request <id>: <reason>` on stderr, so a line in the
journal can be matched to nginx's log of the same request; a request that
carries no `X-Request-Id` is named `-`. auth writes one such line for each
request it answers with a 5xx, any status from 500 through 599 — its own are a
500, when its own database fails the request, and a 502, when Google fails a
sign-in (`S3-sign-in.md`) — and nothing for any other answer: a 4xx is the
caller's to fix, not trouble.

auth serves on the terms every app of the platform serves on. The socket is
its only way in. Every app runs as the one `ikigenba` user, so any app can
reach any sibling's socket, and nginx reaches them all; nothing else on the
host can. The suite is a closed system that only we deploy services into, and
auth trusts it: `X-Request-Id`, 32 lowercase hexadecimal characters, is set by
nginx on every request it forwards to auth and on every `/check` subrequest,
overwriting whatever a client sent, and a sibling that calls auth directly
copies it from the request it is serving. auth decides identity itself, from
the session cookie or a token, and calls no sibling.

## The host starts auth

The socket keeps out every process that is not part of the suite or nginx,
which a port on loopback would not: any process on the host can connect to a
loopback port, and only the `ikigenba` user and nginx can connect to
`/run/ikigenba/auth.sock`. systemd owns the socket, so it exists, and accepts
connections into its queue, before auth starts and while it is stopped; auth's
part is to serve what arrives on it. `systemctl start` returns once auth has
reported that it is ready.

Command:

```
$ sudo systemctl start ikigenba-auth.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed auth: `/opt/auth/bin/auth` exists, and
  `ikigenba-auth.socket` and `ikigenba-auth.service` are published.
- `/opt/auth/etc/env` sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  `WORKSPACE_DOMAIN`, and `DRAIN_SECONDS`, each to a valid value.
- `ikigenba-auth.socket` is active, so `/run/ikigenba/auth.sock` exists and
  accepts connections.
- `/opt/auth/state/auth.db` exists, from an earlier start.
- `ikigenba-auth.service` is not running.

Postconditions:

- `ikigenba-auth.service` is `active`, and auth is serving on
  `/run/ikigenba/auth.sock`: a connection there, and every connection queued
  before auth started, is answered by auth.
- auth listens on no other socket and no port.
- `/opt/auth/state/auth.db` is the database it opened; it existed already.
- No network call to Google was made; the Google settings were read from the
  environment, not checked against Google.
- auth has written nothing to the journal.
- It keeps running until it is signalled.

## A developer serves auth on a laptop

A laptop has no `ikigenba-auth.socket`, so the developer lets
`systemd-socket-activate` hold a socket and pass it to auth exactly as
systemd would. A TCP socket on loopback serves a browser and `curl` alike,
and the later groups' requests go to `http://localhost:3001`, the origin
whose callback is registered on the OAuth client for development
(`S3-sign-in.md`). The three lines are `systemd-socket-activate`'s own: it
announces the socket, and it starts auth only when the first connection
arrives, which auth then answers. auth adds nothing to them. There is no
`NOTIFY_SOCKET` here, so auth reports readiness to nobody.

Command:

```
$ systemd-socket-activate -l 127.0.0.1:3001 auth
```

Output:

```
Listening on 127.0.0.1:3001 as 3.
Communication attempt on fd 3.
Execing auth (auth)
```

Does not exit. The lines are on stderr; stdout is empty. The first line
appears at once, the other two when the first connection arrives.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- The developer's shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  and `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `state/auth.db` exists in the working directory, from an earlier start.
- Nothing is listening on `127.0.0.1:3001`.

Postconditions:

- auth is serving on `127.0.0.1:3001` and on no other address, and it
  answered the connection that started it.
- `state/auth.db` is the database it opened; it existed already.
- No network call to Google was made.
- It keeps running until it is signalled.

## The host starts auth for the first time

The database file does not yet exist. auth creates the `state/` directory if
it is absent, then creates `state/auth.db` and its schema and serves. The
same start succeeds when `state/` already exists and only the database is
absent. Neither a fresh deployment nor a developer's first local run needs
the directory created beforehand. The paths are relative to auth's working
directory.

Command:

```
$ systemd-socket-activate -l 127.0.0.1:3001 auth
```

Output:

```
Listening on 127.0.0.1:3001 as 3.
Communication attempt on fd 3.
Execing auth (auth)
```

Does not exit. The lines are on stderr; stdout is empty. They are
`systemd-socket-activate`'s own; auth adds nothing to them.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- The developer's shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  and `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- Nothing is listening on `127.0.0.1:3001`.
- `state/auth.db` does not exist.
- Either `state/` is absent and auth can create it in its working directory,
  or `state/` is an existing directory in which auth can create the database.
- A connection is made to `127.0.0.1:3001`.

Postconditions:

- `state/` exists, created by auth if it was absent.
- `state/auth.db` now exists, with its schema, created by this start.
- auth is serving on `127.0.0.1:3001` and on no other address, and it
  answered the connection that started it.
- It keeps running until it is signalled.

## The host starts auth where its state directory cannot be created

A regular file named `state` occupies the path where auth needs its state
directory. auth has taken its socket, but it reports the database setup
failure and exits before it serves or reports ready. The failure is not the
caller's usage, so it exits 1.

Command:

```
$ systemd-socket-activate -l 127.0.0.1:3001 auth
```

Output:

```
Listening on 127.0.0.1:3001 as 3.
Communication attempt on fd 3.
Execing auth (auth)
auth: cannot open database state/auth.db: <reason>
```

Exits 1. The text is on stderr; stdout is empty. The first three lines are
`systemd-socket-activate`'s own; the last is auth's. `<reason>` is the
underlying directory-creation failure. The connection that started auth is
closed unanswered.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- The developer's shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  and `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- Nothing is listening on `127.0.0.1:3001`.
- `state` is an existing regular file in auth's working directory.
- A connection is made to `127.0.0.1:3001`.

Postconditions:

- The existing `state` file is unchanged.
- No database was created. auth served nothing and told systemd nothing.

## The host starts auth with a database it cannot open

`state/auth.db` exists but auth cannot open it — the file is not writable by
the service user, or its contents are not a valid SQLite database. auth
writes a diagnostic naming the database problem and exits before it serves
or reports ready. A missing file is not this error: an absent
`state/auth.db` is created on first start (see `The host starts auth for the
first time`). Under systemd the start fails, and `systemctl start` reports
it.

Command:

```
$ systemd-socket-activate -l 127.0.0.1:3001 auth
```

Output:

```
Listening on 127.0.0.1:3001 as 3.
Communication attempt on fd 3.
Execing auth (auth)
auth: cannot open database state/auth.db: <reason>
```

Exits 1. The text is on stderr; stdout is empty. The first three lines are
`systemd-socket-activate`'s own; the last is auth's. `<reason>` is the
underlying open failure. The connection that started auth is closed
unanswered.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- The developer's shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  and `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- Nothing is listening on `127.0.0.1:3001`.
- `state/auth.db` exists but cannot be opened.
- A connection is made to `127.0.0.1:3001`.

Postconditions:

- Nothing has changed. auth served nothing and told systemd nothing.

## The host stops auth

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's
`Ctrl-C` sends `SIGINT`, which auth treats the same way. auth stops taking
new connections, finishes the requests it has already accepted, and exits.
It closes its own copy of the socket and nothing more: the socket belongs to
systemd, which keeps it open, and auth never removes
`/run/ikigenba/auth.sock`. It waits at most `DRAIN_SECONDS` for requests to
finish, which on a host is always less than the time the service unit allows
before systemd kills it.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

auth exits 0. Nothing is on stdout or stderr.

Preconditions:

- auth is serving as process `<pid>`, on the socket systemd or
  `systemd-socket-activate` passed it.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- Every request auth has accepted finishes within 5 seconds of the signal.

Postconditions:

- Every request accepted before the signal received its full response.
- `/run/ikigenba/auth.sock` still exists, and connections made to it after
  auth exited wait in the socket's queue for the next auth to answer.

## The host stops auth while a request outlasts the drain

auth waits for accepted requests only until its drain deadline,
`DRAIN_SECONDS` after the signal, so that it always exits before the service
unit's stop timeout and is never killed mid-write by systemd. A request
still running at the deadline — a sign-in callback still waiting on Google,
say — is cut off: its connection is closed without the rest of its response.
Losing a request is trouble, so auth says how many it lost and exits
non-zero.

Command:

```
$ kill -TERM <pid>
```

Output:

```
auth: stopped with <n> requests unfinished
```

auth exits 1, 5 seconds after the signal. The line is on stderr; stdout is
empty. `<n>` is the number of requests still running at the deadline. When
`<n>` is 1 the line reads `auth: stopped with 1 request unfinished`.

Preconditions:

- auth is serving as process `<pid>`, on the socket systemd or
  `systemd-socket-activate` passed it.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `<n>` of the requests auth has accepted are still running 5 seconds after
  the signal.

Postconditions:

- Every request that finished within 5 seconds of the signal received its
  full response; the `<n>` that did not were cut off.
- `/run/ikigenba/auth.sock` still exists, and connections made to it after
  auth exited wait in the socket's queue for the next auth to answer.

## The host restarts auth during a deploy

A deploy replaces auth's binary and restarts `ikigenba-auth.service` alone;
`ikigenba-auth.socket` stays up throughout. Between the old auth exiting and
the new one being ready, connections wait in the socket's queue instead of
being refused — nginx's `/check` subrequests for every other app among them —
so neither a visitor to auth nor a visitor to any app auth guards sees auth
missing. That holds because auth finishes what it accepted before it exits
and leaves the socket where systemd put it.

Command:

```
$ sudo systemctl restart ikigenba-auth.service
```

Output:

```
```

Exits 0, once the new auth has reported that it is ready. Nothing is on
stdout or stderr.

Preconditions:

- auth is serving on `/run/ikigenba/auth.sock` under
  `ikigenba-auth.service`.
- nginx is sending requests to `/run/ikigenba/auth.sock`, for auth's own
  pages and as `/check` subrequests, throughout the restart.

Postconditions:

- Every request nginx sent was answered, by the old auth or the new one;
  none was refused and none was cut off.
- A new auth process is serving on `/run/ikigenba/auth.sock`, over the same
  `state/auth.db`.

## The host starts auth without a socket

Run bare, with no socket passed in, auth has nothing to serve on, and it does
not open one of its own: there is no port or address it falls back to. That
is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or
`LISTEN_PID` naming some other process, counts as no socket passed in. The
detail says how to run auth correctly.

Command:

```
$ auth
```

Output:

```
auth: no socket was passed in

run it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3001 auth'
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- The developer's shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  and `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not auth's
  process id.

Postconditions:

- Nothing has changed. auth opened no database, listened on nothing, and told
  systemd nothing; an absent `state/auth.db` is still absent.

## The host passes auth more than one socket

auth serves on exactly one socket. A unit that passes it several is
misconfigured, and auth will not guess which one it was meant to serve on.

Command:

```
$ systemd-socket-activate -l 127.0.0.1:3001 -l 127.0.0.1:3002 auth
```

Output:

```
Listening on 127.0.0.1:3001 as 3.
Listening on 127.0.0.1:3002 as 4.
Communication attempt on fd 3.
Execing auth (auth)
auth: 2 sockets were passed in, expected 1

run it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3001 auth'
```

Exits 2. The text is on stderr; stdout is empty. The first four lines are
`systemd-socket-activate`'s own; the rest is auth's. The connection that
started auth is closed unanswered.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- The developer's shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  and `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- Nothing is listening on `127.0.0.1:3001` or `127.0.0.1:3002`.
- A connection is made to `127.0.0.1:3001`.

Postconditions:

- Nothing has changed. auth opened no database, served on neither socket, and
  told systemd nothing.

## The host gives auth a drain deadline that is not a number of seconds

auth reads `DRAIN_SECONDS` before it serves, so a bad value is found at start
rather than at the moment auth is asked to stop. A value that is not a
positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's
mistake, so it is a usage error and auth serves nothing. The value is quoted
back verbatim. auth sets no upper limit: keeping the drain inside the service
unit's stop timeout is opsctl's to enforce.

Command:

```
$ DRAIN_SECONDS=abc systemd-socket-activate -l 127.0.0.1:3001 auth
```

Output:

```
Listening on 127.0.0.1:3001 as 3.
Communication attempt on fd 3.
Execing auth (auth)
auth: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The text is on stderr; stdout is empty. The first three lines are
`systemd-socket-activate`'s own; the last is auth's. The connection that
started auth is closed unanswered.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- The developer's shell exports `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  and `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- Nothing is listening on `127.0.0.1:3001`.
- A connection is made to `127.0.0.1:3001`.

Postconditions:

- Nothing has changed. auth opened no database, served nothing, and told
  systemd nothing.

## The host starts auth without a Google setting

A required Google setting is not in auth's environment. auth reports the
missing name and refuses to start. It checks the Google settings before
anything else, so the name is reported whether or not a socket was passed in
and whatever `DRAIN_SECONDS` holds.

Command:

```
$ auth
```

Output:

```
auth: GOOGLE_CLIENT_ID is not set
```

Exits 2. The line is on stderr; stdout is empty. `GOOGLE_CLIENT_SECRET` and
`WORKSPACE_DOMAIN` are each required the same way and fail identically with
their own name; when several are missing, the first of `GOOGLE_CLIENT_ID`,
`GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN` is the one named.

Preconditions:

- `bin/auth` exists and is on the developer's `PATH` as `auth`.
- `GOOGLE_CLIENT_ID` is unset or empty in the environment; this story's
  shell does not export it.

Postconditions:

- Nothing has changed. auth opened no database, listened on nothing, and told
  systemd nothing.
