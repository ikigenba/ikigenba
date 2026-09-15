# opsctl draft review

## Consumer usage

Start with the [operator workflows](review/consumer-workflows.md): configure/init, install/manage, backup/restore, and release installation. Detailed examples and failure branches are linked there. These are proposed contract exercises, not executed deployment commands.

## Result

**Partial draft: all feasible authoring, correction and independent verification completed.** Every one of the nine story groups is represented across 16 design files (D00 is a non-normative scope guide; D01–D15 contain contracts). Nine product-decision issue files remain, plus external observation obligations. No implementation or baseline-check operation was performed.

Input commit: `1e9b63f0254d153fa7a041e86649cfcbb994bfb4`. [Source inventory](review/source-inventory.json) records every source file hash and all 111 section locators. Introductions, command/help/output blocks, preconditions and postconditions are included in each scope's finer-grained ledger. All source hashes are unchanged.

## Coverage reconciliation

“Verified” below means the supplied scenario is covered by independently reviewed normative contracts in their stated domain. Shared unresolved adapter/default/evidence limits still apply. “Partial” means a specific input outcome depends on a linked decision. Extra failure policies outside the example scenarios are tracked separately; scenario counts alone do not establish design completeness. No criterion was excluded by the user.

| Story group | Sections | Verified | Partial | Design | Current evidence |
|---|---:|---:|---:|---|---|
| bootstrap | 7 | 7 | 0 | D01–D02 | [early verification](review/early-verification-final.md) |
| config | 11 | 11 | 0 | D03 | [early verification](review/early-verification-final.md) |
| dns | 13 | 12 | 1 | D04 | [early verification](review/early-verification-final.md) |
| init | 6 | 4 | 2 | D05 | [early verification](review/early-verification-final.md) |
| nginx | 7 | 7 | 0 | D06 | [webhost full review](review/webhost-final-verification.md) |
| certificates | 8 | 8 | 0 | D07 | [webhost full review](review/webhost-final-verification.md) |
| apps | 25 | 21 | 4 | D08–D10 | [apps/backup handoff](review/apps-backup-summary.md), [app integration](review/integration-apps.md) |
| backup | 26 | 18 | 8 | D11–D14 | [backup integration](review/integration-backup.md) |
| release | 8 | 8 | 0 | D15 | [release verification](review/release-verification.md) |
| **Total** | **111** | **96** | **15** | | |

Finer-grained counts retain their local granularity rather than being misleadingly added together: early bootstrap/config 127 cells (123 verified, 4 contextual/not-applicable); DNS 93 (91 verified, 2 partial); init 28 source/boundary rows; webhost 25 story rows plus structural/Write integration criteria; release 39 criteria (38 normative, one external-caller context); ground 12/12. Apps/backup source-block counts are inventories, not counts of independent acceptance outcomes. Their complete per-scope criterion verdicts are linked from the handoff.

The former root-owned init restore-regeneration row is now closed by [backup integration](review/integration-backup.md) and [webhost replay](review/webhost-integration-verification.md). The broader app stdout conflict in the initial integration report is superseded by the same passing replay. Final help-key corrections supersede old help requirement references: [current mapping](review/help-key-integration.md) and [passing fresh verification](review/help-key-verification.md).

## Integration and ground

- [Shared D00/D01 verification](review/boundaries-verification-2.md): public seams, acyclic ownership, root/process isolation, create-only uploads, and permanent original IDs.
- [Init integration](review/early-init-verification-final.md): ten-key config task, certificate → nginx → replication → timers, current inputs and failures.
- [App integration](review/integration-apps.md) with [final correction replay](review/webhost-integration-verification.md): install/removal/status, metadata, retained data and report preservation.
- [Backup integration](review/integration-backup.md): archive membership, independent clocks, restore callbacks, reserved unit guards, host restoration, retirement timestamps and collisions.
- [Release](review/release-verification.md) and [ground](review/ground-verification.md): installer origin/version behavior, exact gates/test set/commit convention, isolated unprivileged installer tests.

## Unresolved product decisions

| Issue | Affected criteria/scopes | Needed answer |
|---|---|---|
| [Cloud adapter and secret encoding](issues/cloud-adapter-approval.md) | D01 production wiring; install and every cloud backup/restore | Approve proposed S3/SSM direct modules and establish the parameter encoding. [Concrete module proposal](review/cloud-adapter-proposal.md). |
| [Replication defaults](issues/backup-replication-periods.md) | Init ready/retry; database-bearing backup model; install/restore regeneration | Define zero/unset database/WAL periods with declared databases and whether a host without a backup prefix can initialize/install databases. Invalid present values already reject. The configured all-zero/no-database case is now covered. |
| [Install output](issues/app-install-output-conflicts.md) | Apps first install/upgrade/default/start failure; install/status relationship | Decide whether every install prints fetch, and whether three-value service reports remain distinct from four-column status. |
| [Sensitive journal output](issues/install-journal-secret.md) | Install startup failure with logged environment values | Choose preservation, redaction or suppression when external journal output contains those values. |
| [ACME existing TTL](issues/dns-acme-existing-ttl.md) | DNS auth introduction/postcondition for existing non-60 TXT sets | Preserve existing TTL or set it to 60 while keeping all values. |
| [Nginx reload failure](issues/nginx-reload-failure.md) | Extended apply failure after successful config test | Retain or restore the tested file, and specify any recovery reload. |
| [Retirement final sync](issues/retire-final-database-guarantee.md) | Retire help/success/unreadable-service guarantee; related uninstall sync | Resolve timeout/fallback contradiction and establish how successful final synchronization is known. |
| [Restore database removal](issues/restore-database-removal.md) | Mechanisms/no-database restore when current manifest has a DB and incoming manifest removes it | Define Litestream stop/regenerate/restart behavior and reporting for that transition. |
| [Restore retry intent](issues/restore-retry-inactive.md) | Retry after files restored but database replica missing | Preserve current inactivity or define a recovery-intent mechanism/explicit restart that distinguishes failed restore from deliberate inactivity. |

These issues isolate dependent outcomes; unrelated behavior is authored and verified. Alternative proposals remain outside normative design. No assumption is labeled user-approved.

## External observations

[Consolidated obligations](issues/external-observations.md) link the detailed [webhost](issues/webhost-external-observations.md) and [release](issues/release-external-observations.md) observations. [Environment evidence](review/environment-observations.md) records host paths/versions and successful synthetic archive round-trips; [SQLite observations](review/apps-lifecycle-observations.md) support persistent journal-mode reporting. Litestream is absent on dev; nginx was inactive; sample release HEAD requests returned 404. Help grammar is not proof of production behavior. Cloud/CA/DNS/replication/recovery behavior still needs the specified live observations before check-spec.

## Implementation handoff

[Canonical gap](review/implementation-gap.md): **364 design IDs, 84 test IDs, 42 matched; 322 adds and 42 removals.** Relative to original design, 319 IDs are new and 39 retired; the difference from the test gap reflects preexisting mismatches. Full sets are in [implementation-gap.json](review/implementation-gap.json).

Authoring checks found no duplicate declarations, malformed normative sections, changed retained original requirement texts, changed story hashes, or source/test/module edits. `git diff --check` passed. No implementation gates were run. No build-spec, check-spec, commit, installation or publication was performed.
