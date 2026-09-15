# CLI/config/DNS final independent verification

Fresh verifier `/root/early/reverify_cli_dns` independently reviewed full source
stories, D01–D04, consumer tasks, correction evidence and central observations.
Resolved contracts pass; no remaining drafting defects. No files changed,
probes, tests, gates, builds, commits or child agents in verification.

## Verified corrections

D04 R-WIKZ-XNM7 exported fields permit private resolver state; R-WJSW-BFCW binds
each client to its own supplied LookupNS and preserves context/nil behavior.
D03 R-WHD3-JVVI validates before reading store; R-WEXA-SCE4/R-WG57-644T scope
corrupt handling accordingly. D04 R-WM8P-2YUA validates hook environment before
store, R-WL0S-P73L handles configuration afterward. Config constants/errors each
have individual declarations. All public shapes and D01 dependency directions
resolve; CommandError stdout/stderr details compose with D02.

## Complete criterion matrix

Bootstrap/config labels each have intro/commands/output/pre/post/acceptance.
P=pass, X=separate integration owner, U=partial with unresolved input.

| Labels | Verdict |
|---|---|
| B.00–B.04, B.06–B.07 all | P |
| B.05 commands/output/pre/post/acceptance | P |
| B.05.intro | N/A |
| C.00.intro; C.02.intro | X: backup-first/ten-key inventory |
| C.00/C.02 remaining five | P |
| C.01/C.03/C.09.intro | N/A |
| C.01/C.03/C.09 remaining five | P |
| C.04–C.08, C.10–C.11 all | P |

120 aggregate cells: 114 P, 2 X, 4 N/A, no drafting failure. All 43 appended
pre/postcondition bullet locators pass: B.01–B.07 all 16, C.01–C.11 all 27;
these match every source bullet exactly with no missing/duplicate. They expand
aliases, not additional outcomes. Replacing 36 aggregate pre/post cells with
43 children yields 127 cells: 121 P, 2 X, 4 N/A.

| DNS labels | Verdict |
|---|---|
| INTRO all6; HELP all6; CHECK_OK all8; CHECK_FAILED all7 | P |
| READ all6; MUTATE all8; OUTSIDE all6; LIST_UNKNOWN all5 | P |
| AUTH INTRO.01, POST.01 | U only existing non-60 TTL; remaining portions pass |
| AUTH COMMAND.01, OUTPUT.01, ACCEPTANCE.01, PRE.01–02, POST.02 | P |
| CLEANUP all7; HOOK_MANUAL all6; TIMEOUT all7; UNCONFIGURED all6; INVALID all7 | P |

DNS: 93 criteria, 91 P and 2 partial with unresolved portion, no drafting failure.

## Source heading counts

Opening prose excluded. Bootstrap: all7 full. Config: 10 full, C.02 fresh-host
heading partial pending ten-key integration; every locally owned outcome passes.
DNS: 12 full, AUTH heading partial only existing non-60 TTL. C.00 first-backup
opening prose separately owned by backup integration.

## Usage and mechanics

Exact config/DNS help source bytes pass. Top-level frame exact and all14 names
and descriptions match: backup, cert, config, dns, host, init, install, nginx,
restart, restore, retire, status, uninstall, version. Consumer tasks complete
and use declared public names. New/existing60 paired challenges pass; generic
TTL preservation and cleanup pass.

Against unpadded HEAD filenames: D02 7 retained/8 added/5 removed; D03 12/25/10;
D04 19/35/11. Every retained requirement line byte-identical, zero edited ids.

## Remaining limits

User decision: ../issues/dns-acme-existing-ttl.md. Before check-spec observations:
actual certbot hooks/paired challenges, Route53 pagination, signing/default
credential chain and TXT escaping. Historical observations carry provenance
but no raw transcript/time; environment observations prove availability only.
