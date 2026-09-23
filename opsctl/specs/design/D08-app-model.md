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

The artifact and manifest inputs described here come from the supplied opsctl
stories. They do not assert the behavior or internal layout of another
sub-project. Command execution and lifecycle ordering belong to the consuming
designs.

## REQUIREMENTS

- R-XMCC-E2QF: Package `internal/apps` MUST own the shared service discovery, app-name validation, and manifest decoding contract consumed by `internal/cli`, `internal/nginx`, and `internal/backup`; discovery and decoding MUST use host-local inputs without any registration database or remote deployment record.
- R-XNK8-RUH4: Package `internal/apps` MUST export `Database` as a struct with exactly `Engine string` and `Path string` fields.
- R-U40N-4DRE: Package `internal/apps` MUST export `Manifest` as a struct with exactly `App string`, `Default bool`, `Secrets []string`, `Env map[string]string`, and `Database *Database` fields.
- R-XQ01-JDYI: Package `internal/apps` MUST export `Service` as a struct with exactly `Name string`, `Manifest *Manifest`, and `ManifestError error` fields.
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

- R-U8W8-NGQ6: Package `internal/apps` MUST export `Timeouts` as a struct with exactly `DrainSeconds int64` and `StopSeconds int64` fields, and `ReadTimeouts(store config.Store) (Timeouts, error)`.
- R-UA45-18GV: The configuration keys `apps.drain_seconds` and `apps.stop_seconds` MUST be the space-wide app timing settings: `apps.drain_seconds` is how many whole seconds every app may spend draining once told to stop, and `apps.stop_seconds` is how many whole seconds systemd waits for every app's service to stop; an absent key or an empty value MUST mean `5` and `10` respectively, and no manifest field sets or overrides either.
- R-UBC1-F07K: `ReadTimeouts` MUST read both timing keys from `store` and return their values; a nonempty value MUST consist only of ASCII decimal digits, leading zeros permitted, denoting a whole number from 1 through 9223372036, and otherwise `ReadTimeouts` MUST return an error whose `Error()` is exactly `<key> is not a positive whole number of seconds: '<value>'` with the stored value substituted unchanged, judging `apps.drain_seconds` before `apps.stop_seconds` so only the first offending key is named; when both are valid and the stop value is not greater than the drain value it MUST return an error whose `Error()` is exactly `apps.stop_seconds (<S>) is not greater than apps.drain_seconds (<D>)`, with `<S>` and `<D>` the effective values in decimal without leading zeros, defaults included.
- R-UCJX-SRY9: `ReadTimeouts` MUST NOT write the store; a store read failure other than an absent key, including a corrupt store, MUST be returned as an error wrapping the store's error (so `errors.Is` matches `config.ErrCorrupt` for a corrupt store) rather than as a timing finding.
