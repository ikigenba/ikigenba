# opsctl — Manual verification (out of gate)

This doc owns the acceptance checks the autonomous `ralph` build loop **cannot**
perform: opsctl's **real-substrate** Verification ids, whose correctness depends
on the real box — a genuine cross-device filesystem topology, the real
`/etc/ikigenba/env`, the service user actually being able to read/write a path,
real `nginx -t` against the real cert, or a real package manager / release
installer. These checks are the tree's **manual layer** and are run by a human
operator on the live box (`int.ikigenba.com`), never by the default gate. This
tree has no composed or live test layer and no `go test -tags live` invocation.

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

**Negative.** Run `ssh int 'command -v definitely-not-an-opsctl-binary'` and
require a nonzero exit with no path printed. That is the loud failure the
positive command produces for a missing baseline binary. The fake `System`
accepts any package name; only the real
`dnf` proves AL2023 actually provides these binaries under the names `git`,
`sqlite`, `poppler-utils`, `tar`, and `curl-minimal`. The proof is the runnable
binaries, not that an install was requested.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, and observed versions.

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

**Negative.** Run
`ssh int 'sudo -u nginx test -x /root/.local/bin/oauth'` and require a nonzero
exit. This control demonstrates that the any-user positive check fails loudly
when the installer uses a root-private default location. The fake `System`
records the `install-script` call without
running it; only the real installer against the real GitHub release proves a
runnable `linux/amd64` binary is fetched, unpacked, and placed executable. A
default-`BINDIR` implementation would land it in `~/.local/bin`, not the global
path, and fail the any-user check.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, installed path/mode/version, and unprivileged result.

### D1 — `R-WRJF-H7J9` — restore reconstructs `cache/` owned by the service user

**Positive.** After `opsctl restore <app>` on the box, the unit returns to
`active` and loopback `/health` responds 200:

```sh
ssh int 'sudo systemctl is-active <app>; curl -s -m5 -o /dev/null -w "%{http_code}\n" http://127.0.0.1:<port>/health'
```

Pass: `active` and `200` — the service user could write `cache/` on restart.

**Negative.** During a sanctioned disposable-service check, make `cache/`
unwritable by the service account, restart it, and require `systemctl is-active`
or the loopback health probe to fail nonzero/non-200; restore ownership before
continuing. This loud control proves the positive restart exercised
service-user access. The fake accepts any chown; only a real restore + unit
restart proves `<app>:<app>` ownership actually lets the live service write. The
proof is the running, health-200 service.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, snapshot key, ownership, unit state, and health status.

### D2 — `R-66UP-LI59` — stage completes across separate filesystems (no `EXDEV`)

**Positive.** On the box, where `/tmp` and `OPSCTL_ROOT` (`/opt`) are separate
mounts:

```sh
ssh int 'sudo opsctl stage <app> <version> --artifact <bundle>; df /tmp /opt'
```

Pass: `stage` completes and the version appears staged, with no cross-device
(`EXDEV`) error; `df` confirms `/tmp` and `/opt` are genuinely distinct devices.

**Negative.** Run `ssh int 'test "$(df --output=source /tmp | tail -1)" !=
"$(df --output=source /opt | tail -1)"'` and require success. If it fails, the
cross-device precondition was not exercised and the check fails loudly. A stage
implementation that uses direct rename will instead fail with `EXDEV`. A unit
test on one temp filesystem shares a device trivially;
only a real box with distinct mounts exercises the rename that the pre-fix code
broke on.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, artifact/version, both devices, and stage result.

### D3 — `R-6FE0-9WC4` — opsctl auto-loads `/etc/ikigenba/env`

**Positive.** Run an env-dependent verb interactively **without** sourcing the env
first:

```sh
ssh int 'sudo opsctl backup <app>'   # no `. /etc/ikigenba/env`
```

Pass: the verb reaches its S3 step (e.g. `IKIGENBA_BACKUP_BUCKET` is resolved)
rather than failing `IKIGENBA_BACKUP_BUCKET is required`.

**Negative.** Temporarily move `/etc/ikigenba/env` aside during the sanctioned
check, run the same unsourced command, and require the missing required-variable
diagnostic; restore the file immediately with a shell trap. If the command still
reaches S3, the positive probe was contaminated by another environment source.
A unit test hands the loader a temp path and never exercises
the fixed box path under a non-systemd interactive launch; only the box proves
`main` wires `LoadEnvFile` to the real `/etc/ikigenba/env`.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, backup key, and confirmation that no shell env was sourced.

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

**Negative.** In a temporary copy of the generated nginx configuration, replace
the certificate path with `/definitely/missing.pem` and run `nginx -t -c
<temporary-config>`; require a nonzero exit naming the missing certificate. Do
not install or reload the broken copy. The fake `System` never runs real
`nginx -t` against the real
cert nor proves the include still resolves; the loop phase for D4 covers only
`R-MSOP-5MDA`/`R-MTWL-JE3Z`/`R-MV4H-X5UO`/`R-MXKA-OPC2` (partial-Decision split).
The proof is apex + all service routes serving after a real deploy.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, dashboard version, nginx result, and HTTP statuses.

### D8 — `R-AXY7-K8GA` — deploy leaves the served tree readable through the front door

**Positive.** After a real `opsctl deploy sites <version>` on the box, an
anonymous fetch of a published public site:

```sh
ssh int 'curl -s -m5 -o /dev/null -w "%{http_code}\n" https://int.ikigenba.com/srv/sites/public/<published-site>/'
```

Pass: `200` — the deployed sites process serves the public tier through nginx;
the state-ownership chown already owns the served tree (no separate www step).

**Negative.** Fetch a deliberately absent public-site name with the same curl
command and require a non-200 status. This is the loud control proving that the
positive 200 is not an unconditional nginx response. The fake accepts any
chown; only a real deploy + live HTTP
fetch proves the tree is still readable by nginx afterward.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, sites version/name, and positive and control statuses.

### D9 — `R-B0E0-BRXO` — restore reconstitutes the served tree's ownership

**Positive.** After a real `opsctl restore sites <key>` on the box, the same
anonymous fetch:

```sh
ssh int 'curl -s -m5 -o /dev/null -w "%{http_code}\n" https://int.ikigenba.com/srv/sites/public/<published-site>/'
```

Pass: `200` — the restored tree is owned by and servable through the sites
process regardless of the snapshot's captured metadata.

**Negative.** Fetch a deliberately absent public-site name with the same curl
command and require a non-200 status. If both names return 200, the positive
probe is not specific enough and does not count. The fake accepts any chown;
only a real restore + live HTTP
fetch proves ownership was reconstituted to the service user.

**Record.** Update this id's row in **Recording the result** with the date,
opsctl commit, snapshot key, ownership, and positive and control statuses.

### `web` group access — served trees readable without state disclosure

**Positive.** Choose a deployed `<svc>` whose real process account is a member
of the `web` group, then run:

```sh
ssh int "sudo -u <svc> test -r /opt/<svc>/state/www/public"
ssh int "sudo -u <svc> test -r /opt/<svc>/state/www/private"
```

Pass: both commands exit zero, proving the real account can traverse and read
both served trees through its real uid/gid memberships.

**Negative.** The same account must be denied both the service database and a
directory listing of `state/`:

```sh
ssh int "sudo -u <svc> test ! -r /opt/<svc>/state/<svc>.db"
ssh int "sudo -u <svc> sh -c '! ls /opt/<svc>/state >/dev/null 2>&1'"
```

Pass: both inverted checks exit zero. Remove the `!` from either command as a
control and require a loud nonzero exit; a readable database or successful
listing blocks acceptance.

**Record.** Update the `web` group row in **Recording the result** with the
date, opsctl commit, selected account, both positive exits, and both denials.

## Recording the result

These are manual gates. Record each run (date, opsctl commit/sha, the positive
observation) wherever the box's deploy/acceptance log lives; they are **not**
tracked by `project/plan/STATUS.md` (the autonomous loop's manifest only). A
lightweight running record follows.

| id | last verified | opsctl commit | observed |
|---|---|---|---|
| `R-JRO8-5Q0R` (D10) | 2026-07-23 | e8567e70 | `init-box` on int: git/sqlite3/pdftotext/pdftoppm/pdfinfo/tar/curl all resolve; baseline packages present (tar, curl-minimal added). |
| `R-MMF1-HFMO` (D11) | 2026-07-23 | e8567e70 | `init-box --skip-cert` on int installed `/usr/local/bin/oauth` (`v0.1.2`, mode 0755); `oauth -V` exit 0; unprivileged `nginx` user ran it. Apex block hash unchanged (no front-door drift). |
| `R-WRJF-H7J9` (D1) | 2026-08-07 | 075afac1 | `opsctl restore notify` on int from a seconds-old snapshot (`notify-v0.20.0+823310ac.20260807T170657Z`): unit returned `active`, loopback `/health` 200, `/opt/notify/cache` owned `notify:notify` after restart. |
| `R-66UP-LI59` (D2) | 2026-08-07 | 075afac1 | `opsctl stage sites v0.24.0+d5fd912c` on int completed with `/tmp` (tmpfs) and `/opt` (nvme) confirmed distinct devices via `df`; no `EXDEV`; version listed by `opsctl releases sites`. |
| `R-6FE0-9WC4` (D3) | 2026-08-07 | 075afac1 | Interactive `ssh int 'sudo opsctl backup notify'` with no env sourcing resolved `IKIGENBA_BACKUP_BUCKET` and uploaded the S3 snapshot — `/etc/ikigenba/env` auto-loaded. |
| `R-MYS7-2H2R` (D4) | 2026-08-07 | 075afac1 | Real `opsctl deploy dashboard v0.23.0+ba5a1e61`: apex block rendered to `conf.d/dashboard.conf`, `nginx -t` + reload succeeded against the real cert; apex 200 (ssl_verify 0), `/services` 200, `/srv/sites/public/...` 200, protected `/srv/{crm,ledger,wiki,telemetry}/mcp` all 401, no 502/503; authenticated MCP calls still mint. |
| `R-AXY7-K8GA` (D8) | 2026-08-07 | 075afac1 | Real `opsctl deploy sites v0.24.0+d5fd912c`: unit `active`, loopback health 200, anonymous fetch of two published public sites (`dnd-rules`, `world-building-006`) returned 200 through nginx. |
| `R-B0E0-BRXO` (D9) | 2026-08-07 | 075afac1 | `opsctl restore sites` on int from the pre-deploy snapshot (`sites-v0.23.0+823310ac.20260807T170926Z`): unit `active`, `state/` and `state/www` owned `sites:sites`, anonymous public-site fetch 200. |
| web group access | not yet recorded | — | Run both positive reads and both negative disclosure checks above. |
