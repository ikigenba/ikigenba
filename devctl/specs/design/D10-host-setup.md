# D10-host-setup

Shared host setup is the part of putting opsctl on a space's host that
`space create` (D07) and `space init` (D11) have in common, and the
configuration store operations that `apex` (D14) needs on top of them. It
lives in `internal/hostsetup` and reaches opsctl only through its published
interface: the release assets it publishes, and the installed commands run
over ssh through `host.Host` (D06).

Release selection is data obtained at run time. The newest published release
is found from the public releases listing, its installer is fetched onto the
host and run, and the version it names is what the `opsctl` step line
reports. No version is pinned anywhere in devctl; `space init --opsctl`
fetches the named release's installer from opsctl's documented release
download and runs it the same way. devctl never relies on anything the
installer leaves on the host besides the installed `opsctl`.

Configuration is the eleven keys the installed opsctl declares, and nothing
else. Five derive from the root, the zone, and the space: `host.name` is the
space domain, `dns.provider` is `route53`, `dns.zones` is the root and the
zone id, `aws.region` is the region, and `backup.s3_uri` is the bucket named
after the root followed by the space's label. Four are the backup periods,
which derive from nothing on the developer's side: create sets them to the
defaults declared here, and init leaves whatever the host holds. `acme.email`
is set when the caller supplies an address and left alone otherwise. The
inputs are plain values carried in one `Config`, so a caller cannot hand the
host an inconsistent pair. Every key is set with its own `sudo opsctl config
set KEY=VALUE`, in a fixed order, and the count of keys set is what the
callers print. The eleventh key, `host.apex`, is never touched by
`Configure`: it is changed only through the single-key operations, by `apex
set` and `apex clear`, and by create when it clears a restored store.

Three single-key operations expose the store's `set`, `del`, and `get` for
those callers. `get` distinguishes an unset key, which opsctl reports with
exit 1, from a value. Every other opsctl command a devctl command runs on a
host — `init`, `status`, `restart`, `disable`, `enable`, `retire`, `host
restore`, `cert obtain`, `nginx apply` — is not configuration and is issued directly through
`(host.Host).Sudo` by the design that owns the step, as D06, D07, and D13
already do; only `host.Host` and this package cross the opsctl boundary.
Failures of every operation here are the `*host.CommandError` D06 defines,
returned unchanged, so the diagnostic relay is the one D06 states.

## REQUIREMENTS

- R-FXNQ-9MRO: Package `internal/hostsetup` MUST export `Release` with exactly `Version string` and `InstallerURL string`.

- R-L6YF-GLM5: Package `internal/hostsetup` MUST export `ReleasesURL = "https://api.github.com/repos/ikigenba/ikigenba/releases"`, `DownloadURL = "https://github.com/ikigenba/ikigenba/releases/download"`, `InstallerPath = "/tmp/opsctl-install"`, and `DNSProvider = "route53"`.

- R-YJ1K-MA17: `Latest` MUST obtain published releases through `Deps.Exec` running `curl -fsSL` against ReleasesURL with `per_page=100` and successive `page` values starting at 1, stopping at a page with fewer than 100 entries; it MUST select the non-draft, non-prerelease `opsctl/<valid version>` release with newest `published_at` (lexicographically first tag breaks a tie), and obtain its installer URL from the asset named `install.sh`. An absent matching release, missing installer asset, malformed response or request failure MUST cause an error; Latest MUST NOT fall back to an embedded pin.

- R-G3R8-6HH5: `InstallLatest` MUST select a release with `Latest`, fetch its InstallerURL on the host with `curl -fsSL -o InstallerPath`, run `sudo bash InstallerPath <version>`, and return that selected version on success; any failure MUST stop further work and use step `opsctl`. It MUST never inspect another project’s source or compile opsctl.

- R-ZERJ-X644: `Upgrade` MUST fetch `<DownloadURL>/opsctl/<version>/install.sh` on the host with `curl -fsSL -o InstallerPath <that URL>` through `(host.Host).Run` with step `opsctl`, then run `sudo bash InstallerPath <version>` through `(host.Host).Sudo` with step `opsctl`, even if the requested version equals the installed version; the first failure MUST stop further work and be returned unchanged. It MUST not consult the releases listing, discover a newer version, set configuration, or run any file the installer left on the host.

- R-G670-Y0YJ: `Version` MUST invoke installed `sudo opsctl version` with step `opsctl` and carry its stdout, with trailing newlines removed, into the kept-version display; it MUST not validate, interpret or use that text to choose behavior or a release, and MUST return host execution errors unchanged.

- R-G9UQ-3C6M: `Latest` MUST use Deps.Dir as each curl command’s directory, return a `ProcessError` for a nonzero curl exit, and return an error wrapping process-start or JSON decode failure otherwise; release discovery MUST write nothing to the command’s stdout.

- R-GB2M-H3XB: Package `internal/hostsetup` MUST export `ProcessError` with exactly `Label string`, `Status int`, and `Stderr string`, implementing `Error() string` as `<Label>: exit status <Status>`, `Detail() string` as `seam.QuoteOutput(Stderr)`, and `ExitCode() int` as 1.

- R-GCAI-UVO0: Package `internal/hostsetup` MUST export `Latest(ctx context.Context, deps seam.Deps) (Release, error)`.

- R-GDIF-8NEP: Package `internal/hostsetup` MUST export `InstallLatest(ctx context.Context, h host.Host) (string, error)`.

- R-GEQB-MF5E: Package `internal/hostsetup` MUST export `Upgrade(ctx context.Context, h host.Host, version string) error`.

- R-GFY8-06W3: Package `internal/hostsetup` MUST export `Version(ctx context.Context, h host.Host) (string, error)`.

- R-5O2A-WNPM: Package `internal/hostsetup` MUST export the constants `KeyHostName = "host.name"`, `KeyHostApex = "host.apex"`, `KeyACMEEmail = "acme.email"`, `KeyAWSRegion = "aws.region"`, `KeyBackupS3URI = "backup.s3_uri"`, `KeyBackupServiceFilesSeconds = "backup.service_files_seconds"`, `KeyBackupHostFilesSeconds = "backup.host_files_seconds"`, `KeyBackupServiceDBSeconds = "backup.service_db_seconds"`, `KeyBackupServiceWALSeconds = "backup.service_wal_seconds"`, `KeyDNSProvider = "dns.provider"`, and `KeyDNSZones = "dns.zones"`.

- R-8B46-A2Q8: Package `internal/hostsetup` MUST export `BackupPeriods`, a struct whose fields are exactly `HostFilesSeconds int`, `ServiceFilesSeconds int`, `ServiceDBSeconds int`, and `ServiceWALSeconds int`.

- R-8CC2-NUGX: Package `internal/hostsetup` MUST export the constants `DefaultHostFilesSeconds = 86400`, `DefaultServiceFilesSeconds = 86400`, `DefaultServiceDBSeconds = 86400`, and `DefaultServiceWALSeconds = 300`, and the function `DefaultBackupPeriods() BackupPeriods`.

- R-8DJZ-1M7M: `DefaultBackupPeriods` MUST return a `BackupPeriods` whose `HostFilesSeconds`, `ServiceFilesSeconds`, `ServiceDBSeconds`, and `ServiceWALSeconds` are respectively `DefaultHostFilesSeconds`, `DefaultServiceFilesSeconds`, `DefaultServiceDBSeconds`, and `DefaultServiceWALSeconds`.

- R-8ERV-FDYB: Package `internal/hostsetup` MUST export `Config`, a struct whose fields are exactly `Root string`, `Region string`, `ZoneID string`, `Space spaceref.Space`, `Email string`, and `Periods *BackupPeriods`.

- R-8FZR-T5P0: Package `internal/hostsetup` MUST export `Configure(ctx context.Context, h host.Host, step string, cfg Config) (int, error)`.

- R-8H7O-6XFP: Package `internal/hostsetup` MUST export `SetKey(ctx context.Context, h host.Host, step, key, value string) error`, `DelKey(ctx context.Context, h host.Host, step, key string) error`, and `GetKey(ctx context.Context, h host.Host, step, key string) (string, bool, error)`.

- R-5PA7-AFGB: `Configure` MUST set its keys through one call to `(h).Sudo` per key, each with the step `step` and exactly the arguments `opsctl`, `config`, `set`, and `<key>=<value>`, and MUST issue them in this order: `KeyHostName`, `KeyDNSProvider`, `KeyDNSZones`, `KeyAWSRegion`, `KeyBackupS3URI`, then, only when `cfg.Periods` is not nil, `KeyBackupHostFilesSeconds`, `KeyBackupServiceFilesSeconds`, `KeyBackupServiceDBSeconds`, and `KeyBackupServiceWALSeconds`, then, only when `cfg.Email` is not empty, `KeyACMEEmail`, and MUST NOT make any other `Deps.Exec` or `Deps.Stream` call.

- R-8JNG-YGX3: `Configure` MUST derive the five derived values as `KeyHostName` = `cfg.Space.Domain`, `KeyDNSProvider` = `DNSProvider`, `KeyDNSZones` = `cfg.Root`, `:`, and `cfg.ZoneID` joined, `KeyAWSRegion` = `cfg.Region`, and `KeyBackupS3URI` = `s3://`, `cfg.Root`, `/`, `cfg.Space.Label`, and `/` joined, verified at least by reproducing the arguments `host.name=sbx1.ikigenba.dev`, `dns.provider=route53`, `dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78`, `aws.region=us-east-2`, and `backup.s3_uri=s3://ikigenba.dev/sbx1/` for a `Config` whose `Root` is `ikigenba.dev`, `Region` is `us-east-2`, `ZoneID` is `Z09565073GHK8BYWQ1A78`, and `Space` is the label `sbx1` under that root.

- R-8KVD-C8NS: When `cfg.Periods` is not nil, `Configure` MUST set the four period keys to the corresponding `BackupPeriods` fields rendered as decimal integers, verified at least by reproducing `backup.host_files_seconds=86400`, `backup.service_files_seconds=86400`, `backup.service_db_seconds=86400`, and `backup.service_wal_seconds=300` for `DefaultBackupPeriods()`; when `cfg.Email` is not empty it MUST set `KeyACMEEmail` to `cfg.Email` verbatim.

- R-8M39-Q0EH: `Configure` MUST return, with a nil error, the number of keys it set when every set exited 0 — verified at least by returning 10 for a `Config` with non-nil `Periods` and non-empty `Email`, 5 for one with nil `Periods` and empty `Email`, and 6 for one with nil `Periods` and non-empty `Email` — and on the first failing set it MUST issue no further command and return the number of keys already set together with that call's error unchanged.

- R-5QI3-O770: `Configure`, `InstallLatest`, `Upgrade`, and `Version` MUST NOT issue any command whose arguments name `KeyHostApex`; `host.apex` MUST be set or removed only through `SetKey` and `DelKey`, called by the command designs that own the step (`apex set` and `apex clear` in D14, and create after `opsctl host restore` in D07).

- R-8OJ2-HJVV: `SetKey` MUST make exactly one call to `(h).Sudo` with the step `step` and exactly the arguments `opsctl`, `config`, `set`, and `key`, `=`, and `value` joined, discard its output, and return its error unchanged, so that a failure is the `*host.CommandError` D06 defines.

- R-8PQY-VBMK: `DelKey` MUST make exactly one call to `(h).Sudo` with the step `step` and exactly the arguments `opsctl`, `config`, `del`, and `key`, discard its output, and return its error unchanged; it MUST NOT treat a zero exit for a key that was not set as anything but success.

- R-8QYV-93D9: `GetKey` MUST make exactly one call to `(h).Sudo` with the step `step` and exactly the arguments `opsctl`, `config`, `get`, and `key`; on exit 0 it MUST return that call's `Output.Stdout` with trailing newline characters removed, `true`, and a nil error; when the call returns a `*host.CommandError` whose `Status` is 1 it MUST return an empty string, `false`, and a nil error, since the installed opsctl documents exit 1 for `config get` as the key not being set; any other error MUST be returned unchanged with an empty string and `false`.

- R-8TEO-0MUN: `Configure`, `SetKey`, `DelKey`, and `GetKey` MUST NOT parse, validate, or interpret the standard output or standard error of any opsctl command beyond what R-8QYV-93D9 states, and MUST NOT alter the bytes of an `Output` or `*host.CommandError` they return.

- R-8UMK-EELC: Package `internal/hostsetup` MUST import neither `internal/cloud` nor `internal/cloud/awssdk`: every value it writes to the host arrives as a plain string or a `spaceref.Space` in `Config`, verified by a test that inspects the package's imports.
