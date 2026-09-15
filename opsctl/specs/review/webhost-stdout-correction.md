# Nginx standalone and composed failure output correction

Fresh bounded author: `/root/webhost/nginx_stdout_correct`. This is a contract
review exercise, not runtime evidence. Independent integration verification
is pending. Historical verifier reports remain unchanged.

## Complete consumer tasks

All tasks use D01 `cli.Run` dependencies with EUID 0, an isolated Root,
recording Execute, configured host.name, and controlled cloud/filesystem
fixtures. Run separate cases with an existing nginx destination (known bytes
and mode) and with that destination absent. Fix the injected fault and retry
the operator command to complete its ordinary successful path.

| Task | Previous contract | Corrected result |
|---|---|---|
| Run `opsctl nginx show` then, in a fresh fixture, `opsctl nginx apply`; inject a discovery failure. Repeat with malformed crm manifest and with two routed defaults. | Empty stdout was required, but the wording also applied to composed callers. | R-G0B8-HTXJ/R-G1J4-VLO8 retain empty stdout, D02 failure diagnostic and exit 1 for each standalone command. Render returns no candidate. Invalid manifests/defaults identify the offending service/error or conflicting services before rendering, output, writes or Execute; nginx destination and host state remain unchanged. Repair the input and show returns the normal bytes, while apply publishes, tests and reloads under its existing requirements. |
| Run `opsctl nginx apply` with valid manifests; inject candidate preparation failure, then separately publication failure. | All CLI callers had to have empty stdout. | R-G0B8-HTXJ keeps standalone stdout empty, D02 diagnostic and exit 1; candidate temporaries are cleaned and no test/reload occurs. Preparation preserves prior destination bytes/mode or absence; atomic publication remains R-5I4M-D78O. Repair the fault and retry normal apply. |
| Run `opsctl install s3://fixture/crm-v1.tar.xz` with a valid controlled artifact, configuration, secrets and successful unit setup. At the Configure callback inject a discovery/preparation failure; separately change a manifest after install prevalidation so nginx discovers malformed input or conflicting defaults. | D09 R-P7P9-J6SH retained already emitted successful reports while D06 demanded empty stdout for all CLI callers. | R-G0B8-HTXJ/R-G1J4-VLO8 propagate the failure; D09 R-P0DV-8KCB stops before Litestream work and app start, and R-P7P9-J6SH returns code 1 with a D02 diagnostic and completed progress retained on stdout. No nginx success report appears. R-PA52-AQ9V retains prior completed installation effects; nginx's own failure guarantees still hold. Repair the fault and reinstall the same artifact, preserving state/cache and completing all ordinary reports. |
| Run `opsctl uninstall crm` with valid installed prerequisites and successful stop, unit removal and file removal. Make remaining-service discovery fail when Configure calls Apply; separately introduce a malformed remaining manifest or conflicting routed defaults at that boundary. | D10 completed progress/effects conflicted with D06's command-wide empty-stdout demand. | D10 R-M5Q4-4D4D/R-M9DT-9OCG own composition: completed stop/unit/files reports and effects remain, later nginx/Litestream success is not claimed, and the operational error follows lifecycle's command diagnostic and exit policy. The nginx call itself makes no candidate writes or external calls for discovery/invalid-input failures. Preserved app state remains untouched. The failed command ends with those partial effects reported; after repairing the remaining manifests/access, standalone nginx apply can finish routing. This exercise promises no automatic uninstall retry or replication recovery after removal. |
| Run `opsctl restore crm` with a restore fixture that reaches D14's NginxRegenerator and inject a malformed restored manifest or publication failure. | Write had a correct domain failure contract but its supporting review named retired D14 CLI wiring. | D06 R-QFX9-9MQV propagates failure and preserves the nginx destination; R-QEPC-VV06 prohibits external execution by Write. D14 R-BE42-GCU4 owns CLI wiring, completed stdout steps, exit 1 and stopped-unit diagnostic. D14 prevents subsequent unit starts and owns recovery; this correction changes no restore requirement. |

The install invalid-input cases deliberately inject the error after install's
own prevalidation. A static malformed manifest would otherwise be rejected
before reaching the composition seam being reviewed. No command-wide rollback
or global no-execution claim is inferred from a domain operation's no-effects
promise.

## Requirement changes and scope

Exactly two IDs were minted using `idgen -n 2`:

| Retired | Current | Change |
|---|---|---|
| R-FWBC-EQSE | R-G0B8-HTXJ | Scope empty stdout/D02/exit 1 to standalone nginx show/apply; composed callers own output and exit policy. |
| R-FXJ8-SIJ3 | R-G1J4-VLO8 | Same output scope correction for malformed manifests and conflicting defaults. |

The domain clauses and every other D06 line were compared against the
immediately preceding snapshot and preserved; only the two requirement lines
changed. D06 still contains 22 requirements. Both current IDs occur in design
and are absent from the canonical cmd/internal *_test.go set; both retired
IDs are absent from design and tests. The scoped gap therefore has two new
adds, with no test-removal entries for these retired draft IDs.

Current nginx author maps, the earlier author correction map, the Write author
review and the resolved input issue now use current IDs. Write's supporting
D14 locator is R-BE42-GCU4. Historical verifier reports retain the exact IDs
they reviewed. No source/tests, probes, gates, build/check run or commits were
performed. Existing reload-state and external-observation issues are unchanged.
