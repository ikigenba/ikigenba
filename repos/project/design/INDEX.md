# repos — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. To resolve an id, grep this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Composition root & chassis boot — R-EISY-2LYZ, R-EL8Q-U5GD
- D2 → `project/design/D02.md` — Data model & migrations — R-EMGN-7X72, R-ENOJ-LOXR, R-EOWF-ZGOG, R-TY2R-GFRU, R-TZAN-U7IJ, R-ICIJ-13TA, R-IDQF-EVJZ
- D3 → `project/design/D03.md` — GitHub-fact intake: the webhooks consumer & dispatch table — R-EQ4C-D8F5, R-ERC8-R05U, R-ESK5-4RWJ, R-ETS1-IJN8, R-EUZX-WBDX, R-EW7U-A34M, R-IG68-6F1D
- D4 → `project/design/D04.md` — Repo lifecycle & git custody — R-EXFQ-NUVB, R-EYNN-1MM0, R-EZVJ-FECP, R-F13F-T63E, R-F3J8-KPKS, R-C9CO-ODYU
- D5 → `project/design/D05.md` — The session engine: worktree-per-session, queue, and the confined agent — R-F4R4-YHBH, R-F5Z1-C926, R-F76X-Q0SV, R-F8EU-3SJK, R-F9MQ-HKA9, R-FAUM-VC0Y, R-FC2J-93RN, R-2U0F-NNXH
- D6 → `project/design/D06.md` — The issue protocol: labels, the check gate, and runner-side GitHub I/O — R-FDAF-MVIC, R-FEIC-0N91, R-FFQ8-EEZQ, R-FGY4-S6QF, R-FI61-5YH4, R-FKLT-XHYI, R-FLTQ-B9P7, R-2V8C-1FO6, R-894D-CUA2, R-APSC-24AL
- D7 → `project/design/D07.md` — The MCP tool surface — R-FN1M-P1FW, R-FO9J-2T6L, R-FPHF-GKXA, R-FQPB-UCNZ, R-FRX8-84EO, R-IEYB-SNAO, R-1WZF-FVH9
- D8 → `project/design/D08.md` — Events: the session-outcome families — R-FT54-LW5D, R-FUD0-ZNW2, R-FVKX-DFMR
- D9 → `project/design/D09.md` — State layout & retention — R-FWST-R7DG, R-FY0Q-4Z45, R-G0GI-WILJ
- D10 → `project/design/D10.md` — nginx fragment & the canonical landing page — R-G1OF-AAC8, R-G2WB-O22X, R-G448-1TTM, R-UZVS-S08C, R-V13P-5RZ1
- D11 → `project/design/D11.md` — The two direct HTTP peers (`HTTPTokenSource`, `GitHubPeer`) take the Router-provided instrumented outbound HTTP client (`rt.HTTPClient(…)` at the composition root; nil-client fallbacks deleted; each peer's requests reach the wire through it carrying the call's live context); git subprocesses and dispatched agent sessions are explicitly out of recording scope — R-BT9N-POGV, R-BUHK-3G7K
- D12 → `project/design/D12.md` — A session carries its correlation id in its row (`sessions.correlation_id`, additive migration), captured once in `Runner.Enqueue` from the ambient context and re-attached both to the context a dispatched run executes under (so its GitHub peer calls stay on the chain) and to the runner's detached completion contexts (so the outcome event does), including across a restart via `Recover` — R-BVPG-H7Y9, R-BWXC-UZOY, R-LM2I-ORUI, R-BY59-8RFN, R-BZD5-MJ6C
- D13 → `project/design/D13.md` — nginx fragment: all three gated locations capture the introspection-minted correlation id with `auth_request_set` and overwrite `X-Correlation-Id` upstream; the ungated PRM bootstrap sets it to `""` so the chassis mints — R-9DUI-TUQJ, R-9F2F-7MH8

- D14 → `project/design/D14.md` — Suite-contract conformance: the opsctl install layout & the authored env contract — adopts `R-4LKF-FB23` (root `project/design/D08.md`), `R-8DF1-W89F`, `R-8IAN-FB87` (root `project/design/D11.md`); mints none of its own
- D15 → `project/design/D15.md` — Env-channel conformance: session-engine knobs surface in the manifest; the org is customer data — owns R-L9EG-DDWC; adopts R-VKB6-SHHV (root `project/design/D11.md`)

## Verification ids → Decision

- R-1WZF-FVH9 → D7 — `project/design/D07.md`
- R-2U0F-NNXH → D5 — `project/design/D05.md`
- R-2V8C-1FO6 → D6 — `project/design/D06.md`
- R-4LKF-FB23 → D14 — `project/design/D14.md` (adopted from root `project/design/D08.md`)
- R-894D-CUA2 → D6 — `project/design/D06.md`
- R-8DF1-W89F → D14 — `project/design/D14.md` (adopted from root `project/design/D11.md`)
- R-8IAN-FB87 → D14 — `project/design/D14.md` (adopted from root `project/design/D11.md`)
- R-9DUI-TUQJ → D13 — `project/design/D13.md`
- R-9F2F-7MH8 → D13 — `project/design/D13.md`
- R-APSC-24AL → D6 — `project/design/D06.md`
- R-BT9N-POGV → D11 — `project/design/D11.md`
- R-BUHK-3G7K → D11 — `project/design/D11.md`
- R-BVPG-H7Y9 → D12 — `project/design/D12.md`
- R-BWXC-UZOY → D12 — `project/design/D12.md`
- R-BY59-8RFN → D12 — `project/design/D12.md`
- R-BZD5-MJ6C → D12 — `project/design/D12.md`
- R-C9CO-ODYU → D4 — `project/design/D04.md`
- R-EISY-2LYZ → D1 — `project/design/D01.md`
- R-EL8Q-U5GD → D1 — `project/design/D01.md`
- R-EMGN-7X72 → D2 — `project/design/D02.md`
- R-ENOJ-LOXR → D2 — `project/design/D02.md`
- R-EOWF-ZGOG → D2 — `project/design/D02.md`
- R-EQ4C-D8F5 → D3 — `project/design/D03.md`
- R-ERC8-R05U → D3 — `project/design/D03.md`
- R-ESK5-4RWJ → D3 — `project/design/D03.md`
- R-ETS1-IJN8 → D3 — `project/design/D03.md`
- R-EUZX-WBDX → D3 — `project/design/D03.md`
- R-EW7U-A34M → D3 — `project/design/D03.md`
- R-EXFQ-NUVB → D4 — `project/design/D04.md`
- R-EYNN-1MM0 → D4 — `project/design/D04.md`
- R-EZVJ-FECP → D4 — `project/design/D04.md`
- R-F13F-T63E → D4 — `project/design/D04.md`
- R-F3J8-KPKS → D4 — `project/design/D04.md`
- R-F4R4-YHBH → D5 — `project/design/D05.md`
- R-F5Z1-C926 → D5 — `project/design/D05.md`
- R-F76X-Q0SV → D5 — `project/design/D05.md`
- R-F8EU-3SJK → D5 — `project/design/D05.md`
- R-F9MQ-HKA9 → D5 — `project/design/D05.md`
- R-FAUM-VC0Y → D5 — `project/design/D05.md`
- R-FC2J-93RN → D5 — `project/design/D05.md`
- R-FDAF-MVIC → D6 — `project/design/D06.md`
- R-FEIC-0N91 → D6 — `project/design/D06.md`
- R-FFQ8-EEZQ → D6 — `project/design/D06.md`
- R-FGY4-S6QF → D6 — `project/design/D06.md`
- R-FI61-5YH4 → D6 — `project/design/D06.md`
- R-FKLT-XHYI → D6 — `project/design/D06.md`
- R-FLTQ-B9P7 → D6 — `project/design/D06.md`
- R-FN1M-P1FW → D7 — `project/design/D07.md`
- R-FO9J-2T6L → D7 — `project/design/D07.md`
- R-FPHF-GKXA → D7 — `project/design/D07.md`
- R-FQPB-UCNZ → D7 — `project/design/D07.md`
- R-FRX8-84EO → D7 — `project/design/D07.md`
- R-FT54-LW5D → D8 — `project/design/D08.md`
- R-FUD0-ZNW2 → D8 — `project/design/D08.md`
- R-FVKX-DFMR → D8 — `project/design/D08.md`
- R-FWST-R7DG → D9 — `project/design/D09.md`
- R-FY0Q-4Z45 → D9 — `project/design/D09.md`
- R-G0GI-WILJ → D9 — `project/design/D09.md`
- R-G1OF-AAC8 → D10 — `project/design/D10.md`
- R-G2WB-O22X → D10 — `project/design/D10.md`
- R-G448-1TTM → D10 — `project/design/D10.md`
- R-ICIJ-13TA → D2 — `project/design/D02.md`
- R-IDQF-EVJZ → D2 — `project/design/D02.md`
- R-IEYB-SNAO → D7 — `project/design/D07.md`
- R-IG68-6F1D → D3 — `project/design/D03.md`
- R-L9EG-DDWC → D15 — `project/design/D15.md`
- R-LM2I-ORUI → D12 — `project/design/D12.md`
- R-TY2R-GFRU → D2 — `project/design/D02.md`
- R-TZAN-U7IJ → D2 — `project/design/D02.md`
- R-UZVS-S08C → D10 — `project/design/D10.md`
- R-V13P-5RZ1 → D10 — `project/design/D10.md`
- R-VKB6-SHHV → D15 — `project/design/D15.md` (adopted from root `project/design/D11.md`)
