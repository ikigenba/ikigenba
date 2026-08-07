# Phase 28 — Telemetry tag realignment: split R-1GXN-BPJP, drop the two feed_test.go mistags

*Realizes design Decision 15 (recorder), slice R-PTNV-XRYV, plus structural tag
hygiene for Decision 18's ids. No dependencies.*

Three tag corrections, no behavior change and no new test logic:

1. **`telemetry/recorder_test.go`** — `TestRecorderDisabledPerformsNoRequests`
   (~line 360) currently carries `// R-1GXN-BPJP`. D15 split that compound id:
   R-1GXN-BPJP now covers config resolution only (its home stays
   `config/config_test.go` `TestResolve_TelemetrySuiteWideDefaultsAndOverrides`),
   and the disabled-recorder clause is the new **R-PTNV-XRYV**. Replace the tag
   comment with `// R-PTNV-XRYV`. The test already asserts exactly that behavior
   (5000 records enqueued against a live sink, zero requests observed).
2. **`feed/feed_test.go`** — delete the stray `// R-1Z85-29O4` (~line 246) and
   `// R-20G1-G1ET` (~line 250) tag comments in
   `TestStartProducerAndLiveConsumerObserveCorrelatedHops`. D18's two ids
   describe **telemetry records** (kind `publish`/`consume`); their proofs of
   record are `appkit_test.go` (`TestRunServeRecordsLifecycleStartAndStop`,
   R-1Z85-29O4) and `consumers_test.go`
   (`TestConsumerWorkerRecordsRootsOnlyForUnusableWireCorrelations`,
   R-20G1-G1ET). The feed test asserts `observe.Event` hook hops, a different
   behavior; the test itself stays exactly as it is, untagged.

**Done when:**

- `grep -rn 'R-PTNV-XRYV' --include='*_test.go' .` from the appkit root prints
  exactly one line, in `telemetry/recorder_test.go` on
  `TestRecorderDisabledPerformsNoRequests` — the recorder constructed with
  `Enabled: false` performs zero requests against a live sink.
- `grep -rn 'R-1GXN-BPJP' --include='*_test.go' .` prints exactly one line, in
  `config/config_test.go`.
- `grep -rn 'R-1Z85-29O4' --include='*_test.go' .` prints exactly one line, in
  `appkit_test.go`; `grep -rn 'R-20G1-G1ET' --include='*_test.go' .` prints
  exactly one line, in `consumers_test.go`.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` silent, `go test ./...` all passing, from `appkit/`).
