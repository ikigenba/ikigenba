# App lifecycle consumer tasks

These are proposed public consumers; `internal/apps` does not yet exist in the
implementation. No build or execution of these sketches was performed.

## Remove an installed app and retain its data

The CLI obtains `host.name` through `config.Store{Root: deps.Root}.Get`,
constructs a `host.Env` from the supplied D01 dependencies, and calls:

```go
err := apps.Uninstall(ctx, env, "crm", apps.UninstallHooks{
    Report: report, // writes step + ": ok (" + detail + ")\n"
    Configure: func(ctx context.Context, removed apps.Manifest) error {
        if err := nginx.Apply(ctx, env, hostName); err != nil { return err }
        names := removed.App + "." + hostName
        if removed.Default { names += ", " + hostName }
        if err := report("nginx", names + " removed"); err != nil { return err }
        changed, err := backup.Regenerate(ctx, env, store)
        if err != nil { return err }
        detail := "unchanged"
        if changed {
            result, err := env.Execute(ctx, host.Command{
                Name: "systemctl", Args: []string{"restart", "litestream.service"},
            })
            if err != nil || result.ExitCode != 0 {
                return &host.CommandError{Label: "systemctl restart litestream.service", Result: result, Err: err}
            }
            detail = "updated"
            if removed.Database != nil { detail = removed.Database.Path + " removed" }
        }
        return report("litestream", detail)
    },
})
```

On success the caller has five ordered lines, the state tree survives, routing
has been removed and changed replication configuration restarted. A default
without a database reports both removed names and unchanged Litestream. The
CLI renders LifecycleError using D02. A later `Status` includes `crm - - -`;
a later `Install` can reuse the retained state. Absent and data-only services
fail before modification. Observe no cloud calls and preserve other apps.

## Restart using the installed secret environment

```go
row, err := apps.Restart(ctx, env, "crm")
if err == nil {
    _, err = fmt.Fprintf(stdout, "service: ok (%s %s %s)\n", row.Name, row.Version, row.State)
}
```

Successful restart uses the existing unit/env, works for active, inactive and
failed starting states, and leaves nginx/Litestream untouched. Failure returns
LifecycleError carrying the start failure and journal detail; CLI writes that
diagnostic only and exits 1. A subsequent Status retains the failed unit fact.
The install-output-conflicts issue continues to track the source's claim of
equality with four-column status; the provided three-value restart example is
preserved.

## Report the host, including restored data and replication mode

```go
rows, err := apps.Status(ctx, env)
if err == nil {
    for _, row := range rows {
        if _, err = fmt.Fprintf(stdout, "%s %s %s %s\n", row.Name, row.Version, row.State, row.JournalMode); err != nil { break }
    }
}
```

The complete action returns 0 after successful rendering even when rows include
`failed`, `delete` or `-`; only enumeration or output failure makes the command
fail. With crm plus restored gmail the product is `crm v0.1.0 active wal` and
`gmail - - -`, one LF per row. An empty host prints nothing. The field result
is independent of other fields' query failures. Temporary-root process
fixtures assert all process paths stay rooted and no service control occurs;
file fixtures exercise valid WAL/rollback format, missing and malformed DBs,
without installing sqlite3 or adding a Go dependency.

## Help and argument refusal

Call `cli.Run([]string{command, "--help"}, ...)` and its `-h` variant for each
command with nonroot EUID and deliberately failing host/cloud dependencies:
expect the source help bytes, exit 0, empty stderr and zero dependency calls.
Root invocations without APP or with multiple APP values use the exact D10
usage diagnostics; status rejects an operand before discovery. D02 supplies
nonroot action refusal independently of these operation contracts.
