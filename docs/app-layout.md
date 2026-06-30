# App layout — the `/opt/<service>/` tree

> **Status: normative target.** This doc defines the canonical on-box folder
> schema every deployable app obeys. Some of it is already how the box works;
> some is the target we are bringing the tooling into compliance with over a
> series of sessions. Current gaps are listed under
> [Open divergences](#open-divergences) — that section is the compliance
> backlog, not a description of steady state.
>
> Authoritative implementation of the paths: `opsctl/internal/opsctl/layout.go`.
> When code and this doc disagree, that is a divergence to fix, not a license to
> ignore the doc — change the schema here deliberately, then bring code to it.

## The principle

`/opt/<service>/` is a **private FHS root**. Every folder mirrors a standard
Filesystem Hierarchy Standard location, and that analogy — not a bespoke
convention — fixes each folder's meaning, ownership, and lifecycle. If you're
unsure what belongs in a folder or how it should behave, ask "what does the real
FHS path do?" and match it.

This buys three things for free, because FHS already separates them:

| Tier | FHS | Folders | Nature |
|---|---|---|---|
| **Shipped** | `/usr` | `bin/` `libexec/` `share/` | Arrives in the deploy artifact. Immutable, reproducible, **never backed up** (it comes back from the artifact). Version-pinned, so it **rolls back with the binary**. |
| **Config** | `/etc` | `etc/` | Host-specific configuration. Partly shipped defaults, partly generated on the box. |
| **Local** | `/var` | `state/` `cache/` `backups/` | Born on the box, **never shipped**. `state/` is the *only* thing backup captures; `cache/` is transient; `backups/` is local deploy scaffolding. |

The tier an item lands in **is** its backup, rollback, and ownership policy.
That's the whole point of codifying FHS: the policy falls out of the location.

## Consequence — uniform apps, agnostic tooling

Because every app has the **identical** tree, everything that operates on an app
is **app-agnostic**: `opsctl`, the systemd unit, the launcher, and backup/restore
all behave the same for every service, parameterized by nothing but the **service
name**. The systemd unit is one template —
`ExecStart=/usr/local/bin/ikigenba-launch <service>`, `User=<service>`,
`WorkingDirectory=/opt/<service>` — byte-identical across apps once the name is
substituted. There is no per-app special-casing anywhere in the tooling; that is
the payoff of the schema.

The **only** per-app variables are the **name** and the **loopback port** (the
port reaches the unit via `etc/manifest.env`). So one tool manages N services
with zero per-service branches.

> *Forward note: a designed-but-not-yet-implemented service registry
> (`service-registry-design.md`) makes the port a lookup from a single
> `name → port` table in `appkit`, with `manifest.env` generated from it. Once it
> lands, the **name** becomes the sole per-app variable. No work implied for the
> current schema effort.*

## The tree

The fixed top-level set every app has is **`bin/ libexec/ etc/ share/ state/
cache/ backups/`**. (`state/www/` is a state subtree used only by
file-serving services.)

```
/opt/<service>/
├── bin/
│   └── run            -> ../libexec/<service>-<version>   (stable exec target; atomic swap)
├── libexec/
│   └── <service>-<version>                                (versioned binaries; N kept, pruned)
├── etc/
│   ├── manifest.env                                       (GENERATED on box from the binary)
│   └── nginx.conf                                         (SHIPPED; target — see divergences)
├── share/                                                 (SHIPPED read-only resources; target)
├── state/
│   ├── <service>.db (+ -wal/-shm)                         (the app database)
│   └── www/{working,public,private}                      (file-serving services only)
├── cache/
│   └── <service>.db.generation                            (event-plane epoch sidecar)
└── backups/
    └── pre-<version>.db                                   (local pre-migration snapshots)
```

### Per-folder reference

| Path | FHS analog | Tier | Contents & lifecycle | Owner (intent) |
|---|---|---|---|---|
| `bin/run` | `/usr/bin` entry | shipped | Symlink → active `libexec/<service>-<version>`. The launcher execs this stable path; deploy/rollback atomically repoint it. | root |
| `libexec/<service>-<version>` | `/usr/libexec` | shipped | One immutable versioned binary. Multiple kept; `prune` deletes old ones (and their matching `backups/pre-*.db`). | root |
| `etc/manifest.env` | `/etc` | config | `KEY=val`, **regenerated every deploy** by running `<binary> manifest` and stamping abs `*_DB_PATH`/`*_GENERATION_PATH`. Derived ⇒ rolls back for free (re-run the old binary). The launcher sources it. | root `0644` |
| `etc/nginx.conf` | `/etc` | config (shipped) | The service's nginx location fragment, **shipped in the artifact** and installed at deploy. *Target* — today it is applied manually at `setup` time. | root |
| `share/` | `/usr/share` | shipped | Read-only resources the app **reads** at runtime (assets, templates, data files). The app never writes here. Empty today; defined so future shipped files have a home without changing the delivery mechanism. | root |
| `state/<service>.db` | `/var/lib` | local | The app DB. **Never overwritten by deploy** except the explicit pre-migration snapshot; `migrate` runs forward-only against it. The one thing that must survive a reinstall. | `<service>:<service>` |
| `state/www/{working,public,private}` | `/var/lib` | local | File-serving services' served tree: drafts → publish symlinks → private surface. Under `state/`, so it **is** in the backup. nginx (`www-data`) reads it. | see [Permissions](#permissions) |
| `cache/<service>.db.generation` | `/var/cache` | local | Transient derived data (event-plane epoch). **Not backed up; wiped and re-minted on restore.** Safe to delete anytime. | `<service>:<service>` |
| `backups/pre-<version>.db` | `/var/backups` | local | **Local** pre-migration DB snapshot keyed by the FROM-version; rollback restores it. Pruned with its release. Not part of the S3 backup. | root |

## Delivery

- **Today:** `bin/ship` builds one static binary and `scp`s it to the box `/tmp`;
  `opsctl stage` places it at `libexec/<service>-<version>`. Only the binary
  travels; `etc/nginx.conf` is applied separately, by hand, at `setup`.
- **Target:** the unit of delivery is **one versioned `tar.gz` bundle** carrying
  the shipped tiers — the binary plus `etc/`-bound config (`nginx.conf`) plus
  anything in `share/`. `stage` unpacks it; `deploy` activates it. One atomic,
  versioned, extensible artifact: adding a `share/` file later needs no change to
  the transfer mechanism. The bundle is retained per version so a rollback
  re-applies the **matching** config/`share/`, the same way `libexec/` already
  keeps old binaries.

## Backup / restore policy

Derived entirely from the tiers — there are two layered mechanisms:

- **`opsctl backup` / `opsctl restore`** (box-level, S3 — the reference): tars
  **`state/` only** → `s3://<bucket>/<app>/snapshots/<ts>.tar`, keeps 30, writes
  an `<app>/latest` pointer. **Restore** stops the unit, snapshots current
  `state/` to `pre-restore/` first, then wipes `state/`+`cache/`, untars
  `state/`, and recreates an **empty** `cache/`. It never touches `bin/`,
  `libexec/`, `etc/`, or `backups/` — they're reproducible from the artifact. The
  apex/dashboard additionally backs up the TLS cert tree as a separate stream.
- **`<binary> backup` / `<binary> restore`** (per-binary, single DB): `VACUUM
  INTO` for a consistent snapshot; used by `deploy` to write `backups/pre-<version>.db`
  and by `rollback` to restore it. Any restore re-mints the event-plane epoch by
  removing the `cache/` generation sidecar.

The rule, stated once: **`state/` is the backup. `cache/` is reset. The shipped
and config tiers are reproducible and are not in the backup.**

## Permissions

Ownership follows the tier:

- **System user `<service>`** — `useradd --system`, nologin, matching group, home
  `/opt/<service>`. The unit runs `User=<service>`.
- **App-owned (`<service>:<service>`):** `state/` (and `state/<service>.db`) and
  `cache/`. Migrate runs as root and creates root-owned DB files, so deploy
  re-chowns `state/` back to the app on every deploy, so the service can take a
  write lock.
- **Root-owned:** the binaries (`libexec/<service>-<version>`), the structural
  dirs, `etc/manifest.env`, and all system config outside `/opt/<service>`.
- **nginx (`www-data`) traversal of `state/www/`:** the intended model is a shared
  **`web` group** (members `<service>` + `www-data`) so nginx reads the served
  tree by group membership while `state/` itself stays private to the app.
  ⚠️ Exact mode bits are currently contested in the code — see divergences.

## System-config paths (outside `/opt/<service>`)

Written by `init-box` (one-time, box-global) and `setup` (one-time, per-app),
rooted at `/`:

| Path | Owner | Written by |
|---|---|---|
| `/etc/systemd/system/<service>.service` | root | setup |
| `/etc/systemd/system/ikigenba-{certbot-renew,backup}.{timer,service}` | root | init-box |
| `/etc/nginx/conf.d/<defaultapp>.conf` (apex server block) | root | init-box |
| `/etc/nginx/conf.d/locations/<service>.conf` (location fragment) | root | setup *(target: deploy)* |
| `/etc/letsencrypt/{archive,renewal,live}`, `/var/lib/letsencrypt` | root | init-box / certbot |
| `/etc/ikigenba/env` (box-global `EnvironmentFile`, SSM-seeded) | root | provisioning (outside opsctl) |

## Open divergences

The compliance backlog — where current code/docs do not yet meet this schema:

1. **`share/` does not exist yet.** No app creates or ships it. Add it to the
   `setup` tree and the delivery bundle.
2. **Delivery is single-binary, not a bundle.** `bin/ship` ships only the binary;
   there is no `tar.gz`, no shipped `etc/nginx.conf`, no `share/`. The whole
   "shipped tier travels together and rolls back by version" model is the target.
3. **`etc/nginx.conf` is not shipped or deploy-installed.** Today the fragment is
   applied by hand at `setup` and never re-applied on deploy, so a fragment change
   ships silently un-applied (the original motivation —
   `opsctl/project/research/deploy-nginx-fragment-research.md`). Folds into the
   bundle work above.
4. **Permissions disagree across four sources.** `setup.go`'s worker branch
   (`state/` `0711`, `state/www` `0750 <service>:web`) vs its routed/sites branch
   (`state/www` `0755 <service>:<service>`) vs `layout.go`'s comment vs `D01`
   (`state/` `0711`, cache/backups `0750`). No two fully agree, and the installed
   mode depends on whether the app passes an nginx fragment. **Reconcile to one
   model** (the `web`-group scheme is the `D01`-blessed intent) in a dedicated
   session and make `setup` apply it uniformly.
5. **Stale `data/` references** (old layout, pre-`state/`): `deploy.md:125`
   (the fresh-start `mv` command — would fail) and `dashboard/AGENTS.md:147`.
   Fix to `state/<service>.db`.

## Related docs

- `opsctl/project/design/D01.md` (the tree), `D02.md` (libexec + `bin/run`),
  `D05.md` (state/cache split), `D08.md` (per-service adoption + converter).
- `deploy.md` (operator deploy workflow).
- `opsctl/project/research/deploy-nginx-fragment-research.md` (the deploy/nginx
  gap that started this).
- Superseded: `docs/archive/adr-deployment-redesign.md`,
  `docs/archive/versioning.md` (predate the `state/`+`cache/` split).
