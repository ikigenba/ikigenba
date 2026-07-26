# registry

The shared port-registry library for the ikigenba suite: one small,
dependency-free Go module (module path `registry`) holding the authoritative
`service name → loopback port` table. Callers resolve a service by name
(`registry.MustPort("crm")`, `registry.BaseURL("crm")`), so each port literal is
written down in exactly one place. It is a leaf library (standard library only, so
even `opsctl` can adopt it without the appkit chassis), not a deployable service.

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

- `registry.go`: the whole library. The `Service`/`Block` types, the frozen
  `Services` table, and the `Port`/`MustPort`/`BaseURL` lookups.
- `registry_test.go`: guardrail tests (unique names/ports, ports within their
  block range, `dashboard` pinned to `3000`).
- `doc.go`: the package doc.
- `project/`: the spec the build loop works from.

## Tests

- `GOWORK=off go build ./...`
- `GOWORK=off go test ./...`

`GOWORK=off` matches the deterministic prod build and proves the module resolves
standalone.

## Versioning

Not versioned. registry is a shared library consumed within the mono-repo, with no
`VERSION` file, no git tag, and no deploy.
