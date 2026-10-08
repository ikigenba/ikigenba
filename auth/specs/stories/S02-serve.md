# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits:
auth never opens one of its own. It takes the socket the way systemd socket
activation passes it — `LISTEN_PID` names auth's own process, `LISTEN_FDS` is
`1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*`
variables from its environment once it has taken it. What kind of socket it is,
and where it lives, is the host's business: auth serves whatever it is passed
the same way. On a host, opsctl publishes `ikigenba-auth.socket` beside
`ikigenba-auth.service`, which runs `/opt/auth/bin/auth` with no arguments as
the `ikigenba` user, with `/opt/auth` as its working directory and
`/opt/auth/etc/env` as its environment file; the host's nginx sends auth the
requests for auth's own hostname and the identity subrequest, `/check`, for
every other app, or `/check/open` for an app that serves guests. The service is
`Type=notify`: auth tells systemd it is ready, by sending `READY=1` to
`$NOTIFY_SOCKET`, once it is serving. The actor in these stories is the host,
whether that is systemd or a developer at a terminal standing in for it.

The environment auth reads is the two Google secrets `GOOGLE_CLIENT_ID` and
`GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, `DRAIN_SECONDS`,
`IKIGENBA_PUBLIC_URL`, `IKIGENBA_CALLBACK_URL`, and `IKIGENBA_SERVICES`. The
first three are required. `DRAIN_SECONDS` is how long auth drains when stopped,
a positive whole number of seconds, and 5 when it is unset or empty. On a host,
opsctl owns that value and the service unit's stop timeout: both are space-wide
settings in opsctl's configuration, opsctl writes the drain into every app's
`etc/env` and the stop timeout (10 seconds by default, always longer than the
drain) into every service unit, and an app's manifest never sets either.
`IKIGENBA_SERVICES` is the path of the host's services file, which lists the
platform's services for the launcher in the banner of auth's signed-in pages
(`S03-sign-in.md`) and names the telemetry service auth delivers its events to
(below). On a host, opsctl sets it in the environment the host gives auth,
normally `/var/lib/ikigenba/services.json`; on a host that has no services
file it is unset, and auth's pages then carry no launcher. For the banner auth
reads the variable once, when it starts, and never fails to start over it:
unset, empty, a path not in its plain form (`S03-sign-in.md`), or naming a file
that is missing or unreadable, auth starts and serves all the same, and says
nothing about the launcher. A services file it cannot use also leaves auth no
telemetry service to deliver its events to, which is trouble of its own (see
`The host starts auth where telemetry cannot be reached`).

`IKIGENBA_PUBLIC_URL` and `IKIGENBA_CALLBACK_URL` are optional, and each is
independent of the other. Each is an origin: `http` or `https`, then `://`, a
host, and optionally `:` and a port of digits, with nothing else — no user
part, no path (not even a trailing `/`), no query, and no fragment. A host sets
neither, and with neither set auth's own origin is `https://auth.<space>`, the
Google `redirect_uri` is `https://auth.<space>/login/google/callback` for the
space the request's `Host` names, and sign-out accepts origins on the space
over `https` with no port (`S03-sign-in.md`). A sandbox, the local runner a
developer runs the platform in, sets both: a sandbox named `wip` on port 7400
sets `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400` and
`IKIGENBA_CALLBACK_URL=http://localhost:7400` (`S09-in-a-sandbox.md`). When
`IKIGENBA_PUBLIC_URL` is set, it is auth's own origin, the one its token
actions accept (`S05-tokens.md`), and sign-out accepts origins on the space with
its scheme and port instead of `https` and none (`S03-sign-in.md`). When
`IKIGENBA_CALLBACK_URL` is set, the Google `redirect_uri` is that value
followed by `/login/google/callback` —
`http://localhost:7400/login/google/callback` in that sandbox — the same in the
redirect that starts a sign-in and in the code exchange that finishes it,
whatever `Host` the request names. Either way the space is still read from the
request's `Host`. auth does not read `IKIGENBA_SANDBOX`, which a sandbox also
sets. Only an unset variable is absent: one present in the environment with an
empty value is not an origin, and like any other value that is not one it stops
auth from starting. A value is an origin only as a browser writes one: its host
is lowercase, with no empty label and no trailing `.`, and its port, if any, has
no leading zero, is 1 to 65535, and is not the scheme's default (`80` for
`http`, `443` for `https`).

auth checks its environment first — `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
`WORKSPACE_DOMAIN`, then `DRAIN_SECONDS`, then `IKIGENBA_PUBLIC_URL`, then
`IKIGENBA_CALLBACK_URL` — then looks for its socket, and only then opens its
SQLite database at `state/auth.db`, relative to its working directory. So a
start refused as a usage error has touched nothing, not even the database.
Opening it, auth creates `state/` if it is absent and `state/auth.db` if it is
absent, brings the database up to date by applying, in order, every migration
it carries that the database has not had (`S01-bootstrap.md`), and only then
serves and tells systemd it is ready. A database it cannot open is a start it
refuses, with one line on stderr, `auth: cannot open database state/auth.db:
<reason>`, and exit status 1. A database that records a migration it does not
carry, one a newer auth has upgraded, it serves as it finds it, applying
nothing and warning once on stderr. The users, sessions, sign-ins in flight
and tokens it keeps there outlive every restart and deploy. auth is the
database's only writer, and the host replicates it as the manifest declares
(`S01-bootstrap.md`).
Starting touches no network: the Google settings are read and required at
startup, but Google itself is reached only when a human signs in
(`S03-sign-in.md`), so auth serves even while Google is unreachable, and
`/check` and `/me` keep answering from the local database (`S04-check.md`). In
the stories below that run `auth` directly, its environment sets
`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
`WORKSPACE_DOMAIN=michaelgreenly.dev`, and leaves `IKIGENBA_PUBLIC_URL` and
`IKIGENBA_CALLBACK_URL` unset, unless a story says otherwise.

auth records what it does as a trail of events, which it delivers to the
platform's telemetry service: the entry named `telemetry` in the services file
`IKIGENBA_SERVICES` names, at that entry's socket. Each event carries its
time, the service `auth`, its name, a request id, a user, and attributes; the
request id and the user are empty for an event that has none. auth delivers
its events one at a time, in the order it records them, and never holds up an
answer to do it. It reads the services file afresh for every delivery, so a
telemetry service installed after auth started is reached without restarting
auth. In every group's stories, "auth records `<event>` with `<key>=<value>`,
…, under request id `<request-id>` and user `<user-id>`" means auth records
that event with exactly those attributes and that request id and user ("no
request id", "no user" when they are empty), and unless a story says
otherwise the telemetry service is reachable, so every event auth records
reaches the trail.

- Once auth is serving, as it reports ready, it records `service.started` with
  `version`, the string `auth --version` prints (`S01-bootstrap.md`); its
  request id and user are empty. A start that fails before auth is serving
  records no event.
- Every request auth serves — its pages, `/check`, `/check/open`, `/me`, the
  files under `/_appkit/`, a 404 — is recorded twice: `request.started` with
  `method` and `path`, the request's method and its URL path without the query,
  when it arrives, and `request.finished` with `status`, the status auth
  answered, `duration_us`, how long auth took to answer in whole microseconds,
  `request_bytes`, how many bytes of the request's body auth read, and
  `response_bytes`, how many bytes of body its answer carried, once it has
  answered. Both carry the request's id, its `X-Request-Id`, and the user named
  by its `X-User-Id`, empty when it carries none. Every event auth records while
  answering a request comes between the two and carries the same request id. A
  request that arrives without an `X-Request-Id`, as one does when no nginx
  stands in front of auth, is given an id of the same shape, 32 lowercase
  hexadecimal characters, and is recorded under it.
- When auth is stopped, it finishes the requests it accepted, so each has
  recorded its `request.finished`, and then records `service.stopping` with
  `reason`, the name of the signal that stopped it, `SIGTERM` or `SIGINT`; its
  request id and user are empty. It is the last event auth records, and auth
  waits for its events to be delivered within the same drain deadline before
  it exits.

auth's stderr holds only trouble, so under systemd the journal shows nothing
else. Trouble is of three kinds. One is a condition auth cannot continue
from: a start it refuses, a database it cannot open, a stop that cut requests
off; each has its own diagnostic in the stories below. Another is a database
a newer auth has upgraded, which auth serves but warns of once when it starts
(`The host starts auth with a database a newer auth has upgraded`). The third
is an event auth could not deliver: when the telemetry service does not take
an event after a few quick tries — there is no services file, no entry named
`telemetry` in it, or nothing accepting on its socket — or when auth's events
queue up faster than it can deliver them, auth writes the event to stderr as
one line, `auth: undelivered event: <event>`, where `<event>` is the event as
it would have been delivered, a single-line JSON object whose members are, in
order, `time`, `service`, `event`, `request_id`, `user`, and `attrs`. auth
then goes on serving: telemetry being unreachable never stops auth from
starting or answering. A request auth answers is not trouble, whatever its
status. A handled failure — a 500 when auth's own database fails a request
(`S04-check.md`), a 502 when Google fails a sign-in (`S03-sign-in.md`) — writes
nothing to stderr; its `request.finished` records the status, and the request
id ties it to nginx's log of the same request.

auth serves on the terms every app of the platform serves on. The socket it is
passed is its only way in. Only nginx and the suite's own apps can reach it;
keeping everything else out is the host's job, not auth's. The suite is a closed
system that only we deploy services into, and auth trusts it: `X-Request-Id`, 32
lowercase hexadecimal characters, is set by nginx on every request it forwards
to auth and on every `/check` and `/check/open` subrequest, overwriting whatever
a client sent, and a sibling that calls auth directly copies it from the request
it is serving. auth decides identity itself, from the session cookie or a token,
and calls no sibling.

## The host starts auth

systemd owns the socket, so it exists, and accepts connections into its
queue, before auth starts and while it is stopped; auth's part is to serve
what arrives on it. `systemctl start` returns once auth has
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
- `ikigenba-auth.socket` is active, so the socket it holds accepts
  connections.
- `/opt/auth/etc/env` sets `IKIGENBA_SERVICES` to the host's services file,
  which has an entry named `telemetry` whose socket accepts events.
- `/opt/auth/state/auth.db` exists, from an earlier start, and records no
  migration this auth does not carry.
- `ikigenba-auth.service` is not running.

Postconditions:

- `ikigenba-auth.service` is `active`, and auth is serving on the socket
  `ikigenba-auth.socket` passed it: a connection there, and every connection
  queued before auth started, is answered by auth.
- auth listens on no other socket.
- `/opt/auth/state/auth.db` is the database it opened; it existed already,
  and is now up to date. Every user, session, and token it held is still
  there.
- No network call to Google was made; the Google settings were read from the
  environment, not checked against Google.
- auth records `service.started` with `version=v<semver>`, the version
  `/opt/auth/bin/auth --version` prints, under no request id and no user.
- auth has written nothing to the journal.
- It keeps running until it is signalled.

## The host starts auth for the first time

The database file does not yet exist. auth creates the `state/` directory if
it is absent, then creates `state/auth.db` and its schema and serves. The
same start succeeds when `state/` already exists and only the database is
absent. Neither a fresh deployment nor any other first start needs the
directory created beforehand. The paths are relative to auth's working
directory. A database auth creates holds no users, sessions, or tokens: the
first member to sign in is the first user.

Command:

```
$ auth
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so auth reports readiness to nobody.
- `IKIGENBA_SERVICES` names a services file with an entry named `telemetry`
  whose socket accepts events.
- `state/auth.db` does not exist.
- Either `state/` is absent and auth can create it in its working directory,
  or `state/` is an existing directory in which auth can create the database.

Postconditions:

- `state/` exists, created by auth if it was absent.
- `state/auth.db` now exists, created by this start, and is up to date:
  `auth db status` prints `0001 applied <time>`, `0002 applied <time>` and
  `0003 applied <time>`, each `<time>` being the moment this start applied it
  (`S01-bootstrap.md`).
- The database holds no users, sessions, or tokens.
- auth is serving on the socket it was passed, and on no other.
- auth records `service.started` with `version=v<semver>`, the version
  `auth --version` prints, under no request id and no user.
- It keeps running until it is signalled.

## The host starts auth over a database an earlier auth wrote

An auth from before the migrations kept its database without recording any
migration. The first start of this auth on it applies all three migrations it
carries: `0001` finds the tables already there and changes nothing, `0002`
gives each token that still has a bare id the prefixed one, and `0003` makes
each token a personal token, changing nothing else about it
(`S05-tokens.md`). Every user, session, sign-in in flight and token the
database held is still there, and from then on the database is up to date.

Command:

```
$ auth
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `IKIGENBA_SERVICES` names a services file with an entry named `telemetry`
  whose socket accepts events.
- `state/auth.db` exists, written by an auth from before the migrations, and
  records no migration.

Postconditions:

- auth is serving on the socket it was passed, over the same `state/auth.db`.
- `auth db status` prints `0001 applied <time>`, `0002 applied <time>` and
  `0003 applied <time>`, each `<time>` being the moment this start applied it
  (`S01-bootstrap.md`).
- Every user, session, and token the database held is still there; a token's
  id now carries the `tok_` prefix and the token is a personal token, and
  nothing else about it has changed: it keeps its secret, name, times and
  state, and authenticates everywhere it did (`S05-tokens.md`).
- auth records `service.started` with `version=v<semver>`, the version
  `auth --version` prints, under no request id and no user.
- It keeps running until it is signalled.

## The host starts auth where telemetry cannot be reached

A developer running auth at a terminal, or a host with no telemetry service
installed, gives auth nowhere to deliver its events. auth serves all the same:
each event it records, from `service.started` on, goes to stderr instead,
once a few quick tries to deliver it have failed. That is trouble, so it is on
stderr; it is not a reason to stop.

Command:

```
$ auth
```

Output:

```
auth: undelivered event: {"time":"<time>","service":"auth","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
```

Does not exit. The line is on stderr; stdout is empty. `<time>` is when auth
became ready, in UTC, as `2026-10-02T14:03:09.123456Z`: six fractional digits
and a `Z`. `v<semver>` is what `auth --version` prints. Every later event auth
records — the two of every request it serves, and `service.stopping` when it
is stopped — is written the same way, one line each, in the order recorded.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `IKIGENBA_SERVICES` is unset, or names a services file that has no entry
  named `telemetry`, or whose `telemetry` entry names a socket nothing accepts
  on.

Postconditions:

- auth is serving on the socket it was passed, and answers every request as
  it would with telemetry reachable.
- Nothing reached the trail.

## The host starts auth where its state directory cannot be created

A regular file named `state` occupies the path where auth needs its state
directory. auth has taken its socket, but it reports the database setup
failure and exits before it serves or reports ready. The failure is not the
caller's usage, so it exits 1.

Command:

```
$ auth
```

Output:

```
auth: cannot open database state/auth.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the
underlying directory-creation failure.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `state` is an existing regular file in auth's working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database was created. auth served nothing, told systemd nothing, and
  recorded no event.

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
$ auth
```

Output:

```
auth: cannot open database state/auth.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the
underlying open failure.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `state/auth.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. auth served nothing, told systemd nothing, and recorded
  no event.

## The host starts auth with a database a newer auth has upgraded

A deploy rolled back to an older binary leaves it over a database a newer auth
has upgraded: the database records a migration this auth does not carry. Each
release's migrations only add to the schema the release before it uses, so
this auth serves that database as it finds it, and the rollback works. It
applies nothing, not even a migration it carries that the database lacks, and
otherwise starts and serves as it always does. It says once, on stderr, that
the database is ahead of it, naming the lowest version it does not know,
zero-padded to four digits; that line is no event, and nothing reaches the
trail for it. `auth db status` lists every version it does not know as
`unknown` (`S01-bootstrap.md`). Under systemd the start succeeds, and the
journal holds the line.

Command:

```
$ auth
```

Output:

```
auth: unknown migration version 0004: database is ahead of this binary
```

Does not exit. The line is on stderr, written before auth reports ready;
stdout is empty.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`, carrying migrations `0001`,
  `0002` and `0003` only.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `IKIGENBA_SERVICES` names a services file with an entry named `telemetry`
  whose socket accepts events.
- `state/auth.db` exists and records versions `0001`, `0002`, `0003`, and
  `0004` as applied. It holds a member's session, live now.

Postconditions:

- auth is serving on the socket it was passed, over the same `state/auth.db`:
  `/check` with that member's session cookie answers 200 (`S04-check.md`).
- Starting changed nothing in the database: it still records `0001`, `0002`,
  `0003`, and `0004`, and holds the users, sessions, and tokens it held.
- auth records `service.started` with `version=v<semver>`, the version
  `auth --version` prints, under no request id and no user.
- The line above is all auth has written to stderr.
- It keeps running until it is signalled.

## The host stops auth

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C`
sends `SIGINT`, which auth treats the same way. auth stops taking new
connections, finishes the requests it has already accepted, records
`service.stopping`, and exits once its events are delivered. It closes its
own copy of the socket and nothing more: the socket belongs to
systemd, which keeps it open, and auth never removes it. It waits at most
`DRAIN_SECONDS` for requests to finish, which on a host is always less than the
time the service unit allows before systemd kills it.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

auth exits 0. Nothing is on stdout or stderr.

Preconditions:

- auth is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `IKIGENBA_SERVICES` names a services file with an entry named `telemetry`
  whose socket accepts events.
- Every request auth has accepted finishes within 5 seconds of the signal.

Postconditions:

- Every request accepted before the signal received its full response, and
  auth recorded its `request.finished`.
- After the last of those, auth records `service.stopping` with
  `reason=SIGTERM`, under no request id and no user, the last event it
  records. Stopped with `SIGINT` instead, it records `reason=SIGINT`.
- The socket auth was passed still exists, and connections made to it after
  auth exited wait in the socket's queue for the next auth to answer.

## The host stops auth while a request outlasts the drain

auth waits for accepted requests only until its drain deadline,
`DRAIN_SECONDS` after the signal, so that it always exits before the service
unit's stop timeout and is never killed mid-write by systemd. A request
still running at the deadline — a sign-in callback still waiting on Google,
say — is cut off: its connection is closed without the rest of its response,
and its `request.finished` never reaches the trail, so in the trail it is a
request that started and never finished. Losing a request is trouble, so auth
says how many it lost and exits non-zero. The drain deadline has passed by
then, so auth does not wait to deliver `service.stopping`: it writes it to
stderr as an undelivered event, after any other event it had not yet
delivered.

Command:

```
$ kill -TERM <pid>
```

Output: the two lines below, and possibly further `auth: undelivered event:`
lines, as described after the block.

```
auth: undelivered event: {"time":"<time>","service":"auth","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
auth: stopped with <n> requests unfinished
```

auth exits 1, 5 seconds after the signal. The lines are on stderr; stdout is
empty. `<time>` is when auth recorded `service.stopping`, in UTC with six
fractional digits and a `Z`. `<n>` is the number of requests still running at
the deadline. When `<n>` is 1 the `stopped with` line reads
`auth: stopped with 1 request unfinished`. Any event auth recorded but had not
delivered by the deadline comes before the `service.stopping` line, in the
order recorded, one `auth: undelivered event:` line each. A cut-off request
whose handler ends before auth exits records its `request.finished` too late
for the trail, and it appears as one more `auth: undelivered event:` line;
where that line falls relative to the `service.stopping` line and to the
`stopped with` line is not fixed.

Preconditions:

- auth is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `<n>` of the requests auth has accepted are still running 5 seconds after
  the signal.
- `IKIGENBA_SERVICES` names a services file with an entry named `telemetry`
  whose socket accepts events.

Postconditions:

- Every request that finished within 5 seconds of the signal received its
  full response, and auth recorded its `request.finished`; the `<n>` that did
  not were cut off, and the trail holds their `request.started` and no
  `request.finished`; any `request.finished` of theirs is only on stderr.
- `service.stopping` did not reach the trail; it is on stderr.
- The socket auth was passed still exists, and connections made to it after
  auth exited wait in the socket's queue for the next auth to answer.

## The host restarts auth during a deploy

A deploy replaces auth's binary and restarts `ikigenba-auth.service` alone;
`ikigenba-auth.socket` stays up throughout. Between the old auth exiting and the
new one being ready, connections wait in the socket's queue instead of being
refused — nginx's `/check` and `/check/open` subrequests for every other app
among them — so neither a visitor to auth nor a visitor to any app auth guards
sees auth missing. That holds because auth finishes what it accepted before it
exits and leaves the socket where systemd put it. Users, sessions, and tokens
outlive the deploy: the new auth opens the same `state/auth.db`, bringing it
up to date first when the new binary carries a migration the database has not
had, so a visitor signed in before the deploy is still signed in after it and
every token still authenticates.

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

- auth is serving under `ikigenba-auth.service`, on the socket
  `ikigenba-auth.socket` passed it.
- nginx is sending requests to auth, for auth's own pages and as `/check`
  subrequests, throughout the restart.
- The host's services file has an entry named `telemetry` whose socket
  accepts events.

Postconditions:

- Every request nginx sent was answered, by the old auth or the new one;
  none was refused and none was cut off. Each is recorded by the auth that
  answered it, with its `request.started` and `request.finished`.
- The old auth recorded `service.stopping` with `reason=SIGTERM`, after the
  `request.finished` of every request it answered; the new auth recorded
  `service.started` with `version=v<semver>`, the version of the binary the
  deploy installed, so the trail shows the deploy as a new version in a start event.
- A new auth process is serving on the same socket, over the same
  `state/auth.db`, now up to date. Every user, session, and token the old
  auth held is still there.

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

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
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
$ auth
```

Output:

```
auth: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `2`: two listening
  sockets are passed in, as file descriptors 3 and 4.

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
$ DRAIN_SECONDS=abc auth
```

Output:

```
auth: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `LISTEN_PID` is auth's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. auth opened no database, served nothing, and told
  systemd nothing.

## The host gives auth a public URL that is not an origin

auth reads `IKIGENBA_PUBLIC_URL` before it serves, so a bad value is found at
start rather than at the first request that needs auth's own origin. A value
that is not an origin — `http://auth.wip.localhost:7400/` with its trailing
`/`, a path, a query, a fragment, a user part, a scheme other than `http` or
`https`, no host, a host with an uppercase letter
(`http://Auth.wip.localhost:7400`) or with an empty label or a trailing `.`
(`http://auth.wip.localhost.:7400`), or a port that is not digits, has a
leading zero, is `0` or above `65535`, or is the scheme's default
(`http://auth.wip.localhost:80`) — is the caller's mistake, so it is a usage
error and auth serves nothing. A variable present with an empty
value is refused the same way, as `IKIGENBA_PUBLIC_URL is '', not an origin`;
only an unset variable means auth's own origin is `https://auth.<space>`. The
value is quoted back verbatim. auth checks it after `DRAIN_SECONDS` and before
`IKIGENBA_CALLBACK_URL`, so it is the one named when both are bad, and before
it looks for its socket, so it is reported whether or not a socket was passed
in.

Command:

```
$ IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400/ auth
```

Output:

```
auth: IKIGENBA_PUBLIC_URL is 'http://auth.wip.localhost:7400/', not an origin
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `IKIGENBA_PUBLIC_URL` is set to a value that is not an origin.

Postconditions:

- Nothing has changed. auth opened no database, listened on nothing, and told
  systemd nothing; an absent `state/auth.db` is still absent.

## The host gives auth a callback URL that is not an origin

auth reads `IKIGENBA_CALLBACK_URL` before it serves, so a bad value is found
at start rather than when someone first signs in. It must be an origin by the
same rule as `IKIGENBA_PUBLIC_URL`: `http://localhost:7400/`, with its trailing
`/`, is not one, because auth adds `/login/google/callback` to the origin
itself. A variable present with an empty value is refused the same way; only
an unset variable means the callback is read from the request's `Host`. The
value is quoted back verbatim. auth checks it after `IKIGENBA_PUBLIC_URL` and
before it looks for its socket, so it is reported whether or not a socket was
passed in.

Command:

```
$ IKIGENBA_CALLBACK_URL=http://localhost:7400/ auth
```

Output:

```
auth: IKIGENBA_CALLBACK_URL is 'http://localhost:7400/', not an origin
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/auth` exists and is on the `PATH` as `auth`.
- auth's environment sets `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
  `WORKSPACE_DOMAIN=michaelgreenly.dev`.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `IKIGENBA_PUBLIC_URL` is unset, or an origin.
- `IKIGENBA_CALLBACK_URL` is set to a value that is not an origin.

Postconditions:

- Nothing has changed. auth opened no database, listened on nothing, and told
  systemd nothing; an absent `state/auth.db` is still absent.

## The host starts auth without a Google setting

A required Google setting is not in auth's environment. auth reports the
missing name and refuses to start. It checks the Google settings before
anything else, so the name is reported whether or not a socket was passed in
and whatever `DRAIN_SECONDS`, `IKIGENBA_PUBLIC_URL`, and
`IKIGENBA_CALLBACK_URL` hold.

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

- `bin/auth` exists and is on the `PATH` as `auth`.
- `GOOGLE_CLIENT_ID` is unset or empty in auth's environment.

Postconditions:

- Nothing has changed. auth opened no database, listened on nothing, and told
  systemd nothing.
