# scripts — Design Index

Each Decision maps to its `project/design/DNN.md`; every `R-XXXX-XXXX` id maps to its Decision/file. Resolve an id by grepping this index (or the Decision files directly). Regenerate this manifest whenever a Decision is added or its Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — The landing handler and its v1 content (service name + version) — owns R-LAND-7Q3D, R-LAND-9R5F, R-LAND-1S7G, R-LAND-3T9H
- D2 → `project/design/D02.md` — Route wiring: `GET /{$}` mounted ungated through `Spec.Handlers` — owns R-ROUT-8U2J, R-ROUT-1V4K, R-ROUT-3W6L
- D3 → `project/design/D03.md` — scripts's own Carbon design assets (shipped in `share/www/static`) — owns R-ASST-5X8M, R-ASST-7Y1N, R-ASST-9Z3P
- D4 → `project/design/D04.md` — nginx fragment: the exact-match session-gated `= /srv/scripts/` location — owns R-NGNX-2A5Q, R-NGNX-4B7R, R-NGNX-6C9S, R-NGNX-8D1T, R-LYZ1-YEW5
- D5 → `project/design/D05.md` — Docs state current truth: state the landing-page surface in scripts's doctrine — none (structural; docs-only)
- D6 → `project/design/D06.md` — Conform the landing page to the cron canonical template — none (structural; markup-only)
- D7 → `project/design/D07.md` — A top-left Home link to the dashboard landing page — owns R-HOME-8R2V
- D8 → `project/design/D08.md` — Self-serve the landing page's fonts and eliminate the FOUT (relative stylesheet link + `font-display: optional` + self-served `src` + `<head>` preload + session-gated nginx `/srv/scripts/static/`) — owns R-M59W-5CAW, R-M6HS-J41L, R-M8XL-ANIZ, R-MA5H-OF9O, R-MBDE-270D
- D9 → `project/design/D09.md` — Runs live under the service-owned `cache/` dir, not the root-owned AppDir (`scriptsRuntimeRoot` returns `filepath.Dir(cfg.GenerationPath)` in every layout; fixes the on-box boot crash-loop) — owns R-RUNS-CDIR, R-RUNS-BOOT; adopts R-4LKF-FB23 (root `project/design/D08.md`)
- D10 → `project/design/D10.md` — Adopt `registry`: resolve scripts' own port and peer addresses by name at startup (own port via `MustPort`, dropbox base via `BaseURL`, `go.mod` require/replace, guardrail test that no `30xx` literal remains; peer feed defaults handed to the chassis by D11) — owns R-RGST-SELF, R-RGST-DBOX, R-RGST-NLIT, R-RGST-GMOD; adopts R-8DF1-W89F, R-8IAN-FB87 (root `project/design/D11.md`)
- D11 → `project/design/D11.md` — Consumer loops through `Spec.Consumers` (chassis-owned) + composition-root normalization (delete `runConsumer`/`Workers`/the `var rt` capture/the legacy `Consumes`+`Subscriptions` fields; one fully-formed Spec literal) — owns R-8WN1-0VQI, R-8XUX-ENH7
- D12 → `project/design/D12.md` — Web surface from `share/www` through the chassis (de-embed; `Spec.WWW`, delete `internal/web`) — owns R-8Z2T-SF7W, R-90AQ-66YL
- D13 → `project/design/D13.md` — MCP surface over `appkit/mcp`: `internal/mcp` becomes the sixteen-tool domain table; chassis `health`+`reflection` added; runtime contract moves to `Spec.Health` — owns R-91IM-JYPA, R-92QI-XQFZ
- D14 → `project/design/D14.md` — Delete the `internal/db` open/migrate shim and true up the doctrine — none (structural)
- D15 → `project/design/D15.md` — The session-gated locations opt into the apex `@login_bounce`: a logged-out human navigation goes to sign-in, not a bare 401 (bearer tier deliberately excluded) — owns R-465K-NCPV, R-47DH-14GK, R-49T9-SNXY
- D16 → `project/design/D16.md` — The bearer tier's identity plumbing: scripts-named variables forwarding all four owner headers — owns R-4EOV-BQWQ, R-4FWR-PINF, R-LXR5-KN5G
- D17 → `project/design/D17.md` — Event-routing conformance: triggers become canonical filter strings (trigger surface + consumer) — owns R-7TR5-QSY4, R-7UZ2-4KOT, R-7W6Y-ICFI, R-7XEU-W467, R-7YMR-9VWW, R-7ZUN-NNNL, R-812K-1FEA
- D18 → `project/design/D18.md` — Event-routing conformance: producer kinds `succeeded`/`failed`, subject = /<script name>, family registry, outbox migration — owns R-82AG-F74Z, R-83IC-SYVO, R-84Q9-6QMD, R-85Y5-KID2
- D19 → `project/design/D19.md` — Structured MCP adoption: `structuredContent` + `outputSchema` on the thirteen structured domain tools, prose exceptions kept text-only, typed error codes from the closed vocabulary (cites `project/design/D20.md` at the repo root) — owns R-C0G0-V0QL, R-C1NX-8SHA, R-C2VT-MK7Z, R-C43Q-0BYO, R-C5BM-E3PD, R-C6JI-RVG2, R-C7RF-5N6R, R-CA77-X6O5
- D20 → `project/design/D20.md` — Error-taxonomy enrichment: `too_large` and `source_unavailable` become real domain sentinels (`script.ErrTooLarge`/`script.ErrSourceUnavailable` classify the import size cap and mirror-fetch failure; mirrors prompts D27) — owns R-CBF4-AYEU, R-CCN0-OQ5J, R-CDUX-2HW8
- D21 → `project/design/D21.md` — The runner-injected `suite` Python module: embedded stdlib-only `suite.py` materialized beside `main.py`, runtime facts as `SUITE_*` env vars, `suite.event()` (the trigger envelope), the `ToolError` exception model — owns R-HVKP-FQRD, R-HWSL-TII2, R-NKLN-6D6F, R-HZ8E-L1ZG
- D22 → `project/design/D22.md` — `suite.mcp(service, tool, args)`: the generic MCP verb client (identity asserted by owner id, structuredContent verbatim, prose fallback, typed errors) — owns R-Q9X1-8DQ2, R-I1O7-CLGU, R-I2W3-QD7J, R-I5BW-HWOX, R-I6JS-VOFM
- D23 → `project/design/D23.md` — `suite.fetch(content_url, dest)`: the content-plane acceptor (loopback URL confinement, streamed + hash-verified, pinned failure mapping) — owns R-I7RP-9G6B, R-I8ZL-N7X0, R-IA7I-0ZNP
- D24 → `project/design/D24.md` — `suite.files`: the file share's filesystem API, service-agnostic (all seven verbs, streaming, `X-Client-Id: scripts:<script_id>`, status-derived failure mapping) — owns R-IBFE-EREE, R-ICNA-SJ53, R-IDV7-6AVS, R-IF33-K2MH, R-IGAZ-XUD6, R-IHIW-BM3V
- D25 → `project/design/D25.md` — Content-plane holder: `GET /run-content` over run dirs (chassis loopback guard, 404-never-leaks) + `content_url` on non-directory `run_fs_list` entries — owns R-IIQS-PDUK, R-IJYP-35L9, R-IMEH-UP2N, R-INME-8GTC
- D26 → `project/design/D26.md` — `describe` teaches the runtime contract (the `suite` module, the error model, products travel by reference) and the git-backed model (entrypoint, pinned checkout, run token, authored merge) — owns R-IOUA-M8K1, R-2ZVO-S8YU
- D27 → `project/design/D27.md` — `suite.files` share paths: client-side leading-slash normalization on every share-path argument (`list` prefix included, absent path unchanged), absolute-canonical teaching in `describe` — owns R-ZECX-40UZ, R-ZFKT-HSLO, R-ZGSP-VKCD
- D28 → `project/design/D28.md` — Owner-id keying: rebuild the `scripts` table (`owner_id` sole scoping key + write-once `owner_email` snapshot, rows dropped, `idx_scripts_source` rekeyed), rekey all scoping/`ownsScript` on `owner_id`, expose both owner fields on the MCP surface — owns R-Q2LM-XR9W, R-Q3TJ-BJ0L, R-Q51F-PARA, R-Q69C-32HZ, R-Q7H8-GU8O, R-Q8P4-ULZD

- D29 → `project/design/D29.md` — `runs.correlation_id`: mint-or-inherit at spawn, stored on the run, exposed on the run surface — owns R-4OW5-Q1ND, R-4Q42-3TE2, R-4RBY-HL4R, R-4SJU-VCVG
- D30 → `project/design/D30.md` — The chain crosses the sandbox boundary: `SUITE_CORRELATION_ID` and `X-Correlation-Id` on every `suite.*` call — owns R-4UZN-MWCU, R-4W7K-0O3J
- D31 → `project/design/D31.md` — nginx fragment: capture the edge-minted chain id on the gated locations, strip it on the ungated one — owns R-ENOZ-F427, R-EOWV-SVSW
- D32 → `project/design/D32.md` — Rebuild to adopt: chain continuation across the consumer fan-out, the ctx-bearing `Append`, the origin at spawn, and the recorded boundary of a run — owns R-4XFG-EFU8, R-4YNC-S7KX, R-4ZV9-5ZBM, R-5135-JR2B
- D33 → `project/design/D33.md` — Env-channel conformance: the run TTL surfaces in the manifest, bounded at 6h — owns R-HDCE-C6WU; adopts R-VKB6-SHHV (root `project/design/D11.md`), R-34EZ-J9BF (root `project/design/D26.md`)
- D34 → `project/design/D34.md` — Testing-language conformance: adopt the suite contract and make `python3` (and, with D38, `git`) a hard precondition — none minted; adopts R-O1AD-MRKW, R-O2IA-0JBL (root `project/design/D23.md`)
- D35 → `project/design/D35.md` — Script definitions are git-backed trees: `main.py` is the entrypoint, `scripts/<name_key>` is the repo (additive migration; adopts root `project/design/D24.md`) — owns R-20IL-OWGP, R-21QI-2O7E, R-22YE-GFY3, R-246A-U7OS
- D36 → `project/design/D36.md` — The version-plane client: `internal/repos` behind the `script.VersionPlane` seam, injected from `registry.BaseURL("repos")` — owns R-2A9S-R2E9, R-IKGJ-ND9W, R-ILOG-150L, R-IO48-SOHZ, R-IPC5-6G8O, R-IQK1-K7ZD; adopts R-35MV-X124 (root `project/design/D26.md`)
- D37 → `project/design/D37.md` — The write path: every authoring verb is a commit; `delete` archives; `get` reads `main` (no materialized copy — recorded deviation from root `project/design/D24.md`) — owns R-2BHP-4U4Y, R-2CPL-ILVN, R-2DXH-WDMC, R-2F5E-A5D1, R-2GDA-NX3Q, R-2HL7-1OUF, R-2IT3-FGL4
- D38 → `project/design/D38.md` — A run is a real clone pinned to a sha, with a run token in its environment — owns R-2K0Z-T8BT, R-2L8W-702I, R-2MGS-KRT7, R-2NOO-YJJW, R-2OWL-CBAL, R-2RCE-3URZ, R-2SKA-HMIO
- D39 → `project/design/D39.md` — `repos` becomes a trigger source: scripts is the suite's CI runner — owns R-2TS6-VE9D, R-2V03-9602
- D40 → `project/design/D40.md` — Seeding existing scripts into the plane, then retiring the `body` column (guarded migration) — owns R-2W7Z-MXQR, R-2XFW-0PHG, R-2YNS-EH85
- D41 → `project/design/D41.md` — Outbox schema convergence: the `outbox_correlation` rebuild migration, the restored frozen body, and the migration-immutability guard — owns R-NGXY-11YC, R-NJDQ-SLFQ; adopts R-NFQ1-NA7N (root `project/design/D25.md`)
- D42 → `project/design/D42.md` — Adopt the suite brand icon contract: the shipped icon set and its link markup — mints none; adopts R-RYDN-YNR5, R-RZLK-CFHU (root `project/design/D29.md`)
- D43 → `project/design/D43.md` — Adopt the suite lint contract (`root project/design/D30.md`) at tier `strict` — none (structural; the contract carries no per-service ids)

## Verification ids → Decision

- R-20IL-OWGP → D35 → `project/design/D35.md`
- R-21QI-2O7E → D35 → `project/design/D35.md`
- R-22YE-GFY3 → D35 → `project/design/D35.md`
- R-246A-U7OS → D35 → `project/design/D35.md`
- R-2A9S-R2E9 → D36 → `project/design/D36.md`
- R-2BHP-4U4Y → D37 → `project/design/D37.md`
- R-2CPL-ILVN → D37 → `project/design/D37.md`
- R-2DXH-WDMC → D37 → `project/design/D37.md`
- R-2F5E-A5D1 → D37 → `project/design/D37.md`
- R-2GDA-NX3Q → D37 → `project/design/D37.md`
- R-2HL7-1OUF → D37 → `project/design/D37.md`
- R-2IT3-FGL4 → D37 → `project/design/D37.md`
- R-2K0Z-T8BT → D38 → `project/design/D38.md`
- R-2L8W-702I → D38 → `project/design/D38.md`
- R-2MGS-KRT7 → D38 → `project/design/D38.md`
- R-2NOO-YJJW → D38 → `project/design/D38.md`
- R-2OWL-CBAL → D38 → `project/design/D38.md`
- R-2RCE-3URZ → D38 → `project/design/D38.md`
- R-2SKA-HMIO → D38 → `project/design/D38.md`
- R-2TS6-VE9D → D39 → `project/design/D39.md`
- R-2V03-9602 → D39 → `project/design/D39.md`
- R-2W7Z-MXQR → D40 → `project/design/D40.md`
- R-2XFW-0PHG → D40 → `project/design/D40.md`
- R-2YNS-EH85 → D40 → `project/design/D40.md`
- R-2ZVO-S8YU → D26 → `project/design/D26.md`
- R-34EZ-J9BF → D33 → `project/design/D33.md` (adopted from root `project/design/D26.md`)
- R-35MV-X124 → D36 → `project/design/D36.md` (adopted from root `project/design/D26.md`)
- R-465K-NCPV → D15 → `project/design/D15.md`
- R-47DH-14GK → D15 → `project/design/D15.md`
- R-49T9-SNXY → D15 → `project/design/D15.md`
- R-4EOV-BQWQ → D16 → `project/design/D16.md`
- R-4FWR-PINF → D16 → `project/design/D16.md`
- R-4LKF-FB23 → D9 → `project/design/D09.md` (adopted from root `project/design/D08.md`)
- R-4OW5-Q1ND → D29 → `project/design/D29.md`
- R-4Q42-3TE2 → D29 → `project/design/D29.md`
- R-4RBY-HL4R → D29 → `project/design/D29.md`
- R-4SJU-VCVG → D29 → `project/design/D29.md`
- R-4UZN-MWCU → D30 → `project/design/D30.md`
- R-4W7K-0O3J → D30 → `project/design/D30.md`
- R-4XFG-EFU8 → D32 → `project/design/D32.md`
- R-4YNC-S7KX → D32 → `project/design/D32.md`
- R-4ZV9-5ZBM → D32 → `project/design/D32.md`
- R-5135-JR2B → D32 → `project/design/D32.md`
- R-7TR5-QSY4 → D17 → `project/design/D17.md`
- R-7UZ2-4KOT → D17 → `project/design/D17.md`
- R-7W6Y-ICFI → D17 → `project/design/D17.md`
- R-7XEU-W467 → D17 → `project/design/D17.md`
- R-7YMR-9VWW → D17 → `project/design/D17.md`
- R-7ZUN-NNNL → D17 → `project/design/D17.md`
- R-812K-1FEA → D17 → `project/design/D17.md`
- R-82AG-F74Z → D18 → `project/design/D18.md`
- R-83IC-SYVO → D18 → `project/design/D18.md`
- R-84Q9-6QMD → D18 → `project/design/D18.md`
- R-85Y5-KID2 → D18 → `project/design/D18.md`
- R-8DF1-W89F → D10 → `project/design/D10.md` (adopted from root `project/design/D11.md`)
- R-8IAN-FB87 → D10 → `project/design/D10.md` (adopted from root `project/design/D11.md`)
- R-8WN1-0VQI → D11 → `project/design/D11.md`
- R-8XUX-ENH7 → D11 → `project/design/D11.md`
- R-8Z2T-SF7W → D12 → `project/design/D12.md`
- R-90AQ-66YL → D12 → `project/design/D12.md`
- R-91IM-JYPA → D13 → `project/design/D13.md`
- R-92QI-XQFZ → D13 → `project/design/D13.md`
- R-ASST-5X8M → D3 → `project/design/D03.md`
- R-ASST-7Y1N → D3 → `project/design/D03.md`
- R-ASST-9Z3P → D3 → `project/design/D03.md`
- R-C0G0-V0QL → D19 → `project/design/D19.md`
- R-C1NX-8SHA → D19 → `project/design/D19.md`
- R-C2VT-MK7Z → D19 → `project/design/D19.md`
- R-C43Q-0BYO → D19 → `project/design/D19.md`
- R-C5BM-E3PD → D19 → `project/design/D19.md`
- R-C6JI-RVG2 → D19 → `project/design/D19.md`
- R-C7RF-5N6R → D19 → `project/design/D19.md`
- R-CA77-X6O5 → D19 → `project/design/D19.md`
- R-CBF4-AYEU → D20 → `project/design/D20.md`
- R-CCN0-OQ5J → D20 → `project/design/D20.md`
- R-CDUX-2HW8 → D20 → `project/design/D20.md`
- R-ENOZ-F427 → D31 → `project/design/D31.md`
- R-EOWV-SVSW → D31 → `project/design/D31.md`
- R-HDCE-C6WU → D33 → `project/design/D33.md`
- R-HOME-8R2V → D7 → `project/design/D07.md`
- R-HVKP-FQRD → D21 → `project/design/D21.md`
- R-HWSL-TII2 → D21 → `project/design/D21.md`
- R-HZ8E-L1ZG → D21 → `project/design/D21.md`
- R-I1O7-CLGU → D22 → `project/design/D22.md`
- R-I2W3-QD7J → D22 → `project/design/D22.md`
- R-I5BW-HWOX → D22 → `project/design/D22.md`
- R-I6JS-VOFM → D22 → `project/design/D22.md`
- R-I7RP-9G6B → D23 → `project/design/D23.md`
- R-I8ZL-N7X0 → D23 → `project/design/D23.md`
- R-IA7I-0ZNP → D23 → `project/design/D23.md`
- R-IBFE-EREE → D24 → `project/design/D24.md`
- R-ICNA-SJ53 → D24 → `project/design/D24.md`
- R-IDV7-6AVS → D24 → `project/design/D24.md`
- R-IF33-K2MH → D24 → `project/design/D24.md`
- R-IGAZ-XUD6 → D24 → `project/design/D24.md`
- R-IHIW-BM3V → D24 → `project/design/D24.md`
- R-IIQS-PDUK → D25 → `project/design/D25.md`
- R-IJYP-35L9 → D25 → `project/design/D25.md`
- R-IKGJ-ND9W → D36 → `project/design/D36.md`
- R-ILOG-150L → D36 → `project/design/D36.md`
- R-IMEH-UP2N → D25 → `project/design/D25.md`
- R-INME-8GTC → D25 → `project/design/D25.md`
- R-IO48-SOHZ → D36 → `project/design/D36.md`
- R-IOUA-M8K1 → D26 → `project/design/D26.md`
- R-IPC5-6G8O → D36 → `project/design/D36.md`
- R-IQK1-K7ZD → D36 → `project/design/D36.md`
- R-LAND-1S7G → D1 → `project/design/D01.md`
- R-LAND-3T9H → D1 → `project/design/D01.md`
- R-LAND-7Q3D → D1 → `project/design/D01.md`
- R-LAND-9R5F → D1 → `project/design/D01.md`
- R-LXR5-KN5G → D16 → `project/design/D16.md`
- R-LYZ1-YEW5 → D4 → `project/design/D04.md`
- R-M59W-5CAW → D8 → `project/design/D08.md`
- R-M6HS-J41L → D8 → `project/design/D08.md`
- R-M8XL-ANIZ → D8 → `project/design/D08.md`
- R-MA5H-OF9O → D8 → `project/design/D08.md`
- R-MBDE-270D → D8 → `project/design/D08.md`
- R-NFQ1-NA7N → D41 → `project/design/D41.md` (adopted from root `project/design/D25.md`)
- R-NGNX-2A5Q → D4 → `project/design/D04.md`
- R-NGNX-4B7R → D4 → `project/design/D04.md`
- R-NGNX-6C9S → D4 → `project/design/D04.md`
- R-NGNX-8D1T → D4 → `project/design/D04.md`
- R-NGXY-11YC → D41 → `project/design/D41.md`
- R-NJDQ-SLFQ → D41 → `project/design/D41.md`
- R-NKLN-6D6F → D21 → `project/design/D21.md`
- R-O1AD-MRKW → D34 → `project/design/D34.md` (adopted from root `project/design/D23.md`)
- R-O2IA-0JBL → D34 → `project/design/D34.md` (adopted from root `project/design/D23.md`)
- R-Q2LM-XR9W → D28 → `project/design/D28.md`
- R-Q3TJ-BJ0L → D28 → `project/design/D28.md`
- R-Q51F-PARA → D28 → `project/design/D28.md`
- R-Q69C-32HZ → D28 → `project/design/D28.md`
- R-Q7H8-GU8O → D28 → `project/design/D28.md`
- R-Q8P4-ULZD → D28 → `project/design/D28.md`
- R-Q9X1-8DQ2 → D22 → `project/design/D22.md`
- R-RGST-DBOX → D10 → `project/design/D10.md`
- R-RGST-GMOD → D10 → `project/design/D10.md`
- R-RGST-NLIT → D10 → `project/design/D10.md`
- R-RGST-SELF → D10 → `project/design/D10.md`
- R-ROUT-1V4K → D2 → `project/design/D02.md`
- R-ROUT-3W6L → D2 → `project/design/D02.md`
- R-ROUT-8U2J → D2 → `project/design/D02.md`
- R-RUNS-BOOT → D9 → `project/design/D09.md`
- R-RUNS-CDIR → D9 → `project/design/D09.md`
- R-RYDN-YNR5 → D42 → `project/design/D42.md` (adopted from root `project/design/D29.md`)
- R-RZLK-CFHU → D42 → `project/design/D42.md` (adopted from root `project/design/D29.md`)
- R-VKB6-SHHV → D33 → `project/design/D33.md` (adopted from root `project/design/D11.md`)
- R-ZECX-40UZ → D27 → `project/design/D27.md`
- R-ZFKT-HSLO → D27 → `project/design/D27.md`
- R-ZGSP-VKCD → D27 → `project/design/D27.md`

_Retired: R-RGST-PEER (was D10) — the peer feed-URL default resolution it pinned became chassis-owned when D11 moved the consumer loops to `Spec.Consumers`; the behavior is pinned by appkit's `R-464U-T3T1`/`R-47CR-6VJQ`._

_Retired: R-I0GA-YTQ5 (was D22) — the `suite.mcp` happy-path assertion pinned the identity header as `X-Owner-Email`; the owner-id conversion (D28, appkit D13) makes `X-Owner-Id` the gated/asserted header, a changed discriminating behavior now pinned by R-Q9X1-8DQ2._

_Retired: R-25E7-7ZFH, R-27TZ-ZIWV, R-291W-DANK (were D36) — they pinned the first build's client against an invented `/repositories/*` REST surface repos never serves; D36's rewrite pins the real surface (MCP domain verbs + plumbing byte routes) with fresh ids R-IKGJ-ND9W, R-ILOG-150L, R-IO48-SOHZ, R-IPC5-6G8O, R-IQK1-K7ZD, and the old tagged tests are deleted with them._

_Retired: R-HY0I-7A8R (was D21) — it pinned `suite.event()` returning the bare trigger payload verbatim; the suite-wide envelope alignment rewrites that contract (one `{source, kind, subject, event_id, payload}` shape across scripts and prompts), a changed discriminating behavior now pinned by R-NKLN-6D6F._

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped
to the id(s) whose tests prove it; the quality of each proof is the audit's
question, the mapping's completeness is this manifest's. Regenerated with the
rest of the index.

1. A logged-in dashboard user opening `/srv/scripts/` sees a Carbon-styled page
   showing the service name and running version →
   R-LAND-7Q3D, R-LAND-9R5F, R-LAND-1S7G
2. A browser with no dashboard session is refused with `401` →
   R-NGNX-4B7R
3. The version shown matches the version the deployed binary reports →
   R-LAND-1S7G
4. The page's fonts and colors match Carbon and it loads its own embedded
   `tokens.css` and fonts →
   R-ASST-5X8M, R-ASST-9Z3P, R-M8XL-ANIZ
5. An MCP client still discovers the AS via the PRM well-known and calls the
   bearer `/mcp` unchanged; the landing page changed nothing →
   R-ROUT-1V4K, R-ROUT-3W6L
6. `/srv/scripts/feed` still returns `404` and `/health` still responds — the
   landing page shadowed neither →
   R-NGNX-8D1T
7. A script that imports `suite` and calls another service's tool runs to success
   with the effect visible in the target →
   R-Q9X1-8DQ2, R-HVKP-FQRD
8. A script triggered by a file-share event fetches the referenced bytes to disk
   and writes a result file back to the share →
   R-7ZUN-NNNL, R-I7RP-9G6B, R-IDV7-6AVS
9. A file a run wrote is fetched byte-identical afterward by another service using
   only what scripts reported →
   R-IIQS-PDUK, R-INME-8GTC
10. An agent given only scripts' self-description authors a working script using
    the runtime helper without hand-rolling HTTP →
    R-IOUA-M8K1, R-2ZVO-S8YU
11. Creating a script and editing it twice keeps every prior version recoverable,
    and the scripts that existed before stay intact →
    R-2DXH-WDMC, R-2HL7-1OUF, R-2W7Z-MXQR
12. A script cloned to a laptop with a second file the entrypoint imports, pushed
    back, is used whole by the next run →
    R-2L8W-702I
13. A run started long, then edited, finishes on the version it started, and
    scripts reports which version that was →
    R-2MGS-KRT7, R-2K0Z-T8BT
14. A script triggered by a push runs against exactly the pushed code and reports
    back, merging only if its author wrote that step →
    R-2TS6-VE9D, R-2RCE-3URZ
15. Deleting a script removes it from the list while its history stays
    recoverable →
    R-2GDA-NX3Q
16. A run is findable from any record of work it caused; a run and the completion
    it publishes carry the causing action's chain id, and asking scripts for that
    id returns exactly those runs →
    R-4RBY-HL4R, R-4SJU-VCVG, R-4XFG-EFU8
