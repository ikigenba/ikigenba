# Service restore consumer exercise

## Current and proposed use

Current implementation has no `internal/backup` package and no restore command.
The earlier draft called `backup.Restore(ctx, env, objects, store, "crm", &target)`.
The corrected API adds the required nginx regeneration callback below; the CLI
interaction remains one invocation:

```console
$ sudo opsctl restore crm --at 2026-09-11T18:00:00Z
source: ok (crm/2026-09-11T03:00:02Z.tar.zst, 1.2 MiB)
stop: ok (ikigenba-crm.service, litestream.service)
files: ok (/opt/crm/etc, /opt/crm/state, 12 files)
db: ok (/opt/crm/state/crm.db, at 2026-09-11T18:00:00Z)
litestream: ok (unchanged)
start: ok (litestream.service, ikigenba-crm.service)
```

`--at` reports the target: in a replication gap the actual recovered database
can precede that target. Newest mode instead reports the actual recovered point.
Neither chooses the tarball timestamp as its database target.

## Domain caller: complete task and inspect a partial failure

Inside this Go module, a caller with a D01 injected host and cloud environment
and a D03 store can restore and inspect the complete result without CLI imports:

```go
package consumer

import (
    "context"
    "errors"
    "fmt"
    "io"
    "time"

    "github.com/ikigenba/ikigenba/opsctl/internal/backup"
    "github.com/ikigenba/ikigenba/opsctl/internal/cloud"
    "github.com/ikigenba/ikigenba/opsctl/internal/config"
    "github.com/ikigenba/ikigenba/opsctl/internal/host"
    "github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

func recoverCRM(ctx context.Context, env host.Env, objects cloud.Env,
    store config.Store, out io.Writer) error {
    target := time.Date(2026, 9, 11, 18, 0, 0, 0, time.UTC)
    hostName, err := store.Get("host.name")
    if err != nil {
        return err
    }
    if hostName == "" {
        return errors.New("host.name not set")
    }
    regenerateNginx := backup.NginxRegenerator(func(ctx context.Context) error {
        return nginx.Write(ctx, env, hostName)
    })
    report, err := backup.Restore(ctx, env, objects, store, "crm", &target, regenerateNginx)
    for _, step := range report.Steps {
        fmt.Fprintf(out, "%s: ok (%s)\n", step.Name, step.Detail)
    }
    var failure *backup.RestoreError
    if errors.As(err, &failure) {
        // A caller can inspect recovery state without parsing Error().
        fmt.Fprintf(out, "failed stage: %s; stopped: %v\n", failure.Stage, failure.Stopped)
    }
    return err
}
```

The supplied host root is temporary in gate consumers; process and cloud
fixtures implement D01. This example is a review exercise, not a test or a
claim of external integration readiness. `Restore`, `RestoreReport`,
`RestoreStep`, `RestoreError`, their fields and error methods are all declared
in D14; input types are declared in D01/D03. `NginxRegenerator` is declared by R-1NHS-71WK; `nginx.Write` by D06
R-QC9K-4BIS. This orchestration belongs to `internal/cli`, whose D01
import permissions include both packages; `internal/backup` does not import nginx.
The callback writes the rendered nginx configuration after restored data and
any Litestream regeneration are in place, before either unit starts. It
does not execute nginx, reload it, or add stdout steps.

## Distinct user tasks

| Task and invocation | Complete observable result |
|---|---|
| Running service, `sudo opsctl restore crm` | Source, stop, files, newest database, regeneration, start; both running; code untouched |
| Already deliberately stopped, same invocation | Stop reports already inactive; start reports left inactive; Litestream running |
| Service never installed, same invocation | Stop reports no app unit; restores etc/state, regenerates incoming database, starts only Litestream; status can report `crm - - wal` under lifecycle design |
| Reserved service name, `sudo opsctl restore backup-host` with a valid data-only archive | Restores selected service trees; never queries/stops/starts `ikigenba-backup-host.service`; nginx regenerates before completion |
| Nginx callback failure after restoration | Retains completed steps, returns stage `nginx regeneration`, leaves stopped units stopped with diagnostic; no starts, previous nginx bytes or absence preserved by Write |
| No database before or after, `sudo opsctl restore dashboard` | Source, stop, files, nginx regeneration without a report step, start; no database or replication steps; other replication uninterrupted |
| No files before target, `sudo opsctl restore crm --at 2026-08-01T00:00:00Z` | Empty stdout, exit 1, `opsctl: crm: no backup at or before 2026-08-01T00:00:00Z`; no /opt reads or unit changes |
| No backups, `sudo opsctl restore gmail` | Empty stdout, exit 1, `opsctl: no backups for gmail under <configured-prefix>`; no service created |
| Files but no snapshot, `sudo opsctl restore crm` | Three completed stdout steps, exit 1; stderr identifies no snapshot and both units stopped; files retained and config unchanged |
| Help, either `opsctl restore --help` or `opsctl restore -h` | Story help with the inherited host.name key added, stdout only, exit 0, works without root |
| Missing/multiple SERVICE or invalid --at | Empty stdout, exit 2, exact grammar reason and help hint; no effects |

### Unresolved directly beside affected tasks

- Retrying the missing-snapshot task after supplying the replica: backup story
  lines 1014–1016 promises both units start, while 902–936 promises an inactive
  app stays inactive. The CLI has no option distinguishing intentional stop
  from a previous failure, and no persistence contract records that intent.
  User must choose retry behavior or authorize a distinguishable recovery state.
  D14 does not select arbitrary retry semantics.
- Restoring an incoming manifest without a database over an installed database:
  backup lines 862–898 says no Litestream stop/regeneration for no database,
  while 65–71 requires stopping every holder before replacing state and
  151–153 says manifest changes regenerate. The guaranteed no-database branch
  in D14 is only the case where both manifests have no database. Safety remains
  required; reporting/config reconciliation for database removal awaits user
  decision. Incoming database over no prior service is fully specified.

## Evidence and limits

D12's archive roundtrip and D01 cloud/host seams support local deterministic
consumers. The designated host lacks Litestream (environment-observations.md):
actual restore command grammar, newest-point discovery, no-snapshot diagnosis,
WAL mode after recovery, gap selection and reconciliation with newer remote
history remain unobserved, as do real S3 role permissions. They must be proven
before check-spec; no live tool capability is claimed by these examples.
Regenerate's zero/unset replication-period issue also affects restore. No
migration or seed tool is invoked: apps.md:75–82 assigns those to app startup.

## Source reconciliation for this correction

`init.md:28–32` requires both files affected by `/opt` to regenerate on restore;
`backup.md:778` prohibits nginx reload, which Write does not perform.
`backup.md:689–717` supplies exact help bytes, including `SERVICE's`; the
added `host.name` help row follows `stories/README.md:34–37`, which requires
every read configuration key even when declared by another story group.
This addition is derived from that convention and the required nginx input,
not an assertion that the source help originally contained the row.
The listed lifecycle and replication-period issues remain unresolved.
