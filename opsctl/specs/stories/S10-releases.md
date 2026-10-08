# Stories — releases

A suite release reaches a host as one folder, `/opt/ikigenba/releases/<sha>/`, named by the full 40-character lowercase commit sha it was built from: `release.json` (`{"sha":...,"built":...,"devctl":...}`), `opsctl/bin/opsctl`, and for each app `<app>/` holding only `bin/`, `libexec/`, `lib/`, `share/` and `etc/`, with `bin/` holding exactly `bin/<app>` and `etc/` the app's `manifest.toml` and its `nginx.conf*`. Unpacking the folder is devctl's; `opsctl activate <sha> [label]`, run by the opsctl inside that folder, makes it the release the host runs, and `opsctl rollback` goes back to the one before. Two symlinks beside `releases/` are the only record of what the host runs: `/opt/ikigenba/current` names the running release's folder and `/opt/ikigenba/previous` the one before it, and nothing else on the host or anywhere else says which release is running. `/usr/local/bin/opsctl` is a symlink to `/opt/ikigenba/current/opsctl/bin/opsctl`, so plain `opsctl` is always current's. A release's label lives in `releases/<sha>/label`, present only when the last activate of that sha gave one; a release is shown as `r142 (c604e32)` with a label and `c604e32` without, the short sha being the first 7 characters. Both commands report in install's step-line form: one `<step>: ok (<detail>)` line on stdout per step that succeeded, or `<step>: failed: <reason>` for the one that did not, after which nothing more runs, `opsctl: activate failed` (or `rollback failed`) is on stderr, with the journal quoted `> ` after an empty line when a service failed, and the exit is 1. Every activate checks before it changes anything: the release tree, the host's layout, every manifest, every app's secrets and the slices' room for the release; then it writes the label, every app's environment file and units, points `previous` at what `current` named and `current` at the new folder, reloads systemd, regenerates nginx, `/run/ikigenba/services.json` and `/etc/litestream.yml`, restarts every app one at a time, core apps first and each `active` before the next is touched, and, only when all of that succeeded, removes the releases neither link names.

The top-level usage gains two lines under `Commands:`:

```
  activate  make an unpacked release the one this host runs
  rollback  go back to the release this host ran before
```

The steps, in order, and what each reports on success:

| step | detail | what it does |
|---|---|---|
| `release` | `c604e32, r142`, or `c604e32` with no label | checks the release tree, then makes the folder owned by root and not writable by group or other |
| `layout` | `releases`, `per app; cutover of 8 apps`, or `fresh host` | says which kind of host this is, and refuses one that cannot take a release |
| `manifests` | `8 apps` | every app's manifest is valid by install's rules |
| `secrets` | `2 keys` | every name every manifest lists is in `/<host.name>/<app>`; the count is across all apps |
| `resources` | `8 apps` | install's slice and memory checks for the whole release, with install's `; warning: ...` clauses |
| `snapshot` | `8 services` | cutover only: snapshots every service, as `opsctl snapshot` does |
| `cutover` | `removed 8 apps from /opt` | cutover only: removes the per-app units, `/opt/<app>/` and `/var/lib/ikigenba/services.json` |
| `label` | `r142`, or `none` | writes `releases/<sha>/label`, or removes it |
| `env` | `8 apps` | writes `/etc/opt/ikigenba/<app>/env` for every app in the release |
| `units` | `8 apps` | creates `/var/opt/ikigenba/<app>/`, owned `ikigenba:ikigenba` with mode `0750`, for an app that has none, as install's `data` step does; writes both units of every app and `ikigenba-services.service`; stops and removes an app the release lacks |
| `links` | `current c604e32, previous 1a2b3c4` | moves `previous` then `current`, then re-creates `/usr/local/bin/opsctl` as a link to `/opt/ikigenba/current/opsctl/bin/opsctl` every time, replacing a file or a link to anything else |
| `systemd` | `daemon-reload` | reloads systemd |
| `nginx` | `8 apps` | regenerates `/etc/nginx/conf.d/ikigenba.conf` from current and reloads nginx |
| `services` | `8 services` | writes `/run/ikigenba/services.json` from current |
| `litestream` | `updated`, or `unchanged` | regenerates `/etc/litestream.yml` from current's manifests, restarting `litestream.service` only when it changed |
| `service` | `auth c604e32 active`, one line per app | restarts the app, or starts it if inactive, and waits for it to be `active`; a disabled app is not started and reads `disabled` |
| `retention` | `removed 1 release`, or `nothing removed` | removes every folder in `releases/` named by a full sha that neither link names; leaves any other entry |

The `release`, `layout`, `manifests`, `secrets` and `resources` steps write nothing on the host but the release folder's own ownership and modes. The `service` lines run core apps (manifest `slice = "core"`) first, then the rest, in name order within each. On a released host each app's units are `/etc/systemd/system/ikigenba-<app>.socket`, listening at `/run/ikigenba/<app>.sock` as install writes it, and `/etc/systemd/system/ikigenba-<app>.service`, running `/opt/ikigenba/current/<app>/bin/<app>` with working directory `/var/opt/ikigenba/<app>`, environment file `/etc/opt/ikigenba/<app>/env`, a runtime directory `/run/ikigenba/<app>/` and a private `/tmp`, and everything else (slice, resources, stop timeout, user) as install writes it. Its environment file holds what install writes (each secret, the manifest's `[env]`, `DRAIN_SECONDS`) with `IKIGENBA_SERVICES=/run/ikigenba/services.json`, `IKIGENBA_COMMIT=<full sha>`, and `IKIGENBA_RELEASE=<label>` only when current's release has a label. `ikigenba-services.service` is a oneshot that runs `/usr/local/bin/opsctl services apply` at boot, ordered before every app's service (`S09-services.md`). Since the units name `current` rather than a release, moving the link is what changes the binary each app runs; the restart is what makes it run.

The stories use the example host `sbx.ikigenba.dev` and three releases: `c604e32` (`c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, label `r142`), `1a2b3c4` (`1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`, label `r141`) and `9f8e7d6` (`9f8e7d6c5b4a39281706f5e4d3c2b1a098765432`). Each release holds the suite's eight apps, `auth`, `telemetry`, `mcp`, `repos`, `sites`, `scripts`, `events` and `dummy`, with the manifests the suite ships: `auth` and `telemetry` are core apps; `auth` lists two secrets and no other app lists any; every app but `mcp` declares a database. Unless a story says otherwise, every app is enabled, `/<host.name>/auth` holds both of `auth`'s secrets, and `host.name`, `aws.region` and `backup.s3_uri` are set.

## An operator asks what `activate` can do

Command:

```
$ opsctl activate --help
```

```
$ opsctl activate -h
```

Output:

```
Usage: opsctl activate SHA [LABEL]

Make the release unpacked at /opt/ikigenba/releases/SHA/ the one this host
runs. SHA is the full 40-character commit sha. LABEL, one token of letters,
digits, '.', '_', '/' and '-', is written to releases/SHA/label and given to
every app as IKIGENBA_RELEASE; without LABEL the label file is removed.
Must be run by the opsctl inside that release,
/opt/ikigenba/releases/SHA/opsctl/bin/opsctl.

Every check runs before anything outside the release changes: the release
tree, the host's layout, every app's manifest, its secrets in
/<host.name>/<app>, and the slices' room for the release. Then every app's
environment file and units are written, previous is pointed at what current
named and current at the release, nginx, /run/ikigenba/services.json and
/etc/litestream.yml are regenerated, and every app is restarted one at a
time, core apps first, each active before the next. A disabled app stays
disabled and is not started. An app the host runs that the release lacks is
stopped and its units removed; its data under /var/opt/ikigenba/APP/ is kept.
The first failure stops the run. After a run that succeeds, every release
neither current nor previous names is removed.

On a host whose apps were installed one by one, every service is snapshotted
and the apps' units and /opt/APP/ removed before the release's are written.
Activating the release current already names redoes everything but moving
current and previous.

Configuration keys:
  aws.region          the region this host's parameters live in
  host.name           the fully-qualified name this host answers at
  host.apex           the app that answers at the parent of host.name; unset means none
  backup.s3_uri       the prefix the cutover's snapshots are written under
  apps.drain_seconds  how long an app may drain when stopped (default 5)
  apps.stop_seconds   how long systemd waits for an app to stop (default 10)
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is on the host.

Postconditions:

- Nothing has changed.

## An agent activates a release on a host that runs releases

`devctl` has unpacked `c604e32` beside the release the host runs and now runs that release's own opsctl by its absolute path, because `/usr/local/bin/opsctl` is still the old release's. Every check passes, every app is restarted onto the new release, and the release that was `previous` before the run is removed because neither link names it any more.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (removed 1 release)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` names `releases/9f8e7d6c5b4a39281706f5e4d3c2b1a098765432`; every app runs `1a2b3c4` and is `active`.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` is unpacked whole, and its `release.json` names `c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`.
- The two releases' manifests declare the same databases at the same paths.
- `init` has reported the host ready.

Postconditions:

- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` and everything in it is owned by root and writable by neither group nor other, and holds `label` with the text `r142`.
- `/opt/ikigenba/current` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18` and `/opt/ikigenba/previous` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`. `previous` was moved before `current`. `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl`, so `opsctl version` prints `r142 (c604e32)`.
- `releases/9f8e7d6c5b4a39281706f5e4d3c2b1a098765432/` is gone; `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d/` is untouched.
- Every app's `/etc/opt/ikigenba/<app>/env` was written whole, owned `root:root` with mode `0600`, and holds what install writes, `IKIGENBA_SERVICES=/run/ikigenba/services.json`, `IKIGENBA_COMMIT=c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18` and `IKIGENBA_RELEASE=r142`.
- Every app's two units are as the group describes and are enabled; `ikigenba-services.service` is written and enabled. systemd was reloaded.
- `/etc/nginx/conf.d/ikigenba.conf` was regenerated from current, including each `/opt/ikigenba/current/<app>/etc/nginx.conf*`, and nginx reloaded. `/run/ikigenba/services.json` lists the eight apps, read from current, owned `root:ikigenba` with mode `0640`.
- `/etc/litestream.yml` is byte for byte as it was, and `litestream.service` was not restarted.
- Every app's service was restarted once, in the order the lines show, each `active` before the next was restarted; no socket was stopped, so a request that arrived during an app's restart waited and was answered by the new process. `status` shows `auth c604e32 r142 active active wal`, and the same for every app with a database.
- `/var/opt/ikigenba/<app>/` is untouched for every app.

## An agent activates a release without a label

A release that is not given a label is shown by its short sha alone, everywhere: the `release` and `label` lines, `opsctl version`, and every app's banner, because no app is given `IKIGENBA_RELEASE`.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18
```

Output:

```
release: ok (c604e32)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (none)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`, there is no `/opt/ikigenba/previous`, and `releases/` holds only that folder and `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/`, unpacked whole.

Postconditions:

- Everything the previous story's postconditions say, except: `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` holds no `label`, no app's environment file holds an `IKIGENBA_RELEASE` line, `opsctl version` prints `c604e32`, `status` shows `auth c604e32 - active active wal`, and no release was removed.

## An agent moves a host from per-app installs to releases

The first activate on a host whose apps were installed one by one is the cutover. Each app's data is already at `/var/opt/ikigenba/<app>/` and its environment file at `/etc/opt/ikigenba/<app>/env`, where a release expects them, so nothing has to move: what goes is what each install brought, `/opt/<app>/` and the units that run it. Before anything goes, every service is snapshotted, so the host's data is in one set of objects that `opsctl restore --from` can put back whatever happens next. The cutover step notes which apps are disabled and stops nothing; each app keeps serving from its old process until the rolling restart replaces it. `/usr/local/bin/opsctl` is a file the per-app host installed, and the `links` step replaces it with the link.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (per app; cutover of 8 apps)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
snapshot: ok (8 services)
cutover: ok (removed 8 apps from /opt)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous none)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 disabled)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host is a per-app host: there is no `/opt/ikigenba/current`; each of the eight apps was installed by `opsctl install`, so `/opt/<app>/` holds `bin/`, `etc/` and `share/`, and its units run `/opt/<app>/bin/<app>`. `/usr/local/bin/opsctl` is a regular file. `host.apex` is set.
- Every app's `state/` is under `/var/opt/ikigenba/<app>/` and its environment file is `/etc/opt/ikigenba/<app>/env`; no `/opt/<app>/` holds `state/` or `etc/env`.
- `dummy` is disabled; every other app is `active`.
- litestream has replicated every declared database under `<backup.s3_uri><app>/`, and the host's role can write under `<backup.s3_uri>snapshots/`.
- `init` has reported the host ready, and `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` is unpacked whole.

Postconditions:

- `<backup.s3_uri>snapshots/<service>/<timestamp>.tar.zst` was written for each of the eight services, all with one timestamp, as `opsctl snapshot` writes them, before anything on the host was removed.
- No `/opt/<app>/` exists for any of the eight apps, `/var/lib/ikigenba/services.json` is gone, and no unit or nginx configuration names a path under `/opt/<app>/`.
- The regular file `/usr/local/bin/opsctl` was replaced by a link to `/opt/ikigenba/current/opsctl/bin/opsctl`. `/opt/ikigenba/current` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, and there is no `/opt/ikigenba/previous`.
- Every app's environment file, units, nginx block, services entry and the rest are as "An agent activates a release on a host that runs releases" leaves them.
- `dummy`'s units are written and still disabled and inactive; it was not started, its names answer `503`, and its entry in `/run/ikigenba/services.json` is `"enabled": false`.
- `/var/opt/ikigenba/<app>/` is untouched for every app, and `/etc/litestream.yml` is byte for byte as it was, so replication was never interrupted.
- Every enabled app's service was restarted onto `/opt/ikigenba/current/<app>/bin/<app>`, core apps first, each `active` before the next.

## An agent activates the first release on a fresh host

A fresh host has neither a release nor per-app installs. Bringing it up is three commands, all by the absolute path of the release's own opsctl, because `/usr/local/bin/opsctl` does not exist until `init` makes it: `opsctl config set` for each key the space needs (`S02-config.md`), `opsctl init` for the host's own setup (`S04-init.md`), and then this activate. `init` made `/usr/local/bin/opsctl` a link to the opsctl that ran it, so certbot's hooks could reach `opsctl dns`; `activate` replaces it with the link to current's.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (fresh host)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous none)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (updated)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The packages the host's first boot installs are there, the space's secrets are in SSM, and DNS is delegated.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` is unpacked whole. There is no `/opt/ikigenba/current`, no `/opt/ikigenba/previous`, no app under `/opt/<app>/` and no app unit.
- The keys were set and `init` ran, both by `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl`, and `init` reported the host ready; the account `ikigenba` exists, which `init` created (`S04-init.md`), and `/usr/local/bin/opsctl` is a symlink to that executable.

Postconditions:

- `/opt/ikigenba/current` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, there is no `previous`, and `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl`, replacing `init`'s link to the release's opsctl by its own path.
- `/var/opt/ikigenba/<app>/` exists for every app, owned `ikigenba:ikigenba` with mode `0750`, holding only what the app created when it started; every app started with an empty database.
- `/etc/litestream.yml` names the seven declared databases and `litestream.service` was restarted.
- Everything else is as "An agent activates a release on a host that runs releases" leaves it, and `https://auth.sbx.ikigenba.dev` and every other app's name answer.

## An agent activates the release the host already runs, with a label

Activating the sha `current` already names is how a release is relabelled, and how a host whose environment or units have drifted is put right. Everything is redone except moving `current` and `previous`: `previous` never ends up naming the same release as `current`. Because `/usr/local/bin/opsctl` resolves to current's opsctl, which is the one inside this release, plain `opsctl` may run it.

Command:

```
$ sudo opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4 kept)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty. On a host with no `previous` the line reads `links: ok (current c604e32, previous none)`.

Preconditions:

- `/opt/ikigenba/current` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18` and `/opt/ikigenba/previous` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`; `releases/` holds nothing else.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/label` holds `r141` or does not exist.

Postconditions:

- `current` and `previous` are as they were; neither was rewritten. `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl` all the same, so a file or a link to anything else left there was replaced.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/label` holds `r142`, every app's environment file holds `IKIGENBA_RELEASE=r142`, and `opsctl version` prints `r142 (c604e32)`.
- Every environment file and unit was written whole again, nginx, `/run/ikigenba/services.json` and `/etc/litestream.yml` were regenerated, and every app was restarted once, as on any activate.

## An agent activates the release the host already runs, without a label

The label file reflects the latest activate, so activating current's sha with no label takes the label away.

Command:

```
$ sudo opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18
```

Output:

```
release: ok (c604e32)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (none)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4 kept)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for the previous story, and `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/label` holds `r142`.

Postconditions:

- `current` and `previous` are as they were, and `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl`.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/label` does not exist, no app's environment file holds an `IKIGENBA_RELEASE` line, and `opsctl version` prints `c604e32`.
- Everything else was redone as in the previous story.

## An agent activates the release the host ran before

Activating the sha `previous` names is not a rollback: it moves forward like any other activate, so the release being left becomes `previous`. It is run by that release's own opsctl.

Command:

```
$ sudo /opt/ikigenba/releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d/opsctl/bin/opsctl activate 1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d r141
```

Output:

```
release: ok (1a2b3c4, r141)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r141)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current 1a2b3c4, previous c604e32)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth 1a2b3c4 active)
service: ok (telemetry 1a2b3c4 active)
service: ok (dummy 1a2b3c4 active)
service: ok (events 1a2b3c4 active)
service: ok (mcp 1a2b3c4 active)
service: ok (repos 1a2b3c4 active)
service: ok (scripts 1a2b3c4 active)
service: ok (sites 1a2b3c4 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18` and `/opt/ikigenba/previous` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`; `releases/` holds nothing else.

Postconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`. Both folders are still there. `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl`, so `opsctl version` prints `r141 (1a2b3c4)`.
- `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d/label` holds `r141`, every app's environment file holds `IKIGENBA_COMMIT=1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `IKIGENBA_RELEASE=r141`, and every app runs `1a2b3c4`.

## An agent activates a release beside folders neither link names

Retention keeps the two releases a host can go back and forth between and nothing more. It only judges folders named by a full sha: devctl may be unpacking into a folder of its own beside them, and anything else in `releases/` is left alone.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (removed 2 releases)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` names `releases/9f8e7d6c5b4a39281706f5e4d3c2b1a098765432`.
- `releases/` also holds `5d4c3b2a1908f7e6d5c4b3a29180f7e6d5c4b3a2/`, an older release, and `tmp.8Qw2Lr/`, a folder devctl is unpacking into.

Postconditions:

- `releases/` holds `c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/`, `1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d/` and `tmp.8Qw2Lr/`, the last untouched. `9f8e7d6c5b4a39281706f5e4d3c2b1a098765432/` and `5d4c3b2a1908f7e6d5c4b3a29180f7e6d5c4b3a2/` are gone.
- Everything else is as "An agent activates a release on a host that runs releases" leaves it.

## An agent activates a release that drops an app

The host runs `gmail` from the current release, and the new release does not carry it. The app is stopped and disabled and its units and environment directory removed, but its data stays where it is: `gmail` becomes a data-only service, which backup, snapshot and retire still archive (`S08-backup.md`) and a later release that carries it again picks up.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps; gmail stopped and removed, state kept)
links: ok (current c604e32, previous 1a2b3c4)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (updated)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`, which holds the eight apps and `gmail`, whose manifest declares a database at `state/gmail.db`; there is no `previous`, and `releases/` holds only the two releases.
- `gmail` is `active` and `/var/opt/ikigenba/gmail/state/` holds its data.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` holds the eight apps and no `gmail/`.

Postconditions:

- `ikigenba-gmail.socket` and `ikigenba-gmail.service` were stopped and disabled, and their unit files and `/etc/opt/ikigenba/gmail/` are gone.
- `/var/opt/ikigenba/gmail/` is byte for byte as it was.
- nginx has no block for `gmail`, `/run/ikigenba/services.json` does not list it, and `/etc/litestream.yml` no longer names `gmail.db`; `litestream.service` was restarted.
- `status` shows `gmail - - - - -`.
- Every other app is as "An agent activates a release on a host that runs releases" leaves it.

## An agent activates a release over a disabled app

Disabling is the operator's decision and an activate does not undo it, any more than an install does: the app's environment file and units are rewritten for the new release, neither unit is enabled or started, and its `service` line says so in its turn.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 disabled)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (nothing removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`, there is no `previous`, and `releases/` holds only the two releases.
- `dummy` is disabled: both its units are disabled and inactive.

Postconditions:

- `dummy`'s environment file and both unit files were rewritten for `c604e32`; both units are still disabled and inactive, and `/run/ikigenba/dummy.sock` does not exist.
- nginx still answers `503` for `dummy`'s names, and `/run/ikigenba/services.json` lists `dummy` with `"enabled": false`.
- `opsctl enable dummy` later starts it on `c604e32`.
- Every other app is as "An agent activates a release on a host that runs releases" leaves it.

## An agent activates a release whose app will not come up

The rolling restart stops at the first app that does not become `active`. By then every environment file and unit has been written and the links have moved, so the apps already restarted run the new release; the ones after the failure were not restarted and keep running their old process. Nothing is undone, and no release is removed, so `opsctl rollback` still has `previous` to go back to.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: failed: mcp: service failed to start
opsctl: activate failed

> ikigenba-mcp.service: Main process exited, code=exited, status=1/FAILURE
> mcp: open /opt/ikigenba/current/mcp/etc/catalog.json: no such file or directory
```

Exits 1. The step outcome lines are on stdout; the command diagnostic and quoted journal are on stderr.

Preconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` names `releases/9f8e7d6c5b4a39281706f5e4d3c2b1a098765432`; every app is `active`.
- `c604e32`'s `mcp` binary exits at start, before it tells systemd it is ready.

Postconditions:

- `/opt/ikigenba/current` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18` and `/opt/ikigenba/previous` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`. `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl`.
- No retention ran: `releases/9f8e7d6c5b4a39281706f5e4d3c2b1a098765432/` is still there.
- `auth`, `telemetry`, `dummy` and `events` run `c604e32`. `mcp`'s service is `failed`. `repos`, `scripts` and `sites` were not restarted: each still runs the process it ran before the command, from `1a2b3c4`.
- Every app's environment file and units, nginx, `/run/ikigenba/services.json` and `/etc/litestream.yml` are as a successful activate of `c604e32` leaves them.
- `sudo opsctl rollback` puts the host back on `1a2b3c4`, and activating `c604e32` again retries the whole run.

## An agent runs `activate` with a missing or malformed argument

Command:

```
$ sudo opsctl activate
```

Output:

```
opsctl: activate needs SHA

see 'opsctl activate --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. The other argument errors give the same three lines with their own first line:

- More than two arguments: `opsctl: activate takes SHA and an optional LABEL`.
- A first argument that is not 40 lowercase hexadecimal characters, such as `c604e32`: `opsctl: activate takes a full commit sha, not 'c604e32'`.
- A label that is not one or more of letters, digits, `.`, `_`, `/` and `-`, such as `r 142`: `opsctl: label 'r 142' may hold only letters, digits, '.', '_', '/' and '-'`.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An agent activates a release with an opsctl from another release

Only the opsctl a release carries knows how to put that release in place, so `activate` refuses to be run by any other: plain `opsctl` is current's, and current is not the release being activated. The diagnostic names the executable to run instead. A sha with no folder under `releases/` is refused the same way, since no opsctl can be inside it.

Command:

```
$ sudo opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

```
$ sudo /usr/local/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
opsctl: activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 must be run by /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`, so `/usr/local/bin/opsctl` resolves to that release's opsctl.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` is unpacked whole.

Postconditions:

- Nothing has changed.

## An agent activates a release whose `release.json` is missing

A release folder without its `release.json` was not unpacked whole, so it is not trusted to be the release its name says.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
opsctl: /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/release.json is missing; unpack the release again
```

Exits 1. The line is on stderr; stdout is empty. A `release.json` that is there but cannot be read gives the same line.

Preconditions:

- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl` exists and `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/release.json` does not.

Postconditions:

- Nothing has changed.

## An agent activates a release whose `release.json` names another commit

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
opsctl: /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/release.json names 9f8e7d6c5b4a39281706f5e4d3c2b1a098765432; unpack the release again
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/release.json` holds `"sha":"9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"`.

Postconditions:

- Nothing has changed.

## An agent activates a release whose tree is not a release's

Each app's tree may hold only the five directories a release lays out, and its `bin/` only the app's own binary, because the unit runs `bin/<app>` and nothing else is meant to be there.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: failed: dummy: sbin is not allowed; an app holds only bin, libexec, lib, share and etc
opsctl: activate failed
```

Exits 1. The step outcome line is on stdout; the last line is on stderr. A `bin/` holding anything besides `bin/dummy` gives `release: failed: dummy: bin holds more than dummy` the same way.

Preconditions:

- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/dummy/` holds `sbin/` beside its other directories, and its `release.json` names its sha.

Postconditions:

- Nothing has changed: no link, label, environment file, unit, nginx configuration or services file was written, and no app was restarted.

## An agent activates a release on a per-app host whose data has not moved

A per-app host with an app whose `state/` is still under `/opt/<app>/` predates the move to `/var/opt/ikigenba/`, and the cutover would remove `/opt/<app>/` with the data in it. The remedy is the per-app host's own: install the app once more, which moves its data, then activate.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: failed: crm: /opt/crm/state has not moved; install crm first
opsctl: activate failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr. An app whose environment file is still `/opt/<app>/etc/env` gives `layout: failed: crm: /opt/crm/etc/env has not moved; install crm first` the same way. The first such app in name order is named, its state before its environment file.

Preconditions:

- The host is a per-app host, and `/opt/crm/` holds `bin/`, `etc/`, `share/` and `state/`.

Postconditions:

- Nothing has changed beyond the release folder's ownership and modes: no snapshot was taken, `/opt/crm/` and every other `/opt/<app>/` are untouched, and no app was stopped.

## An agent activates a release on a host that was never initialised

A release runs inside the slices `init` writes and is served under the certificate `init` obtains, so a host without them is sent to `init` first, by the release's own opsctl.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: failed: host is not initialised; run '/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl init' first
opsctl: activate failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr.

Preconditions:

- The host is fresh or a per-app host, and `/etc/systemd/system/ikigenba.slice`, one of the other slice units, or the host's certificate is missing.

Postconditions:

- Nothing has changed beyond the release folder's ownership and modes.
- Running the named `init`, then the same activate, succeeds.

## An agent activates a release whose manifest is not valid

Every manifest in the release is judged by install's rules before anything is written, and the first invalid one stops the run with install's message, prefixed by the app.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: failed: dummy: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket
opsctl: activate failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr. The message is install's (`S07-apps.md`) with the app's name in place of the file's, so every other manifest refusal reads the same way after `manifests: failed: <app>: `.

Preconditions:

- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/dummy/etc/manifest.toml` names `port = 3100`.

Postconditions:

- Nothing has changed beyond the release folder's ownership and modes; every app keeps running the release it had.

## An agent activates a release whose secret has never been pushed

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: failed: auth: no value for 'GOOGLE_CLIENT_SECRET' in /sbx.ikigenba.dev/auth
opsctl: activate failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr.

Preconditions:

- `auth`'s manifest names `GOOGLE_CLIENT_SECRET`, and `/sbx.ikigenba.dev/auth` does not hold it or does not exist.

Postconditions:

- Nothing has changed beyond the release folder's ownership and modes; every app keeps running the release it had.

## An agent's cutover cannot snapshot a service

The snapshot is what makes the cutover safe to start, so a service that cannot be snapshotted stops it before anything is removed: the host is still a per-app host, every app running as it was.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (per app; cutover of 8 apps)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
snapshot: failed: repos: no replica under the prefix
opsctl: activate failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr. Any reason `opsctl snapshot` gives for a service (`S08-backup.md`) reads the same way after `snapshot: failed: <service>: `.

Preconditions:

- As for "An agent moves a host from per-app installs to releases", except that `<backup.s3_uri>repos/` holds nothing litestream wrote.

Postconditions:

- Every `/opt/<app>/`, every per-app unit, `/var/lib/ikigenba/services.json`, `/usr/local/bin/opsctl` and the nginx configuration are untouched, and no app was stopped or restarted. There is no `/opt/ikigenba/current`.
- Snapshot objects already written for other services stay where they are.

## An operator asks what `rollback` can do

Command:

```
$ opsctl rollback --help
```

```
$ opsctl rollback -h
```

Output:

```
Usage: opsctl rollback

Make the release previous names the one this host runs again. current is
pointed at it and previous removed, then every app's environment file and
units are written from it, nginx, /run/ikigenba/services.json and
/etc/litestream.yml are regenerated, and every app is restarted as 'opsctl
activate' does. The release keeps the label it last had. Run by the opsctl
inside that release; any other opsctl hands the command to
/opt/ikigenba/previous/opsctl/bin/opsctl. After a run that succeeds the
release rolled away from is removed, so there is nothing for a second
rollback to go back to.

Configuration keys:
  aws.region          the region this host's parameters live in
  host.name           the fully-qualified name this host answers at
  host.apex           the app that answers at the parent of host.name; unset means none
  apps.drain_seconds  how long an app may drain when stopped (default 5)
  apps.stop_seconds   how long systemd waits for an app to stop (default 10)
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is on the host.

Postconditions:

- Nothing has changed.

## An operator rolls back to the previous release

A release went out and is wrong. Plain `opsctl` is the new release's, so it hands the command to the opsctl inside `previous`, and the operator sees only that one's output. It does what activate does, for the release `previous` names and with its label as it stands, without the layout, snapshot, cutover and label steps, because the release was already in place.

Command:

```
$ sudo opsctl rollback
```

Output:

```
release: ok (1a2b3c4, r141)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current 1a2b3c4, previous none)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth 1a2b3c4 active)
service: ok (telemetry 1a2b3c4 active)
service: ok (dummy 1a2b3c4 active)
service: ok (events 1a2b3c4 active)
service: ok (mcp 1a2b3c4 active)
service: ok (repos 1a2b3c4 active)
service: ok (scripts 1a2b3c4 active)
service: ok (sites 1a2b3c4 active)
retention: ok (removed 1 release)
```

Exits 0. The lines are on stdout; stderr is empty. Run as `sudo /opt/ikigenba/previous/opsctl/bin/opsctl rollback` it prints the same.

Preconditions:

- `/opt/ikigenba/current` names `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18` and `/opt/ikigenba/previous` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d`, whose `label` holds `r141`; `releases/` holds nothing else.
- Every app runs `c604e32`.

Postconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` does not exist. `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl`, replacing a file or a link to anything else, so `opsctl version` now prints `r141 (1a2b3c4)`.
- `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` is gone. `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d/label` is untouched.
- Every app's environment file holds `IKIGENBA_COMMIT=1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `IKIGENBA_RELEASE=r141`; units, nginx, `/run/ikigenba/services.json` and `/etc/litestream.yml` are as an activate of `1a2b3c4` leaves them, and every app was restarted once, core apps first, onto `1a2b3c4`.
- `/var/opt/ikigenba/<app>/` is untouched for every app: a rollback changes code, not data.

## An operator rolls back with no previous release

A host that has activated only one release, or has just rolled back, or still runs per-app installs or nothing at all, has nowhere to go back to.

Command:

```
$ sudo opsctl rollback
```

Output:

```
opsctl: no previous release to roll back to
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `/opt/ikigenba/previous` does not exist.

Postconditions:

- Nothing has changed.

## An operator rolls back twice

The first rollback removed `previous` and the release it rolled away from, so the second has nothing to go back to and refuses as any host without `previous` does. Going forward again is an activate.

Command:

```
$ sudo opsctl rollback
```

```
$ sudo opsctl rollback
```

Output:

```
opsctl: no previous release to roll back to
```

Exits 1. The second command's line is on stderr; its stdout is empty. The first command prints and exits as "An operator rolls back to the previous release".

Preconditions:

- As for "An operator rolls back to the previous release".

Postconditions:

- After the second command, the host is as the first left it: `current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and there is no `previous`.

## An operator gives `rollback` an argument

There is only one release to roll back to, so `rollback` takes no sha.

Command:

```
$ sudo opsctl rollback 1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d
```

Output:

```
opsctl: rollback takes no arguments

see 'opsctl rollback --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator rolls back to a release whose app will not come up

A rollback's restart stops at the first app that does not become `active`, as activate's does. The links have already moved and `previous` is gone, and no release is removed, so the release rolled away from is still on disk.

Command:

```
$ sudo opsctl rollback
```

Output:

```
release: ok (1a2b3c4, r141)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current 1a2b3c4, previous none)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth 1a2b3c4 active)
service: ok (telemetry 1a2b3c4 active)
service: ok (dummy 1a2b3c4 active)
service: ok (events 1a2b3c4 active)
service: failed: mcp: service failed to start
opsctl: rollback failed

> ikigenba-mcp.service: Main process exited, code=exited, status=1/FAILURE
> mcp: open /opt/ikigenba/current/mcp/etc/catalog.json: no such file or directory
```

Exits 1. The step outcome lines are on stdout; the command diagnostic and quoted journal are on stderr.

Preconditions:

- As for "An operator rolls back to the previous release", and `1a2b3c4`'s `mcp` binary exits at start, before it tells systemd it is ready.

Postconditions:

- `/opt/ikigenba/current` names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` does not exist. `/usr/local/bin/opsctl` was re-created as a link to `/opt/ikigenba/current/opsctl/bin/opsctl`.
- No retention ran: `releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/` is still there, so activating it again by its own opsctl goes forward.
- `auth`, `telemetry`, `dummy` and `events` run `1a2b3c4`; `mcp`'s service is `failed`; `repos`, `scripts` and `sites` were not restarted and still run their `c604e32` processes.
