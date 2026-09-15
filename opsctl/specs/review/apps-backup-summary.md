# Apps and backup coordinator handoff

## Complete consumer tasks

- App manifest/discovery: apps-model-usage.md
- Install and upgrade: apps-install-consumers.md
- Uninstall, restart, status: apps-lifecycle-consumers.md
- Replication setup/regeneration: backup-model-consumers.md
- File backup and timers: backup-files-consumers.md
- Host backup/restore and retirement: backup-host-consumers.md
- Service restore: backup-restore-consumers.md

## Scope and counts

All seven reserved scopes authored. Current requirement counts before root final help replacements (replacements do not change count): D08 13; D09 24; D10 30; D11 13; D12 18; D13 28; D14 26. Total152.

All152 requirements received independent whole-scope verification, with fresh correction verification where required. D14’s last verification leaves one all-read-keys help omission; root’s fresh help author/verifier owns the final shared D05/D09/D10/D14 help inventory. No local authoring remains unstarted.

Source inventory: apps25 headings/260 source blocks; backup26 headings/270 source blocks including every introduction, command/output, pre/post block. Block counts are inventory chunks, not independently completed outcomes. Fine-grained requirements maps live in each scope coverage review. Heading reconciliation: apps21 verified completed contracts/4 partial-output headings; backup18 verified/8 partial headings per root independent integration. External observations are separate from completed design verification. Uninstall ordinary scope includes conditional synchronization, with proof/failure guarantee not established.

## Independent evidence

apps-model-verification.md; apps-install-verification.md; apps-lifecycle-verification.md; backup-model-verification.md; backup-files-verification.md; apps-backup-corrections-verification.md; backup-host-restore-verification.md; backup-restore-correction-verification.md. Root owns final end-to-end integration reports.

SQLite header/fresh-reader observations in apps-lifecycle-observations.md resolve persistent wal/delete reporting without new dependencies. Root environment-observations.md records actual archive roundtrips and unavailable Litestream. No builds/tests/checks/commits performed; implementation gap remains parent-owned canonical report.

## Product decisions and evidence issues

- app-install-output-conflicts.md: install fetch-line variants and claimed equality with four-column status; apps headings124,188,232,392 plus intro relationship.
- install-journal-secret.md: direct intentional env-value disclosure prohibited; known secrets inside arbitrary external journal bytes need redaction-versus-verbatim decision. No redaction invented.
- backup-replication-periods.md: zero/unset database/WAL periods and absent-prefix policy; malformed/negative/overflow/present-invalid-prefix validation is completed, not blocked.
- retire-final-database-guarantee.md: excluded database cannot also be claimed timeout archive fallback; reliable final-sync proof missing. Normal ordering/conditional successful path complete.
- restore-retry-inactive.md: initial no-replica failure covered, later retry-start intent conflicts with deliberate-inactive preservation.
- restore-database-removal.md: old database→restored no-database transition conflicts with no-Litestream-action branch.
- Root cloud adapter approval and external observation issues remain; no new direct modules approved, no AWS CLI fallback assumed.

## Shared decisions

Story-provided archive/manifest is authorized public input, not inferred sibling internals. D08 owns shared discovery/model, CLI composes apps/nginx/backup, D11 owns replication. D12 includes etc/env under explicit whole-etc backup; generated nginx/units excluded. Atomic create-only cloud uploads preserve collisions, one sampled RFC3339Nano instant per operation and pinned clock for retirement’s common timestamp. D14 CLI NginxRegenerator invokes nginx.Write after restored data before starts, without reload, plus reserved-unit derivation guard. These are technical source interpretations, not claims of separate user approval.
