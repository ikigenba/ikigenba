# D09 independent verification

Fresh read-only verifier `/root/apps_backup/install_verify` reviewed all 22 requirements and 27 coverage rows against apps introduction/install stories and D01/D03/D06/D08/D11. Verdict: two correctable additions; otherwise consistent within recorded partial-output issues.

## Correctable defects

1. R-P2TO-03TP orders success reports but does not guarantee file stdout after artifact validation before default-conflict/missing-secret failures. Add that guarantee, including halt on report callback failure. Affects INSTALL-second-default-all and INSTALL-missing-secret-all.
2. Story fresh environment values must never appear printed or on command lines. R-OWQ6-3948 covers rejected values only and D01 ReadSecrets constraint covers only retrieval, not downstream install or plain values. Add install-wide prohibition for secret/plain values in reports, diagnostics, subprocess arguments on success and failure. Affects INSTALL-fresh-env and upgrade inheritance.

All 27 rows reviewed; every row passes its completed D09-owned content except those two defects and recorded fetch-line/status-equality issue. Intro role/encoding external behavior is pending central cloud/evidence issues. Upgrade/default/start-failure exact fetch variants remain unresolved; fresh service equality remains unresolved. Postfailure status and restore ordering remain adjacent ownership D10/D14.

Public APIs, typed error fields, callbacks and examples match D01/D03/D06/D08/D09/D11. CLI composition is acyclic. Callback failures preserve prior effects/no later steps. ParseManifest and Install share apps; private error classification suffices for exact unusable-name failure and needs no exported error type. Nonroot ikigenba account/writable app directory is supported by app-created persistent data; installer cannot touch state/cache. Story schema is authorized input, no sibling internals read. All22 IDs/modal requirements canonical; author replacement history supplied, transient mint history not independently reconstructible.

Central cloud adapter/parameter representation and environment observations remain required before check-spec. Basic xz archive roundtrip is observed but not deployment/hostile input handling. No source/test/build/check/commit performed.
