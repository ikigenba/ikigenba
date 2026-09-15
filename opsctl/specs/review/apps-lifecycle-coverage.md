# App lifecycle source coverage (author report)

Scope: `specs/stories/apps.md` introductory lifecycle claims and lines 465–end.
Independent verification is pending; this ledger is an author mapping, not a coverage verdict.

Shared intro: lines 5–9 (uninstall retains state; restart changes no installed disk state; host facts) map to R-M4I7-QLDO, R-M85W-VWLR, R-MBTM-17TU, R-MFHB-6J1X, R-MGP7-KASM. D08 owns discovery/model; D02 owns top-level usage and action root refusal.

| Source criterion | Outcome and owning requirement ids |
|---|---|
| apps.md:465 — An operator asks what `uninstall` can do | uninstall help exact bytes, any-user success, no changes. R-LUR0-OFG4, R-LYEP-TQO7 |
| apps.md:502 — `opsctl` is installed on the host. | R-LUR0-OFG4, R-LYEP-TQO7 |
| apps.md:506 — Nothing has changed. | R-LUR0-OFG4, R-LYEP-TQO7 |
| apps.md:508 — An agent uninstalls an app | uninstall successful and inactive/failed unit cases, ordering, preserved state/remote data/other apps, resulting status/backup. R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:538 — `host.name` is set. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:539 — `crm` is installed and its unit is `active`. Its manifest declares a `[database]` at `state/crm.db`, and `litestream.service` is replicating it. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:544 — `ikigenba-crm.service` is inactive and disabled, `/etc/systemd/system/ikigenba-crm.service` is gone, and systemd has been reloaded. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:547 — `/opt/crm/` holds `state/` and nothing else. `bin/`, `etc/` with its `env` file, `share/`, and `cache/` are gone. Nothing under `state/` was read or written: the database, its `-wal` and `-shm`, and litestream's metadata directory are as the service left them. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:551 — `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded: `crm.ikigenba.dev` answers 404 under the host's wildcard block. Had `crm` been the default app, the line would have read `crm.ikigenba.dev, ikigenba.dev removed` and the apex would answer 404 again. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:555 — `/etc/litestream.yml` has been regenerated from the manifests left under `/opt` and no longer names `/opt/crm/state/crm.db`. Because the file changed, `litestream.service` was restarted. It was stopped after the service, so no writer was open and its shutdown sync shipped every frame it held: the replica under `<backup.s3_uri>crm/` holds `crm.db` as of the last committed transaction. Every other declared database paused for the restart and is replicating again. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:562 — `status` shows `crm - - -`: a service with a `state/` and no manifest, the shape a restore into a fresh host leaves. `opsctl backup` now tars the whole of `state/`, the quiet database included, because no manifest declares it. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:565 — `/ikigenba/<host.name>/crm` and the object under `<backup.s3_uri>deploy/` are untouched. Installing `crm` again lands over its `state/` exactly as a deploy over a restore does. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:568 — No other app on the host has changed. | R-M0UI-LA5L, R-M22E-Z1WA, R-M3AB-CTMZ, R-M4I7-QLDO, R-M5Q4-4D4D, R-M6Y0-I4V2, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:570 — An operator uninstalls the host's default app | default routes removed, unchanged replication bytes and unit, retained state. R-M4I7-QLDO, R-M5Q4-4D4D, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:596 — `dashboard` is installed, its manifest sets `default = true`, and it declares no database. | R-M4I7-QLDO, R-M5Q4-4D4D, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:601 — `https://ikigenba.dev` and `https://dashboard.ikigenba.dev` both answer 404. The host has no default app until an install brings one. | R-M4I7-QLDO, R-M5Q4-4D4D, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:603 — `/etc/litestream.yml` is byte for byte as it was and `litestream.service` was not restarted. | R-M4I7-QLDO, R-M5Q4-4D4D, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:605 — `/opt/dashboard/` holds `state/` and nothing else. | R-M4I7-QLDO, R-M5Q4-4D4D, R-M85W-VWLR, R-M9DT-9OCG |
| apps.md:607 — An operator uninstalls an app that is not installed | uninstalled and absent distinctions, no effects. R-LZMM-7IEW |
| apps.md:630 — `/opt/gmail/` holds a `state/` and no `bin/gmail`, and there is no `ikigenba-gmail.service`; or `/opt/gmail/` does not exist. | R-LZMM-7IEW |
| apps.md:635 — Nothing has changed. | R-LZMM-7IEW |
| apps.md:637 — An operator runs `uninstall` with no app, or more than one | uninstall operand diagnostics. R-LYEP-TQO7 |
| apps.md:658 — `opsctl` is running as root. | R-LYEP-TQO7 |
| apps.md:662 — Nothing has changed. | R-LYEP-TQO7 |
| apps.md:664 — An operator asks what `restart` can do | restart help exact bytes and any-user no effects. R-LVYX-276T |
| apps.md:691 — `opsctl` is installed on the host. | R-LVYX-276T |
| apps.md:695 — Nothing has changed. | R-LVYX-276T |
| apps.md:697 — A developer restarts an app | restart new process and unchanged installed configuration, untouched units/routing/replication. R-MALP-NG35, R-MBTM-17TU, R-MD1I-EZKJ |
| apps.md:718 — `crm` is installed. Its unit may be `active`, `inactive`, or `failed`. | R-MALP-NG35, R-MBTM-17TU, R-MD1I-EZKJ |
| apps.md:722 — `ikigenba-crm.service` is `active` and its main process is a new one. | R-MALP-NG35, R-MBTM-17TU, R-MD1I-EZKJ |
| apps.md:723 — Nothing under `/opt/crm/` or `/etc/` was written. The environment systemd gave the new process is `/opt/crm/etc/env` as the last install wrote it. | R-MALP-NG35, R-MBTM-17TU, R-MD1I-EZKJ |
| apps.md:725 — No unit was enabled or disabled, nginx was not reloaded, and `litestream.service` was not touched: the app closed and reopened its database, and litestream never stopped watching it. | R-MALP-NG35, R-MBTM-17TU, R-MD1I-EZKJ |
| apps.md:728 — No other app on the host has changed. | R-MALP-NG35, R-MBTM-17TU, R-MD1I-EZKJ |
| apps.md:730 — A developer restarts an app whose service will not come back | restart failed service, quoted journal, retained failed state and disk state. R-ME9E-SRB8, R-MGP7-KASM, R-MHX3-Y2JB |
| apps.md:754 — `crm` is installed and its binary exits at start. | R-ME9E-SRB8, R-MGP7-KASM, R-MHX3-Y2JB |
| apps.md:758 — The unit is `failed`, and `status` shows `crm v0.1.0 failed wal`. Nothing on disk changed and nothing was rolled back: the host is left where an operator can look at it. | R-ME9E-SRB8, R-MGP7-KASM, R-MHX3-Y2JB |
| apps.md:762 — An operator restarts an app that is not installed | restart absent/uninstalled and operand diagnostics. R-LYEP-TQO7, R-LZMM-7IEW |
| apps.md:783 — `/opt/gmail/` holds a `state/` and no `bin/gmail`, and there is no `ikigenba-gmail.service`; or `/opt/gmail/` does not exist. | R-LYEP-TQO7, R-LZMM-7IEW |
| apps.md:788 — Nothing has changed. | R-LYEP-TQO7, R-LZMM-7IEW |
| apps.md:790 — An operator asks what `status` can do | status help exact bytes, any-user no effects. R-LX6T-FYXI |
| apps.md:825 — `opsctl` is installed on the host. | R-LX6T-FYXI |
| apps.md:829 — Nothing has changed. | R-LX6T-FYXI |
| apps.md:831 — A developer asks what a host is running | status sorted host facts, actual binary version, failed units exit zero, database/no database. R-MFHB-6J1X, R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0, R-PWID-NYOW |
| apps.md:860 — Three apps are installed; `gmail`'s unit is `failed`. | R-MFHB-6J1X, R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0, R-PWID-NYOW |
| apps.md:861 — `crm`'s manifest declares a `[database]` and that database is in WAL mode. Neither of the others declares one. | R-MFHB-6J1X, R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0, R-PWID-NYOW |
| apps.md:866 — Nothing has changed. | R-MFHB-6J1X, R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0, R-PWID-NYOW |
| apps.md:872 — A developer asks what a host with no apps is running | empty host produces no output. R-MFHB-6J1X, R-MJ50-BUA0 |
| apps.md:889 — No directory under `/opt/` holds an `etc/` or a `state/`. | R-MFHB-6J1X, R-MJ50-BUA0 |
| apps.md:893 — Nothing has changed. | R-MFHB-6J1X, R-MJ50-BUA0 |
| apps.md:895 — A developer asks about a host holding a service opsctl did not install | state-only service retained with unknown fields. R-MFHB-6J1X, R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0 |
| apps.md:918 — `/opt/gmail/` holds a `state/` and no `bin/gmail`, there is no `ikigenba-gmail.service`, and its manifest declares no database. | R-MFHB-6J1X, R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0 |
| apps.md:923 — Nothing has changed. | R-MFHB-6J1X, R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0 |
| apps.md:925 — A developer asks about a host whose database stopped being replicated | non-WAL mode visible without changing it. R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0, R-PWID-NYOW |
| apps.md:955 — `crm`'s manifest declares a `[database]` and that database's journal mode is `delete`. | R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0, R-PWID-NYOW |
| apps.md:960 — Nothing has changed. status reads the journal mode and never sets it: putting the database back into WAL mode is the app's to do, not opsctl's. | R-MGP7-KASM, R-MHX3-Y2JB, R-MJ50-BUA0, R-PWID-NYOW |
| apps.md:967 — An operator gives status an argument | status operand diagnostic and no effects. R-LYEP-TQO7 |
| apps.md:987 — `opsctl` is running as root. | R-LYEP-TQO7 |
| apps.md:991 — Nothing has changed. | R-LYEP-TQO7 |

## Limits carried forward

- `specs/issues/app-install-output-conflicts.md`: source says service-line equality with status but supplies three-value restart versus four-column status. D10 preserves restart bytes; comparison remains unresolved.
- `specs/issues/external-observations.md` and `specs/issues/retire-final-database-guarantee.md`: Litestream final shutdown synchronization remains unobserved; D10 fixes the caller ordering and conditional intended result.
- `apps-lifecycle-observations.md` establishes persistent journal-mode observations and a standard-library implementation route without new runtime dependencies.
- Exact repeated sample stdout/stderr bytes are represented by the help literals and report/diagnostic templates; no consumer infers application versions from artifact labels.

## Public additions

`apps.UninstallHooks`, `Uninstall`, `Restart`, `StatusRow`, `Status`, `LifecycleError`. No changes to D08/D09/D11 shared declarations, no new import edges, and no source or tests changed.

30 new ids in D10. No existing requirement ids replaced. Consumers: `apps-lifecycle-consumers.md`.


## Final local scope status

Completed contracts passed independent verification. See the corresponding `*-verification.md` reports (D14 also `backup-restore-correction-verification.md`). Product/evidence issues remain explicitly scoped. Root owns the final cross-command help-key inventory correction and verification; historical author-pending labels above are superseded by those verdicts.
