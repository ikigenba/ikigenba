# Stories — serve

The bare binary serves. The host's service unit runs it with no arguments and
an environment file that sets `PORT`; nothing else is passed. dummy listens on
`127.0.0.1:$PORT` only, because nginx on the host terminates TLS and proxies
to loopback. A healthy server prints nothing, so under systemd the journal
holds only trouble. The actor in these stories is the host, whether that is
systemd or a developer at a terminal standing in for it.

## The host starts dummy

Command:

```
$ PORT=3000 dummy
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/dummy` exists.
- Nothing is listening on `127.0.0.1:3000`.

Postconditions:

- dummy is listening on `127.0.0.1:3000` and on no other address.
- It keeps running until it is signalled.

## The host stops dummy

`systemctl stop` and `systemctl restart` send `SIGTERM`. dummy finishes the
requests it has accepted, closes the listener, and exits.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

The server exits 0. Nothing is on stdout or stderr.

Preconditions:

- dummy is running with `PORT=3000` as process `<pid>`.

Postconditions:

- Every request accepted before the signal received its full response.
- Nothing is listening on `127.0.0.1:3000`.

## The host starts dummy without a port

Command:

```
$ dummy
```

Output:

```
dummy: PORT is not set
```

Exits 2. The line is on stderr; stdout is empty. A value that is not an
integer from 1 to 65535 fails the same way with `dummy: PORT is 'abc', not a
port number`.

Preconditions:

- `bin/dummy` exists.
- `PORT` is unset or empty in the environment.

Postconditions:

- Nothing has changed. Nothing is listening.

## The host starts dummy on a port already in use

The line is the listener's own complaint, in the shape opsctl's install
story relays for a service that will not start.

Command:

```
$ PORT=3000 dummy
```

Output:

```
dummy: listen tcp 127.0.0.1:3000: bind: address already in use
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `bin/dummy` exists.
- Another process is listening on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed. The other process still holds the port.
