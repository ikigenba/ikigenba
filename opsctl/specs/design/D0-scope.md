# D0-scope

Scope preamble for opsctl. This document carries no requirements and mints no
ids. It fixes the boundary the numbered design documents are authored inside
and records the decisions taken in discussion so the reasons survive.

## What opsctl is

Ikigenba is a PaaS for running internal apps. Its core platform services all
run on one Linux host. `opsctl` is the operations CLI for that host: a single
Go binary installed at `/usr/local/bin/opsctl`, run as root over ssh by humans
and by agents, that bootstraps the platform and manages it from then on. Every
piece of platform state it owns is a plain file the host already understands —
a JSON config file, an nginx config, systemd units — and every external
dependency is an ordinary binary on the PATH or a credential the host's
environment already provides.

The box knows only that it is a Linux server. It never learns which cloud it
runs in. AWS access (S3, Route 53) goes through the Go SDK's default
credential chain, which on the intended host resolves to an instance role,
and `opsctl` never reads instance metadata or assumes anything about it.

## Decisions taken in discussion

- **Root only.** `opsctl` refuses to run as any other user, loudly, before
  doing anything. Only help and version output are exempt.
- **Platform name on disk, tool name for the binary.** Config at
  `/etc/ikigenba/config.json`, nginx at `/etc/nginx/conf.d/ikigenba.conf`,
  units named `ikigenba-*` in `/etc/systemd/system/`. `opsctl` is only the
  name of the binary.
- **Config store.** A flat string-to-string map in one JSON file, keys
  matching `^[a-z0-9_.-]+$`, values without newlines. Written by atomic
  rename under an exclusive lock; a corrupt file is an error, never treated as
  empty. Verbs: `get`, `set KEY=VALUE`, `del`, `list`. Each later design
  declares the keys it reads.
- **Line-oriented output for agents.** stdout carries only the answer.
  Diagnostics go to stderr, one per line, prefixed with the program and
  command name. Exit codes are 0 success, 1 operation failed, 2 usage or
  preflight failure, 3 refused because not root — and the help text lists
  them. No JSON mode yet.
- **Idempotent commands, thin `init`.** Every setup command may be re-run
  safely. `init` is the documented sequence of the setup sub-commands behind
  one fail-fast preflight that lists every missing prerequisite at once.
- **Services are discovered, not registered.** A service is any
  `/opt/<name>/` holding an `etc/` or `state/` directory; `cache/` is never
  backed up. No registry file.
- **nginx is generated, never backed up.** The one file `opsctl` writes under
  `/etc/nginx` is a pure function of the config store plus service discovery.
  Port 80 redirects to 443; each zone gets a wildcard `server` that returns
  404; a catch-all rejects the TLS handshake for unknown names; services get
  hostname-routed `server` blocks later.
- **DNS goes through opsctl.** `opsctl` is the only thing on the box that
  writes DNS records, behind a provider seam with Route 53 as the first and
  only provider; one provider is active at a time. certbot obtains wildcard
  certificates per zone over DNS-01 using manual hooks that call `opsctl`.
- **Backup to S3 through the SDK.** A daily systemd timer runs `opsctl backup
  run`, which tars `/etc/ikigenba/` and `/etc/letsencrypt/` to the configured
  S3 prefix. `restore` does only the data step, warns if units are active,
  and never starts or stops anything. Litestream and per-service backup
  arrive with the first service that has a database.
- **Two hand-maintained documents.** `bootstrap.md` is platform-agnostic and
  ends when `ssh root@<zone>` works; `setup.md` takes that host through
  install, `config set`, and `init`. Neither is spec-governed.

## Deferred

`start`/`stop` of the whole platform, per-service commands and units,
Litestream and database backup, weekly full plus daily incremental file
backups, a `--json` output mode, additional DNS providers, self-update.
