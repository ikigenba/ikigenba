# wiki — Design Index

Each Decision maps to its `DNN.md` file; every Verification id maps to the Decision that owns it. Id lookup is a grep against this index. This file is regenerated whenever a Decision is added or its Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Dependency on the prompts service (unified inference; no provider dependency anywhere) — owns R-KDHD-V3XI, R-A3D7-NTLV
- D2 → `project/design/D02.md` — Service skeleton: package layout, Spec wiring, and the config/secret composition root — owns R-6RVX-P1IG; adopts R-4LKF-FB23 (root `project/design/D08.md`), R-8DF1-W89F, R-8IAN-FB87 (root `project/design/D11.md`)
- D3 → `project/design/D03.md` — The phase-1 data model — owns R-7SNG-0G9A, R-7TVC-E7ZZ, R-7V38-RZQO, R-7WB5-5RHD, R-RU0J-77HX, R-RV8F-KZ8M, R-RXO8-CIQ0, R-RYW4-QAGP, R-S041-427E, R-S1BX-HTY3, R-S2JT-VLOS, R-1O8B-FNX4
- D4 → `project/design/D04.md` — The ingest pipeline: handoff and apply over the completion queue — owns R-M8RN-87WV, R-K73J-J3W3, R-K8BF-WVMS, R-K9JC-ANDH, R-KAR8-OF46, R-KBZ5-26UV, R-KEEX-TQC9, R-KFMU-7I2Y, R-MB7F-ZRE9, R-MCFC-DJ4Y, R-MG31-IUD1, R-OQJZ-K337, R-ORRV-XUTW, R-OSZS-BMKL, R-OU7O-PEBA, R-OVFL-361Z
- D5 → `project/design/D05.md` — The LLM seam (`internal/llm`): the prompts completion-queue client — owns R-JW4G-367U, R-JXCC-GXYJ, R-JYK8-UPP8, R-JZS5-8HFX, R-K101-M96M, R-K27Y-00XB, R-K3FU-DSO0, R-K4NQ-RKEP, R-UCLK-JDHN, R-UDTG-X58C, R-9S84-C6J2
- D6 → `project/design/D06.md` — The extract stage (`internal/extract`) — owns R-VYU0-BPAX, R-XJBY-H8JZ, R-XKJU-V0AO, R-W19T-38SB, R-9UTW-ZFF0, R-9W1T-D75P, R-8TAF-XBSM, R-8UIC-B3JB, R-7BO4-JMO9, R-7CW0-XEEY, R-7E3X-B65N
- D7 → `project/design/D07.md` — The compile stage (`internal/compile`): full recompile from claims, clean article prose (no ids), 12k cap enforced — owns R-FQLB-QWS6, R-FT14-IG9K, R-FU90-W809, R-FVGX-9ZQY, R-9X9P-QYWE, R-9YHM-4QN3, R-VA32-HERT
- D8 → `project/design/D08.md` — Search returns: hybrid retrieval over pages, behind one seam — none — structural
- D9 → `project/design/D09.md` — `ask` (`internal/ask`): hybrid-retrieval pipeline, grounded/cited/honest-empty — owns R-BAFW-D24P, R-BBNS-QTVE, R-BCVP-4LM3, R-5UPD-VVNA, R-5VXA-9NDZ, R-690G-MZTK, R-5X56-NF4O, R-6A8D-0RK9, R-05CG-3H6Y, R-9ZPI-IIDS, R-NFXF-9QGN, R-NH5B-NI7C, R-NID8-19Y1
- D10 → `project/design/D10.md` — The MCP tool surface (`internal/mcp`) + identity — owns R-MUQ4-K1JS, R-MVY0-XTAH, R-MX5X-BL16, R-MYDT-PCRV, R-MZLQ-34IK, R-1QO4-77EI, R-N4KO-2WTZ, R-01OQ-Y5YV, R-02WN-BXPK, R-044J-PPG9, R-03GW-PX5K, R-04HB-QM7T, R-8DB1-UI1Q, R-8EIY-89SF, R-1Y7B-TN7Y, R-N729-RY1I
- D11 → `project/design/D11.md` — Subject addressing: the public path == identity (`type/norm_name`) — owns R-DRX6-PWSW, R-DT53-3OJL, R-DUCZ-HGAA
- D12 → `project/design/D12.md` — Page links: read-time mention detection + markdown footer — owns R-ZUDC-NJIP, R-ZVL9-1B9E, R-ZWT5-F303, R-ZY11-SUQS, R-ZZ8Y-6MHH, R-00GU-KE86, R-1WP9-CLM9, R-1XX5-QDCY, R-1Z52-453N
- D14 → `project/design/D14.md` — Job lifecycle & control: `aborted`, abort, re-run, and atomic integrate — owns R-0SCX-95OZ, R-0TKT-MXFO, R-0USQ-0P6D, R-0W0M-EGX2, R-0X8I-S8NR, R-0YGF-60EG, R-0ZOB-JS55, R-10W7-XJVU, R-KGUQ-L9TN, R-KI2M-Z1KC
- D15 → `project/design/D15.md` — Cursor pagination: the contract + the list seams — owns R-17C5-VP2I, R-18K2-9GT7, R-XYAZ-V0XE, R-XZIW-8SO3, R-Y1YP-0C5H, R-19RY-N8JW, R-1C7R-ES1A, R-1DFN-SJRZ
- D16 → `project/design/D16.md` — MCP surface expansion: control & footprint verbs + paginated lists — owns R-37NS-BRXR, R-Y36L-E3W6, R-Y4EH-RVMV, R-38VO-PJOG, R-3A3L-3BF5, R-3CJD-UUWJ, R-3EZ6-MEDX, R-3G73-064M, R-E4WX-G9H2, R-1T3W-YQVW
- D17 → `project/design/D17.md` — DB concurrency: a single-writer handle + a concurrent read pool (reads never blocked) — owns R-FUCC-IT4M, R-FVK8-WKVB, R-FWS5-ACM0, R-FY01-O4CP
- D18 → `project/design/D18.md` — Output-token budget & honest truncation handling — owns R-MSKH-GPX5, R-MTSD-UHNU, R-MV0A-89EJ, R-MW86-M158
- D19 → `project/design/D19.md` — Per-call-site configuration in production (retire the single global model) — owns R-GGIG-AN7W, R-GHQC-OEYL, R-GK65-FYFZ, R-GLE1-TQ6O, R-A25B-A1V6
- D25 → `project/design/D25.md` — Aliases table & name resolution — owns R-BGPF-NVTU, R-BHXC-1NKJ, R-BJ58-FFB8, R-BKD4-T71X, R-BLL1-6YSM, R-BMSX-KQJB, R-BO0T-YIA0, R-1PG7-TFNT
- D26 → `project/design/D26.md` — The merge work item & execution — owns R-NEFH-U8IO, R-NFNE-809D, R-NGVA-LS02, R-NI36-ZJQR, R-NJB3-DBHG, R-NKIZ-R385, R-NLQW-4UYU, R-NMYS-IMPJ, R-NPEL-A66X, R-HUDR-AWS9
- D27 → `project/design/D27.md` — Merge MCP surface (`merge` + `merges`) — owns R-DWDM-RVA7, R-DYTF-JERL, R-E01B-X6IA, R-E198-AY8Z, R-1RW0-KZ57, R-E2H4-OPZO, R-1VJP-QADA, R-E3P1-2HQD
- D28 → `project/design/D28.md` — Blackhole empty-normalization content — owns R-Z5JL-2IBS, R-Z6RH-GA2H, R-Z7ZD-U1T6
- D29 → `project/design/D29.md` — Alias-aware path entry for the read lookups (`page` / `claims`) — owns R-AF1X-PG7K, R-AG2Y-PH8L, R-AH3Z-PJ9M, R-AL5R-PL1P
- D30 → `project/design/D30.md` — Where page embeddings are stored — owns R-9OCK-FJK1, R-9PKG-TBAQ, R-9QSD-731F, R-9S09-KUS4
- D31 → `project/design/D31.md` — The keyword lane: full-text search returns — owns R-203P-F1ET, R-22JI-6KW7, R-23RE-KCMW, R-24ZA-Y4DL, R-NC9Q-4F8K
- D32 → `project/design/D32.md` — The meaning lane: in-memory vector search — owns R-3WOB-6U4Q, R-3XW7-KLVF, R-3Z43-YDM4, R-40C0-C5CT
- D33 → `project/design/D33.md` — Merging the two lanes: rank fusion + exact-name pin — owns R-79KD-1622, R-7AS9-EXSR, R-7C05-SPJG, R-7D82-6HA5, R-Q8RI-7POG, R-NDHM-I6Z9, R-NEPI-VYPY
- D34 → `project/design/D34.md` — The embedding call site: prompts `/embed`, one model for both sides — owns R-Z932-H2RA, R-1385-QVMS, R-14G2-4NDH, R-15NY-IF46
- D35 → `project/design/D35.md` — Keeping page vectors current, in the background — owns R-6XNX-FNXO, R-6YVT-TFOD, R-703Q-77F2, R-71BM-KZ5R, R-72JI-YQWG, R-73RF-CIN5
- D36 → `project/design/D36.md` — Preparing the question: one analysis call — owns R-QB7A-Z95U, R-QCF7-D0WJ, R-QDN3-QSN8, R-A0XE-WA4H
- D38 → `project/design/D38.md` — Keep the meaning lane consistent across a merge (close the D35 merge gap) — owns R-MRG8-K2WP, R-LP5Q-9XTD, R-EV2H-6RKN, R-FM7A-D0SZ, R-WS3C-J4QB
- D39 → `project/design/D39.md` — The human web surface: gating, embedded assets, and the exact-root mount — owns R-LAND-PG01, R-LAND-NMVR, R-LAND-CARB, R-LAND-ROOT, R-LAND-UNGT
- D41 → `project/design/D41.md` — The header-bar Home link targets the wiki's own landing — owns R-HIMC-K791
- D42 → `project/design/D42.md` — Web read surface: foundation (routes, base-href, package shape, seam injection) — owns R-WC29-XALJ, R-WDA6-B2C8, R-WFPZ-2LTM, R-WGXV-GDKB
- D43 → `project/design/D43.md` — The home / reset page (`GET /{$}`, no `q`) — none — composition (proof via D80/D81 ids)
- D44 → `project/design/D44.md` — The ask result page (`GET /{$}` with `?q=…`) + the `MentionsIn` seam — owns R-ARN9-5YPS, R-AU31-XI76, R-AVAY-B9XV, R-AWIU-P1OK, R-NPVU-26CX, R-AXQR-2TF9, R-8FQU-M1J4, R-2L34-0E1U, R-2MB0-E5SJ
- D45 → `project/design/D45.md` — The subject page (`GET /subject/{type}/{slug}`) — owns R-PH2F-47LB, R-PIAB-HZC0, R-PJI7-VR2P, R-PLY0-NAK3, R-NONX-OEM8, R-PODT-EU1H, R-8GYQ-ZT9T, R-Y5AQ-HXAR
- D47 → `project/design/D47.md` — nginx: session-gate the whole human surface (root + `/subject/` + `/static/`) — none — structural
- D48 → `project/design/D48.md` — The markdown → sanitized-HTML rendering seam (`internal/markdown`) — owns R-SS0J-U7PG, R-ST8G-7ZG5, R-SUGC-LR6U, R-SVO8-ZIXJ, R-SWW5-DAO8, R-SY41-R2EX, R-SZBY-4U5M, R-T0JU-ILWB, R-T1RQ-WDN0, R-T2ZN-A5DP
- D49 → `project/design/D49.md` — Token-based CSS for the rendered markdown element set — owns R-9EPS-LWWY, R-9FXO-ZONN
- D50 → `project/design/D50.md` — Eliminate the web-font FOUT (font-display + self-served fonts + preload) — owns R-KFVF-EMEO, R-KH3B-SE5D, R-KIB8-65W2, R-KJJ4-JXMR
- D51 → `project/design/D51.md` — Registry adoption: resolve wiki's own port through `registry` — owns R-JDBC-V0EV, R-JEJ9-8S5K, R-JFR5-MJW9
- D52 → `project/design/D52.md` — Web surface from `share/www` through the chassis (de-embed the read UI) — owns R-JGZ2-0BMY, R-JI6Y-E3DN, R-JJEU-RV4C
- D53 → `project/design/D53.md` — MCP surface over `appkit/mcp`: `internal/mcp` becomes the tool table — owns R-JKMR-5MV1
- D54 → `project/design/D54.md` — Composition-root normalization: one inline `Spec` in `cmd/wiki/main.go` — none — structural
- D55 → `project/design/D55.md` — Delete the `internal/db` chassis shim (keep the read handle) and true up the docs — none — structural
- D56 → `project/design/D56.md` — `ask` MCP citations are fully-qualified front-door URLs — owns R-Y7OR-PH1I, R-HOJB-ZR3T, R-YA4K-H0IW
- D57 → `project/design/D57.md` — Adopt the MCP self-discovery convention: instructions, lean tool descriptions, a `guide` tool — owns R-YDS9-MBQZ, R-YF06-03HO, R-YG82-DV8D, R-YHFY-RMZ2, R-YINV-5EPR
- D58 → `project/design/D58.md` — Inline first-occurrence subject links: the positional matcher + markdown-safe linkifier (`internal/wiki`) — owns R-82BY-EKDH, R-83JU-SC46, R-84RR-63UV, R-877J-XNC9, R-88FG-BF2Y, R-89NC-P6TN, R-8AV9-2YKC, R-8C35-GQB1
- D59 → `project/design/D59.md` — Every user-facing subject link is a fully-qualified front-door URL (retire the web-relative rule) — owns R-8I6N-DL0I, R-8JEJ-RCR7
- D60 → `project/design/D60.md` — the session-gated web locations opt into the apex `@login_bounce`: a logged-out human navigation goes to sign-in, not a bare 401 — none — structural
- D61 → `project/design/D61.md` — Structured MCP adoption: `StructuredResult`, typed error codes, and per-tool output schemas — owns R-EMCA-BCU2, R-ENK6-P4KR, R-EPZZ-GO25, R-ER7V-UFSU, R-ESFS-87JJ, R-ETNO-LZA8, R-EW3H-DIRM, R-EXBD-RAIB
- D62 → `project/design/D62.md` — Origin attribution on every inference call — owns R-16VU-W6UV, R-183R-9YLK
- D63 → `project/design/D63.md` — The retirement: the recorder stack and every in-repo tuning executable stay gone — owns R-1BRG-F9TN, R-KFX6-MNEW, R-A4L4-1LCK
- D64 → `project/design/D64.md` — Prompts calls ride the chassis's instrumented client, and the chain id replaces every wiki-minted correlation id — owns R-XLHZ-WPT5, R-XMPW-AHJU, R-XNXS-O9AJ, R-XP5P-2118
- D65 → `project/design/D65.md` — Inherit the chain across the async boundary; mint a root only for genuinely self-started work — owns R-XGME-DMUD, R-XJ27-56BR, R-KIH2-R4UC, R-XKA3-IY2G
- D66 → `project/design/D66.md` — nginx: forward the edge-minted correlation id on gated locations, strip it on the ungated one — none — structural
- D70 → `project/design/D70.md` — Remove the in-repo tuning machinery — none — structural
- D71 → `project/design/D71.md` — The committed tune-folder workspace: `autotune/<step>/`, one per pipeline prompt — owns R-A5T0-FD39, R-A88T-6WKN, R-A9GP-KOBC, R-7YU7-T9RG
- D72 → `project/design/D72.md` — The five scorers: deterministic where mechanical, sol-judged where prose — owns R-AAOL-YG21, R-ABWI-C7SQ, R-AD4E-PZJF, R-AECB-3RA4, R-AFK7-HJ0T, R-AGS3-VARI, R-8024-71I5, R-81A0-KT8U
- D73 → `project/design/D73.md` — Testing-language conformance: adopt the suite contract and move the env-gated smokes into the live layer — none minted; adopts R-O1AD-MRKW, R-O2IA-0JBL (root `project/design/D23.md`)
- D74 → `project/design/D74.md` — Scopes: named content partitions, the registry, and the hard wall — owns R-GSMY-FWWD, R-GTUU-TON2, R-GV2R-7GDR, R-GWAN-L84G, R-GXIJ-YZV5, R-GYQG-CRLU, R-GZYC-QJCJ, R-H2E5-I2TX, R-H3M1-VUKM, R-H4TY-9MBB, R-H61U-NE20, R-H79R-15SP
- D75 → `project/design/D75.md` — Scope on the MCP surface: the mandatory parameter + the four management verbs — owns R-H8HN-EXJE, R-H9PJ-SPA3, R-HAXG-6H0S, R-HC5C-K8RH, R-HDD8-Y0I6, R-HEL5-BS8V, R-HFT1-PJZK, R-HH0Y-3BQ9, R-HI8U-H3GY, R-HJGQ-UV7N
- D76 → `project/design/D76.md` — The scoped web surface: tier + scope in every URL, the landing cookie, the selector — owns R-HKON-8MYC, R-HLWJ-MEP1, R-HN4G-06FQ, R-HOCC-DY6F, R-HQS5-5HNT, R-HS01-J9EI, R-HT7X-X157, R-I0JC-7NLD
- D77 → `project/design/D77.md` — nginx: the two scope tiers (public ungated, private session-gated) — none — structural
- D78 → `project/design/D78.md` — Both tiers serve ask: the public tier gets the full experience — owns R-4Q8B-N9SX, R-4RG8-11JM, R-HY3J-G43Z
- D79 → `project/design/D79.md` — Truthful submit controls: the Ask label + script-driven pending feedback — owns R-VBAY-V6II, R-4TW0-SL10, R-VDQR-MPZW
- D80 → `project/design/D80.md` — The page shell: boxed top-down layout, header bar (brand mark + universal question form), footer — owns R-HG6J-SNRN, R-HHEG-6FIC, R-2MYX-Y4PN, R-2O6U-BWGC, R-2PEQ-PO71, R-2QMN-3FXQ
- D81 → `project/design/D81.md` — Suggested pages: the scope's seven newest subjects on every scope home — owns R-HJU8-XYZQ, R-HL25-BQQF, R-HMA1-PIH4, R-HOPU-H1YI
- D82 → `project/design/D82.md` — Scope instructions: an operator-authored preamble on every inference call — owns R-8FVJ-PUMZ, R-8H3G-3MDO, R-8JJ8-V5V2, R-8KR5-8XLR, R-8LZ1-MPCG, R-8N6Y-0H35, R-8OEU-E8TU, R-8PMQ-S0KJ, R-8QUN-5SB8, R-8S2J-JK1X
- D83 → `project/design/D83.md` — The ask response cache: in-memory, LRU, shared by both surfaces — owns R-02RF-HMOW, R-03ZB-VEFL, R-0578-966A, R-06F4-MXWZ, R-07N1-0PNO, R-08UX-EHED, R-0A2T-S952, R-0BAQ-60VR, R-0CIM-JSMG, R-0DQI-XKD5, R-0EYF-BC3U, R-0G6B-P3UJ, R-0IM4-GNBX, R-0JU0-UF2M, R-0L1X-86TB
- D84 → `project/design/D84.md` — Scope-labeled vector cache: the scope-carrying update seam + scope-wide eviction on scope delete — owns R-R1EX-LNNQ, R-R2MT-ZFEF, R-R3UQ-D754, R-R52M-QYVT, R-R6AJ-4QMI
- D85 → `project/design/D85.md` — Orphan-free page embeddings: guarded writes, subject-joined hydration, sweep reaping — owns R-R7IF-IID7, R-R8QB-WA3W, R-R9Y8-A1UL, R-RB64-NTLA
- D86 → `project/design/D86.md` — `ask` tolerates a stale retrieval hit: drop-and-continue — owns R-RDLX-FD2O, R-RETT-T4TD
- D87 → `project/design/D87.md` — The exact-name pin inside the scope wall: scoped resolution, subject-id hits — owns R-RG1Q-6WK2, R-RH9M-KOAR, R-RIHI-YG1G
- D88 → `project/design/D88.md` — A job whose row is gone integrates nothing: the dead-job discard — owns R-RJPF-C7S5, R-RKXB-PZIU
- D89 → `project/design/D89.md` — An honest ask failure on the web: a styled error page and a logged cause — owns R-RM58-3R9J, R-RND4-HJ08
- D90 → `project/design/D90.md` — Adopt the suite brand icon contract: the shipped icon set and its link markup — mints none; adopts R-RYDN-YNR5, R-RZLK-CFHU (root `project/design/D29.md`)
- D91 → `project/design/D91.md` — The prompts seam proven against the real binary (composed) — owns R-UF1D-AWZ1, R-UG99-OOPQ
- D92 → `project/design/D92.md` — Ask-serving routes clear the chassis write deadline — owns R-KJAJ-CTB1, R-KKIF-QL1Q
- D93 → `project/design/D93.md` — Serialized ingest admission: the durable per-scope lease — owns R-OXVD-UPJD, R-P2QZ-DSI5, R-OZ3A-8HA2, R-P1J3-00RG, R-P3YV-RK8U
- D94 → `project/design/D94.md` — The inbox drain contract: no item may stop the drain — owns R-P56S-5BZJ, R-P6EO-J3Q8, R-P7MK-WVGX, R-P8UH-AN7M
- D95 → `project/design/D95.md` — Job liveness and the honest job surface — owns R-PA2D-OEYB, R-PBAA-26P0, R-PCI6-FYFP, R-PEXZ-7HX3, R-PDQ2-TQ6E, R-PG5V-L9NS, R-PHDR-Z1EH
- D96 → `project/design/D96.md` — Statements: claims and corrections, the suppression record, and the effective claim set — owns R-74CQ-9083, R-76SJ-0JPH, R-780F-EBG6, R-798B-S36V, R-7AG8-5UXK
- D97 → `project/design/D97.md` — The match stage (`internal/match`): judging what a correction covers, in the pipeline and in merge — owns R-7FBT-OXWC, R-7GJQ-2PN1, R-7HRM-GHDQ, R-7IZI-U94F, R-7K7F-80V4, R-7LFB-LSLT, R-7MN7-ZKCI, R-7NV4-DC37, R-7QAX-4VKL, R-7RIT-INBA, R-7SQP-WF1Z, R-7TYM-A6SO, R-7V6I-NYJD
- D98 → `project/design/D98.md` — The corrections read surface: labeled kinds and visible suppression on `claims`, and the guide — owns R-7WEF-1QA2, R-7XMB-FI0R
- D99 → `project/design/D99.md` — Adopt the suite lint contract (`root project/design/D30.md`) at tier `strict` — none (structural; the contract carries no per-service ids)

## Verification ids → Decision

- R-00GU-KE86 → D12 (`project/design/D12.md`)
- R-01OQ-Y5YV → D10 (`project/design/D10.md`)
- R-02RF-HMOW → D83 (`project/design/D83.md`)
- R-02WN-BXPK → D10 (`project/design/D10.md`)
- R-03GW-PX5K → D10 (`project/design/D10.md`)
- R-03ZB-VEFL → D83 (`project/design/D83.md`)
- R-044J-PPG9 → D10 (`project/design/D10.md`)
- R-04HB-QM7T → D10 (`project/design/D10.md`)
- R-0578-966A → D83 (`project/design/D83.md`)
- R-05CG-3H6Y → D9 (`project/design/D09.md`)
- R-06F4-MXWZ → D83 (`project/design/D83.md`)
- R-07N1-0PNO → D83 (`project/design/D83.md`)
- R-08UX-EHED → D83 (`project/design/D83.md`)
- R-0A2T-S952 → D83 (`project/design/D83.md`)
- R-0BAQ-60VR → D83 (`project/design/D83.md`)
- R-0CIM-JSMG → D83 (`project/design/D83.md`)
- R-0DQI-XKD5 → D83 (`project/design/D83.md`)
- R-0EYF-BC3U → D83 (`project/design/D83.md`)
- R-0G6B-P3UJ → D83 (`project/design/D83.md`)
- R-0IM4-GNBX → D83 (`project/design/D83.md`)
- R-0JU0-UF2M → D83 (`project/design/D83.md`)
- R-0L1X-86TB → D83 (`project/design/D83.md`)
- R-0SCX-95OZ → D14 (`project/design/D14.md`)
- R-0TKT-MXFO → D14 (`project/design/D14.md`)
- R-0USQ-0P6D → D14 (`project/design/D14.md`)
- R-0W0M-EGX2 → D14 (`project/design/D14.md`)
- R-0X8I-S8NR → D14 (`project/design/D14.md`)
- R-0YGF-60EG → D14 (`project/design/D14.md`)
- R-0ZOB-JS55 → D14 (`project/design/D14.md`)
- R-10W7-XJVU → D14 (`project/design/D14.md`)
- R-1385-QVMS → D34 (`project/design/D34.md`)
- R-14G2-4NDH → D34 (`project/design/D34.md`)
- R-15NY-IF46 → D34 (`project/design/D34.md`)
- R-16VU-W6UV → D62 (`project/design/D62.md`)
- R-17C5-VP2I → D15 (`project/design/D15.md`)
- R-183R-9YLK → D62 (`project/design/D62.md`)
- R-18K2-9GT7 → D15 (`project/design/D15.md`)
- R-19RY-N8JW → D15 (`project/design/D15.md`)
- R-1BRG-F9TN → D63 (`project/design/D63.md`)
- R-1C7R-ES1A → D15 (`project/design/D15.md`)
- R-1DFN-SJRZ → D15 (`project/design/D15.md`)
- R-1O8B-FNX4 → D3 (`project/design/D03.md`)
- R-1PG7-TFNT → D25 (`project/design/D25.md`)
- R-1QO4-77EI → D10 (`project/design/D10.md`)
- R-1RW0-KZ57 → D27 (`project/design/D27.md`)
- R-1T3W-YQVW → D16 (`project/design/D16.md`)
- R-1VJP-QADA → D27 (`project/design/D27.md`)
- R-1WP9-CLM9 → D12 (`project/design/D12.md`)
- R-1XX5-QDCY → D12 (`project/design/D12.md`)
- R-1Y7B-TN7Y → D10 (`project/design/D10.md`)
- R-1Z52-453N → D12 (`project/design/D12.md`)
- R-203P-F1ET → D31 (`project/design/D31.md`)
- R-22JI-6KW7 → D31 (`project/design/D31.md`)
- R-23RE-KCMW → D31 (`project/design/D31.md`)
- R-24ZA-Y4DL → D31 (`project/design/D31.md`)
- R-2L34-0E1U → D44 (`project/design/D44.md`)
- R-2MB0-E5SJ → D44 (`project/design/D44.md`)
- R-2MYX-Y4PN → D80 (`project/design/D80.md`)
- R-2O6U-BWGC → D80 (`project/design/D80.md`)
- R-2PEQ-PO71 → D80 (`project/design/D80.md`)
- R-2QMN-3FXQ → D80 (`project/design/D80.md`)
- R-37NS-BRXR → D16 (`project/design/D16.md`)
- R-38VO-PJOG → D16 (`project/design/D16.md`)
- R-3A3L-3BF5 → D16 (`project/design/D16.md`)
- R-3CJD-UUWJ → D16 (`project/design/D16.md`)
- R-3EZ6-MEDX → D16 (`project/design/D16.md`)
- R-3G73-064M → D16 (`project/design/D16.md`)
- R-3WOB-6U4Q → D32 (`project/design/D32.md`)
- R-3XW7-KLVF → D32 (`project/design/D32.md`)
- R-3Z43-YDM4 → D32 (`project/design/D32.md`)
- R-40C0-C5CT → D32 (`project/design/D32.md`)
- R-4LKF-FB23 → D2 (`project/design/D02.md`, adopted from root `project/design/D08.md`)
- R-4Q8B-N9SX → D78 (`project/design/D78.md`)
- R-4RG8-11JM → D78 (`project/design/D78.md`)
- R-4TW0-SL10 → D79 (`project/design/D79.md`)
- R-5UPD-VVNA → D9 (`project/design/D09.md`)
- R-5VXA-9NDZ → D9 (`project/design/D09.md`)
- R-5X56-NF4O → D9 (`project/design/D09.md`)
- R-690G-MZTK → D9 (`project/design/D09.md`)
- R-6A8D-0RK9 → D9 (`project/design/D09.md`)
- R-6RVX-P1IG → D2 (`project/design/D02.md`)
- R-6XNX-FNXO → D35 (`project/design/D35.md`)
- R-6YVT-TFOD → D35 (`project/design/D35.md`)
- R-703Q-77F2 → D35 (`project/design/D35.md`)
- R-71BM-KZ5R → D35 (`project/design/D35.md`)
- R-72JI-YQWG → D35 (`project/design/D35.md`)
- R-73RF-CIN5 → D35 (`project/design/D35.md`)
- R-74CQ-9083 → D96 (`project/design/D96.md`)
- R-76SJ-0JPH → D96 (`project/design/D96.md`)
- R-780F-EBG6 → D96 (`project/design/D96.md`)
- R-798B-S36V → D96 (`project/design/D96.md`)
- R-79KD-1622 → D33 (`project/design/D33.md`)
- R-7AG8-5UXK → D96 (`project/design/D96.md`)
- R-7AS9-EXSR → D33 (`project/design/D33.md`)
- R-7BO4-JMO9 → D6 (`project/design/D06.md`)
- R-7C05-SPJG → D33 (`project/design/D33.md`)
- R-7CW0-XEEY → D6 (`project/design/D06.md`)
- R-7D82-6HA5 → D33 (`project/design/D33.md`)
- R-7E3X-B65N → D6 (`project/design/D06.md`)
- R-7FBT-OXWC → D97 (`project/design/D97.md`)
- R-7GJQ-2PN1 → D97 (`project/design/D97.md`)
- R-7HRM-GHDQ → D97 (`project/design/D97.md`)
- R-7IZI-U94F → D97 (`project/design/D97.md`)
- R-7K7F-80V4 → D97 (`project/design/D97.md`)
- R-7LFB-LSLT → D97 (`project/design/D97.md`)
- R-7MN7-ZKCI → D97 (`project/design/D97.md`)
- R-7NV4-DC37 → D97 (`project/design/D97.md`)
- R-7QAX-4VKL → D97 (`project/design/D97.md`)
- R-7RIT-INBA → D97 (`project/design/D97.md`)
- R-7SNG-0G9A → D3 (`project/design/D03.md`)
- R-7SQP-WF1Z → D97 (`project/design/D97.md`)
- R-7TVC-E7ZZ → D3 (`project/design/D03.md`)
- R-7TYM-A6SO → D97 (`project/design/D97.md`)
- R-7V38-RZQO → D3 (`project/design/D03.md`)
- R-7V6I-NYJD → D97 (`project/design/D97.md`)
- R-7WB5-5RHD → D3 (`project/design/D03.md`)
- R-7WEF-1QA2 → D98 (`project/design/D98.md`)
- R-7XMB-FI0R → D98 (`project/design/D98.md`)
- R-7YU7-T9RG → D71 (`project/design/D71.md`)
- R-8024-71I5 → D72 (`project/design/D72.md`)
- R-81A0-KT8U → D72 (`project/design/D72.md`)
- R-82BY-EKDH → D58 (`project/design/D58.md`)
- R-83JU-SC46 → D58 (`project/design/D58.md`)
- R-84RR-63UV → D58 (`project/design/D58.md`)
- R-877J-XNC9 → D58 (`project/design/D58.md`)
- R-88FG-BF2Y → D58 (`project/design/D58.md`)
- R-89NC-P6TN → D58 (`project/design/D58.md`)
- R-8AV9-2YKC → D58 (`project/design/D58.md`)
- R-8C35-GQB1 → D58 (`project/design/D58.md`)
- R-8DB1-UI1Q → D10 (`project/design/D10.md`)
- R-8DF1-W89F → D2 (`project/design/D02.md`, adopted from root `project/design/D11.md`)
- R-8EIY-89SF → D10 (`project/design/D10.md`)
- R-8FQU-M1J4 → D44 (`project/design/D44.md`)
- R-8FVJ-PUMZ → D82 (`project/design/D82.md`)
- R-8GYQ-ZT9T → D45 (`project/design/D45.md`)
- R-8H3G-3MDO → D82 (`project/design/D82.md`)
- R-8I6N-DL0I → D59 (`project/design/D59.md`)
- R-8IAN-FB87 → D2 (`project/design/D02.md`, adopted from root `project/design/D11.md`)
- R-8JEJ-RCR7 → D59 (`project/design/D59.md`)
- R-8JJ8-V5V2 → D82 (`project/design/D82.md`)
- R-8KR5-8XLR → D82 (`project/design/D82.md`)
- R-8LZ1-MPCG → D82 (`project/design/D82.md`)
- R-8N6Y-0H35 → D82 (`project/design/D82.md`)
- R-8OEU-E8TU → D82 (`project/design/D82.md`)
- R-8PMQ-S0KJ → D82 (`project/design/D82.md`)
- R-8QUN-5SB8 → D82 (`project/design/D82.md`)
- R-8S2J-JK1X → D82 (`project/design/D82.md`)
- R-8TAF-XBSM → D6 (`project/design/D06.md`)
- R-8UIC-B3JB → D6 (`project/design/D06.md`)
- R-9EPS-LWWY → D49 (`project/design/D49.md`)
- R-9FXO-ZONN → D49 (`project/design/D49.md`)
- R-9OCK-FJK1 → D30 (`project/design/D30.md`)
- R-9PKG-TBAQ → D30 (`project/design/D30.md`)
- R-9QSD-731F → D30 (`project/design/D30.md`)
- R-9S09-KUS4 → D30 (`project/design/D30.md`)
- R-9S84-C6J2 → D5 (`project/design/D05.md`)
- R-9UTW-ZFF0 → D6 (`project/design/D06.md`)
- R-9W1T-D75P → D6 (`project/design/D06.md`)
- R-9X9P-QYWE → D7 (`project/design/D07.md`)
- R-9YHM-4QN3 → D7 (`project/design/D07.md`)
- R-9ZPI-IIDS → D9 (`project/design/D09.md`)
- R-A0XE-WA4H → D36 (`project/design/D36.md`)
- R-A25B-A1V6 → D19 (`project/design/D19.md`)
- R-A3D7-NTLV → D1 (`project/design/D01.md`)
- R-A4L4-1LCK → D63 (`project/design/D63.md`)
- R-A5T0-FD39 → D71 (`project/design/D71.md`)
- R-A88T-6WKN → D71 (`project/design/D71.md`)
- R-A9GP-KOBC → D71 (`project/design/D71.md`)
- R-AAOL-YG21 → D72 (`project/design/D72.md`)
- R-ABWI-C7SQ → D72 (`project/design/D72.md`)
- R-AD4E-PZJF → D72 (`project/design/D72.md`)
- R-AECB-3RA4 → D72 (`project/design/D72.md`)
- R-AF1X-PG7K → D29 (`project/design/D29.md`)
- R-AFK7-HJ0T → D72 (`project/design/D72.md`)
- R-AG2Y-PH8L → D29 (`project/design/D29.md`)
- R-AGS3-VARI → D72 (`project/design/D72.md`)
- R-AH3Z-PJ9M → D29 (`project/design/D29.md`)
- R-AL5R-PL1P → D29 (`project/design/D29.md`)
- R-ARN9-5YPS → D44 (`project/design/D44.md`)
- R-AU31-XI76 → D44 (`project/design/D44.md`)
- R-AVAY-B9XV → D44 (`project/design/D44.md`)
- R-AWIU-P1OK → D44 (`project/design/D44.md`)
- R-AXQR-2TF9 → D44 (`project/design/D44.md`)
- R-BAFW-D24P → D9 (`project/design/D09.md`)
- R-BBNS-QTVE → D9 (`project/design/D09.md`)
- R-BCVP-4LM3 → D9 (`project/design/D09.md`)
- R-BGPF-NVTU → D25 (`project/design/D25.md`)
- R-BHXC-1NKJ → D25 (`project/design/D25.md`)
- R-BJ58-FFB8 → D25 (`project/design/D25.md`)
- R-BKD4-T71X → D25 (`project/design/D25.md`)
- R-BLL1-6YSM → D25 (`project/design/D25.md`)
- R-BMSX-KQJB → D25 (`project/design/D25.md`)
- R-BO0T-YIA0 → D25 (`project/design/D25.md`)
- R-DRX6-PWSW → D11 (`project/design/D11.md`)
- R-DT53-3OJL → D11 (`project/design/D11.md`)
- R-DUCZ-HGAA → D11 (`project/design/D11.md`)
- R-DWDM-RVA7 → D27 (`project/design/D27.md`)
- R-DYTF-JERL → D27 (`project/design/D27.md`)
- R-E01B-X6IA → D27 (`project/design/D27.md`)
- R-E198-AY8Z → D27 (`project/design/D27.md`)
- R-E2H4-OPZO → D27 (`project/design/D27.md`)
- R-E3P1-2HQD → D27 (`project/design/D27.md`)
- R-E4WX-G9H2 → D16 (`project/design/D16.md`)
- R-EMCA-BCU2 → D61 (`project/design/D61.md`)
- R-ENK6-P4KR → D61 (`project/design/D61.md`)
- R-EPZZ-GO25 → D61 (`project/design/D61.md`)
- R-ER7V-UFSU → D61 (`project/design/D61.md`)
- R-ESFS-87JJ → D61 (`project/design/D61.md`)
- R-ETNO-LZA8 → D61 (`project/design/D61.md`)
- R-EV2H-6RKN → D38 (`project/design/D38.md`)
- R-EW3H-DIRM → D61 (`project/design/D61.md`)
- R-EXBD-RAIB → D61 (`project/design/D61.md`)
- R-FM7A-D0SZ → D38 (`project/design/D38.md`)
- R-FQLB-QWS6 → D7 (`project/design/D07.md`)
- R-FT14-IG9K → D7 (`project/design/D07.md`)
- R-FU90-W809 → D7 (`project/design/D07.md`)
- R-FUCC-IT4M → D17 (`project/design/D17.md`)
- R-FVGX-9ZQY → D7 (`project/design/D07.md`)
- R-FVK8-WKVB → D17 (`project/design/D17.md`)
- R-FWS5-ACM0 → D17 (`project/design/D17.md`)
- R-FY01-O4CP → D17 (`project/design/D17.md`)
- R-GGIG-AN7W → D19 (`project/design/D19.md`)
- R-GHQC-OEYL → D19 (`project/design/D19.md`)
- R-GK65-FYFZ → D19 (`project/design/D19.md`)
- R-GLE1-TQ6O → D19 (`project/design/D19.md`)
- R-GSMY-FWWD → D74 (`project/design/D74.md`)
- R-GTUU-TON2 → D74 (`project/design/D74.md`)
- R-GV2R-7GDR → D74 (`project/design/D74.md`)
- R-GWAN-L84G → D74 (`project/design/D74.md`)
- R-GXIJ-YZV5 → D74 (`project/design/D74.md`)
- R-GYQG-CRLU → D74 (`project/design/D74.md`)
- R-GZYC-QJCJ → D74 (`project/design/D74.md`)
- R-H2E5-I2TX → D74 (`project/design/D74.md`)
- R-H3M1-VUKM → D74 (`project/design/D74.md`)
- R-H4TY-9MBB → D74 (`project/design/D74.md`)
- R-H61U-NE20 → D74 (`project/design/D74.md`)
- R-H79R-15SP → D74 (`project/design/D74.md`)
- R-H8HN-EXJE → D75 (`project/design/D75.md`)
- R-H9PJ-SPA3 → D75 (`project/design/D75.md`)
- R-HAXG-6H0S → D75 (`project/design/D75.md`)
- R-HC5C-K8RH → D75 (`project/design/D75.md`)
- R-HDD8-Y0I6 → D75 (`project/design/D75.md`)
- R-HEL5-BS8V → D75 (`project/design/D75.md`)
- R-HFT1-PJZK → D75 (`project/design/D75.md`)
- R-HG6J-SNRN → D80 (`project/design/D80.md`)
- R-HH0Y-3BQ9 → D75 (`project/design/D75.md`)
- R-HHEG-6FIC → D80 (`project/design/D80.md`)
- R-HI8U-H3GY → D75 (`project/design/D75.md`)
- R-HIMC-K791 → D41 (`project/design/D41.md`)
- R-HJGQ-UV7N → D75 (`project/design/D75.md`)
- R-HJU8-XYZQ → D81 (`project/design/D81.md`)
- R-HKON-8MYC → D76 (`project/design/D76.md`)
- R-HL25-BQQF → D81 (`project/design/D81.md`)
- R-HLWJ-MEP1 → D76 (`project/design/D76.md`)
- R-HMA1-PIH4 → D81 (`project/design/D81.md`)
- R-HN4G-06FQ → D76 (`project/design/D76.md`)
- R-HOCC-DY6F → D76 (`project/design/D76.md`)
- R-HOJB-ZR3T → D56 (`project/design/D56.md`)
- R-HOPU-H1YI → D81 (`project/design/D81.md`)
- R-HQS5-5HNT → D76 (`project/design/D76.md`)
- R-HS01-J9EI → D76 (`project/design/D76.md`)
- R-HT7X-X157 → D76 (`project/design/D76.md`)
- R-HUDR-AWS9 → D26 (`project/design/D26.md`)
- R-HY3J-G43Z → D78 (`project/design/D78.md`)
- R-I0JC-7NLD → D76 (`project/design/D76.md`)
- R-JDBC-V0EV → D51 (`project/design/D51.md`)
- R-JEJ9-8S5K → D51 (`project/design/D51.md`)
- R-JFR5-MJW9 → D51 (`project/design/D51.md`)
- R-JGZ2-0BMY → D52 (`project/design/D52.md`)
- R-JI6Y-E3DN → D52 (`project/design/D52.md`)
- R-JJEU-RV4C → D52 (`project/design/D52.md`)
- R-JKMR-5MV1 → D53 (`project/design/D53.md`)
- R-JW4G-367U → D5 (`project/design/D05.md`)
- R-JXCC-GXYJ → D5 (`project/design/D05.md`)
- R-JYK8-UPP8 → D5 (`project/design/D05.md`)
- R-JZS5-8HFX → D5 (`project/design/D05.md`)
- R-K101-M96M → D5 (`project/design/D05.md`)
- R-K27Y-00XB → D5 (`project/design/D05.md`)
- R-K3FU-DSO0 → D5 (`project/design/D05.md`)
- R-K4NQ-RKEP → D5 (`project/design/D05.md`)
- R-K73J-J3W3 → D4 (`project/design/D04.md`)
- R-K8BF-WVMS → D4 (`project/design/D04.md`)
- R-K9JC-ANDH → D4 (`project/design/D04.md`)
- R-KAR8-OF46 → D4 (`project/design/D04.md`)
- R-KBZ5-26UV → D4 (`project/design/D04.md`)
- R-KDHD-V3XI → D1 (`project/design/D01.md`)
- R-KEEX-TQC9 → D4 (`project/design/D04.md`)
- R-KFMU-7I2Y → D4 (`project/design/D04.md`)
- R-KFVF-EMEO → D50 (`project/design/D50.md`)
- R-KFX6-MNEW → D63 (`project/design/D63.md`)
- R-KGUQ-L9TN → D14 (`project/design/D14.md`)
- R-KH3B-SE5D → D50 (`project/design/D50.md`)
- R-KI2M-Z1KC → D14 (`project/design/D14.md`)
- R-KIB8-65W2 → D50 (`project/design/D50.md`)
- R-KIH2-R4UC → D65 (`project/design/D65.md`)
- R-KJAJ-CTB1 → D92 (`project/design/D92.md`)
- R-KJJ4-JXMR → D50 (`project/design/D50.md`)
- R-KKIF-QL1Q → D92 (`project/design/D92.md`)
- R-LAND-CARB → D39 (`project/design/D39.md`)
- R-LAND-NMVR → D39 (`project/design/D39.md`)
- R-LAND-PG01 → D39 (`project/design/D39.md`)
- R-LAND-ROOT → D39 (`project/design/D39.md`)
- R-LAND-UNGT → D39 (`project/design/D39.md`)
- R-LP5Q-9XTD → D38 (`project/design/D38.md`)
- R-M8RN-87WV → D4 (`project/design/D04.md`)
- R-MB7F-ZRE9 → D4 (`project/design/D04.md`)
- R-MCFC-DJ4Y → D4 (`project/design/D04.md`)
- R-MG31-IUD1 → D4 (`project/design/D04.md`)
- R-MRG8-K2WP → D38 (`project/design/D38.md`)
- R-MSKH-GPX5 → D18 (`project/design/D18.md`)
- R-MTSD-UHNU → D18 (`project/design/D18.md`)
- R-MUQ4-K1JS → D10 (`project/design/D10.md`)
- R-MV0A-89EJ → D18 (`project/design/D18.md`)
- R-MVY0-XTAH → D10 (`project/design/D10.md`)
- R-MW86-M158 → D18 (`project/design/D18.md`)
- R-MX5X-BL16 → D10 (`project/design/D10.md`)
- R-MYDT-PCRV → D10 (`project/design/D10.md`)
- R-MZLQ-34IK → D10 (`project/design/D10.md`)
- R-N4KO-2WTZ → D10 (`project/design/D10.md`)
- R-N729-RY1I → D10 (`project/design/D10.md`)
- R-NC9Q-4F8K → D31 (`project/design/D31.md`)
- R-NDHM-I6Z9 → D33 (`project/design/D33.md`)
- R-NEFH-U8IO → D26 (`project/design/D26.md`)
- R-NEPI-VYPY → D33 (`project/design/D33.md`)
- R-NFNE-809D → D26 (`project/design/D26.md`)
- R-NFXF-9QGN → D9 (`project/design/D09.md`)
- R-NGVA-LS02 → D26 (`project/design/D26.md`)
- R-NH5B-NI7C → D9 (`project/design/D09.md`)
- R-NI36-ZJQR → D26 (`project/design/D26.md`)
- R-NID8-19Y1 → D9 (`project/design/D09.md`)
- R-NJB3-DBHG → D26 (`project/design/D26.md`)
- R-NKIZ-R385 → D26 (`project/design/D26.md`)
- R-NLQW-4UYU → D26 (`project/design/D26.md`)
- R-NMYS-IMPJ → D26 (`project/design/D26.md`)
- R-NONX-OEM8 → D45 (`project/design/D45.md`)
- R-NPEL-A66X → D26 (`project/design/D26.md`)
- R-NPVU-26CX → D44 (`project/design/D44.md`)
- R-O1AD-MRKW → D73 (`project/design/D73.md`) (adopted from root `project/design/D23.md`)
- R-O2IA-0JBL → D73 (`project/design/D73.md`) (adopted from root `project/design/D23.md`)
- R-OQJZ-K337 → D4 (`project/design/D04.md`)
- R-ORRV-XUTW → D4 (`project/design/D04.md`)
- R-OSZS-BMKL → D4 (`project/design/D04.md`)
- R-OU7O-PEBA → D4 (`project/design/D04.md`)
- R-OVFL-361Z → D4 (`project/design/D04.md`)
- R-OXVD-UPJD → D93 (`project/design/D93.md`)
- R-OZ3A-8HA2 → D93 (`project/design/D93.md`)
- R-P1J3-00RG → D93 (`project/design/D93.md`)
- R-P2QZ-DSI5 → D93 (`project/design/D93.md`)
- R-P3YV-RK8U → D93 (`project/design/D93.md`)
- R-P56S-5BZJ → D94 (`project/design/D94.md`)
- R-P6EO-J3Q8 → D94 (`project/design/D94.md`)
- R-P7MK-WVGX → D94 (`project/design/D94.md`)
- R-P8UH-AN7M → D94 (`project/design/D94.md`)
- R-PA2D-OEYB → D95 (`project/design/D95.md`)
- R-PBAA-26P0 → D95 (`project/design/D95.md`)
- R-PCI6-FYFP → D95 (`project/design/D95.md`)
- R-PDQ2-TQ6E → D95 (`project/design/D95.md`)
- R-PEXZ-7HX3 → D95 (`project/design/D95.md`)
- R-PG5V-L9NS → D95 (`project/design/D95.md`)
- R-PH2F-47LB → D45 (`project/design/D45.md`)
- R-PHDR-Z1EH → D95 (`project/design/D95.md`)
- R-PIAB-HZC0 → D45 (`project/design/D45.md`)
- R-PJI7-VR2P → D45 (`project/design/D45.md`)
- R-PLY0-NAK3 → D45 (`project/design/D45.md`)
- R-PODT-EU1H → D45 (`project/design/D45.md`)
- R-Q8RI-7POG → D33 (`project/design/D33.md`)
- R-QB7A-Z95U → D36 (`project/design/D36.md`)
- R-QCF7-D0WJ → D36 (`project/design/D36.md`)
- R-QDN3-QSN8 → D36 (`project/design/D36.md`)
- R-R1EX-LNNQ → D84 (`project/design/D84.md`)
- R-R2MT-ZFEF → D84 (`project/design/D84.md`)
- R-R3UQ-D754 → D84 (`project/design/D84.md`)
- R-R52M-QYVT → D84 (`project/design/D84.md`)
- R-R6AJ-4QMI → D84 (`project/design/D84.md`)
- R-R7IF-IID7 → D85 (`project/design/D85.md`)
- R-R8QB-WA3W → D85 (`project/design/D85.md`)
- R-R9Y8-A1UL → D85 (`project/design/D85.md`)
- R-RB64-NTLA → D85 (`project/design/D85.md`)
- R-RDLX-FD2O → D86 (`project/design/D86.md`)
- R-RETT-T4TD → D86 (`project/design/D86.md`)
- R-RG1Q-6WK2 → D87 (`project/design/D87.md`)
- R-RH9M-KOAR → D87 (`project/design/D87.md`)
- R-RIHI-YG1G → D87 (`project/design/D87.md`)
- R-RJPF-C7S5 → D88 (`project/design/D88.md`)
- R-RKXB-PZIU → D88 (`project/design/D88.md`)
- R-RM58-3R9J → D89 (`project/design/D89.md`)
- R-RND4-HJ08 → D89 (`project/design/D89.md`)
- R-RU0J-77HX → D3 (`project/design/D03.md`)
- R-RV8F-KZ8M → D3 (`project/design/D03.md`)
- R-RXO8-CIQ0 → D3 (`project/design/D03.md`)
- R-RYDN-YNR5 → D90 — `project/design/D90.md` (adopted from root `project/design/D29.md`)
- R-RYW4-QAGP → D3 (`project/design/D03.md`)
- R-RZLK-CFHU → D90 — `project/design/D90.md` (adopted from root `project/design/D29.md`)
- R-S041-427E → D3 (`project/design/D03.md`)
- R-S1BX-HTY3 → D3 (`project/design/D03.md`)
- R-S2JT-VLOS → D3 (`project/design/D03.md`)
- R-SS0J-U7PG → D48 (`project/design/D48.md`)
- R-ST8G-7ZG5 → D48 (`project/design/D48.md`)
- R-SUGC-LR6U → D48 (`project/design/D48.md`)
- R-SVO8-ZIXJ → D48 (`project/design/D48.md`)
- R-SWW5-DAO8 → D48 (`project/design/D48.md`)
- R-SY41-R2EX → D48 (`project/design/D48.md`)
- R-SZBY-4U5M → D48 (`project/design/D48.md`)
- R-T0JU-ILWB → D48 (`project/design/D48.md`)
- R-T1RQ-WDN0 → D48 (`project/design/D48.md`)
- R-T2ZN-A5DP → D48 (`project/design/D48.md`)
- R-UCLK-JDHN → D5 (`project/design/D05.md`)
- R-UDTG-X58C → D5 (`project/design/D05.md`)
- R-UF1D-AWZ1 → D91 (`project/design/D91.md`)
- R-UG99-OOPQ → D91 (`project/design/D91.md`)
- R-VA32-HERT → D7 (`project/design/D07.md`)
- R-VBAY-V6II → D79 (`project/design/D79.md`)
- R-VDQR-MPZW → D79 (`project/design/D79.md`)
- R-VYU0-BPAX → D6 (`project/design/D06.md`)
- R-W19T-38SB → D6 (`project/design/D06.md`)
- R-WC29-XALJ → D42 (`project/design/D42.md`)
- R-WDA6-B2C8 → D42 (`project/design/D42.md`)
- R-WFPZ-2LTM → D42 (`project/design/D42.md`)
- R-WGXV-GDKB → D42 (`project/design/D42.md`)
- R-WS3C-J4QB → D38 (`project/design/D38.md`)
- R-XGME-DMUD → D65 (`project/design/D65.md`)
- R-XJ27-56BR → D65 (`project/design/D65.md`)
- R-XJBY-H8JZ → D6 (`project/design/D06.md`)
- R-XKA3-IY2G → D65 (`project/design/D65.md`)
- R-XKJU-V0AO → D6 (`project/design/D06.md`)
- R-XLHZ-WPT5 → D64 (`project/design/D64.md`)
- R-XMPW-AHJU → D64 (`project/design/D64.md`)
- R-XNXS-O9AJ → D64 (`project/design/D64.md`)
- R-XP5P-2118 → D64 (`project/design/D64.md`)
- R-XYAZ-V0XE → D15 (`project/design/D15.md`)
- R-XZIW-8SO3 → D15 (`project/design/D15.md`)
- R-Y1YP-0C5H → D15 (`project/design/D15.md`)
- R-Y36L-E3W6 → D16 (`project/design/D16.md`)
- R-Y4EH-RVMV → D16 (`project/design/D16.md`)
- R-Y5AQ-HXAR → D45 (`project/design/D45.md`)
- R-Y7OR-PH1I → D56 (`project/design/D56.md`)
- R-YA4K-H0IW → D56 (`project/design/D56.md`)
- R-YDS9-MBQZ → D57 (`project/design/D57.md`)
- R-YF06-03HO → D57 (`project/design/D57.md`)
- R-YG82-DV8D → D57 (`project/design/D57.md`)
- R-YHFY-RMZ2 → D57 (`project/design/D57.md`)
- R-YINV-5EPR → D57 (`project/design/D57.md`)
- R-Z5JL-2IBS → D28 (`project/design/D28.md`)
- R-Z6RH-GA2H → D28 (`project/design/D28.md`)
- R-Z7ZD-U1T6 → D28 (`project/design/D28.md`)
- R-Z932-H2RA → D34 (`project/design/D34.md`)
- R-ZUDC-NJIP → D12 (`project/design/D12.md`)
- R-ZVL9-1B9E → D12 (`project/design/D12.md`)
- R-ZWT5-F303 → D12 (`project/design/D12.md`)
- R-ZY11-SUQS → D12 (`project/design/D12.md`)
- R-ZZ8Y-6MHH → D12 (`project/design/D12.md`)

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped
to the id(s) whose tests most directly prove it; the quality of each proof is
the audit's question, the mapping's completeness is this manifest's. One id may
serve several criteria, and one criterion may draw on several ids. Regenerated
with the rest of the index.

1. A scope isolates its content; the same subject name in another scope keeps its own page →
   R-GV2R-7GDR, R-GXIJ-YZV5, R-GYQG-CRLU
2. A multi-minute job still finishes, and a job in flight across a wiki/inference restart still reaches done or explicit failure without resubmitting →
   R-PA2D-OEYB, R-P1J3-00RG, R-NLQW-4UYU
3. An uncreated scope name fails clearly and creates nothing; omitting the scope is an error, never a guess →
   R-H4TY-9MBB, R-H9PJ-SPA3, R-H8HN-EXJE
4. An illegal or already-taken scope name is refused with a reason; `default` exists uncreated →
   R-GTUU-TON2, R-HDD8-Y0I6, R-GSMY-FWWD
5. Deleting a scope removes all its content and leaves others untouched; deleting `default` is refused →
   R-H3M1-VUKM, R-HEL5-BS8V
6. Immediately after deletion the scope finds nothing, and recreating the name yields an empty scope free of the deleted generation's content →
   R-H3M1-VUKM, R-R6AJ-4QMI, R-RJPF-C7S5
7. A failed answer returns a styled page in plain words with the question retained, never a bare or internal error →
   R-RM58-3R9J, R-RND4-HJ08
8. Listing scopes shows each name and whether it is private or public →
   R-HAXG-6H0S
9. Flipping a scope public lets a logged-out browser open its pages and ask it questions with the same cited answers; flipping back closes access →
   R-HFT1-PJZK, R-HLWJ-MEP1, R-4Q8B-N9SX
10. A private scope's pages are indistinguishable from nonexistent to a logged-out browser, and no page reveals private scope names →
    R-HLWJ-MEP1, R-HQS5-5HNT
11. The bare address lands on the last-picked scope (or `default`); a link to another scope shows that content without changing where I land next →
    R-HN4G-06FQ, R-HOCC-DY6F
12. A copied page link opens the same page for the recipient regardless of their own scope choice →
    R-HS01-J9EI, R-8I6N-DL0I
13. Instructions set, read back verbatim, and clear; a nonexistent scope fails, and over-cap text is refused naming the limit with the old value intact →
    R-8H3G-3MDO, R-8JJ8-V5V2, R-8QUN-5SB8
14. With instructions set, a subsequent ingest and ask in that scope are interpreted under them; another scope is unaffected →
    R-8LZ1-MPCG, R-8OEU-E8TU
15. A story's "130 years ago" gets no real founding year; asking answers in the story's own terms, not from the ingest date →
    R-8TAF-XBSM, R-XJBY-H8JZ, R-8UIC-B3JB
16. After changing instructions, re-running an earlier ingest re-digests its text under the new instructions, replacing that ingest's claims →
    R-0X8I-S8NR, R-0YGF-60EG
17. Ingesting text returns a handle without waiting for processing →
    R-M8RN-87WV
18. During an ingest's multi-minute phase, status/jobs/subjects/claims/pages/ask all return promptly and another submission is accepted immediately →
    R-FUCC-IT4M, R-FVK8-WKVB, R-P2QZ-DSI5
19. A hung or long job blocks no read and no submission, stays abortable, and the service stays responsive →
    R-FUCC-IT4M, R-0TKT-MXFO
20. A completed ingest's subjects and pages become visible all at once, never partially applied →
    R-K9JC-ANDH, R-OVFL-361Z
21. Watch a job go pending/working → terminal by handle; list newest-first filtered by states and/or a time window, paged, several states in one query →
    R-XYAZ-V0XE, R-37NS-BRXR, R-XZIW-8SO3
22. Get the count of jobs matching a state/time filter without retrieving them, correct across many jobs →
    R-Y36L-E3W6, R-Y1YP-0C5H
23. An invalid state yields a clear error naming the valid five, which are discoverable from the jobs surface →
    R-Y4EH-RVMV
24. Aborting a pending or working job leaves nothing partial and shows as `aborted`, distinct from `failed` →
    R-0SCX-95OZ, R-0USQ-0P6D, R-38VO-PJOG
25. Re-running a terminal job reprocesses from original text and replaces its claims; re-running a pending/working job is refused →
    R-0X8I-S8NR, R-0YGF-60EG, R-3A3L-3BF5
26. Text too large to digest in one pass fails with a reason recorded on the job, leaving no partial subjects/claims/pages →
    R-MTSD-UHNU, R-KBZ5-26UV
27. Every inference call wiki made is visible per-stage on the central service, an ingest's calls share the handle wiki reports on status, and a call is attributed to me →
    R-XLHZ-WPT5, R-N729-RY1I, R-16VU-W6UV
28. Page through jobs, subjects, and a subject's claims by cursor, with any filter applied before paging →
    R-17C5-VP2I, R-18K2-9GT7, R-3CJD-UUWJ
29. After an ingest, list the subjects it produced and view any subject's claims and page by readable `type/slug` →
    R-01OQ-Y5YV, R-02WN-BXPK, R-PDQ2-TQ6E
30. Every subject is named `type/slug`, an `ask` citation is a fully-qualified front-door link, and no internal id is shown →
    R-05CG-3H6Y, R-Y7OR-PH1I, R-HOJB-ZR3T
31. A compiled page carries no bracketed id or citation marker, and its opening sentence describes the subject in plain words, not as an internal term →
    R-VA32-HERT, R-FQLB-QWS6
32. On every web page the question button reads **Ask**, and after a click the page visibly shows it is working until the answer appears →
    R-VBAY-V6II, R-4TW0-SL10, R-VDQR-MPZW
33. A page shows the subjects it points to and those pointing to it, and a link exists exactly when the subject's (or a merged) name appears, never a variant →
    R-ZUDC-NJIP, R-ZVL9-1B9E, R-ZY11-SUQS
34. Ingesting more text about the same subject updates its page, not a duplicate →
    R-MCFC-DJ4Y, R-OU7O-PEBA
35. A document ruling an earlier fact false stops the page stating it, with no trace of falsehood or correction in the prose, everything else intact; a carried truth lands →
    R-7NV4-DC37, R-7QAX-4VKL, R-7BO4-JMO9
36. Two merely-disagreeing documents (plain assertions, no ruling) keep both accounts on the page — nothing is retired →
    R-7CW0-XEEY, R-7LFB-LSLT
37. After a correction stands, a later document reasserting the retired fact does not bring it back →
    R-7RIT-INBA
38. A wrong correction is reversed by a newer explicit statement; what it held down returns to the page →
    R-7SQP-WF1Z, R-780F-EBG6
39. Listing a subject's claims shows every statement labeled claim/correction, what is retired, and which correction retired each — nothing deleted →
    R-7WEF-1QA2
40. Corrections retiring every claim leave the subject pageless (record inspectable); new material brings a page back →
    R-7QAX-4VKL, R-7AG8-5UXK
41. Merging survivor and folded yields one page holding both subjects' claims; the folded subject and page are gone and its claims persist relabelled →
    R-NGVA-LS02, R-HUDR-AWS9
42. After a merge, asking the folded name, ingesting under it, following a link to it, and looking up its old `type/slug` all return the survivor →
    R-HUDR-AWS9, R-AF1X-PG7K, R-BLL1-6YSM
43. A merge returns a handle promptly and completes in the background; nothing merges unless requested →
    R-DYTF-JERL, R-NFNE-809D
44. I cannot un-merge in this release — the fold is one-directional, the folded subject and page removed →
    R-NGVA-LS02
45. List merges performed, newest-first and paged: what folded into what, and when →
    R-E2H4-OPZO, R-HI8U-H3GY
46. Original raw text and extracted claims remain retrievable after the page is built →
    R-04HB-QM7T, R-0X8I-S8NR
47. Asking about a subject without naming it exactly, in different words, returns a cited answer drawn only from ingested content →
    R-BAFW-D24P, R-690G-MZTK, R-GYQG-CRLU
48. A question spanning several subjects answers from those it has and does not fail because one was never ingested →
    R-Q8RI-7POG, R-BBNS-QTVE
49. Asking something the wiki holds nothing on returns an explicit "nothing here," not a fabrication →
    R-BBNS-QTVE, R-RETT-T4TD
50. Nothing done through `ask` changes any subject, claim, or page →
    R-5X56-NF4O
51. No page exceeds 12,000 characters →
    R-FT14-IG9K, R-7V38-RZQO
52. Health reports the service up; reflection reports no published or subscribed events →
    R-MX5X-BL16, R-MVY0-XTAH
53. A logged-in user opens the mount root and gets a styled page with a search box and a service/version footer, without disturbing the MCP surface →
    R-LAND-PG01, R-LAND-NMVR
54. A scope home shows its seven most recently added pages, newest first, each opening its page; older pages still turn up by search and links →
    R-HJU8-XYZQ, R-HL25-BQQF
55. Every page carries a Home control back to the wiki landing and the suite brand mark to the dashboard landing →
    R-HIMC-K791, R-2MYX-Y4PN
56. Typing a question turns the page into a cited answer from compiled pages — the same answer `ask` gives — and a plain "nothing here" when empty →
    R-ARN9-5YPS, R-AWIU-P1OK, R-AXQR-2TF9
57. The answer page footer lists each subject whose name or alias appears, each opening that subject's page →
    R-AU31-XI76, R-2MB0-E5SJ
58. In an `ask` answer, on MCP and web, the first naming of a subject is an inline link and later namings are plain text →
    R-8DB1-UI1Q, R-82BY-EKDH, R-8FQU-M1J4
59. On a subject page the first prose naming of another subject is an inline link, and the subject's own name is never linked →
    R-8GYQ-ZT9T, R-89NC-P6TN
60. Every subject link the web shows is a fully-qualified front-door URL, while brand/header/Home controls stay in-app →
    R-8I6N-DL0I, R-8JEJ-RCR7
61. The header on every page carries the question box ready for the next question; the answer page holds the question just asked, editable →
    R-2O6U-BWGC, R-2PEQ-PO71
62. A subject page shows the compiled prose and a footer of subjects it points to and that point to it; following any lands on that page →
    R-PH2F-47LB, R-PIAB-HZC0, R-PODT-EU1H
63. Markdown prose renders as styled HTML — heading, bold, list, code, quote, table — never the literal markers →
    R-SS0J-U7PG, R-ST8G-7ZG5, R-SY41-R2EX
64. The rendered web prose is styled with the suite's shared design tokens, visually consistent, no separate styling system →
    R-9FXO-ZONN, R-9EPS-LWWY
65. An inline link in prose renders as a working link; prose with no link renders no inline links →
    R-SZBY-4U5M, R-T2ZN-A5DP
66. Raw HTML or script in compiled-page or answer prose is neutralized in the rendered page →
    R-T0JU-ILWB, R-T1RQ-WDN0
67. A subject named by an alias merged into it links to the surviving subject's page by exact normalized name, never a similar one →
    R-84RR-63UV, R-8C35-GQB1, R-1XX5-QDCY
68. Nothing done on the web changes any subject/claim/page/job; ingest, merge, and job control are not reachable from the web →
    R-2QMN-3FXQ, R-5X56-NF4O
69. `extract`, `match`, `compile`, `ask`, and `embed` models configure independently, and which model runs behind each is confirmable →
    R-GGIG-AN7W, R-GHQC-OEYL, R-GLE1-TQ6O, R-7FBT-OXWC
70. Wiki holds no model-provider credential; with only the central inference service reachable, every model-needing capability works →
    R-KDHD-V3XI, R-A3D7-NTLV
71. One committed tune folder per step lets the standalone tuning tool run end to end on a plain checkout with no suite up, producing a baseline scorecard →
    R-A5T0-FD39, R-A9GP-KOBC, R-AAOL-YG21, R-7YU7-T9RG
72. Each folder's starting prompt is byte-for-byte the service's current prompt for that step, folder and code decoupled →
    R-A5T0-FD39, R-A88T-6WKN
73. Each folder's config measures the prompt under the step's serving model (`gpt-5.6-luna`) with `gpt-5.6-sol` proposing revisions, both confirmable →
    R-A88T-6WKN
74. The five case sets share one fictional universe with held-out/tuning placement aligned across steps →
    R-A9GP-KOBC, R-7YU7-T9RG
75. Scoring an extract, analysis, or correction-matching candidate twice against the same cases gives the same score, no model consulted →
    R-AAOL-YG21, R-ABWI-C7SQ, R-8024-71I5
76. A compile or synthesis output breaking the mechanical contract is punished the same way every time, regardless of judge opinion →
    R-AD4E-PZJF, R-AECB-3RA4
77. The wiki repo builds and its whole suite passes with no tuning executable; the tuning loop and its records belong to the external tool →
    R-1BRG-F9TN, R-KFX6-MNEW, R-A4L4-1LCK
78. A tuning run leaves the committed tree clean — writes land in the ignored run workspace, and no run edits the folder's prompt/cases/config/scorer →
    R-A88T-6WKN, R-A5T0-FD39
79. The deployed service is unchanged by the tune folders — no new tool, endpoint, credential, or behavior attributable to them →
    R-A4L4-1LCK, R-KFX6-MNEW
80. Every chat call — extract, match, compile, and both ask stages — serves on `gpt-5.6-luna` in an unconfigured environment, confirmable per site →
    R-A25B-A1V6, R-9UTW-ZFF0, R-9X9P-QYWE, R-7FBT-OXWC
