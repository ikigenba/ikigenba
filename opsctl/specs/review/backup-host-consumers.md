# D13 consumer tasks

## Save a certificate and recover a rebuilt host

```sh
sudo opsctl host backup
# host: ok (2026-09-12T03:00:04Z.tar.zst, 48.2 KiB)
# On the rebuilt host, provision aws.region and backup.s3_uri first.
sudo opsctl host restore
# source: ok (host/2026-09-12T03:00:04Z.tar.zst, 48.2 KiB)
# files: ok (/etc/ikigenba, /etc/letsencrypt, 31 files)
sudo opsctl init
```

The first two actions exit 0 with empty stderr in this successful fixture. Restore
uses the source prefix and region captured before replacing the configuration
store, even when the restored keys differ. It restores manually entered keys and
certificate symlinks as well as regular files; stale destination entries disappear.
It leaves apps, units, nginx and the cloud objects alone. Init is the distinct
operation that applies the recovered configuration. Displayed bytes, counts and
times are illustrative fixtures, not observed live backup results.

## Package task: recover host data and expose completed phases

```go
func recoverHost(ctx context.Context, env host.Env, remote cloud.Env, out io.Writer) error {
    result, err := backup.HostRestore(ctx, env, remote, config.Store{Root: env.Root})
    if result.SourceReady {
        fmt.Fprintf(out, "source: ok (%s, %.1f KiB)\n", result.Object, float64(result.Size)/1024)
    }
    if result.FilesRestored {
        fmt.Fprintf(out, "files: ok (/etc/ikigenba, /etc/letsencrypt, %d files)\n", result.Files)
    }
    return err
}
```

D13 declares HostRestore and HostRestoreResult; D01 declares host.Env and
cloud.Env; D03 declares config.Store. io.Writer, context.Context and fmt belong
to the standard library. The CLI maps the returned operational error to the D02
diagnostic and exit 1, retaining completed rows. Malformed archives fail before
either tree changes. Replacement failure can leave completed replacement effects;
this interface does not claim a cross-directory filesystem transaction.

## Stop and preserve a host before an external destroy

```sh
sudo opsctl retire
# services: ok (crm, dashboard stopped)
# litestream: ok (stopped, crm.db synced)
# crm: ok (2026-09-12T14:22:51Z.tar.zst, 1.2 MiB)
# dashboard: ok (2026-09-12T14:22:51Z.tar.zst, 1.1 MiB)
# host: ok (2026-09-12T14:22:51Z.tar.zst, 48.2 KiB)
```

This is the ordinary path conditional on established successful final sync. The
protocol establishing that fact and timeout behavior remain unresolved in
`../issues/retire-final-database-guarantee.md`. A zero exit from systemctl alone
cannot establish it. The snippet is consequently a target consumer interaction,
not a complete verified destroy authorization procedure. The external caller
owns termination; opsctl never invokes it.

A failed crm archive read replaces its row with
`crm: failed: /opt/crm/state/outbox: permission denied`, still writes dashboard
and host, exits 1, and leaves stderr empty. All units remain stopped, existing
objects survive, and the database replica was current before file backup began
on this conditional path. All three successful objects share a single instant,
including fractional seconds where present. No per-object timestamp adjustment
can hide a collision. Keeping the host instead permits explicit systemctl starts
or a reboot; retirement has not disabled units or timers.

The package consumer calls
`backup.Retire(ctx, env, remote, config.Store{Root: env.Root})` and renders
RetireResult: ServicesStopped/Services gate the services row, LitestreamStopped
and SyncedDatabases gate the completed successful synchronization row, Files
supplies D12 rows and Host supplies the final KiB row. It uses each archive Err
for the exit status and the separate error only for operational diagnostics.
D13 declares all these names except FileResult (D12). SyncedDatabases is evidence
of synchronization, never a list inferred solely from manifests after unit stop.
Missing synchronization evidence remains beside this usage as an unresolved API
behavior, rather than being supplied by an invented external probe.

## Help and invalid grammar without effects

```sh
opsctl host --help
opsctl host -h
opsctl retire --help
opsctl retire -h
sudo opsctl host
# stderr: opsctl: no host subcommand given
#
# see 'opsctl host --help' for usage
sudo opsctl host status
# stderr: opsctl: unknown host subcommand 'status'
#
# see 'opsctl host --help' for usage
```

The help tasks work for any uid, exit 0, print exact story text on stdout and
leave stderr empty. The invalid host tasks exit 2 with empty stdout. All leave
host state untouched. A valid action from a non-root caller uses D02 exit 3 and
never reads or changes host state.

## Dependency evidence and limits

`environment-observations.md` records real synthetic tar --zstd/--xz creation and
extraction on dev with matching bytes and cleaned temporary fixtures. This
supports the archive format, not a platform backup or live S3 operation. The
cloud adapter and live external operation observations remain tracked centrally
in `../issues/cloud-adapter-approval.md` and `../issues/external-observations.md`.
Litestream final-sync observation is absent. No AWS SDK dependency or AWS CLI
installation is proposed by this scope.
