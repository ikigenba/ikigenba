# nginx — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolve an id by grepping this index (or the Decision files
directly). Regenerate this file whenever a Decision is added or its Verification
ids change.

## Decisions

- **D1** → `project/design/D01.md` — The local dev front door: one unprivileged nginx on `:8080` — ids: none (one nginx config file in a tree no module owns; proven by `nginx -t` and by running the real chain)
- **D2** → `project/design/D02.md` — `run`: fragment regeneration and foreground launch — ids: none (untested-by-decision repo-root shell tooling; structural `bash -n` + byte-identical fragment check)
- **D3** → `project/design/D03.md` — The parked `default_server` front door for non-apex hosts — ids: none (two static committed files + an operator runbook; the real-CA/real-nginx claim is verified once, on the live box, outside any gate)
- **D4** → `project/design/D04.md` — The testing-language contract: `nginx/` is manual-only, and its conformance is a committed `AGENTS.md` — ids: none (structural adoption of `root project/design/D23.md`; no module and no test file could carry an id tag, so the contract's per-service ids are deliberately not cited)
- **D5** → `project/design/D05.md` — The `michaelgreenly.dev` vhost: the operator's site domain served from `sites` — ids: none (one static committed file + an operator runbook; the real-CA/real-DNS/real-nginx claim is verified once, on the live box, outside any gate)

## Verification ids → Decision

None. This tree mints no requirement ids; see the "no test-file glob and no id
tags" convention in `project/design/CONVENTIONS.md` for why, and each Decision's
**Verification** section for the check that stands in its place.

## Success criteria → ids

`nginx/` mints **no requirement ids** (D1–D5): it is one config tree no Go module
owns, with no test file an id could tag. Each product success criterion
(`project/product/README.md`, in order) therefore maps to the **structural or
manual proof mechanism** that stands in for an id here — the `nginx -t` /
`bash -n` structural gates and the once-only live-stack checks named in each
Decision's **Verification**. The mapping's completeness is this manifest's; the
proofs themselves live in the Decisions.

1. Local front door root returns the dashboard → D1 behavioural check (`bin/start`
   up, the root address serves the dashboard), atop the D1 structural gate
   `nginx -p . -c nginx.conf -t` exiting 0.
2. Service path with no credentials is refused at the front door → D1 behavioural
   check (a `/srv/<svc>/` request with no credentials is refused at nginx via
   `auth_request`, before the service).
3. Service path with valid credentials reaches the service, prefix stripped,
   identity headers present → D1 behavioural check (real `auth_request` against
   the real dashboard; prefix stripped; `X-Owner-Email`/`X-Client-Id` injected).
4. Starting the front door where it has never run before succeeds → D2 structural
   `bash -n run` exiting 0, plus the D2 manual check that a `run` brings the front
   door up cleanly on a fresh checkout.
5. A never-before-seen service route is reachable through the front door → D2
   byte-identical fragment check (`locations/<svc>.conf` regenerated, one per
   service shipping `etc/nginx.conf`, `diff`-clean) plus the D2 manual per-service
   `/srv/<svc>/` reachability check.
6. Live-box HTTPS to a kept non-apex domain returns the parked page → D3 live-box
   manual check (a cert-validating request to at least two of the certificate's
   names returns 200 with the page text).
7. Live-box request to the box's bare address returns the parked page → D3
   live-box manual check (the `default_server` parked page answers the bare
   address with a success status).
8. Parked answer live, the account's apex still returns the dashboard → D3 live-box
   manual check (the apex is unaffected; the excluded domain still fails HTTPS
   validation, proving the exclusion is in force).
9. Parked files installed on the box are byte-identical to the committed ones → D3
   byte-identical check of the installed parked files against their committed
   sources.
10. Live-box HTTPS to `michaelgreenly.dev` returns the operator's public site,
    no longer the parked page → D5 live-box manual check (a cert-validating
    request returns 200 with the site's content, the certificate being the
    domain's own lineage).
11. Site domain live, the other parked domains still park and the apex still
    serves → D5 live-box manual checks (another parked name still returns the
    parked page; the apex still returns the dashboard and routes a
    `/srv/<svc>/` path).
12. The vhost file installed on the box is byte-identical to the committed one →
    D5 byte-identical check of the installed file against its committed source.
