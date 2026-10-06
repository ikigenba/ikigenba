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

A manifest may also carry a `[resources]` table bounding how much of the host
the app may take: `cpu_weight`, its share of CPU under contention, and
`io_weight`, its share of disk IO, each a whole number from 1 to 10000 where
systemd's default is 100; and `memory_max`, a ceiling written as a string of
bytes with an optional `K`, `M`, or `G` (powers of 1024). Each key is
optional, and an absent one leaves systemd's default: an equal share, and no
memory ceiling. These are the ranges and units systemd.resource-control(5)
documents for `CPUWeight=`, `IOWeight=`, and `MemoryMax=`, which install
writes them as (D09). Any other key in the table is refused, so a misspelt
limit is never silently dropped, and a value systemd could not apply is
refused rather than installed without the bound it asked for. The model holds
`memory_max` as its byte count, so the unit carries one canonical spelling.
A byte count past the largest signed 64-bit value is refused with the same
words as any other malformed `memory_max`; suffixes are upper-case only, as
the stories spell them.

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
same model names the services file and the variable every app's environment
carries to find it, and the `ikigenba` account whose group owns both the
installed trees and that file, so install, init, and the services package
agree on them.

The artifact and manifest inputs described here come from the supplied opsctl
stories. They do not assert the behavior or internal layout of another
sub-project. Command execution and lifecycle ordering belong to the consuming
designs.

SQLite is the only engine the platform keeps, so a `[database]` names
`sqlite` or is refused. An
app owns its schema and migrates it forward itself at start; opsctl never
runs a migration and no app is seeded.

## REQUIREMENTS

- R-YNP2-AKCZ: Package `internal/apps` MUST export `Database` as a struct with `Engine string` and `Path string` fields.
- R-7P5I-XTXZ: Package `internal/apps` MUST export `Resources` as a struct with `CPUWeight int`, `MemoryMax int64`, and `IOWeight int` fields, where a zero field means the manifest does not set that limit.
- R-RGBS-29LX: Package `internal/apps` MUST export `Manifest` as a struct with `App string`, `Description string`, `Default bool`, `MCP bool`, `Guests bool`, `Secrets []string`, `Env map[string]string`, `Database *Database`, and `Resources Resources` fields.
- R-YQ4V-23UD: Package `internal/apps` MUST export `Service` as a struct with `Name string`, `Manifest *Manifest`, and `ManifestError error` fields.
- R-158S-PZEM: Package `internal/apps` MUST export `ValidateName(name string) error`, returning nil exactly for a name of one through 63 ASCII letters, digits, or hyphens whose first and last characters are letters or digits and whose lowercase form is none of `host`, `deploy`, `snapshots`, `seed`, `backup-host`, `backup-services`, or `renew-certificate`; all other inputs MUST return an error identifying the unusable name.
- R-XSFU-AXFW: Package `internal/apps` MUST export `ParseManifest(data []byte) (Manifest, error)`, accepting a TOML document and returning an error for malformed TOML or an invalid recognized field rather than a partially usable manifest.
- R-RHJO-G1CM: `ParseManifest` MUST map TOML `app`, `description`, `default`, `mcp`, `guests`, `secrets`, `[env]`, `[database]`, and `[resources]` to the `Manifest` fields `App`, `Description`, `Default`, `MCP`, `Guests`, `Secrets`, `Env`, `Database`, and `Resources` respectively, with database `engine` and `path` mapped to `Database.Engine` and `Database.Path` and resources `cpu_weight`, `memory_max`, and `io_weight` mapped to `Resources.CPUWeight`, `Resources.MemoryMax` (the byte count R-7WGX-8GE5 defines), and `Resources.IOWeight`; a present `description` MUST be kept verbatim, without trimming; absent fields MUST produce their Go zero values, so an absent `description` is empty, an absent `mcp` or `guests` is `false`, and an absent `[resources]` or an absent key within it leaves the corresponding `Resources` field zero; absent `[database]` MUST produce nil `Database`, and absent `secrets` or `[env]` MUST be usable as empty collections.
- R-RIRK-TT3B: `ParseManifest` MUST require a present `app` to be a string accepted by `ValidateName`, a present `description` to be a string, a present `default`, a present `mcp`, and a present `guests` each to be a Boolean, a present `secrets` to be an array of strings, every `[env]` value to be a string, and a present `resources` to be a table, returning an error for any of them of another type; fields other than `app`, `description`, `default`, `mcp`, `guests`, `secrets`, `[env]`, `[database]`, `[resources]`, and `port` MUST not contribute to the returned model, and an omitted `app` MUST remain valid for a service whose manifest declares only other capabilities.
- R-YBA6-4L0A: `ParseManifest` MUST reject a document with a top-level `port` key, whatever its value or type, returning an error whose `Error()` is exactly `'port' is not allowed; the host gives the app its socket`; no app listens on a port, and the host hands every app its socket (D09).
- R-Y2OV-VJIN: When a document carries a top-level `port` key and also fails another `ParseManifest` rule, including an `app` value `ValidateName` rejects, `ParseManifest` MUST return the `port` error.
- R-YCI2-ICQZ: `ParseManifest` MUST reject a document whose `description` is a string holding any character from U+0000 through U+001F or U+007F, line feed and tab included, returning an error whose `Error()` is exactly `'description' must be one line of text`; a `description` holding none of them, including an empty one or one of only spaces, MUST NOT fail this rule.
- R-YDPY-W4HO: `ParseManifest` MUST reject a document whose `mcp` is `true` and whose `description` is absent or is a string that `strings.TrimSpace` reduces to the empty string, returning an error whose `Error()` is exactly `'mcp' is true but 'description' is empty; an MCP service must say what it offers`; when `mcp` is `false` or absent, an absent, empty, or whitespace-only `description` MUST be accepted.
- R-YEXV-9W8D: When a document breaks the rule of R-YDPY-W4HO and also any other `ParseManifest` rule, `ParseManifest` MUST return the other rule's error, so a `description` that is not one line of text gets the error of R-YCI2-ICQZ whatever `mcp` is, and a document carrying `port` gets the `port` error (R-Y2OV-VJIN).
- R-XW3J-G8NZ: `ParseManifest` MUST require a present `[database]` to contain `engine = "sqlite"` and a string `path` naming a file strictly below `state/` relative to the service directory, rejecting absolute paths, empty path components, `.` or `..` components, and any other engine; decoding MUST not require the named database to exist or change its journal mode.
- R-17OL-HIW0: `ParseManifest` MUST reject a document whose `[database]` table has no `engine`, or an `engine` that is anything other than the string `sqlite`, a value of another type included, returning an error whose `Error()` is exactly `'database.engine' must be "sqlite"`; a document that breaks this rule and also the `path` rule of R-XW3J-G8NZ, a rule of R-7V90-UONG, R-7WGX-8GE5, or R-7XOT-M84U, or the rule of R-YDPY-W4HO MUST get this error, and one that breaks this rule and any other `ParseManifest` rule MUST get the other rule's error.
- R-7V90-UONG: `ParseManifest` MUST reject a document whose `[resources]` table holds `cpu_weight` that is not a TOML integer from 1 through 10000, returning an error whose `Error()` is exactly `'resources.cpu_weight' must be a whole number from 1 to 10000`, and one holding `io_weight` that is not a TOML integer from 1 through 10000, returning an error whose `Error()` is exactly `'resources.io_weight' must be a whole number from 1 to 10000`; so `0`, `10001`, `-1`, `50.0`, and the string `"50"` are each refused, while `1`, `100`, and `10000` are accepted.
- R-7WGX-8GE5: `ParseManifest` MUST accept a `[resources]` `memory_max` exactly when it is a TOML string of one or more ASCII decimal digits, optionally followed by exactly one of `K`, `M`, or `G`, whose byte count — the number multiplied by 1024 for `K`, 1048576 for `M`, 1073741824 for `G`, and 1 with no suffix — is from 1 through 9223372036854775807, and MUST set `Resources.MemoryMax` to that byte count, so `"512M"` gives 536870912 and `"1048576"` gives 1048576; any other `memory_max`, including `"0"`, `"0M"`, `"512MB"`, `"512m"`, `"1.5G"`, `"50%"`, `""`, `"infinity"`, the integer `536870912`, `"17179869185G"` (whose byte count exceeds the limit although it wraps to 1073741824 modulo 2^64), and a value whose byte count exceeds 9223372036854775807, MUST be rejected with an error whose `Error()` is exactly `'resources.memory_max' must be a whole number of bytes, optionally followed by K, M, or G`.
- R-7XOT-M84U: `ParseManifest` MUST reject a document whose `[resources]` table holds a key other than `cpu_weight`, `memory_max`, and `io_weight`, whatever its value, returning an error whose `Error()` is exactly `'resources.<key>' is not allowed; the resources are cpu_weight, memory_max, and io_weight` with the key as written in the document substituted for `<key>`, so `cpu_quota = 50` gives `'resources.cpu_quota' is not allowed; the resources are cpu_weight, memory_max, and io_weight`.
- R-7YWP-ZZVJ: When a `[resources]` table breaks more than one of the rules of R-7V90-UONG, R-7WGX-8GE5, and R-7XOT-M84U, `ParseManifest` MUST return the error for the first offending entry in the order `cpu_weight`, `memory_max`, `io_weight`, then unknown keys in ascending bytewise name order; so a table holding `cpu_weight = 0` and `memory_max = "512MB"` gives the `cpu_weight` error, and one holding `zz = 1` and `cpu_quota = 1` gives the `cpu_quota` error. A document that breaks one of those rules and also any other `ParseManifest` rule except that of R-YDPY-W4HO MUST get the other rule's error, so `port` (R-Y2OV-VJIN), a `description` that is not one line of text, and an `app` that `ValidateName` rejects each win over a `[resources]` fault, while a `[resources]` fault wins over the empty-description fault of R-YDPY-W4HO.
- R-XXBF-U0EO: Package `internal/apps` MUST export `Discover(root string) ([]Service, error)`, returning services in ascending bytewise `Name` order, one for each immediate directory `/opt/<name>/` holding an `etc/` or `state/` directory, resolving every filesystem access under `root`; a missing `/opt` MUST produce an empty successful result, and failure to enumerate `/opt` MUST return an error.
- R-XYJC-7S5D: For each service, `Discover` MUST read `/opt/<name>/etc/manifest.toml` when present and use `ParseManifest` to populate `Service.Manifest`; a missing manifest MUST leave both `Manifest` and `ManifestError` nil, while a read or decode failure MUST leave `Manifest` nil and populate `ManifestError` without dropping that service or failing discovery of the other services.
- R-XZR8-LJW2: `Discover` MUST derive `Service.Name` from the immediate directory name rather than a binary, unit, artifact filename, or manifest value, retaining services with only `state/` and no manifest, executable, or systemd unit; a successfully parsed manifest whose nonempty `App` differs from that directory name MUST instead be reported as a per-service `ManifestError` with nil `Manifest`.
- R-16GP-3R5B: `Discover`, `ParseManifest`, and `ValidateName` MUST perform no filesystem writes, subprocess execution, or schema migration; the manifest carries no authoritative version, and the installed app's executable queried by the lifecycle/status consumer is the source of its reported version.
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
