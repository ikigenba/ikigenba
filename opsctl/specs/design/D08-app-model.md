# D08-app-model

The apps package gives installation, status, nginx generation, and backup one
view of the services on the host. A directory can hold useful service data
without an installed executable. Its manifest contributes routing and database
declarations independently of whether its service can run.

A manifest names its app, whether it is the host's default, its secrets, its
plain settings and its database. It may also carry a `description`, one line
of text saying what the app offers, and `mcp`, which is `false` unless set:
`true` puts the app's tools in the suite's MCP catalog. The services file
(D15) carries both for every installed app, so readers learn them without
parsing manifests. It may also carry `guests`, likewise `false` unless set:
`true` says the app serves guests, visitors with no credential, which only
nginx generation (D06) acts on. `guests` is in no command's output and not in
the services file. A `guests` that is not a Boolean is refused like any other
mistyped key. An app that sets `mcp = true` must say what it offers, so
its description may not be missing, empty, or only whitespace; and a
description is one line of text wherever it is shown, so it may hold no
control character. These are rules about the manifest, like `port`, so they
hold wherever a manifest is parsed: `install` refuses in its `file` step, and
a regeneration over a hand-edited manifest that breaks them refuses as it does
for any unreadable manifest. When both description rules are broken, the
one-line rule is the one reported. A description is kept exactly as written,
never trimmed. The manifest names no port: the host hands every app its
socket, so a manifest that still carries `port` is refused rather than
ignored. How long an app may drain and how long systemd waits for it to stop
are not the app's choice either; they are two space-wide keys in the store,
read and validated here so install and init agree on them.

A manifest may also carry a `[resources]` table saying where the app runs
and how much of the host it may take, every key optional and defaulted at
parse, so the model always holds a complete set: `slice`, `core` or `apps`
(default `apps`), the slice the app runs in; `memory_max`, a ceiling written
as a string of bytes with an optional `K`, `M`, or `G` (powers of 1024,
default `128M`); `go_memory_limit`, the Go runtime's soft limit, in the same
form, no larger than `memory_max` (default three quarters of it, rounded
down); `cpu_weight`, its share of CPU within its slice, a whole number from 1
to 10000 (default 100); `delegate`, a Boolean; and `oom_policy`, whose one
value is `continue`. These are the ranges and units systemd.resource-control(5)
and systemd.service(5) document for the settings install writes them as
(D09). Any other key in the table is refused, `io_weight` included, so a
misspelt limit is never silently dropped and a value systemd could not apply
is refused rather than installed without the bound it asked for. The model
holds byte counts, so the unit carries one canonical spelling. A byte count
past the largest signed 64-bit value is refused with the same words as any
other malformed value; suffixes are upper-case only, as the stories spell
them. Whether `memory_max` fits the host is not the manifest's rule: install
judges it against the slice units (D09), and only install does.

A package may also ship an icon, `share/icon.svg`, which puts its app in the
host's service launcher (D15); an app without one is still listed in the
services file, it just shows no launcher tile. The icon is not part of the
manifest or of a discovered `Service`: discovery stays what nginx and backup
need, and the services package reads the icon itself when it regenerates the
services file. What belongs here is what an icon is allowed to be, because
`install` checks it in its `file` step (D09) before anything is written: at
most 64 KiB, and an SVG image — one well-formed XML document whose root
element is `svg`. Icons must be UTF-8: a leading byte-order mark is allowed, a
declared other encoding is not, and a duplicate attribute makes the document
not well-formed. The icon is still embedded byte for byte, mark included. The
same model names the services file, `/run/ikigenba/services.json`, or
`/var/lib/ikigenba/services.json` on a per-app host, and the variable every
app's environment carries to find it, and the `ikigenba` account whose group
owns both the installed trees and that file, so install, init, and the services package
agree on them.

A service keeps its files in two places besides its environment file. Its
*package directory* holds what its release or package brought: `bin/`, `etc/`,
and `share/`, and in a release also `libexec/` and `lib/`. Its *data
directory*, `/var/opt/ikigenba/<name>/` under `DataRoot`, is the app's working
directory and holds what the app writes as it runs: `state/`, its data, and
`cache/`, which it can lose. A manifest's relative paths, `[database].path`
among them, resolve against the data directory, so every app's data is a
sibling of every other's.

Where the package directory is depends on the host's layout, which
`ReadLayout` reads. A *released host*, one where `/opt/ikigenba/current`
exists (D16), runs the release that link names, and an app's package
directory is `/opt/ikigenba/current/<name>/`; there a service is an app of the
current release, a directory holding `bin/<name>` and `etc/manifest.toml`, or
any `/var/opt/ikigenba/<name>/` holding `state/`, and `/opt/<name>/` makes no
service. A *per-app host* has no `current` and holds apps `install` put under
`/opt/<name>/` (D09); there a service is any `/opt/<name>/` holding `etc/` or
any `/var/opt/ikigenba/<name>/` holding `state/`, and a `state/` left under
`/opt/<name>/` by an older install is not a service by itself; install and
uninstall move it (D09, D10). A *fresh host* has neither. Each discovered
service carries its package directory as a host path, so nginx (D06), the
services file (D15), and status (D10) read an app's files through it without
knowing the layout, and a service with only its kept state is *data-only*: it
has no package directory, no units, no nginx block, and no services entry, but
backup still archives it. `services` and `opsctl` are reserved names: the first
is the boot unit `ikigenba-services.service` (D17), the second the opsctl
directory of every release.

The environment file is host configuration, not part of what the package
brought: `/etc/opt/ikigenba/<name>/env` under `EnvRoot`, alone in its own
directory, generated from the manifest, the secrets parameter, and the store.
`install` writes it whole every time (D09), `init` rewrites the store's values
in it, and `uninstall` removes its directory (D10). It is never backed up: a
host writes it again rather than carrying a stale copy forward. A host whose
apps were installed before it lived there holds it at `/opt/<name>/etc/env`,
which nothing reads but a unit an older install wrote; it goes with the old
`etc/` when an install replaces it, and with `/opt/<name>/` when an uninstall
removes it.

The release, artifact, and manifest inputs described here come from the supplied opsctl
stories. They do not assert the behavior or internal layout of another
sub-project. Command execution and lifecycle ordering belong to the consuming
designs.

SQLite is the only engine the platform keeps, so a `[database]` names
`sqlite` or is refused. An
app owns its schema and migrates it forward itself at start; opsctl never
runs a migration and no app is seeded.

## REQUIREMENTS

- R-YNP2-AKCZ: Package `internal/apps` MUST export `Database` as a struct with `Engine string` and `Path string` fields.
- R-Y1RD-E8L4: Package `internal/apps` MUST export `Resources` as a struct with `Slice string`, `MemoryMax int64`, `GoMemoryLimit int64`, `CPUWeight int`, `Delegate bool`, and `OOMPolicy string` fields.
- R-RGBS-29LX: Package `internal/apps` MUST export `Manifest` as a struct with `App string`, `Description string`, `Default bool`, `MCP bool`, `Guests bool`, `Secrets []string`, `Env map[string]string`, `Database *Database`, and `Resources Resources` fields.
- R-8T01-X9IR: Package `internal/apps` MUST export `Service` as a struct with `Name string`, `Dir string`, `Manifest *Manifest`, and `ManifestError error` fields; `Dir` is the service's *package directory* as a host path, never resolved under a root: `/opt/ikigenba/current/<name>` for a service of the current release, `/opt/<name>` for a per-app install, and empty for a *data-only service*, one known only by its data directory.
- R-SWFY-IA5G: Package `internal/apps` MUST export `ValidateName(name string) error`, returning nil exactly for a name of one through 63 ASCII letters, digits, or hyphens whose first and last characters are letters or digits and whose lowercase form is not a *reserved name*; all other inputs MUST return an error whose `Error()` is exactly `'<name>' is not a usable app name` with `name` substituted as given.
- R-SYVR-9TMU: The reserved names MUST include `host`, `deploy`, `snapshots`, `seed`, `backup-host`, `backup-services`, and `renew-certificate`, and no name other than those and the names a requirement of this design adds.
- R-T03N-NLDJ: `services` and `opsctl` MUST also be reserved names: the first would make an app's service unit the boot unit `ikigenba-services.service` (D17), and the second is the opsctl directory of every release folder (D16).
- R-XSFU-AXFW: Package `internal/apps` MUST export `ParseManifest(data []byte) (Manifest, error)`, accepting a TOML document and returning an error for malformed TOML or an invalid recognized field rather than a partially usable manifest.
- R-ZATO-AEFN: `ParseManifest` MUST map TOML `app`, `description`, `default`, `mcp`, `guests`, `secrets`, `[env]`, `[database]`, and `[resources]` to the `Manifest` fields `App`, `Description`, `Default`, `MCP`, `Guests`, `Secrets`, `Env`, `Database`, and `Resources` respectively, with database `engine` and `path` mapped to `Database.Engine` and `Database.Path` and resources `slice`, `memory_max`, `go_memory_limit`, `cpu_weight`, `delegate`, and `oom_policy` mapped to `Resources.Slice`, `Resources.MemoryMax` (the byte count R-7WGX-8GE5 defines), `Resources.GoMemoryLimit` (the byte count R-ZD9H-1XX1 defines), `Resources.CPUWeight`, `Resources.Delegate`, and `Resources.OOMPolicy`; a present `description` MUST be kept verbatim, without trimming; absent fields MUST produce their Go zero values, so an absent `description` is empty, an absent `mcp` or `guests` is `false`, except that an absent `[resources]`, or an absent key within it, MUST give the corresponding `Resources` field its default: `Slice` `apps`, `MemoryMax` 134217728 (128 MiB), `GoMemoryLimit` three quarters of the resulting `MemoryMax` rounded down to a whole byte, computed without overflow, `CPUWeight` 100, `Delegate` false, and `OOMPolicy` empty; so a manifest with no `[resources]` gives `Slice` `apps`, `MemoryMax` 134217728, `GoMemoryLimit` 100663296, and `CPUWeight` 100, one whose table holds only `memory_max = "256M"` gives `GoMemoryLimit` 201326592, and one whose table holds only `memory_max = "9223372036854775807"` gives `GoMemoryLimit` 6917529027641081855; absent `[database]` MUST produce nil `Database`, and absent `secrets` or `[env]` MUST be usable as empty collections.
- R-RIRK-TT3B: `ParseManifest` MUST require a present `app` to be a string accepted by `ValidateName`, a present `description` to be a string, a present `default`, a present `mcp`, and a present `guests` each to be a Boolean, a present `secrets` to be an array of strings, every `[env]` value to be a string, and a present `resources` to be a table, returning an error for any of them of another type; fields other than `app`, `description`, `default`, `mcp`, `guests`, `secrets`, `[env]`, `[database]`, `[resources]`, and `port` MUST not contribute to the returned model, and an omitted `app` MUST remain valid for a service whose manifest declares only other capabilities.
- R-YBA6-4L0A: `ParseManifest` MUST reject a document with a top-level `port` key, whatever its value or type, returning an error whose `Error()` is exactly `'port' is not allowed; the host gives the app its socket`; no app listens on a port, and the host hands every app its socket (D09).
- R-Y2OV-VJIN: When a document carries a top-level `port` key and also fails another `ParseManifest` rule, including an `app` value `ValidateName` rejects, `ParseManifest` MUST return the `port` error.
- R-YCI2-ICQZ: `ParseManifest` MUST reject a document whose `description` is a string holding any character from U+0000 through U+001F or U+007F, line feed and tab included, returning an error whose `Error()` is exactly `'description' must be one line of text`; a `description` holding none of them, including an empty one or one of only spaces, MUST NOT fail this rule.
- R-YDPY-W4HO: `ParseManifest` MUST reject a document whose `mcp` is `true` and whose `description` is absent or is a string that `strings.TrimSpace` reduces to the empty string, returning an error whose `Error()` is exactly `'mcp' is true but 'description' is empty; an MCP service must say what it offers`; when `mcp` is `false` or absent, an absent, empty, or whitespace-only `description` MUST be accepted.
- R-YEXV-9W8D: When a document breaks the rule of R-YDPY-W4HO and also any other `ParseManifest` rule, `ParseManifest` MUST return the other rule's error, so a `description` that is not one line of text gets the error of R-YCI2-ICQZ whatever `mcp` is, and a document carrying `port` gets the `port` error (R-Y2OV-VJIN).
- R-T2JG-F4UX: `ParseManifest` MUST require a present `[database]` to contain `engine = "sqlite"` and a string `path` naming a file strictly below `state/` relative to the service's data directory (R-T1BK-1D48), rejecting absolute paths, empty path components, `.` or `..` components, and any other engine; decoding MUST not require the named database to exist or change its journal mode.
- R-T3RC-SWLM: `ParseManifest` MUST reject a document whose `[database]` table has no `engine`, or an `engine` that is anything other than the string `sqlite`, a value of another type included, returning an error whose `Error()` is exactly `'database.engine' must be "sqlite"`; when a document breaks this rule and every other rule it breaks is among the `path` rule of R-T2JG-F4UX, the `[resources]` rules of R-ZC1K-O66C, R-7WGX-8GE5, R-ZD9H-1XX1, R-ZEHD-FPNQ, R-ZFP9-THEF, R-ZGX6-7954, R-ZI52-L0VT, and R-ZJCY-YSMI, and the rule of R-YDPY-W4HO, it MUST get this error, and otherwise it MUST get the error of the other rules that those rules' own precedence requirements select.
- R-ZC1K-O66C: `ParseManifest` MUST accept a `[resources]` `slice` exactly when it is the TOML string `core` or the TOML string `apps`, setting `Resources.Slice` to it, and MUST reject any other value, including the strings `edge`, `Core`, and the empty string and the integer `1`, with an error whose `Error()` is exactly `'resources.slice' must be "core" or "apps"`.
- R-ZD9H-1XX1: `ParseManifest` MUST accept a `[resources]` `go_memory_limit` exactly when it is a TOML string meeting the grammar and byte-count range R-7WGX-8GE5 states for `memory_max`, and MUST set `Resources.GoMemoryLimit` to that byte count, so `"128M"` gives 134217728; any other `go_memory_limit`, including `"0"`, `"128MB"`, `"128m"`, `"1.5G"`, `""`, and the integer `134217728`, MUST be rejected with an error whose `Error()` is exactly `'resources.go_memory_limit' must be a whole number of bytes, optionally followed by K, M, or G`.
- R-ZEHD-FPNQ: When a `[resources]` table holds a `go_memory_limit` that R-ZD9H-1XX1 accepts and either a `memory_max` that R-7WGX-8GE5 accepts or no `memory_max`, `ParseManifest` MUST reject the document exactly when that `go_memory_limit` byte count is greater than the `Resources.MemoryMax` the table gives (its own `memory_max`, or the default 134217728 when absent), with an error whose `Error()` is exactly `'resources.go_memory_limit' must not be larger than 'resources.memory_max'`; so `go_memory_limit = "512M"` beside `memory_max = "256M"`, and `go_memory_limit = "129M"` with no `memory_max`, are refused, while `go_memory_limit = "256M"` beside `memory_max = "256M"`, and `go_memory_limit = "128M"` with no `memory_max`, are accepted.
- R-ZFP9-THEF: `ParseManifest` MUST accept a `[resources]` `cpu_weight` exactly when it is a TOML integer from 1 through 10000, setting `Resources.CPUWeight` to it, and MUST reject any other `cpu_weight` with an error whose `Error()` is exactly `'resources.cpu_weight' must be a whole number from 1 to 10000`; so `0`, `10001`, `20000`, `-1`, `50.0`, and the string `"50"` are each refused, while `1`, `100`, and `10000` are accepted.
- R-ZGX6-7954: `ParseManifest` MUST accept a `[resources]` `delegate` exactly when it is a TOML Boolean, setting `Resources.Delegate` to it, and MUST reject any other value, including the strings `"yes"` and `"true"` and the integer `1`, with an error whose `Error()` is exactly `'resources.delegate' must be true or false`.
- R-ZI52-L0VT: `ParseManifest` MUST accept a `[resources]` `oom_policy` exactly when it is the TOML string `continue`, setting `Resources.OOMPolicy` to `continue`, and MUST reject any other value, including the strings `"stop"`, `"kill"`, `"Continue"`, and `""` and the Boolean `true`, with an error whose `Error()` is exactly `'resources.oom_policy' must be "continue"`.
- R-7WGX-8GE5: `ParseManifest` MUST accept a `[resources]` `memory_max` exactly when it is a TOML string of one or more ASCII decimal digits, optionally followed by exactly one of `K`, `M`, or `G`, whose byte count — the number multiplied by 1024 for `K`, 1048576 for `M`, 1073741824 for `G`, and 1 with no suffix — is from 1 through 9223372036854775807, and MUST set `Resources.MemoryMax` to that byte count, so `"512M"` gives 536870912 and `"1048576"` gives 1048576; any other `memory_max`, including `"0"`, `"0M"`, `"512MB"`, `"512m"`, `"1.5G"`, `"50%"`, `""`, `"infinity"`, the integer `536870912`, `"17179869185G"` (whose byte count exceeds the limit although it wraps to 1073741824 modulo 2^64), and a value whose byte count exceeds 9223372036854775807, MUST be rejected with an error whose `Error()` is exactly `'resources.memory_max' must be a whole number of bytes, optionally followed by K, M, or G`.
- R-ZJCY-YSMI: `ParseManifest` MUST reject a document whose `[resources]` table holds a key other than `slice`, `memory_max`, `go_memory_limit`, `cpu_weight`, `delegate`, and `oom_policy`, whatever its value, returning an error whose `Error()` is exactly `'resources.<key>' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy` with the key as written in the document substituted for `<key>`, so `io_weight = 50` gives `'resources.io_weight' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy` and `cpu_quota = 50` gives the same words naming `cpu_quota`.
- R-ZKKV-CKD7: When a `[resources]` table breaks more than one of the rules of R-ZC1K-O66C, R-7WGX-8GE5, R-ZD9H-1XX1, R-ZEHD-FPNQ, R-ZFP9-THEF, R-ZGX6-7954, R-ZI52-L0VT, and R-ZJCY-YSMI, `ParseManifest` MUST return only the error of the first in the order `slice`, `memory_max`, `go_memory_limit`'s form (R-ZD9H-1XX1), `go_memory_limit` against `memory_max` (R-ZEHD-FPNQ), `cpu_weight`, `delegate`, `oom_policy`, then unknown keys in ascending bytewise name order; so `slice = "edge"` with `memory_max = "512MB"` gives the `slice` error, `memory_max = "512MB"` with `go_memory_limit = "1.5G"` gives the `memory_max` error, `go_memory_limit = "1.5G"` with `cpu_weight = 0` gives the `go_memory_limit` form error, `go_memory_limit = "512M"` beside `memory_max = "256M"` with `cpu_weight = 0` gives the comparison error, `delegate = 1` with `oom_policy = "stop"` gives the `delegate` error, and `zz = 1` with `io_weight = 1` gives the `io_weight` error. A document that breaks one of those rules and also any other `ParseManifest` rule except that of R-YDPY-W4HO MUST get the other rule's error, so `port` (R-Y2OV-VJIN), a `description` that is not one line of text, and an `app` that `ValidateName` rejects each win over a `[resources]` fault, while a `[resources]` fault wins over the empty-description fault of R-YDPY-W4HO.
- R-8VFU-OT05: Package `internal/apps` MUST export `type Layout string` with the constants `Fresh Layout = "fresh host"`, `PerApp Layout = "per app"`, and `Released Layout = "releases"`, and `ReadLayout(root string) (Layout, error)`.
- R-8WNR-2KQU: `ReadLayout` MUST return `Released` exactly when an entry of any type, a dangling symbolic link included, exists at `/opt/ikigenba/current` (`release.CurrentLink`, D16) under `root`, judged without following it; otherwise `PerApp` exactly when some immediate directory `/opt/<name>/` under `root`, for a name other than `ikigenba`, holds an `etc/` directory; and otherwise `Fresh`. A host in each layout is a *released host*, a *per-app host*, and a *fresh host* respectively. A failure to examine those paths other than their absence MUST return an error.
- R-8Z3J-U488: Package `internal/apps` MUST export `Discover(root string) ([]Service, error)`, returning services in ascending bytewise `Name` order, one per name, resolving every filesystem access under `root`. When `ReadLayout(root)` gives `Released`, a name MUST be a service exactly when `/opt/ikigenba/current/<name>` is a directory, judged without following it as a symbolic link once `current` itself is followed, holding a regular file `bin/<name>` and an entry `etc/manifest.toml`, its `Dir` then being `/opt/ikigenba/current/<name>`, or when `/var/opt/ikigenba/<name>/` holds a `state/` directory, and no `/opt/<name>/` MUST make or change a service. Otherwise a name MUST be a service exactly when `/opt/<name>/`, for a name other than `ikigenba`, is a directory holding an `etc/` directory, its `Dir` then being `/opt/<name>`, or when `/var/opt/ikigenba/<name>/` holds a `state/` directory, a `/opt/<name>/` holding a `state/` but no `etc/` not making a service by itself. A missing `/opt`, `/opt/ikigenba/current`, or `/var/opt/ikigenba` MUST contribute no services without error, so a root with none gives an empty successful result, and failure to enumerate one that exists MUST return an error.
- R-U9YN-KYA0: Package `internal/apps` MUST export `DiscoverRelease(root, sha string) ([]Service, error)`.
- R-UB6J-YQ0P: `DiscoverRelease` MUST return what `Discover` returns on a released host whose `current` names `releases/<sha>`, with the release folder for `sha` (D16) in place of `/opt/ikigenba/current` and each such service's `Dir` being `/opt/ikigenba/releases/<sha>/<name>`, whatever the host's layout and whether or not `current` exists or names another release, so a restore run before the first activate reads what the release it runs from declares; a missing release folder MUST give an error, and it MUST perform no filesystem write, subprocess execution, or schema migration.
- R-90BG-7VYX: For each service with a nonempty `Dir`, `Discover` MUST read `<Dir>/etc/manifest.toml` under `root` when present and use `ParseManifest` to populate `Service.Manifest`; a missing manifest, and a data-only service, MUST leave both `Manifest` and `ManifestError` nil, while a read or decode failure MUST leave `Manifest` nil and populate `ManifestError` without dropping that service or failing discovery of the other services.
- R-91JC-LNPM: `Discover` MUST derive `Service.Name` from the immediate directory name under `/opt`, `/opt/ikigenba/current`, or `/var/opt/ikigenba` rather than a binary, unit, artifact filename, or manifest value, and MUST retain a data-only service with only a `/var/opt/ikigenba/<name>/state/` and no package directory, manifest, executable, or systemd unit; a successfully parsed manifest whose nonempty `App` differs from that directory name MUST instead be reported as a per-service `ManifestError` with nil `Manifest`.
- R-92R8-ZFGB: `Discover`, `ReadLayout`, `ParseManifest`, and `ValidateName` MUST perform no filesystem writes, subprocess execution, or schema migration; the manifest carries no authoritative version.
- R-YRCR-FVL2: Package `internal/apps` MUST export `Timeouts` as a struct with `DrainSeconds int64` and `StopSeconds int64` fields, and `ReadTimeouts(store config.Store) (Timeouts, error)`.
- R-UA45-18GV: The configuration keys `apps.drain_seconds` and `apps.stop_seconds` MUST be the space-wide app timing settings: `apps.drain_seconds` is how many whole seconds every app may spend draining once told to stop, and `apps.stop_seconds` is how many whole seconds systemd waits for every app's service to stop; an absent key or an empty value MUST mean `5` and `10` respectively, and no manifest field sets or overrides either.
- R-UBC1-F07K: `ReadTimeouts` MUST read both timing keys from `store` and return their values; a nonempty value MUST consist only of ASCII decimal digits, leading zeros permitted, denoting a whole number from 1 through 9223372036, and otherwise `ReadTimeouts` MUST return an error whose `Error()` is exactly `<key> is not a positive whole number of seconds: '<value>'` with the stored value substituted unchanged, judging `apps.drain_seconds` before `apps.stop_seconds` so only the first offending key is named; when both are valid and the stop value is not greater than the drain value it MUST return an error whose `Error()` is exactly `apps.stop_seconds (<S>) is not greater than apps.drain_seconds (<D>)`, with `<S>` and `<D>` the effective values in decimal without leading zeros, defaults included.
- R-UCJX-SRY9: `ReadTimeouts` MUST NOT write the store; a store read failure other than an absent key, including a corrupt store, MUST be returned as an error wrapping the store's error (so `errors.Is` matches `config.ErrCorrupt` for a corrupt store) rather than as a timing finding.
- R-93Z5-D770: Package `internal/apps` MUST export the constants `IconPath = "share/icon.svg"`, the package-relative path of a service's launcher icon; `ServicesPath = "/run/ikigenba/services.json"`, the services file of every host but a per-app host (D15); `PerAppServicesPath = "/var/lib/ikigenba/services.json"`, the services file of a per-app host; and `ServicesEnv = "IKIGENBA_SERVICES"`, the name of the environment variable that carries the services file's path to every app.
- R-G34X-CPC0: Package `internal/apps` MUST export the constant `DataRoot = "/var/opt/ikigenba"`, the host directory beneath which every service's data directory lies.
- R-T1BK-1D48: A service `<name>`'s *data directory* MUST be `DataRoot` followed by `/<name>`, that is `/var/opt/ikigenba/<name>/`, resolved under the root an operation is given (`Discover`'s `root`, `host.Env.Root`, or `cli.Deps.Root`, D01); it is the app's working directory, the `WorkingDirectory=` of the service unit install writes (D09) and of the one `WriteReleaseUnits` writes (D17), and every operation that reads or writes a file a manifest names by a relative path, `Database.Path` among them, MUST resolve that path against it, so `Status` (D10) reads the journal mode of a `crm` declaring `state/crm.db` from `/var/opt/ikigenba/crm/state/crm.db` under its root and never from `/opt/crm/state/crm.db`.
- R-02XL-DHW4: Package `internal/apps` MUST export the constant `EnvRoot = "/etc/opt/ikigenba"`, the host directory beneath which every service's environment file lies.
- R-9571-QYXP: A service `<name>`'s *environment file* MUST be `EnvRoot` followed by `/<name>/env`, that is `/etc/opt/ikigenba/<name>/env`, in the *environment directory* `/etc/opt/ikigenba/<name>/`, both resolved under the root an operation is given (`Discover`'s `root`, `host.Env.Root`, or `cli.Deps.Root`, D01); it is the file the service unit names as its `EnvironmentFile=`, and `/opt/<name>/etc/env`, where an older install wrote it, is not the environment file: no operation reads it, and only the removal of `/opt/<name>/etc/` or `/opt/<name>/` takes it away. An environment directory MUST NOT by itself make its name a service: `Discover` MUST return no service for a name present only as `/etc/opt/ikigenba/<name>/`, and the same services with or without it otherwise.
- R-8YZ3-XU4K: Package `internal/apps` MUST export the sentinel errors `ErrIconNotSVG`, whose `Error()` is exactly `share/icon.svg is not an SVG image`, and `ErrIconTooLarge`, whose `Error()` is exactly `share/icon.svg is larger than 64 KiB`.
- R-9070-BLV9: Package `internal/apps` MUST export `CheckIcon(data []byte) error`.
- R-91EW-PDLY: `CheckIcon` MUST return `ErrIconTooLarge` exactly when `data` is longer than 65536 bytes, judging size before content, so 65536 bytes never fails for size, 65537 bytes always does, and data both too large and not an SVG image returns `ErrIconTooLarge`; for data of at most 65536 bytes it MUST return nil when `data` is an SVG image and otherwise `ErrIconNotSVG`, each returned error being the sentinel itself.
- R-M1NV-I2WL: For `CheckIcon`, `data` MUST be an *SVG image* exactly when all of these hold: it is valid UTF-8; after one leading UTF-8 byte-order mark (EF BB BF), when present, is set aside, an `encoding/xml` `Decoder` in its default strict mode with no `CharsetReader` reads the rest to end of input without error; the tokens hold exactly one top-level element, whose name's local part is `svg` in any namespace or none; no element carries two attributes with the same name (the same namespace and local part); and every character-data token outside that element holds only spaces, tabs, CRs, and LFs, while comments, processing instructions (the XML declaration included), and directives are allowed outside it. So empty data, text alone, a PNG, invalid UTF-8, an XML declaration naming an encoding other than UTF-8, unbalanced or unclosed tags, an undefined entity, a duplicate attribute, a root other than `svg`, a second top-level element, and text after the root element are each `ErrIconNotSVG`, while a BOM-prefixed SVG image is accepted.
- R-93UP-GX3C: `CheckIcon` MUST be a pure function of `data`: it MUST NOT read or write the filesystem, execute a process, read the environment or a clock, or perform network access.
- R-952L-UOU1: Package `internal/apps` MUST export `EnsureAccount(ctx context.Context, env host.Env) error`.
- R-96AI-8GKQ: `EnsureAccount` MUST ensure a non-root system account named `ikigenba` exists, with no login shell and no home directory created for it, whose primary group is named `ikigenba`: it MUST execute `id --user ikigenba` through `env.Execute`, treat exit status 1 as an absent account and then execute `useradd --system --no-create-home --shell /usr/sbin/nologin --user-group ikigenba`; for a present account it MUST fail when the reported uid is 0 and MUST execute `id --group --name ikigenba`, failing unless its output with surrounding whitespace removed is `ikigenba`, without changing the account's group. Any other execution error or nonzero exit MUST return an error wrapping `*host.CommandError`, and it MUST change nothing but the account.
