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

The designs cover command conventions and configuration, DNS and preflight,
nginx and certificates, app lifecycle, service and host backups, and restore
and retirement. Every app runs behind a systemd socket unit that holds its
Unix socket, which nginx proxies to, so no app listens on a TCP port and an
upgrade refuses no request; an operator can disable an app, which then stays
disabled through every operation until it is enabled. Release publication and the installer that puts the binary on
a host are maintained outside these designs. Setup composes certificate,
nginx, replication and timer operations. Generated files are reconstructed from
configuration and service declarations; application state outlives installation.

External programs and cloud access cross explicit dependency seams. The
requirements state the supported public boundary; facts about external tools
that still need observation, and choices that still need a human answer, are
working material kept outside the repository and put to the user while the
design is drafted. Nothing is settled by this overview.

## REQUIREMENTS

This document declares no requirements.
