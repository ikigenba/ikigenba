# Init independent verification, round 1

Fresh read-only verifier `/root/early/verify_init` inspected full init stories,
setup additions, D01/D03/D04/D06/D07/D11/D12 and consumer usage. No edits or probes.

## Findings

D05 composition is sound for the ten-key positive-period configured-host case.
D07 lacks cert.Obtain empty-email rejection despite D05 passing the absent key
as empty; webhost owns a fresh correction. D11 zero/unset replication periods
and absent region/prefix remain unresolved in ../issues/backup-replication-periods.md.
The ready example originally said “any desired backup keys”; the coordinator
replaced this with all ten concrete settings and adjacent unresolved cases.

## Complete 28-row matrix

P=pass, U=partial due to input/API behavior, B=requires integration owner.

| Labels from early-init.md | Verdict |
|---|---|
| init/intro.command; init/intro.refresh | U |
| init/intro.top-help; init/intro.host-key; init/intro.sequence; init/intro.all-checks; init/intro.streams | P |
| init/intro.regeneration | B: restore integration owner |
| init/help.command-output; init/help.pre-post | P |
| init/ready.command-output; init/ready.post.files-units | U |
| init/ready.pre.path; init/ready.pre.store-dns; init/ready.post.idempotent; init/ready.wildcard-explanation | P |
| init/not-ready.command-output; init/not-ready.pre-post | P |
| init/retry.command-pre; init/retry.output-post | U |
| init/argument.command-output; init/argument.pre-post | P |
| init/corrupt.command-output; init/corrupt.pre-post | P |
| certificates/intro.init-order; nginx/intro.init-step | P |
| backup/what-is-backed-up.init-steps; backup/period-zero.command-post | U |

Verifier reported 18 P/9 U/1 B, but the explicit matrix contains 19 P/8 U/1 B;
this arithmetic discrepancy is recorded for follow-up instead of silently
changing the verifier's report. Six init headings: help, not-ready, arguments,
corrupt fully verified; ready and retry partial (12 covered and 4 partial
heading criteria). Intro/addition rows counted separately.

## Evidence

Exact decoded help matches story. Four independent ordered PATH checks;
provider opens once; dns.Open callback returns preopened provider and retains
injected LookupNS. Host membership independent of provider failure. Wildcard
requires equal nonempty canonical IP sets; both lookups execute. Setup order,
termination, retained prior effects, silent successful subprocess output and
captured error detail explicit. Every API resolves. Four HEAD requirement
lines byte-identical, sixteen added, ten removed; no design fences.

External blockers remain separate: missing Litestream on designated host and
unobserved issuance/application/renewal, recorded in central evidence.
