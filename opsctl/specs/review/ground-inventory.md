# Project ground inventory (non-normative)

Sources: project and ancestor `AGENTS.md`; `../.agents/skills/spec/SKILL.md`
("Project ground"); `../.agents/skills/spec/references/draft.md`; all nine
story groups enumerated by `../stories/README.md`; current D01 and D15.
Read during the 2026-09-14 draft from the worktree whose original review
baseline is `1e9b63f0254d153fa7a041e86649cfcbb994bfb4`.
Labels below are stable review criteria, not requirement ids. This is an
author inventory pending independent ground verification.

| Criterion | Source locator and required outcome | Ground disposition |
|---|---|---|
| GR.01 | spec skill / Project ground: concrete project directory and current implementation | Opening identifies module; stale claim that source/module are absent corrected. |
| GR.02 | Existing AGENTS / Toolchain and Gates: runnable ordered checks | Go 1.26+, golangci-lint v2, llm-lint/key retained; exact five ordered commands unchanged. |
| GR.03 | Existing AGENTS / Test files and spec skill / Tagging: one canonical test-id set | Only `cmd/` and `internal/` Go test files; unchanged canonical grep; assets and shell files excluded. |
| GR.04 | Existing AGENTS / Dependencies; D01 direct module declaration | Exact three approved AWS modules retained; no additional direct dependency or AWS CLI substitution. |
| GR.05 | Existing AGENTS / Commit conventions and ancestor branch rules | Commit template and co-author unchanged; ancestor no-stash/main-only publication still applies. |
| GR.06 | bootstrap / ordinary-user refusal; config / mutations; D01 run seam | Temporary Root and injected EUID; no dependence on gate process uid. |
| GR.07 | dns / live checks and challenges; init / preflight and setup; nginx / apply; certificates / obtain | DNS/cloud/process/time fixtures; live evidence on dev separate from test gates. |
| GR.08 | apps / install, uninstall, restart, status; backup / backup and restore | Domain root invariant extends to subprocess arguments; process/cloud fixtures prevent real service/archive/network actions. |
| GR.09 | release / fresh install, upgrade, repeat, refusals, corruption and version mismatch; D15 standalone shell boundary | Unmodified script in bubblewrap filesystem/user/network isolation; namespace uid cases; fixture downloads and candidate programs; no public installer root override. |
| GR.10 | release / root ownership and untouched configuration | Assert namespace uid 0 ownership and preserved fixture `/etc`/`/opt`; real deployment directories inaccessible. |
| GR.11 | release / The release: static binary, tag/version/asset shape, checksum | Go tests inspect locally built artifacts and exercise publication fixtures; no actual publication from gates. |
| GR.12 | spec skill / absent tool cannot pass; draft reference / observed dependencies | Bash/bwrap capabilities observed locally; unavailable namespace is failure. Host observations remain limited to evidence actually collected. |

Consumer exercises and additional local capability evidence are in
`ground-usage.md`; existing live-host/toolchain evidence remains in
`environment-observations.md`. This ground covers testing/build constraints,
not semantic completion of the story contracts. Release behavior issues and
cloud/dependency evidence issues remain with their design owners.
