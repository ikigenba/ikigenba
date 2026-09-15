# D13-space-app-operations

Restart invokes the installed host tool; logs streams journalctl through SSH
after checking that the unit exists. Neither operation consults a checkout or
transfers secrets. Streaming makes follow useful before the remote command
exits.

## REQUIREMENTS

- R-H0OI-IAHW: Package `internal/spaceapps` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`; args MUST begin with `restart` or `logs`.

- R-H1WE-W28L: Package `internal/spaceapps` MUST export `NoAppError` with exactly `App string` and `Domain string`, implementing `Error() string` as `no app '<App>' on '<Domain>'`.

- R-H34B-9TZA: Restart and logs MUST each require domain and app; restart accepts only help, while logs additionally accepts `--follow` and `--since <when>`/`--since=<when>` before or after operands, with repeated follow idempotent and last since winning. Since MUST consume a following nonempty value even when it begins with `-`, including `-1h`, except a recognized logs option which indicates a missing value.

- R-H4C7-NLPZ: Space app operation syntax failures MUST use `space.UsageError` and help `devctl space --help`, saying `space <subcommand> needs <domain> and <app>` for missing operands, `space <subcommand> takes only <domain> and <app>` for extra operands, `unknown option '<option>'` for unknown options, or `option '--since' requires a value`; no external access MUST occur on these failures.

- R-H5K4-1DGO: `devctl space restart --help` and `devctl space restart -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space restart <domain> <app>

  Have opsctl restart one app's service. Deploy the existing file to apply pushed
  secrets; a restart uses the environment already installed on the host.
  ```

- R-H7ZW-SWY2: `devctl space logs --help` and `devctl space logs -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space logs <domain> <app> [--since <when>] [--follow]

  Print the last 100 journal lines for an installed app. With --since, print all
  lines from that moment using journalctl's time syntax.

  Options:
    --since <when>   read from this moment; passed unchanged to journalctl
    --follow         stream new lines until interrupted
  ```

- R-H97T-6OOR: Restart and logs MUST resolve the account and target instance, return existing missing-space or non-running errors before stdout or SSH, and neither read a checkout nor mutate any cloud resource or parameter.

- R-HAFP-KGFG: Restart MUST run `sudo opsctl restart <app>` with step `restart`, discard successful output and report `restart: ok (opsctl restarted <app>)` only for exit 0; any failure MUST return the host error without a success line.

- R-HBNL-Y865: Before running journalctl, logs MUST query the host with `sudo systemctl show --property=LoadState --value ikigenba-<app>.service`; a returned `not-found` load state MUST yield `NoAppError` without journal output, and query execution failures MUST surface as host errors rather than an absent app.

- R-HCVI-BZWU: Logs MUST run `sudo journalctl -u ikigenba-<app>.service -n 100 --no-pager` by default; when since is supplied, it MUST replace `-n 100` with `--since <unchanged value>`, and when follow is supplied it MUST insert `-f` before `--no-pager`. It MUST neither parse the since value nor invoke opsctl for this operation.

- R-HE3E-PRNJ: Logs MUST stream journal stdout byte for byte through `Host.StreamSudo` with step `logs`, without status decoration or buffering until exit; journal failure MUST preserve already-delivered stdout and quote stderr in a host diagnostic with exit 1. An interrupt MUST cancel the stream and return 0 without a diagnostic when cancellation is the only failure.

- R-HFBB-3JE8: Before a logs unit query, logs MUST reject an app for which `appref.ValidName` is false with `space.UsageError` saying `'<app>' is not a usable app name` and Help `devctl space --help`, without SSH; unit names MUST be formed only from the validated app.
