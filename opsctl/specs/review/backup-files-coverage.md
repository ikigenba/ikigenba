# D12 author coverage handoff

Independently verified completed scope; see linked whole-scope and correction verdicts. Inputs are unchanged story files
`specs/stories/backup.md` lines 1–369 and 490–530, plus certificate renewal intro
`specs/stories/certificates.md` lines 17–29. Stable shared boundaries: D08 verified
by apps-model-verification; D11 verified positive period subset. D01 create-only PutObject and ErrAlreadyExists are landed as R-AOUN-W2JU and
R-AQ2K-9UAJ; D12 R-ZMEX-3VL4 agrees with that shared contract.

| Source criteria | Requirements / ownership |
| --- | --- |
| BACKUP-001–003 host prefix, service/host separation | R-DAD6-H3N5, R-MNA5-UT7A, R-DCSZ-8N4J; host archive D13, IAM external |
| BACKUP-004 retire orchestration | D13 owns; consumes R-D7XD-PK5R, R-D95A-3BWG, R-ZMEX-3VL4 |
| BACKUP-005–008 top usage/config vocabulary | D02/D11/D13/D14; D12 consumes service-file and host-file period keys |
| BACKUP-009 file walk rather than transactional DB backup | R-DCSZ-8N4J, R-DE0V-MEV8 |
| BACKUP-010–013 DB replication ownership/exclusion | D11, R-DCSZ-8N4J |
| BACKUP-014 backup does not stop units | R-DE0V-MEV8; restore outage D14 |
| BACKUP-015–020 restore window/clock discussion | D14 owns restore; R-DE0V-MEV8 acknowledges file/database clock separation |
| BACKUP-021 service directories/cache/generated exclusions | R-DCSZ-8N4J; host data D13 |
| BACKUP-022–025 discovery/database/no-manifest ordinary state | D08, R-MNA5-UT7A, R-DCSZ-8N4J |
| BACKUP-026 init replication/timers/renewal | R-FJVO-5X9J through R-FSEY-UBGE; D05 init ordering, D11 replication |
| BACKUP-027 regeneration and BACKUP-028 retention | D11; file backup no-delete/lifecycle R-DE0V-MEV8 |
| BACKUP-029–038 both help forms, exact bytes, any user, no effects | R-DIWH-5HU0 |
| BACKUP-039–048 scheduled/all backup, ordered report, DB exclusions, same timestamp, append only, rerun | R-FNJD-B8HM, R-D7XD-PK5R, R-D95A-3BWG, R-MNA5-UT7A, R-DCSZ-8N4J, R-DE0V-MEV8, R-DGGO-DYCM, R-ZMEX-3VL4 |
| BACKUP-049–058 named backup and no other service reads | R-MNA5-UT7A, R-DGGO-DYCM |
| BACKUP-059–068 failed service read, continue other services, report status, DB unaffected | R-DF8S-06LX, R-DGGO-DYCM, R-DE0V-MEV8 |
| BACKUP-069–077 missing named service diagnostic/no effects | R-MNA5-UT7A, R-DGGO-DYCM |
| BACKUP-078–086 missing prefix/region, no service/object reads | R-DAD6-H3N5, R-DGGO-DYCM; host/restore shared diagnostic owners D13/D14 |
| backup.md:490–530 zero or absent periods; pairs still exist; manual services; renewal always on; config+init change | R-FMBG-XGQX, R-FNJD-B8HM, R-FOR9-P08B, R-FPZ6-2RZ0; no declared DB/all-zero prefix empty spans D11 period issue |
| certificates.md:17–29 renewal root/twice daily/random/persistent/always on/mask package timer | R-FMBG-XGQX, R-FOR9-P08B, R-FR72-GJPP |
| Supporting structural/error/grammar invariants | R-FJVO-5X9J, R-FSEY-UBGE, R-D7XD-PK5R, R-D95A-3BWG, R-DHOK-RQ3B |

## Review notes

18 requirements authored; service backup and generated schedules kept in one
bounded document. D12 exports Files, FileResult, SetupTimers. No source, tests,
module requirements, build/check operations, commits or host mutations performed.
Consumer tasks are in `backup-files-consumers.md` and include interpretation of
`etc/env` inclusion. Root coordinated create-only PutObject; no per-service
collision retry is designed because it would split a shared retirement timestamp.
Reserved `host`/`deploy` service directory names are rejected to prevent collision
with host backup or deployment object namespaces; ordinary backup directory names
otherwise do not inherit the more restrictive install name policy.

Local design-id gap check: all current D12 ids are absent from the project's
existing test-id set and therefore additions. No previous D12 existed. One draft
requirement was replaced while writing (R-DBL2-UVDU -> R-N0YJ-VRTI -> R-MNA5-UT7A) without reusing
its id; its text is no longer normative. External observations and cloud adapter
approval remain central issues, not claims of completed runtime validation.

## Correction author handoff — verified subsequently

Replaced R-N0YJ-VRTI with R-MNA5-UT7A. Discovery may read manifests for reserved names; those services fail before archive construction, further data reads or cloud-prefix access. Explicit invalid operands fail before service access. Ordinary directory names retain D08 discovery semantics. Added mixed-service and explicit-reserved consumer cases.


## Final local verification

Fresh correction verifier passed completed scope; see `apps-backup-corrections-verification.md` and preceding whole-scope verification reports. Remaining named product/evidence issues are unchanged. Earlier pending labels are historical author handoff status, superseded by this verdict.
