# Install coverage ledger (author claims; independent verification required)

Source: `specs/stories/apps.md`, read in this worktree. Each `all` row includes its command, output, exit/streams, preconditions and postconditions; subrows separate first-install outcomes. Line ranges below are advisory; heading names and criterion labels identify the source.

| Criterion | Source lines | Requirements | Scope / open decision |
|---|---|---|---|
| INTRO-object-role | 3–9 | R-H3LD-O3WP, R-OT2G-XXW5, R-OXY2-H0UX, R-P1LR-MC30 | Covered installation; uninstall/restart/status assigned D10. |
| INTRO-name | 11–15 | R-OT2G-XXW5, R-P6HD-5F1S | D08 defines label/reserved-name rule. |
| INTRO-help | 17–24 |  | D02 top-level help; D10 action contracts. |
| INTRO-region | 26–30 | R-H3LD-O3WP | Region passed to injected cloud. |
| INTRO-layout-version | 32–35 | R-OT2G-XXW5, R-P1LR-MC30 | Direct story schema, live binary version. |
| INTRO-manifest | 36–52 | R-OT2G-XXW5, R-OVI9-PHDJ, R-OWQ6-3948, R-P0DV-8KCB | D08 parse model; D11 database meaning. |
| INTRO-default | 54–55 | R-OUAD-BPMU, R-P0DV-8KCB | D06 routing covers apex/404. |
| INTRO-integrations | 57–63 | R-P0DV-8KCB | Manifest regeneration, before app start. |
| INTRO-secret-boundary | 65–67 | R-H3LD-O3WP, R-OVI9-PHDJ, R-OWQ6-3948 | Host-role client and exact parameter; remote IAM scope observation pending. |
| INTRO-app-schema | 69–77 | R-OXY2-H0UX, R-BGS4-B6XD, R-P1LR-MC30 | No migrations/seeding; app may create own state. Restore-before-start assigned D13. |
| INSTALL-help-all | 79–122 | R-OO6V-EUXD | Exact bytes, aliases, all users, streams, exit, no change. |
| INSTALL-fresh-command-streams | 124–154 | R-OPER-SMO2, R-P2TO-03TP, R-P41K-DVKE, R-P59G-RNB3 | Service/status equality unresolved output issue. |
| INSTALL-fresh-preconditions | 153–161 | R-H3LD-O3WP, R-OT2G-XXW5, R-OVI9-PHDJ | Init readiness is scenario precondition, not rerun by install. |
| INSTALL-fresh-files | 163–166 | R-OXY2-H0UX | Complete bin/etc/share replacement; state/cache untouched. |
| INSTALL-fresh-env | 167–171 | R-OVI9-PHDJ, R-OWQ6-3948 | 0600, all requested keys/plain settings/PORT, no extras/leaks. |
| INSTALL-fresh-unit | 172–175 | R-BGS4-B6XD, R-P1LR-MC30 | Nonroot account, working directory/env, restart policy, enable/reload. |
| INSTALL-fresh-nginx | 176–178 | R-P0DV-8KCB, R-P2TO-03TP | D06 guarantees actual route config and reload. |
| INSTALL-fresh-replication | 179–184 | R-P0DV-8KCB, R-P2TO-03TP | D11 config, restart only changed, before app. |
| INSTALL-fresh-version-other-apps | 185–186 | R-OXY2-H0UX, R-P1LR-MC30 | Live active/version, unrelated apps unchanged. |
| INSTALL-upgrade-all | 188–231 | R-OXY2-H0UX, R-P0DV-8KCB, R-P1LR-MC30, R-P2TO-03TP, R-PA52-AQ9V | Same database no replication restart; path change restarts; state preserved. Exact seven-line claim blocked output issue. |
| INSTALL-default-all | 232–269 | R-OUAD-BPMU, R-P0DV-8KCB, R-P2TO-03TP | Default report/routing; no DB unchanged. Fetch omission blocked output issue; other-name404 D06. |
| INSTALL-second-default-all | 270–304 | R-OUAD-BPMU, R-P41K-DVKE, R-P7P9-J6SH | Exact diagnostic exit1 after fetch/file; no writes; same-app default allowed; other default must first be removed. |
| INSTALL-missing-secret-all | 305–335 | R-OVI9-PHDJ, R-P41K-DVKE, R-P7P9-J6SH | Exact key/parameter diagnostic exit1, missing parameter equivalent, no writes. |
| INSTALL-not-app-all | 336–364 | R-OT2G-XXW5, R-P41K-DVKE, R-P6HD-5F1S, R-P7P9-J6SH | Absent object no stdout; missing/malformed manifest after fetch; exit2, no change. |
| INSTALL-reserved-name-all | 365–391 | R-OT2G-XXW5, R-P41K-DVKE, R-P6HD-5F1S | Exact name diagnostic, DNS failures, exit2, no change. |
| INSTALL-start-failure-all | 392–436 | R-P0DV-8KCB, R-P7P9-J6SH, R-P8X5-WYJ6, R-PA52-AQ9V | Journal quoted, exit1, no rollback; fetch presence blocked output issue. Status failure row D10. |
| INSTALL-argument-failures-all | 437–464 | R-OPER-SMO2 | No/many/invalid URI exact diagnostic and guidance exit2 no change; root D02. |

## Change and gap handoff

D09 adds 22 requirements. The initial configuration requirement was replaced (R-OQMO-6EER → R-H3LD-O3WP), and one initial account requirement was superseded while authoring (R-OZ5Y-USLM → R-BGS4-B6XD); no existing design requirement was edited in place. The canonical design/test grep must show the 22 current D09 IDs as additions; root owns the repository-wide mechanical gap. No implementation, tests, gates, check, build, commit or push was performed.

Local D09 gap recomputation: 22 additions, 0 matching test IDs. Replaced draft IDs occur in no existing tests.

## Correction author handoff — fresh verification pending

Added R-MH6N-XYHT (file report before default/secret checks, callback failure stops) and R-20N5-WTQH (install-wide prohibition on deliberately emitting secret/plain environment values). Existing journal quoting requirement is unchanged; externally logged environment values remain unresolved in install-journal-secret.md. INSTALL-second-default-all, INSTALL-missing-secret-all, INSTALL-fresh-env and upgrade inheritance now have explicit coverage. Consumer examples include writer failure and the unresolved sensitive-journal branch.


## Final local verification

Fresh correction verifier passed completed scope; see `apps-backup-corrections-verification.md` and preceding whole-scope verification reports. Remaining named product/evidence issues are unchanged. Earlier pending labels are historical author handoff status, superseded by this verdict.
