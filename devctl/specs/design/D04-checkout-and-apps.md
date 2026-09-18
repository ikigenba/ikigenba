# D04-checkout-and-apps

Checkout discovery and keyring lookup supply local inputs. Only app identity and
secret names are decoded from manifests; host-specific settings remain opaque.
An app's `main` package sits at `cmd/<name>/` under its directory, like every
other binary in the repository. The shared appref package defines the same usable-name and version grammar for
build and deploy. A checkout tag belongs to one app, independently of branch
ancestry.

## REQUIREMENTS

- R-VGLU-EXV4: Package `internal/checkout` MUST export a `Checkout` struct whose fields are exactly `Root string` and `Deps seam.Deps`, and `Open(ctx context.Context, deps seam.Deps) (*Checkout, error)`.

- R-VHTQ-SPLT: Package `internal/checkout` MUST export the methods `Path(elem ...string) string`, `Apps() ([]App, error)`, and `App(name string) (App, error)` on `*Checkout`.

- R-VLHF-Y0TW: Package `internal/checkout` MUST export an `App` struct whose fields are exactly `Name string`, `Dir string`, and `Manifest Manifest`.

- R-VNX8-PKBA: Package `internal/checkout` MUST export `DecodeManifest(r io.Reader) (Manifest, error)`.

- R-VQD1-H3SO: Package `internal/checkout` MUST export a `NotInCheckoutError` struct whose only field is `Dir string` and whose `Error()` returns `'<Dir>' is not inside a git checkout`, and a `NoAppError` struct whose only field is `Name string` and whose `Error()` returns `no app '<Name>' in the checkout`.

- R-VRKX-UVJD: Package `internal/checkout` MUST export a `ManifestError` struct whose fields are exactly `App string`, `Detail string`, and `Err error`, with the methods `Error() string`, returning `<App>: etc/manifest.toml: <Detail>`, and `Unwrap() error`, returning `Err`.

- R-VSSU-8NA2: Package `internal/checkout` MUST export a `GitError` struct whose fields are exactly `Args []string`, `ExitCode int`, and `Stderr string`, whose `Error()` returns `git <Args joined by single spaces>: exit status <ExitCode>`.

- R-VU0Q-MF0R: Package `internal/keyring` MUST export `Lookup(ctx context.Context, deps seam.Deps, name string) (string, error)` and a `NoValueError` struct whose only field is `Name string` and whose `Error()` returns `no value for '<Name>' in the keyring or the environment`.

- R-VV8N-06RG: `checkout.Open` MUST pass exactly one `seam.Cmd` to `deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `rev-parse` and `--show-toplevel`, and whose `Dir` is `deps.Dir`, and when that process exits 0 with a non-empty standard output MUST return a `*Checkout` whose `Root` is that standard output with its trailing newlines removed and whose `Deps` is `deps`.

- R-VWGJ-DYI5: `checkout.Open` MUST return a nil `*Checkout` and a `*NotInCheckoutError` whose `Dir` is `deps.Dir` when that process exits non-zero or its standard output is empty after trailing newlines are removed, and MUST return a nil `*Checkout` and an error whose message contains `git rev-parse --show-toplevel` when `deps.Exec` returns a non-nil error.

- R-VYWC-5HZJ: `(*Checkout).Path` MUST return `Root` joined with `elem` by the path separator, cleaned, and MUST return `Root` when `elem` is empty.

- R-TI90-6NW4: `checkout.Open` consumers MUST resolve checkout app paths under `Checkout.Root`, even when `Deps.Dir` is a subdirectory. Deploy file operands MUST instead follow D9: relative to `Deps.Dir`, or unchanged when absolute, without checkout discovery. Every `seam.Cmd` that `internal/checkout` passes to `Deps.Exec` other than Open's MUST carry Dir equal to Root; tests MUST cover different Dir and Root values and a relative deploy operand.

- R-L5YW-0VX6: `(*Checkout).Apps` MUST return one `App` for each entry of `Root` that is a directory, holds a regular file at `ManifestFile`, and holds at least one file directly in its `cmd/<name>` directory whose name ends in `.go` but not in `_test.go` and whose package clause is `main`, where `<name>` is that entry's name, so that such a file directly in the entry, in a `cmd/<other>` directory for any `<other>` that is not `<name>`, or in a subdirectory of `cmd/<name>` does not qualify the entry; each carrying that directory's name as `Name`, `Path(Name)` as `Dir`, and the `Manifest` decoded from its `ManifestFile` as `Manifest`; MUST return them sorted ascending by `Name`; MUST return an empty result and a nil error when `Root` holds no such entry; and MUST pass no `seam.Cmd` to `Deps.Exec`.

- R-W2K1-AT7M: `(*Checkout).Apps` MUST return a `*ManifestError` whose `App` is the directory's name when that directory's `ManifestFile` cannot be read or `DecodeManifest` of its contents fails, with that failure's error as `Err`, and MUST return a `*ManifestError` whose `App` is the directory's name and whose `Detail` is `app is '<decoded app>', not '<directory name>'` when the decoded `Manifest.App` is not that directory's name.

- R-W3RX-OKYB: `(*Checkout).App` MUST return the `App` whose `Name` equals `name` among those `Apps` returns, MUST return a `*NoAppError` for `name` when there is none, and MUST return the error `Apps` returned when `Apps` failed.

- R-W7FM-TW6E: `(*Checkout).Head` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `rev-parse` and `HEAD`, and whose `Dir` is `Root`, and when that process exits 0 MUST return its standard output with its trailing newlines removed.

- R-W8NJ-7NX3: `(*Checkout).Clean` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `status` and `--porcelain`, and whose `Dir` is `Root`, and when that process exits 0 MUST return true if and only if its standard output is empty, so that an untracked file reported as a change makes the result false.

- R-W9VF-LFNS: `(*Checkout).TagsAtHead` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `tag`, `--points-at` and `HEAD`, and whose `Dir` is `Root`, and when that process exits 0 MUST return one element for each non-empty line of its standard output, in the order the lines appear, and a result with no element when it has none.

- R-WDJ4-QQVV: `keyring.Lookup` MUST return the value of `deps.Getenv(name)` with its trailing newlines removed, and MUST pass no `seam.Cmd` to `deps.Exec`, when that value is not empty after that removal.

- R-WER1-4IMK: When `deps.Getenv(name)` is empty after its trailing newlines are removed, `keyring.Lookup` MUST pass exactly one `seam.Cmd` to `deps.Exec`, whose `Path` is `secret-tool`, whose `Args` are exactly `lookup`, `name` and `name`'s value, and whose `Dir` is `deps.Dir`; MUST return that process's standard output with its trailing newlines removed when it exits 0 and that output is not empty after the removal; MUST return an empty string and a `*NoValueError` for `name` when it exits non-zero or that output is empty after the removal; and MUST return an empty string and an error whose message contains `secret-tool` when `deps.Exec` returns a non-nil error.

- R-WFYX-IAD9: A value `keyring.Lookup` returns MUST NOT appear in the `Path`, `Args`, `Dir`, or `Env` of any `seam.Cmd` passed to `Deps.Exec`, and MUST NOT appear in anything any command writes to stdout or stderr, verified by running `secrets push` and `space create` through `cli.Run` with a sentinel value reachable through `Deps.Getenv` and asserting that the sentinel appears in neither stream nor in any recorded `seam.Cmd`.

- R-WIEQ-9TUN: When a command fails because `checkout.Open`, `(*Checkout).Apps`, or `(*Checkout).App` returned an error that `errors.As` matches to a `*NotInCheckoutError`, a `*NoAppError`, or a `*ManifestError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line on stderr, write nothing further to stdout, and return 2, verified at least by `devctl build bogus` and `devctl --account <name> secrets push foo.sbx.ikigenba.dev bogus` each writing the single line `devctl: no app 'bogus' in the checkout`.

- R-WKUJ-1DC1: When a command fails because `keyring.Lookup` of a name that an app's `Manifest.Secrets` lists returned a `*NoValueError`, `cli.Run` MUST write the single line `devctl: <app>: no value for '<name>' in the keyring or the environment` to stderr, where `<app>` is that app's `Name`, write nothing further to stdout, and return 2, verified at least for `secrets push` and `space create` each reproducing `devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment`.

- R-CIHV-MSVJ: Package `internal/checkout` MUST export the methods `Head(ctx context.Context) (string, error)`, `Clean(ctx context.Context) (bool, error)`, `TagsAtHead(ctx context.Context) ([]string, error)` on `*Checkout`.

- R-CJPS-0KM8: Package `internal/checkout` MUST export `ManifestFile = "etc/manifest.toml"`.

- R-CKXO-ECCX: `Head`, `Clean`, and `TagsAtHead` MUST return a `*GitError` carrying the command arguments, exit status and stderr for any non-zero process exit, and a non-`GitError` wrapping the runner error when `Deps.Exec` cannot run the process.

- R-CM5K-S43M: Package `internal/checkout` MUST export `Manifest` with exactly `App string` and `Secrets []string`, decoded from the TOML keys `app` and `secrets`.

- R-CNDH-5VUB: `checkout.DecodeManifest` MUST decode `app` and `secrets`, treat an absent `secrets` key as an empty list, and ignore all other valid TOML fields, including `port`, `default`, `[env]`, and `[database]`; those fields MUST NOT be validated or interpreted by devctl.

- R-COLD-JNL0: `checkout.DecodeManifest` MUST reject invalid TOML, an absent or empty string `app`, a non-string `app`, and a present `secrets` value that is not an array of strings.

- R-CPT9-XFBP: When a checkout method returns a `*GitError`, `cli.Run` MUST write its message prefixed `devctl: ` to stderr, followed by exactly one empty line and `seam.QuoteOutput(Stderr)` when nonempty, preserve preceding stdout, and return 1.

- R-CR16-B72E: `appref.ValidName` MUST accept ASCII lowercase letters, digits and hyphens in a label of 1–63 bytes with an alphanumeric first and last byte, except exactly `host`, `deploy`, `backup-host`, `backup-services`, and `renew-certificate`, which MUST be rejected.

- R-CS92-OYT3: `appref.ValidVersion` MUST accept a literal `v` followed by three dot-separated nonnegative decimal integers without leading zeroes, optionally followed by a hyphen and nonempty dot-separated ASCII alphanumeric/hyphen prerelease identifiers (numeric identifiers have no leading zeroes), optionally followed by `+` and nonempty dot-separated ASCII alphanumeric/hyphen metadata identifiers; all other forms MUST be rejected.

- R-CTGZ-2QJS: `appref.VersionForTag` MUST succeed only for a usable `app` and a tag consisting exactly of that app, `/`, and a valid version, returning the version byte for byte; unrelated, bare-version and malformed tags MUST not match.

- R-CUOV-GIAH: `appref.ParseFile` MUST accept only a basename consisting of a usable app, `-`, a valid version, and `.tar.xz`, returning both components verbatim; names with path separators or no unique valid split MUST fail. Hyphenated app names, prereleases and build metadata MUST retain their complete spelling.

- R-CVWR-UA16: Package `internal/appref` MUST export `ValidName(name string) bool`.

- R-CX4O-81RV: Package `internal/appref` MUST export `ValidVersion(version string) bool`.

- R-CZKG-ZL99: Package `internal/appref` MUST export `VersionForTag(app, tag string) (string, bool)`.

- R-D0SD-DCZY: Package `internal/appref` MUST export `ParseFile(name string) (app, version string, err error)`.
