# bin — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolve an id by grepping this index (or the Decision files
directly). Regenerate this file whenever a Decision is added or its Verification
ids change.

## Decisions

- **D1** → `bin/project/design/D01.md` — Version production: `bin/bump` and `bin/ship` — ids: none (bash orchestration in the deliberately-untested tier; consumer-side proof on the box)
- **D2** → `bin/project/design/D02.md` — `bin/push-secrets`: seeding a service's deployed secrets from its `etc/env.list` — ids: none (bash orchestration around a live cloud API; verified once outside the loop)
- **D3** → `bin/project/design/D03.md` — The local dev stack: `bin/start` / `bin/stop`, and how a service joins it — ids: none (untested orchestration; its staging half's ids belong to D5)
- **D4** → `bin/project/design/D04.md` — `bin/create-migration`: the single mint for a migration version — ids: none (bash in the untested tier; verified once outside the loop)
- **D5** → `bin/project/design/D05.md` — The manifest readers are proven under the green gate (`bin/bintest`) — ids: R-V3XG-PB8R, R-V6D9-GUQ5, R-V7L5-UMGU
- **D6** → `bin/project/design/D06.md` — `bin/bintest` proves the library-dependency contract — ids: none minted here; realizes the umbrella's R-3R5W-79JK, R-3SDS-L1A9, R-3TLO-YT0Y, R-3UTL-CKRN (owned by `root project/design/D22.md`, `[proof: bin]`)
- **D7** → `bin/project/design/D07.md` — The testing-language contract: `bin/bintest` is hermetic-only, and `bin/` gets an `AGENTS.md` — ids: none minted here; cites the umbrella's R-O1AD-MRKW, R-O2IA-0JBL (owned by `root project/design/D23.md`, `[proof: per-service]`)
- **D8** → `bin/project/design/D08.md` — `bin/bump` enforces the changelog contract, proven in `bin/bintest` — ids: none minted here; realizes the umbrella's R-CKLX-X89X, R-CLTU-B00M, R-CN1Q-ORRB, R-CO9N-2JI0 (owned by `root project/design/D28.md`, `[proof: bin]`)
- **D9** → `bin/project/design/D09.md` — `bin/lint` enforces the suite lint contract, proven in `bin/bintest` — ids: none minted here; realizes the umbrella's R-WW5B-L155, R-WXD7-YSVU, R-WYL4-CKMJ, R-WZT0-QCD8, R-X10X-443X, R-X28T-HVUM, R-X3GP-VNLB, R-X4OM-9FC0, R-X5WI-N72P (owned by `root project/design/D30.md`, `[proof: bin]`)

## Verification ids → Decision

- R-3R5W-79JK → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-3SDS-L1A9 → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-3TLO-YT0Y → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-3UTL-CKRN → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-CKLX-X89X → D8 (`bin/project/design/D08.md`; owned by `root project/design/D28.md`)
- R-CLTU-B00M → D8 (`bin/project/design/D08.md`; owned by `root project/design/D28.md`)
- R-CN1Q-ORRB → D8 (`bin/project/design/D08.md`; owned by `root project/design/D28.md`)
- R-CO9N-2JI0 → D8 (`bin/project/design/D08.md`; owned by `root project/design/D28.md`)
- R-O1AD-MRKW → D7 (`bin/project/design/D07.md`; owned by `root project/design/D23.md`)
- R-O2IA-0JBL → D7 (`bin/project/design/D07.md`; owned by `root project/design/D23.md`)
- R-V3XG-PB8R → D5 (`bin/project/design/D05.md`)
- R-V6D9-GUQ5 → D5 (`bin/project/design/D05.md`)
- R-V7L5-UMGU → D5 (`bin/project/design/D05.md`)
- R-WW5B-L155 → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-WXD7-YSVU → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-WYL4-CKMJ → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-WZT0-QCD8 → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-X10X-443X → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-X28T-HVUM → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-X3GP-VNLB → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-X4OM-9FC0 → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)
- R-X5WI-N72P → D9 (`bin/project/design/D09.md`; owned by `root project/design/D30.md`)

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped to
the id(s) whose Verification most directly proves it. Much of this tooling is
deliberately untested orchestration (D1–D4); those criteria are traced to the
nearest id-backed proof — the production dependency contract, the changelog gate,
the manifest-reader tests, or the manual-layer testing contract. The strength of
each proof is the audit's question, the mapping's completeness is this manifest's.

1. Advancing a version writes the next version with its changelog record and rejects a non-conforming version file →
   R-CN1Q-ORRB
2. A version whose changelog omits the release fails with what to add, while `--dry-run` still reports the next version →
   R-CKLX-X89X, R-CLTU-B00M, R-CO9N-2JI0
3. Release delivery is exactly one artifact named by the binary's own identity, every tier present →
   R-3R5W-79JK, R-3TLO-YT0Y, R-3UTL-CKRN
4. The produced artifact stages on-box with no manual rearrangement →
   R-V6D9-GUQ5, R-V3XG-PB8R
5. The local stack answers each service on its own port behind one front door, and teardown leaves nothing running →
   R-V6D9-GUQ5, R-V7L5-UMGU
6. Two same-day migrations for one service get distinct, unambiguously ordered names →
   R-O1AD-MRKW, R-O2IA-0JBL
7. Secrets seeding previews and performs exactly the env's declared keys, masked, with a multi-line value intact →
   R-O1AD-MRKW, R-O2IA-0JBL
8. The ordinary test run exercises the layout readers on the real scripts and fails on drift from the box layout →
   R-V3XG-PB8R, R-V6D9-GUQ5, R-V7L5-UMGU, R-3R5W-79JK
9. A tree violating its declared style bar fails the check and its release is refused with no artifact; an undeclared tree is reported without failing; an unpinned style tool refuses to judge →
   R-WW5B-L155, R-WXD7-YSVU, R-X28T-HVUM, R-X5WI-N72P
