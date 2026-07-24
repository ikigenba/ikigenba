# Phase 51 — The committed parked front-door artifacts and their install runbook

*Realizes design Decision 13 (parked `default_server` front door). No dependency
on another pending phase.*

Adds the two static artifacts that answer non-apex hosts, plus the operator
procedure that installs them on the one box those domains resolve to. This phase
writes files; it does not touch the live box, and nothing it produces runs under
the test suite.

End state:

- **`parked/index.html`** at repo root — the bare-bones HTML5 page from D13,
  byte-for-byte as the Decision specifies it: doctype, `lang="en"`, utf-8 charset,
  the viewport meta, a `<title>` carrying the sentence, and a single `<p>` carrying
  the same sentence. No CSS, no favicon, no scripts, no robots directives.
- **`parked/nginx.conf`** at repo root — the server file from D13: a `:80` block
  and a `:443` block, both `default_server` on IPv4 and IPv6, `server_name _;`,
  the `:80` block carrying the ACME location rooted at `/var/lib/letsencrypt`
  before its HTTPS redirect, the `:443` block referencing
  `/etc/letsencrypt/live/parked/{fullchain,privkey}.pem` and serving
  `/var/www/parked`. No HSTS header.
- **A new `deploy.md` section** giving the complete install procedure in D13's
  order — `certbot certonly --webroot` with `--cert-name parked` and the nine
  `-d` flags, installing the two files root-owned `0644` under a `0755`
  `/var/www/parked/`, `nginx -t`, `systemctl reload nginx` — followed by the
  verification checklist D13's Verification section enumerates (two cert-validating
  parked names including `metaspot.org`, `ikigai-group.io` over HTTP with HTTPS
  still failing validation, the bare IP, the apex plus one `/srv/<svc>/` path still
  working, and `certbot certificates` showing two lineages).
- **The repo-root `CLAUDE.md` layout table** gains a `parked/` row, since it
  documents every top-level directory.

The existing `dashboard/etc/nginx.conf`, `opsctl`, `init-box`, the systemd units,
and the local dev front door under `nginx/` are **not** modified by this phase.

**Done when:**

This phase realizes a Decision that mints no requirement ids (D13 Verification:
its only falsifiable claim needs a real CA and a real nginx, so it is verified on
the box outside the loop). Its bar is therefore structural and deterministic —
every check below is a command whose result is exact:

- `go build ./...` succeeds and `go test ./...` from the repo root exits 0 — the
  suite is still green, confirming this phase touched no Go code.
- `parked/index.html` and `parked/nginx.conf` both exist.
- `grep -c "These aren't the droids you're looking for!" parked/index.html`
  returns exactly `2` (the `<title>` and the `<p>`).
- `grep -c '<!doctype html>' parked/index.html` returns exactly `1`.
- `grep -c 'default_server' parked/nginx.conf` returns exactly `4` (two `listen`
  directives per block, two blocks).
- `grep -c '/etc/letsencrypt/live/parked/' parked/nginx.conf` returns exactly `2`.
- `grep -c 'well-known/acme-challenge' parked/nginx.conf` returns exactly `1`.
- `grep -c 'Strict-Transport-Security' parked/nginx.conf` returns exactly `0`.
- `grep -c 'cert-name parked' deploy.md` returns exactly `1`, and each of the nine
  certificate domains appears in `deploy.md`:
  `for d in ikigenba.com ikigenba.dev logic-refinery.com logic-refinery.io logic-refinery.net logic-refinery.tv metaspot.net metaspot.org michaelgreenly.com; do grep -q -- "-d $d" deploy.md || exit 1; done`
  exits 0.
- `grep -c 'ikigai-group.io' parked/nginx.conf` returns exactly `0` — the excluded
  domain appears nowhere in the server file (it is reached only via
  `default_server`, never by name).
- `grep -c '| \*\*parked\*\*' CLAUDE.md` returns exactly `1`.
- `grep -c 'default_server' dashboard/etc/nginx.conf` returns exactly `0` — the
  apex block still claims no default, so the parked file is the only holder of
  the role and the two cannot conflict.

Going live is explicitly **not** part of this bar. The page is not serving when
this phase completes; the operator runs the new `deploy.md` section afterward.
