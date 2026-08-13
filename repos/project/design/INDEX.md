# repos — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. To resolve an id, grep this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

**Retired Decision numbers — never reused:** D3 (GitHub-fact intake / the
webhooks consumer), D4 (repo lifecycle over GitHub clones), D5 (the session
engine), D6 (the issue protocol), D7 (the v1 MCP surface), D8 (session-outcome
events), D9 (worktree & transcript state layout), D11 (the two direct HTTP
peers), D12 (session correlation ids). Their behaviors left the product with the
v2 rewrite and their ids were deleted with them.

## Decisions

- D1 → `project/design/D01.md` — Composition root & chassis boot — R-EISY-2LYZ, R-EL8Q-U5GD
- D2 → `project/design/D02.md` — Data model & migrations — R-IWEY-SUZH, R-IYUR-KEGV, R-J02N-Y67K, R-J1AK-BXY9, R-J2IG-PPOY
- D10 → `project/design/D10.md` — nginx fragment & the web locations — R-G1OF-AAC8, R-J3QD-3HFN, R-UZVS-S08C, R-V13P-5RZ1, R-G448-1TTM
- D13 → `project/design/D13.md` — The nginx fragment captures the minted correlation id on gated locations and strips it on the ungated one — R-9DUI-TUQJ, R-9F2F-7MH8
- D14 → `project/design/D14.md` — Suite-contract conformance: the opsctl install layout & the authored env contract — R-4LKF-FB23 (adopted), R-8DF1-W89F (adopted), R-8IAN-FB87 (adopted)
- D15 → `project/design/D15.md` — Env-channel conformance: repos' three knobs, and an environment with no credentials — R-39AL-2CA7, R-QX3N-8GNH, R-QYBJ-M8E6, R-QZJG-004V, R-VKB6-SHHV (adopted)
- D16 → `project/design/D16.md` — Adopt the suite testing-language contract — R-O1AD-MRKW (adopted), R-O2IA-0JBL (adopted)
- D17 → `project/design/D17.md` — Custody: the bare-repo store, the `Git` seam, and the ref choke point — R-J4Y9-H96C, R-J665-V0X1, R-J7E2-8SNQ, R-J8LY-MKEF, R-J9TV-0C54, R-JB1R-E3VT, R-JC9N-RVMI
- D18 → `project/design/D18.md` — The loopback filesystem + commit API — R-JJL2-2I2O, R-JKSY-G9TD, R-JM0U-U1K2, R-JN8R-7TAR, R-JOGN-LL1G, R-JPOJ-ZCS5, R-JQWG-D4IU, R-JS4C-QW9J, R-JTC9-4O08, R-JUK5-IFQX, R-JVS1-W7HM, R-JY7U-NQZ0
- D19 → `project/design/D19.md` — The git smart-HTTP door — R-JZFR-1IPP, R-K0NN-FAGE, R-K1VJ-T273, R-K33G-6TXS, R-K4BC-KLOH, R-K5J8-YDF6, R-K6R5-C55V, R-K7Z1-PWWK
- D20 → `project/design/D20.md` — Run tokens: short-lived, repository-scoped push credentials — R-36US-ASST, R-382O-OKJI, R-KAEU-HGDY, R-KBMQ-V84N, R-KCUN-8ZVC, R-KE2J-MRM1
- D21 → `project/design/D21.md` — Statuses and the `merge` verb — R-KFAG-0JCQ, R-KHQ8-S2U4, R-KIY5-5UKT, R-KK61-JMBI, R-KLDX-XE27, R-KMLU-B5SW, R-KNTQ-OXJL, R-KP1N-2PAA, R-KQ9J-GH0Z, R-KRHF-U8RO
- D22 → `project/design/D22.md` — The MCP tool surface — R-KSPC-80ID, R-KTX8-LS92, R-KV54-ZJZR, R-KWD1-DBQG, R-KXKX-R3H5, R-KYSU-4V7U, R-JFAI-W00R, R-JGIF-9RRG, R-L00Q-IMYJ, R-L2GJ-A6FX
- D23 → `project/design/D23.md` — Events: the `push` and `archived` families — R-JDHK-5ND7, R-JFXC-X6UL, R-JH59-AYLA, R-JID5-OQBZ
- D24 → `project/design/D24.md` — The landing page lists the live repositories (server render + layout) — R-RP30-4NV1, R-RQAW-IFLQ, R-RRIS-W7CF, R-RSQP-9Z34, R-RTYL-NQTT, R-RV6I-1IKI, R-RWEE-FAB7, R-RXMA-T21W, R-RYU7-6TSL, R-S023-KLJA, R-S19Z-YD9Z, R-S2HW-C50O, R-S4XP-3OI2, R-S65L-HG8R
- D25 → `project/design/D25.md` — Client-side filter, sort, and pagination of the landing listing — R-S7DH-V7ZG, R-S8LE-8ZQ5, R-S9TA-MRGU, R-SB17-0J7J, R-SC93-EAY8, R-SDGZ-S2OX, R-SEOW-5UFM, R-SFWS-JM6B, R-SH4O-XDX0, R-SICL-B5NP, R-SJKH-OXEE, R-SKSE-2P53, R-SN86-U8MH, R-SOG3-80D6
- D26 → `project/design/D26.md` — Browser wiring proof: one minimal headless-Chrome test (chromedp) — R-SPNZ-LS3V, R-SQVV-ZJUK, R-SS3S-DBL9, R-STBO-R3BY, R-SUJL-4V2N, R-SVRH-IMTC, R-SWZD-WEK1, R-SY7A-A6AQ, R-SZF6-NY1F
- D27 → `project/design/D27.md` — Adopt the suite brand icon contract: the shipped icon set and its link markup — mints none; adopts R-RYDN-YNR5, R-RZLK-CFHU (root `project/design/D29.md`)

## Verification ids → Decision

- R-36US-ASST → D20 — `project/design/D20.md`
- R-382O-OKJI → D20 — `project/design/D20.md`
- R-39AL-2CA7 → D15 — `project/design/D15.md`
- R-4LKF-FB23 → D14 — `project/design/D14.md` (adopted from root `project/design/D08.md`)
- R-8DF1-W89F → D14 — `project/design/D14.md` (adopted from root `project/design/D11.md`)
- R-8IAN-FB87 → D14 — `project/design/D14.md` (adopted from root `project/design/D11.md`)
- R-9DUI-TUQJ → D13 — `project/design/D13.md`
- R-9F2F-7MH8 → D13 — `project/design/D13.md`
- R-EISY-2LYZ → D1 — `project/design/D01.md`
- R-EL8Q-U5GD → D1 — `project/design/D01.md`
- R-G1OF-AAC8 → D10 — `project/design/D10.md`
- R-G448-1TTM → D10 — `project/design/D10.md`
- R-IWEY-SUZH → D2 — `project/design/D02.md`
- R-IYUR-KEGV → D2 — `project/design/D02.md`
- R-J02N-Y67K → D2 — `project/design/D02.md`
- R-J1AK-BXY9 → D2 — `project/design/D02.md`
- R-J2IG-PPOY → D2 — `project/design/D02.md`
- R-J3QD-3HFN → D10 — `project/design/D10.md`
- R-J4Y9-H96C → D17 — `project/design/D17.md`
- R-J665-V0X1 → D17 — `project/design/D17.md`
- R-J7E2-8SNQ → D17 — `project/design/D17.md`
- R-J8LY-MKEF → D17 — `project/design/D17.md`
- R-J9TV-0C54 → D17 — `project/design/D17.md`
- R-JB1R-E3VT → D17 — `project/design/D17.md`
- R-JC9N-RVMI → D17 — `project/design/D17.md`
- R-JDHK-5ND7 → D23 — `project/design/D23.md`
- R-JFAI-W00R → D22 — `project/design/D22.md`
- R-JFXC-X6UL → D23 — `project/design/D23.md`
- R-JGIF-9RRG → D22 — `project/design/D22.md`
- R-JH59-AYLA → D23 — `project/design/D23.md`
- R-JID5-OQBZ → D23 — `project/design/D23.md`
- R-JJL2-2I2O → D18 — `project/design/D18.md`
- R-JKSY-G9TD → D18 — `project/design/D18.md`
- R-JM0U-U1K2 → D18 — `project/design/D18.md`
- R-JN8R-7TAR → D18 — `project/design/D18.md`
- R-JOGN-LL1G → D18 — `project/design/D18.md`
- R-JPOJ-ZCS5 → D18 — `project/design/D18.md`
- R-JQWG-D4IU → D18 — `project/design/D18.md`
- R-JS4C-QW9J → D18 — `project/design/D18.md`
- R-JTC9-4O08 → D18 — `project/design/D18.md`
- R-JUK5-IFQX → D18 — `project/design/D18.md`
- R-JVS1-W7HM → D18 — `project/design/D18.md`
- R-JY7U-NQZ0 → D18 — `project/design/D18.md`
- R-JZFR-1IPP → D19 — `project/design/D19.md`
- R-K0NN-FAGE → D19 — `project/design/D19.md`
- R-K1VJ-T273 → D19 — `project/design/D19.md`
- R-K33G-6TXS → D19 — `project/design/D19.md`
- R-K4BC-KLOH → D19 — `project/design/D19.md`
- R-K5J8-YDF6 → D19 — `project/design/D19.md`
- R-K6R5-C55V → D19 — `project/design/D19.md`
- R-K7Z1-PWWK → D19 — `project/design/D19.md`
- R-KAEU-HGDY → D20 — `project/design/D20.md`
- R-KBMQ-V84N → D20 — `project/design/D20.md`
- R-KCUN-8ZVC → D20 — `project/design/D20.md`
- R-KE2J-MRM1 → D20 — `project/design/D20.md`
- R-KFAG-0JCQ → D21 — `project/design/D21.md`
- R-KHQ8-S2U4 → D21 — `project/design/D21.md`
- R-KIY5-5UKT → D21 — `project/design/D21.md`
- R-KK61-JMBI → D21 — `project/design/D21.md`
- R-KLDX-XE27 → D21 — `project/design/D21.md`
- R-KMLU-B5SW → D21 — `project/design/D21.md`
- R-KNTQ-OXJL → D21 — `project/design/D21.md`
- R-KP1N-2PAA → D21 — `project/design/D21.md`
- R-KQ9J-GH0Z → D21 — `project/design/D21.md`
- R-KRHF-U8RO → D21 — `project/design/D21.md`
- R-KSPC-80ID → D22 — `project/design/D22.md`
- R-KTX8-LS92 → D22 — `project/design/D22.md`
- R-KV54-ZJZR → D22 — `project/design/D22.md`
- R-KWD1-DBQG → D22 — `project/design/D22.md`
- R-KXKX-R3H5 → D22 — `project/design/D22.md`
- R-KYSU-4V7U → D22 — `project/design/D22.md`
- R-L00Q-IMYJ → D22 — `project/design/D22.md`
- R-L2GJ-A6FX → D22 — `project/design/D22.md`
- R-O1AD-MRKW → D16 — `project/design/D16.md` (adopted from root `project/design/D23.md`)
- R-O2IA-0JBL → D16 — `project/design/D16.md` (adopted from root `project/design/D23.md`)
- R-QX3N-8GNH → D15 — `project/design/D15.md`
- R-QYBJ-M8E6 → D15 — `project/design/D15.md`
- R-QZJG-004V → D15 — `project/design/D15.md`
- R-RP30-4NV1 → D24 — `project/design/D24.md`
- R-RQAW-IFLQ → D24 — `project/design/D24.md`
- R-RRIS-W7CF → D24 — `project/design/D24.md`
- R-RSQP-9Z34 → D24 — `project/design/D24.md`
- R-RTYL-NQTT → D24 — `project/design/D24.md`
- R-RV6I-1IKI → D24 — `project/design/D24.md`
- R-RWEE-FAB7 → D24 — `project/design/D24.md`
- R-RXMA-T21W → D24 — `project/design/D24.md`
- R-RYDN-YNR5 → D27 — `project/design/D27.md` (adopted from root `project/design/D29.md`)
- R-RYU7-6TSL → D24 — `project/design/D24.md`
- R-RZLK-CFHU → D27 — `project/design/D27.md` (adopted from root `project/design/D29.md`)
- R-S023-KLJA → D24 — `project/design/D24.md`
- R-S19Z-YD9Z → D24 — `project/design/D24.md`
- R-S2HW-C50O → D24 — `project/design/D24.md`
- R-S4XP-3OI2 → D24 — `project/design/D24.md`
- R-S65L-HG8R → D24 — `project/design/D24.md`
- R-S7DH-V7ZG → D25 — `project/design/D25.md`
- R-S8LE-8ZQ5 → D25 — `project/design/D25.md`
- R-S9TA-MRGU → D25 — `project/design/D25.md`
- R-SB17-0J7J → D25 — `project/design/D25.md`
- R-SC93-EAY8 → D25 — `project/design/D25.md`
- R-SDGZ-S2OX → D25 — `project/design/D25.md`
- R-SEOW-5UFM → D25 — `project/design/D25.md`
- R-SFWS-JM6B → D25 — `project/design/D25.md`
- R-SH4O-XDX0 → D25 — `project/design/D25.md`
- R-SICL-B5NP → D25 — `project/design/D25.md`
- R-SJKH-OXEE → D25 — `project/design/D25.md`
- R-SKSE-2P53 → D25 — `project/design/D25.md`
- R-SN86-U8MH → D25 — `project/design/D25.md`
- R-SOG3-80D6 → D25 — `project/design/D25.md`
- R-SPNZ-LS3V → D26 — `project/design/D26.md`
- R-SQVV-ZJUK → D26 — `project/design/D26.md`
- R-SS3S-DBL9 → D26 — `project/design/D26.md`
- R-STBO-R3BY → D26 — `project/design/D26.md`
- R-SUJL-4V2N → D26 — `project/design/D26.md`
- R-SVRH-IMTC → D26 — `project/design/D26.md`
- R-SWZD-WEK1 → D26 — `project/design/D26.md`
- R-SY7A-A6AQ → D26 — `project/design/D26.md`
- R-SZF6-NY1F → D26 — `project/design/D26.md`
- R-UZVS-S08C → D10 — `project/design/D10.md`
- R-V13P-5RZ1 → D10 — `project/design/D10.md`
- R-VKB6-SHHV → D15 — `project/design/D15.md` (adopted from root `project/design/D11.md`)

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped
to the id(s) whose tests prove it; the quality of each proof is the audit's
question, the mapping's completeness is this manifest's. Regenerated with the
rest of the index.

1. Editing a site/script/prompt through its service leaves an attributed commit,
   no separate git step →
   R-JQWG-D4IU, R-JUK5-IFQX
2. Owner clones from a laptop with a dashboard token, sees full history, commits
   on a branch, and pushes it back visibly →
   R-JZFR-1IPP, R-K0NN-FAGE
3. `git push --force` to `main` fails and leaves `main` at the prior commit →
   R-K1VJ-T273, R-JB1R-E3VT
4. An on-box run clones only its one repository, pushes a branch, cannot push
   `main` even fast-forward, and cannot touch any other repository →
   R-KAEU-HGDY, R-KCUN-8ZVC
5. Merging a branch with a failing/outstanding check is refused naming the check;
   recording a pass then merging succeeds and moves `main` →
   R-KMLU-B5SW, R-KNTQ-OXJL, R-KIY5-5UKT
6. Merging a branch that conflicts with `main` is refused and leaves `main`
   unchanged →
   R-KLDX-XE27
7. An owning service fetches the whole current tree with no version-control
   leftovers, and any single file at a named point in history →
   R-JOGN-LL1G, R-JKSY-G9TD
8. Archiving makes a repository vanish from listings while its history stays
   intact and its name is reusable →
   R-KYSU-4V7U, R-J8LY-MKEF
9. Renaming leaves the repository reachable under the new name with the same
   commits and unreachable under the old →
   R-KXKX-R3H5
10. Every branch movement, from any door, is observable as one suite event naming
    the repository, branch, commit, and actor →
    R-K4BC-KLOH, R-JID5-OQBZ
11. Nothing repos does causes a request to github.com →
    R-SZF6-NY1F
12. A logged-in user opening `/srv/repos/` sees the name, version, and live-repo
    listing; typing narrows it; a row's copy control yields the clone address;
    archived repos are absent; a sessionless browser is refused →
    R-RXMA-T21W, R-SQVV-ZJUK, R-SVRH-IMTC, R-UZVS-S08C
