# D12 independent verification

Fresh verifier `/root/apps_backup/backup_files_verify` reviewed all 18 requirements, all scoped backup/help/timer story blocks and consumers against D01/D08/D11. Read-only, no inherited author history.

Verdict: one correctable defect; remainder passes.

## Correction needed

R-N0YJ-VRTI says reserved host/deploy are rejected before accessing that service during all-service backup, but D08 Discover reads each manifest. Narrow all-service guard to archive/data/cloud-prefix access after discovery, explicitly permit discovery manifest reads. Explicit invalid operands may still reject before service access. Exact lowercase namespace collision protection is appropriate, blanket install-name rules unnecessary.

## Verified criteria

| Scope | Verdict/evidence |
|---|---|
| Prefix and source separation | PASS R-DAD6-H3N5, R-DCSZ-8N4J |
| FileResult/Files retirement sharing | API PASS; retirement behavior D13 |
| Whole etc/state, four database exclusions | PASS R-DCSZ-8N4J, R-DE0V-MEV8 with D11 DatabaseFiles |
| No units/replication/retention/local mutation | PASS R-DE0V-MEV8 |
| Generated external files excluded, etc/env included | PASS explicit entire etc and restore EnvironmentFile justify root interpretation |
| Discovery/data-only services/manifest errors | PASS except reserved pre-access contradiction |
| Backup help both forms exact bytes and effects | PASS R-DIWH-5HU0 |
| All-service sorted reports/exclusions/timestamp | PASS R-D95A-3BWG, R-DCSZ-8N4J, R-DE0V-MEV8, R-DGGO-DYCM, R-ZMEX-3VL4, except reserved pre-access |
| Named service no other reads | PASS R-N0YJ-VRTI, R-DGGO-DYCM |
| Per-service unreadable failure continues, no partial object | PASS R-DF8S-06LX |
| Missing service exact failure/no effects | PASS R-N0YJ-VRTI/R-DGGO-DYCM |
| Missing prefix/region before service/cloud access | PASS R-DAD6-H3N5/R-DGGO-DYCM |
| Zero/unset file timer disablement/manual services | PASS R-FMBG-XGQX,R-FNJD-B8HM,R-FOR9-P08B,R-FPZ6-2RZ0 |
| Renewal root/twice-daily/random/persistent/always enabled/mask | PASS R-FJVO-5X9J through R-FSEY-UBGE |
| Exact APIs and full consumer tasks | PASS |
| Create-only shared cloud guarantee | PASS R-ZMEX-3VL4 matches landed D01 R-AOUN-W2JU/R-AQ2K-9UAJ |
| Format/boundaries/no unsupported dependencies | PASS |

Coverage artifact needs stale forthcoming D01 correction note updated and final post-correction verification recorded. Existing cloud approval/external observations and D11 DB zero/unset period issues remain. No source/test/build/check/commit performed.
