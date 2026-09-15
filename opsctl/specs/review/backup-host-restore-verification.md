# D13 / D14 independent verification

Fresh read-only verifier `/root/apps_backup/host_restore_verify` reviewed D13's28 and D14's23 requirements against complete scoped stories, intros, consumers and public boundaries.

D13 PASS completed contract; retirement final-sync guarantee remains specifically unresolved. D14 three correctable defects; completed unaffected branches pass.

## Required D14 corrections

- R-GDJG-Z5DJ exact help contains SERVICE`s typo and mismatched delimiters: remint exact literal.
- R-G9VR-TU5G prohibits nginx regeneration although init.md28–32 says restore regenerates both files. backup.md forbids reload only. Root settles regeneration without reload, CLI callback invokes shared nginx.Write before final starts, no backup→nginx import.
- R-FST6-H1RQ permits generated backup-host/backup-services/renew-certificate unit names, then R-FYWO-DWH7 derives units. Protect app-unit derivation with apps.ValidateName as D13 already does.

## Per-heading closure

| Heading source | Verdict |
|---|---|
| Host help370 | PASS R-YOW2-WOED |
| Host backup418 | PASS R-YBH6-P78Q,R-YCP3-2YZF,R-YDWZ-GQQ4,R-YF4V-UIGT,R-YL8D-RD6A,R-Z2AZ-45K0 |
| Host certificate restore452 | PASS R-YGCS-8A7I through R-YNO6-IWNO,R-Z2AZ-45K0 |
| Retire help531 | Exact help PASS; advertised sync unresolved |
| Final retirement577 | Conditional success PASS R-YSJS-1ZMG through R-YYN9-YUBX,R-Z3IV-HXAP; sync timeout/proof unresolved |
| Retirement service failure639 | PASS conditional sync; continue others/failed row/stopped units |
| Restore help675 | Correct help typo |
| Ordinary restore729 | Local PASS; nginx integration correction |
| At restore783 | PASS target vs recovered point R-FU12-UTIF,R-G1CH-5FYL,R-G9VR-TU5G |
| Too early829 | PASS R-FV8Z-8L94 no opt reads/effects |
| No database860 | PASS stable no-db; declaration removal unresolved |
| Already stopped900 | PASS deliberate inactive; retry intent unresolved |
| No backups938 | PASS no effects |
| No replica962 | Initial failure PASS; retry activation unresolved |
| Never installed1018 | PASS data-only/0755/no app unit/replication; D10 status owns report |
| Restore arguments1068 | PASS R-GCBK-LDMU |
| Host arguments1096 | PASS R-YQ3Z-AG52,R-YRBV-O7VR |

Complete consumer names/shapes match. Archive/source/root safety, current source-prefix capture before host config replacement, env preservation, create-only collisions and same retirement timestamp pass. No private algorithms/sibling internals found. All new modal ids canonical, transient mint history not independently reconstructible. Remaining genuine issues retire-final-database-guarantee.md, restore-retry-inactive.md, restore-database-removal.md, period policy/cloud adapter/external observations. No source/test/build/check/commit.
