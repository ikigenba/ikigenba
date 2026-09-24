# D04-checkout-and-apps

Checkout discovery and keyring lookup supply local inputs. Only app identity and
secret names are decoded from manifests; host-specific settings remain opaque.
The one exception is `port`: no app listens on a port any more, because the
host hands each app its socket, so a manifest that still names one is refused
where it is decoded. `build` therefore refuses it before compiling anything,
and every command that reads the checkout's apps refuses the same way, as it
already does for any other broken manifest.
An app's `main` package sits at `cmd/<name>/` under its directory, like every
other binary in the repository. The shared appref package defines the same usable-name and version grammar for
build and deploy. A checkout tag belongs to one app, independently of branch
ancestry.

The checkout is also where the platform is stated. devctl has no
configuration of its own: the root domain and its region live in the root
file, `infra/terraform.tfvars.json` at the checkout root, the same file
Terraform reads, and every command that touches AWS or a host reads it there.
The checkout root is the path `checkout.Open` discovers (`git rev-parse
--show-toplevel`, so in a linked worktree it is that worktree's top level).
The file's contents are one JSON object with the string keys `domain` and
`region`; any other key is Terraform's business and is ignored. The three ways
the read fails on the developer's side, being outside a checkout, a checkout
without the file, and a malformed file, are typed errors with the exact
diagnostics the bootstrap stories fix, and all three are preflight failures
(exit 2) reported before any cloud client is opened. A malformed file is one
that is not exactly one JSON object, one missing `domain` or `region` or
holding an empty string there, or one whose `domain` or `region` is not a
string. Both entry points exist because a command that already opened the
checkout for its apps should not run `git` a second time to find the same
root.

The `<space>` operand every space-taking command accepts, and the
`<app>.<space>` operand `apex set` accepts, are parsed by one shared package,
`internal/spaceref`, and by nothing else. The rule is the stories': strip the
root suffix if present, then what remains must be exactly one valid DNS label
(a space) or exactly two (an app on a space). A label is an RFC 1123 label in
lowercase, 1 to 63 bytes of lowercase ASCII letters, digits and hyphens with
no hyphen at either end; uppercase input is refused, never folded, because
the stories show `Crm.sbx1` refused as a bad label. The reserved app names
`appref.ValidName` rejects apply to the `<app>` part of `<app>.<space>`, with
the same `is not a usable app name` diagnostic build and logs use, and do not
apply to a space label: they exist because of collisions with host-side
service and bucket-key names, and a space label never sits in those
positions. Parsing returns the label and the full space domain together,
because downstream names need both: the role is `<space domain>`, the bucket
prefix is `<label>/`. Every refusal is exit 2 and happens before anything is
looked up.

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

- R-QG5U-LTSE: `checkout.Open` consumers MUST resolve checkout app paths and the root file under `Checkout.Root`, even when `Deps.Dir` is a subdirectory. Deploy file operands MUST instead follow D9: relative to `Deps.Dir`, or unchanged when absolute, never against `Checkout.Root`, although deploy discovers the checkout to read the root file. Every `seam.Cmd` that `internal/checkout` passes to `Deps.Exec` other than Open's MUST carry Dir equal to Root; tests MUST cover different Dir and Root values and a relative deploy operand.

- R-L5YW-0VX6: `(*Checkout).Apps` MUST return one `App` for each entry of `Root` that is a directory, holds a regular file at `ManifestFile`, and holds at least one file directly in its `cmd/<name>` directory whose name ends in `.go` but not in `_test.go` and whose package clause is `main`, where `<name>` is that entry's name, so that such a file directly in the entry, in a `cmd/<other>` directory for any `<other>` that is not `<name>`, or in a subdirectory of `cmd/<name>` does not qualify the entry; each carrying that directory's name as `Name`, `Path(Name)` as `Dir`, and the `Manifest` decoded from its `ManifestFile` as `Manifest`; MUST return them sorted ascending by `Name`; MUST return an empty result and a nil error when `Root` holds no such entry; and MUST pass no `seam.Cmd` to `Deps.Exec`.

- R-W2K1-AT7M: `(*Checkout).Apps` MUST return a `*ManifestError` whose `App` is the directory's name when that directory's `ManifestFile` cannot be read or `DecodeManifest` of its contents fails, with that failure's error as `Err`, and MUST return a `*ManifestError` whose `App` is the directory's name and whose `Detail` is `app is '<decoded app>', not '<directory name>'` when the decoded `Manifest.App` is not that directory's name.

- R-W3RX-OKYB: `(*Checkout).App` MUST return the `App` whose `Name` equals `name` among those `Apps` returns, MUST return a `*NoAppError` for `name` when there is none, and MUST return the error `Apps` returned when `Apps` failed.

- R-W7FM-TW6E: `(*Checkout).Head` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `rev-parse` and `HEAD`, and whose `Dir` is `Root`, and when that process exits 0 MUST return its standard output with its trailing newlines removed.

- R-W8NJ-7NX3: `(*Checkout).Clean` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `status` and `--porcelain`, and whose `Dir` is `Root`, and when that process exits 0 MUST return true if and only if its standard output is empty, so that an untracked file reported as a change makes the result false.

- R-W9VF-LFNS: `(*Checkout).TagsAtHead` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `tag`, `--points-at` and `HEAD`, and whose `Dir` is `Root`, and when that process exits 0 MUST return one element for each non-empty line of its standard output, in the order the lines appear, and a result with no element when it has none.

- R-WDJ4-QQVV: `keyring.Lookup` MUST return the value of `deps.Getenv(name)` with its trailing newlines removed, and MUST pass no `seam.Cmd` to `deps.Exec`, when that value is not empty after that removal.

- R-WER1-4IMK: When `deps.Getenv(name)` is empty after its trailing newlines are removed, `keyring.Lookup` MUST pass exactly one `seam.Cmd` to `deps.Exec`, whose `Path` is `secret-tool`, whose `Args` are exactly `lookup`, `name` and `name`'s value, and whose `Dir` is `deps.Dir`; MUST return that process's standard output with its trailing newlines removed when it exits 0 and that output is not empty after the removal; MUST return an empty string and a `*NoValueError` for `name` when it exits non-zero or that output is empty after the removal; and MUST return an empty string and an error whose message contains `secret-tool` when `deps.Exec` returns a non-nil error.

- R-WFYX-IAD9: A value `keyring.Lookup` returns MUST NOT appear in the `Path`, `Args`, `Dir`, or `Env` of any `seam.Cmd` passed to `Deps.Exec`, and MUST NOT appear in anything any command writes to stdout or stderr, verified by running `secrets push` and `space create` through `cli.Run` with a sentinel value reachable through `Deps.Getenv` and asserting that the sentinel appears in neither stream nor in any recorded `seam.Cmd`.

- R-QEXY-821P: When a command fails because `checkout.Open`, `checkout.ReadRootFile`, `(*Checkout).ReadRootFile`, `(*Checkout).Apps`, or `(*Checkout).App` returned an error that `errors.As` matches to a `*NotInCheckoutError`, a `*NoRootFileError`, a `*RootFileError`, a `*NoAppError`, or a `*ManifestError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line on stderr, write nothing further to stdout, and return 2, verified at least by `devctl build bogus` and `devctl secrets push sbx1 bogus` each writing the single line `devctl: no app 'bogus' in the checkout`, and by `devctl space list` writing the single line `devctl: '<Deps.Dir>' is not inside a git checkout` when the fake `git` process exits non-zero, `devctl: no infra/terraform.tfvars.json in the checkout` in a temporary checkout that has no root file, and `devctl: infra/terraform.tfvars.json: missing 'region'` in one whose root file holds `{"domain": "ikigenba.dev"}`.

- R-WKUJ-1DC1: When a command fails because `keyring.Lookup` of a name that an app's `Manifest.Secrets` lists returned a `*NoValueError`, `cli.Run` MUST write the single line `devctl: <app>: no value for '<name>' in the keyring or the environment` to stderr, where `<app>` is that app's `Name`, write nothing further to stdout, and return 2, verified at least for `secrets push` and `space create` each reproducing `devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment`.

- R-CIHV-MSVJ: Package `internal/checkout` MUST export the methods `Head(ctx context.Context) (string, error)`, `Clean(ctx context.Context) (bool, error)`, `TagsAtHead(ctx context.Context) ([]string, error)` on `*Checkout`.

- R-CJPS-0KM8: Package `internal/checkout` MUST export `ManifestFile = "etc/manifest.toml"`.

- R-CKXO-ECCX: `Head`, `Clean`, and `TagsAtHead` MUST return a `*GitError` carrying the command arguments, exit status and stderr for any non-zero process exit, and a non-`GitError` wrapping the runner error when `Deps.Exec` cannot run the process.

- R-CM5K-S43M: Package `internal/checkout` MUST export `Manifest` with exactly `App string` and `Secrets []string`, decoded from the TOML keys `app` and `secrets`.

- R-NG5I-UMQ6: `checkout.DecodeManifest` MUST decode `app` and `secrets`, treat an absent `secrets` key as an empty list, and ignore every other valid TOML key, including `default`, `[env]`, and `[database]`, which devctl MUST NOT validate or interpret; the one other key it acts on is a top-level `port`, which R-NM90-RHFN refuses.

- R-NM90-RHFN: `checkout.DecodeManifest` MUST return an error whose message is exactly `'port' is not allowed; the host gives the app its socket` when its input is valid TOML holding a top-level key `port`, whatever that key's value or type, and MUST NOT refuse a `port` key inside a table such as `[env]`; `(*Checkout).Apps` MUST carry that message unchanged as the `Detail` of the `*ManifestError` it returns for that directory; verified at least by `DecodeManifest` refusing `app = "crm"` with `port = 3100` and with `port = "3100"`, each with exactly that message, and accepting `app = "crm"` with `[env]` holding `port = "3100"`; and by `(*Checkout).Apps` on a temporary directory whose only app is `crm/etc/manifest.toml` holding `app = "crm"` and `port = 3100`, returning a `*ManifestError` for `crm` whose `Detail` is exactly that message.

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

- R-Q2QY-ECMR: Package `internal/checkout` MUST export `RootFilePath = "infra/terraform.tfvars.json"`.

- R-Q3YU-S4DG: Package `internal/checkout` MUST export a `RootFile` struct whose fields are exactly `Domain string` and `Region string`, decoded from the JSON keys `domain` and `region`.

- R-Q56R-5W45: Package `internal/checkout` MUST export `ReadRootFile(ctx context.Context, deps seam.Deps) (RootFile, error)` and the method `ReadRootFile() (RootFile, error)` on `*Checkout`.

- R-Q6EN-JNUU: Package `internal/checkout` MUST export a `NoRootFileError` struct whose only field is `Checkout string` and whose `Error()` returns `no infra/terraform.tfvars.json in the checkout`, and a `RootFileError` struct whose only field is `Detail string` and whose `Error()` returns `infra/terraform.tfvars.json: <Detail>`.

- R-Q8UG-B7C8: `(*Checkout).ReadRootFile` MUST read the regular file at `Path(RootFilePath)` and MUST pass no `seam.Cmd` to `Deps.Exec`; when no file exists at that path it MUST return a zero `RootFile` and a `*NoRootFileError` whose `Checkout` is `Root`; when the file exists but cannot be read it MUST return a zero `RootFile` and an error whose message contains `infra/terraform.tfvars.json` and that `errors.As` matches to neither `*NoRootFileError` nor `*RootFileError`.

- R-QA2C-OZ2X: `(*Checkout).ReadRootFile` MUST return a zero `RootFile` and a `*RootFileError` whose `Detail` is `not a JSON object` when the file's contents are not exactly one JSON value that is an object, verified at least for an empty file, invalid JSON, a JSON array, a JSON string, `null`, and an object followed by further non-whitespace bytes.

- R-QBA9-2QTM: `(*Checkout).ReadRootFile` MUST return a zero `RootFile` and a `*RootFileError` whose `Detail` is `missing '<key>'` when the object has no member `<key>` or its value is the empty string, and `'<key>' is not a string` when the object has a member `<key>` whose value is not a JSON string, checking `domain` first and `region` second so that the first failing key decides the `Detail`, verified at least by `{"domain": "ikigenba.dev"}` giving `missing 'region'`, `{}` giving `missing 'domain'`, `{"domain": 1, "region": "us-east-2"}` giving `'domain' is not a string`, `{"domain": "ikigenba.dev", "region": null}` giving `'region' is not a string`, and `{"domain": "", "region": "us-east-2"}` giving `missing 'domain'`.

- R-QCI5-GIKB: When the object has string members `domain` and `region` that are both non-empty, `(*Checkout).ReadRootFile` MUST return a `RootFile` whose `Domain` and `Region` are those values byte for byte, without trimming or case change, and a nil error, and MUST ignore every other member of the object whatever its type, verified at least with `{"domain": "ikigenba.dev", "region": "us-east-2", "instance_type": "t4g.small", "tags": {"a": 1}}`.

- R-QDQ1-UAB0: `checkout.ReadRootFile` MUST call `Open` with `ctx` and `deps` and, when `Open` fails, return a zero `RootFile` and `Open`'s error unchanged; otherwise it MUST return the result of `(*Checkout).ReadRootFile` on the opened checkout, so that the whole call passes exactly one `seam.Cmd` to `deps.Exec`, which is `Open`'s.

- R-N1LD-IX1I: Every command that calls `Deps.Cloud` MUST call `checkout.ReadRootFile` or `(*Checkout).ReadRootFile` before its first call to `Deps.Cloud` and MUST report its own usage errors, at least a missing or extra operand and an unknown option, without calling `checkout.ReadRootFile` or `checkout.Open` and without passing any `seam.Cmd` to `Deps.Exec`; MUST make that root-file read before it parses a `<space>` or `<app>.<space>` operand; and when that read returns an error MUST call `Deps.Cloud` not at all; verified at least through `cli.Run` by `devctl space status` with no operand passing no `seam.Cmd` and by `devctl space list` in a temporary checkout that has no root file leaving a recording fake `Deps.Cloud` with no call.

- R-QILN-DD9S: Package `internal/spaceref` MUST export a `Space` struct whose fields are exactly `Label string` and `Domain string`.

- R-QJTJ-R50H: Package `internal/spaceref` MUST export an `App` struct whose fields are exactly `Name string`, `Space Space`, and `Hostname string`.

- R-QL1G-4WR6: Package `internal/spaceref` MUST export `Parse(operand, root string) (Space, error)`, `ParseApp(operand, root string) (App, error)`, and `ValidLabel(label string) bool`.

- R-QM9C-IOHV: Package `internal/spaceref` MUST export a `NotASpaceError` struct whose fields are exactly `Operand string` and `Root string`, whose `Error()` returns `'<Operand>' is not a space: a space is one label under '<Root>'`, and an `InvalidLabelError` struct whose only field is `Operand string` and whose `Error()` returns `'<Operand>' is not a valid label`; both MUST have the method `ExitCode() int` returning 2.

- R-QNH8-WG8K: Package `internal/spaceref` MUST export a `NotAnAppError` struct whose only field is `Operand string` and whose `Error()` returns `'<Operand>' is not an app on a space: <app>.<space>`, and an `UnusableAppError` struct whose only field is `Name string` and whose `Error()` returns `'<Name>' is not a usable app name`; both MUST have the method `ExitCode() int` returning 2.

- R-QOP5-A7Z9: `spaceref.ValidLabel` MUST return true if and only if `label` is 1 to 63 bytes long, every byte is an ASCII lowercase letter, an ASCII digit, or `-`, and neither its first nor its last byte is `-`; verified at least by accepting `sbx1`, `a`, `a-b`, `host`, and a 63-byte label, and rejecting the empty string, `Foo_1`, `Crm`, `-a`, `a-`, `a.b`, `sbx1 `, and a 64-byte label; and every name `appref.ValidName` accepts MUST be one `ValidLabel` accepts.

- R-QR4Y-1RGN: `spaceref.Parse` MUST return a zero `Space` and a `*NotASpaceError` carrying `operand` and `root` when `operand` equals `root`, or when the remainder of `operand` after one removal of a trailing `.` followed by `root`, if `operand` ends in that, contains a `.`; otherwise it MUST return a zero `Space` and an `*InvalidLabelError` carrying `operand` when `ValidLabel` of that remainder is false; verified at least by `crm.sbx1`, `crm.sbx1.ikigenba.dev`, `foo.example.com`, `ikigenba.dev`, and `sbx1.ikigenba.dev.ikigenba.dev` with root `ikigenba.dev` each giving `'<operand>' is not a space: a space is one label under 'ikigenba.dev'`, and by `Foo_1` and `Foo_1.ikigenba.dev` each giving `'<operand>' is not a valid label`.

- R-QSCU-FJ7C: When `spaceref.Parse` does not fail, it MUST return a `Space` whose `Label` is the remainder described by the refusal rule and whose `Domain` is that `Label`, `.`, and `root`, with `root` unchanged, verified at least by `sbx1` and `sbx1.ikigenba.dev` with root `ikigenba.dev` each returning `Label` `sbx1` and `Domain` `sbx1.ikigenba.dev`, and by the suffix comparison being byte-exact so that `sbx1.IKIGENBA.DEV` gives a `*NotASpaceError`.

- R-QTKQ-TAY1: `spaceref.ParseApp` MUST return a zero `App` and a `*NotAnAppError` carrying `operand` when `operand` equals `root` or when the remainder of `operand`, formed as `Parse` forms it, does not consist of exactly two `.`-separated pieces; otherwise it MUST return a zero `App` and an `*InvalidLabelError` carrying `operand` when `ValidLabel` of either piece is false, and then a zero `App` and an `*UnusableAppError` carrying the first piece when `appref.ValidName` of the first piece is false; verified at least by `sbx1`, `crm.sbx1.example.com`, `ikigenba.dev`, and `crm.ikigenba.dev` with root `ikigenba.dev` each giving `'<operand>' is not an app on a space: <app>.<space>`, by `Crm.sbx1` and `crm.Sbx1.ikigenba.dev` each giving `'<operand>' is not a valid label`, and by `host.sbx1` giving `'host' is not a usable app name`.

- R-QUSN-72OQ: When `spaceref.ParseApp` does not fail, it MUST return an `App` whose `Name` is the first piece, whose `Space` is what `Parse` returns for the second piece with the same `root`, and whose `Hostname` is `Name`, `.`, and `Space.Domain`, verified at least by `crm.sbx1` and `crm.sbx1.ikigenba.dev` with root `ikigenba.dev` each returning `Name` `crm`, `Space.Label` `sbx1`, `Space.Domain` `sbx1.ikigenba.dev`, and `Hostname` `crm.sbx1.ikigenba.dev`.

- R-QW0J-KUFF: Every command that takes a `<space>` operand MUST obtain the space by calling `spaceref.Parse` with the operand as typed and the `Domain` of the checkout's `RootFile`, `apex set` MUST obtain the app and space by calling `spaceref.ParseApp` the same way, no package other than `internal/spaceref` MUST split an operand on `.` or compare it with the root to decide what it names, and when the call returns an error the command MUST return that error unchanged and MUST call `Deps.Cloud` not at all; verified at least through `cli.Run` by `devctl space status crm.sbx1` writing the single line `devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'`, `devctl space status Foo_1` writing `devctl: 'Foo_1' is not a valid label`, and `devctl apex set sbx1` writing `devctl: 'sbx1' is not an app on a space: <app>.<space>`, each to stderr with empty stdout, exit 2, and a recording fake `Deps.Cloud` left with no call.
