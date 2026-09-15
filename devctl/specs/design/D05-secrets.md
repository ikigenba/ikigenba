# D05-secrets

Secrets remain one SecureString JSON object per app and space. Push gathers
every required value before writing any object; list exposes names only.
Rotation is a push followed by a deploy of the existing artifact. Listing
retained parameters does not require a live instance.

## REQUIREMENTS

- R-FP8V-1HBG: Package `internal/secrets` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.

- R-FQGR-F925: Package `internal/secrets` MUST export an `Entry` struct whose fields are exactly `App string` and `Keys []string`.

- R-FSWK-6SJJ: Package `internal/secrets` MUST export `Push(ctx context.Context, deps seam.Deps, acct *account.Account, domain string, apps []checkout.App) ([]Entry, error)`, `List(ctx context.Context, acct *account.Account, domain string) ([]Entry, error)`, and `Names(ctx context.Context, acct *account.Account, domain, app string) ([]string, error)`.

- R-FU4G-KKA8: Package `internal/secrets` MUST export `Parameter(domain, app string) string`, returning `/ikigenba/` followed by `domain`, `/` and `app`, and `Prefix(domain string) string`, returning `/ikigenba/` followed by `domain`, verified at least by `Parameter("foo.sbx.ikigenba.dev", "crm")` being `/ikigenba/foo.sbx.ikigenba.dev/crm` and `Prefix("foo.sbx.ikigenba.dev")` being `/ikigenba/foo.sbx.ikigenba.dev`.

- R-FVCC-YC0X: Package `internal/secrets` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage`, and `ExitCode() int`, returning 2.

- R-FWK9-C3RM: Package `internal/secrets` MUST export an `ObjectError` struct whose fields are exactly `Parameter string` and `Reason string`, with the method `Error() string`, returning `<Parameter>: <Reason>`.

- R-FXS5-PVIB: `cli.Run` MUST dispatch the command `secrets` to `secrets.Run`, passing the arguments that follow `secrets`, the `stdout` writer `cli.Run` was given, `deps`, and the profile name `--account` carried, and MUST return 0 when `secrets.Run` returns a nil error.

- R-CAGK-A9JL: When a command's `Run` returns a non-nil error that `errors.As` matches to none of `interface{ ExitCode() int }`, `*cloud.Error`, `*account.NoSpaceError`, `*account.NoZoneError`, `*checkout.NotInCheckoutError`, `*checkout.NoAppError`, `*checkout.ManifestError`, `*checkout.GitError`, and `*keyring.NoValueError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line of stderr, write nothing further to stdout, and return 1.

- R-TB68-9LEO: `devctl secrets --help` and `devctl secrets -h` MUST print exactly this text to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> secrets <subcommand> <domain> [<app>]

  Push the values an app's manifest names from this machine's keyring to the
  space's Parameter Store entry, or list which names a space holds. Values are
  never printed.

  Subcommands:
    push <domain> [<app>]   write /ikigenba/<domain>/<app> for one app, or every app
    list <domain> [<app>]   print the key names held for one app, or every app

  Every subcommand needs --account. Run 'devctl secrets <subcommand> --help' for details.
  ```

- R-TCE4-ND5D: `devctl --account <name> secrets push --help` and `devctl --account <name> secrets push -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> secrets push <domain> [<app>]

  Write the space's Parameter Store entry for an app from this machine's keyring:
  a SecureString at /ikigenba/<domain>/<app> holding a JSON object whose keys are
  the names the app's manifest declares. Each value comes from the environment
  variable of that name, or from the login keyring. Values are never printed.

  With <app> omitted, every app in the checkout is written, in name order. Every
  value is gathered before anything is written, so one missing value leaves every
  app's entry as it was.

  Arguments:
    <domain>   the space to write the entry in
    <app>      one app of the checkout; omitted, every app
  ```

- R-W1MO-Y7YK: `devctl --account <name> secrets list --help` and `devctl --account <name> secrets list -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> secrets list <domain> [<app>]

  Print the key names the space's Parameter Store entry holds for an app: the
  app, then its names sorted and comma-separated, or - when the entry is empty.
  Only names are printed; a value never is.

  With <app> omitted, every entry under /ikigenba/<domain>/ is printed, in app
  order, and a space that holds none prints nothing.

  Arguments:
    <domain>   the space to read the entries of
    <app>      one app the space holds an entry for; omitted, every app
  ```

- R-G2NR-8YH3: The subcommand set of `secrets` MUST be exactly `push` and `list`; each MUST take one `<domain>` operand and an optional `<app>` operand in that order and MUST accept no option other than `--help` and `-h`.

- R-G3VN-MQ7S: `devctl --account <name> secrets` MUST write exactly the three lines `devctl: secrets needs <subcommand>`, an empty line, and `see 'devctl secrets --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-G53K-0HYH: `secrets` invoked with a first argument that is neither `push`, `list`, `--help`, nor `-h` and does not begin with `-` MUST write exactly the three lines `devctl: unknown subcommand '<name>'`, an empty line, and `see 'devctl secrets --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-G6BG-E9P6: `secrets push` and `secrets list` invoked with no `<domain>` operand MUST write exactly the three lines `devctl: secrets push needs <domain>` or `devctl: secrets list needs <domain>` respectively, an empty line, and `see 'devctl secrets --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-G8R9-5T6K: `secrets push` and `secrets list` invoked with more than two operands MUST write exactly the three lines `devctl: secrets push takes at most <domain> and <app>` or `devctl: secrets list takes at most <domain> and <app>` respectively, an empty line, and `see 'devctl secrets --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-G9Z5-JKX9: An argument of `secrets` or of one of its subcommands that begins with `-` and is neither `--help` nor `-h` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl secrets --help' for usage` to be written to stderr, nothing to stdout, and exit 2.

- R-GB71-XCNY: `secrets push` MUST call `checkout.Open` and then `(*Checkout).Apps` or `(*Checkout).App` before it calls `account.Open`, and when that resolution fails it MUST NOT call `deps.Cloud` at all, verified at least by `devctl --account <name> secrets push foo.sbx.ikigenba.dev bogus` writing the single line `devctl: no app 'bogus' in the checkout`, exiting 2, and leaving a recording fake `Deps.Cloud` with no call.

- R-GCEY-B4EN: `secrets push <domain>` MUST push every app `(*Checkout).Apps` returns, in the ascending `Name` order `Apps` returns them in, and `secrets push <domain> <app>` MUST push only the one app `(*Checkout).App` returns for `<app>`.

- R-GEUR-2NW1: `secrets.Push` MUST obtain the value of every name in `Manifest.Secrets` of every app in `apps` through `keyring.Lookup` before it calls `PutSecureParameter` for any app, and when any of those lookups returns an error it MUST call `PutSecureParameter` not at all and MUST return an error that wraps the lookup's error and whose message begins `<that app's Name>: `, verified at least with three apps whose missing value belongs to the last of them in name order and a fake `SSM` that records every write.

- R-GG2N-GFMQ: For each app it pushes, `secrets.Push` MUST call `PutSecureParameter` exactly once, with `Parameter(domain, app.Name)` as the name and a value that is a JSON object whose keys are exactly the distinct names of `app.Manifest.Secrets` and whose value for each is that name's `keyring.Lookup` value byte for byte, and that value MUST be exactly `{}` when `app.Manifest.Secrets` has no name.

- R-GHAJ-U7DF: `secrets push` MUST write, for each app whose parameter was written and in the order they were written, exactly the line `<app>: ok (<n> keys)` to stdout, where `<app>` is the app's name and `<n>` is the decimal count of the keys of the object written — `0 keys` for an app with no names and `1 keys` for an app with one — and MUST write nothing else to stdout, verified at least by reproducing `crm: ok (3 keys)` alone and the three lines `crm: ok (3 keys)`, `dashboard: ok (0 keys)`, `gmail: ok (2 keys)`.

- R-GIIG-7Z44: `secrets.Push` MUST return one `Entry` for each app whose `PutSecureParameter` call succeeded, in the order those calls were made, each carrying that app's name as `App` and the keys of the object written, sorted ascending, as `Keys`, whether or not it also returns a non-nil error, and `secrets push` MUST write the line of every returned `Entry` to stdout before a non-nil error is returned to `cli.Run`.

- R-GJQC-LQUT: `secrets list <domain>` MUST write one line to stdout for each `Entry` that `List` returns, in the order `List` returns them, consisting of the `Entry`'s `App`, one space, and either its `Keys` joined by `,` with no space or `-` when it has none; MUST write nothing to stdout when `List` returns no `Entry`; and MUST exit 0, verified at least by reproducing the three lines `crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG`, `dashboard -`, `gmail GMAIL_CLIENT_ID,GMAIL_CLIENT_SECRET` and by reproducing empty output.

- R-GKY8-ZILI: `secrets list <domain> <app>` MUST write exactly one line to stdout, consisting of `<app>`, one space, and either the names `Names(ctx, acct, domain, app)` returned joined by `,` with no space or `-` when it returned none, and MUST exit 0, verified at least by reproducing `crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG`.

- R-GM65-DAC7: `secrets.List` MUST call `Clients.SSM.ListParameters` with `Prefix(domain)` and MUST return one `Entry` for each returned `Parameter` whose `Name` is `Prefix(domain)`, a `/`, and one further segment containing no `/`, ignoring every other returned `Parameter`, each `Entry` carrying that segment as `App` and the keys of that parameter's value, sorted ascending, as `Keys`; MUST return them sorted ascending by `App`; and MUST return no `Entry` and a nil error when there is none.

- R-GNE1-R22W: `secrets.Names` MUST call `Clients.SSM.GetParameter` with `Parameter(domain, app)` and return the keys of that parameter's value sorted ascending, and MUST return no names and a nil error when that call returns an error that `errors.As` matches to a `*cloud.Error` whose `Code` is `ParameterNotFound`.

- R-GOLY-4TTL: `secrets.List` and `secrets.Names` MUST each return an `*ObjectError` whose `Parameter` is the parameter's name and whose `Reason` is `not a JSON object of strings` when that parameter's value is not a JSON object every one of whose values is a string, and `cli.Run` MUST report it as the single stderr line `devctl: /ikigenba/foo.sbx.ikigenba.dev/crm: not a JSON object of strings` with exit 1 for the parameter of that name.

- R-GR1Q-WDAZ: `secrets list` MUST NOT call `checkout.Open` and MUST pass no `seam.Cmd` to `deps.Exec`.

- R-GS9N-A51O: No exported function or type of `internal/secrets` MUST yield a secret value: `Entry.Keys`, the result of `Names`, and every byte `Run` writes MUST hold only parameter key names, app names and the fixed text this design declares, verified by running `secrets push` and `secrets list` through `cli.Run` with every `keyring.Lookup` value and every value of every `cloud.Parameter` the fake `SSM` returns set to a distinct sentinel, and asserting that no sentinel appears in stdout, in stderr, or in any `Entry` or name the package returned.

- R-GTHJ-NWSD: No package of this module other than `internal/secrets` MUST reference `PutSecureParameter` in a non-test file, verified by a test over the packages' syntax trees, and `internal/spacecreate` MUST write a space's apps' secrets objects only by calling `secrets.Push`.

- R-D209-R4QN: `secrets push` MUST resolve the space before looking up secret values or writing parameters and return the lookup error unchanged; `secrets list` MUST read the requested parameters without requiring an instance, allowing retained objects to be listed after destruction.

- R-D386-4WHC: `secrets push` MUST perform no host operation and MUST NOT deploy or restart an app; re-deploying an existing artifact MUST still upload and invoke install, even when its version is already installed, so a later deploy carries rotated secrets.

- R-D4G2-IO81: When a command's `Run` returns a non-nil error that `errors.As` matches to `interface{ ExitCode() int }`, `cli.Run` MUST write `devctl: ` followed by that error's message as the first line of stderr, MUST follow that line with exactly one empty line and the already-formatted result of `Detail() string` when `errors.As` also matches the error to `interface{ Detail() string }` and that result is not empty, MUST write nothing further to stdout, and MUST return the result of `ExitCode()`, which MUST be 1 or 2.
