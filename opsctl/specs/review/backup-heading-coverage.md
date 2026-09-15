# Backup heading coverage

Complete source heading inventory: 26 headings. Introduction-level outcomes tracked additionally in model/operation scope ledgers.

| # | Source | Heading | Owner | Status |
|---|---|---|---|---|
| 1 | backup.md:38 | Two mechanisms, and where the line between them falls | D11 | Partial — backup-replication-periods.md |
| 2 | backup.md:113 | What is backed up, and what is not | D11 | Partial — backup-replication-periods.md |
| 3 | backup.md:172 | An operator asks what `backup` can do | D12 | Locally verified; see scoped verification, product/evidence limitations below |
| 4 | backup.md:216 | The host backs up its services | D12 | Locally verified; see scoped verification, product/evidence limitations below |
| 5 | backup.md:258 | An operator backs up one service | D12 | Locally verified; see scoped verification, product/evidence limitations below |
| 6 | backup.md:287 | The host backs up a service it cannot read | D12 | Locally verified; see scoped verification, product/evidence limitations below |
| 7 | backup.md:320 | An operator backs up a service that is not there | D12 | Locally verified; see scoped verification, product/evidence limitations below |
| 8 | backup.md:344 | An agent backs up a host with nowhere to put it | D12 | Locally verified; see scoped verification, product/evidence limitations below |
| 9 | backup.md:370 | An operator asks what `host` can do | D13 | Locally verified; see scoped verification, product/evidence limitations below |
| 10 | backup.md:418 | The host backs up its own configuration | D13 | Locally verified; see scoped verification, product/evidence limitations below |
| 11 | backup.md:452 | An operator gives a rebuilt host its certificate back | D13 | Locally verified; see scoped verification, product/evidence limitations below |
| 12 | backup.md:490 | An operator asks for a period of zero | D12 | Partial — backup-replication-periods.md (all-zero DB consequence) |
| 13 | backup.md:531 | An operator asks what `retire` can do | D13 | Partial — retire-final-database-guarantee.md |
| 14 | backup.md:577 | An operator takes a host's final backup | D13 | Partial — retire-final-database-guarantee.md |
| 15 | backup.md:639 | A host's final backup cannot read one service | D13 | Partial — retire-final-database-guarantee.md |
| 16 | backup.md:675 | An operator asks what `restore` can do | D14 | Locally verified; see scoped verification, product/evidence limitations below |
| 17 | backup.md:729 | An operator puts a service back as it was | D14 | Locally verified; see scoped verification, product/evidence limitations below |
| 18 | backup.md:783 | An operator puts a service back as it was at a moment | D14 | Locally verified; see scoped verification, product/evidence limitations below |
| 19 | backup.md:829 | An operator asks for a moment with no backup before it | D14 | Locally verified; see scoped verification, product/evidence limitations below |
| 20 | backup.md:860 | An operator puts back a service that keeps no database | D14 | Partial — restore-database-removal.md |
| 21 | backup.md:900 | An operator restores a service that was already stopped | D14 | Locally verified deliberate-inactive behavior; retry issue belongs to files-without-replica heading |
| 22 | backup.md:938 | An operator restores a service with no backups | D14 | Locally verified; see scoped verification, product/evidence limitations below |
| 23 | backup.md:962 | A restore finds files but no database | D14 | Partial — restore-retry-inactive.md |
| 24 | backup.md:1018 | An operator restores into a host that has never run the service | D14 | Locally verified; see scoped verification, product/evidence limitations below |
| 25 | backup.md:1068 | An operator runs restore with no service, or more than one | D14 | Locally verified; see scoped verification, product/evidence limitations below |
| 26 | backup.md:1096 | An operator runs `host` with no subcommand, or one that does not exist | D13 | Locally verified; see scoped verification, product/evidence limitations below |

## Classification

Product-decision partial headings: 8. Root independent integration confirms 18 verified / 8 partial headings. Remaining headings have verified completed contracts; D14 callback corrections passed final local verification. Root owns the remaining transitive configuration-key help correction and its final verification. External observations apply separately and are not represented as production verification.

The first two model headings include completed D08/D11 contracts and unresolved zero/unset configuration. Retirement help’s grammar is verified while its advertised no-loss guarantee is partial. The period-zero heading completes file timers but its all-zero database consequence shares the replication issue. Ordinary restore headings inherit incoming-no-database and retry issues only where those transitions apply.
