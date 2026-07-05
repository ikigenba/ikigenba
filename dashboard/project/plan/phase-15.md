# Phase 15 — Telemetry data layer: the in-memory store + metric readers + service discovery

*Realizes design Decision 11 (telemetry store) and Decision 12 (metric readers +
service discovery). Depends on nothing prior — it is a new leaf package. Creates
`dashboard/internal/telemetry/` (store + readers + their tests) only; no HTTP, no
`cmd/`, no template, no schema, no migration. Confined to the dashboard tree.*

Build the new `internal/telemetry` package as the pure data layer the collector
(Phase 16) and the charts (Phase 17) sit on.

**1. The store (D11).** A `Store` holding one fixed-size ring buffer per series
key (`MaxSamples = 1440`), plus recorded `totalMem`/`totalDisk` capacities, guarded
by one mutex. `Append(key, Sample)` evicts the oldest sample past 1440;
`SetCapacities` records the totals; `Snapshot()` returns an **independent** copy
(ordered oldest→newest) of every series plus the capacities. `Sample{At, Value}`
with `Value` in bytes and `0` meaning "unavailable".

**2. The metric readers (D12), each over an injected path/root.**
`readMemInfo(io.Reader)` parses `MemAvailable`/`MemTotal` (KiB→bytes);
`readDiskFree(path)` returns free/total via `statfs`; `readCgroupMem(cgroupRoot,
svc)` reads `<cgroupRoot>/system.slice/<svc>.service/memory.current` and returns
`(0, nil)` for an absent path; `dirSize(dir)` sums regular-file sizes and returns
`(0, nil)` for an absent dir. Absent path → 0; an unexpected (permission/parse)
error → non-nil error for the collector to log later.

**3. Service discovery (D12).** `services(manifestRoot)` returns the MCP service
names via `appkit/inventory.Read` (so the dashboard, lacking `MCP=true`, is
excluded), sorted.

**4. Tests** co-located in `internal/telemetry/*_test.go`, one clearly-named test
per id, using fixture readers and `t.TempDir()` trees for the path-injected readers
and a staged temp manifest root for discovery. `readDiskFree` is exercised against
a **real** temp path (real `statfs`, not a stub).

**Done when:** the suite is green — `cd dashboard && go build ./...`, `go vet
./...`, `gofmt -l .` (no output), `go test ./...`, and `bin/check-migrations
dashboard` (from the repo root) all succeed with zero failures (design
*Conventions*) — and every id below is covered by a genuine named test:

- **R-EZVQ-IQOL** — ring caps at 1440, oldest evicted, order preserved.
- **R-F13M-WIFA** — `Snapshot` is independent of later `Append`s.
- **R-F2BJ-AA5Z** — `Snapshot` carries the recorded capacities.
- **R-F4RC-1TND** — `readMemInfo` parses MemAvailable/MemTotal, KiB→bytes.
- **R-F5Z8-FLE2** — `readDiskFree` returns real `statfs` free ≤ total, total > 0.
- **R-F774-TD4R** — `readCgroupMem` reads `memory.current` from a fixture tree.
- **R-F8F1-74VG** — `readCgroupMem` returns `(0, nil)` for an absent path.
- **R-F9MX-KWM5** — `dirSize` sums all regular-file bytes under a temp tree.
- **R-FAUT-YOCU** — `dirSize` returns `(0, nil)` for an absent dir.
- **R-FC2Q-CG3J** — `services` returns MCP names and excludes the dashboard.
