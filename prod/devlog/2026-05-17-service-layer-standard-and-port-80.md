# 2026-05-17 — service-layer standard added; port 80 re-opened for ACME

## What

Two coupled changes:

1. **Port 80 re-enabled on the `biz` SG** (and in the "create a server"
   standard): ingress is now **80 + 443 from `0.0.0.0/0`**, 22 still
   admin-`/32` only. Applied to live prod as another `create_before_destroy`
   replace (new `sg-094251e8cf71e4ed3`, instance repointed in-place, old SG
   destroyed last, ~5s, zero drift after).
2. **New `### Service layer` section in `AGENTS.md`** (mirrored via the
   `CLAUDE.md → AGENTS.md` symlink) defining how a deployed app sits on a box:
   per-app layout, the `bin/{setup,deploy,backup,restore}` quartet, the
   launcher secrets pattern, the systemd unit shape, nginx+certbot, journald.

## Why these choices

**Port 80 is a deliberate, scoped partial revert — not a regression.** The
service standard adopts `ralph-scoops`'s proven nginx + certbot Let's Encrypt
setup, which validates via the **HTTP-01 challenge**. Let's Encrypt's
validators have no fixed CIDR, so 80 must be open to the world, and nginx uses
80 for the ACME path plus the 80→443 redirect. Alternatives were considered and
rejected: **TLS-ALPN-01** (443-only, would have preserved no-port-80) and
**DNS-01** (we own the Route53 zones) both work without port 80 but would have
diverged from the battle-tested prior art and, for DNS-01, required adding a
`route53:ChangeResourceRecordSets` grant to the instance role. The decision was
explicit: keep the proven web/TLS pattern, accept public 80. The security win
that actually mattered — SSH locked to the admin `/32` — is **retained**;
only the cosmetic "no 80" part of the earlier hardening is undone.

**Description change → SG replace, again — and that's now fine.** Re-adding the
80 rule plus an accurate description re-triggered the immutable-description
replace. Because `name_prefix` + `create_before_destroy` are now permanent on
the SG (see the SG-hardening devlog), the replace is safe and ~5s. We chose
accuracy of the canonical reference over avoiding churn: a pure rule-add would
have been in-place (no replace) but left the description wrong on the file every
future server mirrors. Correctness of the reference beats minimizing a
now-safe replace.

**Per-app, not a fixed `/opt/app`.** The earlier sketch leaned toward a fixed
`/opt/app`/`app` triplet for consistency with the hostname-independent
philosophy. Overruled deliberately: a box may eventually run more than one
service, so install root `/opt/<app>`, system user `<app>`, and unit
`<app>.service` are per-app. The hostname-independent philosophy still holds
where it counts (the shared `/metaspot/<env>/app-config` path and
`/etc/metaspot/env` are not per-app).

**Launcher pattern for secrets, explicitly not systemd-native.** Three options
were on the table: (A) root `ExecStartPre` → tmpfs `EnvironmentFile`,
(B) systemd credentials (`LoadCredential`), (C) a launcher wrapper that fetches
at every start. `ralph-scoops` already proved (C) in prod and its own comments
reject (A)/(B) ("no env file, no ExecStartPre ordering games"). Adopted (C):
the secret lives only in the launched process's environment, never on disk
(not even tmpfs), and rotation collapses to `systemctl restart`. Adapted to the
current standard, the launcher reads the **shared** `app-config` blob and
`jq`s out the app's own JSON key (rather than `ralph-scoops`'s dedicated
per-secret parameter, which died with it). A fail-fast smoke test before `exec`
is kept from the prior art so a bad/expired secret surfaces via systemd instead
of erroring silently every cycle.

**`backup`/`restore` promoted to first-class scripts.** The prefix-isolated
backups bucket only delivers its guarantee if every app uses it the same
disciplined way, so the quartet is `setup`/`deploy`/`backup`/`restore` — not
`provision`/`deploy` plus ad-hoc backup. Names are fixed across all apps so the
operational surface is identical everywhere; `setup` is the rename of
`ralph-scoops`'s `provision`.

## Tradeoffs accepted / deferred

- **Public port 80.** Accepted for ACME. It only ever serves the challenge and
  a 301 to 443; no app content is exposed on it.
- **AWS CLI not in the base AMI.** AL2023 does not ship `aws-cli`; `bin/setup`
  must `dnf install` it (and `jq`) explicitly. Documented in the standard so a
  fresh box's launcher isn't missing its fetch tool — `ralph-scoops`'s
  provision happened not to install it and relied on it being present.
- **Scripts are documented contract, not yet code.** `AGENTS.md` specifies the
  four scripts' behavior; no canonical implementations are checked into
  `metaspot` yet (it is the infra repo). Whether to ship reference
  `bin/*` here vs. let each app repo own them is deferred.
- **No systemd sandboxing baseline** (`ProtectSystem`, `NoNewPrivileges`, …)
  was mandated — `ralph-scoops` had none and "varies by app" was the stated
  expectation. Revisit if a hardening baseline becomes worth standardizing.
