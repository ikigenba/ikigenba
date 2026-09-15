# D13 author coverage ledger

Source: `specs/stories/backup.md`, current working tree. This is an author's map;
independent verification remains required. Review labels below identify distinct
criteria and are not requirement ids. D13 is `../design/D13-host-backup.md`.

| Criterion and source locator | Contract | Author disposition |
|---|---|---|
| HOST-I1, intro :3–11, :113–127: one host prefix; separate host/service contents; no nginx or units | R-YBH6-P78Q, R-YCP3-2YZF, R-YISK-ZTOW; D12 service scope | Covered locally; cross-document integration needed |
| HOST-I2, intro :13–16: final backup prevents loss | R-YHKO-M1Y7 is host validation only; retire R-YUZK-TJ3U, R-YXFD-L2L8 conditional successful path | Timeout and general no-loss guarantee unresolved: retire-final-database-guarantee.md |
| HOST-I3, intro :18–35: command menu, region/prefix, timer period keys | D02 menu; D12 timers; R-YBH6-P78Q config | Shared ownership |
| HOST-I4, :38–111: files are nontransactional, declared db owned by Litestream, independent clocks | D11/D12; R-YUZK-TJ3U preserves ordinary exclusions | Covered through dependencies, except retirement timeout conflict |
| HOST-H1, :370–417: host noun motivation, both help flags, exact usage text, stdout/stderr, exit 0, any uid, installed prerequisite and unchanged postcondition | R-YOW2-WOED; D02 installation/root conventions | Covered |
| HOST-B1, :419–437: scheduled command; certificate reuse motive; exact command and successful output | D12 timer; R-YL8D-RD6A, R-Z2AZ-45K0 | Covered |
| HOST-B2, :439–447: configured region/prefix and writable role preconditions; stdout only exit 0 | R-YBH6-P78Q, R-YF4V-UIGT, R-YL8D-RD6A; D01 cloud boundary | Contract covered; live S3 evidence pending centrally |
| HOST-B3, :449–453: timestamped host object; both trees with modes; no opt reads; no overwrite/delete | R-YCP3-2YZF, R-YDWZ-GQQ4 | Covered |
| HOST-R1, :455–471: preserve CA-limited cert plus all store keys, exact restore command and two rows | R-YISK-ZTOW, R-YNO6-IWNO, R-Z2AZ-45K0 | Covered |
| HOST-R2, :473–481: stdout only exit 0, configured source/read role, existing host backup | R-YBH6-P78Q, R-YGCS-8A7I, R-YNO6-IWNO; D01 cloud | Contract covered; live S3 evidence pending |
| HOST-R3, :483–489: newest exact replacement/modes/stale deletion, no opt/unit/nginx effects, init applies restored state, no cloud writes/deletes | R-YGCS-8A7I, R-YHKO-M1Y7, R-YISK-ZTOW, R-YK0H-DLFL | Covered |
| RET-H1, :531–573: distinct verb; stop/final-copy intent; exact help for both flags; any uid; stdout only exit 0; unchanged host | R-YZV6-CM2M; R-YUZK-TJ3U | Help covered, final-sync proof unresolved |
| RET-S1, :575–583: external destroy caller, one row per phase, identical file commands, same timestamp | R-YUZK-TJ3U, R-YW7H-7AUJ, R-YYN9-YUBX, R-Z2AZ-45K0 | Covered conditional ordinary sync |
| RET-S2, :585–595: successful final WAL closure, timeout fallback, daemon timing/signal claims | R-YUZK-TJ3U does not infer sync from stop status | Timeout/fallback product decision and live success protocol unresolved in retire-final-database-guarantee.md; external-observations.md tracks live evidence |
| RET-S3, :597–617: exact retire command/output; exit 0 and empty stderr; configured bucket; active crm/dashboard and declared crm.db preconditions | R-YBH6-P78Q, R-YSJS-1ZMG, R-YTRO-FRD5, R-YYN9-YUBX | Covered conditional successfully synchronized path |
| RET-S4, :619–630: apps stop before Litestream; inactive units; no disable/mask/removal/timer change; current db replica | R-YSJS-1ZMG, R-YTRO-FRD5, R-YUZK-TJ3U, R-YXFD-L2L8 | Stop invariants covered; external final-sync proof unresolved |
| RET-S5, :631–637: all named same-time objects, ordinary exclusions, prior objects preserved; empty-service three rows | R-YUZK-TJ3U, R-YW7H-7AUJ, R-YXFD-L2L8, R-Z3IV-HXAP | Covered conditional normal sync where databases exist |
| RET-F1, :639–670: unreadable crm file; report all failures; continue dashboard/host; stdout only exit 1; units remain down; prior backups untouched; crm db shipped first | R-YUZK-TJ3U, R-YXFD-L2L8, R-YYN9-YUBX | Archive failure covered conditional normal sync; timeout decision not resolved |
| HOST-G1, :1096–1134: missing/unknown subcommand exact diagnostics, blank line/hint, stderr only exit 2, root prerequisite, unchanged state | R-YQ3Z-AG52, R-YRBV-O7VR; D02 root | Covered |
| HOST-S1, necessary public names/package shapes, injected host/cloud/config boundaries and result phase evidence | R-Y5DO-SCJ9, R-Y6LL-649Y, R-Y7TH-JW0N, R-Y91D-XNRC, R-YA9A-BFI1; D01/D11 layout | Covered; Retire synchronization mechanism explicitly open |
| HOST-F1, necessary archive safety/config capture/failure reporting, KiB precision | R-YBH6-P78Q through R-YNO6-IWNO; R-Z132-QDTB | Covered |

Consumer review: `backup-host-consumers.md`. All new ids are from `idgen`.
No source/test/build/check/commit operation was performed. Existing requirement
text in neighboring designs was not changed. New archive filename conventions
match D12 UTC RFC3339Nano with whole-second story examples retained.


## Final local scope status

Completed contracts passed independent verification. See the corresponding `*-verification.md` reports (D14 also `backup-restore-correction-verification.md`). Product/evidence issues remain explicitly scoped. Root owns the final cross-command help-key inventory correction and verification; historical author-pending labels above are superseded by those verdicts.
