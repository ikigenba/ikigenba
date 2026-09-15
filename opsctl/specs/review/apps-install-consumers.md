# Install consumer tasks (author review; verifier pending)

## Operator task

Current target before this draft has no install operation. Proposed complete task:

```sh
opsctl install --help
sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.1.0.tar.xz
sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.2.0.tar.xz
sudo opsctl status
```

First install obtains artifact and named secrets using the host role, publishes
files and the nonroot unit, configures routing and replication, then starts the
app. Upgrade preserves state/cache, restarts the active app and leaves unchanged
replication alone. Status belongs to D10 and reports the live binary and unit.

**Unresolved beside this usage:** [output conflicts](../issues/app-install-output-conflicts.md)
blocks fetch-line omission/presence for upgrade/default/service failure and the
claim that install's last line equals status. Literal service examples are kept;
no equality guarantee is authored. No seven-line upgrade promise is chosen.

## CLI package consumer

Current public API: D08 supplies discovery and decoding; no installation API.
Proposed API adds one meaningful operation with report and integration hooks.
The CLI adapter below performs the complete installation composition. `ctx`,
`env`, `remote`, `store`, `uri`, `stdout` are the application's already validated
context, `host.Env`, `cloud.Env`, `config.Store`, URI and output writer. Error
rendering uses `InstallError` and D02; application calls `cli.Run` normally.

```go
report := func(step, detail string) error {
    _, err := fmt.Fprintf(stdout, "%s: ok (%s)\n", step, detail)
    return err
}
hostName, err := store.Get("host.name")
if err != nil { return err }
hooks := apps.InstallHooks{
    Report: report,
    Configure: func(ctx context.Context, manifest apps.Manifest) error {
        if err := nginx.Apply(ctx, env, hostName); err != nil { return err }
        names := manifest.App + "." + hostName
        if manifest.Default { names += ", " + hostName }
        if err := report("nginx", names); err != nil { return err }
        changed, err := backup.Regenerate(ctx, env, store)
        if err != nil { return err }
        detail := "unchanged"
        if changed {
            result, err := env.Execute(ctx, host.Command{
                Name: "systemctl", Args: []string{"restart", "litestream.service"},
            })
            if err != nil || result.ExitCode != 0 {
                return &host.CommandError{
                    Label: "systemctl restart litestream.service", Result: result, Err: err,
                }
            }
            detail = "updated"
            if manifest.Database != nil { detail = manifest.Database.Path }
        }
        return report("litestream", detail)
    },
}
return apps.Install(ctx, env, remote, store, uri, hooks)
```

The code is a body of an error-returning CLI helper, not a new exported API.
All package selectors resolve to D01, D03, D06, D08, D09 and D11. A nil Execute
is rejected by the domain required-dependency contract before dependent actions;
CLI passes the normalized injected environment. Configure is a consumer boundary:
the app package guarantees files/unit ready before it runs and no start until
it succeeds. It never imports nginx or backup. Private installed-version/unit
probing can be shared with D10 without adding an artificial public probe API.

## Failure task

Install an archive lacking a required secret: fetch and file reports succeed,
the error names the missing key and parameter, exit is 1, and no installed
state changes. Install a file lacking a manifest: fetch report succeeds, error
names the object and missing member, exit is 2, and no installed state changes.
Install a valid app whose startup fails: all prior effects remain inspectable,
exit is 1, and the journal is quoted on stderr. These are fully specified except
fetch-line presence in the startup-failure example.

## Choices and external evidence

- The nonroot system account is `ikigenba`, with no login shell/home creation;
  `/opt/<app>` is writable by it so the app itself can create its state. Install
  never creates or changes state/cache. Account management is after validation.
- Environment key collision, newline and NUL input is rejected before mutation.
  Valid values are escaped for systemd EnvironmentFile and never exposed in
  argv or diagnostics. This closes routine serialization ambiguity safely.
- Archive input is the directly supplied story schema, not sibling internals.
  Link/path restrictions prevent unsafe artifact writes and preserve the no
  state/cache touch guarantee.
- Shared cloud's production adapter remains blocked on dependency approval;
  no S3 or SSM module is approved by this document.
- [External observations](../issues/external-observations.md) and
  `environment-observations.md` record pending live systemd/user management,
  EnvironmentFile encoding, xz/tar and AWS observations required before check.

## Correction: observable validation reports and environment handling

| Task | Before correction | Proposed outcome |
| --- | --- | --- |
| Install valid default artifact when another app is default | File report timing unspecified | Fetch then file stdout, then competing-default stderr; no mutation |
| Install valid artifact with missing required secret | File report timing unspecified | Fetch then file stdout, then missing-key stderr; no mutation |
| Output writer fails on file report | General later-action rule | Stop before default check, secret read or mutation |
| Install environment contains secret marker and plain marker | Validation-only value restriction | Neither marker deliberately emitted as environment data in any report, diagnostic or argv, including failure paths |

App port metadata still appears in its declared report even when the same digits
are a plain setting's value. Failed startup with ordinary journal output still
quotes it verbatim under D09. A journal containing an environment value exposes
an unresolved conflict: [journal and secret handling](../issues/install-journal-secret.md).
No redaction or suppression behavior has been selected. Fresh verification pending.
