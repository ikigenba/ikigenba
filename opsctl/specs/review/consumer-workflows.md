# Operator workflow review

These are proposed contract examples, not commands executed by this drafting run. The draft is partial until linked product decisions are resolved. See each scope review for exact output and failure examples.

## Configure and initialize one host

Run as root on the selected host. Use the real hosted-zone ID and host-specific S3 prefix.

```sh
opsctl config set dns.provider=route53
opsctl config set dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78
opsctl config set host.name=ikigenba.dev
opsctl config set acme.email=ops@ikigenba.dev
opsctl config set aws.region=us-east-2
opsctl config set backup.s3_uri=s3://example-host-backups/ikigenba.dev/
opsctl config set backup.host_files_seconds=86400
opsctl config set backup.service_files_seconds=86400
opsctl config set backup.service_db_seconds=86400
opsctl config set backup.service_wal_seconds=60
opsctl init
```

This uses positive replication periods, the currently specified success domain. [Zero/unset period and absent-prefix decisions](../issues/backup-replication-periods.md) remain open. Cloud production adapter approval and external observations are also pending. [Detailed setup and failure usage](early-init.md).

## Install and operate an app

The artifact must satisfy the declared app manifest/archive contract and the configured secret parameter must supply its requested names.

```sh
opsctl install s3://example-host-backups/ikigenba.dev/deploy/crm-v0.1.0.tar.xz
opsctl status
opsctl restart crm
opsctl uninstall crm
```

Uninstall retains app state. Its final Litestream shutdown synchronization shares the [pending synchronization proof and guarantee](../issues/retire-final-database-guarantee.md). [Install progress-output conflicts](../issues/app-install-output-conflicts.md) and [sensitive journal output](../issues/install-journal-secret.md) remain open. See the app scope reviews for full preconditions and exact behavior.

## Back up and recover

With backups and required cloud access available:

```sh
opsctl backup crm
opsctl host backup
opsctl restore crm
```

Restore has service-state and replication effects; the detailed restore usage documents them. [Database-removal](../issues/restore-database-removal.md) and [retry activation](../issues/restore-retry-inactive.md) policies remain open. Retirement's [final database-sync guarantee](../issues/retire-final-database-guarantee.md) must be settled before treating it as proof that a host can be discarded.

## Install or upgrade the opsctl binary

See [release usage](release-usage.md). It shows fetching and invoking the release installer, upgrading through its saved copy, and failures. The sample release asset remains unavailable in the recorded live probe; publishing or installing a release was not part of this draft run.

## Additional scope examples

- [Shared in-process and cloud consumers](boundaries.md)
- [Bootstrap and configuration](early-cli-config.md)
- [DNS and ACME hooks](early-dns.md)
- [Nginx preview/apply](webhost-nginx-author.md)
- [Certificates](webhost-cert-author.md)
- [App model](apps-model-usage.md)
- [Replication](backup-model-consumers.md)
- [Service restore](backup-restore-consumers.md)
- [Installer test isolation](ground-usage.md)
