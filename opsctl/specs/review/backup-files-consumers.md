# D12 consumer tasks

## Preserve all service files before an operator change

```sh
sudo opsctl backup
# crm: ok (2026-09-12T03:00:04Z.tar.zst, 1.2 MiB)
# dashboard: ok (2026-09-12T03:00:04Z.tar.zst, 1.1 MiB)
sudo opsctl backup crm
# crm: ok (2026-09-12T14:22:51Z.tar.zst, 1.2 MiB)
```

These are illustrative fixture sizes/times, not live observations. Both complete
with exit 0 and empty stderr. crm excludes its declared database and companions;
dashboard keeps ordinary state. A failed crm file read instead prints the failure
row on stdout, still uploads dashboard, and exits 1 with empty stderr. Missing
gmail is an operation failure on stderr with empty stdout. Missing backup prefix
or region fails before service or object reads. Help is available without root.

## Change a schedule and retain manual backup capability

```sh
sudo opsctl config set backup.host_files_seconds=3600
sudo opsctl config set backup.service_files_seconds=0
sudo opsctl init
systemctl is-enabled ikigenba-backup-services.timer
# disabled (exit 1)
sudo systemctl start ikigenba-backup-services.service
```

Init leaves host backup scheduled hourly, service backup disabled/stopped, and
renewal enabled/active. Manual service activation still runs the root backup
command. A subsequent positive service period and init re-enable that timer.

## Package consumer: complete timer setup step

```go
func setupTimers(ctx context.Context, env host.Env) error {
    return backup.SetupTimers(ctx, env, config.Store{Root: env.Root})
}
```

Declarations: D01 owns host.Env; D03 owns config.Store; D12 owns SetupTimers.
This setup task uses no cloud client and initiates no backup. The caller reports
its returned error under the init timers step using D01's command-error boundary.

## Interpretation and pending evidence

The service archive includes `etc/env`: backup's explicit complete `etc/` source
set and restore's replace-then-restart behavior require retaining the existing
unit's EnvironmentFile. The help's general generated-file exclusion refers to
the separately identified nginx and unit outputs outside those source trees.
This is a technical reading of the stories, not a new user approval; integration
verification must assess it alongside apps install and restore.

Systemd and certbot grammar has read-only live help evidence in
`webhost-live-help.txt`. `environment-observations.md` additionally records live
`/usr/bin/zstd` and GNU tar `--zstd`/`--xz` capability; no archive roundtrip was
observed. Generated unit acceptance, observed timer enabling and
masking behavior, and archive compressor compatibility still need real-host
observations before check-spec, tracked centrally in `../issues/external-observations.md`. Cloud adapter approval/evidence remains tracked
in `../issues/cloud-adapter-approval.md`; no AWS CLI or module is added here.

## Package consumer: backup a service and report every finding

```go
func preserve(ctx context.Context, env host.Env, remote cloud.Env, out io.Writer) error {
    results, err := backup.Files(ctx, env, remote, config.Store{Root: env.Root}, "crm")
    if err != nil { return err }
    failed := false
    for _, result := range results {
        if result.Err != nil {
            fmt.Fprintf(out, "%s: failed: %s\n", result.Service, result.Err)
            failed = true
        } else {
            fmt.Fprintf(out, "%s: ok (%s, %.1f MiB)\n", result.Service, result.Object, float64(result.Size)/1048576)
        }
    }
    if failed { return fmt.Errorf("service backup report contains a failure") }
    return nil
}
```

The complete task reports service findings and returns an aggregate status to its
caller; the CLI consumer uses the status for exit 1 without copying findings to
stderr. D12 declares Files and FileResult; D01 declares cloud.Env/host.Env; D03
declares config.Store. Empty service selects all. Retirement can copy host.Env
and set Now to a closure returning one captured time before invoking file and
host backup, avoiding a second clock read for the shared run timestamp.

Repeated ordinary runs use distinct RFC3339Nano clock values even within one
second. A colliding clock value is an upload failure, never permission to replace
an object; retirement does not advance individual service timestamps independently.

## Correction: reserved namespace discovered alongside ordinary services

Before correction, all-service backup could not both read every manifest through
Discover and reject a reserved service before any service access. Proposed:

```sh
sudo opsctl backup
# Fixture includes crm and host as D08 service directories.
# crm: ok (2026-09-12T03:00:04Z.tar.zst, 1.2 MiB)
# host: failed: <reserved-service reason>
sudo opsctl backup host
# Operational diagnostic on stderr, empty stdout, exit 1.
```

The all-service task may read host/etc/manifest.toml during discovery, then
returns a failed host result without archive construction, further host-service
data reads or access to its cloud prefix; crm still succeeds and the report
exits 1 with empty stderr. Repeat with deploy. Explicit reserved operands fail
before service access. An ordinary service directory name outside install's
name grammar remains selectable under D08. Reason placeholders above illustrate
unspecified text, not mandated output bytes. Fresh verification pending.
