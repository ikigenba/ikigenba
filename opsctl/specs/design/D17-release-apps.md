# D17-release-apps

On a released host (D16) every app runs from the release `current` names, and
what it is given is host configuration generated from that release: its
environment file and its two units. They are written by `activate` and
`rollback` (D18), by `init`'s `apps` step on a released host, and, for the
environment file, by `restore`; each of those composes the operations below, so
an app's files never depend on which command wrote them last. Package
`internal/apps` owns them, beside the per-app host's files (D09).

The environment file is the app's secrets from the parameter
`/<host.name>/<app>`, its manifest's `[env]`, `DRAIN_SECONDS`, the services
file's variable, and the release's identity: `IKIGENBA_COMMIT` always, and
`IKIGENBA_RELEASE` only when the release has a label. Secrets and settings are
encoded as on a per-app host (D09), a double-quoted value with `\` and `"`
escaped, which systemd.exec(5) (`EnvironmentFile=`) reads back as the literal
value; the four host-set lines are bare. Only the secrets need the cloud, so
reading them is its own operation and rendering the file is pure.

The service unit names `current` rather than a release, so moving the link
changes which binary every app runs and a restart makes it run.
`RuntimeDirectory=ikigenba/<app>` gives the app `/run/ikigenba/<app>/`, created
at start, owned by the unit's user, and removed at stop, and `PrivateTmp=yes`
gives it a private `/tmp` (systemd.exec(5)); the listening socket stays
`/run/ikigenba/<app>.sock`, held by the socket unit `WriteReleaseUnits` writes. Every
app's service also wants and is ordered after `ikigenba-services.service`, the
oneshot that writes `/run/ikigenba/services.json` at boot (D15), so the file is
there whenever an app starts, though an app still starts if the oneshot fails
(systemd.unit(5), `Wants=` and `After=`; systemd.service(5), `Type=oneshot`
counts as started once its process exits, and `RemainAfterExit=yes` keeps it
active, so it runs once per boot).

## REQUIREMENTS

- R-A9FQ-DEEM: Package `internal/apps` MUST export the constant `ServicesUnit = "ikigenba-services.service"`, the unit that writes the services file at boot.
- R-AANM-R65B: Package `internal/apps` MUST export `ReadSecrets(ctx context.Context, remote cloud.Env, region, hostName string, m Manifest) (map[string]string, error)`.
- R-ABVJ-4XW0: `ReadSecrets` MUST return an empty map, opening no cloud client, when `m.Secrets` is empty; otherwise it MUST open `remote` with `region`, read the parameter `/<hostName>/<m.App>` through `Client.ReadSecrets` once, treat a parameter that does not exist (an error matching `cloud.ErrNotFound`) as holding no keys, and return a map holding exactly the distinct names `m.Secrets` lists with their values, an empty present value counting as present; when a listed name is absent it MUST return an error whose `Error()` is exactly `<app>: no value for '<name>' in /<hostName>/<app>` for the first absent name in manifest order, and any other open or read failure MUST be returned as an error wrapping it. It MUST NOT put a value in any error.
- R-AD3F-IPMP: Package `internal/apps` MUST export `ReleaseEnv(m Manifest, secrets map[string]string, t Timeouts, r release.Release) ([]byte, error)`.
- R-AEBB-WHDE: `ReleaseEnv` MUST return an error identifying `m.App` and the offending name, and holding no value, when a name `m.Secrets` lists or a key of `m.Env` does not match `[A-Za-z_][A-Za-z0-9_]*`, is `DRAIN_SECONDS`, `PORT`, `IKIGENBA_SERVICES`, `IKIGENBA_COMMIT`, or `IKIGENBA_RELEASE`, is both a listed secret and a key of `m.Env`, or has a value holding NUL, CR, or LF.
- R-AFJ8-A943: Otherwise `ReleaseEnv` MUST return exactly these LF-terminated lines and nothing else: for each distinct name in `m.Secrets` in manifest order, then for each key of `m.Env` in ascending bytewise order, the name, `="`, the value with every `\` written `\\` and every `"` written `\"`, and `"`; then `DRAIN_SECONDS=` and `t.DrainSeconds` in decimal without leading zeros; then `IKIGENBA_SERVICES=/run/ikigenba/services.json` (`ServicesEnv`, `=`, `ServicesPath`); then `IKIGENBA_COMMIT=` and `r.SHA`; then, exactly when `r.Label` is nonempty, `IKIGENBA_RELEASE=` and `r.Label`. So a manifest listing no secrets and no `[env]`, with drain 5 and `Release{SHA: "c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18", Label: "r142"}`, gives `DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/run/ikigenba/services.json\nIKIGENBA_COMMIT=c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18\nIKIGENBA_RELEASE=r142\n`, and the same with an empty `Label` lacks the last line.
- R-AGR4-O0US: Package `internal/apps` MUST export `WriteEnv(env host.Env, app string, data []byte) error`.
- R-SV82-4IER: `WriteEnv` MUST leave, under `env.Root`, `/etc/opt/`, `/etc/opt/ikigenba/` (`EnvRoot`), and the app's environment directory `/etc/opt/ikigenba/<app>/` present, each it creates owned by user `root` and group `root` (`root:root`) with its mode set to `0755` directly under `env.Root` whatever the process umask, so a test observes it with `Lstat`, and each that already exists unchanged in ownership and mode; it MUST then replace the app's environment file `/etc/opt/ikigenba/<app>/env` whole with exactly `data`, owned `root:root` with mode `0600` set directly under `env.Root`, by writing a temporary file in the environment directory and renaming it over the destination, never editing the previous file in place, leaving no temporary file behind, creating nothing else, and executing no command, `chown` included, so the ownership is that of the root process that writes. `WriteEnv` MUST reject a name `ValidateName` rejects before touching the filesystem.
- R-AJ6X-FKC6: Package `internal/apps` MUST export `WriteReleaseUnits(env host.Env, app string, m Manifest, t Timeouts) error` and `WriteServicesUnit(env host.Env) error`.
- R-ALMQ-73TK: `WriteReleaseUnits` MUST replace `/etc/systemd/system/ikigenba-<app>.socket` under `env.Root` whole with exactly `"[Unit]\nDescription=Ikigenba <app> socket\n\n[Socket]\nListenStream=/run/ikigenba/<app>.sock\nSocketUser=ikigenba\nSocketGroup=nginx\nSocketMode=0660\nRemoveOnStop=yes\nBacklog=4096\n\n[Install]\nWantedBy=sockets.target\n"`, the socket path resolved under `env.Root` as D01's root boundary resolves every host path, and `/etc/systemd/system/ikigenba-<app>.service` whole with exactly `"[Unit]\nDescription=Ikigenba <app> app\nRequires=ikigenba-<app>.socket\nAfter=ikigenba-<app>.socket ikigenba-services.service\nWants=ikigenba-services.service\n\n[Service]\nType=notify\nExecStart={C}/bin/<app>\nWorkingDirectory={D}\nEnvironmentFile={E}\nUser=ikigenba\nRuntimeDirectory=ikigenba/<app>\nPrivateTmp=yes\nRestart=on-failure\nTimeoutStopSec={T}\n{R}\n[Install]\nWantedBy=multi-user.target\n"`, where `\n` is one LF byte, `{C}` is `/opt/ikigenba/current/<app>`, `{D}` the app's data directory `/var/opt/ikigenba/<app>` and `{E}` its environment file `/etc/opt/ikigenba/<app>/env`, each resolved under `env.Root`, `{T}` is `t.StopSeconds` in decimal without leading zeros, and `{R}` is, for `m.Resources`, `Slice=ikigenba-<s>.slice\n` with `<s>` its `Slice`, then `CPUWeight=<c>\n`, then `MemoryMax=<m>\n` with its `MemoryMax` byte count, then `MemoryLow=32M\n` exactly when `Slice` is `core`, then `Environment=GOMEMLIMIT=<g>\n` with its `GoMemoryLimit` byte count, then `Delegate=yes\n` exactly when `Delegate` is true, then `OOMPolicy=continue\n` exactly when `OOMPolicy` is `continue`, every number in decimal without leading zeros; both files MUST end with mode `0644`, and neither MAY hold any other line, `IOWeight=` and `KillMode=` included.
- R-AMUM-KVK9: `WriteServicesUnit` MUST replace `/etc/systemd/system/ikigenba-services.service` under `env.Root` whole with exactly `"[Unit]\nDescription=Ikigenba services file\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart={O} services apply\n\n[Install]\nWantedBy=multi-user.target\n"`, `{O}` being `/usr/local/bin/opsctl` resolved under `env.Root`, with mode `0644`.
- R-AO2I-YNAY: `WriteReleaseUnits` and `WriteServicesUnit` MUST write each file by renaming a complete temporary file in `/etc/systemd/system/` over it, leave no temporary file behind, create `/etc/systemd/system/` with mode `0755` when absent, execute no command (reloading and enabling are their callers'), and change no other file; `WriteReleaseUnits` MUST reject a name `ValidateName` rejects before touching the filesystem.
- R-APAF-CF1N: `ReadSecrets` and `ReleaseEnv` MUST NOT write the filesystem or execute a process, and `ReleaseEnv` MUST be a pure function of its arguments.
