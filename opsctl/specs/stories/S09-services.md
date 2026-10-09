# Stories — services file

The apps on a host learn which services it offers from one file opsctl owns:
`/run/ikigenba/services.json`, owned `root:ikigenba` with mode `0640`, so every
app, running as the user `ikigenba`, can read it and none can write it. Like
`/etc/nginx/conf.d/ikigenba.conf` (`S05-nginx.md`), the file is a pure function
of the release `/opt/ikigenba/current` names, of the configuration store's
`host.name`, and of which apps systemd reports disabled, so it is generated
rather than edited, and it is never backed up — a restored host regenerates it.
It is written to a temporary file in the same directory and renamed into place,
so a reader never sees a half-written file, and on a released host it is
rewritten wherever nginx's configuration is regenerated: `activate`,
`rollback`, `enable`, `disable`, `restore`, `init`, and `nginx apply`. The two files therefore never disagree
about which apps are disabled. It is written only after nginx's configuration
has succeeded: a command that fails before or at its nginx step leaves the file
as it was. `opsctl services apply` writes the file alone, from the same inputs,
and touches nothing else. The file is always written, even when it lists
nothing.

`/run` is emptied when the host restarts, so the file is put back at boot
with no operator step: `activate`'s `units` step (`S10-releases.md`) writes
and enables `ikigenba-services.service`, a oneshot unit that runs
`/usr/local/bin/opsctl services apply` and is ordered before every app's
service. On a host where `activate` has run, the file is there whenever an app
runs. An app finds it through its environment: every app's
`/etc/opt/ikigenba/<app>/env` holds the line
`IKIGENBA_SERVICES=/run/ikigenba/services.json`.

The top-level usage gains the line `  services  regenerate the services file`
under `Commands:`.

These stories are written for a released host, one where `/opt/ikigenba/current`
exists. A host still laid out per app (`S07-apps.md`) keeps that layout's file,
`/var/lib/ikigenba/services.json`, with its own rule — every installed, routed
app under `/opt/<name>/` — and named by its apps' `IKIGENBA_SERVICES`.

A service is in the file if and only if it is an app the release `current`
names: a directory `/opt/ikigenba/current/<name>/` holding `bin/<name>` and an
`etc/manifest.toml` that names its `app`. A data-only service — one that only
keeps a `state/` under `/var/opt/ikigenba/<name>/`, because the current
release holds no such app — is not in the file, and an `/opt/<name>/` puts
nothing in it. The file is one JSON object whose `services` array holds one
object per such service, in ascending name order, laid out exactly as the
stories show it: the object pretty-printed, one service per line, its members
in the order below. Readers depend only on the JSON value, never on the
layout.

- `name` — the service's name, the `app` its manifest names; it is all a
  launcher tile shows.
- `url` — always `https://<name>.<host.name>`, for the default app and the
  apex app too, never the host's own name or the apex.
- `description` — the manifest's `description`, verbatim, as a JSON string;
  `""` when the manifest has none.
- `socket` — always `/run/ikigenba/<name>.sock`, the socket nginx sends the
  service's requests to, whether or not the app is disabled.
- `enabled` — `false` when systemd reports the app's socket unit,
  `ikigenba-<name>.socket`, disabled, the same fact that makes nginx answer
  `503` for it; otherwise `true`.
- `mcp` — the manifest's `mcp`; `false` when the manifest has none. `true`
  means the service's tools belong in the suite's MCP catalog.
- `icon` — the contents of `/opt/ikigenba/current/<name>/share/icon.svg`,
  verbatim, as a JSON string, present only when the release ships that file for
  the app.
- `group` — the manifest's `[home]` `group`, `"core"` or `"application"`;
  `"application"` when the manifest has no `[home]` table or no `group` in it
  (see `S07-apps.md`). It is the last member of every entry, after `icon` when
  there is one.

The launcher in every app's banner shows only the entries that carry an
`icon`: the icon is the whole launcher opt-in, and the manifest carries no
launcher key. A reader drawing the launcher skips an entry whose `icon` is not
a string.

The file has no version field, and its format only grows: a later opsctl may
add members, and a reader ignores any member it does not know. `name`, `url`,
`description`, `socket`, `enabled`, `mcp`, and `group` are in every entry;
`icon` is optional, and a reader treats its absence as no icon. A change an
existing reader could not survive never edits this file; it becomes a new file
with its own variable — `services.v2.json`, named by `IKIGENBA_SERVICES_V2`,
say — and opsctl writes both until no app in the release reads the old one.

## An operator asks what `services` can do

Command:

```
$ opsctl services --help
```

```
$ opsctl services -h
```

Output:

```
Usage: opsctl services <subcommand>

Generate /run/ikigenba/services.json, the services file every app reads
through IKIGENBA_SERVICES, from the release /opt/ikigenba/current names,
host.name, and which apps systemd reports disabled. The file is generated,
never edited.

Subcommands:
  apply  write the file; with no current release, write an empty list

Configuration keys:
  host.name  the fully-qualified name this host answers at

ikigenba-services.service runs 'opsctl services apply' at boot, before any
app starts, so the file is there again after the host restarts.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An app reads which services a host offers

`auth`, `crm`, and `dashboard` are the apps of the current release, so all
three are in the file. `dashboard` is the host's default app and `crm` holds
the apex, and each still has `https://<name>.<host.name>` as its `url`: a
reader names a service by its own name. `auth` ships no icon and its manifest
sets neither `description` nor `mcp`, so its entry has `""`, `false`, and no
`icon`: it is in the file but not in the launcher, since the authenticator is
not somewhere a user goes on purpose. `auth` is the one core service of the
three, so its entry's `group` is `"core"`; `crm` and `dashboard` carry no
`[home]` table, so theirs is `"application"`. `crm` describes itself and
offers its tools to the MCP catalog. Each icon is its file's bytes, the
trailing newline included.

The manifests' relevant keys:

`/opt/ikigenba/current/auth/etc/manifest.toml`:

```toml
app = "auth"

[home]
group = "core"
```

`/opt/ikigenba/current/crm/etc/manifest.toml`:

```toml
app = "crm"
description = "Customers, contacts, and deals"
mcp = true
```

`/opt/ikigenba/current/dashboard/etc/manifest.toml`:

```toml
app = "dashboard"
default = true
```

`/opt/ikigenba/current/crm/share/icon.svg`:

```
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>
```

`/opt/ikigenba/current/dashboard/share/icon.svg`:

```
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect width="20" height="20" x="2" y="2"/></svg>
```

Command:

```
$ cat /run/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false, "group": "core" },
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "description": "Customers, contacts, and deals", "socket": "/run/ikigenba/crm.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n", "group": "application" },
    { "name": "dashboard", "url": "https://dashboard.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/dashboard.sock", "enabled": true, "mcp": false, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><rect width=\"20\" height=\"20\" x=\"2\" y=\"2\"/></svg>\n", "group": "application" }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `crm`.
- `/opt/ikigenba/current` is a link to
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, whose apps
  are `auth`, `crm`, and `dashboard`, each with an `etc/manifest.toml` holding
  the keys shown and a `bin/<name>`.
- `/opt/ikigenba/current/crm/share/icon.svg` and
  `/opt/ikigenba/current/dashboard/share/icon.svg` hold the contents shown;
  `/opt/ikigenba/current/auth/share/icon.svg` does not exist.
- systemd reports every app's socket unit enabled.
- The file was last written by one of the commands that rewrite it, with the
  host in this state.
- The reader runs as `ikigenba` or as root.

Postconditions:

- Nothing has changed.

## An app reads the file while a service is disabled

`crm` has been disabled with `opsctl disable crm`. It stays in the file, in its
place by name, with `enabled` false and every other member as before, its
`socket` included, so the launcher can show it as unavailable rather than leave
it out — the way nginx keeps its names and answers `503`.

Command:

```
$ cat /run/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false, "group": "core" },
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "description": "Customers, contacts, and deals", "socket": "/run/ikigenba/crm.sock", "enabled": false, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n", "group": "application" },
    { "name": "dashboard", "url": "https://dashboard.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/dashboard.sock", "enabled": true, "mcp": false, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><rect width=\"20\" height=\"20\" x=\"2\" y=\"2\"/></svg>\n", "group": "application" }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- As for the previous story, and systemd reports `ikigenba-crm.socket`
  disabled.
- The file was last written by `opsctl disable crm` or a later command that
  rewrites it.

Postconditions:

- Nothing has changed.

## An app finds the services file through its environment

An app is never told the path any other way: it reads the variable its
environment file sets, the same line for every app in the release, launcher
service or not.

Command:

```
$ sudo grep IKIGENBA_SERVICES /etc/opt/ikigenba/crm/env
```

Output:

```
IKIGENBA_SERVICES=/run/ikigenba/services.json
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm` is an app of the current release, and its environment file was last
  written by `activate`, `rollback`, `init`, or `restore`.

Postconditions:

- Nothing has changed.

## An app reads the file on a host with no current release

A fresh host that has run `init`, or any host with no `current` where
`services apply` has run, lists nothing. The file is still there: a reader
finds no services, never a missing file.

Command:

```
$ cat /run/ikigenba/services.json
```

Output:

```
{
  "services": []
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`.
- `/opt/ikigenba/current` does not exist.
- The host is a fresh host and `opsctl init` has run, or `opsctl services
  apply` has run, since the host last started.

Postconditions:

- Nothing has changed.

## An app reads the file where a service is not in the current release

A service is in the file only when the current release holds its app. `gmail`
was an app of an earlier release; the current one does not hold it, and its
kept `state/` makes it a data-only service, so it is left out, though the
host still backs its data up. `wiki` is left behind under `/opt/wiki/` with a
manifest naming its app and an icon, from the time the host was laid out per
app; on a released host an `/opt/<name>/` makes no service, so it is left out
too. `crm` is as in the first story.

Command:

```
$ cat /run/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "description": "Customers, contacts, and deals", "socket": "/run/ikigenba/crm.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n", "group": "application" }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`.
- The current release's only app is `crm`, with its manifest and icon as in
  the first story, and systemd reports `ikigenba-crm.socket` enabled.
- `/var/opt/ikigenba/gmail/` holds a `state/`, and
  `/opt/ikigenba/current/gmail/` does not exist.
- `/opt/wiki/` holds `bin/wiki`, an `etc/manifest.toml` naming
  `app = "wiki"`, and a `share/icon.svg`, and `/opt/ikigenba/current/wiki/`
  does not exist.

Postconditions:

- Nothing has changed.

## An operator regenerates the services file

`services apply` writes the file from what is on the host now, without
regenerating nginx or touching any app. Nothing is printed; the answer is the
exit code.

Command:

```
$ sudo opsctl services apply
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- The host is as in the first story.
- `opsctl` is running as root.

Postconditions:

- `/run/ikigenba/services.json` holds the file of the first story, owned
  `root:ikigenba` with mode `0640`, written to a temporary file in the same
  directory and renamed into place.
- `/etc/nginx/conf.d/ikigenba.conf` was not written and nginx was not
  reloaded; no app's unit or environment file changed and no app was started,
  stopped, or restarted.
- Applying again with nothing else changed writes the same bytes and exits 0.

## An operator regenerates the services file on a host with no current release

With no release to read, `services apply` writes the empty list rather than
refusing, so a reader still finds a file. This holds on a fresh host and on a
per-app host alike; a per-app host's own `/var/lib/ikigenba/services.json` is
not its to write.

Command:

```
$ sudo opsctl services apply
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`.
- `/opt/ikigenba/current` does not exist: the host is a fresh host or a
  per-app host.
- `opsctl` is running as root.

Postconditions:

- `/run/ikigenba/services.json` holds the file of the story above with no
  current release, owned `root:ikigenba` with mode `0640`.
- `/var/lib/ikigenba/services.json`, where a per-app host has one, is
  untouched.

## An operator restarts the host

`/run` comes back empty after a restart, and every app reads the services
file as soon as it starts. `ikigenba-services.service` runs at boot, before
any app's service, so each app finds the file there, with no step from the
operator. The file is the same as before the restart, since nothing it is
made from changed.

Command:

```
$ sudo systemctl reboot
$ cat /run/ikigenba/services.json
```

Output: the file of the first story.

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The host is as in the first story, and `activate` has written and enabled
  `ikigenba-services.service`, whose command is
  `/usr/local/bin/opsctl services apply`.
- `/usr/local/bin/opsctl` is a link to
  `/opt/ikigenba/current/opsctl/bin/opsctl`.

Postconditions:

- `ikigenba-services.service` ran once at boot and exited successfully
  before any `ikigenba-<app>.service` started.
- `/run/ikigenba/services.json` exists, owned `root:ikigenba` with mode
  `0640`, holding the file of the first story.

## An agent regenerates the services file on a host with no host.name

Every entry's `url` is made from `host.name`, so with none set there is no
file to write, and `services apply` refuses as `nginx apply` does.

Command:

```
$ sudo opsctl services apply
```

Output:

```
opsctl: host.name not set
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `host.name` is unset or empty.
- `opsctl` is running as root.

Postconditions:

- Nothing has changed. `/run/ikigenba/services.json` is as it was, or still
  absent.

## An operator gives services apply an argument

`apply` has nothing to name or choose; it always writes the whole file.

Command:

```
$ sudo opsctl services apply crm
```

Output:

```
opsctl: services apply takes no arguments

see 'opsctl services --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed. The file was not written.

## An operator runs `services` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl services
```

Output:

```
opsctl: no services subcommand given

see 'opsctl services --help' for usage
```

Command:

```
$ sudo opsctl services show
```

Output:

```
opsctl: unknown services subcommand 'show'

see 'opsctl services --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
