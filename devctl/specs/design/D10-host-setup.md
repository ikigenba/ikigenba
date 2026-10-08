# D10-host-setup

Shared host setup is the part of putting opsctl on a space's host that
`space create` (D07) and `space init` (D11) have in common, and the
configuration store operations that `apex` (D14) needs on top of them. It
lives in `internal/hostsetup` and reaches opsctl only through its published
interface: the release assets it publishes, and the installed commands run
over ssh through `host.Host` (D06).

A host gets its opsctl in one of two ways. A space created from a release
(D07) runs the opsctl inside that release, by its absolute path, from the
moment the release is unpacked, and every later activate brings its own; no
published opsctl is fetched. `space init --opsctl` (D11), which stays until
the per-app form goes away and is not used on a host that runs a release,
fetches the named release's installer from opsctl's documented release
download onto the host and runs it. No version is pinned anywhere in devctl,
and devctl never relies on anything the installer leaves on the host besides
the installed `opsctl`.

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
callers print. `Config.Opsctl` names the opsctl the keys are set with: empty,
the `opsctl` on root's PATH, as `space init` uses it; for `space create`, the
release's own, by absolute path. The eleventh key, `host.apex`, is never touched by
`Configure`: in this package only the single-key operations change it, for
`apex set` and `apex clear`. The one exception is outside the package: create,
clearing a restored store, deletes it with a direct `config del` run by the
release's own opsctl, since the single-key operations run the `opsctl` on
root's PATH, which a fresh host does not have yet.

Three single-key operations expose the store's `set`, `del`, and `get` for
`apex`, always through the `opsctl` on root's PATH. `get` distinguishes an unset key, which opsctl reports with
exit 1, from a value. Every other opsctl command a devctl command runs on a
host — `init`, `status`, `restart`, `disable`, `enable`, `retire`, `host
restore`, `cert obtain`, `nginx apply` — is not configuration and is issued directly through
`(host.Host).Sudo` by the design that owns the step, as D06, D07, and D13
already do; only `host.Host` and this package cross the opsctl boundary.
Failures of every operation here are the `*host.CommandError` D06 defines,
returned unchanged, so the diagnostic relay is the one D06 states.

## REQUIREMENTS

- R-ZERJ-X644: `Upgrade` MUST fetch `<DownloadURL>/opsctl/<version>/install.sh` on the host with `curl -fsSL -o InstallerPath <that URL>` through `(host.Host).Run` with step `opsctl`, then run `sudo bash InstallerPath <version>` through `(host.Host).Sudo` with step `opsctl`, even if the requested version equals the installed version; the first failure MUST stop further work and be returned unchanged. It MUST not consult the releases listing, discover a newer version, set configuration, or run any file the installer left on the host.

- R-G670-Y0YJ: `Version` MUST invoke installed `sudo opsctl version` with step `opsctl` and carry its stdout, with trailing newlines removed, into the kept-version display; it MUST not validate, interpret or use that text to choose behavior or a release, and MUST return host execution errors unchanged.

- R-GEQB-MF5E: Package `internal/hostsetup` MUST export `Upgrade(ctx context.Context, h host.Host, version string) error`.

- R-GFY8-06W3: Package `internal/hostsetup` MUST export `Version(ctx context.Context, h host.Host) (string, error)`.

- R-5O2A-WNPM: Package `internal/hostsetup` MUST export the constants `KeyHostName = "host.name"`, `KeyHostApex = "host.apex"`, `KeyACMEEmail = "acme.email"`, `KeyAWSRegion = "aws.region"`, `KeyBackupS3URI = "backup.s3_uri"`, `KeyBackupServiceFilesSeconds = "backup.service_files_seconds"`, `KeyBackupHostFilesSeconds = "backup.host_files_seconds"`, `KeyBackupServiceDBSeconds = "backup.service_db_seconds"`, `KeyBackupServiceWALSeconds = "backup.service_wal_seconds"`, `KeyDNSProvider = "dns.provider"`, and `KeyDNSZones = "dns.zones"`.

- R-8B46-A2Q8: Package `internal/hostsetup` MUST export `BackupPeriods`, a struct whose fields are exactly `HostFilesSeconds int`, `ServiceFilesSeconds int`, `ServiceDBSeconds int`, and `ServiceWALSeconds int`.

- R-8CC2-NUGX: Package `internal/hostsetup` MUST export the constants `DefaultHostFilesSeconds = 86400`, `DefaultServiceFilesSeconds = 86400`, `DefaultServiceDBSeconds = 86400`, and `DefaultServiceWALSeconds = 300`, and the function `DefaultBackupPeriods() BackupPeriods`.

- R-8DJZ-1M7M: `DefaultBackupPeriods` MUST return a `BackupPeriods` whose `HostFilesSeconds`, `ServiceFilesSeconds`, `ServiceDBSeconds`, and `ServiceWALSeconds` are respectively `DefaultHostFilesSeconds`, `DefaultServiceFilesSeconds`, `DefaultServiceDBSeconds`, and `DefaultServiceWALSeconds`.

- R-8FZR-T5P0: Package `internal/hostsetup` MUST export `Configure(ctx context.Context, h host.Host, step string, cfg Config) (int, error)`.

- R-8H7O-6XFP: Package `internal/hostsetup` MUST export `SetKey(ctx context.Context, h host.Host, step, key, value string) error`, `DelKey(ctx context.Context, h host.Host, step, key string) error`, and `GetKey(ctx context.Context, h host.Host, step, key string) (string, bool, error)`.

- R-8JNG-YGX3: `Configure` MUST derive the five derived values as `KeyHostName` = `cfg.Space.Domain`, `KeyDNSProvider` = `DNSProvider`, `KeyDNSZones` = `cfg.Root`, `:`, and `cfg.ZoneID` joined, `KeyAWSRegion` = `cfg.Region`, and `KeyBackupS3URI` = `s3://`, `cfg.Root`, `/`, `cfg.Space.Label`, and `/` joined, verified at least by reproducing the arguments `host.name=sbx1.ikigenba.dev`, `dns.provider=route53`, `dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78`, `aws.region=us-east-2`, and `backup.s3_uri=s3://ikigenba.dev/sbx1/` for a `Config` whose `Root` is `ikigenba.dev`, `Region` is `us-east-2`, `ZoneID` is `Z09565073GHK8BYWQ1A78`, and `Space` is the label `sbx1` under that root.

- R-8KVD-C8NS: When `cfg.Periods` is not nil, `Configure` MUST set the four period keys to the corresponding `BackupPeriods` fields rendered as decimal integers, verified at least by reproducing `backup.host_files_seconds=86400`, `backup.service_files_seconds=86400`, `backup.service_db_seconds=86400`, and `backup.service_wal_seconds=300` for `DefaultBackupPeriods()`; when `cfg.Email` is not empty it MUST set `KeyACMEEmail` to `cfg.Email` verbatim.

- R-8M39-Q0EH: `Configure` MUST return, with a nil error, the number of keys it set when every set exited 0 — verified at least by returning 10 for a `Config` with non-nil `Periods` and non-empty `Email`, 5 for one with nil `Periods` and empty `Email`, and 6 for one with nil `Periods` and non-empty `Email` — and on the first failing set it MUST issue no further command and return the number of keys already set together with that call's error unchanged.

- R-8OJ2-HJVV: `SetKey` MUST make exactly one call to `(h).Sudo` with the step `step` and exactly the arguments `opsctl`, `config`, `set`, and `key`, `=`, and `value` joined, discard its output, and return its error unchanged, so that a failure is the `*host.CommandError` D06 defines.

- R-8PQY-VBMK: `DelKey` MUST make exactly one call to `(h).Sudo` with the step `step` and exactly the arguments `opsctl`, `config`, `del`, and `key`, discard its output, and return its error unchanged; it MUST NOT treat a zero exit for a key that was not set as anything but success.

- R-8QYV-93D9: `GetKey` MUST make exactly one call to `(h).Sudo` with the step `step` and exactly the arguments `opsctl`, `config`, `get`, and `key`; on exit 0 it MUST return that call's `Output.Stdout` with trailing newline characters removed, `true`, and a nil error; when the call returns a `*host.CommandError` whose `Status` is 1 it MUST return an empty string, `false`, and a nil error, since the installed opsctl documents exit 1 for `config get` as the key not being set; any other error MUST be returned unchanged with an empty string and `false`.

- R-8TEO-0MUN: `Configure`, `SetKey`, `DelKey`, and `GetKey` MUST NOT parse, validate, or interpret the standard output or standard error of any opsctl command beyond what R-8QYV-93D9 states, and MUST NOT alter the bytes of an `Output` or `*host.CommandError` they return.

- R-WFMH-V6LF: Package `internal/hostsetup` MUST export `DownloadURL = "https://github.com/ikigenba/ikigenba/releases/download"`, `InstallerPath = "/tmp/opsctl-install"`, and `DNSProvider = "route53"`.

- R-WGUE-8YC4: Package `internal/hostsetup` MUST export `Config`, a struct whose fields are exactly `Root string`, `Region string`, `ZoneID string`, `Space spaceref.Space`, `Email string`, `Periods *BackupPeriods`, and `Opsctl string`.

- R-WI2A-MQ2T: `Configure` MUST set its keys through one call to `(h).Sudo` per key, each with the step `step` and exactly the arguments `<program>`, `config`, `set`, and `<key>=<value>`, where `<program>` is `cfg.Opsctl` when it is not empty and `opsctl` when it is, and MUST issue them in this order: `KeyHostName`, `KeyDNSProvider`, `KeyDNSZones`, `KeyAWSRegion`, `KeyBackupS3URI`, then, only when `cfg.Periods` is not nil, `KeyBackupHostFilesSeconds`, `KeyBackupServiceFilesSeconds`, `KeyBackupServiceDBSeconds`, and `KeyBackupServiceWALSeconds`, then, only when `cfg.Email` is not empty, `KeyACMEEmail`, and MUST NOT make any other `Deps.Exec` or `Deps.Stream` call; verified at least by the remote argument vector `sudo opsctl config set host.name=sbx1.ikigenba.dev` for an empty `Opsctl` and `sudo /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl config set host.name=sbx1.ikigenba.dev` for that `Opsctl`.

- R-8GWZ-UVC6: `Configure`, `Upgrade`, and `Version` MUST NOT issue any command whose arguments name `KeyHostApex`; `SetKey` and `DelKey`, called by `apex set` and `apex clear` (D14), MUST be the only operations of this package that set or remove `host.apex`; the one other command that changes it is outside this package: `space create`'s `restore` step (D07) removes it with a direct `(host.Host).Sudo` of `config del host.apex` run by its release's opsctl, which `DelKey`, always running the `opsctl` on root's PATH, cannot run.
