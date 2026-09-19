# D00-scope

This is a reading guide to the current target. The numbered requirement lists
are the contract; this overview introduces no separate decisions.

opsctl operates one Linux host running one deployment of Ikigenba. Operators,
remote agents and host timers invoke its installed binary. The configuration
store describes that host, while service discovery reads what is on its disk.

The designs cover command conventions and configuration, DNS and preflight,
nginx and certificates, app lifecycle, service and host backups, and restore
and retirement. Release publication and the installer that puts the binary on
a host are maintained outside these designs. Setup composes certificate,
nginx, replication and timer operations. Generated files are reconstructed from
configuration and service declarations; application state outlives installation.

External programs and cloud access cross explicit dependency seams. The design
and its review evidence distinguish the supported public boundary from facts
about external tools that still need observation and choices that still need a
human answer. Unresolved questions live under `specs/issues/`; none is settled
by this overview.
