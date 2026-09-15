# D11 consumer review

## Initialize replication after nginx setup

The CLI already supplies a context, injected host environment, and the D03 store.
This fragment is a complete replication setup task; the caller reports returned
errors through its command's diagnostic policy.

```go
func setup(ctx context.Context, env host.Env) error {
    return backup.SetupReplication(ctx, env, config.Store{Root: env.Root})
}
```

## Refresh replication after an installed manifest changed

```go
func refresh(ctx context.Context, env host.Env) error {
    changed, err := backup.Regenerate(ctx, env, config.Store{Root: env.Root})
    if err != nil { return err }
    if !changed { return nil }
    result, err := env.Execute(ctx, host.Command{
        Name: "systemctl", Args: []string{"restart", "litestream.service"},
    })
    if err != nil || result.ExitCode != 0 {
        return &host.CommandError{Label: "litestream restart", Result: result, Err: err}
    }
    return nil
}
```

The D09 install command owns progress output and preserving subprocess detail;
this package task shows the exact shared API and the conditional action. A
restore instead calls Regenerate only after database restoration, while the
shared Litestream unit is stopped, then performs its own required start even if
`changed == false`. That distinction resolves the introductory unchanged-file
claim against the restore's mandatory outage; no additional restart is required.

## Identify the files owned by database replication

```go
paths := backup.DatabaseFiles(apps.Database{Engine: "sqlite", Path: "state/crm.db"})
// paths = state/crm.db, state/crm.db-wal, state/crm.db-shm,
//         state/.crm.db-litestream (entire subtree)
```

D12 uses this exclusion set for its tarball and owns the complete backup command.
A database-free manifest needs no exclusion set. Database path validation belongs
to D08; D11 does not parse the application manifest independently.

## Unresolved input adjacent to these tasks

All setup/refresh examples assume positive periods, a region, and an S3 prefix.
See [period policy](../issues/backup-replication-periods.md) for missing/zero values
and hosts whose account performs no backup.

## Correction: rejected configuration leaves replication intact

Current pre-correction contract covered unbounded positive input only. Proposed
consumer retains the same `backup.Regenerate(ctx, env, store)` call: with valid
region/prefix, each period accepts whole seconds 1 through 9223372036. The upper
bound fits a signed 64-bit nanosecond duration without arithmetic overflow.

Exercise each key with `abc`, `-1`, `1.5`, `9223372037`, and a decimal value too
large for any machine integer. Each returns `changed == false` with an error
naming the key and leaves existing litestream.yml byte-identical (or absent).
Exercise present prefixes `https://bucket/path/`, `s3:///path/`, and
`s3://bucket/path/?query=1`: each fails before writes naming backup.s3_uri.
Boundary values `1` and `9223372036` generate their exact seconds without wrap.
No default is invented. Missing/empty/zero periods and missing/empty prefixes
still require the adjacent policy decision. Fresh verification pending.
