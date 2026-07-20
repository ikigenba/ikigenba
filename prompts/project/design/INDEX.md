# prompts agentkit migration — Design Index

Each Decision maps to its `DNN.md` file. Every `R-XXXX-XXXX` id maps to its Decision and file. Regenerate this index when a Decision is added or its Verification ids change. To resolve an id, grep this file or the Decision files directly.

## Decisions

| Decision | File | Title | Verification ids |
|----------|------|-------|-----------------|
| D1 | project/design/D01.md | Module dependency | none — structural |
| D2 | project/design/D02.md | Config struct | R-JTBA-4RDB, R-JUJ6-IJ40 |
| D3 | project/design/D03.md | Validation | R-JVR2-WAUP, R-JWYZ-A2LE, R-1ONM-PPDU, R-1PVJ-3H4J, R-1R3F-H8V8, R-1SBB-V0LX, R-1TJ8-8SCM, R-1UR4-MK3B, R-1VZ1-0BU0, R-JY6V-NUC3, R-JZES-1M2S |
| D4 | project/design/D04.md | Provider factory | none — structural |
| D5 | project/design/D05.md | Built-in sandbox tools | R-F5X1-XH6C, R-GNY2-Y47H, R-K1UK-T5K6 |
| D6 | project/design/D06.md | Suite discovery | R-K32H-6XAV, R-K4AD-KP1K, R-9JNO-RZM2, R-9KVL-5RCR, R-9M3H-JJ3G, R-EF0V-TP9R |
| D7 | project/design/D07.md | Runner | R-K5I9-YGS9, R-1X6X-E3KP, R-1ZMQ-5N23, R-K6Q6-C8IY, R-K7Y2-Q09N, R-K95Z-3S0C |
| D8 | project/design/D08.md | DB migration | R-KBLR-VBHQ, R-KCTO-938F |
| D9 | project/design/D09.md | MCP schema | R-KE1K-MUZ4, R-20UM-JESS, R-222I-X6JH |
| D10 | project/design/D10.md | Two front doors: the session-gated human root and the bearer-gated agent surface | R-LAND-ROOT, R-LAND-UNGT, R-7NY0-UIO6, R-7P5X-8AEV |
| D12 | project/design/D12.md | A top-left Home link to the dashboard landing page | R-HOME-2T4X |
| D13 | project/design/D13.md | Self-serve the pages' fonts and eliminate the FOUT | R-DFKP-IVZU, R-DGSL-WNQJ, R-DI0I-AFH8, R-DJ8E-O77X, R-DKGB-1YYM |
| D14 | project/design/D14.md | Adopt the shared `registry` for all loopback addressing | R-RG01-PORT, R-RG03-DBOX, R-RG04-NLIT |
| D15 | project/design/D15.md | Consumer loops through `Spec.Consumers` (chassis-owned) | R-DFV4-7W4Y, R-DH30-LNVN |
| D16 | project/design/D16.md | Web surface from `share/www` through the chassis (de-embed) | R-DIAW-ZFMC, R-DJIT-D7D1 |
| D17 | project/design/D17.md | MCP surface over `appkit/mcp`: `internal/mcp` becomes the tool table | R-DKQP-QZ3Q, R-DLYM-4QUF |
| D18 | project/design/D18.md | Delete the chassis shims (`internal/db` wrappers) and true up the doctrine doc | none — structural |
| D19 | project/design/D19.md | Progressive suite-tool discovery (deferred suite tools) | R-9NBD-XAU5, R-9OJA-B2KU, R-9PR6-OUBJ, R-A69O-ATWI |
| D20 | project/design/D20.md | Session-gated locations opt into the apex `@login_bounce` (bearer tier excluded) | R-3RIS-23TJ, R-3SQO-FVK8, R-3TYK-TNAX |
| D21 | project/design/D21.md | Content-plane acceptor: the `Fetch` sandbox tool | R-65YV-4ES6, R-676R-I6IV, R-68EN-VY9K, R-69MK-9Q09, R-6AUG-NHQY |
| D22 | project/design/D22.md | Content-plane holder: run sandbox files at `GET /run-content` | R-6C2D-19HN, R-6EI5-SSZ1, R-6FQ2-6KPQ |
| D23 | project/design/D23.md | Box PDF tooling in the framing prompt; model-native PDF is a non-goal | R-6I5U-Y474 |
| D24 | project/design/D24.md | Event-routing conformance: triggers become canonical filter strings | R-6JDR-BVXT, R-6KLN-PNOI, R-6LTK-3FF7, R-6N1G-H75W, R-6O9C-UYWL, R-6PH9-8QNA, R-6QP5-MIDZ, R-6RX2-0A4O |
| D25 | project/design/D25.md | Event-routing conformance: producer kinds `run.succeeded`/`run.failed`, subject = /<prompt name> | R-6T4Y-E1VD, R-6UCU-RTM2, R-6VKR-5LCR, R-ZS8A-TVOF |
| D26 | project/design/D26.md | File-share sandbox tools: `File*` over the share's loopback filesystem API | R-F74Y-B8X1, R-F8CU-P0NQ, R-F9KR-2SEF, R-FASN-GK54, R-FC0J-UBVT, R-FD8G-83MI, R-FEGC-LVD7 |
| D27 | project/design/D27.md | Structured MCP adoption: `StructuredResult`, typed error codes, output schemas, shared loopback guard | R-B4QM-WZGJ, R-B5YJ-AR78, R-B76F-OIXX, R-B8EC-2AOM, R-B9M8-G2FB, R-BC21-7LWP, R-BD9X-LDNE, R-BEHT-Z5E3, R-BFPQ-CX4S, R-BGXM-QOVH, R-BI5J-4GM6 |
| D28 | project/design/D28.md | The `calls` table: one durable row per inference unit | R-5J1W-8BCM, R-5K9S-M33B, R-5LHO-ZUU0, R-5MPL-DMKP, R-5NXH-REBE |
| D29 | project/design/D29.md | `POST /complete`: the synchronous one-shot completion endpoint | R-5P5E-5623, R-5QDA-IXSS, R-5ST3-AHA6, R-5U0Z-O90V, R-5V8W-20RK, R-5WGS-FSI9, R-5XOO-TK8Y, R-5YWL-7BZN |
| D30 | project/design/D30.md | `POST /embed`: the synchronous embedding endpoint | R-604H-L3QC, R-61CD-YVH1, R-62KA-CN7Q, R-63S6-QEYF, R-6503-46P4, R-667Z-HYFT |
| D31 | project/design/D31.md | Admission control: bounded inference concurrency | R-67FV-VQ6I, R-68NS-9HX7, R-6B3L-11EL, R-6CBH-ET5A |
| D32 | project/design/D32.md | `calls` and `usage`: the inspection and reporting MCP tools | R-6DJD-SKVZ, R-6ERA-6CMO, R-6FZ6-K4DD, R-6H72-XW42, R-6IEZ-BNUR |
| D33 | project/design/D33.md | Sessions on the record: runs write `calls` rows | R-6JMV-PFLG, R-6KUS-37C5, R-6M2O-GZ2U, R-6NAK-UQTJ |
| D34 | project/design/D34.md | The `ui/` namespace: one session-gated prefix for the human browse surface | R-ZW7P-88WL, R-ZXFL-M0NA, R-ZYNH-ZSDZ |
| D35 | project/design/D35.md | The browse UI: server-rendered prompts/runs pages with a per-run calls log | R-ZZVE-DK4O, R-013A-RBVD, R-03J3-IVCR, R-04QZ-WN3G, R-05YW-AEU5, R-076S-O6KU, R-08EP-1YBJ, R-09ML-FQ28, R-0AUH-THSX, R-0C2E-79JM, R-0DAA-L1AB, R-0EI6-YT10, R-0FQ3-CKRP, R-0GXZ-QCIE, R-0I5W-4493, R-0JDS-HVZS, R-LAND-NMVR, R-LAND-CARB |
| D36 | project/design/D36.md | Owner-id keying: rebuild `prompts`/`runs`, rekey store on `owner_id` | R-E59O-RJC7, R-E6HL-5B2W, R-E7PH-J2TL, R-E8XD-WUKA |
| D37 | project/design/D37.md | Owner-id at the MCP tool surface: scope on `X-Owner-Id`, snapshot the email, expose both | R-EBD6-OE1O, R-ECL3-25SD, R-EDSZ-FXJ2 |

## Verification ids → Decision

| id | Decision | File |
|----|----------|------|
| R-013A-RBVD | D35 | project/design/D35.md |
| R-03J3-IVCR | D35 | project/design/D35.md |
| R-04QZ-WN3G | D35 | project/design/D35.md |
| R-05YW-AEU5 | D35 | project/design/D35.md |
| R-076S-O6KU | D35 | project/design/D35.md |
| R-08EP-1YBJ | D35 | project/design/D35.md |
| R-09ML-FQ28 | D35 | project/design/D35.md |
| R-0AUH-THSX | D35 | project/design/D35.md |
| R-0C2E-79JM | D35 | project/design/D35.md |
| R-0DAA-L1AB | D35 | project/design/D35.md |
| R-0EI6-YT10 | D35 | project/design/D35.md |
| R-0FQ3-CKRP | D35 | project/design/D35.md |
| R-0GXZ-QCIE | D35 | project/design/D35.md |
| R-0I5W-4493 | D35 | project/design/D35.md |
| R-0JDS-HVZS | D35 | project/design/D35.md |
| R-1ONM-PPDU | D3 | project/design/D03.md |
| R-1PVJ-3H4J | D3 | project/design/D03.md |
| R-1R3F-H8V8 | D3 | project/design/D03.md |
| R-1SBB-V0LX | D3 | project/design/D03.md |
| R-1TJ8-8SCM | D3 | project/design/D03.md |
| R-1UR4-MK3B | D3 | project/design/D03.md |
| R-1VZ1-0BU0 | D3 | project/design/D03.md |
| R-1X6X-E3KP | D7 | project/design/D07.md |
| R-1ZMQ-5N23 | D7 | project/design/D07.md |
| R-20UM-JESS | D9 | project/design/D09.md |
| R-222I-X6JH | D9 | project/design/D09.md |
| R-3RIS-23TJ | D20 | project/design/D20.md |
| R-3SQO-FVK8 | D20 | project/design/D20.md |
| R-3TYK-TNAX | D20 | project/design/D20.md |
| R-5J1W-8BCM | D28 | project/design/D28.md |
| R-5K9S-M33B | D28 | project/design/D28.md |
| R-5LHO-ZUU0 | D28 | project/design/D28.md |
| R-5MPL-DMKP | D28 | project/design/D28.md |
| R-5NXH-REBE | D28 | project/design/D28.md |
| R-5P5E-5623 | D29 | project/design/D29.md |
| R-5QDA-IXSS | D29 | project/design/D29.md |
| R-5ST3-AHA6 | D29 | project/design/D29.md |
| R-5U0Z-O90V | D29 | project/design/D29.md |
| R-5V8W-20RK | D29 | project/design/D29.md |
| R-5WGS-FSI9 | D29 | project/design/D29.md |
| R-5XOO-TK8Y | D29 | project/design/D29.md |
| R-5YWL-7BZN | D29 | project/design/D29.md |
| R-604H-L3QC | D30 | project/design/D30.md |
| R-61CD-YVH1 | D30 | project/design/D30.md |
| R-62KA-CN7Q | D30 | project/design/D30.md |
| R-63S6-QEYF | D30 | project/design/D30.md |
| R-6503-46P4 | D30 | project/design/D30.md |
| R-65YV-4ES6 | D21 | project/design/D21.md |
| R-667Z-HYFT | D30 | project/design/D30.md |
| R-676R-I6IV | D21 | project/design/D21.md |
| R-67FV-VQ6I | D31 | project/design/D31.md |
| R-68EN-VY9K | D21 | project/design/D21.md |
| R-68NS-9HX7 | D31 | project/design/D31.md |
| R-69MK-9Q09 | D21 | project/design/D21.md |
| R-6AUG-NHQY | D21 | project/design/D21.md |
| R-6B3L-11EL | D31 | project/design/D31.md |
| R-6C2D-19HN | D22 | project/design/D22.md |
| R-6CBH-ET5A | D31 | project/design/D31.md |
| R-6DJD-SKVZ | D32 | project/design/D32.md |
| R-6EI5-SSZ1 | D22 | project/design/D22.md |
| R-6ERA-6CMO | D32 | project/design/D32.md |
| R-6FQ2-6KPQ | D22 | project/design/D22.md |
| R-6FZ6-K4DD | D32 | project/design/D32.md |
| R-6H72-XW42 | D32 | project/design/D32.md |
| R-6I5U-Y474 | D23 | project/design/D23.md |
| R-6IEZ-BNUR | D32 | project/design/D32.md |
| R-6JDR-BVXT | D24 | project/design/D24.md |
| R-6JMV-PFLG | D33 | project/design/D33.md |
| R-6KLN-PNOI | D24 | project/design/D24.md |
| R-6KUS-37C5 | D33 | project/design/D33.md |
| R-6LTK-3FF7 | D24 | project/design/D24.md |
| R-6M2O-GZ2U | D33 | project/design/D33.md |
| R-6N1G-H75W | D24 | project/design/D24.md |
| R-6NAK-UQTJ | D33 | project/design/D33.md |
| R-6O9C-UYWL | D24 | project/design/D24.md |
| R-6PH9-8QNA | D24 | project/design/D24.md |
| R-6QP5-MIDZ | D24 | project/design/D24.md |
| R-6RX2-0A4O | D24 | project/design/D24.md |
| R-6T4Y-E1VD | D25 | project/design/D25.md |
| R-6UCU-RTM2 | D25 | project/design/D25.md |
| R-6VKR-5LCR | D25 | project/design/D25.md |
| R-7NY0-UIO6 | D10 | project/design/D10.md |
| R-7P5X-8AEV | D10 | project/design/D10.md |
| R-9JNO-RZM2 | D6 | project/design/D06.md |
| R-9KVL-5RCR | D6 | project/design/D06.md |
| R-9M3H-JJ3G | D6 | project/design/D06.md |
| R-9NBD-XAU5 | D19 | project/design/D19.md |
| R-9OJA-B2KU | D19 | project/design/D19.md |
| R-9PR6-OUBJ | D19 | project/design/D19.md |
| R-A69O-ATWI | D19 | project/design/D19.md |
| R-B4QM-WZGJ | D27 | project/design/D27.md |
| R-B5YJ-AR78 | D27 | project/design/D27.md |
| R-B76F-OIXX | D27 | project/design/D27.md |
| R-B8EC-2AOM | D27 | project/design/D27.md |
| R-B9M8-G2FB | D27 | project/design/D27.md |
| R-BC21-7LWP | D27 | project/design/D27.md |
| R-BD9X-LDNE | D27 | project/design/D27.md |
| R-BEHT-Z5E3 | D27 | project/design/D27.md |
| R-BFPQ-CX4S | D27 | project/design/D27.md |
| R-BGXM-QOVH | D27 | project/design/D27.md |
| R-BI5J-4GM6 | D27 | project/design/D27.md |
| R-DFKP-IVZU | D13 | project/design/D13.md |
| R-DFV4-7W4Y | D15 | project/design/D15.md |
| R-DGSL-WNQJ | D13 | project/design/D13.md |
| R-DH30-LNVN | D15 | project/design/D15.md |
| R-DI0I-AFH8 | D13 | project/design/D13.md |
| R-DIAW-ZFMC | D16 | project/design/D16.md |
| R-DJ8E-O77X | D13 | project/design/D13.md |
| R-DJIT-D7D1 | D16 | project/design/D16.md |
| R-DKGB-1YYM | D13 | project/design/D13.md |
| R-DKQP-QZ3Q | D17 | project/design/D17.md |
| R-DLYM-4QUF | D17 | project/design/D17.md |
| R-E59O-RJC7 | D36 | project/design/D36.md |
| R-E6HL-5B2W | D36 | project/design/D36.md |
| R-E7PH-J2TL | D36 | project/design/D36.md |
| R-E8XD-WUKA | D36 | project/design/D36.md |
| R-EBD6-OE1O | D37 | project/design/D37.md |
| R-ECL3-25SD | D37 | project/design/D37.md |
| R-EDSZ-FXJ2 | D37 | project/design/D37.md |
| R-EF0V-TP9R | D6 | project/design/D06.md |
| R-F5X1-XH6C | D5 | project/design/D05.md |
| R-F74Y-B8X1 | D26 | project/design/D26.md |
| R-F8CU-P0NQ | D26 | project/design/D26.md |
| R-F9KR-2SEF | D26 | project/design/D26.md |
| R-FASN-GK54 | D26 | project/design/D26.md |
| R-FC0J-UBVT | D26 | project/design/D26.md |
| R-FD8G-83MI | D26 | project/design/D26.md |
| R-FEGC-LVD7 | D26 | project/design/D26.md |
| R-GNY2-Y47H | D5 | project/design/D05.md |
| R-HOME-2T4X | D12 | project/design/D12.md |
| R-JTBA-4RDB | D2 | project/design/D02.md |
| R-JUJ6-IJ40 | D2 | project/design/D02.md |
| R-JVR2-WAUP | D3 | project/design/D03.md |
| R-JWYZ-A2LE | D3 | project/design/D03.md |
| R-JY6V-NUC3 | D3 | project/design/D03.md |
| R-JZES-1M2S | D3 | project/design/D03.md |
| R-K1UK-T5K6 | D5 | project/design/D05.md |
| R-K32H-6XAV | D6 | project/design/D06.md |
| R-K4AD-KP1K | D6 | project/design/D06.md |
| R-K5I9-YGS9 | D7 | project/design/D07.md |
| R-K6Q6-C8IY | D7 | project/design/D07.md |
| R-K7Y2-Q09N | D7 | project/design/D07.md |
| R-K95Z-3S0C | D7 | project/design/D07.md |
| R-KBLR-VBHQ | D8 | project/design/D08.md |
| R-KCTO-938F | D8 | project/design/D08.md |
| R-KE1K-MUZ4 | D9 | project/design/D09.md |
| R-LAND-CARB | D35 | project/design/D35.md |
| R-LAND-NMVR | D35 | project/design/D35.md |
| R-LAND-ROOT | D10 | project/design/D10.md |
| R-LAND-UNGT | D10 | project/design/D10.md |
| R-RG01-PORT | D14 | project/design/D14.md |
| R-RG03-DBOX | D14 | project/design/D14.md |
| R-RG04-NLIT | D14 | project/design/D14.md |
| R-ZS8A-TVOF | D25 | project/design/D25.md |
| R-ZW7P-88WL | D34 | project/design/D34.md |
| R-ZXFL-M0NA | D34 | project/design/D34.md |
| R-ZYNH-ZSDZ | D34 | project/design/D34.md |
| R-ZZVE-DK4O | D35 | project/design/D35.md |
