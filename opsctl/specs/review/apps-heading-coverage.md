# Apps heading coverage

Complete source heading inventory: 25 headings. Introduction-level outcomes tracked additionally in model/operation scope ledgers.

| # | Source | Heading | Owner | Status |
|---|---|---|---|---|
| 1 | apps.md:79 | An operator asks what `install` can do | D09 | Locally verified; see scoped verification, product/evidence limitations below |
| 2 | apps.md:124 | An agent installs an app | D09 | Partial — app-install-output-conflicts.md |
| 3 | apps.md:188 | An agent deploys a new version over a running app | D09 | Partial — app-install-output-conflicts.md |
| 4 | apps.md:232 | An agent installs the host's default app | D09 | Partial — app-install-output-conflicts.md |
| 5 | apps.md:270 | An operator installs a second default app | D09 | Locally verified; see scoped verification, product/evidence limitations below |
| 6 | apps.md:305 | An agent installs an app whose secret has never been pushed | D09 | Locally verified; see scoped verification, product/evidence limitations below |
| 7 | apps.md:336 | An operator installs a file that is not an app | D09 | Locally verified; see scoped verification, product/evidence limitations below |
| 8 | apps.md:365 | An operator installs an app with a reserved name | D09 | Locally verified; see scoped verification, product/evidence limitations below |
| 9 | apps.md:392 | An agent installs an app whose service will not come up | D09 | Partial — app-install-output-conflicts.md; install-journal-secret.md |
| 10 | apps.md:437 | An operator runs `install` with no file, or more than one | D09 | Locally verified; see scoped verification, product/evidence limitations below |
| 11 | apps.md:465 | An operator asks what `uninstall` can do | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 12 | apps.md:508 | An agent uninstalls an app | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 13 | apps.md:570 | An operator uninstalls the host's default app | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 14 | apps.md:607 | An operator uninstalls an app that is not installed | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 15 | apps.md:637 | An operator runs `uninstall` with no app, or more than one | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 16 | apps.md:664 | An operator asks what `restart` can do | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 17 | apps.md:697 | A developer restarts an app | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 18 | apps.md:730 | A developer restarts an app whose service will not come back | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 19 | apps.md:762 | An operator restarts an app that is not installed | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 20 | apps.md:790 | An operator asks what `status` can do | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 21 | apps.md:831 | A developer asks what a host is running | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 22 | apps.md:872 | A developer asks what a host with no apps is running | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 23 | apps.md:895 | A developer asks about a host holding a service opsctl did not install | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 24 | apps.md:925 | A developer asks about a host whose database stopped being replicated | D10 | Locally verified; see scoped verification, product/evidence limitations below |
| 25 | apps.md:967 | An operator gives status an argument | D10 | Locally verified; see scoped verification, product/evidence limitations below |

## Classification

Product-decision partial headings: 4. Remaining headings have verified completed contracts; D14 callback corrections passed final local verification. Root owns the remaining transitive configuration-key help correction and its final verification. External observations apply separately and are not represented as production verification.

The introduction-level claim equating install/restart progress with status is unresolved. Uninstall at line508 has verified ordering/conditional sync, but actual shutdown synchronization and detectable failure are not proven; see retirement and external-observation issues. All app operation success branches also rely on the separately tracked replication-period policy where regeneration is required.
