# D11 independent verification

Fresh read-only verifier `/root/apps_backup/backup_model_verify` reviewed all 12 requirements against backup intro/model, apps regeneration, D01/D08 and the three consumer examples.

Verdict: PASS for specified positive-input scope. No blocking drafting defect. Missing/zero replication period and absent-prefix policies remain unresolved, and external behavior is unobserved.

| Criterion group | Verdict and evidence |
|---|---|
| Host-specific namespace | Scoped PASS R-JQKI-HMNQ; IAM isolation observation pending |
| Separate file/database ownership | Scoped PASS R-JJ94-707K, R-JPCM-3UX1, R-JQKI-HMNQ; file archive downstream |
| Region/prefix/positive intervals | PASS R-JQKI-HMNQ; zero/missing policy issue |
| Resident shared replication | PASS R-JJ94-707K, R-JRSE-VEEF, R-JVG4-0PMI; preflight D05 |
| Four exact database exclusions | PASS R-JMWT-CBFN, R-JO4P-Q36C |
| Stopped restore regeneration/restart | Scoped PASS R-JWO0-EHD7; detailed restore D14 |
| Discovery including data-only databases | PASS R-JPCM-3UX1 |
| Init ordering and enablement | PASS R-JLOW-YJOY, R-JVG4-0PMI, R-JWO0-EHD7 |
| Stable regeneration and conditional install restart | PASS R-JT0B-9654, R-JU87-MXVT, R-JWO0-EHD7 |
| Disabled retention/omitted credentials | Draft PASS R-JRSE-VEEF; actual no-delete behavior observation pending |
| Exact three APIs/import direction | PASS D01/D08 agreement |
| Errors/determinism/atomic visibility | PASS R-JPCM-3UX1, R-JT0B-9654, R-JU87-MXVT, R-JVG4-0PMI |
| All three consumer examples | PASS declared names/signatures |
| Format/id permanence | PASS twelve new modal requirements, no prose-only declarations |

Issue quality follow-up: negative/malformed/oversized interval rejection should be distinguished from missing/zero intended policy rather than automatically requiring user decision. The consolidated external-observation issue referenced by the coverage must exist by final reconciliation. Existing observation proves Litestream absent on dev; official configuration docs support draft schema but not real observed operation.

No source/test/build/check/commit performed. Adjacent archives/retirement/detailed restore remain unverified by this model pass.
