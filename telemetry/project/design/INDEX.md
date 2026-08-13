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
- D6 → `project/design/D06.md` — The nginx location fragment — R-W60I-CYGD, R-W78E-QQ72, R-W8GB-4HXR, R-W9O7-I9OG, R-WAW3-W1F5, R-67LH-0ZXP, R-68TD-EROE
- D7 → `project/design/D07.md` — Test strategy and the end-to-end layer — R-5PIJ-TFHS, R-WC40-9T5U, R-WDBW-NKWJ
- D8 → `project/design/D08.md` — MCP input schemas conform to the agentkit tool-schema subset — R-D2X0-57VI, R-D44W-IZM7, R-D5CS-WRCW
- D9 → `project/design/D09.md` — Suite-contract conformance: the opsctl install layout & the authored env contract — adopts `R-4LKF-FB23` (root `project/design/D08.md`), `R-8DF1-W89F`, `R-8IAN-FB87` (root `project/design/D11.md`); mints none of its own
- D10 → `project/design/D10.md` — Adopt the suite testing-language contract — adopts `R-O1AD-MRKW`, `R-O2IA-0JBL` (root `project/design/D23.md`); mints none of its own
- D11 → `project/design/D11.md` — The canonical landing page (byte-for-byte suite template, telemetry text only) — R-6B96-6B5S, R-6CH2-K2WH
- D12 → `project/design/D12.md` — Adopt the suite brand icon contract: the shipped icon set and its link markup — mints none; adopts R-RYDN-YNR5, R-RZLK-CFHU (root `project/design/D29.md`)

## Verification ids → Decision

- R-4LKF-FB23 → D9 — `project/design/D09.md` (adopted from root `project/design/D08.md`)
- R-5PIJ-TFHS → D7 — `project/design/D07.md`
- R-67LH-0ZXP → D6 — `project/design/D06.md`
- R-68TD-EROE → D6 — `project/design/D06.md`
- R-6B96-6B5S → D11 — `project/design/D11.md`
- R-6CH2-K2WH → D11 — `project/design/D11.md`
- R-8DF1-W89F → D9 — `project/design/D09.md` (adopted from root `project/design/D11.md`)
- R-8IAN-FB87 → D9 — `project/design/D09.md` (adopted from root `project/design/D11.md`)
- R-D2X0-57VI → D8 — `project/design/D08.md`
- R-D44W-IZM7 → D8 — `project/design/D08.md`
- R-D5CS-WRCW → D8 — `project/design/D08.md`
- R-NTSI-XWI1 → D1 — `project/design/D01.md`
- R-O1AD-MRKW → D10 — `project/design/D10.md` (adopted from root `project/design/D23.md`)
- R-O2IA-0JBL → D10 — `project/design/D10.md` (adopted from root `project/design/D23.md`)
- R-RYDN-YNR5 → D12 — `project/design/D12.md` (adopted from root `project/design/D29.md`)
- R-RZLK-CFHU → D12 — `project/design/D12.md` (adopted from root `project/design/D29.md`)
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

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped
to the id(s) whose tests most directly prove it; the mapping's completeness is
this manifest's concern, each proof's quality the audit's. Regenerated with the
rest of the index.

1. With telemetry stopped, every other service keeps working — no error, no
   hang →
   R-5PIJ-TFHS
2. A submitted batch is acknowledged and every record afterwards findable via
   search, including after a restart →
   R-VIUF-3BD6, R-WDBW-NKWJ, R-WC40-9T5U
3. A mixed batch stores the well-formed records and reports the rejected count;
   a submission that is not a well-formed batch is refused whole →
   R-VK2B-H33V, R-VLA7-UUUK
4. A submission from outside the box (through the front door) is refused and
   stores nothing →
   R-VMI4-8ML9
5. A chain spanning several services returns all its records in order and states
   the retention boundary →
   R-VZX0-G3QW, R-VCQX-6GNP
6. A chain id with no records returns an empty answer, not an error →
   R-VZX0-G3QW
7. Search on any combination of axes returns exactly the matches, and the
   continuation pages with no record repeated or skipped →
   R-VXH7-OK9I, R-VYP4-2C07, R-VDYT-K8EE
8. Two records from different services sharing a body digest are both returned by
   a digest search →
   R-VF6P-Y053, R-VXH7-OK9I
9. Any record read back in full by its id; an unrecognized id reports not-found →
   R-W2CT-7N8A
10. No record carries a body, raw header dump, or conversation; oversized and
    sensitive arguments appear as size and digest →
    R-W4SL-Z6PO, R-W2CT-7N8A
11. The surface offers only search, chain, get, guide plus chassis tools —
    nothing that deletes, edits, redacts, annotates, or aggregates →
    R-VW9B-ASIT
12. Calling a telemetry tool is itself recorded; a submission on the private
    reporting path produces no record about itself →
    R-VOXX-062N
13. A graceful start and stop are both findable; a killed service leaves a start
    and no stop →
    R-5PIJ-TFHS
14. A reporting service's discarded-record count is reported, attributed to that
    service →
    R-VNQ0-MEBY
15. A record past the retention window is gone with no operator action, one
    inside remains; default 90 days →
    R-VRDP-RPK1, R-VTTI-J91F, R-VGEM-BRVS
16. An agent using only the service's own guide can search, follow a chain, and
    read a record →
    R-W4SL-Z6PO
17. A signed-in mount shows the uniform landing page naming the service and
    version; signed out leads to sign-in →
    R-6B96-6B5S, R-67LH-0ZXP
