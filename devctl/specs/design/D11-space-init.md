# D11-space-init

Reinitialization keeps cloud resources and secrets intact while applying account
configuration to an existing running host. An explicit opsctl option upgrades
first; an explicit email changes that key. The default keeps the installed tool
and saved email.

## REQUIREMENTS

- R-GH64-DYMS: Package `internal/spaceinit` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`.

- R-GIE0-RQDH: Space init MUST accept exactly one domain, optional `--opsctl <version>`/`--opsctl=<version>` and `--acme-email <address>`/`--acme-email=<address>`, and help; options MAY precede or follow the domain and the last repeated value MUST win.

- R-GJLX-5I46: Space init argument failures MUST return `space.UsageError` with help `devctl space --help`, saying `space init needs <domain>` for missing domain, `space init takes only <domain>` for extra operands, `unknown option '<option>'` for unknown flags, and `option '<option>' requires a value` for missing or empty option values; it MUST make no external call on these failures.

- R-GM1P-X1LK: `devctl space init --help` and `devctl space init -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the root refusal, help MUST work without an account and before any external operation:

  ```
  Usage: devctl --account <name> space init <domain> [--opsctl <version>] [--acme-email <address>]

  Set the host's nine derived keys again and run opsctl init. Keep its installed
  opsctl and email unless an option names a replacement. Cloud resources and
  secrets are unchanged.

  Options:
    --opsctl <version>      install this release using the host's saved installer
    --acme-email <address>  replace the CA contact address
  ```

- R-GN9M-ATC9: Space init MUST resolve the account, target instance and its zone before printing; an absent or non-running instance MUST return the existing account/space error with empty stdout and no SSH.

- R-GOHI-OL2Y: Space init MUST report `account: ok (<domain property>, <region>)`, `domain: ok (zone <zone name> <zone id>)`, and `instance: ok (<id> running, <address>)`, then call `hostsetup.Upgrade` when opsctl was supplied or `hostsetup.Version` otherwise, and configure the nine derived keys with an optional tenth email key.

- R-GPPF-2CTN: After configuration succeeds, space init MUST report `opsctl: ok (<version> installed, <count> keys set)` for an explicit upgrade or `opsctl: ok (<version> kept, <count> keys set)` otherwise, then run `sudo opsctl init` with step `init`, discard successful remote output, and report `init: ok` only on success.

- R-GQXB-G4KC: Space init MUST stop at the first failure, retain keys already written, quote the complete selected remote failure stream through `host.CommandError`, and leave instance state, address, records, IAM resources, secrets and S3 objects unchanged; it MUST neither open a checkout nor deploy, restore or restart an app.
