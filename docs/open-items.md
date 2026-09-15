# Open items — the spaces workflow

Gaps found by reading the `opsctl` and `devctl` stories against the spaces
workflow they describe, together with the infra ground they depend on. Each
item names the stories involved, the evidence, and a suggested resolution.
An item is closed by deleting it; git holds the history. Items are ordered
largest first within each section.

These are story-level items across three projects, so they live here rather
than in any one project's `specs/issues/`, which is gitignored working state
for the build run and halts it while non-empty.

## Missing commands and concepts

### 2. No devctl path to re-run init, change a key, or upgrade opsctl on a live space

- Stories: `devctl/specs/stories/space-lifecycle.md` (create),
  `opsctl/specs/stories/init.md`, `release.md`.
- Evidence: create says "create is the only thing that chooses a version, and
  no later devctl command changes it." init.md says changing a period or a
  zone is `config set` followed by `init`. release.md says a new opsctl acts
  on a host "on the next init". No devctl command runs `config set`, `init`,
  or the saved installer on an existing space.
- Consequence: a durable space (the apex) can never take a new opsctl, a
  changed backup period, or a re-read of its manifests without an operator
  doing it by hand over ssh. Sandbox spaces can be recreated instead.
- Suggested resolution: a `devctl space init <domain>` that re-derives the
  ten keys and runs `opsctl init`, and a `devctl space upgrade <domain>
  [<version>]` that runs the host's saved installer and then init.

### 3. Certificate renewal has no owner

- Stories: `opsctl/specs/stories/certificates.md`, `init.md` (timers step),
  `infra/templates/space-first-boot.sh`.
- Evidence: certificates.md hands renewal to "a systemd timer on the host, or
  `devctl space start`". init's `timers` step writes only the two backup
  timers. First boot installs certbot and says nothing about its timer.
- Consequence: a space that runs for 90 days without a stop/start cycle
  expires its certificate. The stories anticipate this: `--acme-email` is
  required "because a wrong address is only discovered when a certificate
  quietly expires."
- Suggested resolution: either the `timers` step writes and enables a
  `certbot renew` timer, or first boot enables the package's own timer and
  `init` checks that it is enabled.

### 4. The sandbox cannot receive branch work

- Stories: `devctl/specs/stories/build.md`, `deploy.md`; account property
  `deploy_from_main_only`.
- Evidence: build requires HEAD to be on `origin/main` with a tag pointing at
  it, unconditionally, and checks the binary's version against the tag.
  `deploy_from_main_only` appears in every precondition list and is read by
  nothing. The sandbox account sets it to `false`.
- Consequence: the sandbox can only run released code, which defeats the
  purpose the property implies.
- Suggested resolution: decide. Either drop the property, or add a story for
  an account where it is false: build accepts an untagged or off-main HEAD,
  names the file by commit (`crm-<sha>.tar.xz` or similar), skips the
  tag-versus-binary check, and deploy refuses such a file in an account
  where the property is true. Note build takes no `--account`, so the
  decision may belong to deploy.

### 5. Destroy takes no final backup

- Stories: `devctl/specs/stories/space-lifecycle.md` (destroy),
  `opsctl/specs/stories/backup.md`.
- Evidence: destroy's first step terminates the instance. In the durable
  account, service files are copied daily and WAL every 15 minutes, so up to
  a day of files and 15 minutes of committed changes are lost on destroy.
  No story states this.
- Suggested resolution: when the account keeps backups, destroy runs
  `opsctl backup` and `opsctl host backup` over ssh before terminating, and
  reports those as steps. Otherwise the destroy story states the loss.

### 6. Rebuilding a durable space is never sequenced

- Stories: `opsctl/specs/stories/backup.md` (host restore, restore into a
  host that never ran the service), `devctl/specs/stories/space-lifecycle.md`
  (create), `restore.md`, `deploy.md`.
- Evidence: the pieces exist on the opsctl side, but no story walks them in
  order. devctl exposes neither `host restore` nor `init`. create's own
  `init` obtains a fresh certificate before the old one could be restored,
  which is the rate-limit case host backup exists to avoid.
- Suggested resolution: write the disaster-recovery story end to end
  (destroy or lose host, create, host restore, init, restore each app,
  deploy each app) and decide which of its steps devctl drives.

### 7. Apps cannot be removed, restarted, or inspected

- Stories: `opsctl/specs/stories/apps.md`, `devctl/specs/stories/deploy.md`,
  `secrets.md`.
- Evidence: there is no `opsctl uninstall` and no devctl counterpart, so a
  deployed app is permanent. No story restarts an app or reads its journal
  after the install-time relay. Secret rotation works by pushing and then
  deploying the same file again, but no story says so.
- Suggested resolution: an `opsctl uninstall <app>` (stop and disable the
  unit, remove `bin/`, `etc/`, `share/`, regenerate nginx and litestream,
  keep `state/`) with a devctl counterpart; a written story for rotating a
  secret; a decision on whether restart and logs are devctl commands or
  stay ssh-by-hand.

### 8. Seed data has no path

- Stories: `devctl/specs/stories/restore.md`, `infra/AGENTS.md` ("Spaces").
- Evidence: infra says the sandbox "never backs up — its data is seed data".
  restore is deliberately within one space only ("no other space was read").
  Nothing puts data onto a sandbox space.
- Suggested resolution: decide where seed data comes from: the app itself
  on first start, a `devctl` command that stages a tarball under a space's
  own prefix for `restore` to find, or nothing, in which case infra's note
  is corrected.

### 9. The app tag convention is underspecified

- Stories: `devctl/specs/stories/build.md`, `opsctl/specs/stories/release.md`.
- Evidence: build assumes one bare `v*` tag at HEAD names the app's version
  and checks `<app> --version` against it. opsctl releases as
  `opsctl/<version>`. With several apps in one checkout, which tag names
  which app when two point at HEAD, and whether every app's version bumps
  in lockstep with one tag, is undecided.
- Suggested resolution: state the convention. Either every app shares one
  release tag and one version, or tags are `<app>/<version>` and build
  looks for that app's tag alone.

## Where the stories disagree with infra

These are outside the stories, but the workflow depends on them.

### 10. Account backup properties do not match

- Stories read `backup_host_files_seconds`, `backup_service_files_seconds`,
  `backup_service_db_seconds`, `backup_service_wal_seconds` from
  `/ikigenba/account`. `infra/*/account.tf` publishes `backup_full_seconds`,
  `backup_incremental_seconds`, `backup_wal_seconds`.
- Resolution: change Terraform to publish the four keys the stories name.

### 11. The launch template does not install litestream

- `init` requires litestream on PATH; backup.md says it "comes with the
  account's launch template". `space-first-boot.sh` installs nginx, certbot,
  awscli-2, and jq.
- Resolution: first boot installs litestream at a pinned version.

### 12. Elastic IPs

- infra says "A space holds no Elastic IP". Every create story allocates
  one. The default quota is five per region, which the sandbox will hit.
- Resolution: update infra's contract to match the stories, and either
  request a quota increase or state the limit.

### 13. Litestream retention versus the no-delete role

- Litestream's retention expects to delete objects. The space role
  deliberately holds no `s3:DeleteObject`. The stories do not say which
  wins.
- Resolution: check litestream's behaviour when delete is refused; either
  set retention to never in the generated `litestream.yml` and lean on
  bucket expiry, or grant delete under the space's own prefix.

### 14. Sandbox recreation and the duplicate-certificate limit

- Recreating the same sandbox domain repeatedly hits Let's Encrypt's
  duplicate-certificate limit. The sandbox has no host backup to restore a
  certificate from.
- Resolution: state the limit in the create story, or give the sandbox a
  host backup period so the certificate survives a recreate.

## Smaller inconsistencies

### 15. secrets push requires an instance that create has not launched yet

- `secrets push` preconditions: "The space exists in the account (an
  instance tagged `Space=<domain>`)". create pushes secrets before it
  launches the instance.
- Resolution: create's secrets step skips the existence check, and the
  story says so.

### 16. Reserved app names

- An app named `host` or `deploy` collides with the `host/` and `deploy/`
  prefixes under the space's backup URI. Nothing refuses them.
- Resolution: build and install refuse those names.

### 17. create checks app names against the checkout, not the host

- create refuses `<app>.<space>` by reading the checkout's apps. An app
  deployed from an older checkout and since removed from it is not seen.
- Resolution: accept as is and say so, or ask the existing space's host.
