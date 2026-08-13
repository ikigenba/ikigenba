# github — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolving an id is a grep against this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — A stateless connector on the appkit chassis — none (structural)
- D2 → `project/design/D02.md` — App authentication: the installation-token source — owns `R-DLMX-CNDL`, `R-DMUT-QF4A`, `R-DO2Q-46UZ`, `R-DPAM-HYLO`, `R-DQII-VQCD`, `R-DRQF-9I32`
- D3 → `project/design/D03.md` — The typed GitHub REST v3 client — owns `R-DVE4-ETB5`, `R-DWM0-SL1U`, `R-DXTX-6CSJ`, `R-DZ1T-K4J8`, `R-E09P-XW9X`, `R-E1HM-BO0M`, `R-E2PI-PFRB`, `R-E3XF-37I0`, `R-E55B-GZ8P`, `R-E6D7-UQZE`, `R-E7L4-8IQ3`, `R-EA0X-027H`, `R-EB8T-DTY6`, `R-ECGP-RLOV`, `R-D0IM-VQ7H`
- D4 → `project/design/D04.md` — The MCP tool surface — owns `R-EEWI-J569`, `R-EHCB-AONN`, `R-EIK7-OGEC`, `R-EJS4-2851`, `R-EL00-FZVQ`, `R-EM7W-TRMF`, `R-ENFT-7JD4`, `R-X3XX-6BNN`
- D5 → `project/design/D05.md` — The loopback `GET /pr` twin for scripts — owns `R-EPVL-Z2UI`, `R-ETJB-4E2L`
- D6 → `project/design/D06.md` — The landing page and nginx fragment — owns `R-WI06-793Q`, `R-WJ82-L0UF`, `R-WKFY-YSL4`, `R-WLNV-CKBT`, `R-WMVR-QC2I`, `R-WO3O-43T7`, `R-WPBK-HVJW`, `R-WQJG-VNAL`, `R-WRRD-9F1A`, `R-WSZ9-N6RZ`, `R-WU76-0YIO`, `R-EYEW-NH1D`, `R-1GOK-GA2F`, `R-1HWG-U1T4`
- D7 → `project/design/D07.md` — The session-gated locations opt into the apex `@login_bounce`: a logged-out human navigation goes to sign-in, not a bare 401 (bearer tier deliberately excluded) — owns `R-42HV-I1HS`, `R-43PR-VT8H`, `R-44XO-9KZ6`
- D8 → `project/design/D08.md` — Structured MCP adoption — owns `R-FI1O-9E44`, `R-FJ9K-N5UT`, `R-FKHH-0XLI`, `R-FLPD-EPC7`, `R-FMX9-SH2W`, `R-FO56-68TL`, `R-FPD2-K0KA`, `R-FQKY-XSAZ`, `R-FT0R-PBSD`
- D9 → `project/design/D09.md` — Issue-execution support verbs: `pr_create`, `issue_comments`, `label_add`, `label_remove` — owns `R-GJYX-0UGN`, `R-F70H-NRU9`, `R-GL6T-EM7C`, `R-GMEP-SDY1`, `R-GNMM-65OQ`, `R-F88E-1JKY`, `R-GOUI-JXFF`, `R-GQ2E-XP64`
- D10 → `project/design/D10.md` — The loopback `GET /token` twin: installation tokens for repos' git plumbing — owns `R-GSI7-P8NI`, `R-GTQ4-30E7`, `R-GUY0-GS4W`, `R-GW5W-UJVL`
- D11 → `project/design/D11.md` — GitHub's outbound calls move onto the shared instrumented HTTP client — owns `R-01NE-H6K8`, `R-02VA-UYAX`, `R-05B3-MHSB`, `R-06J0-09J0`
- D12 → `project/design/D12.md` — nginx fragment: forward the edge-minted `X-Correlation-Id` on gated locations, blank it on ungated ones — owns `R-1S5A-Z3ZD`, `R-1TD7-CVQ2`, `R-1UL3-QNGR`
- D13 → `project/design/D13.md` — Suite-contract conformance: the opsctl install layout and the authored env contract — adopts `R-4LKF-FB23` (root `project/design/D08.md`), `R-8DF1-W89F`, `R-8IAN-FB87` (root `project/design/D11.md`); mints none of its own
- D14 → `project/design/D14.md` — Adopt the suite testing-language contract (`root project/design/D23.md`): hermetic + composed in the gate, a manual layer whose runbook is `project/github-verification.md` (where D2's `R-DMUT-QF4A` is proven, out of gate), no live layer, the `go`-on-`PATH` precondition, `GOWORK=off` mode — mints none; adopts `R-O1AD-MRKW`, `R-O2IA-0JBL` (root `project/design/D23.md`)
- D15 → `project/design/D15.md` — Adopt the suite brand icon contract: the shipped icon set and its link markup — mints none; adopts R-8MFA-HUNC, R-RYDN-YNR5, R-RZLK-CFHU (root `project/design/D29.md`)

## Verification ids → Decision

- R-01NE-H6K8 → D11 — `project/design/D11.md`
- R-02VA-UYAX → D11 — `project/design/D11.md`
- R-05B3-MHSB → D11 — `project/design/D11.md`
- R-06J0-09J0 → D11 — `project/design/D11.md`
- R-1GOK-GA2F → D6 — `project/design/D06.md`
- R-1HWG-U1T4 → D6 — `project/design/D06.md`
- R-1S5A-Z3ZD → D12 — `project/design/D12.md`
- R-1TD7-CVQ2 → D12 — `project/design/D12.md`
- R-1UL3-QNGR → D12 — `project/design/D12.md`
- R-42HV-I1HS → D7 — `project/design/D07.md`
- R-43PR-VT8H → D7 — `project/design/D07.md`
- R-44XO-9KZ6 → D7 — `project/design/D07.md`
- R-4LKF-FB23 → D13 — `project/design/D13.md` (adopted from root `project/design/D08.md`)
- R-8DF1-W89F → D13 — `project/design/D13.md` (adopted from root `project/design/D11.md`)
- R-8IAN-FB87 → D13 — `project/design/D13.md` (adopted from root `project/design/D11.md`)
- R-8MFA-HUNC → D15 — `project/design/D15.md` (adopted from root `project/design/D29.md`)
- R-D0IM-VQ7H → D3 — `project/design/D03.md`
- R-DLMX-CNDL → D2 — `project/design/D02.md`
- R-DMUT-QF4A → D2 — `project/design/D02.md`
- R-DO2Q-46UZ → D2 — `project/design/D02.md`
- R-DPAM-HYLO → D2 — `project/design/D02.md`
- R-DQII-VQCD → D2 — `project/design/D02.md`
- R-DRQF-9I32 → D2 — `project/design/D02.md`
- R-DVE4-ETB5 → D3 — `project/design/D03.md`
- R-DWM0-SL1U → D3 — `project/design/D03.md`
- R-DXTX-6CSJ → D3 — `project/design/D03.md`
- R-DZ1T-K4J8 → D3 — `project/design/D03.md`
- R-E09P-XW9X → D3 — `project/design/D03.md`
- R-E1HM-BO0M → D3 — `project/design/D03.md`
- R-E2PI-PFRB → D3 — `project/design/D03.md`
- R-E3XF-37I0 → D3 — `project/design/D03.md`
- R-E55B-GZ8P → D3 — `project/design/D03.md`
- R-E6D7-UQZE → D3 — `project/design/D03.md`
- R-E7L4-8IQ3 → D3 — `project/design/D03.md`
- R-EA0X-027H → D3 — `project/design/D03.md`
- R-EB8T-DTY6 → D3 — `project/design/D03.md`
- R-ECGP-RLOV → D3 — `project/design/D03.md`
- R-EEWI-J569 → D4 — `project/design/D04.md`
- R-EHCB-AONN → D4 — `project/design/D04.md`
- R-EIK7-OGEC → D4 — `project/design/D04.md`
- R-EJS4-2851 → D4 — `project/design/D04.md`
- R-EL00-FZVQ → D4 — `project/design/D04.md`
- R-EM7W-TRMF → D4 — `project/design/D04.md`
- R-ENFT-7JD4 → D4 — `project/design/D04.md`
- R-EPVL-Z2UI → D5 — `project/design/D05.md`
- R-ETJB-4E2L → D5 — `project/design/D05.md`
- R-EYEW-NH1D → D6 — `project/design/D06.md`
- R-F70H-NRU9 → D9 — `project/design/D09.md`
- R-F88E-1JKY → D9 — `project/design/D09.md`
- R-FI1O-9E44 → D8 — `project/design/D08.md`
- R-FJ9K-N5UT → D8 — `project/design/D08.md`
- R-FKHH-0XLI → D8 — `project/design/D08.md`
- R-FLPD-EPC7 → D8 — `project/design/D08.md`
- R-FMX9-SH2W → D8 — `project/design/D08.md`
- R-FO56-68TL → D8 — `project/design/D08.md`
- R-FPD2-K0KA → D8 — `project/design/D08.md`
- R-FQKY-XSAZ → D8 — `project/design/D08.md`
- R-FT0R-PBSD → D8 — `project/design/D08.md`
- R-GJYX-0UGN → D9 — `project/design/D09.md`
- R-GL6T-EM7C → D9 — `project/design/D09.md`
- R-GMEP-SDY1 → D9 — `project/design/D09.md`
- R-GNMM-65OQ → D9 — `project/design/D09.md`
- R-GOUI-JXFF → D9 — `project/design/D09.md`
- R-GQ2E-XP64 → D9 — `project/design/D09.md`
- R-GSI7-P8NI → D10 — `project/design/D10.md`
- R-GTQ4-30E7 → D10 — `project/design/D10.md`
- R-GUY0-GS4W → D10 — `project/design/D10.md`
- R-GW5W-UJVL → D10 — `project/design/D10.md`
- R-O1AD-MRKW → D14 — `project/design/D14.md` (adopted from root `project/design/D23.md`)
- R-O2IA-0JBL → D14 — `project/design/D14.md` (adopted from root `project/design/D23.md`)
- R-RYDN-YNR5 → D15 — `project/design/D15.md` (adopted from root `project/design/D29.md`)
- R-RZLK-CFHU → D15 — `project/design/D15.md` (adopted from root `project/design/D29.md`)
- R-WI06-793Q → D6 — `project/design/D06.md`
- R-WJ82-L0UF → D6 — `project/design/D06.md`
- R-WKFY-YSL4 → D6 — `project/design/D06.md`
- R-WLNV-CKBT → D6 — `project/design/D06.md`
- R-WMVR-QC2I → D6 — `project/design/D06.md`
- R-WO3O-43T7 → D6 — `project/design/D06.md`
- R-WPBK-HVJW → D6 — `project/design/D06.md`
- R-WQJG-VNAL → D6 — `project/design/D06.md`
- R-WRRD-9F1A → D6 — `project/design/D06.md`
- R-WSZ9-N6RZ → D6 — `project/design/D06.md`
- R-WU76-0YIO → D6 — `project/design/D06.md`
- R-X3XX-6BNN → D4 — `project/design/D04.md`

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped to
the id(s) whose Verification most directly proves it; the quality of each proof
is the audit's question, the mapping's completeness is this manifest's.
Regenerated with the rest of the index.

1. A `prompts` agent lists its tools, finds the `github` verbs, and reads a real `@ikigenba` pull request through them →
   R-EEWI-J569, R-DZ1T-K4J8
2. An agent comments on an issue and it appears authored by the App with no owner email or marker →
   R-E7L4-8IQ3, R-EJS4-2851
3. A `scripts` job fetches a PR over the loopback route and gets its data; the public front door is refused →
   R-EPVL-Z2UI, R-ETJB-4E2L, R-FT0R-PBSD
4. `health` succeeds only on real `@ikigenba` auth and fails loudly on a bad key or app id →
   R-EL00-FZVQ, R-DMUT-QF4A
5. The connector works across the installation token's expiry — requests an hour apart see no token-expiry failure →
   R-DPAM-HYLO, R-DQII-VQCD
6. A logged-in user opens `/srv/github/` and sees the service name and version; a sessionless browser gets 401 →
   R-WI06-793Q, R-1HWG-U1T4
7. A caller opens a PR from a branch (authored by the App, no marker), adds then removes one label leaving others untouched, and reads an issue's full comment thread in order →
   R-GJYX-0UGN, R-GL6T-EM7C, R-GMEP-SDY1, R-F70H-NRU9
8. A sibling service gets a working short-lived credential over the loopback route and does a real git op; the public front door is refused and the credential appears in no log →
   R-GTQ4-30E7, R-GUY0-GS4W, R-GW5W-UJVL
