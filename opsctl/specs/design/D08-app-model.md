# D08-app-model

The apps package gives installation, status, nginx generation, and backup one
view of the services on the host. A directory can hold useful service data
without an installed executable. Its manifest contributes routing and database
declarations independently of whether its service can run.

The artifact and manifest inputs described here come from the supplied opsctl
stories. They do not assert the behavior or internal layout of another
sub-project. Command execution and lifecycle ordering belong to the consuming
designs.

## REQUIREMENTS

- R-XMCC-E2QF: Package `internal/apps` MUST own the shared service discovery, app-name validation, and manifest decoding contract consumed by `internal/cli`, `internal/nginx`, and `internal/backup`; discovery and decoding MUST use host-local inputs without any registration database or remote deployment record.
- R-XNK8-RUH4: Package `internal/apps` MUST export `Database` as a struct with exactly `Engine string` and `Path string` fields.
- R-XOS5-5M7T: Package `internal/apps` MUST export `Manifest` as a struct with exactly `App string`, `Port int`, `Default bool`, `Secrets []string`, `Env map[string]string`, and `Database *Database` fields.
- R-XQ01-JDYI: Package `internal/apps` MUST export `Service` as a struct with exactly `Name string`, `Manifest *Manifest`, and `ManifestError error` fields.
- R-A2TS-K4M0: Package `internal/apps` MUST export `ValidateName(name string) error`, returning nil exactly for a name of one through 63 ASCII letters, digits, or hyphens whose first and last characters are letters or digits and whose lowercase form is none of `host`, `deploy`, `backup-host`, `backup-services`, or `renew-certificate`; all other inputs MUST return an error identifying the unusable name.
- R-XSFU-AXFW: Package `internal/apps` MUST export `ParseManifest(data []byte) (Manifest, error)`, accepting a TOML document and returning an error for malformed TOML or an invalid recognized field rather than a partially usable manifest.
- R-XTNQ-OP6L: `ParseManifest` MUST map TOML `app`, `port`, `default`, `secrets`, `[env]`, and `[database]` to their correspondingly named `Manifest` fields, with database `engine` and `path` mapped to `Database.Engine` and `Database.Path`; absent fields MUST produce their Go zero values, absent `[database]` MUST produce nil `Database`, and absent `secrets` or `[env]` MUST be usable as empty collections.
- R-XUVN-2GXA: `ParseManifest` MUST require a present `app` to be a string accepted by `ValidateName`, a present `port` to be an integer from 1 through 65535, a present `default` to be a Boolean, a present `secrets` to be an array of strings, and every `[env]` value to be a string; unrelated fields MUST not contribute to the returned model, and omitted `app` and `port` MUST remain valid for services whose manifest declares only other capabilities.
- R-XW3J-G8NZ: `ParseManifest` MUST require a present `[database]` to contain `engine = "sqlite"` and a string `path` naming a file strictly below `state/` relative to the service directory, rejecting absolute paths, empty path components, `.` or `..` components, and any other engine; decoding MUST not require the named database to exist or change its journal mode.
- R-XXBF-U0EO: Package `internal/apps` MUST export `Discover(root string) ([]Service, error)`, returning services in ascending bytewise `Name` order, one for each immediate directory `/opt/<name>/` holding an `etc/` or `state/` directory, resolving every filesystem access under `root`; a missing `/opt` MUST produce an empty successful result, and failure to enumerate `/opt` MUST return an error.
- R-XYJC-7S5D: For each service, `Discover` MUST read `/opt/<name>/etc/manifest.toml` when present and use `ParseManifest` to populate `Service.Manifest`; a missing manifest MUST leave both `Manifest` and `ManifestError` nil, while a read or decode failure MUST leave `Manifest` nil and populate `ManifestError` without dropping that service or failing discovery of the other services.
- R-XZR8-LJW2: `Discover` MUST derive `Service.Name` from the immediate directory name rather than a binary, unit, artifact filename, or manifest value, retaining services with only `state/` and no manifest, executable, or systemd unit; a successfully parsed manifest whose nonempty `App` differs from that directory name MUST instead be reported as a per-service `ManifestError` with nil `Manifest`.
- R-Y0Z4-ZBMR: `Discover`, `ParseManifest`, and `ValidateName` MUST perform no filesystem writes, subprocess execution, schema migration, or seed loading; the manifest carries no authoritative version, and the installed app's executable queried by the lifecycle/status consumer is the source of its reported version.
