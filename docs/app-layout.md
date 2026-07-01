# App layout — the `/opt/<service>/` tree

> **Status: normative — code is in compliance.** This doc defines the canonical
> on-box folder schema every deployable app obeys, and `opsctl` now implements it
> (the versioned bundle + three-symlink swap). The one remaining gap is the
> permission model; it is listed under
> [Open divergences](#open-divergences).
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

The active release is selected by **three symlinks repointed atomically on every
deploy** — `bin/run`, `etc/current`, and `share/current` — so the binary, its
config, and its static assets all cut over together (and roll back together).

```
/opt/<service>/
├── bin/
│   └── run                -> ../libexec/<service>-<version>   (stable exec target; atomic swap #1)
├── libexec/
│   └── <service>-<version>                                    (versioned binaries; N kept, pruned)
├── etc/
│   ├── <version>/
│   │   ├── manifest.env                                       (SHIPPED in the bundle; authored in-repo)
│   │   └── nginx.conf                                         (SHIPPED in the bundle)
│   ├── current            -> <version>                        (active config; atomic swap #2)
│   └── manifest.env                                           (stable path the dashboard inventory reads)
├── share/
│   ├── <version>/                                             (SHIPPED read-only resources)
│   └── current            -> <version>                        (active assets; atomic swap #3)
├── state/
│   ├── <service>.db (+ -wal/-shm)                             (the app database)
│   └── www/{working,public,private}                          (file-serving services only)
├── cache/
│   └── <service>.db.generation                                (event-plane epoch sidecar)
└── backups/                                                   (vestigial — see divergences; not the S3 backup)
```

### Per-folder reference

| Path | FHS analog | Tier | Contents & lifecycle | Owner (intent) |
|---|---|---|---|---|
| `bin/run` | `/usr/bin` entry | shipped | Symlink → active `libexec/<service>-<version>`. The launcher execs this stable path; deploy/rollback atomically repoint it. | root |
| `libexec/<service>-<version>` | `/usr/libexec` | shipped | One immutable versioned binary. Multiple kept; `prune` deletes old ones (and their matching `backups/pre-*.db`). | root |
| `etc/<version>/manifest.env` | `/etc` | config (shipped) | `KEY=val`, **authored in-repo** (`<service>/etc/manifest.env`) and **shipped in the deploy bundle** — no on-box `manifest` verb runs. Selected by `etc/current`; the launcher sources `etc/current/manifest.env`. The dashboard inventory reads the stable `etc/manifest.env` path. | root `0644` |
| `etc/<version>/nginx.conf` | `/etc` | config (shipped) | The service's nginx location fragment, **shipped in the bundle** and applied on every deploy via `etc/current` (nginx reload). Rolls back with the release. | root |
| `share/<version>/` | `/usr/share` | shipped | Read-only resources the app **reads** at runtime (assets, templates, data files). The app never writes here. Shipped in the bundle; selected by `share/current`. Empty for services that ship none. | root |
| `state/<service>.db` | `/var/lib` | local | The app DB. **Never overwritten by deploy** except the explicit pre-migration snapshot; `migrate` runs forward-only against it. The one thing that must survive a reinstall. | `<service>:<service>` |
| `state/www/{working,public,private}` | `/var/lib` | local | File-serving services' served tree: drafts → publish symlinks → private surface. Under `state/`, so it **is** in the backup. nginx (`www-data`) reads it. | see [Permissions](#permissions) |
| `cache/<service>.db.generation` | `/var/cache` | local | Transient derived data (event-plane epoch). **Not backed up; wiped and re-minted on restore.** Safe to delete anytime. | `<service>:<service>` |
| `backups/` | `/var/backups` | local | **Vestigial.** The per-binary pre-migration DB snapshot (`pre-<version>.db`) is no longer written — backup/restore/rollback are now box-level `opsctl` S3 operations. `setup` may still create the dir and `prune` still cleans it, but nothing populates it. Not part of the S3 backup. | root |

## Delivery

The unit of delivery is **one versioned `tar.gz` bundle** carrying the shipped
tiers — the binary, the `etc/`-bound config (`manifest.env` + `nginx.conf`), and
anything in `share/`. `bin/ship` builds current `main` and copies the bundle to
the box `/tmp`; `opsctl stage` unpacks it into the versioned slots
(`libexec/<service>-<version>`, `etc/<version>/`, `share/<version>/`); `opsctl
deploy` activates it with the three-symlink swap. One atomic, versioned,
extensible artifact: adding a `share/` file later needs no change to the
transfer mechanism. Each version's bundle is retained so a rollback re-applies
the **matching** binary/config/`share/` together.

## Backup / restore policy

Derived entirely from the tiers, and owned entirely by **`opsctl`** at the box
level — there is **no per-binary `backup`/`restore` verb** (those were removed
from the appkit chassis; the binary's verb set is
`serve`/`version`/`manifest`/`migrate`/`schema`).

- **`opsctl backup`** tars **`state/` only** → `s3://<bucket>/<app>/snapshots/<ts>.tar`,
  keeps 30, writes an `<app>/latest` pointer. The apex/dashboard additionally
  backs up the TLS cert tree as a separate stream.
- **`opsctl restore`** stops the unit, wipes `state/`+`cache/`, untars `state/`,
  and recreates an **empty** `cache/` (re-minting the event-plane epoch). It
  never touches `bin/`, `libexec/`, `etc/`, or `share/` — they're reproducible
  from the retained bundle.
- **`opsctl deploy`** takes an unconditional pre-deploy S3 backup (skipped only
  on the first-ever deploy, when there is no live release to capture) before
  migrating. **`opsctl rollback`** restores an S3 snapshot selected by recency
  (`-N` for the Nth most recent) — not a local pre-migration DB file.

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

The compliance backlog — where current code does not yet meet this schema.

**Still open:**

1. **Permissions disagree across sources.** `setup.go`'s worker branch
   (`state/` `0711`, `state/www` `0750 <service>:web`) vs its routed/sites branch
   (`state/www` `0755 <service>:<service>`) vs `layout.go`'s comment vs `D01`
   (`state/` `0711`, cache/backups `0750`). No two fully agree, and the installed
   mode depends on whether the app passes an nginx fragment. **Reconcile to one
   model** (the `web`-group scheme is the `D01`-blessed intent) in a dedicated
   session and make `setup` apply it uniformly.

**Resolved (code has caught up to this schema):**

- **The `tar.gz` bundle delivery** is implemented — `bin/ship` builds a versioned
  bundle; `stage` unpacks binary + `etc/<version>/` + `share/<version>/`; `deploy`
  activates it with the three-symlink swap. (Former divergences #1 `share/`, #2
  bundle delivery, #3 shipped/deploy-installed `nginx.conf`.)
- **Stale `data/` references** fixed to `state/<service>.db` in `deploy.md` and
  `dashboard/AGENTS.md`.

## Related docs

- `opsctl/project/design/D01.md` (the tree), `D02.md` (libexec + `bin/run`),
  `D05.md` (state/cache split), `D08.md` (per-service adoption + converter).
- `deploy.md` (operator deploy workflow).
- `opsctl/project/research/deploy-nginx-fragment-research.md` (the deploy/nginx
  gap that started this).
- Superseded: `docs/archive/adr-deployment-redesign.md`,
  `docs/archive/versioning.md` (predate the `state/`+`cache/` split).
