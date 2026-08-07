# Phase 23 — Prove the adopted suite contracts at the composition root

*Realizes design Decision 14 (suite-contract conformance: install layout + authored env contract).*

repos adopts three `[proof: per-service]` contract ids (D14) and currently proves
none of them. This phase extends `cmd/repos/main_test.go` (already `package
main`, so `reposSpec()` is directly callable) with a genuine, offline test per
adopted id, matching the shape the other adopting services carry
(`crm/cmd/crm/main_test.go`, `dropbox`, `github`).

End state — `cmd/repos/main_test.go` additionally holds:

- **A committed-manifest portability test** (`R-8DF1-W89F`): reads
  `../../etc/manifest.env` and fails if the bytes contain an absolute `/opt/`
  path or a line beginning `REPOS_DB_PATH=` or `REPOS_GENERATION_PATH=`.
- **A manifest byte-equality test** (`R-8IAN-FB87`): builds a
  `manifest.Fields` from the `appkit.Spec` that `reposSpec()` returns — `App`,
  `Mount`, `Default`, `Port`, `MCP`, `Feed`, the consumer sources as `Consumes`,
  and the Spec's manifest extras — renders it with `appkit/manifest.Emit`, and
  fails unless the result is byte-identical to the committed
  `../../etc/manifest.env`. Read the fields **off the Spec value**, not from
  re-typed literals, so editing the Spec without editing the committed file (or
  the reverse) fails the test.
- **A boot-from-the-install-layout smoke** (`R-4LKF-FB23`): builds the binary
  into a `t.TempDir()` tree shaped like `/opt/repos/` and boots it. The tree is
  `state/`, `cache/`, `libexec/repos-<VERSION>` with `bin/run` symlinked to it,
  `etc/<VERSION>/manifest.env` copied byte-for-byte from the committed authored
  file with `etc/current` → `<VERSION>`, and — repos-specific —
  `share/<VERSION>/www` populated from the committed `share/www/` tree
  (`landing.html` plus `static/`) with `share/current` → `<VERSION>`. The test
  asserts `bin/run`, `etc/current`, and `share/current` resolve to their
  intended targets and that the manifest selected through `etc/current` equals
  the committed one, then runs `bin/run serve` on a free loopback port and polls
  `/health`. It asserts `200` with `service: "repos"` and `status: "ok"`, that
  the DB landed under `state/` and the generation sidecar under `cache/`, that
  the state root repos writes its clones and transcripts into is inside the
  tree's `state/` tier, and that `GET /` renders the landing page served from
  the `share/current` web root.

Four constraints this phase must respect, all stated in D14:

- repos declares `WWW: true`, so the chassis loads a web root at boot and a
  missing or unresolvable one fails `serve`. The `share/` tier is not optional
  scaffolding here — building it is part of the claim.
- `Handlers` runs `runner.ValidateModel`, which rejects an empty provider API
  key, so the smoke passes a throwaway `ANTHROPIC_API_KEY` in the child's
  environment. It is a parse/lookup gate only; nothing contacts a provider.
- repos is a `webhooks` consumer. Offline that feed is unreachable; the
  eventplane consumer retries with backoff inside its own loop and must not fail
  boot. Assert nothing about consumption here — those claims are D3's.
- The smoke is **offline**: no network, no real credentials, no `bin/start`, no
  shared host ports beyond a `t.TempDir()` tree and a free loopback port picked
  at test time.

`R-8IAN-FB87` is a different claim from the existing
`TestManifestVerbMatchesCommittedServiceContract` (`R-EISY-2LYZ`, D1), which
pins the binary's `manifest` **verb**. Leave that test as it is; do not fold
either into the other.

No non-test source changes are expected. If a test cannot be written without a
production-code seam, add the smallest seam that makes the behavior observable —
do not weaken the assertion to fit the current shape.

**Done when:**

- `R-4LKF-FB23` — a fresh `/opt/repos/`-shaped temp root (including the
  `share/<VERSION>/www` tier and `share/current`) boots the built binary via
  `bin/run serve`, serves the health envelope and the landing page, with the DB
  under `state/`, the sidecar under `cache/`, and the file state root under
  `state/`; the id appears verbatim as a `// R-4LKF-FB23` tag on that test.
- `R-8DF1-W89F` — the committed `etc/manifest.env` carries no `/opt/` path and
  no `REPOS_DB_PATH=` / `REPOS_GENERATION_PATH=` line; the id appears verbatim
  as a `// R-8DF1-W89F` tag on that test.
- `R-8IAN-FB87` — `manifest.Emit` over the fields read off `reposSpec()`
  byte-equals the committed `etc/manifest.env`; the id appears verbatim as a
  `// R-8IAN-FB87` tag on that test.
- All three tests are genuine — each fails when its subject is broken (verify by
  temporarily perturbing the committed manifest, the Spec's declared fields, and
  the staged `share/current` tier, seeing the corresponding test fail, then
  restoring). No `t.Skip`, no test that passes vacuously.
- The suite is green per design's *Conventions*, from `repos/`:
  `go build ./...` exits 0, `go test ./...` passes with no failures and no
  `SKIP`, `go vet ./...` is clean, and `gofmt -l .` prints nothing.
- From `repos/`, all three ids are tagged in **test** files (not merely in the
  spec) — this command prints exactly `3`:

  ```
  grep -rhoE 'R-4LKF-FB23|R-8DF1-W89F|R-8IAN-FB87' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l
  ```
