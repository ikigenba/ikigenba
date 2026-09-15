# D10-host-setup

Shared host setup is consumed by create and init. Release selection is data
obtained at run time; configuration is nine derived keys plus optional ACME
email. Only published release assets and installed host commands cross the
opsctl boundary. Outstanding external observations are recorded alongside the
designs.

## REQUIREMENTS

- R-FXNQ-9MRO: Package `internal/hostsetup` MUST export `Release` with exactly `Version string` and `InstallerURL string`.

- R-G03J-1692: Package `internal/hostsetup` MUST export `Configure(ctx context.Context, h host.Host, props account.Properties, zone cloud.Zone, domain string, email *string) (int, error)`.

- R-G1BF-EXZR: Package `internal/hostsetup` MUST export `ReleasesURL = "https://api.github.com/repos/ikigenba/ikigenba/releases"`, `InstallerPath = "/tmp/opsctl-install"`, `SavedInstaller = "/usr/local/share/ikigenba/opsctl-install.sh"`, and `DNSProvider = "route53"`.

- R-YJ1K-MA17: `Latest` MUST obtain published releases through `Deps.Exec` running `curl -fsSL` against ReleasesURL with `per_page=100` and successive `page` values starting at 1, stopping at a page with fewer than 100 entries; it MUST select the non-draft, non-prerelease `opsctl/<valid version>` release with newest `published_at` (lexicographically first tag breaks a tie), and obtain its installer URL from the asset named `install.sh`. An absent matching release, missing installer asset, malformed response or request failure MUST cause an error; Latest MUST NOT fall back to an embedded pin.

- R-G3R8-6HH5: `InstallLatest` MUST select a release with `Latest`, fetch its InstallerURL on the host with `curl -fsSL -o InstallerPath`, run `sudo bash InstallerPath <version>`, and return that selected version on success; any failure MUST stop further work and use step `opsctl`. It MUST never inspect another project’s source or compile opsctl.

- R-G4Z4-K97U: `Upgrade` MUST invoke `sudo bash SavedInstaller <version>` through the host with step `opsctl`, even if the requested version equals the installed version, and MUST return its error unchanged; it MUST not discover a newer version or set configuration on installer failure.

- R-G670-Y0YJ: `Version` MUST invoke installed `sudo opsctl version` with step `opsctl` and carry its stdout, with trailing newlines removed, into the kept-version display; it MUST not validate, interpret or use that text to choose behavior or a release, and MUST return host execution errors unchanged.

- R-G7EX-BSP8: `Configure` MUST set exactly these nine keys with `sudo opsctl config set <key>=<value>`, in this order: `host.name` from domain, `dns.provider` from DNSProvider, `dns.zones` from zone.Name plus `:` plus zone.ID, `aws.region` from props.Region, `backup.s3_uri` as `s3://` plus props.BackupBucket plus `/` plus domain plus `/`, then `backup.host_files_seconds`, `backup.service_files_seconds`, `backup.service_db_seconds`, and `backup.service_wal_seconds` from the corresponding Properties fields rendered in decimal.

- R-G8MT-PKFX: With non-nil email, `Configure` MUST additionally set `acme.email` to the pointed-to value as the tenth key; with nil email it MUST leave that key untouched. It MUST leave every other host key untouched, stop on the first host failure, and return the number of successful key writes and any error.

- R-G9UQ-3C6M: `Latest` MUST use Deps.Dir as each curl command’s directory, return a `ProcessError` for a nonzero curl exit, and return an error wrapping process-start or JSON decode failure otherwise; release discovery MUST write nothing to the command’s stdout.

- R-GB2M-H3XB: Package `internal/hostsetup` MUST export `ProcessError` with exactly `Label string`, `Status int`, and `Stderr string`, implementing `Error() string` as `<Label>: exit status <Status>`, `Detail() string` as `seam.QuoteOutput(Stderr)`, and `ExitCode() int` as 1.

- R-GCAI-UVO0: Package `internal/hostsetup` MUST export `Latest(ctx context.Context, deps seam.Deps) (Release, error)`.

- R-GDIF-8NEP: Package `internal/hostsetup` MUST export `InstallLatest(ctx context.Context, h host.Host) (string, error)`.

- R-GEQB-MF5E: Package `internal/hostsetup` MUST export `Upgrade(ctx context.Context, h host.Host, version string) error`.

- R-GFY8-06W3: Package `internal/hostsetup` MUST export `Version(ctx context.Context, h host.Host) (string, error)`.
