# devctl story design review

## Build, deploy, rotate a secret

```sh
# HEAD has crm/v0.2.0-rc.1 and the checkout is clean.
devctl build crm
# crm/dist/crm-v0.2.0-rc.1.tar.xz
devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.2.0-rc.1.tar.xz
devctl --account 602773793009 secrets push foo.sbx.ikigenba.dev crm
devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.2.0-rc.1.tar.xz
```

Build validates the app tag, binary version and manifest. Both deploys upload
identical artifact bytes and run install; the second applies the pushed secrets.

## Create and reinitialize

```sh
devctl --account 602773793009 space create foo.sbx.ikigenba.dev --acme-email ops@ikigenba.dev
devctl --account 602773793009 space init foo.sbx.ikigenba.dev
devctl --account 602773793009 space init foo.sbx.ikigenba.dev --opsctl v0.2.0 --acme-email alerts@ikigenba.dev
```

Create selects the newest stable published opsctl release and always allocates
an Elastic IP. Plain init keeps the installed release and email.
The account configuration has been observed. Published opsctl and host commands
remain deployment prerequisites; see [external observations](external-observations-2026-09-15.md).

## Rebuild a durable space

```sh
devctl --account 295229566359 space destroy ikigenba.dev
devctl --account 295229566359 space create ikigenba.dev --acme-email ops@ikigenba.dev
devctl --account 295229566359 restore ikigenba.dev crm
devctl --account 295229566359 restore ikigenba.dev dashboard
devctl --account 295229566359 deploy ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
devctl --account 295229566359 deploy ikigenba.dev dashboard/dist/dashboard-v0.0.9.tar.xz
devctl --account 295229566359 space status ikigenba.dev
```

Restore each app before deploying over its data. An unreachable retiring host
requires an explicit `space destroy ikigenba.dev --no-backup` to bypass its
final backup. Success summaries preserve the story-defined backup names, selected backup key,
and certificate renewal outcome; see [host summaries](host-summary-resolution.md).

## Stop, start, inspect and follow

```sh
devctl --account 602773793009 space stop foo.sbx.ikigenba.dev
devctl --account 602773793009 space start foo.sbx.ikigenba.dev
devctl --account 602773793009 space status foo.sbx.ikigenba.dev
devctl --account 602773793009 space restart foo.sbx.ikigenba.dev crm
devctl --account 602773793009 space logs foo.sbx.ikigenba.dev crm --since -1h --follow
# Interrupt to finish following.
```

Stop/start preserve address and DNS. Status and logs relay opaque bytes;
logs streams while the process is still running. Restart uses the installed
host environment; applying pushed secrets uses deploy.

## Restore to a moment; remove and reinstall

```sh
devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm --at 2026-09-11T18:00:00Z
devctl --account 602773793009 remove foo.sbx.ikigenba.dev crm
devctl --account 602773793009 deploy foo.sbx.ikigenba.dev crm/dist/crm-v0.1.0.tar.xz
```

Restore uses only the space's own backups. Remove delegates uninstall while
leaving cloud secrets and artifacts in place.

## Previous design → proposed contract

| Previous usage or behavior | Proposed usage or behavior |
|---|---|
| `space create DOMAIN [--elastic-ip]` | `space create DOMAIN --acme-email ADDRESS`; Elastic IP always allocated |
| Build any HEAD tag reachable from origin/main | Build only an app's `APP/v<semver>` tag, on any branch |
| Deploy through scp and a host `/tmp` file | Upload to the space's S3 deploy prefix; install its URI |
| `restore DOMAIN APP --from SOURCE [--from-account PROFILE]` | `restore DOMAIN APP [--at TIMESTAMP]` using its own backups |
| Stop/start may rewrite DNS | Stop/start preserve the Elastic IP and DNS |
| Destroy starts by terminating the instance | Retained-backup destroy retires first; `--no-backup` explicitly bypasses it |
| No reinitialize, remove, restart or logs command | `space init`, `remove`, `space restart`, `space logs` |
| External error detail printed unquoted | One extra `> ` per external line, including nested quotes |

## Review choices and limitations

- Multiple matching app tags: choose the lexicographically first complete tag.
- Newest opsctl: newest `published_at` among stable, non-draft opsctl releases.
- The existing approved direct module set is preserved and pinned to a verified
  compatible set in D01; see [dependency baseline](dependency-baseline.md).
- No build run or check-spec certification is part of this draft. Gates were
  not run: no Go source or test files exist yet.

## Story coverage

Each of the 94 story headings below has a design owner and relevant behavioral
requirements. These are authoring links, not test coverage. Shared grammar,
root refusal and diagnostic requirements also apply to every command.

### bootstrap

| Story | Design | Requirements |
|---|---|---|
| [A developer asks devctl what it can do](../stories/bootstrap.md#a-developer-asks-devctl-what-it-can-do) | [D2](../design/D02-cli-conventions.md) | `R-C3V3-1JZ7` |
| [A developer asks which devctl they have](../stories/bootstrap.md#a-developer-asks-which-devctl-they-have) | [D2](../design/D02-cli-conventions.md) | `R-DGM7-2KE9`, `R-GV4C-IOFR` |
| [A developer names the account a command acts in](../stories/bootstrap.md#a-developer-names-the-account-a-command-acts-in) | [D2](../design/D02-cli-conventions.md) | `R-DE6E-B0WV`, `R-UYZW-0UKR` |
| [A developer gives the account option no value](../stories/bootstrap.md#a-developer-gives-the-account-option-no-value) | [D2](../design/D02-cli-conventions.md) | `R-9WTY-W51L` |
| [A developer runs devctl with no command](../stories/bootstrap.md#a-developer-runs-devctl-with-no-command) | [D2](../design/D02-cli-conventions.md) | `R-D82W-E67E` |
| [A developer mistypes a command](../stories/bootstrap.md#a-developer-mistypes-a-command) | [D2](../design/D02-cli-conventions.md) | `R-D9AS-RXY3` |
| [A developer mistypes an option](../stories/bootstrap.md#a-developer-mistypes-an-option) | [D2](../design/D02-cli-conventions.md) | `R-DAIP-5POS` |
| [A developer runs devctl as root by mistake](../stories/bootstrap.md#a-developer-runs-devctl-as-root-by-mistake) | [D2](../design/D02-cli-conventions.md) | `R-A1PK-F80D` |

### space-lifecycle

| Story | Design | Requirements |
|---|---|---|
| [A developer asks what `space` can do](../stories/space-lifecycle.md#a-developer-asks-what-space-can-do) | [D6](../design/D06-space.md) | `R-8WFQ-8ZI4` |
| [A developer asks which spaces exist in an account](../stories/space-lifecycle.md#a-developer-asks-which-spaces-exist-in-an-account) | [D6](../design/D06-space.md) | `R-U3XX-UGQ6`, `R-8YVJ-0IZI` |
| [A developer runs a `space` subcommand without naming the account](../stories/space-lifecycle.md#a-developer-runs-a-space-subcommand-without-naming-the-account) | [D2](../design/D02-cli-conventions.md) | `R-3PTI-JABB` |
| [A developer lists an account that has no properties](../stories/space-lifecycle.md#a-developer-lists-an-account-that-has-no-properties) | [D3](../design/D03-cloud-and-account.md) | `R-ZLGA-YMQA` |
| [A developer creates a space](../stories/space-lifecycle.md#a-developer-creates-a-space) | [D7](../design/D07-space-create.md) | `R-E9WN-IVFN`, `R-EDKC-O6NQ`, `R-EIFY-79MI`, `R-F3DQ-ZZV8`, `R-F4LN-DRLX` |
| [A developer creates the apex space](../stories/space-lifecycle.md#a-developer-creates-the-apex-space) | [D7](../design/D07-space-create.md) | `R-EES9-1YEF` |
| [A developer rebuilds a durable space](../stories/space-lifecycle.md#a-developer-rebuilds-a-durable-space) | [D6](../design/D06-space.md), [D7](../design/D07-space-create.md), [D9](../design/D09-deploy-and-restore.md) | `R-3KXX-07CJ`, `R-93SF-DMH9`, `R-FV7X-I3AA`, `R-FHT1-AM4N` |
| [A developer creates a space outside the account's domain](../stories/space-lifecycle.md#a-developer-creates-a-space-outside-the-accounts-domain) | [D7](../design/D07-space-create.md) | `R-Y6TP-DBRN` |
| [A developer creates a space under a subdomain delegated to another account](../stories/space-lifecycle.md#a-developer-creates-a-space-under-a-subdomain-delegated-to-another-account) | [D7](../design/D07-space-create.md) | `R-Y81L-R3IC` |
| [A developer creates a space at a delegated subdomain itself](../stories/space-lifecycle.md#a-developer-creates-a-space-at-a-delegated-subdomain-itself) | [D7](../design/D07-space-create.md) | `R-Y81L-R3IC` |
| [A developer creates a space under an existing space](../stories/space-lifecycle.md#a-developer-creates-a-space-under-an-existing-space) | [D7](../design/D07-space-create.md) | `R-3NDP-RQTX` |
| [A developer creates a space that already exists](../stories/space-lifecycle.md#a-developer-creates-a-space-that-already-exists) | [D7](../design/D07-space-create.md) | `R-Y99I-4V91` |
| [A developer creates a space with a secret missing from the keyring](../stories/space-lifecycle.md#a-developer-creates-a-space-with-a-secret-missing-from-the-keyring) | [D7](../design/D07-space-create.md) | `R-YFD0-1PYI` |
| [A developer runs `space create` without a domain](../stories/space-lifecycle.md#a-developer-runs-space-create-without-a-domain) | [D7](../design/D07-space-create.md) | `R-V1FO-SE25` |
| [A developer runs `space create` without an address for the CA](../stories/space-lifecycle.md#a-developer-runs-space-create-without-an-address-for-the-ca) | [D7](../design/D07-space-create.md) | `R-E8OR-53OY` |
| [A developer's create fails part-way](../stories/space-lifecycle.md#a-developers-create-fails-part-way) | [D7](../design/D07-space-create.md) | `R-EIFY-79MI` |
| [A developer sets a space's host up again](../stories/space-lifecycle.md#a-developer-sets-a-spaces-host-up-again) | [D11](../design/D11-space-init.md) | `R-GOHI-OL2Y`, `R-GPPF-2CTN` |
| [A developer moves a space to a newer opsctl](../stories/space-lifecycle.md#a-developer-moves-a-space-to-a-newer-opsctl) | [D10](../design/D10-host-setup.md), [D11](../design/D11-space-init.md) | `R-G4Z4-K97U`, `R-GPPF-2CTN` |
| [A developer changes where the CA writes](../stories/space-lifecycle.md#a-developer-changes-where-the-ca-writes) | [D10](../design/D10-host-setup.md), [D11](../design/D11-space-init.md) | `R-G8MT-PKFX`, `R-GPPF-2CTN` |
| [A developer's `space init` finds the host not ready](../stories/space-lifecycle.md#a-developers-space-init-finds-the-host-not-ready) | [D6](../design/D06-space.md), [D11](../design/D11-space-init.md) | `R-GQXB-G4KC`, `R-D5NY-WFYQ` |
| [A developer initialises a stopped space, or one that does not exist](../stories/space-lifecycle.md#a-developer-initialises-a-stopped-space-or-one-that-does-not-exist) | [D11](../design/D11-space-init.md) | `R-GN9M-ATC9` |
| [A developer runs `space init` without a domain](../stories/space-lifecycle.md#a-developer-runs-space-init-without-a-domain) | [D11](../design/D11-space-init.md) | `R-GJLX-5I46` |
| [A developer destroys a space](../stories/space-lifecycle.md#a-developer-destroys-a-space) | [D6](../design/D06-space.md) | `R-DSU2-631X`, `R-DU1Y-JUSM` |
| [A developer destroys a space whose account keeps secrets and backups](../stories/space-lifecycle.md#a-developer-destroys-a-space-whose-account-keeps-secrets-and-backups) | [D6](../design/D06-space.md) | `R-3KXX-07CJ`, `R-92KI-ZUQK` |
| [A developer destroys a space whose host cannot take a final backup](../stories/space-lifecycle.md#a-developer-destroys-a-space-whose-host-cannot-take-a-final-backup) | [D6](../design/D06-space.md) | `R-3KXX-07CJ`, `R-D5NY-WFYQ` |
| [A developer destroys a stopped space whose account keeps backups](../stories/space-lifecycle.md#a-developer-destroys-a-stopped-space-whose-account-keeps-backups) | [D6](../design/D06-space.md) | `R-DXPN-P60P`, `R-DYXK-2XRE` |
| [A developer destroys a space without its final backup](../stories/space-lifecycle.md#a-developer-destroys-a-space-without-its-final-backup) | [D6](../design/D06-space.md) | `R-DWHR-BEA0` |
| [A developer destroys a space that is already gone](../stories/space-lifecycle.md#a-developer-destroys-a-space-that-is-already-gone) | [D6](../design/D06-space.md) | `R-DWHR-BEA0`, `R-UIKQ-FPMI` |
| [A developer runs `space destroy` without a domain](../stories/space-lifecycle.md#a-developer-runs-space-destroy-without-a-domain) | [D6](../design/D06-space.md) | `R-TXUF-XM0P` |
| [A developer's destroy fails part-way](../stories/space-lifecycle.md#a-developers-destroy-fails-part-way) | [D6](../design/D06-space.md) | `R-DSU2-631X` |
| [A developer stops a space](../stories/space-lifecycle.md#a-developer-stops-a-space) | [D6](../design/D06-space.md) | `R-U8TJ-DJOY`, `R-8XNM-MR8T` |
| [A developer stops a space that is already stopped](../stories/space-lifecycle.md#a-developer-stops-a-space-that-is-already-stopped) | [D6](../design/D06-space.md) | `R-U8TJ-DJOY`, `R-8XNM-MR8T` |
| [A developer starts a stopped space](../stories/space-lifecycle.md#a-developer-starts-a-stopped-space) | [D6](../design/D06-space.md) | `R-DRM5-SBB8` |
| [A developer asks what a space is running](../stories/space-lifecycle.md#a-developer-asks-what-a-space-is-running) | [D6](../design/D06-space.md) | `R-S9WX-LSSA` |
| [A developer asks what a space with no apps is running](../stories/space-lifecycle.md#a-developer-asks-what-a-space-with-no-apps-is-running) | [D6](../design/D06-space.md) | `R-S9WX-LSSA` |
| [A developer asks what a stopped space is running](../stories/space-lifecycle.md#a-developer-asks-what-a-stopped-space-is-running) | [D6](../design/D06-space.md) | `R-U6DQ-M07K` |
| [A developer stops, starts, or asks about a space that does not exist](../stories/space-lifecycle.md#a-developer-stops-starts-or-asks-about-a-space-that-does-not-exist) | [D6](../design/D06-space.md), [D13](../design/D13-space-app-operations.md) | `R-U55U-88GV`, `R-H97T-6OOR` |
| [A developer's start cannot reach the host](../stories/space-lifecycle.md#a-developers-start-cannot-reach-the-host) | [D6](../design/D06-space.md) | `R-DRM5-SBB8` |
| [A developer restarts an app on a space](../stories/space-lifecycle.md#a-developer-restarts-an-app-on-a-space) | [D13](../design/D13-space-app-operations.md) | `R-HAFP-KGFG` |
| [A developer's restart fails on the host](../stories/space-lifecycle.md#a-developers-restart-fails-on-the-host) | [D6](../design/D06-space.md), [D13](../design/D13-space-app-operations.md) | `R-HAFP-KGFG`, `R-D5NY-WFYQ` |
| [A developer reads an app's journal](../stories/space-lifecycle.md#a-developer-reads-an-apps-journal) | [D13](../design/D13-space-app-operations.md) | `R-HBNL-Y865`, `R-HCVI-BZWU`, `R-HE3E-PRNJ` |
| [A developer follows an app's journal, or reads it from a moment](../stories/space-lifecycle.md#a-developer-follows-an-apps-journal-or-reads-it-from-a-moment) | [D13](../design/D13-space-app-operations.md) | `R-H34B-9TZA`, `R-HCVI-BZWU`, `R-HE3E-PRNJ` |
| [A developer asks for the journal of an app that is not on the space](../stories/space-lifecycle.md#a-developer-asks-for-the-journal-of-an-app-that-is-not-on-the-space) | [D13](../design/D13-space-app-operations.md) | `R-HBNL-Y865`, `R-H1WE-W28L` |
| [A developer runs `space restart` or `space logs` without a domain or an app](../stories/space-lifecycle.md#a-developer-runs-space-restart-or-space-logs-without-a-domain-or-an-app) | [D13](../design/D13-space-app-operations.md) | `R-H4C7-NLPZ` |

### secrets

| Story | Design | Requirements |
|---|---|---|
| [A developer asks what `secrets` can do](../stories/secrets.md#a-developer-asks-what-secrets-can-do) | [D5](../design/D05-secrets.md) | `R-TB68-9LEO` |
| [A developer pushes one app's secrets to a space](../stories/secrets.md#a-developer-pushes-one-apps-secrets-to-a-space) | [D5](../design/D05-secrets.md) | `R-GG2N-GFMQ`, `R-GHAJ-U7DF` |
| [A developer pushes every app's secrets to a space](../stories/secrets.md#a-developer-pushes-every-apps-secrets-to-a-space) | [D5](../design/D05-secrets.md) | `R-GCEY-B4EN`, `R-GG2N-GFMQ` |
| [A developer pushes with a value missing from the keyring](../stories/secrets.md#a-developer-pushes-with-a-value-missing-from-the-keyring) | [D5](../design/D05-secrets.md) | `R-GEUR-2NW1` |
| [A developer pushes an app that is not in the checkout](../stories/secrets.md#a-developer-pushes-an-app-that-is-not-in-the-checkout) | [D5](../design/D05-secrets.md) | `R-GB71-XCNY` |
| [A developer pushes to a space that does not exist](../stories/secrets.md#a-developer-pushes-to-a-space-that-does-not-exist) | [D5](../design/D05-secrets.md) | `R-D209-R4QN` |
| [A developer rotates a secret](../stories/secrets.md#a-developer-rotates-a-secret) | [D5](../design/D05-secrets.md) | `R-D386-4WHC` |
| [A developer asks which secret names a space holds](../stories/secrets.md#a-developer-asks-which-secret-names-a-space-holds) | [D5](../design/D05-secrets.md) | `R-GJQC-LQUT`, `R-D209-R4QN` |
| [A developer asks which secret names a space holds for one app](../stories/secrets.md#a-developer-asks-which-secret-names-a-space-holds-for-one-app) | [D5](../design/D05-secrets.md) | `R-GKY8-ZILI` |
| [A developer lists a space that holds no secrets](../stories/secrets.md#a-developer-lists-a-space-that-holds-no-secrets) | [D5](../design/D05-secrets.md) | `R-GJQC-LQUT`, `R-D209-R4QN` |

### build

| Story | Design | Requirements |
|---|---|---|
| [A developer asks what `build` can do](../stories/build.md#a-developer-asks-what-build-can-do) | [D8](../design/D08-build.md) | `R-GSOJ-R4YD` |
| [A developer builds one app at a release](../stories/build.md#a-developer-builds-one-app-at-a-release) | [D8](../design/D08-build.md) | `R-EOJG-44BZ`, `R-EQZ8-VNTD`, `R-F0QF-XTQX` |
| [A developer builds with uncommitted changes](../stories/build.md#a-developer-builds-with-uncommitted-changes) | [D8](../design/D08-build.md) | `R-6M69-Y36S` |
| [A developer builds a prerelease on a branch](../stories/build.md#a-developer-builds-a-prerelease-on-a-branch) | [D8](../design/D08-build.md) | `R-ETF1-N7AR` |
| [A developer builds at a commit that is not tagged](../stories/build.md#a-developer-builds-at-a-commit-that-is-not-tagged) | [D8](../design/D08-build.md) | `R-EPRC-HW2O` |
| [A developer builds an app whose committed manifest is stale](../stories/build.md#a-developer-builds-an-app-whose-committed-manifest-is-stale) | [D8](../design/D08-build.md) | `R-6UPK-MHDN` |
| [A developer builds an app whose version string is stale](../stories/build.md#a-developer-builds-an-app-whose-version-string-is-stale) | [D8](../design/D08-build.md) | `R-EX2Q-SIIU` |
| [A developer builds an app that is not in the checkout](../stories/build.md#a-developer-builds-an-app-that-is-not-in-the-checkout) | [D4](../design/D04-checkout-and-apps.md) | `R-WIEQ-9TUN` |
| [A developer builds an app with a reserved name](../stories/build.md#a-developer-builds-an-app-with-a-reserved-name) | [D8](../design/D08-build.md) | `R-EUMY-0Z1G` |
| [A developer runs `build` without an app](../stories/build.md#a-developer-runs-build-without-an-app) | [D8](../design/D08-build.md) | `R-6G2S-18HB` |
| [A developer's build does not compile](../stories/build.md#a-developers-build-does-not-compile) | [D8](../design/D08-build.md) | `R-EVUU-EQS5`, `R-EM3N-CKUL` |

### deploy

| Story | Design | Requirements |
|---|---|---|
| [A developer asks what `deploy` can do](../stories/deploy.md#a-developer-asks-what-deploy-can-do) | [D9](../design/D09-deploy-and-restore.md) | `R-F99Q-M7XS` |
| [A developer deploys an app they just built](../stories/deploy.md#a-developer-deploys-an-app-they-just-built) | [D9](../design/D09-deploy-and-restore.md) | `R-FGL4-WUDY`, `R-FHT1-AM4N` |
| [A developer promotes a tested release](../stories/deploy.md#a-developer-promotes-a-tested-release) | [D9](../design/D09-deploy-and-restore.md) | `R-FJ0X-ODVC` |
| [A developer deploys a prerelease to a sandbox](../stories/deploy.md#a-developer-deploys-a-prerelease-to-a-sandbox) | [D9](../design/D09-deploy-and-restore.md) | `R-FJ0X-ODVC` |
| [A developer deploys while the space lacks a secret the app declares](../stories/deploy.md#a-developer-deploys-while-the-space-lacks-a-secret-the-app-declares) | [D9](../design/D09-deploy-and-restore.md) | `R-FK8U-25M1` |
| [A developer deploys a file that does not exist](../stories/deploy.md#a-developer-deploys-a-file-that-does-not-exist) | [D9](../design/D09-deploy-and-restore.md) | `R-08GB-YDTQ` |
| [A developer deploys a file that build did not write](../stories/deploy.md#a-developer-deploys-a-file-that-build-did-not-write) | [D9](../design/D09-deploy-and-restore.md) | `R-FBPJ-DRF6`, `R-FE5C-5AWK` |
| [A developer runs `deploy` without a file](../stories/deploy.md#a-developer-runs-deploy-without-a-file) | [D9](../design/D09-deploy-and-restore.md) | `R-V2NL-65SU` |
| [A developer deploys to a space that does not exist](../stories/deploy.md#a-developer-deploys-to-a-space-that-does-not-exist) | [D9](../design/D09-deploy-and-restore.md) | `R-ZGQ0-SX28` |
| [A developer's deploy fails on the host](../stories/deploy.md#a-developers-deploy-fails-on-the-host) | [D6](../design/D06-space.md), [D9](../design/D09-deploy-and-restore.md) | `R-FHT1-AM4N`, `R-D5NY-WFYQ` |
| [A developer asks what `remove` can do](../stories/deploy.md#a-developer-asks-what-remove-can-do) | [D12](../design/D12-remove.md) | `R-GUL0-LFSF` |
| [A developer takes an app off a space](../stories/deploy.md#a-developer-takes-an-app-off-a-space) | [D12](../design/D12-remove.md) | `R-GZGM-4IR7` |
| [A developer removes an app that is not on the space](../stories/deploy.md#a-developer-removes-an-app-that-is-not-on-the-space) | [D6](../design/D06-space.md), [D12](../design/D12-remove.md) | `R-GZGM-4IR7`, `R-D5NY-WFYQ` |
| [A developer removes from a space that does not exist, or one that is stopped](../stories/deploy.md#a-developer-removes-from-a-space-that-does-not-exist-or-one-that-is-stopped) | [D12](../design/D12-remove.md) | `R-3TH7-OLJE` |
| [A developer runs `remove` without a domain or an app](../stories/deploy.md#a-developer-runs-remove-without-a-domain-or-an-app) | [D12](../design/D12-remove.md) | `R-GVSW-Z7J4` |

### restore

| Story | Design | Requirements |
|---|---|---|
| [A developer asks what `restore` can do](../stories/restore.md#a-developer-asks-what-restore-can-do) | [D9](../design/D09-deploy-and-restore.md) | `R-FHIL-ORT8` |
| [A developer puts a space's app back](../stories/restore.md#a-developer-puts-a-spaces-app-back) | [D9](../design/D09-deploy-and-restore.md) | `R-FV7X-I3AA` |
| [A developer puts a space's app back as it was at a moment](../stories/restore.md#a-developer-puts-a-spaces-app-back-as-it-was-at-a-moment) | [D9](../design/D09-deploy-and-restore.md) | `R-FRK8-CS27`, `R-FV7X-I3AA` |
| [A developer restores to a space that does not exist](../stories/restore.md#a-developer-restores-to-a-space-that-does-not-exist) | [D9](../design/D09-deploy-and-restore.md) | `R-FU01-4BJL` |
| [A developer runs `restore` without a domain or an app](../stories/restore.md#a-developer-runs-restore-without-a-domain-or-an-app) | [D9](../design/D09-deploy-and-restore.md) | `R-FQCB-Z0BI`, `R-FRK8-CS27` |
| [A developer's restore fails on the host](../stories/restore.md#a-developers-restore-fails-on-the-host) | [D6](../design/D06-space.md), [D9](../design/D09-deploy-and-restore.md) | `R-FV7X-I3AA`, `R-D5NY-WFYQ` |
