# Architecture

The runtime decisions that hold across sub-projects. Each one is a choice we
made and can unmake; a sub-project's design says how it honours the choice
within its own boundary. For the host itself, see `infra/AGENTS.md`; for how
an app reaches a host, see the `devctl` and `opsctl` specs.

## The shape of a space

A space is one EC2 host. nginx on the host is the single gateway: it
terminates TLS, and every request to a service passes through it. An app
lives under `/opt/<app>/`, runs as `ikigenba-<app>.service`, listens on a
Unix socket rather than a port, and is installed and operated by `opsctl`,
deployed to by `devctl`. An app that keeps state keeps it in SQLite, at the
path its manifest declares.

## An app on disk

Every app has the same shape on a host, and every tool on the platform
assumes it. An app is `/opt/<app>/`, a small filesystem hierarchy:

| directory | holds | written by |
|---|---|---|
| `bin/` | the one static binary, `bin/<app>` | install, from the built file |
| `etc/` | `manifest.toml`, the app's `nginx.conf` fragment, the generated `env` | install, from the built file plus `env` |
| `share/` | static assets, when the app has any | install, from the built file |
| `state/` | everything the app keeps: its SQLite database and files | the app, and only the app |
| `cache/` | what the app can rebuild | the app, and only the app |

`bin/`, `etc/`, and `share/` are the release. They are owned by root, read-only
to the app, and replaced wholesale on every install; nothing an app writes
there survives. `state/` and `cache/` are the app's. Install never creates or
touches them, so installing over a running app keeps its data. Uninstall
removes the release and `cache/` and keeps `state/`, so a later deploy lands
over the data. Backup copies `etc/` and `state/`, never `cache/`, and leaves
the database to litestream.

The app runs as the `ikigenba` account with `/opt/<app>` as its working
directory, so `state/auth.db` in a manifest means `/opt/auth/state/auth.db`.
Its configuration reaches it through `etc/env`, which install generates from
the manifest's `[env]` table and the secrets fetched from the space's
parameter store; nothing is passed on the command line.

How the release gets there:

1. **`devctl build <app>`** at a commit tagged `<app>/v<semver>` produces
   `<app>/dist/<app>-v<semver>.tar.xz`. It holds `bin/<app>`, `etc/`, and
   `share/` when present, and no version anywhere inside. The binary answers
   `--version` and `manifest`, and build checks both against the tag and the
   committed `etc/manifest.toml`.
2. **`devctl deploy <space> <file>`** uploads the file to the space's
   `deploy/` prefix in the backup bucket and runs `opsctl install` over ssh.
   The file never travels over ssh; the host fetches it with its own role.
3. **`opsctl install`** validates the file, writes `etc/env`, replaces the
   release directories, publishes `ikigenba-<app>.service`, regenerates the
   nginx and litestream configuration from every app on the host, and starts
   or restarts the service.

The version is never recorded anywhere but the binary. `space status` asks
each installed `bin/<app>` for it, and that is the only source.

## nginx as the only way in, over Unix sockets

Services listen on Unix sockets, one per app at `/run/ikigenba/<app>.sock`,
and nginx proxies to them with `proxy_pass http://unix:...`. No service
binds a TCP port. Two things follow:

- **Nothing reaches a service except nginx.** A loopback port is open to any
  process on the host; a Unix socket is open to its group. The socket is
  owned by the app's user with group `nginx` and mode `0660`, so nginx can
  connect and a neighbouring app cannot.
- **No port allocation.** The socket path is derived from the app name, so
  there is nothing to declare and nothing to collide.

Where this stands: the `opsctl` app model (D08) carries a `port` in the
manifest and the nginx design (D06) proxies to it. Both change under this
decision, and `port` leaves the manifest, as does `PORT` from the
environment file an app is started with.

## Request authentication at the gateway

The apps on a space are one suite, not a collection of separate products,
and they share one identity. A user signs in once, at `auth.<space>`, and is
that user in every app; an agent holds one token and it opens every app. No
app has its own users, its own login, or its own notion of who is asking.

One auth service per space provides that identity, and nginx enforces it for
every other app. nginx's `auth_request` module makes a subrequest to auth's
`/check` before it serves a routed request, forwarding only the original
request's `Cookie` and `Authorization` headers, and acts on the status that
comes back:

- **200**: auth returns `X-User-Id` and `X-User-Email`. nginx sets those two
  headers on the request it forwards to the app, stripping any `X-User-*`
  the client supplied, so the identity an app reads is always auth's.
- **401**: no accepted credential. nginx redirects the browser to
  `https://auth.<space>/?return=<original URL>`.
- **403**: a credential auth refuses. nginx passes the refusal through.

A credential is either the session cookie `ikigenba_session`, set by a
Google sign-in on auth's own hostname, or a bearer token `ikp_...` a user
minted from their profile. The cookie is set with `Domain=<space>`, so the
browser presents it to every app subdomain and one sign-in is the suite's
sign-in. Apps cannot tell the two credentials apart; they see a user id and
an email and nothing else. Auth's own server block carries no
`auth_request`, since it serves the sign-in page, and `/check` is reachable
only as nginx's internal subrequest, never from the public side.

The consequences for the rest of the platform:

- **Apps carry no authentication code.** An app trusts the two identity
  headers because nothing but nginx can reach its socket (above), and nginx
  sets them only from auth's answer.
- **Sessions and tokens live in one place.** Auth's SQLite database holds
  users, sessions, and tokens; it is replicated like any other (below).
- **The routing is a property of the space, not of auth.** `opsctl` generates
  the `auth_request` locations into every app's server block; auth is
  deployed like any app and only answers the subrequest.

Where this stands: the `auth` stories in this branch speak of a loopback
port and `PORT` in the environment. Under the Unix socket decision above,
auth listens on its socket like every other app, and nginx's subrequest
goes to that socket.

## Zero-downtime deploys via socket activation

An app's listening socket belongs to systemd, not to the app. An
`ikigenba-<app>.socket` unit creates the Unix socket, with its owner, group
and mode; the service unit inherits the already-open socket as a file
descriptor and calls `accept()` on it, never `listen()`.

This is what lets a deploy replace the process without a client-visible gap.
Restarting the service unit leaves the socket open under systemd, so while
the old process drains and the new one starts, the kernel completes incoming
connections and holds them in the socket's accept queue. Nothing is refused.
The new process drains the queue on its first `accept()`, and every queued
client is served, just a few hundred milliseconds late.

nginx needs no configuration for this. It proxies to the same socket path
before, during, and after the restart; every connect succeeds, so it has
nothing to retry or route around. The technique lives entirely in the app
and its unit files. It also removes the stale-socket-path problem an app
that binds its own Unix socket has to handle: systemd creates the path once
and the app never binds it.

What an app must do to take part:

- Read the inherited listener from `LISTEN_FDS` (file descriptor 3) instead
  of binding a socket itself. The code is the same as for a TCP listener;
  most languages hand back a generic listener either way.
- Treat `SIGTERM` as "finish in-flight requests, then exit". Stop calling
  `accept()`; do not close or unlink the listener, which is not the app's.
- Start fast. The accept queue is bounded by the socket unit's `Backlog`, and
  a queued connection counts against nginx's read timeout, so the window
  from old exit to new accept should stay well under a second.

A restart is always of the service unit. Restarting the socket unit closes
the socket and produces exactly the gap this design exists to remove.

Where this stands: the `opsctl` install design (D09) publishes the service
unit alone and restarts it on reinstall. Publishing the socket unit beside
it, and having apps take their listener from it, is the change this decision
calls for.

## Streaming database backups via litestream

Every SQLite database an app declares is replicated continuously to the
space's backup bucket by litestream, running as its own systemd service on
the host. It tails the database's WAL and ships each segment to S3 as it is
written, so the bucket holds a point-in-time history rather than periodic
snapshots. Restore rebuilds the database to a chosen moment from that
history; `devctl restore --at` is the operator's handle on it.

Two consequences follow from the host's write-only credentials
(`infra/AGENTS.md`, "Hosts write, never delete"):

- litestream runs with its own retention off. It cannot delete from the
  bucket, so it does not try; the bucket's lifecycle rule is the only expiry.
- A backup once written is beyond the reach of anything on the host,
  including a compromised app.

Files that are not the database, an app's `etc/` and `state/`, are not
streamed. They travel as tarballs on the timer `opsctl init` writes, and a
restore takes the newest tarball at or before the requested moment alongside
the rebuilt database. The two halves are therefore not the same instant; the
database is the one that is exact.
