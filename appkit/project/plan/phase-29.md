# Phase 29 — Retire the manifest verb; one version channel

*Realizes the umbrella's R-8EMY-A004 (`root project/design/D11.md`,
`[proof: appkit]`) and the D18 version self-report as rewritten.*

The chassis verb surface and version self-report are brought into conformance
with the umbrella contracts, in the appkit root package:

- The verb dispatcher (`appkit.go`) loses its `manifest` case and the
  `runManifest` wiring: the dispatched set is exactly
  `serve`/`version`/`migrate`/`schema`, invoking `manifest` is the unknown-verb
  error (whose message lists only the four verbs), and `appkit/manifest.Emit`
  remains callable as a library function (the drift test and `CONSUMES=`
  derivation keep using it).
- The existing test tagged `R-8EMY-A004`
  (`TestDispatch_ReducedVerbSetAndManifestLibrary`) currently asserts
  `manifest` exits `0` — the opposite of the behavior the id names. It is
  rewritten to assert `manifest` is an unknown-verb error (exit `2`, stderr
  naming the four-verb set) and that `manifest.Emit` is still callable
  directly.
- The dead second version channel is deleted: the package-level `commit` var,
  the `(<commit>)` rendering in `versionString()` (it returns `version`
  verbatim), and the stale comments describing a ship-injected commit stamp
  (`appkit.go`, the `versionString` doc comment). The SHA already travels
  inside the stamped version identity (`+<sha>` build metadata).

**Done when:**

- R-8EMY-A004 — the tagged dispatch test asserts `manifest` (and no other
  spelling of it) is an unknown-verb error while `serve`/`version`/`migrate`/
  `schema` remain dispatched, and `manifest.Emit` is callable as a library
  function.
- Run from the appkit tree root: `grep -rn 'case "manifest"' --include='*.go' --exclude-dir=project .` returns empty (exit 1), and `grep -rn 'appkit\.commit' --include='*.go' --exclude-dir=project .` returns empty (exit 1).
- `GOWORK=off go build ./...` and `go test ./...` in `appkit/` exit 0.
