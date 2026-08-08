# telemetry — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. To resolve an id, grep this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Service skeleton, chassis Spec & composition root — R-V6NF-9LY8, R-V7VB-NDOX, R-NTSI-XWI1
- D2 → `project/design/D02.md` — The record type, the schema, and the append-only store — R-V938-15FM, R-VAB4-EX6B, R-VBJ0-SOX0, R-VCQX-6GNP, R-VDYT-K8EE, R-VF6P-Y053, R-VGEM-BRVS
- D3 → `project/design/D03.md` — The loopback ingest endpoint — R-VIUF-3BD6, R-VK2B-H33V, R-VLA7-UUUK, R-VMI4-8ML9, R-VNQ0-MEBY, R-VOXX-062N, R-VQ5T-DXTC
- D4 → `project/design/D04.md` — Retention: the configured window and the pruner — R-VRDP-RPK1, R-VSLM-5HAQ, R-VTTI-J91F, R-VV1E-X0S4
- D5 → `project/design/D05.md` — The forensic MCP surface: `search`, `chain`, `get`, `guide` — R-VW9B-ASIT, R-VXH7-OK9I, R-VYP4-2C07, R-VZX0-G3QW, R-W2CT-7N8A, R-W3KP-LEYZ, R-W4SL-Z6PO
- D6 → `project/design/D06.md` — The nginx location fragment — R-W60I-CYGD, R-W78E-QQ72, R-W8GB-4HXR, R-W9O7-I9OG, R-WAW3-W1F5
- D7 → `project/design/D07.md` — Test strategy and the end-to-end layer — R-5PIJ-TFHS, R-WC40-9T5U, R-WDBW-NKWJ
- D8 → `project/design/D08.md` — MCP input schemas conform to the agentkit tool-schema subset — R-D2X0-57VI, R-D44W-IZM7, R-D5CS-WRCW
- D9 → `project/design/D09.md` — Suite-contract conformance: the opsctl install layout & the authored env contract — adopts `R-4LKF-FB23` (root `project/design/D08.md`), `R-8DF1-W89F`, `R-8IAN-FB87` (root `project/design/D11.md`); mints none of its own
- D10 → `project/design/D10.md` — Adopt the suite testing-language contract — adopts `R-O1AD-MRKW`, `R-O2IA-0JBL` (root `project/design/D23.md`); mints none of its own

## Verification ids → Decision

- R-4LKF-FB23 → D9 — `project/design/D09.md` (adopted from root `project/design/D08.md`)
- R-5PIJ-TFHS → D7 — `project/design/D07.md`
- R-8DF1-W89F → D9 — `project/design/D09.md` (adopted from root `project/design/D11.md`)
- R-8IAN-FB87 → D9 — `project/design/D09.md` (adopted from root `project/design/D11.md`)
- R-D2X0-57VI → D8 — `project/design/D08.md`
- R-D44W-IZM7 → D8 — `project/design/D08.md`
- R-D5CS-WRCW → D8 — `project/design/D08.md`
- R-NTSI-XWI1 → D1 — `project/design/D01.md`
- R-O1AD-MRKW → D10 — `project/design/D10.md` (adopted from root `project/design/D23.md`)
- R-O2IA-0JBL → D10 — `project/design/D10.md` (adopted from root `project/design/D23.md`)
- R-V6NF-9LY8 → D1 — `project/design/D01.md`
- R-V7VB-NDOX → D1 — `project/design/D01.md`
- R-V938-15FM → D2 — `project/design/D02.md`
- R-VAB4-EX6B → D2 — `project/design/D02.md`
- R-VBJ0-SOX0 → D2 — `project/design/D02.md`
- R-VCQX-6GNP → D2 — `project/design/D02.md`
- R-VDYT-K8EE → D2 — `project/design/D02.md`
- R-VF6P-Y053 → D2 — `project/design/D02.md`
- R-VGEM-BRVS → D2 — `project/design/D02.md`
- R-VIUF-3BD6 → D3 — `project/design/D03.md`
- R-VK2B-H33V → D3 — `project/design/D03.md`
- R-VLA7-UUUK → D3 — `project/design/D03.md`
- R-VMI4-8ML9 → D3 — `project/design/D03.md`
- R-VNQ0-MEBY → D3 — `project/design/D03.md`
- R-VOXX-062N → D3 — `project/design/D03.md`
- R-VQ5T-DXTC → D3 — `project/design/D03.md`
- R-VRDP-RPK1 → D4 — `project/design/D04.md`
- R-VSLM-5HAQ → D4 — `project/design/D04.md`
- R-VTTI-J91F → D4 — `project/design/D04.md`
- R-VV1E-X0S4 → D4 — `project/design/D04.md`
- R-VW9B-ASIT → D5 — `project/design/D05.md`
- R-VXH7-OK9I → D5 — `project/design/D05.md`
- R-VYP4-2C07 → D5 — `project/design/D05.md`
- R-VZX0-G3QW → D5 — `project/design/D05.md`
- R-W2CT-7N8A → D5 — `project/design/D05.md`
- R-W3KP-LEYZ → D5 — `project/design/D05.md`
- R-W4SL-Z6PO → D5 — `project/design/D05.md`
- R-W60I-CYGD → D6 — `project/design/D06.md`
- R-W78E-QQ72 → D6 — `project/design/D06.md`
- R-W8GB-4HXR → D6 — `project/design/D06.md`
- R-W9O7-I9OG → D6 — `project/design/D06.md`
- R-WAW3-W1F5 → D6 — `project/design/D06.md`
- R-WC40-9T5U → D7 — `project/design/D07.md`
- R-WDBW-NKWJ → D7 — `project/design/D07.md`
