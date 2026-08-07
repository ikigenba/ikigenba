# Phase 19 — Prove the adopted suite contracts at the composition root

*Realizes design Decision 13 (suite-contract conformance: install layout + authored env contract).*

`github` adopts three `[proof: per-service]` contract ids (D13) and currently
proves none of them: `cmd/github/` has no test file at all. This phase creates
`cmd/github/main_test.go` and gives each adopted id a genuine, offline test,
matching the shape the other adopting services already carry
(`crm/cmd/crm/main_test.go`, `ledger`, `notify`, `dropbox`).

End state — `cmd/github/main_test.go` exists in `package main` and holds:

- **A committed-manifest portability test** (`R-8DF1-W89F`): reads
  `../../etc/manifest.env` and fails if the bytes contain an absolute `/opt/`
  path or a line beginning `GITHUB_DB_PATH=` or `GITHUB_GENERATION_PATH=`.
- **A manifest byte-equality test** (`R-8IAN-FB87`): renders `manifest.Emit` over
  the fields `internal/githubapp.Spec()` declares (`APP=github`,
  `MOUNT=/srv/github/`, `DEFAULT=false`, `PORT=3203`, `MCP=true`, no `FEED`, no
  `CONSUMES`, no extras) and fails unless it is byte-identical to the committed
  `../../etc/manifest.env`. The test must fail if either side is edited alone.
- **A boot-from-the-install-layout smoke** (`R-4LKF-FB23`): builds the binary into
  a `t.TempDir()` tree shaped like `/opt/github/` — `state/`, `cache/`,
  `libexec/github-<VERSION>`, `bin/run` symlinked to it, `etc/<VERSION>/manifest.env`
  copied byte-for-byte from the committed authored file, and `etc/current`
  symlinked to `<VERSION>` — asserts `bin/run` and `etc/current` resolve to their
  intended targets and that the manifest selected through `etc/current` equals the
  committed one, then runs `bin/run serve` on a free loopback port and polls
  `/health`. It asserts `200` with `service: "github"`, `status: "ok"`, a
  `details` object, and that the DB landed under `state/` and the generation
  sidecar under `cache/`.

Two constraints this phase must respect, both stated in D13:

- The Spec's `Config` hook parses `IKIGENBA_APP_PRIVATE_KEY` and fails to serve if
  it does not parse, so the smoke generates a throwaway RSA key with
  `crypto/rsa` + `encoding/pem` at test time and passes it in the child's
  environment. This is a parse gate only; nothing contacts GitHub.
- The smoke is **offline**: no network, no real credentials, no `bin/start`. The
  health reporter's real GitHub call is expected to fail in this environment and
  surface inside `details` while the envelope still reports `status: "ok"` — the
  liveness route's documented behavior. Do **not** assert on a successful GitHub
  call here; that claim is `R-DMUT-QF4A` under D2, an out-of-loop live
  verification this phase neither touches nor schedules.

`github` embeds its web assets (`internal/web`) and declares no `WWW`, so there is
no `share/` tier to build in the temp root and no `GITHUB_WWW_PATH` to set.

No non-test source changes are expected. If a test cannot be written without a
production-code seam, add the smallest seam that makes the behavior observable —
do not weaken the assertion to fit the current shape.

**Done when:**

- `R-4LKF-FB23` — a fresh `/opt/github/`-shaped temp root boots the built binary
  via `bin/run serve` and serves the health envelope, with the DB under `state/`
  and the sidecar under `cache/`; the id appears verbatim as a `// R-4LKF-FB23`
  tag on that test.
- `R-8DF1-W89F` — the committed `etc/manifest.env` carries no `/opt/` path and no
  `GITHUB_DB_PATH=` / `GITHUB_GENERATION_PATH=` line; the id appears verbatim as a
  `// R-8DF1-W89F` tag on that test.
- `R-8IAN-FB87` — the emitted default manifest byte-equals the committed
  `etc/manifest.env`; the id appears verbatim as a `// R-8IAN-FB87` tag on that
  test.
- All three tests are genuine — each fails when its subject is broken (verify by
  temporarily perturbing the committed manifest / the emitted fields and seeing
  the corresponding test fail, then restoring). No `t.Skip`, no test that passes
  vacuously.
- The suite is green per design's *Conventions*, from `github/`:
  `GOWORK=off go build ./...` exits 0, `GOWORK=off go test ./...` passes with no
  failures and no `SKIP`, `gofmt -l .` prints nothing, `GOWORK=off go vet ./...`
  is clean.
- From `github/`, all three ids are tagged in **test** files (not merely in the
  spec) — this command prints exactly `3`:

  ```
  grep -rhoE 'R-4LKF-FB23|R-8DF1-W89F|R-8IAN-FB87' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l
  ```
