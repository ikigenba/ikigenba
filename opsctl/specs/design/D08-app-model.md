# D08-app-model

The apps package gives installation, status, nginx generation, and backup one
view of the services on the host. A directory can hold useful service data
without an installed executable. Its manifest contributes routing and database
declarations independently of whether its service can run.

A manifest names its app, whether it is the host's default, its secrets, its
plain settings and its database. It names no port: the host hands every app
its socket, so a manifest that still carries `port` is refused rather than
ignored. How long an app may drain and how long systemd waits for it to stop
are not the app's choice either; they are two space-wide keys in the store,
read and validated here so install and init agree on them.

A package may also ship an icon, `share/icon.svg`, which puts its app in the
host's service launcher (D15). The icon is not part of the manifest or of a
discovered `Service`: discovery stays what nginx and backup need, and the
services package reads the icon itself when it regenerates the services file.
What belongs here is what an icon is allowed to be, because `install` checks
it in its `file` step (D09) before anything is written: at most 64 KiB, and an
SVG image — one well-formed XML document whose root element is `svg`. Icons
must be UTF-8: a leading byte-order mark is allowed, a declared other
encoding is not, and a duplicate attribute makes the document not well-formed.
The icon is still embedded byte for byte, mark included. The
same model names the services file and the variable every app's environment
carries to find it, and the `ikigenba` account whose group owns both the
installed trees and that file, so install, init, and the services package
agree on them.

The artifact and manifest inputs described here come from the supplied opsctl
stories. They do not assert the behavior or internal layout of another
sub-project. Command execution and lifecycle ordering belong to the consuming
designs.

## REQUIREMENTS

- R-YNP2-AKCZ: Package `internal/apps` MUST export `Database` as a struct with `Engine string` and `Path string` fields.
- R-YOWY-OC3O: Package `internal/apps` MUST export `Manifest` as a struct with `App string`, `Default bool`, `Secrets []string`, `Env map[string]string`, and `Database *Database` fields.
- R-YQ4V-23UD: Package `internal/apps` MUST export `Service` as a struct with `Name string`, `Manifest *Manifest`, and `ManifestError error` fields.
- R-A2TS-K4M0: Package `internal/apps` MUST export `ValidateName(name string) error`, returning nil exactly for a name of one through 63 ASCII letters, digits, or hyphens whose first and last characters are letters or digits and whose lowercase form is none of `host`, `deploy`, `backup-host`, `backup-services`, or `renew-certificate`; all other inputs MUST return an error identifying the unusable name.
- R-XSFU-AXFW: Package `internal/apps` MUST export `ParseManifest(data []byte) (Manifest, error)`, accepting a TOML document and returning an error for malformed TOML or an invalid recognized field rather than a partially usable manifest.
- R-U58J-I5I3: `ParseManifest` MUST map TOML `app`, `default`, `secrets`, `[env]`, and `[database]` to their correspondingly named `Manifest` fields, with database `engine` and `path` mapped to `Database.Engine` and `Database.Path`; absent fields MUST produce their Go zero values, absent `[database]` MUST produce nil `Database`, and absent `secrets` or `[env]` MUST be usable as empty collections.
- R-UDRU-6JOY: `ParseManifest` MUST require a present `app` to be a string accepted by `ValidateName`, a present `default` to be a Boolean, a present `secrets` to be an array of strings, and every `[env]` value to be a string; fields other than `app`, `default`, `secrets`, `[env]`, `[database]`, and `port` MUST not contribute to the returned model, and an omitted `app` MUST remain valid for a service whose manifest declares only other capabilities.
- R-U7OC-9OZH: `ParseManifest` MUST reject a document with a top-level `port` key, whatever its value or type, returning an error whose `Error()` is exactly `'port' is not allowed; the host gives the app its socket`; no app listens on a port, and the host hands every app its socket (D09).
- R-Y2OV-VJIN: When a document carries a top-level `port` key and also fails another `ParseManifest` rule, including an `app` value `ValidateName` rejects, `ParseManifest` MUST return the `port` error.
- R-XW3J-G8NZ: `ParseManifest` MUST require a present `[database]` to contain `engine = "sqlite"` and a string `path` naming a file strictly below `state/` relative to the service directory, rejecting absolute paths, empty path components, `.` or `..` components, and any other engine; decoding MUST not require the named database to exist or change its journal mode.
- R-XXBF-U0EO: Package `internal/apps` MUST export `Discover(root string) ([]Service, error)`, returning services in ascending bytewise `Name` order, one for each immediate directory `/opt/<name>/` holding an `etc/` or `state/` directory, resolving every filesystem access under `root`; a missing `/opt` MUST produce an empty successful result, and failure to enumerate `/opt` MUST return an error.
- R-XYJC-7S5D: For each service, `Discover` MUST read `/opt/<name>/etc/manifest.toml` when present and use `ParseManifest` to populate `Service.Manifest`; a missing manifest MUST leave both `Manifest` and `ManifestError` nil, while a read or decode failure MUST leave `Manifest` nil and populate `ManifestError` without dropping that service or failing discovery of the other services.
- R-XZR8-LJW2: `Discover` MUST derive `Service.Name` from the immediate directory name rather than a binary, unit, artifact filename, or manifest value, retaining services with only `state/` and no manifest, executable, or systemd unit; a successfully parsed manifest whose nonempty `App` differs from that directory name MUST instead be reported as a per-service `ManifestError` with nil `Manifest`.
- R-Y0Z4-ZBMR: `Discover`, `ParseManifest`, and `ValidateName` MUST perform no filesystem writes, subprocess execution, schema migration, or seed loading; the manifest carries no authoritative version, and the installed app's executable queried by the lifecycle/status consumer is the source of its reported version.
- R-YRCR-FVL2: Package `internal/apps` MUST export `Timeouts` as a struct with `DrainSeconds int64` and `StopSeconds int64` fields, and `ReadTimeouts(store config.Store) (Timeouts, error)`.
- R-UA45-18GV: The configuration keys `apps.drain_seconds` and `apps.stop_seconds` MUST be the space-wide app timing settings: `apps.drain_seconds` is how many whole seconds every app may spend draining once told to stop, and `apps.stop_seconds` is how many whole seconds systemd waits for every app's service to stop; an absent key or an empty value MUST mean `5` and `10` respectively, and no manifest field sets or overrides either.
- R-UBC1-F07K: `ReadTimeouts` MUST read both timing keys from `store` and return their values; a nonempty value MUST consist only of ASCII decimal digits, leading zeros permitted, denoting a whole number from 1 through 9223372036, and otherwise `ReadTimeouts` MUST return an error whose `Error()` is exactly `<key> is not a positive whole number of seconds: '<value>'` with the stored value substituted unchanged, judging `apps.drain_seconds` before `apps.stop_seconds` so only the first offending key is named; when both are valid and the stop value is not greater than the drain value it MUST return an error whose `Error()` is exactly `apps.stop_seconds (<S>) is not greater than apps.drain_seconds (<D>)`, with `<S>` and `<D>` the effective values in decimal without leading zeros, defaults included.
- R-UCJX-SRY9: `ReadTimeouts` MUST NOT write the store; a store read failure other than an absent key, including a corrupt store, MUST be returned as an error wrapping the store's error (so `errors.Is` matches `config.ErrCorrupt` for a corrupt store) rather than as a timing finding.
- R-8XR7-K2DV: Package `internal/apps` MUST export the constants `IconPath = "share/icon.svg"`, the service-relative path of a service's launcher icon; `ServicesPath = "/var/lib/ikigenba/services.json"`, the host path of the services file (D15); and `ServicesEnv = "IKIGENBA_SERVICES"`, the name of the environment variable that carries `ServicesPath` to every app.
- R-8YZ3-XU4K: Package `internal/apps` MUST export the sentinel errors `ErrIconNotSVG`, whose `Error()` is exactly `share/icon.svg is not an SVG image`, and `ErrIconTooLarge`, whose `Error()` is exactly `share/icon.svg is larger than 64 KiB`.
- R-9070-BLV9: Package `internal/apps` MUST export `CheckIcon(data []byte) error`.
- R-91EW-PDLY: `CheckIcon` MUST return `ErrIconTooLarge` exactly when `data` is longer than 65536 bytes, judging size before content, so 65536 bytes never fails for size, 65537 bytes always does, and data both too large and not an SVG image returns `ErrIconTooLarge`; for data of at most 65536 bytes it MUST return nil when `data` is an SVG image and otherwise `ErrIconNotSVG`, each returned error being the sentinel itself.
- R-M1NV-I2WL: For `CheckIcon`, `data` MUST be an *SVG image* exactly when all of these hold: it is valid UTF-8; after one leading UTF-8 byte-order mark (EF BB BF), when present, is set aside, an `encoding/xml` `Decoder` in its default strict mode with no `CharsetReader` reads the rest to end of input without error; the tokens hold exactly one top-level element, whose name's local part is `svg` in any namespace or none; no element carries two attributes with the same name (the same namespace and local part); and every character-data token outside that element holds only spaces, tabs, CRs, and LFs, while comments, processing instructions (the XML declaration included), and directives are allowed outside it. So empty data, text alone, a PNG, invalid UTF-8, an XML declaration naming an encoding other than UTF-8, unbalanced or unclosed tags, an undefined entity, a duplicate attribute, a root other than `svg`, a second top-level element, and text after the root element are each `ErrIconNotSVG`, while a BOM-prefixed SVG image is accepted.
- R-93UP-GX3C: `CheckIcon` MUST be a pure function of `data`: it MUST NOT read or write the filesystem, execute a process, read the environment or a clock, or perform network access.
- R-952L-UOU1: Package `internal/apps` MUST export `EnsureAccount(ctx context.Context, env host.Env) error`.
- R-96AI-8GKQ: `EnsureAccount` MUST ensure a non-root system account named `ikigenba` exists, with no login shell and no home directory created for it, whose primary group is named `ikigenba`: it MUST execute `id --user ikigenba` through `env.Execute`, treat exit status 1 as an absent account and then execute `useradd --system --no-create-home --shell /usr/sbin/nologin --user-group ikigenba`; for a present account it MUST fail when the reported uid is 0 and MUST execute `id --group --name ikigenba`, failing unless its output with surrounding whitespace removed is `ikigenba`, without changing the account's group. Any other execution error or nonzero exit MUST return an error wrapping `*host.CommandError`, and it MUST change nothing but the account.
