# CLI and config author review

Status: authored, awaiting independent verification. Sources: current working-tree `specs/stories/bootstrap.md`, `config.md`, and each story's top-level Commands additions. No source, test, gate, build, or commit operations were performed.

## Current and proposed complete consumer usage

Current contract: `opsctl --help` advertises only config, dns, init, version. Proposed: it advertises backup, cert, config, dns, host, init, install, nginx, restart, restore, retire, status, uninstall, version, with each source's exact description. Release adds no command. The surrounding help frame is unchanged. Domain command execution belongs to those domain documents.

The following complete bootstrap/config tasks use identical current and proposed command syntax. The proposed contract makes the exact help bytes normative, makes read-only effects explicit, and strengthens persistence across concurrent processes and existing directories.

```sh
# Discover the host tool as an ordinary user. Each exits 0, stderr empty.
opsctl --help
opsctl -h
opsctl version
opsctl -V
opsctl --version
opsctl config --help
opsctl config -h
# Version outputs the source version and a newline; v0.1.0 is story example data.
# As an ordinary user these exit 3, stdout empty, stderr: opsctl: must run as root
opsctl init
opsctl config list
# These exit 2, stdout empty, with diagnostic + blank + top-level help hint.
opsctl
opsctl bogus
opsctl --bogus

# Root configures a fresh or existing host; every set exits 0, both streams empty.
sudo opsctl config set host.name=foo.sbx.ikigenba.dev
sudo opsctl config set aws.region=us-east-2
sudo opsctl config set acme.email=ops@ikigenba.dev
sudo opsctl config set dns.provider=route53
sudo opsctl config set dns.zones=sbx.ikigenba.dev:Z02587302QXWONVKW632
sudo opsctl config set backup.s3_uri=s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/
sudo opsctl config set backup.host_files_seconds=86400
sudo opsctl config set backup.service_files_seconds=86400
sudo opsctl config set backup.service_db_seconds=86400
sudo opsctl config set backup.service_wal_seconds=300
sudo opsctl config get dns.zones
# stdout: sbx.ikigenba.dev:Z02587302QXWONVKW632; exit 0
sudo opsctl config list
# All ten KEY=VALUE lines, sorted by key; exit 0.
sudo opsctl config del acme.email
sudo opsctl config del acme.email
# Both exit 0, both streams empty, remaining nine values unchanged.
sudo opsctl config set app.flags=--verbose=true
sudo opsctl config get app.flags
# stdout: --verbose=true; exit 0
sudo opsctl config set 'app.markup=<a>&'
sudo opsctl config set app.empty=
sudo opsctl config get app.empty
# Exactly one newline; absent keys instead fail.
sudo opsctl config del backup.s3_uri
sudo opsctl config get backup.s3_uri
# Exit 1, no stdout; stderr: opsctl: key not set: backup.s3_uri
sudo opsctl config set dns.zones
sudo opsctl config set DNS.Zones=ikigenba.dev
sudo opsctl config set motd='line one
line two'
# Each exits 2, no stdout, exact source diagnostic + blank + config help hint.
sudo opsctl config
sudo opsctl config unset dns.zones
# Exit 2, no changes, exact source diagnostic + blank + config help hint.
sudo opsctl config set backup.service_wal_seconds=60 &
sudo opsctl config set app.flags=--verbose=true &
wait
sudo opsctl config get backup.service_wal_seconds
sudo opsctl config get app.flags
# Values 60 and --verbose=true both persist.
```

Additional complete fixture tasks: with no config file, `config list` returns empty stdout/0, `config get backup.s3_uri` returns missing-key stderr/1, and `config del acme.email` returns empty stdout/0; all leave the file absent. With a config file containing only acme.email, dns.provider, dns.zones, host.name and the source values, `config list` prints exactly the four source lines. With malformed JSON, a non-object, or a non-string value, each of `config get dns.zones`, `config list`, `config set dns.provider=route53`, and `config del dns.provider` returns empty stdout, `opsctl: /etc/ikigenba/config.json is corrupt\n` on stderr, and exit 1; original bytes remain unchanged. These current/proposed interactions agree. Proposed filesystem-error behavior additionally specifies operation/path diagnostics and exit 1.

## Stable source criteria ledger

Locators below use exact source heading plus local labels. Each row expands into the separately named criteria `.intro`, `.commands`, `.output`, `.pre`, `.post`, and `.acceptance`; a source with no explicit acceptance heading uses `.acceptance` for the stated aggregate outcome, not invented acceptance text. `N/A` means that heading contributes no additional intro outcome. Preconditions such as installed tool and root are fixture conditions, supported by D01's Run seam and D02's root rule rather than requirements that provision a host.

Global supporting requirements: grammar R-N211-TYS0; successful stderr R-NGNU-F7OC; diagnostics R-EQ1R-60AG; products R-ER9N-JS15; read-only CLI paths R-ESHJ-XJRU; root R-ENLY-EGT2; no-root exemptions R-ND05-9WG9. These apply to every relevant row.

| Stable locator and exact heading | intro | commands | output | pre | post | acceptance |
|---|---|---|---|---|---|---|
| B.00 bootstrap opening | R-ER9N-JS15, R-EQ1R-60AG | R-EJY9-95KZ | R-ER9N-JS15 | D01 host CLI | global read-only/root | stdout product, stderr diagnostics |
| B.01 An operator asks opsctl what it can do | R-EJY9-95KZ, R-EL65-MXBO | R-EL65-MXBO | R-EL65-MXBO | installed tool fixture | R-ESHJ-XJRU | exact help/0 |
| B.02 An operator asks which opsctl a host has | R-EME2-0P2D | R-NAKC-ICYV | R-NAKC-ICYV | installed tool fixture | R-ESHJ-XJRU | source-held semver |
| B.03 An operator runs opsctl as an ordinary user | R-ENLY-EGT2 | R-ENLY-EGT2 | R-ENLY-EGT2 | EUID != 0 | R-ENLY-EGT2 | refuses before host access |
| B.04 An operator asks for help as an ordinary user | R-ND05-9WG9 | R-ND05-9WG9 | R-EL65-MXBO, R-NAKC-ICYV, R-F5WG-50XH | EUID != 0 | R-ESHJ-XJRU | help/version only exemptions |
| B.05 An agent runs opsctl with no command | N/A | R-CYFL-TDTY | R-CYFL-TDTY | installed tool fixture | R-ESHJ-XJRU | exact diagnostic/2 |
| B.06 An agent names a command that does not exist | R-CZNI-75KN | R-CZNI-75KN | R-CZNI-75KN | installed tool fixture | R-ESHJ-XJRU | names unknown input |
| B.07 An agent names an option that does not exist | R-N211-TYS0 | R-D0VE-KXBC | R-D0VE-KXBC | installed tool fixture | R-ESHJ-XJRU | rejects unknown option/2 |
| C.00 config opening | R-W563-Q6GK, R-W6E0-3Y79, R-W7LW-HPXY, R-NLJF-YAN4, R-NMRC-C2DT, R-NP75-3LV7 | R-EJY9-95KZ | R-EL65-MXBO | flat store | R-F74C-ISO6 | generic string map, empty distinct from absent |
| C.01 An agent asks what `config` can do | N/A | R-F5WG-50XH | R-F5WG-50XH | any EUID | R-F5WG-50XH | exact config help/0 |
| C.02 An agent configures a fresh host | R-O1E4-XBA5, R-F74C-ISO6 | R-O1E4-XBA5 | R-O1E4-XBA5, R-NGNU-F7OC | root, absent or present directory | R-F28Q-ZPPE, R-NP75-3LV7, R-F74C-ISO6, R-F3GN-DHG3, R-F4OJ-R96S | all ten keys exact; others unchanged |
| C.03 An operator reads one value back | N/A | R-R352-8LRC | R-R352-8LRC | given stored value | R-F74C-ISO6 | value/0, no changes |
| C.04 An operator reads a value that was never set | R-NQF1-HDLW | R-R352-8LRC | R-R352-8LRC | absent key or file | R-F74C-ISO6 | missing/1, no file creation |
| C.05 An operator reads the whole store | R-NSUU-8X3A | R-O51U-2MI8 | R-O51U-2MI8 | four-key or empty fixture | R-F74C-ISO6 | sorted complete list/0 |
| C.06 An operator removes a key, twice | R-2ALD-C3F6 | R-O3TX-OURJ | R-O3TX-OURJ, R-NGNU-F7OC | existing key fixture | R-2ALD-C3F6, R-F74C-ISO6, R-F3GN-DHG3 | idempotent removal, absent file stays absent |
| C.07 An agent sets a value that contains an `=` | R-O1E4-XBA5 | R-O1E4-XBA5, R-R352-8LRC | R-R352-8LRC | root | R-NP75-3LV7, R-R5KV-058Q | first equals split, literal HTML chars |
| C.08 An agent sets a malformed key or value | R-NLJF-YAN4, R-NMRC-C2DT | R-8Q6R-PBF6 | R-8Q6R-PBF6 | root | R-8Q6R-PBF6, R-NMRC-C2DT | three ordered validation failures, unchanged bytes |
| C.09 An operator runs `config` with no subcommand, or one that does not exist | N/A | R-D3B7-CGSQ, R-F8C8-WKEV | R-D3B7-CGSQ, R-F8C8-WKEV | root | R-F8C8-WKEV | no/unknown subcommand/2 |
| C.10 An operator works against a corrupt config file | R-WEXA-SCE4 | R-WG57-644T | R-WG57-644T | corrupt fixture | R-WEXA-SCE4, R-WG57-644T | error rather than empty-store reset |
| C.11 Two agents write different keys at the same moment | R-F4OJ-R96S | R-O1E4-XBA5 | R-O1E4-XBA5, R-NGNU-F7OC | root, concurrent processes | R-F4OJ-R96S, R-F3GN-DHG3 | both values persist, complete reader snapshots |

Config opening's “first backed up” is owned by backup; “ten keys are every key required for init” is a cross-document inventory owned by init/integration, not a restriction preventing custom keys.

## Commands contribution ledger

`bootstrap` contributes version; `config` config; `dns` dns; `init` init; `apps` install/uninstall/restart/status; `nginx` nginx; `certificates` cert; `backup` backup/host/restore/retire; `release` no command. Each contribution's `.commands` and `.output` criteria map to R-EJY9-95KZ and R-EL65-MXBO. Alphabetical ordering preserves the existing usage convention; exact source descriptions are retained.

## New requirement trace

- R-EJY9-95KZ, R-EL65-MXBO: all Commands contributions and B.01.
- R-EME2-0P2D: B.02 source-held version, no injection.
- R-ENLY-EGT2: B.03 refusal before any access.
- R-EQ1R-60AG: B.00 and ancestor AGENTS diagnostic convention (external lines quoted; own detail unprefixed).
- R-ER9N-JS15: B.00 stdout answer without decoration and ancestor report convention.
- R-ESHJ-XJRU: B.01–B.07 no-change postconditions.
- R-ETPG-BBIJ, R-EUXC-P398, R-EW59-2UZX, R-EXD5-GMQM, R-EYL1-UEHB, R-EZSY-8680, R-F10U-LXYP: necessary existing public declarations serving C.00–C.11, split from combined structural declaration without reshaping.
- R-F28Q-ZPPE: C.02 permissions postcondition includes an existing directory.
- R-F3GN-DHG3: C.02/C.06/C.11 atomic publication; excludes impossible assertion that a never-created file was always present.
- R-F4OJ-R96S: C.11 simultaneous command processes, not merely goroutines.
- R-F5WG-50XH: C.01 exact help and no-change postcondition.
- R-F74C-ISO6: C.02–C.06 preserve other keys and read-only behavior.
- R-F8C8-WKEV: necessary argument grammar declaration supporting C.01 and C.09; no/unknown command no-change and stdout postconditions.
- R-2ALD-C3F6: C.06 absent-key success scoped to accessible valid store, consistent with C.10 corrupt-store failure.
- R-2BT9-PV5V: necessary filesystem failure contract supporting real file operations and B.01 operation-failure exit semantics.

## Decisions and seam evidence

No exported shapes added. The existing `internal/config` Store/Entry and operations remain coherent as one concern; CLI remains the user-facing adapter. D01 retains Root/EUID and owns dependency direction. All paths used by config resolve against Store.Root, with CLI supplying Deps.Root (D01). No new external dependency introduced; flock is an existing host contract. Live dependency observations belong to the root evidence review. Design-only text changes retain unchanged requirement lines byte-for-byte and mint replacements for changed ones.

Potential integration concern for verifier: global successful stderr-empty rule R-NGNU-F7OC is retained; later domain requirements must not emit successful warnings. Story bootstrap's unprefixed further detail applies to opsctl-authored text; ancestor instruction specifically requires external-program output quoted. No unresolved product decision identified locally.

- R-VW8X-AM1W traces to ancestor external-output diagnostic convention and D01's new `host.CommandError` seam: wrapped external failure carries stdout/stderr to quoted CLI diagnostic detail. This adds no export. Failure captures are diagnostics, distinct from the command's own report findings that stay on stdout. The domain error's captured field names were coordinated with D01; integration verifier checks them against its final declarations.

## Mechanical id handoff

Current cumulative delta from HEAD: 33 added, 15 removed, 19 retained byte-identical requirement lines. Added IDs already tagged in tests: 0. Removed IDs still tagged in tests: 14. Mechanical accounting only; no tests or gates run. Root owns canonical project gap.

Added: R-2ALD-C3F6, R-2BT9-PV5V, R-EJY9-95KZ, R-EL65-MXBO, R-EME2-0P2D, R-ENLY-EGT2, R-EQ1R-60AG, R-ER9N-JS15, R-ESHJ-XJRU, R-ETPG-BBIJ, R-EUXC-P398, R-EW59-2UZX, R-EXD5-GMQM, R-EYL1-UEHB, R-EZSY-8680, R-F10U-LXYP, R-F28Q-ZPPE, R-F3GN-DHG3, R-F4OJ-R96S, R-F5WG-50XH, R-F74C-ISO6, R-F8C8-WKEV, R-VW8X-AM1W, R-W563-Q6GK, R-W6E0-3Y79, R-W7LW-HPXY, R-W8TS-VHON, R-WA1P-99FC, R-WB9L-N161, R-WCHI-0SWQ, R-WEXA-SCE4, R-WG57-644T, R-WHD3-JVVI.

Removed: R-3C3B-V34P, R-EALS-YXXE, R-EBTP-CPO3, R-N9CG-4L86, R-NBS8-W4PK, R-NHVQ-SZF1, R-NJ3N-6R5Q, R-NKBJ-KIWF, R-NNZ8-PU4I, R-NRMX-V5CL, R-NU2Q-MOTZ, R-NVAN-0GKO, R-NWIJ-E8BD, R-NYYC-5RSR, R-R0P9-H29Y.

## Round 1 corrections awaiting verification

R-WHD3-JVVI traces to C.08 malformed arguments and C.09 command grammar: validation precedes reading even a corrupt or inaccessible store. R-WEXA-SCE4 and R-WG57-644T scope corruption to valid input. R-W563-Q6GK, R-W6E0-3Y79, R-W7LW-HPXY, R-W8TS-VHON, R-WA1P-99FC, R-WB9L-N161, R-WCHI-0SWQ split constants and errors into individual declarations.

With corrupt config bytes, repeat the three malformed `config set` examples above: each still exits 2 with its original exact diagnostic, no stdout, and unchanged bytes. A valid `config set dns.provider=route53` instead exits 1 with the corrupt-file diagnostic.

## Individual source preconditions and postconditions

The earlier aggregate `.pre` and `.post` labels remain aliases for all numbered children below, preserving the round-1 verifier locators. Each bullet is located by exact story heading from the table above, section name, and one-based bullet number. Mappings remain proposals pending fresh verification.

| Criterion | Aggregate alias | Complete source bullet | Requirements or fixture |
|---|---|---|---|
| B.01.pre.01 | B.01.pre | `opsctl` is installed on the host. | installed tool fixture |
| B.01.post.01 | B.01.post | Nothing has changed. | R-ESHJ-XJRU |
| B.02.pre.01 | B.02.pre | `opsctl` is installed on the host. | installed tool fixture |
| B.02.post.01 | B.02.post | Nothing has changed. | R-ESHJ-XJRU |
| B.03.pre.01 | B.03.pre | `opsctl` is installed on the host. | EUID != 0 |
| B.03.pre.02 | B.03.pre | The effective user id is not 0. | EUID != 0 |
| B.03.post.01 | B.03.post | Nothing has changed. | R-ENLY-EGT2 |
| B.04.pre.01 | B.04.pre | `opsctl` is installed on the host. | EUID != 0 |
| B.04.pre.02 | B.04.pre | The effective user id is not 0. | EUID != 0 |
| B.04.post.01 | B.04.post | Nothing has changed. | R-ESHJ-XJRU |
| B.05.pre.01 | B.05.pre | `opsctl` is installed on the host. | installed tool fixture |
| B.05.post.01 | B.05.post | Nothing has changed. | R-ESHJ-XJRU |
| B.06.pre.01 | B.06.pre | `opsctl` is installed on the host. | installed tool fixture |
| B.06.post.01 | B.06.post | Nothing has changed. | R-ESHJ-XJRU |
| B.07.pre.01 | B.07.pre | `opsctl` is installed on the host. | installed tool fixture |
| B.07.post.01 | B.07.post | Nothing has changed. | R-ESHJ-XJRU |
| C.01.pre.01 | C.01.pre | `opsctl` is installed on the host. | any EUID |
| C.01.post.01 | C.01.post | Nothing has changed. | R-F5WG-50XH |
| C.02.pre.01 | C.02.pre | `opsctl` is installed on the host and is running as root. | root, absent or present directory |
| C.02.pre.02 | C.02.pre | `/etc/ikigenba/` may or may not exist. | root, absent or present directory |
| C.02.post.01 | C.02.post | `/etc/ikigenba/` exists with mode `0700` and `/etc/ikigenba/config.json` with mode `0600`, holding a JSON object of string values with its keys in sorted order, two-space indentation, and a trailing newline. | R-F28Q-ZPPE, R-NP75-3LV7 |
| C.02.post.02 | C.02.post | The ten keys hold exactly the values given. Any other key is untouched. | R-O1E4-XBA5, R-NP75-3LV7, R-F74C-ISO6 |
| C.02.post.03 | C.02.post | A reader at any moment saw a complete file: each write went to a temporary file in the same directory and was renamed over `config.json` under an exclusive lock on `/etc/ikigenba/config.lock`. | R-F3GN-DHG3, R-F4OJ-R96S |
| C.03.pre.01 | C.03.pre | `dns.zones` is set to that value. | given stored value |
| C.03.post.01 | C.03.post | Nothing has changed. | R-F74C-ISO6 |
| C.04.pre.01 | C.04.pre | `backup.s3_uri` is not in the store, or `config.json` does not exist. | absent key or file |
| C.04.post.01 | C.04.post | Nothing has changed. A missing `config.json` was not created. | R-F74C-ISO6 |
| C.05.pre.01 | C.05.pre | The store holds those four keys. | four-key or empty fixture |
| C.05.post.01 | C.05.post | Nothing has changed. | R-F74C-ISO6 |
| C.06.pre.01 | C.06.pre | `acme.email` is set before the first command. | existing key fixture |
| C.06.post.01 | C.06.post | `acme.email` is not in the store. Every other key is untouched, and `config.json` is a complete file at every moment. | R-2ALD-C3F6, R-F74C-ISO6, R-F3GN-DHG3 |
| C.06.post.02 | C.06.post | `del` against a missing `config.json` returns 0 and does not create it. | R-2ALD-C3F6 |
| C.07.pre.01 | C.07.pre | `opsctl` is running as root. | root |
| C.07.post.01 | C.07.post | `app.flags` holds `--verbose=true`. | R-O1E4-XBA5, R-NP75-3LV7 |
| C.07.post.02 | C.07.post | Characters a JSON encoder likes to escape — `<`, `>`, `&` — are written into `config.json` literally, so a value reads the same in the file as it does from `get`. | R-R5KV-058Q |
| C.08.pre.01 | C.08.pre | `opsctl` is running as root. | root |
| C.08.post.01 | C.08.post | The store is byte for byte as it was. No key was created or changed. | R-8Q6R-PBF6, R-NMRC-C2DT |
| C.09.pre.01 | C.09.pre | `opsctl` is running as root. | root |
| C.09.post.01 | C.09.post | Nothing has changed. | R-F8C8-WKEV |
| C.10.pre.01 | C.10.pre | `/etc/ikigenba/config.json` exists and is not a JSON object whose values are all strings. | corrupt fixture |
| C.10.post.01 | C.10.post | The corrupt file is exactly as it was. `set` and `del` wrote nothing. | R-WEXA-SCE4, R-WG57-644T |
| C.11.pre.01 | C.11.pre | `opsctl` is running as root. | root, concurrent processes |
| C.11.post.01 | C.11.post | Both keys are in the store with the values given. Neither write was lost, and `config.json` was a complete, parseable file at every moment. | R-F4OJ-R96S, R-F3GN-DHG3 |
