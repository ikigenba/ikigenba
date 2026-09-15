# Final independent release verification

Fresh verifier: `/root/release_ground/release_ground_verifier`. Reviewed all source release stories, current D15 (21 requirements), release inventory/usage, prior correction findings, and D01/D02/D05 boundaries. No edits or gates.

**39/39 criteria pass: 38 normative outcomes and one external-consumer context. All 8 story headings pass.**

| Criterion group | Per-criterion disposition | Count |
|---|---|---:|
| REL.P.01–REL.P.05 | Each passes the current inventory requirement mapping | 5/5 |
| REL.01.01–REL.01.08 | Each passes; REL.01.07 now unconditionally saves executing first-install script | 8/8 |
| REL.02.01–REL.02.07 | Each passes; REL.02.07 passes as external caller context using D15 plus D05 | 7/7 |
| REL.03.01–REL.03.04 | Each passes repeat/download/checksum/preservation mapping | 4/4 |
| REL.04.01–REL.04.03 | Each passes; REL.04.02 exact diagnostic-only follows controlling user AGENTS | 3/3 |
| REL.05.01–REL.05.03 | Each passes non-root refusal mapping | 3/3 |
| REL.06.01–REL.06.03 | Each passes missing-release failure mapping | 3/3 |
| REL.07.01–REL.07.03 | Each passes checksum rejection mapping | 3/3 |
| REL.08.01–REL.08.03 | Each passes candidate-version rejection mapping | 3/3 |

The per-criterion source locators, requirements and semantic evidence are in release-inventory.md; every current mapping was independently reviewed and accepted. Fresh, upgrade, repeated, missing operand, ordinary user, missing release, checksum mismatch and version mismatch headings all pass. Corrected structural splits and complete failure consumer tasks pass. Fresh and upgrade script-origin preconditions are consistent; no unresolved user intent remains.

D01 Root invariant scopes cli.Run/domain operations; D15 standalone Bash uses ground namespace isolation. D02 source-owned version and newline output remain authoritative; D15 checks candidate before rename without build-time version override. D05 retains init ownership; installer performs no setup/configuration. No Go exports or commands added by D15.

Contract coverage is distinct from external evidence. REL.P.02, REL.01.01 and REL.01.04 remain subject to successful release asset observation before check-spec. HEAD404 proves response status but neither successful availability nor reason for absence. Sole release issue: ../issues/release-external-observations.md. No implementation, tests, gates, installation, publication or commits performed by verification.
