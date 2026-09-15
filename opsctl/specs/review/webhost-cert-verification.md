# Independent certificate verification

Verifier: fresh read-only agent `/root/webhost/cert_verify`; recorded by coordinator. All eight certificate story headings verified at CLI level. One correctable domain/init seam omission remains; external observations block check-spec separately; D14 backup integration pending.

| Criterion | Verdict | Evidence |
|---|---|---|
| CERT-BASE | VERIFIED; observation pending | R-GFIN-FMDJ/R-YMBY-ZHP5 apex+wildcard/manual DNS/hooks/lineage. |
| CERT-BACKUP | INTEGRATION PENDING | R-YYIY-T743, D14 must cover whole letsencrypt tree and archive targets. |
| CERT-ENTRY | PARTIAL INTEGRATION | D02 R-EL65-MXBO exact top entry, D05 correct sequence; empty email seam below. |
| CERT-TIMER | VERIFIED; observation pending | D12 R-FMBG-XGQX/R-FOR9-P08B/R-FPZ6-2RZ0/R-FR72-GJPP root renew/twice daily/random/persistent/always enabled/package timer stop+mask. |
| CERT-CONFIG | VERIFIED | R-YJW6-7Y7R and D03 Store API/keys. |
| CERT-HELP | VERIFIED | R-YHGD-GEQD decoded exact help byte match. |
| CERT-OBTAIN | VERIFIED; observation pending | R-GFIN-FMDJ/R-YMBY-ZHP5/R-YORR-R16J/R-YXB2-FFDE silent success, saved hooks, cleanup. |
| CERT-CURRENT | VERIFIED; observation pending | R-YNJV-D9FU/R-YXB2-FFDE certbot due decision, no forced renewal. |
| CERT-SHOW | VERIFIED | R-YR7K-IKNX/R-YTND-A45B SANs/issuer/UTC seconds/read-only. |
| CERT-MISSING | VERIFIED | R-YSFG-WCEM/R-YUV9-NVW0 exact error/exit. |
| CERT-REFUSED | VERIFIED; observation pending | R-YORR-R16J/R-YPZO-4SX8/R-YXB2-FFDE and D02 capture. |
| CERT-UNCONFIGURED | VERIFIED CLI | R-YJW6-7Y7R host first/email missing/no operation. |
| CERT-GRAMMAR | VERIFIED | R-YIO9-U6H2/R-YYIY-T743 exact errors/no renewal command. |

Totals: eight of eight headings verified; eleven of thirteen source rows verified, one partial integration and one pending integration. Observation qualifications are separate from coverage.

## Correctable defect

D05 R-A61A-1YZM passes missing acme.email as empty string to cert.Obtain. D07 R-GFIN-FMDJ only defines execution with nonempty email, and R-YJW6-7Y7R validates CLI cert obtain rather than init. Define Obtain empty-input rejection before execution/state changes, preserving API and D05 call. No user decision required.

## Remaining checks and caveats

All public names and consumer tasks resolve. Deploy hook promises active-only successful reload, supports inactive/absent initial nginx without starting it; private construction must satisfy certbot first-word executable validation. Encoded SAN order matches the story fixture when apex then wildcard is encoded. Story supplies no fixture encoded order; this is a fixture/observation exactness caveat, not demonstrated contradiction. D06 certificate paths match. D01 wider permission for cert to import config is satisfiable alongside D07's narrower host-only restriction.

Nineteen unique IDs and canonical structural/behavioral format. D07 untracked means repository history cannot independently prove mint-time permanence; author records replaced R-YL42-LPYG with R-GFIN-FMDJ, no visible reuse. Live help supports grammar; issuance, saved hooks, cleanup, due-only behavior, deploy hook and timers remain unobserved in [external-observations issue](../issues/webhost-external-observations.md). No unresolved certificate product decisions identified. Verifier edited nothing and performed no probes/build/tests/commits.
