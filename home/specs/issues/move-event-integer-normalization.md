# Move event integer normalization

Filed during the user-authorized `build-spec` run for home, starting at commit `608e923eb77ae88046535f154255b04a4f0e1cc9`. The pages implementer reported this blocker, and a fresh verifier independently confirmed it through the published dependency's public API.

Requirements: R-9M0S-4IBH and R-9OGK-W1SV in `specs/design/D07-bookmarks-page.md`.

Both requirements prescribe concrete Go `int` values for the `from` and `to` attributes of `bookmark.moved`. D07's recorded decision explicitly says that “the Go type of a value is what a capture sink observes”. The required published appkit release normalizes every signed integer attribute to `int64` before delivery to the capture sink, so the prescribed observable type cannot be produced through home's required telemetry seam.

The implementer's inspection and the independent verifier's inspection agree: appkit's `telemetry/writer.go` forms each emitted event through `basicAttribute`; `telemetry/event.go` converts signed integer kinds with `reflect.Value.Int()`, which returns `int64`; `telemetry.Capture` records that normalized event. R-JGVH-B9PQ fixes home's telemetry configuration as `*telemetry.Writer`, so home cannot substitute an emitter that preserves `int`.

The verifier ran `go run` on a temporary Go probe outside the repository, using the home module's required appkit dependency, a fixed clock, `telemetry.New`, `telemetry.Capture`, an emit, a flush, and a shutdown. The command exited 0 and reported:

```text
Emit input: from int(1), to int(0)
Capture event bookmark.moved: from int64(1), to int64(0)
D07 concrete int type assertions: from=false to=false
```

The verifier also confirmed the required appkit release and its checksum, `h1:JMf8AhKdUM1e42aw0trE+ezorI0C37Zeh0XPxreEL78=`, through `go list -m -json`. The probe was removed, and its writer shut down.

Passing `int` into `Emit` does not meet D07's explicit captured-type expectation. A test through the permitted concrete writer cannot distinguish signed input types because they all normalize to `int64`. Editing the read-only design, changing the required dependency, or bypassing the prescribed telemetry writer is outside this build run's authority. These two ids remain untagged; independent move-route and other bookmark work can still be completed and verified.

Suggested resolution outside this build: replace and re-mint the two requirements to specify captured `int64` values, or signed integer values without prescribing the original Go type, and correct the recorded decision. Alternatively, authorize and publish an appkit change that preserves the required concrete type before updating home's dependency contract.
