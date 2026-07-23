# Phase 16 — init-box installs the oauth CLI and the packages its installer needs

*Realizes design Decision 10 (box-baseline packages, revised) and 11 (oauth
release install via a generic InstallScript seam). No dependency on any other
pending phase — the D1–D10 work is already in the codebase.*

All work is in the `internal/opsctl` package (plus its `*_test.go` files); nothing
outside the `opsctl/` tree is touched.

## What gets built

1. **D10 — widen the init-box package baseline.** In `initbox.go`, step 1's
   `System.InstallPackages(ctx, …)` call gains `"tar"` and `"curl-minimal"` after
   the existing `nginx, certbot, poppler-utils, git, sqlite`. Update every unit
   test that pins the recorded package string to the new value
   `install-packages:nginx,certbot,poppler-utils,git,sqlite,tar,curl-minimal` —
   `initbox_test.go` (the `R-JQGB-RYA2` test, currently asserting the five-package
   string) and the three assertions in `provision_test.go`. The package literals
   `tar` and `curl-minimal` (not `curl`) are the exact strings; `curl-minimal`
   keeps the `dnf` install an idempotent no-op on AL2023 rather than a swap.

2. **D11 — the generic `InstallScript` seam.** Add to the `System` interface in
   `seam.go`:
   `InstallScript(ctx context.Context, installerURL string, env ...string) error`.
   `RealSystem` implements it by running `curl -fsSL <installerURL> | sh` with the
   given `env` (each `KEY=VALUE`) layered over the process environment. The test
   fake (`helpers_test.go`) records it as `install-script:<installerURL>` together
   with the env it was handed, mirroring how the fake records
   `install-packages:<pkgs>`.

3. **D11 — init-box calls it.** In `initbox.go`, immediately **after** the package
   install (step 1) and **before** the cert/nginx branch, add:
   `o.System.InstallScript(ctx, "https://raw.githubusercontent.com/ikigenba/oauth/main/install.sh", "BINDIR=/usr/local/bin")`,
   wrapped so a failure aborts init-box with `init-box: install oauth: %w`. Being
   above the cert branch, it runs on the `--skip-cert` path too.

## Done when

The suite is green — `GOWORK=off go build ./...` succeeds and `GOWORK=off go test
./...` passes from `opsctl/` — and the **loop-driven** ids are covered by
clearly-named, id-tagged tests:

- **R-JQGB-RYA2** — an init-box test asserts the recorded ops contain
  `install-packages:nginx,certbot,poppler-utils,git,sqlite,tar,curl-minimal`,
  including on the `--skip-cert` path. (The pre-existing test is updated to the new
  string; the three `provision_test.go` assertions are updated to match.)
- **R-ML75-3NVZ** — an init-box test asserts the recorded ops contain an
  `install-script:https://raw.githubusercontent.com/ikigenba/oauth/main/install.sh`
  entry carrying env `BINDIR=/usr/local/bin`, on **both** the default and the
  `--skip-cert` path; a variant asserting a missing call / wrong URL / absent
  `BINDIR` env would fail.

### Out-of-loop (real-substrate — not part of this phase's mechanical done-bar)

These carry live-box proof that the fake `System` cannot falsify; the operator
verifies them on `int.ikigenba.com` after this phase builds, outside the loop:

- **R-JRO8-5Q0R** (D10) — after `opsctl init-box`, `command -v git sqlite3
  pdftotext pdftoppm pdfinfo tar curl` all resolve and their `--version`/`-v`
  invocations exit cleanly; re-running init-box succeeds.
- **R-MMF1-HFMO** (D11) — after `opsctl init-box`, `command -v oauth` resolves to
  `/usr/local/bin/oauth`, the file is mode `0755`, `oauth -V` exits cleanly, and a
  re-run reinstalls the latest and still succeeds.
