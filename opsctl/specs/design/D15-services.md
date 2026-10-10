# D15-services

The apps on a host learn which services it offers from one file opsctl owns:
`/run/ikigenba/services.json`, or `/var/lib/ikigenba/services.json` on a
per-app host (D08). Every app's banner builds its launcher from it, and the
suite's MCP catalog finds its services there. Like the nginx configuration
(D06), the file is a pure function of the services discovery finds (D08), of
the stored `host.name`, and of which apps systemd reports disabled (D10's
`Disabled`), so it is generated, never edited, and never backed up: a restored
host regenerates it. opsctl owns its format as it owns `manifest.toml`; apps
read it through the variable `IKIGENBA_SERVICES`, which every app's environment
file, `/etc/opt/ikigenba/<app>/env`, carries (D08, D09, D17).

A service is *listed* — has an entry in the file — when it has a package
directory (D08), its manifest names its app, and its `bin/<name>` is a regular
file: on a released host an app of the release `current` names, on a per-app
host an app the legacy layout keeps under `/opt/<name>/`. Each entry carries the
service's name and URL, its manifest's `description` and `mcp` (D08), the
socket nginx sends its requests to, whether it is enabled, its icon only when its
package directory holds `share/icon.svg`, and, last, its manifest's home
group (D08), `core` or `application`. The icon does not decide
whether a service is listed, but it is the whole launcher opt-in: a launcher
shows only the entries that carry one, and the manifest carries no launcher
key. A data-only service is never listed, and on a released host an
`/opt/<name>/` left from the per-app layout lists nothing. Package
`internal/services` owns the listing criterion, the file's exact bytes, and
its publication. Package `internal/apps` (D08) owns what the file is built
from and what an app is given: the icon's package-relative path, the file's
paths and variable name
that every app's environment carries, and the `ikigenba` account whose group
owns the file.

`/run` is a tmpfs, emptied when the host restarts, so on a released host the
file is written again at boot by `ikigenba-services.service`, the oneshot D17
declares, which runs `/usr/local/bin/opsctl services apply` before any app's
service starts. `services apply` writes `/run/ikigenba/services.json` alone,
from current, on any host: with no current release it writes the empty list,
and it never writes a per-app host's own file. `/run/ikigenba/` is also where
every app's socket lives, and nginx must reach it, so the services file never
changes that directory's owner or mode; only the file is `root:ikigenba` with
mode `0640`.

The file is rewritten wherever nginx's configuration is regenerated —
`activate`, `rollback`, `enable`, `disable`, `restore`, `init`, and
`nginx apply` — and always after nginx's configuration
has succeeded, so the two files never disagree about which apps are disabled
and a command that fails before or at its nginx step leaves the file as it
was. Since every regeneration refuses a host where a manifest cannot be read
(D06), so does the services file: it never claims to leave such a service out.
An icon that cannot be embedded verbatim — not a regular file, unreadable, or
not UTF-8 — fails the rewrite too,
and the file is left as it was.

The commands that print per-app step lines (`disable`, `enable`) report the
rewrite on a `services` line right after `nginx`. The line names only the
change to the command's own app's entry: `disabled`, `enabled`, or
`unchanged` when that entry did not change, even if another entry did. The
change is measured against the file on disk before the rewrite, because
nothing else remembers the previous entry. `activate`
and `rollback` report the number of entries instead (D18). `init`, `restore`,
`nginx apply`, and `services apply` rewrite the file silently.

The layout is exact — the object pretty-printed, one service per line — so a
rerun that changes nothing rewrites nothing, and a story can show the file.
Strings are written with the minimal JSON escapes: `<`, `>`, and `&` appear
literally (unlike Go's default HTML-safe encoding), so an icon reads as the
SVG it is. Readers still depend only on the JSON value.

The file has no version field and its format only grows. This is a rule for
later revisions of this design: they may add members to the top-level object
or to an entry, and readers ignore members they do not know; they never
remove, rename, or change the type or meaning of `services`, `name`, `url`,
`description`, `socket`, `enabled`, `mcp`, `icon`, or `group`. All but
`icon`, seven members, are in every entry; `icon` is optional, and a reader treats an entry whose `icon` is
absent or not a string as having none, so a launcher that skips entries
without a string icon keeps working as the file grows. A change an existing
reader could not survive never edits this file: it becomes a new file with its
own variable (for example `services.v2.json` named by `IKIGENBA_SERVICES_V2`),
and opsctl writes both until no app in the release reads the old one.

A file written by an opsctl from before `group` has no `group` in any entry.
Since a member present in one entry and absent from the other is a
difference, the first rewrite after an upgrade classifies every entry both
files hold as `updated`, or as `disabled` or `enabled` when that rewrite also
flips it, so a `disable` or `enable` run then still reports its own app's
flip.

## REQUIREMENTS

- R-Z9N9-6FPH: Package `internal/services` MUST export type `Change string` with the constants `Unchanged Change = "unchanged"`, `Added Change = "added"`, `Removed Change = "removed"`, `Disabled Change = "disabled"`, `Enabled Change = "enabled"`, and `Updated Change = "updated"`.
- R-885B-IVTA: Package `internal/services` MUST export type `Changes map[string]Change` with the method `func (c Changes) For(app string) Change`, which MUST return `c[app]` when `app` is a key of `c` and `Unchanged` otherwise, including for a nil `Changes`.
- R-89D7-WNJZ: Package `internal/services` MUST export `Write(ctx context.Context, env host.Env, hostName string) (Changes, error)`.
- R-96EY-4QOE: The *services file* `Write` publishes MUST be `apps.PerAppServicesPath` when `apps.ReadLayout(env.Root)` gives `PerApp`, and `apps.ServicesPath` otherwise, so a released host and a fresh host keep it at `/run/ikigenba/services.json`, a fresh host's file listing nothing; `Write` MUST NOT create, modify, or remove the other path.
- R-97MU-IIF3: Package `internal/services` MUST export `Apply(ctx context.Context, env host.Env, hostName string) error`.
- R-98UQ-WA5S: `Apply` MUST publish `apps.ServicesPath` on every host, with exactly the bytes, publication, ownership, failure, and command rules `Write` follows for that path, listing the listed services when `apps.ReadLayout(env.Root)` gives `Released` and no service otherwise, so on a per-app or fresh host it writes `{\n  "services": []\n}\n`; it MUST NOT create, modify, or remove `apps.PerAppServicesPath`, read the configuration store, reload or test nginx, or change any unit.
- R-9A2N-A1WH: A service returned by `apps.Discover(env.Root)` MUST be a *listed service* exactly when its `Dir` is nonempty, its `Manifest` is non-nil with a nonempty `App`, and `<Dir>/bin/<name>` is a regular file, resolved under `env.Root`; whether it has an icon MUST NOT affect whether it is listed. So on a released host the listed services are apps of the current release and on a per-app host they are per-app installs, a data-only service is never listed, and on a released host no `/opt/<name>/` is. A service that is not a listed service MUST NOT appear in the file, and a listed service MUST appear in it whether or not `apps.Disabled` reports it disabled.
- R-UGXD-9BQC: For each listed service, `Write` MUST produce one entry whose `name` is the service's `Name`; whose `url` is exactly `https://<name>.<hostName>`, also for the service whose manifest sets `default` and for the one `host.apex` names; whose `description` is its `Manifest.Description`, unaltered, which is empty when the manifest has none; whose `socket` is exactly the string `/run/ikigenba/<name>.sock`, never resolved under `env.Root`, also for a disabled service; whose `enabled` is `false` exactly when `apps.Disabled(ctx, env, <name>)` returns true and `true` when it returns false; whose `mcp` is its `Manifest.MCP`; whose `group` is its `Manifest.Home.Group`; and which has an `icon` exactly when `<Dir>/` joined with `apps.IconPath` exists under `env.Root` as a directory entry of any type, that `icon` being the complete bytes of its icon file, trailing line ending included, unaltered.
- R-UJD6-0V7Q: The bytes `Write` publishes MUST be exactly `{` LF, two spaces, `"services": [`, then — when there are no listed services — `]` LF `}` LF, so the empty file is `{\n  "services": []\n}\n`; otherwise LF, one line per listed service in ascending bytewise name order, each line being four spaces, `{ "name": `, the encoded name, `, "url": `, the encoded url, `, "description": `, the encoded description, `, "socket": `, the encoded socket, `, "enabled": `, `true` or `false`, `, "mcp": `, `true` or `false`, then, only for an entry that has an icon, `, "icon": ` and the encoded icon, then `, "group": ` and the encoded group, and finally ` }`, the lines separated by `,` LF, followed by LF, two spaces, `]` LF, `}` LF; there MUST be no other member, including no version member, and no other whitespace.
- R-YJTG-SZ75: Each string value in the file MUST be encoded as `"`, the value's characters, and `"`, where `"` is written `\"`, `\` is written `\\`, U+0008 `\b`, U+000C `\f`, U+000A `\n`, U+000D `\r`, U+0009 `\t`, every other character from U+0000 through U+001F as `\u00` followed by two lowercase hexadecimal digits, U+2028 as `\u2028` and U+2029 as `\u2029`, and every other character — including `<`, `>`, `&`, `/`, U+007F, and all other non-ASCII characters — as its own UTF-8 bytes unchanged; decoding the published file as JSON MUST therefore yield each entry's name, url, description, socket, and icon byte for byte.
- R-9CIG-1LDV: `Write` MUST obtain every entry before creating, modifying, or removing any file or executing any command other than `apps.Disabled` queries, and MUST return nil `Changes` and an error, leaving the services file and its directory exactly as they were (bytes, mode, and presence) and executing no account or ownership command, when `hostName` is empty (error text exactly `host.name not set`), when `apps.ReadLayout` or `apps.Discover` fails, when any listed-service candidate, a discovered service with a nonempty `Dir`, has a non-nil `ManifestError` (giving, for the first such service in ascending name order, an error whose `Error()` is exactly `<name>: etc/manifest.toml: <reason>` when `apps.ParseManifest` rejects that service's manifest bytes, `<reason>` being that rejection's `Error()`, so an installed `repos` whose manifest holds `io_weight = 50` gives `repos: etc/manifest.toml: 'resources.io_weight' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy`, and otherwise an error identifying that service and its manifest failure), or when an `apps.Disabled` query fails (the error wrapping its `*host.CommandError`).
- R-9DQC-FD4K: When a listed service's icon cannot be embedded, `Write` MUST fail as R-9CIG-1LDV prescribes, leaving the file as it was, with an error whose text is exactly `<name>: share/icon.svg is not a regular file` when the entry is not a regular file (a directory or a symbolic link included, which is never followed), `<name>: share/icon.svg is not valid UTF-8` when its bytes are not valid UTF-8, and `<name>: share/icon.svg: ` followed by the read error's text when it cannot be read; the size and SVG rules of `apps.CheckIcon` MUST NOT be applied at regeneration.
- R-9EY8-T4V9: After obtaining every entry, `Write` MUST call `apps.EnsureAccount(ctx, env)` and return its error before any file change. Then, under `env.Root`, for the per-app services file it MUST create `/var/lib/ikigenba/` when absent, creating any missing ancestor with mode `0755`, and set that directory's mode to `0750`, so every successful `Write` on a per-app host leaves it a directory with mode `0750`; for `apps.ServicesPath` it MUST create `/run/ikigenba/` with mode `0755`, and any missing ancestor with mode `0755`, when absent, and MUST leave an existing `/run/ikigenba/` unchanged in owner and mode, since nginx reaches every app's socket through it.
- R-9HE1-KOCN: When the services file under `env.Root` already holds exactly the candidate bytes, `Write` MUST NOT replace it; otherwise it MUST write the candidate to a temporary file in the services file's directory and rename that file over the services file, so a reader observes either the previous file or the complete candidate, never a partial one, and apart from that temporary file, removed before return whenever it was not renamed, it MUST create, modify, and remove no file other than the directories of R-9EY8-T4V9 and the services file.
- R-9ILX-YG3C: Every successful `Write` MUST leave the services file a regular file with mode `0640`, its mode applied directly under `env.Root` so a test observes it with `Lstat`, and MUST establish ownership by executing through `env.Execute` exactly one `chown root:ikigenba` naming, for the per-app services file, `/var/lib/ikigenba` and then the file it keeps, and for `apps.ServicesPath` only the file it keeps (the temporary file, before the rename, when it replaces the services file; the services file itself when it does not), each resolved under `env.Root`; so the file, and on a per-app host its directory, end owned by user `root` and group `ikigenba` whatever their previous owner or mode.
- R-8MS4-44PM: A failure after entries are obtained — of `apps.EnsureAccount`, directory creation or mode, writing, mode, ownership, or rename — MUST make `Write` return nil `Changes` and an error, wrapping `*host.CommandError` for a failed command, and leave `services.json` with its previous bytes and mode or its previous absence, with any temporary file removed.
- R-BZML-VP81: `Write` MUST execute through `env.Execute` no command other than the `apps.Disabled` queries, the commands of `apps.EnsureAccount`, and the one `chown` of R-9ILX-YG3C; it MUST NOT read the configuration store, reload or test nginx, or change any unit.
- R-UKL2-EMYF: On success `Write` MUST return `Changes` holding one key for each name whose entry differs between the file before the call and the file it leaves, and no other key: `Added` when the name has no previous entry, `Removed` when it has no new entry, `Disabled` when the previous `enabled` is `true` and the new one `false`, and `Enabled` when the previous `enabled` is `false` and the new one `true`, whatever else differs, and `Updated` for every other difference. Entries are matched by their `name` member and compared only by the presence and JSON values of `url`, `description`, `socket`, `enabled`, `mcp`, `icon`, and `group`, a member present in one entry and absent from the other being a difference, except that an `icon` whose value is not a string counts as absent; any other member and the layout MUST be ignored.
- R-8QFT-9FXP: For classification, the file before the call MUST be taken to hold no entries when it is absent, cannot be read, or does not decode as a JSON object whose `services` member is an array; within such an array, an element that is not an object or whose `name` member is not a string MUST be ignored, and when several elements share a name the first MUST be used. Such a previous file MUST NOT make `Write` fail.
- R-TRYH-YUAY: For `disable` and `enable`, `internal/cli` MUST call `services.Write` exactly once, with the normalised host name it gave nginx, only after the `nginx` step reported success and before any later step, and report step `services`: on success `services: ok (<app> <change>)` with `<app>` the command's app and `<change>` the text of `Changes.For(<app>)`, or exactly `services: ok (unchanged)` when that is `Unchanged`; on error `services: failed: <reason>` with `<reason>` the error's text, CR and LF escaped as the command's other step lines escape them, after which the command attempts no later step, writes `opsctl: <command> failed` to stderr, and exits 1. A run that fails before or at its `nginx` step MUST NOT call `services.Write`.
- R-XTC1-NPBI: For `nginx apply` and `init`, `internal/cli` MUST call `services.Write` exactly once per run, and for `restore` exactly once per run on a released or per-app host (`apps.ReadLayout` giving `Released` or `PerApp`) and never on a fresh host, so a restore before the first activate never writes a services file; each call MUST use the normalised host name it gave nginx and come only after nginx's configuration was successfully published (`nginx.Apply` returned nil for `nginx apply` and `init`; `nginx.Write` returned nil inside restore's nginx regeneration callback), print nothing about it on success, and on error fail the command in the form D05, D06, and D14 prescribe for it; a run that fails before or at that publication MUST NOT call `services.Write`.
- R-9JTU-C7U1: Backup operations (D11–D13) MUST NOT read, archive, or upload anything under `/var/lib/ikigenba/` or `/run/ikigenba/`, and restore (D14) MUST NOT write either services file except through `services.Write`: the services file is regenerated, never backed up.
- R-9L1Q-PZKQ: `opsctl services --help` and `opsctl services -h` MUST, for any effective user id, perform no host-file access, process execution, or cloud call, exit 0 with empty stderr, and write exactly `"Usage: opsctl services <subcommand>\n\nGenerate /run/ikigenba/services.json, the services file every app reads\nthrough IKIGENBA_SERVICES, from the release /opt/ikigenba/current names,\nhost.name, and which apps systemd reports disabled. The file is generated,\nnever edited.\n\nSubcommands:\n  apply  write the file; with no current release, write an empty list\n\nConfiguration keys:\n  host.name  the fully-qualified name this host answers at\n\nikigenba-services.service runs 'opsctl services apply' at boot, before any\napp starts, so the file is there again after the host restarts.\n"` to stdout, where `\n` denotes one LF byte.
- R-9M9N-3RBF: `opsctl services apply` MUST read `host.name` from the store under `Deps.Root` and call `services.Apply` once with `host.NormalizeName` of it; on success it MUST write nothing to stdout or stderr and exit 0, and on failure it MUST write nothing to stdout, write `opsctl: ` and the error's text and LF to stderr, followed by D02's quoted detail when the error carries a `*host.CommandError`, and exit 1, so with `host.name` unset or empty the only output is `opsctl: host.name not set` and the file is as it was.
- R-9NHJ-HJ24: `opsctl services` MUST accept exactly the subcommand `apply` with no operand; before any host access, `services` with no subcommand, an unknown subcommand `<sub>`, and `apply` with any operand MUST respectively exit 2 with empty stdout and on stderr the three LF-terminated lines `opsctl: no services subcommand given`, `opsctl: unknown services subcommand '<sub>'`, or `opsctl: services apply takes no arguments`, then an empty line, then `see 'opsctl services --help' for usage`.
