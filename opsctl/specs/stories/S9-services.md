# Stories — services file

Every app's banner carries a launcher listing the host's services, and opsctl
owns the one file it is built from: `/var/lib/ikigenba/services.json`, owned
`root:ikigenba` with mode `0640`, in the directory `/var/lib/ikigenba/`, owned
`root:ikigenba` with mode `0750`, so every app, running as the user
`ikigenba`, can read it and none can write it. Like
`/etc/nginx/conf.d/ikigenba.conf` (`S5-nginx.md`), the file is a pure function
of what is on disk under `/opt`, of the configuration store's `host.name`, and
of which apps systemd reports disabled, so it is generated rather than edited,
and it is never backed up — a restored host regenerates it. It is written to a
temporary file in the same directory and renamed into place, so a reader never
sees a half-written file, and it is rewritten wherever nginx's configuration
is regenerated: `install`, `uninstall`, `enable`, `disable`, `restore`, `init`,
and `nginx apply`. The two files therefore never disagree about which apps are
disabled. It is written only after nginx's configuration has succeeded: a
command that fails before or at its nginx step — as every regeneration does
while a service's manifest cannot be read — leaves the file as it was. The file is always written, even when it lists nothing, so on a host
where opsctl has run `init` or `install` it never goes missing. An app finds
it through its environment: every app's `/opt/<app>/etc/env` holds the line
`IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` (`S7-apps.md`), whether or
not the app is itself in the launcher.

A service is in the launcher if and only if it is an installed, routed app —
`/opt/<name>/etc/manifest.toml` names its `app` and `/opt/<name>/bin/<name>`
exists — and its package shipped an icon, `/opt/<name>/share/icon.svg`. The
icon is the whole opt-in: the manifest carries no launcher key. The file is
one JSON object whose `services` array holds one object per launcher service,
in ascending name order, laid out exactly as the stories show it: the object
pretty-printed, one service per line. Readers depend only on the JSON value,
never on the layout.

- `name` — the service's name, the `app` its manifest names; it is all the
  tile shows.
- `url` — always `https://<name>.<host.name>`, for the default app and the
  apex app too, never the host's own name or the apex.
- `icon` — the contents of `share/icon.svg`, verbatim, as a JSON string.
- `enabled` — `false` when systemd reports the app's socket unit,
  `ikigenba-<name>.socket`, disabled, the same fact that makes nginx answer
  `503` for it; otherwise `true`.

The file has no version field, and its format only grows: a later opsctl may
add fields, and a reader ignores any field it does not know. A change an
existing reader could not survive never edits this file; it becomes a new
file with its own variable — `services.v2.json`, named by
`IKIGENBA_SERVICES_V2`, say — and opsctl writes both until no installed app
reads the old one.

## An app reads which services a host offers

`crm` and `dashboard` each shipped an icon, so both are in the file.
`dashboard` is the host's default app and `crm` holds the apex, and each
still has `https://<name>.<host.name>` as its `url`: the launcher links to a
service by its own name. `auth` is installed and routed but shipped no icon,
so it is left out: the authenticator is not somewhere a user goes on purpose.
Each icon is its file's bytes, the trailing newline included.

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
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n", "enabled": true },
    { "name": "dashboard", "url": "https://dashboard.sbx.ikigenba.dev", "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><rect width=\"20\" height=\"20\" x=\"2\" y=\"2\"/></svg>\n", "enabled": true }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `crm`.
- `/opt/auth/`, `/opt/crm/`, and `/opt/dashboard/` each hold an
  `etc/manifest.toml` naming their `app` and a `bin/<name>`, as `install`
  left them. `dashboard`'s manifest sets `default = true`.
- `/opt/crm/share/icon.svg` and `/opt/dashboard/share/icon.svg` hold the
  contents shown; `/opt/auth/share/icon.svg` does not exist.
- systemd reports every app's socket unit enabled.
- The file was last written by one of the commands that rewrite it, with the
  host in this state.
- The reader runs as `ikigenba` or as root.

Postconditions:

- Nothing has changed.

## An app reads the file while a launcher service is disabled

`crm` has been disabled with `opsctl disable crm`. It stays in the file, in
its place by name, with `enabled` false, so the launcher can show it as
unavailable rather than leave it out — the way nginx keeps its names and
answers `503`.

Command:

```
$ cat /var/lib/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n", "enabled": false },
    { "name": "dashboard", "url": "https://dashboard.sbx.ikigenba.dev", "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><rect width=\"20\" height=\"20\" x=\"2\" y=\"2\"/></svg>\n", "enabled": true }
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

## An app reads the file on a host with no launcher services

No installed app shipped an icon, so the array is empty. The file is still
there: an app reading it finds an empty launcher, never a missing file.

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
- No `/opt/<name>/` holds a `share/icon.svg`; or no directory under `/opt/`
  holds an `etc/` or a `state/` at all.
- `opsctl init` or `opsctl install` has run on the host.

Postconditions:

- Nothing has changed.

## An app reads the file where a service with an icon is not an installed app

An icon earns a place in the launcher only for an app a user can reach and
that is on the host to answer. `notes` has an icon and a manifest naming its
app, but no binary: nginx routes it, and `status` shows no version, but it is
not installed, so it is left out. `wiki` has an icon and no manifest naming
its app: it is not routed at all, so it is left out too. `crm` is as in the
first story.

Command:

```
$ cat /var/lib/ikigenba/services.json
```

Output:

```
{
  "services": [
    { "name": "crm", "url": "https://crm.sbx.ikigenba.dev", "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n", "enabled": true }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`.
- `/opt/crm/` is installed as in the first story, with its icon, and systemd
  reports `ikigenba-crm.socket` enabled.
- `/opt/notes/etc/manifest.toml` names `app = "notes"` and
  `/opt/notes/share/icon.svg` exists, but `/opt/notes/bin/notes` does not.
- `/opt/wiki/` holds a `state/` and a `share/icon.svg`, and no
  `etc/manifest.toml`.

Postconditions:

- Nothing has changed.
