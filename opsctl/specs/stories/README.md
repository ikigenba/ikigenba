# stories/

User stories: the intent opsctl is built to serve. Each file is one group of
related stories. A story says who wants what, the preconditions on the host,
the exact command, what each option does, and the postconditions once the
command has run.

opsctl is run on one host, as root, over ssh, by an operator at a terminal and
by an agent — `devctl` from a developer's machine, or a systemd timer on the
host itself. Every story names which. A host runs one complete deployment of
the platform and knows nothing about any other host.

Stories are the input to `specs/design/`. A design realises a group of stories
and assigns it requirement ids. Stories carry no requirement ids. They are
never deleted, designed or not, and when a design changes its stories are
updated to match.

Groups, in the order they are meant to be designed:

1. `bootstrap.md` — running opsctl at all.
2. `config.md` — the host configuration store.
3. `dns.md` — `dns list`, `add`, `remove`, `check`, and the certbot hooks.
4. `init.md` — the preflight and the setup sequence.
5. `nginx.md` — the one nginx file opsctl generates.
6. `certificates.md` — the host's wildcard certificate.
7. `apps.md` — putting an app on the host, taking it off, restarting it, and reading what is on it.
8. `backup.md` — backing up the host and its services, and restoring them.
9. `release.md` — getting opsctl onto a host in the first place.

Each group states the line it adds to the top-level usage text under
`Commands:`; `bootstrap.md` carries the frame with `version` alone. Each also
declares the configuration keys it introduces, and only those; a command's
help text lists every key that command reads, including keys another group
declared.
