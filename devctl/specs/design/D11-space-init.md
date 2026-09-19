# D11-space-init

`space init` is `create`'s `opsctl` and `init` steps run again on a space that
already exists: the way back to a host that matches its configuration store
when something the store came from has changed. It lives in
`internal/spaceinit` and follows the same session every space-taking command
follows: usage errors first, then the root file, then the `<space>` operand
through the one parser, then one cloud session, then the space by its tags,
then the host over ssh. It keeps cloud resources, records, and secrets intact.

The command sets again the five keys `create` derives from the root, the zone,
and the space, through D10's `Configure` with no backup periods, so the four
period keys the operator may have changed since `create` are left alone;
`acme.email` is set only when `--acme-email` supplies an address. Every other
key, `host.apex` included, is untouched. `--opsctl` moves the host to a named
release first by running the installer the host keeps; without it the
installed opsctl is kept and its version reported. Then `sudo opsctl init`
runs, its successful report is not relayed, and a failed one is relayed the
way D06 relays every host command failure. The step names `account`,
`domain`, `instance`, `opsctl`, and `init` are fixed by the stories, as are
the refusals for a stopped space and one that does not exist.

## REQUIREMENTS

- R-OMDZ-2I7D: Package `internal/spaceinit` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout` and no profile name.

- R-ONLV-G9Y2: Space init MUST accept exactly one `<space>` operand, the options `--opsctl <version>`/`--opsctl=<version>` and `--acme-email <address>`/`--acme-email=<address>`, and `--help`/`-h`, and no other option; options MAY precede or follow the operand, and the last value of a repeated option MUST win.

- R-OOTR-U1OR: Space init argument failures MUST return a `*space.UsageError` whose `Help` is `devctl space --help` and whose `Message` is `space init needs <space>` for a missing operand, `space init takes only <space>` for more than one operand, `unknown option '<option>'` for an argument beginning with `-` that is none of its options, and `option '<option>' requires a value` for an `--opsctl` or `--acme-email` whose value is missing or empty; on these failures `Run` MUST call neither `checkout.ReadRootFile` nor `checkout.Open`, MUST pass no `seam.Cmd` to `deps.Exec` or `deps.Stream`, and MUST call `deps.Cloud` not at all; verified at least through `cli.Run` by `devctl space init` writing exactly the three lines `devctl: space init needs <space>`, an empty line, and `see 'devctl space --help' for usage` to stderr with nothing on stdout and exit 2, and by `devctl space init sbx1 --opsctl` and `devctl space init sbx1 --acme-email` writing `devctl: option '--opsctl' requires a value` and `devctl: option '--acme-email' requires a value` as their first lines with exit 2.

- R-OQ1O-7TFG: `devctl space init --help` and `devctl space init -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work outside a checkout, without a root file, and before any external operation, calling `deps.Cloud` not at all and passing no `seam.Cmd` to `deps.Exec` or `deps.Stream`, verified at least with `Deps.Dir` set to a directory that is not inside a git checkout:

  ```
  Usage: devctl space init <space> [--opsctl <version>] [--acme-email <address>]

  Set the host's five derived keys again and run opsctl init. Keep its installed
  opsctl, its CA address, and its backup periods unless an option names a
  replacement. Cloud resources, records, and secrets are unchanged.

  Options:
    --opsctl <version>      move the host to this opsctl release first
    --acme-email <address>  change where the CA sends the space's expiry warnings
  ```

- R-OR9K-LL65: After its arguments are accepted, space init MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged; MUST then obtain the space from the `<space>` operand under R-QW0J-KUFF; MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `checkout.RootFile` read, as D02 R-OO0I-HZUA binds, and return its error unchanged; MUST then call `cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, space.Domain)` with that `Domain` and the parsed `spaceref.Space`'s `Domain` and return its error unchanged, so that a `*cloud.NoSpaceError` reaches `cli.Run` as D03 R-R10I-DEAA describes; MUST then return a `*space.NotRunningError` whose `Domain` is the returned `cloud.Space`'s `Domain` and whose `State` is its `State` when that state is not `cloud.StateRunning`; and MUST then call `session.Clients.Route53.Zone(ctx, root.Domain)` and return its error unchanged; every one of these MUST complete before any byte is written to `stdout` and before any `seam.Cmd` other than the one `checkout.Open` passes reaches `deps.Exec` or `deps.Stream`; verified at least through `cli.Run`, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by `devctl space init gone` writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` with empty stdout and exit 1, and by `devctl space init sbx2` against a fake `EC2` whose `sbx2.ikigenba.dev` instance is `stopped` writing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` with empty stdout and exit 1, neither passing an `ssh` `seam.Cmd` to `deps.Exec` or `deps.Stream`.

- R-OSHG-ZCWU: When every call R-OR9K-LL65 names has returned a nil error, space init MUST write to `stdout`, through `space.Step` and before any `seam.Cmd` whose `Path` is `ssh` reaches `deps.Exec`, exactly the three lines `account: ok (<root.Domain>, <root.Region>, <session.AccountID>)`, `domain: ok (zone <zone.Name> <zone.ID>)`, and `instance: ok (<space.ID> running, <space.Address>)`, in that order, taking the account id from the `Session` `cloud.Connect` returned, the zone from the `cloud.Zone` `Route53.Zone` returned, and the id and address from the `cloud.Space` `cloud.LookupSpace` returned; verified at least by reproducing `account: ok (ikigenba.dev, us-east-2, 295229566359)`, `domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)`, and `instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)`.

- R-U320-UM5O: After the `instance` line, space init MUST, on a `host.Host` whose `Address` is the `cloud.Space`'s `Address` and whose `Deps` is `deps`, call `hostsetup.Upgrade(ctx, h, <version>)` exactly once with the `--opsctl` value verbatim when that option was given and `hostsetup.Version(ctx, h)` exactly once otherwise, never both, and MUST return that call's error unchanged, calling `hostsetup.Configure` not at all in that case; the `<version>` R-GPPF-2CTN's `opsctl` line reports MUST be the `--opsctl` value verbatim in the `installed` form and the string `Version` returned in the `kept` form, neither parsed, validated, nor compared with anything; verified at least with a fake `Deps.Exec` whose `sudo opsctl version` process exits 0 with arbitrary standard output, reproduced in the `kept` line as the string `Version` returned under D10 R-G670-Y0YJ.

- R-OUX9-QWE8: After `Upgrade` or `Version` returned a nil error, space init MUST call `hostsetup.Configure(ctx, h, "opsctl", cfg)` exactly once, on the same `host.Host`, with a `hostsetup.Config` whose `Root` is `root.Domain`, `Region` is `root.Region`, `ZoneID` is the `ID` of the zone `Route53.Zone` returned, `Space` is the parsed `spaceref.Space`, `Email` is the `--acme-email` value verbatim when that option was given and empty otherwise, and `Periods` is nil; MUST return `Configure`'s error unchanged when it is not nil, writing no `opsctl` line; and the `<count>` R-GPPF-2CTN's `opsctl` line reports MUST be the count `Configure` returned; verified at least by reproducing `5 keys set` with the five remote argument vectors `sudo opsctl config set host.name=sbx1.ikigenba.dev`, `sudo opsctl config set dns.provider=route53`, `sudo opsctl config set dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78`, `sudo opsctl config set aws.region=us-east-2`, and `sudo opsctl config set backup.s3_uri=s3://ikigenba.dev/sbx1/`, in that order and with no `backup.*_seconds` key, for the operand `sbx1` in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}` and a zone whose `ID` is `Z09565073GHK8BYWQ1A78`, and by reproducing `6 keys set` with `sudo opsctl config set acme.email=alerts@ikigenba.dev` as the sixth vector for `--acme-email alerts@ikigenba.dev`.

- R-GPPF-2CTN: After configuration succeeds, space init MUST report `opsctl: ok (<version> installed, <count> keys set)` for an explicit upgrade or `opsctl: ok (<version> kept, <count> keys set)` otherwise, then run `sudo opsctl init` with step `init`, discard successful remote output, and report `init: ok` only on success.

- R-OW56-4O4X: The `init` step MUST be exactly one call to `(host.Host).Sudo` with the step `init` and exactly the arguments `opsctl` and `init`, on the same `host.Host` as the `opsctl` step, and a non-nil error from it MUST be returned unchanged, so that a `*host.CommandError` reaches `cli.Run` as D06 R-D9BO-1R6T and D05 R-D4G2-IO81 describe; verified at least through `cli.Run` with a fake `Deps.Exec` whose `sudo opsctl init` process exits 2 with arbitrary multi-line standard output and empty standard error, by the four `ok` lines remaining on stdout with no `init` line, and stderr being exactly `devctl: init: ssh ec2-user@18.118.7.42 sudo opsctl init: exit status 2`, one empty line, and that standard output with every line prefixed `> `, with exit 1, for an instance whose `Address` is `18.118.7.42`.

- R-OXD2-IFVM: Space init MUST stop at the first failing call, passing no further `seam.Cmd` to `deps.Exec` or `deps.Stream` and calling no further client method after it, and MUST leave keys already written as they are, issuing no `opsctl config del`; the `seam.Cmd` values it passes to `deps.Exec` MUST be only the one `checkout.Open` passes and `ssh` commands whose remote argument vector is `sudo bash <hostsetup.SavedInstaller> <version>`, `sudo opsctl version`, `sudo opsctl config set <key>=<value>`, or `sudo opsctl init`, and it MUST pass nothing to `deps.Stream`; it MUST read nothing from the checkout other than the root file, calling neither `(*Checkout).Apps` nor `(*Checkout).App`; and it MUST make no cloud call other than the `STS.CallerAccountID` call `cloud.Connect` makes, the `EC2.ListSpaceInstances` call `cloud.LookupSpace` makes, and its one `Route53.Zone` call, so that no instance state, address, record, IAM resource, parameter, or S3 object changes; verified at least with cloud fakes that fail on any other method, by a recording `Deps.Exec` whose every `ssh` command matches that list, and by a failing `sudo opsctl config set` leaving the fake with no later command.
