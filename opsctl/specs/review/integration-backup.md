# Independent backup workflow integration

Verifier `/root/integration_backup`, 2026-09-14. Read-only review of all 26 backup sections, D01/D03/D06/D08/D10–D14, local usage/coverage, and corrected D14 nginx callback and reserved-unit guard.

**Pass for completed integration scope; no new corrective normative defects.** Partial draft because known policy and external evidence issues remain.

| # / source line | Story section | Verdict / evidence |
|---|---|---|
| 1 / 38 | Two mechanisms | Partial — exclusions/clocks agree; database-removal transition unresolved |
| 2 / 113 | What is backed up | Partial — discovery and ownership agree; replication defaults unresolved |
| 3 / 172 | Backup help | Pass — R-DIWH-5HU0 |
| 4 / 216 | All services | Pass — R-MNA5-UT7A, R-DCSZ-8N4J, R-DGGO-DYCM, R-ZMEX-3VL4 |
| 5 / 258 | One service | Pass — R-MNA5-UT7A, R-DGGO-DYCM |
| 6 / 287 | Unreadable service | Pass — R-DF8S-06LX, R-DGGO-DYCM |
| 7 / 320 | Missing service | Pass — R-MNA5-UT7A, R-DGGO-DYCM |
| 8 / 344 | Missing config | Pass — D12 R-DAD6-H3N5, D13 R-YBH6-P78Q, D14 R-FST6-H1RQ |
| 9 / 370 | Host help | Pass — R-YOW2-WOED |
| 10 / 418 | Host backup | Pass — R-YCP3-2YZF, R-YDWZ-GQQ4, R-YL8D-RD6A |
| 11 / 452 | Host certificate restore | Pass — R-YGCS-8A7I through R-YNO6-IWNO, R-Z2AZ-45K0 |
| 12 / 490 | Zero periods | Partial — file timers covered; replication/init zero policy unresolved |
| 13 / 531 | Retire help | Partial — exact help R-YZV6-CM2M passes; advertised sync guarantee unresolved |
| 14 / 577 | Final retirement | Partial — conditional completed path covered, sync protocol unresolved |
| 15 / 639 | Retirement unreadable service | Partial — reporting/continuation covered, preceding sync guarantee unresolved |
| 16 / 675 | Restore help | Pass — R-1M9V-TA5V; host.name key follows general key-list rule |
| 17 / 729 | Ordinary restore | Pass — source/stop/files/database/regenerate/start chain, R-1OPO-KTN9 |
| 18 / 783 | Point-in-time restore | Pass — R-FU12-UTIF, R-G1CH-5FYL, R-1JU3-1QOH |
| 19 / 829 | No earlier backup | Pass — R-FV8Z-8L94 |
| 20 / 860 | No database | Partial — stable no-database branch R-1IM6-NYXS passes; removal transition unresolved |
| 21 / 900 | Already stopped | Pass — R-G3S9-WZFZ preserves deliberate inactivity |
| 22 / 938 | No backups | Pass — R-FV8Z-8L94 |
| 23 / 962 | Files without replica | Partial — first failure covered, retry activation policy unresolved |
| 24 / 1018 | Never installed | Pass — R-G04K-RO7W, R-G2KD-J7PA, R-GERD-CX48; D10 status crm - - wal |
| 25 / 1068 | Restore grammar | Pass — R-GCBK-LDMU |
| 26 / 1096 | Host grammar | Pass — R-YQ3Z-AG52, R-YRBV-O7VR |

Totals: **18 pass, 8 partial**. Exact source sections are in source-inventory.json. External cloud/replication observations are separate limitations on all applicable runtime claims.

## Cross-document guarantees

D14 callback matches nginx.Write and preserves CLI-only cross-domain composition. Callback failure prevents starts; ValidateName guards every derived app unit. Database exclusion sets agree. File and database cutoff clocks are independent. Host restore captures source configuration/client before replacement. Retirement uses one timestamp and atomic create-only uploads. Restore runs no migrations or seeds, and data-only status reads restored declarations independently of binary/unit presence. Root consumer examples and detailed callback usage match declarations.

Historical author ownership locators require review-only cleanup: D13 is host/retirement, D14 service restore. Scope coordinator notified. No normative correction required by this review.
