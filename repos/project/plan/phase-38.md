# Phase 38 — Composition root, suite-contract conformance, and the tree's declarations

*Realizes design Decisions 1 (composition root), 14 (install layout & env
contract), 15 (env channels), and 16 (testing language). Depends on Phase 37.*

`cmd/repos/spec.go` reaches its final v2 shape — the Spec's fields, the mount
table, the two Workers, the store-only health reporter, and the D15
`ManifestExtras` — and `etc/manifest.env`, `.envrc`, and `AGENTS.md` are brought
into agreement with it. `cmd/repos/main_test.go` carries the conformance proofs:
the install-layout boot smoke against a temp root with **no credentials in the
environment**, the manifest drift comparison derived from `reposSpec()`, the
box-path-literal scan, the `AGENTS.md` declaration check, and the skip-ban scan.

**Done when:** the suite is green, `grep -c 'API_KEY\|GITHUB' .envrc` prints
`0`, and these ids are each covered by a clearly-named test —

- R-EISY-2LYZ — the manifest render carries the five core fields, the registry
  port with no literal in source, and **no** `CONSUMES` line.
- R-EL8Q-U5GD — the assembled handler gates `/mcp`, renders the landing, serves
  `/feed`, and 404s `/content` for a crossed request.
- R-4LKF-FB23 — the `/opt/repos/`-shaped boot smoke: health 200, the DB and git
  root under `state/`, the sidecar under `cache/`, the landing through
  `share/current`, and every symlink resolving.
- R-8DF1-W89F — the committed manifest carries no absolute `/opt/` path and no
  DB/generation path line.
- R-8IAN-FB87 — `manifest.Emit` over fields read off `reposSpec()` matches the
  committed manifest byte-for-byte.
- R-L9EG-DDWC — the resolved defaults of `REPOS_RUN_TOKEN_TTL` and
  `REPOS_MAX_COMMIT_BYTES` equal the committed manifest's values, and the
  manifest carries no retired key.
- R-VKB6-SHHV — no `"/opt` string literal in non-test Go source.
- R-O1AD-MRKW — `AGENTS.md`'s Tests section declares the gate command, the two
  layers, both environmental preconditions, and the GOWORK mode.
- R-O2IA-0JBL — no `t.Skip` outside live-tagged files (there are none).
