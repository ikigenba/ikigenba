# opsctl

The on-box CLI for the ikigenba suite: one privileged binary (module `opsctl`),
invoked as `sudo opsctl <verb>` by an operator over SSH, that owns every box-side
operation (stage, deploy, rollback, prune, status, backup/restore, and box/service
provisioning). It is built in this repo and installed by hand to
`/usr/local/bin/opsctl`. It is tooling, not a deployable service and not an appkit
binary: it is the consumer of the appkit binaries the services produce.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/opsctl/main.go`: the CLI front end. Flag/positional normalization, the
  grouped `--help` verb registry, and the dispatch table.
- `internal/opsctl`: the engine, one file per concern (deploy/stage/rollback/prune,
  backup/restore, setup/init-box/teardown/convert, status/ops, the on-box layout
  model, and the `System`/`AppRunner` seams faked in tests).
- `project/`: the spec the build loop works from.

## Tests

- `GOWORK=off go build ./...`
- `GOWORK=off go test ./...`

Unit tests run against temp dirs via faked seams; claims needing the real box carry
a live-box verification id checked out of loop.

## Versioning

Not versioned. opsctl is on-box tooling built within the mono-repo, with no
`VERSION` file and no git tag; only the deployable apps carry a `<app>/VERSION`.
