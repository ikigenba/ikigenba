# eventplane — Design Index

Each Decision maps to its `DNN.md`; every Verification id maps to its Decision
and file; id lookup is a grep against this index. Regenerated whenever a
Decision is added or its Verification ids change.

## Decisions

- D1 → `D01.md` — Envelope and wire cutover: `kind` + `subject` replace
  `type` — R-39FF-NOQQ, R-3ANC-1GHF, R-3BV8-F884, R-3D34-SZYT, R-3EB1-6RPI,
  R-42P0-U6JE
- D2 → `D02.md` — The `routing` package: canonical key and hand-rolled
  matcher — R-3FIX-KJG7, R-3GQT-YB6W, R-3HYQ-C2XL, R-3J6M-PUOA, R-3KEJ-3MEZ,
  R-3LMF-HE5O, R-3MUB-V5WD, R-41H4-GESP
- D3 → `D03.md` — Producer families: registry, reflection, and filter
  validation — R-3O28-8XN2, R-3QI1-0H4G, R-3RPX-E8V5, R-3SXT-S0LU,
  R-3U5Q-5SCJ
- D4 → `D04.md` — Consumer surface: routing fields on `consumer.Event`,
  Subscription cutover — R-3VDM-JK38, R-3WLI-XBTX, R-3XTF-B3KM, R-3Z1B-OVBB,
  R-4098-2N20, R-95KP-1QIO
- D5 → `D05.md` — Feed guard ownership moves to the chassis — R-Z8Y5-5R0C
- D6 → `D06.md` — `eventplane/correlation`: the suite's correlation-id leaf
  package — R-UBWK-3IAS, R-UD4G-HA1H, R-UECC-V1S6, R-UFK9-8TIV, R-UGS5-ML9K,
  R-UI02-0D09
- D7 → `D07.md` — Correlation on the producer path: outbox column, envelope
  field, ctx-bearing `Append` — R-UJ7Y-E4QY, R-UKFU-RWHN, R-ULNR-5O8C,
  R-UMVN-JFZ1, R-UO3J-X7PQ, R-UPBG-AZGF
- D8 → `D08.md` — Correlation on the consumer path: the chain enters the
  handler's context — R-UQJC-OR74, R-URR9-2IXT, R-USZ5-GAOI, R-UU71-U2F7,
  R-UVEY-7U5W, R-UWMU-LLWL
- D9 → `D09.md` — `eventplane/observe`: an injectable hook on the publish and
  consume paths — R-UXUQ-ZDNA, R-V0AJ-QX4O, R-V1IG-4OVD, R-V2QC-IGM2,
  R-V3Y8-W8CR, R-V565-A03G, R-V6E1-NRU5
- D10 → `D10.md` — Adopt the suite testing-language contract (`root
  project/design/D23.md`): layers, GOWORK mode, environmental preconditions —
  owns none locally; **cites** R-O1AD-MRKW, R-O2IA-0JBL
- D11 → `D11.md` — Generation epochs: a restore mints a fresh epoch and stale
  cursors resync — R-4BT8-D54J, R-4D14-QWV8
- D12 → `D12.md` — Adopt the suite lint contract (`root project/design/D30.md`)
  at tier `strict` — none (structural; the contract carries no per-service ids)

## Verification ids → Decision

- R-39FF-NOQQ — D1 (`D01.md`)
- R-3ANC-1GHF — D1 (`D01.md`)
- R-3BV8-F884 — D1 (`D01.md`)
- R-3D34-SZYT — D1 (`D01.md`)
- R-3EB1-6RPI — D1 (`D01.md`)
- R-3FIX-KJG7 — D2 (`D02.md`)
- R-3GQT-YB6W — D2 (`D02.md`)
- R-3HYQ-C2XL — D2 (`D02.md`)
- R-3J6M-PUOA — D2 (`D02.md`)
- R-3KEJ-3MEZ — D2 (`D02.md`)
- R-3LMF-HE5O — D2 (`D02.md`)
- R-3MUB-V5WD — D2 (`D02.md`)
- R-3O28-8XN2 — D3 (`D03.md`)
- R-3QI1-0H4G — D3 (`D03.md`)
- R-3RPX-E8V5 — D3 (`D03.md`)
- R-3SXT-S0LU — D3 (`D03.md`)
- R-3U5Q-5SCJ — D3 (`D03.md`)
- R-3VDM-JK38 — D4 (`D04.md`)
- R-3WLI-XBTX — D4 (`D04.md`)
- R-3XTF-B3KM — D4 (`D04.md`)
- R-3Z1B-OVBB — D4 (`D04.md`)
- R-4098-2N20 — D4 (`D04.md`)
- R-41H4-GESP — D2 (`D02.md`)
- R-42P0-U6JE — D1 (`D01.md`)
- R-4BT8-D54J — D11 (`D11.md`)
- R-4D14-QWV8 — D11 (`D11.md`)
- R-95KP-1QIO — D4 (`D04.md`)
- R-O1AD-MRKW — D10 (`D10.md`) — cited from `root project/design/D23.md`, `[proof: per-service]`
- R-O2IA-0JBL — D10 (`D10.md`) — cited from `root project/design/D23.md`, `[proof: per-service]`
- R-UBWK-3IAS — D6 (`D06.md`)
- R-UD4G-HA1H — D6 (`D06.md`)
- R-UECC-V1S6 — D6 (`D06.md`)
- R-UFK9-8TIV — D6 (`D06.md`)
- R-UGS5-ML9K — D6 (`D06.md`)
- R-UI02-0D09 — D6 (`D06.md`)
- R-UJ7Y-E4QY — D7 (`D07.md`)
- R-UKFU-RWHN — D7 (`D07.md`)
- R-ULNR-5O8C — D7 (`D07.md`)
- R-UMVN-JFZ1 — D7 (`D07.md`)
- R-UO3J-X7PQ — D7 (`D07.md`)
- R-UPBG-AZGF — D7 (`D07.md`)
- R-UQJC-OR74 — D8 (`D08.md`)
- R-URR9-2IXT — D8 (`D08.md`)
- R-USZ5-GAOI — D8 (`D08.md`)
- R-UU71-U2F7 — D8 (`D08.md`)
- R-UVEY-7U5W — D8 (`D08.md`)
- R-UWMU-LLWL — D8 (`D08.md`)
- R-UXUQ-ZDNA — D9 (`D09.md`)
- R-V0AJ-QX4O — D9 (`D09.md`)
- R-V1IG-4OVD — D9 (`D09.md`)
- R-V2QC-IGM2 — D9 (`D09.md`)
- R-V3Y8-W8CR — D9 (`D09.md`)
- R-V565-A03G — D9 (`D09.md`)
- R-V6E1-NRU5 — D9 (`D09.md`)
- R-Z8Y5-5R0C — D5 (`D05.md`)

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped
to the id(s) whose tests most directly prove it; the quality of each proof is
the audit's question, the mapping's completeness is this manifest's.
Regenerated with the rest of the index.

1. Address `dropbox:create/bills/aws/2026-06.pdf` observable end to end, from
   producer append to consumer delivery →
   R-3EB1-6RPI, R-3D34-SZYT, R-3VDM-JK38, R-3WLI-XBTX
2. A consumer filtering `dropbox:create/bills/**/*.pdf` gets the match and not
   `notes.txt` or the `delete` →
   R-3Z1B-OVBB, R-95KP-1QIO
3. A subjectless event (`ledger:recorded`) selectable by its literal key →
   R-3MUB-V5WD
4. Reflection describes each family with a schema and worked example that agree →
   R-3QI1-0H4G, R-3RPX-E8V5
5. Filter validation: `dropbox:delete/**` cannot match a create-only producer,
   `dropbox:create/**` can →
   R-3O28-8XN2, R-3SXT-S0LU, R-3U5Q-5SCJ
6. An uppercase kind or a non-`/`-rooted subject is refused with an explanation →
   R-39FF-NOQQ, R-3ANC-1GHF
7. An event from correlated work arrives reporting the same correlation id →
   R-UO3J-X7PQ, R-UQJC-OR74, R-URR9-2IXT
8. A handler that republishes carries the original id — the chain survives each
   hop →
   R-UVEY-7U5W
9. A no-correlation event gives its handler a fresh well-formed chain, and two
   give two different ones →
   R-USZ5-GAOI
10. A listener sees one publish and one consume record per event, each with
    address, chain id, outcome, and duration — failures included →
    R-UXUQ-ZDNA, R-V1IG-4OVD, R-V0AJ-QX4O, R-V2QC-IGM2, R-V3Y8-W8CR
11. A hook that panics on every call changes nothing a consumer sees, across a
    reconnect →
    R-V565-A03G
12. Delivery otherwise unchanged: in order, at least once, same recovery →
    R-UWMU-LLWL, R-42P0-U6JE
