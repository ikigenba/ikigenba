# D12-remove

Remove delegates uninstallation to the target host, retaining cloud secrets and
uploaded artifacts. It does not require a checkout or infer which apps the host
can uninstall.

## REQUIREMENTS

- R-GS57-TWB1: Package `internal/remove` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`.

- R-GTD4-7O1Q: Package `internal/remove` MUST export `UsageError` with exactly `Message string` and `Help string`, methods `Error() string` returning Message, `ExitCode() int` returning 2, and `Detail() string` returning `see '<Help>' for usage`.

- R-GUL0-LFSF: `devctl remove --help` and `devctl remove -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> remove <domain> <app>

  Have opsctl on <domain> take <app> off the space: stop and remove its service,
  remove its binary and configuration, and stop routing its name. Its state/ is
  kept on the host and its secrets are kept in the account, so a later deploy of
  <app> lands over its data. What remove does on the host is opsctl's.
  ```

- R-GVSW-Z7J4: Remove MUST accept exactly domain and app operands and only help options; missing operands MUST say `remove needs <domain> and <app>`, extra operands `remove takes only <domain> and <app>`, and unknown options `unknown option '<option>'`, using UsageError with help `devctl remove --help` and no external calls.

- R-GX0T-CZ9T: `cli.Run` MUST dispatch remove to `remove.Run` with its arguments, stdout, deps and profile, returning 0 on nil error.

- R-3TH7-OLJE: Remove MUST resolve the selected account and target space and return existing missing-space or non-running errors before stdout or SSH; it MUST NOT open a checkout or directly mutate cloud resources, secrets or backup objects; backup writes made by the delegated uninstall remain opsctl's responsibility.

- R-GZGM-4IR7: Remove MUST invoke `Host.Sudo` with step `remove` and arguments `opsctl`, `uninstall`, and app; success MUST discard remote output and report exactly `remove: ok (opsctl uninstalled <app>)`, while failure MUST return the host error with no success line. Host-side data retention MUST remain the installed tool’s responsibility.
