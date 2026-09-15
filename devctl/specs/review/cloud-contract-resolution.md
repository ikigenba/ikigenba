# Host role contract correction

Reviewed 2026-09-15 against the existing create, init, secret-consumption,
backup, and deploy stories. No AWS mutation or sibling-project inspection was
performed.

## Concrete omission

D07 requires an embedded role policy and S3 access, but never specifies the
SSM read or DNS challenge permissions needed by the host that create provisions.
The historical devctl D7 at commit `70765b0` describes the substitutions and an
SSM ARN, S3 resources, and DNS conditions; it does not contain a policy document
that the current build could reproduce. Its domain/wildcard DNS conditions
also do not identify the TXT challenge record.

The existing space certificate covers the space domain and its wildcard. Both
use `_acme-challenge.<domain>` for DNS validation. The Route53 provider needs
zone discovery, submission of that TXT record, and polling of the returned
change. This correction concerns that existing certificate workflow only.
The provider documents the three API permissions in its
[official documentation](https://certbot-dns-route53.readthedocs.io/en/stable/).
AWS documents record-type and normalized-name conditions in its
[Route53 IAM guide](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/specifying-conditions-route53.html).

## Requirements added to D07

D07 now contains `R-F3DQ-ZZV8` and `R-F4LN-DRLX` for the following
permissions. The existing S3 requirement remains unchanged.

- The policy produced from `PolicyTemplate` MUST grant `ssm:GetParameter` on
  `arn:aws:ssm:*:<account_id>:parameter/ikigenba/<domain>/*`, allowing the host
  to read each app's secrets object that create and secrets push write. It MUST
  grant no parameter reads outside that space prefix and no parameter writes.
- The policy produced from `PolicyTemplate` MUST grant `route53:ListHostedZones`
  on `*`, `route53:GetChange` on `arn:aws:route53:::change/*`, and
  `route53:ChangeResourceRecordSets` on
  `arn:aws:route53:::hostedzone/<zone_id>`, with
  `ForAllValues:StringEquals` conditions requiring
  `route53:ChangeResourceRecordSetsRecordTypes` to equal `TXT` and
  `route53:ChangeResourceRecordSetsNormalizedRecordNames` to equal
  `_acme-challenge.<domain>`. Tests MUST inspect the substituted JSON policy
  and verify that the space's challenge TXT record is allowed and the space's
  ordinary A records are not granted host mutation access by this policy.

D03's SecureString writer specifies no customer KMS key. Parameter Store uses
its AWS-managed key when no key is supplied; its default key permits account
principals to decrypt. No new customer-key configuration or KMS grant is
needed for this documented workflow. See
[AWS Parameter Store encryption](https://docs.aws.amazon.com/systems-manager/latest/userguide/secure-string-parameter-kms-encryption.html)
and [Parameter Store setup](https://docs.aws.amazon.com/systems-manager/latest/userguide/parameter-store-setting-up.html).

## Existing S3 and retention contract

D07 already requires reads and writes beneath `<domain>/`, including deployment
archive reads, plus listing restricted to that prefix. AWS assigns object
operations to object ARNs and listing to the bucket ARN with a prefix
condition; see [S3 prefix policies](https://docs.aws.amazon.com/AmazonS3/latest/userguide/amazon-s3-policy-keys.html).

The account's destroy-retention flags control devctl, running with the developer's
identity. They do not select a different host role: a retained space still has
to write backups, and a space whose backups are deleted on destroy still has
to read deployment archives. No additional host retention operation is
specified by these devctl stories, so this review adds no speculative storage
permissions or storage features.

## Evidence boundary

These changes complete the missing devctl policy contract using the documented
provider interface. They are not observations that a particular installed
opsctl, instance permissions boundary, or target account already satisfies it.
