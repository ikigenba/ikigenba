# Help configuration-key integration correction

Non-normative author evidence, pending independent verification.

## Source precedence

`specs/stories/README.md`, final paragraph, says a command's help lists every
configuration key that command reads, including keys declared by another story
group. That explicit completeness rule resolves the incomplete command help
snapshots in `init.md`, `apps.md`, and `backup.md`: their missing transitive
configuration reads are a derived drafting correction, not a new product
decision. Only configuration-key rows were added to the four help payloads;
all prior help bytes and other requirements are preserved.

## Contract reader inventory

| Command | Keys listed | Contract evidence |
| --- | --- | --- |
| `init` | `host.name`, `dns.provider`, `dns.zones`, `acme.email`, `aws.region`, `backup.s3_uri`, `backup.host_files_seconds`, `backup.service_files_seconds`, `backup.service_db_seconds`, `backup.service_wal_seconds` | D05 preflight reads provider, zones and host; its setup-input contract reads email and calls D11 `SetupReplication` and D12 `SetupTimers`. D11 `SetupReplication` calls `Regenerate`, which consumes region, prefix and database/WAL periods. D12 consumes the two file periods. |
| `install` | `aws.region`, `host.name`, `backup.s3_uri`, `backup.service_db_seconds`, `backup.service_wal_seconds` | D09 installation reads region and host; its CLI Configure callback calls D11 `Regenerate`, adding prefix and database/WAL periods. |
| `uninstall` | `host.name`, `aws.region`, `backup.s3_uri`, `backup.service_db_seconds`, `backup.service_wal_seconds` | D10 removal CLI reads host and calls D11 `Regenerate` through Configure after removal; regeneration consumes region, prefix and database/WAL periods even when no database remains (its generated empty database sequence still has top-level settings). |
| `restore` | `aws.region`, `backup.s3_uri`, `host.name`, `backup.service_db_seconds`, `backup.service_wal_seconds` | D14 reads prefix and region for cloud recovery; CLI reads host for nginx; incoming-database restore calls D11 `Regenerate` after database recovery and before starting units. Conditional reads count toward command help completeness. |

D10 `Restart` and `Status` have no configuration store reader in their declared
contracts: they query installed binaries, units and manifests/databases. Their
help requirements remain unchanged. Generated timer commands and external
certbot hooks are separate command invocations; their own possible config reads
are not attributed to the parent merely because it invokes or schedules them.
D06 nginx receives a host argument, rather than independently reading config.
The existing implementation has no `internal/backup` package; the reader
inventory is the designed target call chain, not a claim of executed behavior.

## Replacement ids

| Help | Removed id | Fresh replacement |
| --- | --- | --- |
| init | R-LGEA-VQO1 | R-ZYRQ-L5HW |
| install | R-OO6V-EUXD | R-ZZZM-YX8L |
| uninstall | R-LUR0-OFG4 | R-017J-COZA |
| restore | R-1M9V-TA5V | R-02FF-QGPZ |

Ids were minted with `idgen -n 4`. Review references in other scopes are for
root reconciliation after this bounded correction.

## Consumer task: discover all settings before operating a host

Current snapshots list only one key for init, two for install, one for uninstall,
and three for restore. An operator following those inventories cannot discover
the replication periods required by the composed regeneration operation.

Proposed usage, runnable as an ordinary user and independent of host state:

```sh
opsctl init --help
opsctl install --help
opsctl uninstall --help
opsctl restore --help
```

Each command exits 0 with empty stderr and its existing exact help text plus the
added configuration rows. The operator can now collect all ten init inputs or
all five inputs for each subsequent command from its own help before configuring
and operating the host. The corresponding `-h` invocations have the same result.
Help itself does not read any of these keys or perform the described setup.
No choices concerning absent replication periods or an absent backup prefix are
made here; those remain under their existing separately recorded policy issue.
