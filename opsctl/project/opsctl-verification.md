# opsctl — Live verification (out of loop)

This doc owns the acceptance checks the autonomous `ralph` build loop **cannot**
perform: opsctl's **real-substrate** Verification ids, whose correctness depends
on the real box — a genuine cross-device filesystem topology, the real
`/etc/ikigenba/env`, the service user actually being able to read/write a path,
real `nginx -t` against the real cert, or a real package manager / release
installer. These are run by a **human operator** on the live box
(`int.ikigenba.com`), never by `ralph`.

## Why these are out of loop

The loop's `verify` runs only `GOWORK=off go test ./...` from `opsctl/` with a
faked `System` seam — no root, no systemd, one temp filesystem, no network. Each
id below hinges on something the fake accepts unconditionally (any chown, any
package name, any rename target on one device) or that the offline suite cannot
reach (the fixed box env path, a real cross-device rename, a live HTTP fetch
through nginx). A stub cannot falsify them, and a network/box-dependent test
would also fail the loop's "reproducible on identical repo state" bar. So design
mints them as live-substrate ids and **no plan phase schedules them as
loop-gating work** (see `project/plan/README.md`); design still owns each under
its Decision. The offline suite proves construction (paths computed, ops
recorded); this doc proves the real thing runs.

These eight ids are exactly the design ids that do **not** appear as tags in any
`*_test.go` — the out-of-loop set. The offline `comm -23` coverage check
(`project/plan/README.md`) is understood to range over loop-driven ids only;
these are its expected, documented remainder.

## Preconditions

- The box is reachable: `ssh int true` (deploy/verify work happens over SSH).
- `/etc/ikigenba/env` is seeded (Terraform-managed, out of repo): at least
  `IKIGENBA_ROOT=/opt` and `IKIGENBA_DOMAIN=int.ikigenba.com`. opsctl reads it
  automatically at startup, so plain `sudo opsctl <verb>` sees it.
- The opsctl under test is the one installed on the box
  (`/usr/local/bin/opsctl`), not a working-tree build — deploy it first
  (`deploy.md` → "Deploying opsctl Itself").
- opsctl does its AWS work (S3 backup/restore) under the box's EC2 instance role;
  no workstation SSO is needed for these checks.
- Checks that deploy or restore a service (D4/D8/D9) mutate live state and the
  live front door — run them only during a sanctioned maintenance action, not as
  a casual probe.

Run pattern for an interactive verb with the box env loaded:

```sh
ssh int "sudo bash -c 'set -a; . /etc/ikigenba/env; opsctl <verb> …'"
```

## Checks

### D10 — `R-JRO8-5Q0R` — box-baseline binaries resolve and run

**Positive.** After `opsctl init-box` on the box, every baseline binary the
package install provides resolves and runs:

```sh
ssh int 'command -v git sqlite3 pdftotext pdftoppm pdfinfo tar curl \
  && git --version && sqlite3 --version && pdftotext -v && tar --version && curl --version'
```

Pass: all resolve and each version invocation exits cleanly; a re-run of
`init-box` succeeds with the packages already present.

**Falsifiability.** The fake `System` accepts any package name; only the real
`dnf` proves AL2023 actually provides these binaries under the names `git`,
`sqlite`, `poppler-utils`, `tar`, and `curl-minimal`. The proof is the runnable
binaries, not that an install was requested.

### D11 — `R-MMF1-HFMO` — the oauth CLI installs to `/usr/local/bin`, usable by any user

**Positive.** After `opsctl init-box` on the box:

```sh
ssh int 'command -v oauth; stat -c "%a %U:%G" /usr/local/bin/oauth; oauth -V'
# any-user exec (not just root):
ssh int 'sudo -u nginx bash -lc "oauth -V"'
```

Pass: `oauth` resolves to `/usr/local/bin/oauth`, mode `0755`, `oauth -V` exits
cleanly printing a version, and an unprivileged user runs it successfully.
Re-running `init-box` reinstalls the latest release and it still runs.

**Falsifiability.** The fake `System` records the `install-script` call without
running it; only the real installer against the real GitHub release proves a
runnable `linux/amd64` binary is fetched, unpacked, and placed executable. A
default-`BINDIR` implementation would land it in `~/.local/bin`, not the global
path, and fail the any-user check.

### D1 — `R-WRJF-H7J9` — restore reconstructs `cache/` owned by the service user

**Positive.** After `opsctl restore <app>` on the box, the unit returns to
`active` and loopback `/health` responds 200:

```sh
ssh int 'sudo systemctl is-active <app>; curl -s -m5 -o /dev/null -w "%{http_code}\n" http://127.0.0.1:<port>/health'
```

Pass: `active` and `200` — the service user could write `cache/` on restart.

**Falsifiability.** The fake accepts any chown; only a real restore + unit
restart proves `<app>:<app>` ownership actually lets the live service write. The
proof is the running, health-200 service.

### D2 — `R-66UP-LI59` — stage completes across separate filesystems (no `EXDEV`)

**Positive.** On the box, where `/tmp` and `OPSCTL_ROOT` (`/opt`) are separate
mounts:

```sh
ssh int 'sudo opsctl stage <app> <version> --artifact <bundle>; df /tmp /opt'
```

Pass: `stage` completes and the version appears staged, with no cross-device
(`EXDEV`) error; `df` confirms `/tmp` and `/opt` are genuinely distinct devices.

**Falsifiability.** A unit test on one temp filesystem shares a device trivially;
only a real box with distinct mounts exercises the rename that the pre-fix code
broke on.

### D3 — `R-6FE0-9WC4` — opsctl auto-loads `/etc/ikigenba/env`

**Positive.** Run an env-dependent verb interactively **without** sourcing the env
first:

```sh
ssh int 'sudo opsctl backup <app>'   # no `. /etc/ikigenba/env`
```

Pass: the verb reaches its S3 step (e.g. `IKIGENBA_BACKUP_BUCKET` is resolved)
rather than failing `IKIGENBA_BACKUP_BUCKET is required`.

**Falsifiability.** A unit test hands the loader a temp path and never exercises
the fixed box path under a non-systemd interactive launch; only the box proves
`main` wires `LoadEnvFile` to the real `/etc/ikigenba/env`.

### D4 — `R-MYS7-2H2R` — dashboard deploy renders the apex block against real nginx + cert

**Positive.** After a real `opsctl deploy dashboard <version>` on the box:

```sh
ssh int 'curl -s -m5 -o /dev/null -w "apex %{http_code}\n" https://int.ikigenba.com/'
ssh int 'curl -s -m5 -o /dev/null -w "srv  %{http_code}\n" https://int.ikigenba.com/srv/<svc>/health'
```

Pass: the apex renders with the real `IKIGENBA_DOMAIN`, `nginx -t` passes against
the real cert, the reload succeeds, and afterward the apex serves the dashboard
**and** the path-routed `/srv/<svc>/` mounts still resolve through the freshly
installed `include …/locations/*.conf` (public routes 200, protected MCP routes
401; never 502/503).

**Falsifiability.** The fake `System` never runs real `nginx -t` against the real
cert nor proves the include still resolves; the loop phase for D4 covers only
`R-MSOP-5MDA`/`R-MTWL-JE3Z`/`R-MV4H-X5UO`/`R-MXKA-OPC2` (partial-Decision split).
The proof is apex + all service routes serving after a real deploy.

### D8 — `R-AXY7-K8GA` — deploy leaves the served tree readable through the front door

**Positive.** After a real `opsctl deploy sites <version>` on the box, an
anonymous fetch of a published public site:

```sh
ssh int 'curl -s -m5 -o /dev/null -w "%{http_code}\n" https://int.ikigenba.com/srv/sites/public/<published-site>/'
```

Pass: `200` — the deployed sites process serves the public tier through nginx;
the state-ownership chown already owns the served tree (no separate www step).

**Falsifiability.** The fake accepts any chown; only a real deploy + live HTTP
fetch proves the tree is still readable by nginx afterward.

### D9 — `R-B0E0-BRXO` — restore reconstitutes the served tree's ownership

**Positive.** After a real `opsctl restore sites <key>` on the box, the same
anonymous fetch:

```sh
ssh int 'curl -s -m5 -o /dev/null -w "%{http_code}\n" https://int.ikigenba.com/srv/sites/public/<published-site>/'
```

Pass: `200` — the restored tree is owned by and servable through the sites
process regardless of the snapshot's captured metadata.

**Falsifiability.** The fake accepts any chown; only a real restore + live HTTP
fetch proves ownership was reconstituted to the service user.

## Recording the result

These are manual gates. Record each run (date, opsctl commit/sha, the positive
observation) wherever the box's deploy/acceptance log lives; they are **not**
tracked by `project/plan/STATUS.md` (the autonomous loop's manifest only). A
lightweight running record follows.

| id | last verified | opsctl commit | observed |
|---|---|---|---|
| `R-JRO8-5Q0R` (D10) | 2026-07-23 | e8567e70 | `init-box` on int: git/sqlite3/pdftotext/pdftoppm/pdfinfo/tar/curl all resolve; baseline packages present (tar, curl-minimal added). |
| `R-MMF1-HFMO` (D11) | 2026-07-23 | e8567e70 | `init-box --skip-cert` on int installed `/usr/local/bin/oauth` (`v0.1.2`, mode 0755); `oauth -V` exit 0; unprivileged `nginx` user ran it. Apex block hash unchanged (no front-door drift). |
| `R-WRJF-H7J9` (D1) | — | — | not yet recorded |
| `R-66UP-LI59` (D2) | — | — | not yet recorded |
| `R-6FE0-9WC4` (D3) | — | — | not yet recorded |
| `R-MYS7-2H2R` (D4) | — | — | not yet recorded |
| `R-AXY7-K8GA` (D8) | — | — | not yet recorded |
| `R-B0E0-BRXO` (D9) | — | — | not yet recorded |
