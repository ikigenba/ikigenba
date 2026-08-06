# Phase 56 — Take the agentkit version out of the import guards, and move the pin

*Realizes design Decision 1 (module dependency) — structural.*

D1 now owns the *shape* of the agentkit dependency and never its value: published module, exact released pin, no local fork, with `prompts/go.mod` the sole authority on which version. Two guard tests still contradict that by hardcoding a version string, which is the same duplicated-fact problem the design just shed. This phase removes the last copies and moves the pin in the one place that owns it.

What exists at the end of this phase:

- `prompts/agentkit_import_guard_test.go` no longer declares a wanted version. It asserts that `go.mod` requires `github.com/ikigenba/agentkit`, that the requirement is an **exact released tag** (valid semver and not a pseudo-version, per D1's rejection of floating requirements), and that no local `agentkit` module path or `./agentkit`/`../agentkit` replace survives. `golang.org/x/mod/semver` and `golang.org/x/mod/module` supply the version-shape checks; `golang.org/x/mod` is already a dependency.
- `prompts/module_import_guard_test.go` likewise drops its version constant. Its `go.sum` assertion reads the required version **from the parsed `go.mod`** instead of naming one, so it keeps proving the sum file covers the pinned module without knowing which release that is.
- The legacy `go.sum` sentinels in that test (`agentkit v0.0.0`, and the retired fork's `v0.1.0`/`v0.1.1` entries) are deleted. They are version literals, and the guarantee they were protecting — no dependency on a retired local agentkit fork — is already carried by the module-path and replace-directive checks in both files, which do not name versions.
- `prompts/go.mod` requires `github.com/ikigenba/agentkit v0.17.0`, with `go.sum` updated to match. This is the last time a phase carries an agentkit version: once the guards stop hardcoding one, moving the pin is a `go.mod` edit proven by a green suite, not spec work.

Note for the builder: v0.17.0 changes runtime behavior without changing any API. Tool arguments are now validated against each tool's declared schema before dispatch, and a violation comes back to the model as an error result rather than reaching the tool. Nothing prompts consumes changes shape, so this should be a clean compile, but a test that feeds a tool arguments its schema does not permit will now see a refusal where it previously saw a call.

**Done when:** `GOWORK=off go build ./...` and `GOWORK=off go test ./...` both exit 0 with no failing package from `prompts/`, `gofmt -l .` emits no output, and

```
grep -nE 'v[0-9]+\.[0-9]+\.[0-9]+' agentkit_import_guard_test.go module_import_guard_test.go
```

produces **no output**, proving no agentkit version literal survives in either guard.
