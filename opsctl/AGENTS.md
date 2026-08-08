# opsctl

The on-box CLI for the ikigenba suite: one privileged binary (module `opsctl`),
invoked as `sudo opsctl <verb>` by an operator over SSH, that owns every box-side
operation (stage, deploy, rollback, prune, status, backup/restore, and box/service
provisioning). It is built in this repo and installed by hand to
`/usr/local/bin/opsctl`. It is tooling, not a deployable service and not an appkit
binary: it is the consumer of the appkit binaries the services produce.

## How changes are made

Changes go through the spec under `project/`, not direct edits — settle the
spec, then let the build loop realize it. The spec itself is direction-gated:
`project/**` is written only inside an operator-invoked move (the `$open-spec`
→ `$grill-me` → `$seal-spec` arc, or the build loop's completion mutations).
In any other session `project/` is read-only reference — a stale or wrong spec
is a finding to report, not a license to edit, and a settled discussion is not
direction: say what should change and wait. Edit code directly only on
explicit operator instruction. See the `$ikispec` skill for the `project/`
spec contracts and `$ralph` for the unattended build workflow.

## Layout

- `cmd/opsctl/main.go`: the CLI front end. Flag/positional normalization, the
  grouped `--help` verb registry, and the dispatch table.
- `internal/opsctl`: the engine, one file per concern (deploy/stage/rollback/prune,
  backup/restore, setup/init-box/teardown/convert, status/ops, the on-box layout
  model, and the `System`/`AppRunner` seams faked in tests).
- `project/`: the spec the build loop works from.

## Tests

The default gate is `GOWORK=off go test ./...`, run from `opsctl/`. The production
build and the test gate both use `GOWORK=off`; build separately with
`GOWORK=off go build ./...`.

This tree has exactly two testing layers:

- **Hermetic:** tests use temp-dir filesystems, real archives through the real
  `tar` binary, and faked privilege seams.
- **Manual:** privileged checks that require the real box are recorded in
  `project/opsctl-verification.md` and run by an operator outside the gate.

There is no composed layer and no live layer. In particular, this tree has no
`//go:build live` files and no `go test -tags live` invocation. Beyond the Go
toolchain, the hermetic gate requires a real `tar` binary on `PATH`; its absence
is a hard failure, never a skip.

## Versioning

Not versioned. opsctl is on-box tooling built within the mono-repo, with no
`VERSION` file and no git tag; only the deployable apps carry a `<app>/VERSION`.
