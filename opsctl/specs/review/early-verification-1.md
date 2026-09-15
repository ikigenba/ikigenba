# Early contract independent verification, round 1

Verifier: fresh read-only agent `/root/early/verify_cli_dns`. Sources: full bootstrap/config/dns stories, D01–D04, early author reviews, central environment and webhost observations. No edits, probes, or gates.

## Verdict and defects

Corrections required. D04 R-E76H-ITJL exact Client fields exclude private resolver state; R-L5F3-010I lacks normative per-client association with Env.LookupNS. Declare exported fields and retention. D03 R-3C3B-V34P corrupt handling conflicts with R-8Q6R-PBF6 malformed-input handling; validate arguments before reading store. D04 R-F43R-UMKC unconfigured handling conflicts with manual hook environment diagnostics R-LRD9-VWD0/R-LSL6-9O3P; validate hooks first. D03 R-NHVQ-SZF1/R-NJ3N-6R5Q group separate declarations; split with fresh ids.

Existing-set ACME TTL is unresolved: DNS.AUTH.INTRO.01 and DNS.AUTH.POST.01 state 60 while R-LDYD-OF7D/R-FIQK-FVGO preserve existing TTL. See `../issues/dns-acme-existing-ttl.md`.

## Per-criterion matrix

Bootstrap/config labels use six aggregate cells `.intro`, `.commands`, `.output`, `.pre`, `.post`, `.acceptance`; these are aggregate cells, not atomic criteria. P=pass, D=drafting defect, X=other integration owner.

| Labels | Verdict |
|---|---|
| B.00–B.04, B.06–B.07, all cells | P |
| B.05 commands/output/pre/post/acceptance | P |
| B.05.intro; C.01.intro; C.03.intro; C.09.intro | N/A |
| C.00.intro; C.02.intro | X: backup-first/ten-key inventory |
| C.00/C.02 remaining five cells | P |
| C.01/C.03/C.09 remaining five cells | P |
| C.04–C.07, C.11 all six cells | P |
| C.08/C.10 commands/output/acceptance | D: config precedence |
| C.08/C.10 intro/pre/post | P |

Counts: 120 aggregate cells: 108 P, 6 D, 2 X, 4 N/A.

DNS labels use the exact 93-label inventory in early-dns.md. U=unresolved input.

| DNS suffix labels | Verdict |
|---|---|
| INTRO all 6; HELP all 6 | P |
| CHECK_OK INTRO.01, COMMAND.01, OUTPUT.01, ACCEPTANCE.01 | D: resolver association |
| CHECK_OK PRE.01–03, POST.01 | P |
| CHECK_FAILED INTRO.01, COMMAND.01, OUTPUT.01, ACCEPTANCE.01 | D: resolver association |
| CHECK_FAILED PRE.01–02, POST.01 | P |
| READ all 6; MUTATE all 8; OUTSIDE all 6; LIST_UNKNOWN all 5 | P |
| AUTH INTRO.01, POST.01 | U: existing-set TTL |
| AUTH COMMAND.01, OUTPUT.01, ACCEPTANCE.01, PRE.01–02, POST.02 | P |
| CLEANUP all 7 | P |
| HOOK_MANUAL COMMAND.01, OUTPUT.01, ACCEPTANCE.01 | D: precedence |
| HOOK_MANUAL INTRO.01, PRE.01, POST.01 | P |
| TIMEOUT all 7 | P |
| UNCONFIGURED INTRO.01 | D: precedence |
| UNCONFIGURED COMMAND.01, OUTPUT.01, ACCEPTANCE.01, PRE.01, POST.01 | P |
| INVALID all 7 | P |

Counts: 79 P, 12 D, 2 U. Top-level DNS contribution maps to D02 R-EL65-MXBO/R-EJY9-95KZ.

## Mechanical and integration evidence

Retained requirement lines byte-identical HEAD: D02 7 retained/8 added/5 removed; D03 16/15/6; D04 19/34/11. Exact help strings match source; 14 top-level commands match all story additions. No design fences or quoted-above dependencies. Consumer tasks complete; resolver retention and TTL assertions need correction. Dependency directions coherent. Backup-first and ten-key-init inventory require integration ownership.

Before check-spec: actual certbot hook environment/paired challenge behavior, DNS pagination, signing/credential chain, TXT escaping edge cases require observations. Historical Route53 observations carry provenance but no raw timestamp/transcript. Central webhost help proves grammar only.
