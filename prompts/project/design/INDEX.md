# prompts — Design Index

Each Decision maps to its `DNN.md` file. Every `R-XXXX-XXXX` id maps to its Decision and file. Regenerate this index when a Decision is added or its Verification ids change. To resolve an id, grep this file or the Decision files directly.

## Decisions

| Decision | File | Title | Verification ids |
|----------|------|-------|-----------------|
| D1 | project/design/D01.md | Module dependency | none — structural |
| D2 | project/design/D02.md | Config struct | R-JTBA-4RDB, R-JUJ6-IJ40 |
| D3 | project/design/D03.md | Validation | R-JVR2-WAUP, R-JWYZ-A2LE, R-1ONM-PPDU, R-1PVJ-3H4J, R-1R3F-H8V8, R-1SBB-V0LX, R-1TJ8-8SCM, R-1UR4-MK3B, R-1VZ1-0BU0, R-JY6V-NUC3, R-JZES-1M2S, R-SVPV-O479, R-SWXS-1VXY, R-SY5O-FNON, R-SZDK-TFFC, R-T0LH-7761, R-ZAC5-D0ZY |
| D4 | project/design/D04.md | Provider factory | R-ZBK1-QSQN |
| D5 | project/design/D05.md | Built-in sandbox tools | R-F5X1-XH6C, R-GNY2-Y47H, R-K1UK-T5K6, R-ZCRY-4KHC |
| D6 | project/design/D06.md | Suite discovery: the inventory peer set and the live catalog | R-ORZ1-QIWX, R-OT6Y-4ANM, R-OUEU-I2EB |
| D7 | project/design/D07.md | Runner | R-K5I9-YGS9, R-1X6X-E3KP, R-1ZMQ-5N23, R-K6Q6-C8IY, R-K7Y2-Q09N, R-K95Z-3S0C, R-ZHNJ-NNG4 |
| D8 | project/design/D08.md | DB migration | R-KBLR-VBHQ, R-KCTO-938F |
| D9 | project/design/D09.md | MCP schema | R-KE1K-MUZ4, R-20UM-JESS, R-222I-X6JH |
| D10 | project/design/D10.md | Two front doors: the session-gated human root and the bearer-gated agent surface | R-LAND-ROOT, R-LAND-UNGT, R-7NY0-UIO6, R-7P5X-8AEV |
| D12 | project/design/D12.md | A top-left Home link to the dashboard landing page | R-HOME-2T4X |
| D13 | project/design/D13.md | Self-serve the pages' fonts and eliminate the FOUT | R-DFKP-IVZU, R-DGSL-WNQJ, R-DI0I-AFH8, R-DJ8E-O77X, R-DKGB-1YYM |
| D14 | project/design/D14.md | Adopt the shared `registry` for all loopback addressing | R-RG01-PORT, R-RG03-DBOX, R-RG04-NLIT |
| D15 | project/design/D15.md | Consumer loops through `Spec.Consumers` (chassis-owned) | R-DFV4-7W4Y, R-DH30-LNVN; adopts R-4LKF-FB23, R-8DF1-W89F, R-8IAN-FB87 |
| D16 | project/design/D16.md | Web surface from `share/www` through the chassis (de-embed) | R-DIAW-ZFMC, R-DJIT-D7D1 |
| D17 | project/design/D17.md | MCP surface over `appkit/mcp`: `internal/mcp` becomes the tool table | R-DKQP-QZ3Q, R-DLYM-4QUF |
| D18 | project/design/D18.md | Delete the chassis shims (`internal/db` wrappers) and true up the doctrine doc | none — structural |
| D19 | project/design/D19.md | Suite tools behind the three-tool gateway | R-OVMQ-VU50, R-OWUN-9LVP, R-OY2J-NDME, R-OZAG-15D3, R-P0IC-EX3S, R-P1Q8-SOUH, R-P2Y5-6GL6, R-P461-K8BV |
| D20 | project/design/D20.md | the session-gated locations opt into the apex `@login_bounce`: a logged-out human navigation goes to sign-in, not a bare 401 | R-3RIS-23TJ, R-3SQO-FVK8, R-3TYK-TNAX |
| D21 | project/design/D21.md | Content-plane acceptor: the `Fetch` sandbox tool | R-65YV-4ES6, R-676R-I6IV, R-68EN-VY9K, R-69MK-9Q09, R-6AUG-NHQY |
| D22 | project/design/D22.md | Content-plane holder: run sandbox files at `GET /run-content` | R-6C2D-19HN, R-6EI5-SSZ1, R-6FQ2-6KPQ |
| D23 | project/design/D23.md | Box PDF tooling in the framing prompt; model-native PDF is a non-goal | R-6I5U-Y474 |
| D24 | project/design/D24.md | Event-routing conformance: triggers become canonical filter strings (trigger surface + consumer) | R-6JDR-BVXT, R-6KLN-PNOI, R-6LTK-3FF7, R-6N1G-H75W, R-6O9C-UYWL, R-6PH9-8QNA, R-6QP5-MIDZ, R-6RX2-0A4O |
| D25 | project/design/D25.md | Event-routing conformance: producer kinds `run.succeeded`/`run.failed`, subject = /<prompt name> | R-6T4Y-E1VD, R-6UCU-RTM2, R-6VKR-5LCR, R-ZS8A-TVOF |
| D26 | project/design/D26.md | File-share sandbox tools: `File*` over the share's loopback filesystem API | R-F74Y-B8X1, R-F8CU-P0NQ, R-F9KR-2SEF, R-FASN-GK54, R-FC0J-UBVT, R-FD8G-83MI, R-FEGC-LVD7 |
| D27 | project/design/D27.md | Structured MCP adoption: `StructuredResult`, typed error codes, output schemas, shared loopback guard | R-B4QM-WZGJ, R-B5YJ-AR78, R-B76F-OIXX, R-B8EC-2AOM, R-B9M8-G2FB, R-BGXM-QOVH, R-BI5J-4GM6, R-BC21-7LWP, R-BD9X-LDNE, R-BEHT-Z5E3, R-BFPQ-CX4S |
| D28 | project/design/D28.md | The `calls` table: one durable row per inference unit | R-5J1W-8BCM, R-5K9S-M33B, R-5LHO-ZUU0, R-5MPL-DMKP, R-5NXH-REBE |
| D29 | project/design/D29.md | The completion queue: durable handoff for sibling-service inference | R-J7QG-FRDY, R-J8YC-TJ4N, R-JA69-7AVC, R-JBE5-L2M1, R-JCM1-YUCQ, R-JDTY-CM3F, R-JF1U-QDU4, R-JG9R-45KT, R-JHHN-HXBI, R-JJXG-9GSW, R-JL5C-N8JL, R-JMD9-10AA, R-JOT1-SJRO, R-JQ0Y-6BID, R-JR8U-K392, R-JSGQ-XUZR, R-U7PZ-0AIV, R-U8XV-E29K, R-ZJKL-8UQY, R-ZM0E-0E8C, R-ZKSH-MMHN, R-ZN8A-E5Z1, R-06QO-IHU5, R-032Z-D6M2, R-04AV-QYCR, R-ZPO3-5PGF, R-00N6-LN4O, R-01V2-ZEVD, R-05IS-4Q3G, R-ZOG6-RXPQ, R-ZQVZ-JH74, R-ZTBS-B0OI, R-ZUJO-OSF7, R-ZVRL-2K5W, R-ZWZH-GBWL, R-ZY7D-U3NA, R-ZZFA-7VDZ, R-096H-A1BJ |
| D30 | project/design/D30.md | `POST /embed`: the synchronous embedding endpoint | R-604H-L3QC, R-61CD-YVH1, R-62KA-CN7Q, R-63S6-QEYF, R-6503-46P4, R-667Z-HYFT |
| D31 | project/design/D31.md | Admission control: bounded inference concurrency | R-67FV-VQ6I, R-68NS-9HX7, R-6CBH-ET5A, R-6B3L-11EL |
| D32 | project/design/D32.md | `calls` and `usage`: the inspection and reporting MCP tools | R-6DJD-SKVZ, R-6ERA-6CMO, R-6FZ6-K4DD, R-6H72-XW42, R-6IEZ-BNUR |
| D33 | project/design/D33.md | Sessions on the record: runs write `calls` rows | R-6JMV-PFLG, R-6KUS-37C5, R-6M2O-GZ2U, R-6NAK-UQTJ |
| D34 | project/design/D34.md | The `ui/` namespace: one session-gated prefix for the human browse surface | R-ZW7P-88WL, R-ZXFL-M0NA, R-ZYNH-ZSDZ |
| D35 | project/design/D35.md | The browse UI: server-rendered prompts/runs pages with a per-run calls log | R-ZZVE-DK4O, R-013A-RBVD, R-03J3-IVCR, R-04QZ-WN3G, R-05YW-AEU5, R-076S-O6KU, R-08EP-1YBJ, R-09ML-FQ28, R-0AUH-THSX, R-0C2E-79JM, R-0DAA-L1AB, R-0EI6-YT10, R-0FQ3-CKRP, R-0GXZ-QCIE, R-0I5W-4493, R-0JDS-HVZS, R-LAND-NMVR, R-LAND-CARB |
| D36 | project/design/D36.md | Owner-id keying: rebuild `prompts`/`runs`, rekey the store on `owner_id` | R-E59O-RJC7, R-E6HL-5B2W, R-E7PH-J2TL, R-E8XD-WUKA |
| D37 | project/design/D37.md | Owner-id at the MCP tool surface: scope on `X-Owner-Id`, snapshot the email, expose both | R-EBD6-OE1O, R-ECL3-25SD, R-EDSZ-FXJ2 |
| D38 | project/design/D38.md | OpenAI subscription authentication (`auth: "sub"`) | R-T319-YQNF, R-T496-CIE4, R-T6OZ-41VI, R-T7WV-HTM7 |
| D39 | project/design/D39.md | The run directory is durable state, and holds the whole run | R-ZNR1-KI5L |
| D40 | project/design/D40.md | Two loaders: the live definition and the run's pinned commit | R-ZOYX-Y9WA, R-ZREQ-PTDO, R-ZSMN-3L4D |
| D41 | project/design/D41.md | `run_delete`: removing a run and everything it produced | R-ZTUJ-HCV2, R-ZV2F-V4LR, R-ZWAC-8WCG |
| D44 | project/design/D44.md | The correlation id on the record: `runs.correlation_id`, `calls.correlation_id`, mint-or-inherit at spawn | R-HIAG-2MGL, R-HJIC-GE7A, R-HKQ8-U5XZ, R-HLY5-7XOO, R-HN61-LPFD, R-HODX-ZH62 |
| D45 | project/design/D45.md | Chain propagation on suite peer calls: `X-Correlation-Id` on every gateway hop | R-P5DX-Y02K, R-P7TQ-PJJY |
| D46 | project/design/D46.md | nginx fragment: capture the edge-minted chain id on gated locations, strip it on the ungated one | R-HWX8-NVCX, R-HY55-1N3M |
| D47 | project/design/D47.md | Rebuild to adopt: event-plane chain continuation, the `root` record at spawn, and the recorded boundary of a run | R-HZD1-FEUB, R-I0KX-T6L0, R-I1SU-6YBP, R-I30Q-KQ2E, R-I48M-YHT3 |
| D48 | project/design/D48.md | State paths come from `appkit/config`, never from a hardcoded `./tmp` default | R-LBH5-4LO0, R-LCP1-IDEP, R-LDWX-W55E |
| D49 | project/design/D49.md | Env-channel conformance: composed inventory root, manifest-surfaced tuning knobs, a Spec-derived drift oracle, and the bounded run TTL | R-M51H-QWOL, R-M69E-4OFA; adopts R-VKB6-SHHV, R-34EZ-J9BF |
| D50 | project/design/D50.md | Adopt the suite testing-language contract | none of its own; adopts R-O1AD-MRKW, R-O2IA-0JBL |
| D51 | project/design/D51.md | A prompt definition is a git tree: the fixed layout and the repo name key | R-RDFP-IOO1, R-RENL-WGEQ, R-RFVI-A85F |
| D52 | project/design/D52.md | The version-plane client: one seam, injected at the composition root | R-IRRX-XZQ2, R-ISZU-BRGR, R-IU7Q-PJ7G, R-IVFN-3AY5, R-IWNJ-H2OU, R-IXVF-UUFJ, R-RIBB-1RMT, R-RJJ7-FJDI, R-RKR3-TB47 |
| D53 | project/design/D53.md | The write path: create, update, import, rename, and delete go through the version plane | R-RLZ0-72UW, R-ROES-YMCA, R-RPMP-CE2Z, R-RQUL-Q5TO, R-RS2I-3XKD, R-SGGH-RCE9 |
| D54 | project/design/D54.md | Seeding: the one-time, idempotent definition backfill | R-RTAE-HPB2, R-RUIA-VH1R, R-RVQ7-98SG |
| D55 | project/design/D55.md | A run pins a sha and executes a real clone | R-RWY3-N0J5, R-RY60-0S9U, R-S0LS-SBR8, R-S1TP-63HX, R-S31L-JV8M |
| D56 | project/design/D56.md | The run token and the authenticated git door | R-S49H-XMZB, R-S5HE-BEQ0, R-3AIH-G40W, R-RZDW-EK0J; adopts R-35MV-X124 |
| D57 | project/design/D57.md | The framing prompt tells the run its folder is a git clone | R-S953-GPY3, R-SACZ-UHOS |
| D58 | project/design/D58.md | `repos` joins the trigger sources: prompts as the version plane's workflow runner | R-SBKW-89FH, R-SCSS-M166 |
| D59 | project/design/D59.md | Retiring the content columns | R-SE0O-ZSWV, R-SF8L-DKNK |
| D60 | project/design/D60.md | The prompts-owned peer MCP client | R-OLVJ-TO7G, R-OOBC-L7OU, R-OPJ8-YZFJ, R-OQR5-CR68 |
| D61 | project/design/D61.md | Patch-semantics `update`: omitted means unchanged, and no empty prompt is ever stored | R-A8EU-5VUL, R-A9MQ-JNLA, R-AAUM-XFBZ, R-AC2J-B72O, R-ADAF-OYTD, R-AEIC-2QK2 |
| D62 | project/design/D62.md | Adopt the suite brand icon contract: the shipped icon set and its link markup | adopts R-RYDN-YNR5, R-RZLK-CFHU (root project/design/D29.md) |
| D63 | project/design/D63.md | Queue observability: the completion queue's state is visible without opening the database | R-07YK-W9KU, R-15VT-PVYT |
| D64 | project/design/D64.md | Adopt the suite lint contract (`root project/design/D30.md`) at tier `strict` | none — structural |

## Verification ids → Decision

| id | Decision | File |
|----|----------|------|
| R-00N6-LN4O | D29 | project/design/D29.md |
| R-013A-RBVD | D35 | project/design/D35.md |
| R-01V2-ZEVD | D29 | project/design/D29.md |
| R-032Z-D6M2 | D29 | project/design/D29.md |
| R-03J3-IVCR | D35 | project/design/D35.md |
| R-04AV-QYCR | D29 | project/design/D29.md |
| R-04QZ-WN3G | D35 | project/design/D35.md |
| R-05IS-4Q3G | D29 | project/design/D29.md |
| R-05YW-AEU5 | D35 | project/design/D35.md |
| R-06QO-IHU5 | D29 | project/design/D29.md |
| R-076S-O6KU | D35 | project/design/D35.md |
| R-07YK-W9KU | D63 | project/design/D63.md |
| R-08EP-1YBJ | D35 | project/design/D35.md |
| R-096H-A1BJ | D29 | project/design/D29.md |
| R-09ML-FQ28 | D35 | project/design/D35.md |
| R-0AUH-THSX | D35 | project/design/D35.md |
| R-0C2E-79JM | D35 | project/design/D35.md |
| R-0DAA-L1AB | D35 | project/design/D35.md |
| R-0EI6-YT10 | D35 | project/design/D35.md |
| R-0FQ3-CKRP | D35 | project/design/D35.md |
| R-0GXZ-QCIE | D35 | project/design/D35.md |
| R-0I5W-4493 | D35 | project/design/D35.md |
| R-0JDS-HVZS | D35 | project/design/D35.md |
| R-15VT-PVYT | D63 | project/design/D63.md |
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
| R-34EZ-J9BF | D49 | project/design/D49.md (adopted from `root project/design/D26.md`) |
| R-35MV-X124 | D56 | project/design/D56.md (adopted from `root project/design/D26.md`) |
| R-3AIH-G40W | D56 | project/design/D56.md |
| R-3RIS-23TJ | D20 | project/design/D20.md |
| R-3SQO-FVK8 | D20 | project/design/D20.md |
| R-3TYK-TNAX | D20 | project/design/D20.md |
| R-4LKF-FB23 | D15 | project/design/D15.md (adopted from root `project/design/D08.md`) |
| R-5J1W-8BCM | D28 | project/design/D28.md |
| R-5K9S-M33B | D28 | project/design/D28.md |
| R-5LHO-ZUU0 | D28 | project/design/D28.md |
| R-5MPL-DMKP | D28 | project/design/D28.md |
| R-5NXH-REBE | D28 | project/design/D28.md |
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
| R-8DF1-W89F | D15 | project/design/D15.md (adopted from root `project/design/D11.md`) |
| R-8IAN-FB87 | D15 | project/design/D15.md (adopted from root `project/design/D11.md`) |
| R-A8EU-5VUL | D61 | project/design/D61.md |
| R-A9MQ-JNLA | D61 | project/design/D61.md |
| R-AAUM-XFBZ | D61 | project/design/D61.md |
| R-AC2J-B72O | D61 | project/design/D61.md |
| R-ADAF-OYTD | D61 | project/design/D61.md |
| R-AEIC-2QK2 | D61 | project/design/D61.md |
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
| R-F5X1-XH6C | D5 | project/design/D05.md |
| R-F74Y-B8X1 | D26 | project/design/D26.md |
| R-F8CU-P0NQ | D26 | project/design/D26.md |
| R-F9KR-2SEF | D26 | project/design/D26.md |
| R-FASN-GK54 | D26 | project/design/D26.md |
| R-FC0J-UBVT | D26 | project/design/D26.md |
| R-FD8G-83MI | D26 | project/design/D26.md |
| R-FEGC-LVD7 | D26 | project/design/D26.md |
| R-GNY2-Y47H | D5 | project/design/D05.md |
| R-HIAG-2MGL | D44 | project/design/D44.md |
| R-HJIC-GE7A | D44 | project/design/D44.md |
| R-HKQ8-U5XZ | D44 | project/design/D44.md |
| R-HLY5-7XOO | D44 | project/design/D44.md |
| R-HN61-LPFD | D44 | project/design/D44.md |
| R-HODX-ZH62 | D44 | project/design/D44.md |
| R-HOME-2T4X | D12 | project/design/D12.md |
| R-HWX8-NVCX | D46 | project/design/D46.md |
| R-HY55-1N3M | D46 | project/design/D46.md |
| R-HZD1-FEUB | D47 | project/design/D47.md |
| R-I0KX-T6L0 | D47 | project/design/D47.md |
| R-I1SU-6YBP | D47 | project/design/D47.md |
| R-I30Q-KQ2E | D47 | project/design/D47.md |
| R-I48M-YHT3 | D47 | project/design/D47.md |
| R-IRRX-XZQ2 | D52 | project/design/D52.md |
| R-ISZU-BRGR | D52 | project/design/D52.md |
| R-IU7Q-PJ7G | D52 | project/design/D52.md |
| R-IVFN-3AY5 | D52 | project/design/D52.md |
| R-IWNJ-H2OU | D52 | project/design/D52.md |
| R-IXVF-UUFJ | D52 | project/design/D52.md |
| R-J7QG-FRDY | D29 | project/design/D29.md |
| R-J8YC-TJ4N | D29 | project/design/D29.md |
| R-JA69-7AVC | D29 | project/design/D29.md |
| R-JBE5-L2M1 | D29 | project/design/D29.md |
| R-JCM1-YUCQ | D29 | project/design/D29.md |
| R-JDTY-CM3F | D29 | project/design/D29.md |
| R-JF1U-QDU4 | D29 | project/design/D29.md |
| R-JG9R-45KT | D29 | project/design/D29.md |
| R-JHHN-HXBI | D29 | project/design/D29.md |
| R-JJXG-9GSW | D29 | project/design/D29.md |
| R-JL5C-N8JL | D29 | project/design/D29.md |
| R-JMD9-10AA | D29 | project/design/D29.md |
| R-JOT1-SJRO | D29 | project/design/D29.md |
| R-JQ0Y-6BID | D29 | project/design/D29.md |
| R-JR8U-K392 | D29 | project/design/D29.md |
| R-JSGQ-XUZR | D29 | project/design/D29.md |
| R-JTBA-4RDB | D2 | project/design/D02.md |
| R-JUJ6-IJ40 | D2 | project/design/D02.md |
| R-JVR2-WAUP | D3 | project/design/D03.md |
| R-JWYZ-A2LE | D3 | project/design/D03.md |
| R-JY6V-NUC3 | D3 | project/design/D03.md |
| R-JZES-1M2S | D3 | project/design/D03.md |
| R-K1UK-T5K6 | D5 | project/design/D05.md |
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
| R-LBH5-4LO0 | D48 | project/design/D48.md |
| R-LCP1-IDEP | D48 | project/design/D48.md |
| R-LDWX-W55E | D48 | project/design/D48.md |
| R-M51H-QWOL | D49 | project/design/D49.md |
| R-M69E-4OFA | D49 | project/design/D49.md |
| R-O1AD-MRKW | D50 | project/design/D50.md (adopted from `root project/design/D23.md`) |
| R-O2IA-0JBL | D50 | project/design/D50.md (adopted from `root project/design/D23.md`) |
| R-OLVJ-TO7G | D60 | project/design/D60.md |
| R-OOBC-L7OU | D60 | project/design/D60.md |
| R-OPJ8-YZFJ | D60 | project/design/D60.md |
| R-OQR5-CR68 | D60 | project/design/D60.md |
| R-ORZ1-QIWX | D6 | project/design/D06.md |
| R-OT6Y-4ANM | D6 | project/design/D06.md |
| R-OUEU-I2EB | D6 | project/design/D06.md |
| R-OVMQ-VU50 | D19 | project/design/D19.md |
| R-OWUN-9LVP | D19 | project/design/D19.md |
| R-OY2J-NDME | D19 | project/design/D19.md |
| R-OZAG-15D3 | D19 | project/design/D19.md |
| R-P0IC-EX3S | D19 | project/design/D19.md |
| R-P1Q8-SOUH | D19 | project/design/D19.md |
| R-P2Y5-6GL6 | D19 | project/design/D19.md |
| R-P461-K8BV | D19 | project/design/D19.md |
| R-P5DX-Y02K | D45 | project/design/D45.md |
| R-P7TQ-PJJY | D45 | project/design/D45.md |
| R-RDFP-IOO1 | D51 | project/design/D51.md |
| R-RENL-WGEQ | D51 | project/design/D51.md |
| R-RFVI-A85F | D51 | project/design/D51.md |
| R-RG01-PORT | D14 | project/design/D14.md |
| R-RG03-DBOX | D14 | project/design/D14.md |
| R-RG04-NLIT | D14 | project/design/D14.md |
| R-RIBB-1RMT | D52 | project/design/D52.md |
| R-RJJ7-FJDI | D52 | project/design/D52.md |
| R-RKR3-TB47 | D52 | project/design/D52.md |
| R-RLZ0-72UW | D53 | project/design/D53.md |
| R-ROES-YMCA | D53 | project/design/D53.md |
| R-RPMP-CE2Z | D53 | project/design/D53.md |
| R-RQUL-Q5TO | D53 | project/design/D53.md |
| R-RS2I-3XKD | D53 | project/design/D53.md |
| R-RTAE-HPB2 | D54 | project/design/D54.md |
| R-RUIA-VH1R | D54 | project/design/D54.md |
| R-RVQ7-98SG | D54 | project/design/D54.md |
| R-RWY3-N0J5 | D55 | project/design/D55.md |
| R-RY60-0S9U | D55 | project/design/D55.md |
| R-RYDN-YNR5 | D62 | project/design/D62.md |
| R-RZDW-EK0J | D56 | project/design/D56.md |
| R-RZLK-CFHU | D62 | project/design/D62.md |
| R-S0LS-SBR8 | D55 | project/design/D55.md |
| R-S1TP-63HX | D55 | project/design/D55.md |
| R-S31L-JV8M | D55 | project/design/D55.md |
| R-S49H-XMZB | D56 | project/design/D56.md |
| R-S5HE-BEQ0 | D56 | project/design/D56.md |
| R-S953-GPY3 | D57 | project/design/D57.md |
| R-SACZ-UHOS | D57 | project/design/D57.md |
| R-SBKW-89FH | D58 | project/design/D58.md |
| R-SCSS-M166 | D58 | project/design/D58.md |
| R-SE0O-ZSWV | D59 | project/design/D59.md |
| R-SF8L-DKNK | D59 | project/design/D59.md |
| R-SGGH-RCE9 | D53 | project/design/D53.md |
| R-SVPV-O479 | D3 | project/design/D03.md |
| R-SWXS-1VXY | D3 | project/design/D03.md |
| R-SY5O-FNON | D3 | project/design/D03.md |
| R-SZDK-TFFC | D3 | project/design/D03.md |
| R-T0LH-7761 | D3 | project/design/D03.md |
| R-T319-YQNF | D38 | project/design/D38.md |
| R-T496-CIE4 | D38 | project/design/D38.md |
| R-T6OZ-41VI | D38 | project/design/D38.md |
| R-T7WV-HTM7 | D38 | project/design/D38.md |
| R-U7PZ-0AIV | D29 | project/design/D29.md |
| R-U8XV-E29K | D29 | project/design/D29.md |
| R-VKB6-SHHV | D49 | project/design/D49.md (adopted from root `project/design/D11.md`) |
| R-XXXX-XXXX | D18 | project/design/D18.md |
| R-ZAC5-D0ZY | D3 | project/design/D03.md |
| R-ZBK1-QSQN | D4 | project/design/D04.md |
| R-ZCRY-4KHC | D5 | project/design/D05.md |
| R-ZHNJ-NNG4 | D7 | project/design/D07.md |
| R-ZJKL-8UQY | D29 | project/design/D29.md |
| R-ZKSH-MMHN | D29 | project/design/D29.md |
| R-ZM0E-0E8C | D29 | project/design/D29.md |
| R-ZN8A-E5Z1 | D29 | project/design/D29.md |
| R-ZNR1-KI5L | D39 | project/design/D39.md |
| R-ZOG6-RXPQ | D29 | project/design/D29.md |
| R-ZOYX-Y9WA | D40 | project/design/D40.md |
| R-ZPO3-5PGF | D29 | project/design/D29.md |
| R-ZQVZ-JH74 | D29 | project/design/D29.md |
| R-ZREQ-PTDO | D40 | project/design/D40.md |
| R-ZS8A-TVOF | D25 | project/design/D25.md |
| R-ZSMN-3L4D | D40 | project/design/D40.md |
| R-ZTBS-B0OI | D29 | project/design/D29.md |
| R-ZTUJ-HCV2 | D41 | project/design/D41.md |
| R-ZUJO-OSF7 | D29 | project/design/D29.md |
| R-ZV2F-V4LR | D41 | project/design/D41.md |
| R-ZVRL-2K5W | D29 | project/design/D29.md |
| R-ZW7P-88WL | D34 | project/design/D34.md |
| R-ZWAC-8WCG | D41 | project/design/D41.md |
| R-ZWZH-GBWL | D29 | project/design/D29.md |
| R-ZXFL-M0NA | D34 | project/design/D34.md |
| R-ZY7D-U3NA | D29 | project/design/D29.md |
| R-ZYNH-ZSDZ | D34 | project/design/D34.md |
| R-ZZFA-7VDZ | D29 | project/design/D29.md |
| R-ZZVE-DK4O | D35 | project/design/D35.md |

## Success criteria → ids

One line per product success criterion, in product order, mapped to the id(s) whose tests prove it end to end against the assembled artifact.

| # | Success criterion (product order) | Proving ids |
|---|-----------------------------------|-------------|
| 1 | An `openai` prompt on a catalog model runs against the real API | R-JZES-1M2S, R-K5I9-YGS9 |
| 2 | A model-only prompt stores and runs on that model's default provider, OpenRouter included | R-1ONM-PPDU, R-1X6X-E3KP |
| 3 | An unknown provider is rejected at create; no row | R-JVR2-WAUP |
| 4 | An out-of-catalog model, or a provider that cannot serve it, is rejected at create | R-JWYZ-A2LE, R-1PVJ-3H4J |
| 5 | A reasoning setting the model does not accept is rejected, naming the accepted options | R-1R3F-H8V8, R-1SBB-V0LX, R-1TJ8-8SCM |
| 6 | `describe` lists every catalog model; a listed model is accepted and an unlisted one rejected | R-222I-X6JH, R-KE1K-MUZ4 |
| 7 | Config values apply to the run; an omitted key reverts to the default on the next run | R-JTBA-4RDB, R-JUJ6-IJ40 |
| 8 | An update sending only config leaves instructions, system prompt, and name untouched | R-A8EU-5VUL |
| 9 | No create or update may leave a prompt with empty instructions | R-AC2J-B72O, R-ADAF-OYTD, R-A9MQ-JNLA |
| 10 | A pre-migration prompt with no provider still runs after the migration | R-KBLR-VBHQ, R-KCTO-938F |
| 11 | A `zai` prompt with `base_url` targets that URL | R-JTBA-4RDB, R-1X6X-E3KP |
| 12 | A config value the model ignores does not fail the run | R-1UR4-MK3B |
| 13 | Editing a prompt mid-run does not affect that run | R-ZOYX-Y9WA |
| 14 | A logged-in user lands in the styled browse surface; a logged-out one is sent to sign in | R-ZW7P-88WL, R-04QZ-WN3G, R-LAND-NMVR, R-ZYNH-ZSDZ |
| 15 | The runs tab narrows by status or prompt and pages, server-side | R-08EP-1YBJ, R-09ML-FQ28, R-0AUH-THSX, R-076S-O6KU |
| 16 | A run's log shows every call in order, notes aged-out bodies, and keeps oversized text retrievable | R-0EI6-YT10, R-0FQ3-CKRP, R-0GXZ-QCIE, R-0I5W-4493 |
| 17 | A link to a deleted prompt gives a clear page, not a bare error | R-0DAA-L1AB, R-0JDS-HVZS |
| 18 | Every loopback address comes from the registry; no port literal in production source | R-RG01-PORT, R-RG03-DBOX, R-RG04-NLIT |
| 19 | A run discovers a suite service, loads its tools, and completes the task | R-P461-K8BV, R-OWUN-9LVP, R-OZAG-15D3 |
| 20 | A dead suite service errors only its own calls; the run still completes | R-P1Q8-SOUH, R-OUEU-I2EB |
| 21 | A run finished before a restart is still fully readable after it | R-ZNR1-KI5L |
| 22 | Deleting a prompt leaves its runs intact and readable | R-ZWAC-8WCG, R-ZREQ-PTDO |
| 23 | A run opened after its prompt was edited shows the text it actually executed | R-ZOYX-Y9WA, R-S0LS-SBR8 |
| 24 | A prompt created through an agent is already under version control at its first revision | R-RLZ0-72UW, R-RTAE-HPB2 |
| 25 | A file pushed beside a prompt's instructions appears in the next run's working folder | R-S5HE-BEQ0, R-S31L-JV8M |
| 26 | A name differing only in punctuation is refused, naming the holder | R-RDFP-IOO1, R-RENL-WGEQ |
| 27 | Two runs either side of an edit name different revisions, each re-readable | R-RWY3-N0J5, R-S0LS-SBR8 |
| 28 | A run's proposed change waits under its own name, authoritative version untouched | R-RZDW-EK0J, R-S49H-XMZB |
| 29 | A run told nothing about folding work in folds nothing in | R-SACZ-UHOS, R-S953-GPY3 |
| 30 | Deleting a prompt archives its folder rather than destroying it | R-RQUL-Q5TO, R-IRRX-XZQ2 |
| 31 | A change landing in a watched version-controlled folder runs its prompt | R-SBKW-89FH, R-SCSS-M166 |
| 32 | A deleted run is gone completely, and every other run is untouched | R-ZTUJ-HCV2, R-ZV2F-V4LR |
| 33 | A run pulls a shared file in and its report lands back in the share | R-F8CU-P0NQ, R-F9KR-2SEF, R-6QP5-MIDZ |
| 34 | A shared file far larger than any message round-trips and the run completes | R-F8CU-P0NQ, R-FD8G-83MI |
| 35 | A file a run saves into a watched folder triggers the workflows watching it | R-6O9C-UYWL, R-6PH9-8QNA |
| 36 | A sibling submits a completion, is accepted immediately, and later collects the JSON reply; a bad submission is refused with no record | R-J7QG-FRDY, R-JBE5-L2M1, R-JA69-7AVC |
| 37 | A completion continuing a prior exchange answers against that full history | R-JCM1-YUCQ |
| 38 | A completion slower than any request could tolerate is still there to collect | R-JHHN-HXBI, R-JJXG-9GSW |
| 39 | Prompts restarts with completions in flight and none are lost to the restart | R-ZJKL-8UQY, R-ZN8A-E5Z1 |
| 40 | A returning consumer finds its finished results waiting, and acknowledging removes them | R-JL5C-N8JL, R-JMD9-10AA |
| 41 | The same consumer and key twice executes once | R-J8YC-TJ4N |
| 42 | Every failure cause — model, provider, runtime bound, repeated abandonment — is reported explicitly with its reason | R-JF1U-QDU4, R-JG9R-45KT, R-JHHN-HXBI, R-JDTY-CM3F, R-ZKSH-MMHN |
| 43 | A completion whose execution is lost is carried to an answer anyway; none is left stranded | R-ZJKL-8UQY, R-ZKSH-MMHN, R-ZOG6-RXPQ, R-ZTBS-B0OI, R-ZUJO-OSF7 |
| 44 | Refusals and failures carry a stable code saying whether a retry could help | R-ZY7D-U3NA, R-ZZFA-7VDZ |
| 45 | Ordinary restarts do not consume an item's tolerance for abandonment | R-ZN8A-E5Z1 |
| 46 | The owner sees queue depth, oldest waiting item, and abandoned work from the running service | R-07YK-W9KU, R-15VT-PVYT |
| 47 | An embedding batch returns one vector per text, in order, with usage and cost | R-604H-L3QC |
| 48 | A run, a completion, and an embedding are all listable with class, cause, name, model, tokens, cost, timing | R-5J1W-8BCM, R-6DJD-SKVZ, R-6JMV-PFLG |
| 49 | Totals grouped by workload name add up to the individual records | R-5NXH-REBE, R-6FZ6-K4DD |
| 50 | Bodies are readable within the retention window and reported as pruned after it | R-5K9S-M33B, R-6ERA-6CMO |
| 51 | Concurrency caps hold, every run completes, and service work still progresses while sessions saturate | R-67FV-VQ6I, R-68NS-9HX7, R-6B3L-11EL |
| 52 | `auth: "sub"` runs on the subscription when provisioned and is rejected naming the credential when not | R-T319-YQNF, R-SZDK-TFFC, R-T496-CIE4 |
| 53 | `auth: "sub"` with a non-OpenAI provider, an unknown value, or a custom `base_url` is rejected | R-SVPV-O479, R-SWXS-1VXY, R-SY5O-FNON |
| 54 | A record of another service's work leads back to the causing run and its conversation, sharing one chain id | R-HKQ8-U5XZ, R-HN61-LPFD, R-HODX-ZH62, R-HZD1-FEUB |
