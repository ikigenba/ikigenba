# D11 author coverage and evidence

This author mapping has passed independent verification; see the verification files below. Source blocks use
[the coordinator inventory](backup-source-inventory.md). Every requirement in
D11 is new; there was no previous backup model or consumer API to preserve.

| Source criterion | D11 requirement or adjacent owner | Author result |
|---|---|---|
| BACKUP-001 heading | No behavioral criterion | Context |
| BACKUP-002 host-isolated S3 namespace | R-MKUD-39PW; D12/D13/D14 object prefixes; role enforcement external | Drafted path; IAM observation pending |
| BACKUP-003 service versus host trees | R-JJ94-707K; archive scopes D12/D13 | Shared ownership; archive behavior delegated |
| BACKUP-004 retirement final backup | D13 | Delegated |
| BACKUP-005–006 top-level help | D02 | Delegated |
| BACKUP-007–008 region/prefix/period keys | R-MKUD-39PW; file periods D12 | Bounded positive periods and malformed-input rejection drafted; zero/missing/empty policy unresolved |
| BACKUP-009 filesystem snapshot of etc/state | D12 | Delegated |
| BACKUP-010–011 SQLite resident replication | R-JJ94-707K, R-JRSE-VEEF, R-JVG4-0PMI | Continuous shared unit; external SQLite claims observation pending |
| BACKUP-012 preinstalled tool, PATH preflight | R-JJ94-707K; D05 PATH check | No installation; PATH check delegated |
| BACKUP-013 four database exclusions | R-JMWT-CBFN, R-JO4P-Q36C; D12 applies set | Shared exact paths drafted |
| BACKUP-014–015 restore outage and stop-before-replace | R-JWO0-EHD7; D14 full restore order | Shared restart boundary drafted |
| BACKUP-016 one unit, accepted all-database pause | R-JRSE-VEEF, R-JWO0-EHD7; D14 no other-service writes | Drafted |
| BACKUP-017 resume after WAL reset, closest at/before recovery | D14 restore protocol; external Litestream observation | Delegated and observation pending |
| BACKUP-018 loss during restore accepted | D14 failure semantics | Source risk context |
| BACKUP-019 independent files/database clocks | D14 selection | Delegated |
| BACKUP-020 no incremental mode | R-JJ94-707K; D12/D14 command grammar | Delegated command surface; external explanation context |
| BACKUP-021 service/host/db/cache/generated ownership | R-JPCM-3UX1, R-MKUD-39PW; D12/D13 archive member contract | Replication subset drafted; remaining members delegated |
| BACKUP-022 directory discovery | R-JPCM-3UX1, D08 Discover | Drafted |
| BACKUP-023–024 manifest SQLite engine/path | D08 Database/ParseManifest; R-JMWT-CBFN, R-JO4P-Q36C | Shared vocabulary consumed |
| BACKUP-025 database-free state wholly archived | R-JPCM-3UX1; D12 archive | Replication absent; archive delegated |
| BACKUP-026 init litestream then timers | R-JLOW-YJOY, R-JVG4-0PMI, R-JWO0-EHD7; D05 sequence/D12 timers | Drafted |
| BACKUP-027 regenerates changed manifests, unchanged unit, init-only enable | R-JKH0-KRY9, R-JT0B-9654, R-JU87-MXVT, R-JWO0-EHD7 | Drafted with restore outage distinction below |
| BACKUP-028 retention disabled, bucket expiry only | R-JRSE-VEEF | Config drafted; external no-delete proof pending |
| apps.md:57–63,175–183,190–194,223–230 replication install/upgrade | R-JPCM-3UX1, R-JT0B-9654, R-JWO0-EHD7; D09 | Shared portion drafted |
| init.md:22–30,123–140 setup and rerun | R-JLOW-YJOY, R-JVG4-0PMI, R-JWO0-EHD7; D05 | Shared positive-input portion drafted |
| Necessary failure/atomicity/environment constraints | R-JPCM-3UX1, R-JT0B-9654, R-JU87-MXVT, R-JVG4-0PMI; D01 host seam | Drafted |

## Boundary interpretation

BACKUP-027's unchanged-unit wording is reconciled with the explicit restore
examples (`backup.md:776–778,916–936`): an unchanged generated file causes no
additional reload/restart, but restore still stops and starts Litestream for the
files/database replacement. R-JWO0-EHD7 states this specific lifecycle. There is
no claim that restore leaves the running process untouched.

`Regenerate` uses the latest D03 store and D08 manifests. `SetupReplication` is
init-only; callers that manage restore outage or app restart use `Regenerate`.
D01's `host.CommandError` applies to process failures in SetupReplication and
consumer orchestration, preserving vendor output for CLI diagnostics.

## External evidence

Documentation read on 2026-09-14: [Litestream configuration reference](https://litestream.io/reference/config/),
sections Global replica defaults, Snapshots, Retention, and Database settings.
It documents top-level `region`, `sync-interval`, `snapshot.interval`,
`retention.enabled`, and `dbs[].path` with `replica.url`. It documents the default
metadata directory beside the database. These support D11's configuration and
path draft; they are documentation, not a live observation.

[Live environment observation](environment-observations.md) reports Litestream
absent on dev. Required before check-spec: observe the installed Litestream
accepting the generated file, honoring the configured periods and retention
policy, authenticating to the intended S3 prefix through the host role, and
restarting successfully with empty and nonempty database sets. Root owns the
consolidated external-dependency observation issue. Do not treat docs as proof.

## Unresolved input

[Replication period policy](../issues/backup-replication-periods.md) prevents a
complete contract for zero/unset periods and absent/empty prefixes, including
hosts whose accounts back nothing up. No arbitrary defaults have been minted.

## Correction author handoff — verified subsequently

Replaced R-JQKI-HMNQ with R-MKUD-39PW and added R-MM29-H1GL: valid periods are 1..9223372036 seconds, fitting a signed 64-bit nanosecond duration; malformed/negative/fractional/overflow periods and malformed present prefixes fail before writes. BACKUP-007–008 and necessary validation are covered. Zero/missing/empty periods and absent/empty prefix policy remain unresolved; invalid syntax is routine validation, not a user decision. No default chosen.


## Final local verification

Fresh correction verifier passed completed scope; see `apps-backup-corrections-verification.md` and preceding whole-scope verification reports. Remaining named product/evidence issues are unchanged. Earlier pending labels are historical author handoff status, superseded by this verdict.
