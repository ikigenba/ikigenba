# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: mcp never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names mcp's own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-mcp.socket`, which holds the Unix socket `/run/ikigenba/mcp.sock`, beside `ikigenba-mcp.service`, which runs `/opt/mcp/bin/mcp` with no arguments as the `ikigenba` user, with `/opt/mcp` as its working directory and `/opt/mcp/etc/env` as its environment file; nginx proxies to `http://unix:/run/ikigenba/mcp.sock:`. The service is `Type=notify`: mcp tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, mcp drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty. On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's `etc/env` and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. mcp's environment also carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/var/lib/ikigenba/services.json`, which opsctl sets in the environment the host gives mcp. The file lists the platform's services: it feeds the launcher in the connect page's banner (`S03`), and it is where the gateway finds the suite's MCP services and the socket each one serves on (`S06`). mcp reads the variable once, when it starts, and reads the file it names afresh on every request, so a rewritten file shows on the next request without a restart. mcp never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, mcp starts and serves all the same, treats the file as listing no services, and says nothing about it. A diagnostic mcp writes about a request names that request by its `X-Request-Id`, as `mcp: request <id>: <reason>` on stderr, so a line in the journal can be matched to nginx's log of the same request; a request that carries no `X-Request-Id` is named `-`. An app writes one such line for each request it answers with a 5xx, any status from 500 through 599, and nothing for any other answer: a 4xx is the caller's to fix, not trouble. A healthy app prints nothing, so under systemd the journal holds only trouble; mcp has one exception, below. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

These are the terms every app of the platform serves on, the same as dummy's. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. An app that calls a sibling while serving a request calls it directly at its socket, not through nginx, and copies `X-User-Id`, `X-User-Email`, and `X-Request-Id` from the request it is serving onto the call, so the sibling cannot tell the call from one nginx made.

mcp is an app that calls its siblings: calling them is what the gateway is for. While serving a request to `/mcp` it calls a backend directly at the socket that backend's entry in the services file names (on a host, `/run/ikigenba/<name>.sock`), never through nginx, and copies `X-User-Id`, `X-User-Email`, and `X-Request-Id` from the request onto the call; a header the request lacked is not sent. One request to the gateway may wait up to 50 seconds on its backends (`S08`). These backend calls are mcp's one exception to printing nothing when healthy: it writes one line to stderr for each request it makes to a backend, when that request ends, whatever its outcome (`S08`). A request that never reaches a backend writes no such line.

## The host starts mcp

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/mcp.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before mcp starts and while it is stopped; mcp's part is to serve what arrives on it. `systemctl start` returns once mcp has reported that it is ready.

Command:

```
$ sudo systemctl start ikigenba-mcp.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed mcp: `/opt/mcp/bin/mcp` exists, and `ikigenba-mcp.socket` and `ikigenba-mcp.service` are published.
- `ikigenba-mcp.socket` is active, so `/run/ikigenba/mcp.sock` exists and accepts connections.
- `ikigenba-mcp.service` is not running.

Postconditions:

- `ikigenba-mcp.service` is `active`, and mcp is serving on `/run/ikigenba/mcp.sock`: a connection there, and every connection queued before mcp started, is answered by mcp.
- mcp listens on no other socket and no port.
- mcp has written nothing to the journal.
- It keeps running until it is signalled.

## The host stops mcp

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which mcp treats the same way. mcp stops taking new connections, finishes the requests it has already accepted, and exits. It closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and mcp never removes `/run/ikigenba/mcp.sock`. It waits at most `DRAIN_SECONDS` for requests to finish, which on a host is always less than the time the service unit allows before systemd kills it.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

mcp exits 0. Nothing is on stdout or stderr.

Preconditions:

- mcp is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- Every request mcp has accepted finishes within 5 seconds of the signal.

Postconditions:

- Every request accepted before the signal received its full response.
- `/run/ikigenba/mcp.sock` still exists, and connections made to it after mcp exited wait in the socket's queue for the next mcp to answer.

## The host stops mcp while a request outlasts the drain

mcp waits for accepted requests only until its drain deadline, `DRAIN_SECONDS` after the signal, so that it always exits before the service unit's stop timeout and is never killed mid-write by systemd. A request still running at the deadline is cut off: its connection is closed without the rest of its response. The request still running may be one waiting on a backend; it is cut off and counted like any other. Losing a request is trouble, so mcp says how many it lost and exits non-zero.

Command:

```
$ kill -TERM <pid>
```

Output:

```
mcp: stopped with <n> requests unfinished
```

mcp exits 1, 5 seconds after the signal. The line is on stderr; stdout is empty. `<n>` is the number of requests still running at the deadline. When `<n>` is 1 the line reads `mcp: stopped with 1 request unfinished`.

Preconditions:

- mcp is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `<n>` of the requests mcp has accepted are still running 5 seconds after the signal.

Postconditions:

- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off.
- `/run/ikigenba/mcp.sock` still exists, and connections made to it after mcp exited wait in the socket's queue for the next mcp to answer.

## The host restarts mcp during a deploy

A deploy replaces mcp's binary and restarts `ikigenba-mcp.service` alone; `ikigenba-mcp.socket` stays up throughout. Between the old mcp exiting and the new one being ready, connections wait in the socket's queue instead of being refused, so a client never sees mcp missing. That holds because mcp finishes what it accepted before it exits and leaves the socket where systemd put it.

Command:

```
$ sudo systemctl restart ikigenba-mcp.service
```

Output:

```
```

Exits 0, once the new mcp has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- mcp is serving on `/run/ikigenba/mcp.sock` under `ikigenba-mcp.service`.
- A client is sending requests to `/run/ikigenba/mcp.sock` throughout the restart.

Postconditions:

- Every request the client sent was answered, by the old mcp or the new one; none was refused and none was cut off.
- A new mcp process is serving on `/run/ikigenba/mcp.sock`.

## The host starts mcp without a socket

Run bare, with no socket passed in, mcp has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run mcp correctly.

Command:

```
$ mcp
```

Output:

```
mcp: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/mcp` exists and is on the `PATH` as `mcp`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not mcp's process id.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. mcp listened on nothing and told systemd nothing.

## The host passes mcp more than one socket

mcp serves on exactly one socket. A unit that passes it several is misconfigured, and mcp will not guess which one it was meant to serve on.

Command:

```
$ mcp
```

Output:

```
mcp: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/mcp` exists and is on the `PATH` as `mcp`.
- `LISTEN_PID` is mcp's process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. mcp served on neither socket and told systemd nothing.

## The host gives mcp a drain deadline that is not a number of seconds

mcp reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment mcp is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and mcp serves nothing. The value is quoted back verbatim. mcp sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce.

Command:

```
$ DRAIN_SECONDS=abc mcp
```

Output:

```
mcp: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/mcp` exists and is on the `PATH` as `mcp`.
- `LISTEN_PID` is mcp's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. mcp served nothing and told systemd nothing.
