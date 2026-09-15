# Independent nginx verification

Verifier: fresh agent `/root/webhost/nginx_verify`, read-only; recorded by coordinator. Scope: entire nginx story and D06 plus relevant D01/D02/D03/D05/D07/D08/D12 boundaries. Verdict: partial draft pending two correctable errors, input/reload policy, and D14 integration. Live evidence remains a separate check-spec blocker.

| Criterion | Verdict | Evidence |
|---|---|---|
| NG-I1 | PASS | R-54PQ-5Q31, R-5C14-GCJ7, R-5I4M-D78O ownership, deterministic generation, sole persistent nginx file. |
| NG-I1B | PARTIAL | D12 R-DCSZ-8N4J excludes generated nginx from service archives; D05 R-LTT7-37TO/R-A61A-1YZM regenerate through Apply. Host archive exclusion/restore workflow awaits D14. |
| NG-I2 | PARTIAL | D08 discovery and D06 R-5C14-GCJ7/R-5D90-U49W/R-5EGX-7W0L cover ordinary discovery/routing/default/TLS/include/proxy; malformed manifests/multiple defaults remain open. |
| NG-I3 | PASS | D02 R-EL65-MXBO exact top nginx help line. |
| NG-I4 | PASS | D05 R-LGEA-VQO1/R-LTT7-37TO/R-A61A-1YZM nginx.conf step, certificate first, current environment/name. |
| NG-HELP | PASS | R-58DF-B1B4 exact bytes, both flags, any user/no changes. |
| NG-BARE | PASS | R-5C14-GCJ7/R-5D90-U49W/R-5GWP-ZFHZ exact bare frame/read-only. Live TLS effects unobserved. |
| NG-APPS | PASS for story fixture | R-5C14-GCJ7/R-5D90-U49W/R-5EGX-7W0L/R-5GWP-ZFHZ exact crm/dashboard output, empty include, unrouted gmail omitted. |
| NG-APPLY | PARTIAL | R-5I4M-D78O/R-5JCI-QYZD/R-W6Y4-9GMN successful atomic 0644/no-output/repeat/test-reload contract; ordinary preparation error behavior needs correction and reload-failure destination state unresolved. |
| NG-REJECT | PASS | R-5KKF-4QQ2/R-W5Q7-VOVY/R-W6Y4-9GMN restore old bytes/mode or absence, no reload, failure exit/detail. AGENTS/D02 `> ` overrides unquoted story fixture. |
| NG-NOHOST | PASS | R-5AT8-2KSI both commands exact missing/empty config error/no effects. |
| NG-USAGE | PASS for story examples | R-59LB-OT1T missing/unknown errors exact; extra-argument case correction below. |
| NG-STRUCT | PARTIAL | R-54PQ-5Q31/R-55XM-JHTQ/R-575I-X9KF names/signatures resolve; operational error completion needed. |

Heading count: six of seven headings pass their stated fixtures; apply heading is partial. Twelve story-level review rows: nine pass, three partial. Supporting structural row partial.

## Correctable defects

1. R-59LB-OT1T states show/apply accept no arguments but gives no explicit rejection contract for extra arguments: specify exit 2, D02 diagnostic and no effects/configuration reads. D03 R-F8C8-WKEV gives an adjacent convention. No product decision required.
2. Ordinary config corruption/access, discovery failure, rendering failure and unsuccessful publication need explicit propagation: no candidate publication after preparation failure and no downstream test/reload after unsuccessful preparation/publication. Filesystem error wording may remain contextual. Per-service ManifestError remains the separate input-policy issue.

## Independent evidence

Verifier decoded normative strings and independently reconstructed exact help, bare-host and crm/dashboard outputs: all matched byte for byte. Source SHA-256 matches author record. All 15 D06 IDs unique and absent from HEAD prior design. No old D06 text changed under retained ID. All names/templates normative, no code blocks or binding overview prose. D01 host error/execution seam, D03 Store API, D05 workflow, D07 certificate paths and D08 discovery shapes align. D12 excludes nginx from service backups; D14 pending.

Remaining issue references: [input ambiguity](resolved-nginx-input-ambiguities.md), [reload failure](../issues/nginx-reload-failure.md), [external observations](../issues/webhost-external-observations.md). Nginx is inactive on live host; successful config acceptance, TLS/absent glob/reload continuity are not claimed observed. No files edited by verifier, no probes/build/tests/commits.
