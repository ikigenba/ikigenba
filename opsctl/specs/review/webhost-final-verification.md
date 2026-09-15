# Webhost corrected-scope independent verification

Fresh read-only verifier `/root/webhost/webhost_reverify` reviewed all D06/D07 stories, requirements, consumer tasks, corrections and relevant D01/D02/D03/D04/D05/D08/D12/D13 boundaries. This record precedes the additional nginx.Write restore extension; its separate author/verifier report must close that new integration.

**Verdict:** all bounded corrections pass, no additional correctable defect. Nginx 12/12 story rows plus structural row pass; 7/7 headings covered. Certificates 13/13 rows and 8/8 headings covered. The extended failed-reload destination policy remains open separately; external observation gaps block check-spec, not coverage of authored outcomes.

| Criterion | Verdict | Evidence |
|---|---|---|
| NG-I1 | PASS | R-54PQ-5Q31/R-5C14-GCJ7/R-5I4M-D78O ownership/determinism/sole file. |
| NG-I1B | PASS | D12 R-DCSZ-8N4J, D13 R-YCP3-2YZF exclude nginx; D13 R-YISK-ZTOW subsequent init; D05 R-LTT7-37TO/R-A61A-1YZM regenerate. |
| NG-I2 | PASS | D08 plus R-5C14-GCJ7/R-5D90-U49W/R-5EGX-7W0L; R-FXJ8-SIJ3 invalid/default conflict rejection. |
| NG-I3 | PASS | D02 R-EL65-MXBO top help. |
| NG-I4 | PASS | D05 R-LGEA-VQO1/R-LTT7-37TO/R-A61A-1YZM sequence. |
| NG-HELP | PASS | R-58DF-B1B4 independently decoded exact output. |
| NG-BARE | PASS | R-5C14-GCJ7/R-5D90-U49W/R-5GWP-ZFHZ independent exact frame reconstruction. |
| NG-APPS | PASS | R-5C14-GCJ7/R-5D90-U49W/R-5EGX-7W0L/R-5GWP-ZFHZ independent exact fixture. |
| NG-APPLY | PASS stated scenario | R-5I4M-D78O/R-5JCI-QYZD/R-W6Y4-9GMN and R-FTVJ-N7B0/R-FWBC-EQSE success and ordinary preparation/publication failures. Extended failed-reload state remains open. |
| NG-REJECT | PASS | R-5KKF-4QQ2/R-W5Q7-VOVY/R-W6Y4-9GMN rollback/no reload/quoted external detail. |
| NG-NOHOST | PASS | R-5AT8-2KSI/R-FTVJ-N7B0 missing host and other config errors. |
| NG-USAGE | PASS | R-FSNN-9FKB exact errors plus extra args no-effects rejection. |
| NG-STRUCT | PASS | R-54PQ-5Q31/R-55XM-JHTQ/R-575I-X9KF shapes and completed error consumers. |
| CERT-BASE | PASS | R-GFIN-FMDJ/R-YMBY-ZHP5 names/hooks/lineage. |
| CERT-BACKUP | PASS | R-YYIY-T743 and D13 R-YCP3-2YZF/R-YISK-ZTOW whole letsencrypt preservation/restore. |
| CERT-ENTRY | PASS | D02/D05 and R-FRFQ-VNTM domain/init missing email closure. |
| CERT-TIMER | PASS | D12 R-FMBG-XGQX/R-FOR9-P08B/R-FPZ6-2RZ0/R-FR72-GJPP complete timer owner. |
| CERT-CONFIG | PASS | R-YJW6-7Y7R/D03. |
| CERT-HELP | PASS | R-YHGD-GEQD exact independent decoded output. |
| CERT-OBTAIN | PASS | R-GFIN-FMDJ/R-YMBY-ZHP5/R-YORR-R16J/R-YXB2-FFDE. |
| CERT-CURRENT | PASS | R-YNJV-D9FU/R-YXB2-FFDE. |
| CERT-SHOW | PASS | R-YR7K-IKNX/R-YTND-A45B. |
| CERT-MISSING | PASS | R-YSFG-WCEM/R-YUV9-NVW0. |
| CERT-REFUSED | PASS | R-YORR-R16J/R-YPZO-4SX8/R-YXB2-FFDE/D02. |
| CERT-UNCONFIGURED | PASS | R-YJW6-7Y7R/R-FRFQ-VNTM CLI and domain checks. |
| CERT-GRAMMAR | PASS | R-YIO9-U6H2/R-YYIY-T743. |

All 38 declaration IDs in the reviewed snapshot unique: D06 18/D07 20. Retired R-59LB-OT1T and R-YL42-LPYG absent. Author correction records account for additions/replacement; untracked documents and absence of mint-time snapshot limit independent proof of permanence, no visible reuse found.

At review time D14 R-G9VR-TU5G prohibited regeneration as well as reload. Root separately requested nginx.Write and D14 callback correction; this report does not approve that extension. See subsequent webhost-write review records. Existing [reload policy](../issues/nginx-reload-failure.md) and [external observations](../issues/webhost-external-observations.md) remain explicit. No edits/probes/tests/builds/commits by verifier.
