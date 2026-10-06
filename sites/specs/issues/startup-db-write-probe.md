# Startup file restriction conflicts with required db.Open

Filed during the human-invoked sites build-spec run starting at `02f7959d44321298366569051965a9f233befb9b`. The CLI implementation leaf reported the conflict, and fresh verifier `/root/build/wiring/validate_probe` independently confirmed it.

In `specs/design/D03-serve.md:121`, R-XSVP-THZJ restricts what `Run` may create, remove or change under `p.Dir` between being called and accepting its first connection. The allowed entries include the declared state/cache directories, catalog and migration state, and catalog sidecars ending in `-journal`, `-wal` or `-shm`. R-WZM4-N06V (`D03-serve.md:126`), R-WUQJ-3X83 (`D01-layout-and-run-seam.md:100`) and R-XKCF-53SO (`D03-serve.md:119`) require startup to use appkit's `db.Open` for the catalog at the declared location.

The required published appkit release's `db.Open` unconditionally calls `os.CreateTemp(filepath.Dir(path), ".db-write-")`, then removes the resulting file before opening SQLite. Its configuration offers `Path`, `Migrations` and `Now`, with no option to disable or relocate that probe. Thus a successful startup creates and removes an additional entry under `state/` during the restricted interval. This file is not an allowed SQLite sidecar.

Evidence: a temporary-directory CLI `Run` test observing filesystem events before the first connection failed with `go test ./internal/cli -run '^TestRunStartupCreatesOnlyDeclaredEntries$' -count=1`, exit 1. The independent validator reproduced create mask `0x100` and delete mask `0x200` for `state/.db-write-949809242`. Inspection of the published dependency's `db/db.go` lines 68–74 confirmed the unconditional probe. The diagnostic test was removed after recording this issue; R-XSVP-THZJ is not claimed as covered.

There is no compliant sites-only implementation: bypassing `db.Open`, opening elsewhere or modifying the released library would violate the required startup path or dependency boundary. Suggested resolution: permit appkit's temporary write probe during startup and replace the changed requirement with a newly minted id, or change and publish appkit first to provide a compatible open path.
