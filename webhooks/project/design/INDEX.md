# webhooks — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. To resolve an id, grep this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Service skeleton, seams & composition root — R-IC14-FKIK, R-ID90-TC99
- D2 → `project/design/D02.md` — Data model & migrations — R-SZ8I-R4EY, R-T0GF-4W5N, R-T1OB-INWC
- D3 → `project/design/D03.md` — Webhook identity & secret lifecycle — R-37GT-C05G, R-38OP-PRW5, R-39WM-3JMU, R-3CCE-V348, R-3DKB-8UUX, R-3ES7-MMLM
- D4 → `project/design/D04.md` — Public ingress endpoint (/in/<name>) — R-7ISQ-ZZCF, R-7K0N-DR34, R-7L8J-RITT, R-7MGG-5AKI, R-7NOC-J2B7
- D5 → `project/design/D05.md` — Event production (durable-before-ack) — R-GTUZ-AIGW, R-GV2V-OA7L, R-GWAS-21YA, R-GXIO-FTOZ, R-GYQK-TLFO
- D6 → `project/design/D06.md` — MCP tool surface (the four owner tools) — R-5Z8J-Y0YP, R-60GG-BSPE, R-61OC-PKG3, R-62W9-3C6S, R-6445-H3XH, R-65C1-UVO6
- D7 → `project/design/D07.md` — nginx location fragment (tiers) — R-OD12-3CVG, R-OE8Y-H4M5, R-OFGU-UWCU, R-OGOR-8O3J, R-TTUW-5O3V, R-TV2S-JFUK, R-TWAO-X7L9, R-XK5N-0I1E, R-XLDJ-E9S3
- D8 → `project/design/D08.md` — Test strategy, harness & dev-onboarding — R-UELV-YLA4, R-UFTS-CD0T
- D9 → `project/design/D09.md` — Human landing page (`share/www` template & Carbon assets) — R-TMJH-V1NP, R-TNRE-8TEE, R-TOZA-ML53, R-TQ77-0CVS, R-TRF3-E4MH
- D10 → `project/design/D10.md` — Adopt `registry` (own port by name + drift guards) — R-0D7X-9EB6, R-0EFT-N61V, R-0FNQ-0XSK
- D11 → `project/design/D11.md` — Web surface from `share/www` through the chassis (de-embed) — R-0GVM-EPJ9, R-0I3I-SH9Y
- D12 → `project/design/D12.md` — MCP surface over `appkit/mcp` (`internal/mcp` becomes the tool table) — R-0JBF-690N
- D13 → `project/design/D13.md` — Delete the `internal/db` shim, normalize the composition root, true up the doctrine — (structural; no ids)
- D14 → `project/design/D14.md` — The session-gated locations opt into the apex `@login_bounce`: a logged-out human navigation goes to sign-in, not a bare 401 (bearer tier deliberately excluded) — R-4B16-6FON, R-4C92-K7FC, R-4DGY-XZ61
- D15 → `project/design/D15.md` — Event-routing conformance: kind `received`, subject = `/` + hook name — R-A3FB-J3ZK, R-A4N7-WVQ9, R-A5V4-ANGY, R-A730-OF7N
- D16 → `project/design/D16.md` — Structured MCP adoption (structuredContent, output schemas, closed error vocabulary) — R-DRUS-R3AP, R-DT2P-4V1E, R-DUAL-IMS3, R-DVIH-WEIS, R-DWQE-A69H, R-DXYA-NY06
- D17 → `project/design/D17.md` — Per-hook verification schemes: `bearer` and `github-hmac` — R-G7RX-751P, R-G8ZT-KWSE, R-GA7P-YOJ3, R-GBFM-CG9S, R-GCNI-Q80H, R-GDVF-3ZR6, R-GF3B-HRHV

## Verification ids → Decision

- R-0D7X-9EB6 → D10 — `project/design/D10.md`
- R-0EFT-N61V → D10 — `project/design/D10.md`
- R-0FNQ-0XSK → D10 — `project/design/D10.md`
- R-0GVM-EPJ9 → D11 — `project/design/D11.md`
- R-0I3I-SH9Y → D11 — `project/design/D11.md`
- R-0JBF-690N → D12 — `project/design/D12.md`
- R-37GT-C05G → D3 — `project/design/D03.md`
- R-38OP-PRW5 → D3 — `project/design/D03.md`
- R-39WM-3JMU → D3 — `project/design/D03.md`
- R-3CCE-V348 → D3 — `project/design/D03.md`
- R-3DKB-8UUX → D3 — `project/design/D03.md`
- R-3ES7-MMLM → D3 — `project/design/D03.md`
- R-4B16-6FON → D14 — `project/design/D14.md`
- R-4C92-K7FC → D14 — `project/design/D14.md`
- R-4DGY-XZ61 → D14 — `project/design/D14.md`
- R-5Z8J-Y0YP → D6 — `project/design/D06.md`
- R-60GG-BSPE → D6 — `project/design/D06.md`
- R-61OC-PKG3 → D6 — `project/design/D06.md`
- R-62W9-3C6S → D6 — `project/design/D06.md`
- R-6445-H3XH → D6 — `project/design/D06.md`
- R-65C1-UVO6 → D6 — `project/design/D06.md`
- R-7ISQ-ZZCF → D4 — `project/design/D04.md`
- R-7K0N-DR34 → D4 — `project/design/D04.md`
- R-7L8J-RITT → D4 — `project/design/D04.md`
- R-7MGG-5AKI → D4 — `project/design/D04.md`
- R-7NOC-J2B7 → D4 — `project/design/D04.md`
- R-A3FB-J3ZK → D15 — `project/design/D15.md`
- R-A4N7-WVQ9 → D15 — `project/design/D15.md`
- R-A5V4-ANGY → D15 — `project/design/D15.md`
- R-A730-OF7N → D15 — `project/design/D15.md`
- R-DRUS-R3AP → D16 — `project/design/D16.md`
- R-DT2P-4V1E → D16 — `project/design/D16.md`
- R-DUAL-IMS3 → D16 — `project/design/D16.md`
- R-DVIH-WEIS → D16 — `project/design/D16.md`
- R-DWQE-A69H → D16 — `project/design/D16.md`
- R-DXYA-NY06 → D16 — `project/design/D16.md`
- R-G7RX-751P → D17 — `project/design/D17.md`
- R-G8ZT-KWSE → D17 — `project/design/D17.md`
- R-GA7P-YOJ3 → D17 — `project/design/D17.md`
- R-GBFM-CG9S → D17 — `project/design/D17.md`
- R-GCNI-Q80H → D17 — `project/design/D17.md`
- R-GDVF-3ZR6 → D17 — `project/design/D17.md`
- R-GF3B-HRHV → D17 — `project/design/D17.md`
- R-GTUZ-AIGW → D5 — `project/design/D05.md`
- R-GV2V-OA7L → D5 — `project/design/D05.md`
- R-GWAS-21YA → D5 — `project/design/D05.md`
- R-GXIO-FTOZ → D5 — `project/design/D05.md`
- R-GYQK-TLFO → D5 — `project/design/D05.md`
- R-IC14-FKIK → D1 — `project/design/D01.md`
- R-ID90-TC99 → D1 — `project/design/D01.md`
- R-OD12-3CVG → D7 — `project/design/D07.md`
- R-OE8Y-H4M5 → D7 — `project/design/D07.md`
- R-OFGU-UWCU → D7 — `project/design/D07.md`
- R-OGOR-8O3J → D7 — `project/design/D07.md`
- R-SZ8I-R4EY → D2 — `project/design/D02.md`
- R-T0GF-4W5N → D2 — `project/design/D02.md`
- R-T1OB-INWC → D2 — `project/design/D02.md`
- R-TMJH-V1NP → D9 — `project/design/D09.md`
- R-TNRE-8TEE → D9 — `project/design/D09.md`
- R-TOZA-ML53 → D9 — `project/design/D09.md`
- R-TQ77-0CVS → D9 — `project/design/D09.md`
- R-TRF3-E4MH → D9 — `project/design/D09.md`
- R-TTUW-5O3V → D7 — `project/design/D07.md`
- R-TV2S-JFUK → D7 — `project/design/D07.md`
- R-TWAO-X7L9 → D7 — `project/design/D07.md`
- R-UELV-YLA4 → D8 — `project/design/D08.md`
- R-UFTS-CD0T → D8 — `project/design/D08.md`
- R-XK5N-0I1E → D7 — `project/design/D07.md`
- R-XLDJ-E9S3 → D7 — `project/design/D07.md`
