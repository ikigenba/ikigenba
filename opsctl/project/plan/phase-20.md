# Phase 20 — Stage preflight off the manifest verb; delete the dead commit channel

*Realizes design Decision 16 (stage preflight without the retired manifest
verb; one version channel).*

In `internal/opsctl`: preflight's app-identity check moves off the binary's
`manifest` verb onto the bundle's own unpacked `etc/<version>/manifest.env`
(`APP=` must equal the app argument; no `manifest` invocation remains in
preflight). `commitToken` and its call sites are deleted: the stage collision
guard compares staged-vs-incoming artifact bytes (content hash), and `status`
drops its commit column. Existing D15 stage tests (`R-84VR-7U2K` and the
fakeapp/fakeRunner fixtures) are updated to the new mechanisms — the fake no
longer needs to answer a `manifest` verb.

**Done when:**

- R-TA75-P0NF — a test tagged with this id proves the APP-mismatch refusal,
  the APP-match acceptance, and that the `AppRunner` seam records no
  `manifest` invocation in either case.
- R-TBF2-2SE4 — a test tagged with this id proves byte-identical re-stage is
  an idempotent no-op and byte-differing re-stage is refused without
  `--force`.
- Run from the opsctl tree root:
  `grep -rn 'commitToken' --include='*.go' --exclude-dir=project .` returns
  empty (exit 1), and
  `grep -rn '"manifest"' internal/opsctl --include='*.go' | grep -v testdata`
  returns empty (exit 1).
- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0.
