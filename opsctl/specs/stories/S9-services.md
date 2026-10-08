# Stories — services file

The apps on a host learn which services it offers from one file opsctl owns:
`/var/lib/ikigenba/services.json`, owned `root:ikigenba` with mode `0640`, in
the directory `/var/lib/ikigenba/`, owned `root:ikigenba` with mode `0750`, so
every app, running as the user `ikigenba`, can read it and none can write it.
Like `/etc/nginx/conf.d/ikigenba.conf` (`S5-nginx.md`), the file is a pure
function of what is on disk under `/opt`, of the configuration store's
`host.name`, and of which apps systemd reports disabled, so it is generated
rather than edited, and it is never backed up — a restored host regenerates it.
It is written to a temporary file in the same directory and renamed into place,
so a reader never sees a half-written file, and it is rewritten wherever nginx's
configuration is regenerated: `install`, `uninstall`, `enable`, `disable`,
`restore`, `init`, and `nginx apply`. The two files therefore never disagree
about which apps are disabled. It is written only after nginx's configuration
has succeeded: a command that fails before or at its nginx step — as every
regeneration does while a service's manifest cannot be read — leaves the file
as it was. The file is always written, even when it lists nothing, so on a host
where opsctl has run `init` or `install` it never goes missing. An app finds it
through its environment: every app's `/etc/opt/ikigenba/<app>/env` holds the line
`IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` (`S7-apps.md`).

A service is in the file if and only if it is an installed, routed app —
`/opt/<name>/etc/manifest.toml` names its `app` and `/opt/<name>/bin/<name>`
exists. The file is one JSON object whose `services` array holds one object per
such service, in ascending name order, laid out exactly as the stories show it:
the object pretty-printed, one service per line, its members in the order
below. Readers depend only on the JSON value, never on the layout.

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
- `icon` — the contents of `/opt/<name>/share/icon.svg`, verbatim, as a JSON
  string, present only when the app's package shipped that file.

The launcher in every app's banner shows only the entries that carry an
`icon`: the icon is the whole launcher opt-in, and the manifest carries no
launcher key. A reader drawing the launcher skips an entry whose `icon` is not
a string.

The file has no version field, and its format only grows: a later opsctl may
add members, and a reader ignores any member it does not know. `name`, `url`,
`description`, `socket`, `enabled`, and `mcp` are in every entry; `icon` is
optional, and a reader treats its absence as no icon. A change an existing
reader could not survive never edits this file; it becomes a new file with its
own variable — `services.v2.json`, named by `IKIGENBA_SERVICES_V2`, say — and
opsctl writes both until no installed app reads the old one.

## An app reads which services a host offers

`auth`, `crm`, and `dashboard` are installed and routed, so all three are in
the file. `dashboard` is the host's default app and `crm` holds the apex, and
each still has `https://<name>.<host.name>` as its `url`: a reader names a
service by its own name. `auth` shipped no icon and its manifest sets neither
`description` nor `mcp`, so its entry has `""`, `false`, and no `icon`: it is
in the file but not in the launcher, since the authenticator is not somewhere a
user goes on purpose. `crm` describes itself and offers its tools to the MCP
catalog. Each icon is its file's bytes, the trailing newline included.

The manifests' relevant keys:

`/opt/auth/etc/manifest.toml`:

```toml
app = "auth"
```

`/opt/crm/etc/manifest.toml`:

```toml
app = "crm"
description = "Customers, contacts, and deals"
mcp = true
```

`/opt/dashboard/etc/manifest.toml`:

```toml
app = "dashboard"
default = true
```

`/opt/crm/share/icon.svg`:

```
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>
```

`/opt/dashboard/share/icon.svg`:

```
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect width="20" height="20" x="2" y="2"/></svg>
```

Command:

```
$ cat /var/lib/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "description": "Customers, contacts, and deals", "socket": "/run/ikigenba/crm.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n" },
    { "name": "dashboard", "url": "https://dashboard.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/dashboard.sock", "enabled": true, "mcp": false, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><rect width=\"20\" height=\"20\" x=\"2\" y=\"2\"/></svg>\n" }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `crm`.
- `/opt/auth/`, `/opt/crm/`, and `/opt/dashboard/` each hold an
  `etc/manifest.toml` with the keys shown and a `bin/<name>`, as `install`
  left them.
- `/opt/crm/share/icon.svg` and `/opt/dashboard/share/icon.svg` hold the
  contents shown; `/opt/auth/share/icon.svg` does not exist.
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
$ cat /var/lib/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "description": "Customers, contacts, and deals", "socket": "/run/ikigenba/crm.sock", "enabled": false, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n" },
    { "name": "dashboard", "url": "https://dashboard.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/dashboard.sock", "enabled": true, "mcp": false, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><rect width=\"20\" height=\"20\" x=\"2\" y=\"2\"/></svg>\n" }
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

## An app reads the file on a host with no installed apps

No app is installed and routed, so the array is empty. The file is still
there: an app reading it finds no services, never a missing file.

Command:

```
$ cat /var/lib/ikigenba/services.json
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
- No `/opt/<name>/` holds both an `etc/manifest.toml` naming its `app` and a
  `bin/<name>`; or no directory under `/opt/` holds an `etc/` and none under
  `/var/opt/ikigenba/` holds a `state/`.
- `opsctl init` or `opsctl install` has run on the host.

Postconditions:

- Nothing has changed.

## An app reads the file where a service is not an installed app

A service is in the file only when it is routed and on the host to answer.
`notes` has a manifest naming its app, but no binary: nginx routes it, and
`status` shows no version, but it is not installed, so it is left out, though
its manifest sets `mcp = true` and it has an icon. `wiki` has no manifest
naming its app: it is not routed at all, so it is left out too, though it has
an icon. `crm` is as in the first story.

Command:

```
$ cat /var/lib/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "description": "Customers, contacts, and deals", "socket": "/run/ikigenba/crm.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n" }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`.
- `/opt/crm/` is installed as in the first story, with its manifest and icon,
  and systemd reports `ikigenba-crm.socket` enabled.
- `/opt/notes/etc/manifest.toml` names `app = "notes"`, sets
  `description = "Shared notes"` and `mcp = true`, and
  `/opt/notes/share/icon.svg` exists, but `/opt/notes/bin/notes` does not.
- `/var/opt/ikigenba/wiki/` holds a `state/`, so `wiki` is a service, and
  `/opt/wiki/` holds a `share/icon.svg` and no `etc/`.

Postconditions:

- Nothing has changed.
