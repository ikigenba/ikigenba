# D11-backup-model

A service's ordinary files and its declared SQLite database have separate backup
owners. The backup package generates the host's shared Litestream configuration;
command orchestration determines when services stop or start. Replication period
edge cases (zero, empty, or absent periods on a host that declares databases)
remain outside the contract until a policy decision lands.

The bucket every replica points at is the account's one bucket, named after the
root domain, so its name has dots in it and cannot be addressed as a hostname
over TLS. The generated file therefore tells Litestream to address every
replica path-style (`force-path-style: true`), unconditionally: the emitted key
is part of the file's shape, not something derived from the bucket name.

Litestream prunes its own replicas: the generated file turns its retention on
and keeps snapshots for two and a half database periods, so three snapshots at
the daily period. The value follows from `backup.service_db_seconds`, or from
Litestream's own 24-hour snapshot default when that is unset, and has no
setting of its own. Litestream 0.5.17 reads `snapshot.retention` as a Go
duration, so it is written in seconds like the periods, a half second where the
period is odd, and a value longer than the longest Go duration is refused when
Litestream reads the file. How long Litestream keeps level-0 change files, and
how often it checks, stay its own defaults. The bucket's expiry remains as a
backstop, and a restore to a moment older than the oldest kept snapshot fails
as any failed `litestream restore` does (D14).

A declared database's `path` is relative to the service's data directory,
`/var/opt/ikigenba/<service>/`, the app's working directory (D08), so the
generated file names each database there, while the manifest that declares it
is read from the service's package directory (D08): on a released host the
manifests are those of the release `current` names, so an app a release
dropped, a data-only service, has no manifest and contributes no database,
and on a per-app host they are under `/opt/<service>/etc/`. Discovery is
unchanged by whether a service can run: backup and snapshot still take a
service known only by its data directory, while restore accepts only an app
of the release it restores against, or an installed app on a per-app host
(D14). A restore before the first activate has no `current`, so it generates
the file from the manifests of the release it runs from, the one operation
here that is handed its services rather than discovering them.

Neither services file, `/run/ikigenba/services.json` nor a per-app host's
`/var/lib/ikigenba/services.json`, is backed up: it is generated from the
store, the host's apps, and which apps are disabled, like the nginx
configuration and the unit files, so a restored host writes it again (D15,
R-9JTU-C7U1).

## REQUIREMENTS

- R-JKH0-KRY9: Package `internal/backup` MUST export `Regenerate(ctx context.Context, env host.Env, store config.Store) (changed bool, err error)`.
- R-JLOW-YJOY: Package `internal/backup` MUST export `SetupReplication(ctx context.Context, env host.Env, store config.Store) error`.
- R-JMWT-CBFN: Package `internal/backup` MUST export `DatabaseFiles(database apps.Database) []string`.
- R-JO4P-Q36C: For a valid database declaration, `DatabaseFiles` MUST return, in order, its service-relative `Path`, that path with `-wal` appended, that path with `-shm` appended, and the sibling directory whose basename is `.` followed by the database filename followed by `-litestream`; the fourth entry denotes the entire metadata directory subtree.
- R-HLWS-EFZW: `Regenerate` MUST use `apps.Discover` under `env.Root` and include exactly one database entry for each discovered service with a non-nil manifest database, including services without a binary or unit; absent manifests and manifests without databases contribute no entry, while a discovery failure MUST return an error identifying the affected service where available, and a non-nil `ManifestError` MUST return, for the first service in ascending name order whose `ManifestError` is non-nil, an error whose `Error()` is exactly `<name>: etc/manifest.toml: <reason>` when `apps.ParseManifest` rejects that service's manifest bytes, `<reason>` being that rejection's `Error()`, so an installed `repos` whose manifest holds `io_weight = 50` gives `repos: etc/manifest.toml: 'resources.io_weight' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy`, and otherwise an error identifying that service and its manifest failure, each before changing `/etc/litestream.yml`.
- R-D0O4-CTXV: Given a configured nonempty `aws.region`, a valid `backup.s3_uri` naming this host's S3 prefix, and decimal whole-second values from 1 through 9223372036 inclusive for `backup.service_db_seconds` and `backup.service_wal_seconds`, `Regenerate` MUST generate `/etc/litestream.yml` with top-level `region` equal to the region, `snapshot.interval` equal to the database period expressed in seconds, `sync-interval` equal to the WAL period expressed in seconds, and a `socket` block with `enabled` true, `path` equal to `/var/run/litestream.sock` resolved under `env.Root`, and `permissions` equal to `0600`; `dbs` entries MUST appear in service-name order with `path` equal to the service's declared database path resolved against its data directory (D08 R-CVSI-TQZ3), `/var/opt/ikigenba/<service>/`, and under `env.Root`, and a single `replica` map whose `url` is the host prefix followed by `<service>/` and whose `force-path-style` is `true`, emitted for every replica regardless of the bucket's name; it MUST emit an empty `dbs` sequence when no database is declared.
- R-8QV7-053Z: Every configuration produced by `Regenerate` MUST set top-level `retention.enabled` to true and `snapshot.retention` to two and a half database periods written in whole seconds, as `snapshot.interval` is (R-D0O4-CTXV): for a database period of `<P>` whole seconds, with `<Q>` the integer quotient of `5×<P>` by 2, written in decimal without leading zeros, the value MUST be `<Q>s` when `5×<P>` is even and `<Q>.5s` when it is odd, so `86400` gives `216000s`, `3600` gives `9000s`, and `1` gives `2.5s`; when `backup.service_db_seconds` is absent, empty, or zero, the value MUST be `216000s`, two and a half of Litestream's 24-hour default snapshot period, whether or not a database is declared, the rest of that case on a host declaring databases remaining outside the contract as R-RR46-VI3R states. `snapshot.retention` MUST appear in the one top-level `snapshot` map, beside `snapshot.interval` when that is set. It MUST NOT set `l0-retention`, `l0-retention-check-interval`, or any per-database snapshot or retention setting, MUST omit explicit credential values, and MUST name only the shared `litestream.service` as the replication unit through `SetupReplication`; it MUST NOT generate a replication unit per database or any one-shot replication schedule. A database period above 3689348814 whole seconds, whose two and a half periods exceed the longest duration Litestream 0.5.17 accepts, remains outside this contract pending a policy decision.
- R-JT0B-9654: `Regenerate` MUST produce identical bytes for identical database declarations, configured region, prefix, and periods, regardless of manifest enumeration order or unrelated manifest fields; it MUST leave an existing byte-identical configuration untouched and return `false, nil`, and otherwise replace the configuration completely and return `true, nil` only after the complete new file is visible, without exposing a partially written configuration.
- R-JU87-MXVT: `Regenerate` MUST limit its mutations to `/etc/litestream.yml` under `env.Root`, MUST NOT control a unit or read, create, modify, or delete database files, metadata, service data, or S3 objects, and MUST return configuration-read, configuration-write, or context-cancellation failures as errors without reporting a successful change.
- R-JVG4-0PMI: `SetupReplication` MUST call `Regenerate` and, only on success, execute `systemctl enable litestream.service` followed by `systemctl restart litestream.service` when configuration changed or `systemctl start litestream.service` when unchanged; it MUST return an error on an execution error or nonzero process exit and perform no later action after a failure, and MUST NOT roll back a successfully generated configuration on a subsequent unit failure.
- R-TLV0-1ZLH: The init setup sequence MUST invoke `SetupReplication` after nginx configuration and before timer setup, and a service restore of a declared database MUST regenerate the file from the manifests of the release it restores against or, on a per-app host, the installed manifests, through `Regenerate`, or, before the first activate, through `RegenerateServices` with what `apps.DiscoverRelease` returns for its release (D14), neither enabling or disabling `litestream.service`, with database restore regenerating while Litestream is stopped and starting it after successful database restoration even when configuration bytes were unchanged.
- R-GQJK-DQKP: Package `internal/backup` MUST export `RegenerateServices(ctx context.Context, env host.Env, store config.Store, services []apps.Service) (changed bool, err error)`.
- R-GRRG-RIBE: `RegenerateServices` MUST do what `Regenerate` does, every requirement of this design on `Regenerate`'s inputs, validation, errors, output bytes, and effects included, with `services` in place of the services `apps.Discover` returns and without calling `apps.Discover`, so `Regenerate(ctx, env, store)` and `RegenerateServices(ctx, env, store, s)` with `s` what `apps.Discover(env.Root)` returns give the same result; a service with a nil `Manifest`, a data-only service among them, MUST contribute no entry.
- R-RR46-VI3R: Before writing `/etc/litestream.yml`, `Regenerate` MUST reject a present nonempty replication period that contains anything other than decimal digits or whose whole-second value exceeds 9223372036, returning `false` and an error identifying the key; negative, fractional, malformed and overflowing values MUST therefore cause no file change. It MUST likewise reject a present nonempty `backup.s3_uri` unless it is an absolute `s3://` URI with nonempty bucket, no userinfo, port, opaque form, query or fragment, and no `.` or `..` object-prefix path component, returning `false` and an error identifying `backup.s3_uri` before file changes. Zero, empty or absent replication periods for hosts declaring databases, and empty or absent prefixes, remain outside this partial contract pending the recorded policy issue.

- R-1A4E-92DE: Before publishing replication configuration, `Regenerate` MUST reject a database-bearing discovered service whose directory name is empty, `.` or `..`, contains slash or NUL, or equals `host`, `deploy`, `snapshots`, or `seed`, returning an error identifying the service and leaving the previous configuration untouched; other service names MUST NOT be rejected solely because `apps.ValidateName` rejects them, since database-only services need no app unit.
- R-8TAZ-ROLD: When no discovered service declares a database, with a configured nonempty `aws.region` and valid `backup.s3_uri`, and both replication periods absent, empty, or zero, `Regenerate` MUST succeed with an empty `dbs` sequence, configured top-level `region`, `retention.enabled` true, `snapshot.retention` equal to `216000s` (R-8QV7-053Z), and no `snapshot.interval` or `sync-interval` setting; `SetupReplication` and `SetupTimers` with both file-backup periods zero MUST leave both backup timers disabled and stopped and MUST produce no S3 object, while preserving the always-enabled certificate renewal timer.
