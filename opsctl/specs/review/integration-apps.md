# Independent app workflow integration

Verifier `/root/integration_apps`, 2026-09-14; read-only D01/D02/D06/D08–D12 and all 25 apps story headings, plus current restore interaction and root consumer examples.

## Result

Partial pending one correctable D06 output rule and the already-recorded product/evidence issues. No new unresolved user decision.

D06 R-FWBC-EQSE and R-FXJ8-SIJ3 incorrectly require empty stdout for all CLI callers on preparation/input failure. Install D09 R-P7P9-J6SH and uninstall D10 R-M9DT-9OCG preserve earlier completed reports. Narrow D06 empty stdout to standalone nginx commands, then independently reverify composed failures.

| Apps heading number | Independent verdict |
|---|---|
| 1 | Pass — exact install help / any-user help |
| 2 | Partial — normal install contract coherent; output equality, secret journal and cloud/replication issues |
| 3–4 | Partial — normal upgrade/default contracts coherent; fetch-output conflict |
| 5–8 | Pass — competing default, missing secret, malformed artifact, reserved name rejection and no mutation |
| 9 | Partial — failed start state/report contract coherent; fetch/secret-journal policies |
| 10–11 | Pass — install grammar and uninstall help; sync evidence limit carried |
| 12–13 | Partial pending D06 stdout correction; ordinary uninstall/default removal passes; final sync evidence remains |
| 14–25 | Pass — absent/invalid uninstall, restart/help/failure/absence, status/help/empty/data-only/non-WAL/invalid arguments |

Heading locators and exact per-criterion requirement mappings are in the app scope reviews and apps-heading-coverage.md. Evidence for install ownership/order: R-H3LD-O3WP, R-OXY2-H0UX, R-BGS4-B6XD, R-P0DV-8KCB, R-P1LR-MC30; removal R-M22E-Z1WA through R-M9DT-9OCG; restart R-MALP-NG35, R-MBTM-17TU, R-MD1I-EZKJ, R-ME9E-SRB8; status R-MFHB-6J1X, R-MGP7-KASM, R-MJ50-BUA0, R-MHX3-Y2JB, R-PWID-NYOW.

## Boundaries verified

- All host/cloud/model/hook/replication/nginx signatures agree; CLI composition avoids cycles.
- Installation orders unit publication, nginx, changed-only replication refresh, then app start; no migration or seeding work is assigned to opsctl.
- Uninstall's retained state remains discoverable/backed up; absence of manifest removes routing and replication membership. Entire quiet state is backed up afterward.
- Environment file backup inclusion is consistent with recovery. Deliberate value disclosure is prohibited; sensitive external journal handling remains an explicit issue.
- Restore callback uses write-only nginx regeneration. A restored database manifest can yield `app - - wal`; manifest-free retained state yields `app - - -`.
- Root app consumer sequence is valid after its positive-period setup; the requested uninstall synchronization limitation link has been added.

No edits, implementation, tests or gates performed by verifier. Fresh webhost integration correction/verifier report will supersede the specific stdout failure above.

## Final correction disposition

The sole stdout defect above is closed by [fresh webhost integration verification](webhost-integration-verification.md), which replays standalone and composed failures. The app heading reconciliation therefore returns to 21 verified / 4 partial; remaining partial headings are first install, upgrade, default install and failed startup. Final help-key IDs supersede historical help references through [help integration](help-key-verification.md).
