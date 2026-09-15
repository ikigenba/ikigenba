# D08 independent verification

Verifier: fresh agent `/root/apps_backup/apps_model_verify`; read-only; no inherited history.

Verdict: PASS, 12/12 scoped model criteria; all 13 requirements reviewed. Lifecycle/archive/report behavior delegated to D09/D10/D11/D12 is not counted complete by this model pass.

| Criterion | Result / evidence |
|---|---|
| App identity and reserved names | PASS R-A2TS-K4M0, R-XUVN-2GXA |
| Exact TOML model, defaults and failures | PASS R-XNK8-RUH4, R-XOS5-5M7T, R-XSFU-AXFW, R-XTNQ-OP6L, R-XUVN-2GXA, R-XW3J-G8NZ |
| Artifact/version distinction | PASS model R-Y0Z4-ZBMR; D09 archive and D10 executable query pending |
| Zero/one default declaration | PASS structural; D09 conflict and D06 zero-default rendering own behavior |
| Plain env versus secret names | PASS separate fields; D09 serialization pending |
| Database travels with app | PASS Database and host-local manifest decoding |
| App owns migration/seeds | PASS model R-Y0Z4-ZBMR; lifecycle guarantees downstream |
| No remote registration | PASS R-XMCC-E2QF, R-XZR8-LJW2, R-Y0Z4-ZBMR |
| Service discovery definition | PASS R-XQ01-JDYI, R-XXBF-U0EO, R-XZR8-LJW2 |
| Absent manifest/database | PASS R-XTNQ-OP6L, R-XYJC-7S5D; archive semantics downstream |
| Ordered empty/data-only discovery | PASS R-XXBF-U0EO, R-XZR8-LJW2 |
| Independent routing capability | PASS optional Port irrespective database/binary |

Exact declarations, D01 dependency direction, root path resolution, per-service errors, both consumer tasks and requirement format passed. No drafting defects or unresolved product questions within D08. No original requirement ids reused. Verifier could not independently establish transient idgen history; tool author report records minted ids.

Pending external compatibility observation remains disclosed in apps-model-usage.md before check-spec if relied upon. No sibling internals read.
