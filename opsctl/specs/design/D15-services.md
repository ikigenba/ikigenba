# D15-services

Every app's banner carries a launcher listing the host's services, built from
one file opsctl owns: `/var/lib/ikigenba/services.json`. Like the nginx
configuration (D06), the file is a pure function of what is on disk under
`/opt`, of the stored `host.name`, and of which apps systemd reports disabled
(D10's `Disabled`), so it is generated, never edited, and never backed up: a
restored host regenerates it. opsctl owns its format as it owns
`manifest.toml`; apps read it through the variable `IKIGENBA_SERVICES`, which
every app's `etc/env` carries (D05, D09).

A service is in the launcher when it is routed (its manifest names its app,
D06), installed (`bin/<name>` is a regular file), and its package shipped an
icon, `share/icon.svg`. The icon is the whole opt-in; the manifest carries no
launcher key. Package `internal/services` owns that criterion, the file's
exact bytes, and its publication. Package `internal/apps` (D08) owns what the
file is built from and what an app is given: the icon's service-relative path,
the icon check `install` applies in its `file` step, the file's path and
variable name that every app's environment carries, and the `ikigenba`
account whose group owns the file.

The file is rewritten wherever nginx's configuration is regenerated —
`install`, `uninstall`, `enable`, `disable`, `restore`, `init`, and
`nginx apply` — and always after nginx's configuration has succeeded, so the
two files never disagree about which apps are disabled and a command that
fails before or at its nginx step leaves the file as it was. Since every
regeneration refuses a host where a manifest cannot be read (D06), so does
the services file: it never claims to leave such a service out. An icon that
cannot be embedded verbatim — not a regular file, unreadable, or not UTF-8,
as a hand-placed or pre-check icon may be — fails the rewrite too, and the
file is left as it was.

The commands that print step lines (`install`, `uninstall`, `disable`,
`enable`) report the rewrite on a `services` line right after `nginx`. The
line names only the change to the command's own app's entry: `added`,
`removed`, `disabled`, `enabled`, `updated`, or `unchanged` when that entry
did not change, even if another entry did. The change is measured against the
file on disk before the rewrite, because nothing else remembers the previous
icon once `install` has replaced `share/`. `init`, `restore`, and
`nginx apply` rewrite the file silently.

The layout is exact — the object pretty-printed, one service per line — so a
rerun that changes nothing rewrites nothing, and a story can show the file.
Strings are written with the minimal JSON escapes: `<`, `>`, and `&` appear
literally (unlike Go's default HTML-safe encoding), so an icon reads as the
SVG it is. Readers still depend only on the JSON value.

The file has no version field and its format only grows. This is a rule for
later revisions of this design: they may add members to the top-level object
or to an entry, and readers ignore members they do not know; they never
remove, rename, or change the type or meaning of `services`, `name`, `url`,
`icon`, or `enabled`. A change an existing reader could not survive never
edits this file: it becomes a new file with its own variable (for example
`services.v2.json` named by `IKIGENBA_SERVICES_V2`), and opsctl writes both
until no installed app reads the old one.

## REQUIREMENTS

- R-Z9N9-6FPH: Package `internal/services` MUST export type `Change string` with the constants `Unchanged Change = "unchanged"`, `Added Change = "added"`, `Removed Change = "removed"`, `Disabled Change = "disabled"`, `Enabled Change = "enabled"`, and `Updated Change = "updated"`.
- R-885B-IVTA: Package `internal/services` MUST export type `Changes map[string]Change` with the method `func (c Changes) For(app string) Change`, which MUST return `c[app]` when `app` is a key of `c` and `Unchanged` otherwise, including for a nil `Changes`.
- R-89D7-WNJZ: Package `internal/services` MUST export `Write(ctx context.Context, env host.Env, hostName string) (Changes, error)`.
- R-8BT0-O71D: A service returned by `apps.Discover(env.Root)` MUST be a *launcher service* exactly when its `Manifest` is non-nil with a nonempty `App`, `/opt/<name>/bin/<name>` is a regular file, and `/opt/<name>/` joined with `apps.IconPath` exists as a directory entry of any type, every path resolved under `env.Root`; a service lacking any of these MUST NOT appear in the file, and a launcher service MUST appear in it whether or not `apps.Disabled` reports it disabled.
- R-8D0X-1YS2: For each launcher service, `Write` MUST produce one entry whose `name` is the service's `Name`; whose `url` is exactly `https://<name>.<hostName>`, also for the service whose manifest sets `default` and for the one `host.apex` names; whose `icon` is the complete bytes of its icon file, trailing line ending included, unaltered; and whose `enabled` is `false` exactly when `apps.Disabled(ctx, env, <name>)` returns true and `true` when it returns false.
- R-8E8T-FQIR: The bytes `Write` publishes MUST be exactly `{` LF, two spaces, `"services": [`, then — when there are no launcher services — `]` LF `}` LF, so the empty file is `{\n  "services": []\n}\n`; otherwise LF, one line per launcher service in ascending bytewise name order, each line being four spaces, `{ "name": `, the encoded name, `, "url": `, the encoded url, `, "icon": `, the encoded icon, `, "enabled": `, `true` or `false`, and ` }`, the lines separated by `,` LF, followed by LF, two spaces, `]` LF, `}` LF; there MUST be no other member, including no version member, and no other whitespace.
- R-LVKD-L874: Each string value in the file MUST be encoded as `"`, the value's characters, and `"`, where `"` is written `\"`, `\` is written `\\`, U+0008 `\b`, U+000C `\f`, U+000A `\n`, U+000D `\r`, U+0009 `\t`, every other character from U+0000 through U+001F as `\u00` followed by two lowercase hexadecimal digits, U+2028 as `\u2028` and U+2029 as `\u2029`, and every other character — including `<`, `>`, `&`, `/`, U+007F, and all other non-ASCII characters — as its own UTF-8 bytes unchanged; decoding the published file as JSON MUST therefore yield each entry's name, url, and icon byte for byte.
- R-8GOM-7A05: `Write` MUST obtain every entry before creating, modifying, or removing any file or executing any command other than `apps.Disabled` queries, and MUST return nil `Changes` and an error, leaving `/var/lib/ikigenba/` and the services file exactly as they were (bytes, mode, and presence) and executing no account or ownership command, when `hostName` is empty (error text exactly `host.name not set`), when `apps.Discover` fails, when any discovered service has a non-nil `ManifestError` (the error identifying that service and its manifest failure), or when an `apps.Disabled` query fails (the error wrapping its `*host.CommandError`).
- R-8HWI-L1QU: When a launcher service's icon cannot be embedded, `Write` MUST fail as R-8GOM-7A05 prescribes, leaving the file as it was, with an error whose text is exactly `<name>: share/icon.svg is not a regular file` when the entry is not a regular file (a directory or a symbolic link included, which is never followed), `<name>: share/icon.svg is not valid UTF-8` when its bytes are not valid UTF-8, and `<name>: share/icon.svg: ` followed by the read error's text when it cannot be read; the size and SVG rules of `apps.CheckIcon` MUST NOT be applied at regeneration.
- R-8J4E-YTHJ: After obtaining every entry, `Write` MUST call `apps.EnsureAccount(ctx, env)` and return its error before any file change; then, under `env.Root`, it MUST create `/var/lib/ikigenba/` when absent, creating any missing ancestor with mode `0755`, and set that directory's mode to `0750`, so every successful `Write` leaves it a directory with mode `0750` whether it was created, already correct, or had another mode.
- R-8KCB-CL88: When `/var/lib/ikigenba/services.json` under `env.Root` already holds exactly the candidate bytes, `Write` MUST NOT replace it; otherwise it MUST write the candidate to a temporary file in `/var/lib/ikigenba/` and rename that file over `services.json`, so a reader observes either the previous file or the complete candidate, never a partial one, and apart from that temporary file, removed before return whenever it was not renamed, it MUST create, modify, and remove no file other than the directories of R-8J4E-YTHJ and `services.json`.
- R-8LK7-QCYX: Every successful `Write` MUST leave `services.json` a regular file with mode `0640`, its mode applied directly under `env.Root` so a test observes it with `Lstat`, and MUST establish ownership by executing through `env.Execute` exactly one `chown root:ikigenba` naming, in order, `/var/lib/ikigenba` and the file it keeps (the temporary file, before the rename, when it replaces `services.json`; `services.json` itself when it does not), each resolved under `env.Root`; so the directory and file end owned by user `root` and group `ikigenba` whatever their previous owner or mode.
- R-8MS4-44PM: A failure after entries are obtained — of `apps.EnsureAccount`, directory creation or mode, writing, mode, ownership, or rename — MUST make `Write` return nil `Changes` and an error, wrapping `*host.CommandError` for a failed command, and leave `services.json` with its previous bytes and mode or its previous absence, with any temporary file removed.
- R-8O00-HWGB: `Write` MUST execute through `env.Execute` no command other than the `apps.Disabled` queries, the commands of `apps.EnsureAccount`, and the one `chown` of R-8LK7-QCYX; it MUST NOT read the configuration store, reload or test nginx, or change any unit.
- R-LY06-CROI: On success `Write` MUST return `Changes` holding one key for each name whose entry differs between the file before the call and the file it leaves, and no other key: `Added` when the name has no previous entry, `Removed` when it has no new entry, `Disabled` when the previous `enabled` is `true` and the new one `false`, `Enabled` when the previous `enabled` is `false` and the new one `true`, and `Updated` for every other difference; entries are matched by their `name` member and compared only by the JSON values of `url`, `icon`, and `enabled`, ignoring any other member and the layout.
- R-8QFT-9FXP: For classification, the file before the call MUST be taken to hold no entries when it is absent, cannot be read, or does not decode as a JSON object whose `services` member is an array; within such an array, an element that is not an object or whose `name` member is not a string MUST be ignored, and when several elements share a name the first MUST be used. Such a previous file MUST NOT make `Write` fail.
- R-8RNP-N7OE: For `install`, `uninstall`, `disable`, and `enable`, `internal/cli` MUST call `services.Write` exactly once, with the normalised host name it gave nginx, only after the `nginx` step reported success and before any later step, and report step `services`: on success `services: ok (<app> <change>)` with `<app>` the command's app and `<change>` the text of `Changes.For(<app>)`, or exactly `services: ok (unchanged)` when that is `Unchanged`; on error `services: failed: <reason>` with `<reason>` the error's text, CR and LF escaped as the command's other step lines escape them, after which the command attempts no later step, writes `opsctl: <command> failed` to stderr, and exits 1. A run that fails before or at its `nginx` step MUST NOT call `services.Write`.
- R-8SVM-0ZF3: For `nginx apply`, `init`, and `restore`, `internal/cli` MUST call `services.Write` exactly once per run, with the normalised host name it gave nginx, only after nginx's configuration was successfully published (`nginx.Apply` returned nil for `nginx apply` and `init`; `nginx.Write` returned nil inside restore's nginx regeneration callback), print nothing about it on success, and on error fail the command in the form D05, D06, and D14 prescribe for it; a run that fails before or at that publication MUST NOT call `services.Write`.
- R-LZ82-QJF7: Backup operations (D11–D13) MUST NOT read, archive, or upload anything under `/var/lib/ikigenba/`, and restore (D14) MUST NOT write anything there except through `services.Write`: the services file is regenerated, never backed up.
