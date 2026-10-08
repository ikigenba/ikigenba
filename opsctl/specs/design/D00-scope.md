# D00-scope

This is a reading guide to the current target. The numbered requirement lists
in the other designs are the contract; this overview introduces no separate
decisions and declares nothing itself.

opsctl operates one Linux host running one deployment of Ikigenba. Operators,
remote agents and host timers invoke its installed binary. The configuration
store describes that host, while service discovery reads what is on its disk.
The topology is one root domain shared by every space, a host per space named
one label under that root, and at most one of those hosts holding the apex:
the app it names answers at the root as well as under the host's own name.

The suite reaches a host as one release, a folder under
`/opt/ikigenba/releases/` named by the commit sha it was built from, and
`activate` makes it the one the host runs; `current` and `previous` links are
the only record of what runs, and `rollback` goes one step back (D16 to D18).
opsctl belongs to the release it was unpacked with and reports that release as
its version. A host that has activated a release is a released host; one still
in the legacy layout, each app under `/opt/<app>/`, which the first activate
cuts over, is a per-app host; one with neither is fresh (D08). The designs cover command
conventions and configuration, DNS and preflight, nginx and certificates, the
release layout, activate and rollback, the legacy per-app host, app lifecycle, service and host
backups, and restore and retirement. Every app runs behind a systemd socket
unit that holds its Unix socket, which nginx proxies to, so no app listens on
a TCP port and a restart refuses no request; an operator can disable an app,
which then stays disabled through every operation until it is enabled.
Building and unpacking a release are devctl's. Setup composes certificate,
nginx, replication and timer operations. Generated files, an app's
environment file and units among them, are reconstructed from configuration,
the current release and service declarations; application state outlives
every release. One generated file, the services file, lists the host's
services for the launcher in every app's banner and for the suite's MCP
catalog; it is rewritten wherever the nginx configuration is and is never
backed up (D15).

External programs and cloud access cross explicit dependency seams. The
requirements state the supported public boundary; facts about external tools
that still need observation, and choices that still need a human answer, are
working material kept outside the repository and put to the user while the
design is drafted. Nothing is settled by this overview.

## REQUIREMENTS

This document declares no requirements.
