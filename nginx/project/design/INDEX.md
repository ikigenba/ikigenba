# nginx — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolve an id by grepping this index (or the Decision files
directly). Regenerate this file whenever a Decision is added or its Verification
ids change.

## Decisions

- **D1** → `project/design/D01.md` — The local dev front door: one unprivileged nginx on `:8080` — ids: none (one nginx config file in a tree no module owns; proven by `nginx -t` and by running the real chain)
- **D2** → `project/design/D02.md` — `run`: fragment regeneration and foreground launch — ids: none (untested-by-decision repo-root shell tooling; structural `bash -n` + byte-identical fragment check)
- **D3** → `project/design/D03.md` — The parked `default_server` front door for non-apex hosts — ids: none (two static committed files + an operator runbook; the real-CA/real-nginx claim is verified once, on the live box, outside any gate)

## Verification ids → Decision

None. This tree mints no requirement ids; see *Requirement ids* in
`project/design/README.md` for why, and each Decision's **Verification** section
for the check that stands in its place.
