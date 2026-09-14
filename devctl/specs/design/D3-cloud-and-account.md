# D3-cloud-and-account

Everything a command knows about AWS before it does anything specific lives in
three packages: `internal/cloud`, the cloud boundary; `internal/cloud/awssdk`,
the only package that may import the AWS SDK; and `internal/account`, the
account a profile names — its properties, its caller id, its zones, and the
spaces its tags name. Four commands require `--account` and every one of them
starts here. Nothing in this design is a command: no grammar, no usage text, no
step output. D5–D9 sit on top of it.

**One boundary, six narrow interfaces.** `internal/cloud` declares `Clients`,
a bundle of six interfaces — `EC2`, `SSM`, `Route53`, `S3`, `IAM`, `STS` — each
holding only the operations the stories need, in devctl's vocabulary rather
than the SDK's: a `[]Instance` with an id, a state and an address, not a
`*ec2.DescribeInstancesOutput` with reservations inside it. That is what keeps
the SDK in one package, and it is what lets every test above the boundary be a
handful of literals. A bundle is produced by an `Opener`, the one function
that turns a profile name and a region into that account's clients; an empty
region means the region the profile's shared configuration names.

**Nothing here waits.** No method polls, retries a condition, or blocks on a
waiter: each one calls one AWS API operation and returns what came back.
`space create` waiting for an instance to run, for its status checks to pass,
or for a Route 53 change to reach `INSYNC` is a loop above this boundary, built
from `Deps.After` — which is why a fake `Clients` needs no clock and a test
that exercises a wait finishes instantly.

**The shape of an AWS failure.** Three stories fix the text, so the fields are
read off it. A failure is a `*cloud.Error` carrying the service, the AWS
operation, the subject the call named when it named one, and the API error
code; `devctl: ` plus its message is the whole diagnostic, and the exit code is
1 — the command was well-formed and could not do what it was asked. `awssdk` is
where the code is read off the response, through `smithy.APIError`, which is
why the Smithy runtime is a direct dependency. Above the boundary a fake client
returns a `*cloud.Error` literal and the diagnostic is exercised with no AWS
anywhere.

**The region, and why `Clients` is opened twice.** The profile's shared
configuration yields one region, but the region every call must actually use is
the `region` property of the account's own properties — `space list`'s
postcondition says its instances are found "in the region the account's
properties name". So `account.Open` is the only place that orders the two:
it opens `Clients` with an empty region, reads `/ikigenba/account` with them,
and then opens `Clients` a second time in the region those properties name.
The `*Account` it returns carries only the second bundle, so every operation
after the properties load is in the named region by construction. A command
never calls `Deps.Cloud` itself — that is a requirement, not a convention, and
it is what makes the ordering impossible to get wrong.

**The account, and the two lookups every command starts with.** `account.Open`
takes a profile name and hands back an `*Account` carrying three things: the
profile `--account` named byte for byte, the properties decoded from the JSON
object at `/ikigenba/account`, and the clients, already opened in the region
those properties name.

`Properties` is the eleven keys `infra/602773793009/account.tf` and
`infra/295229566359/account.tf` write — `domain`, `backup_bucket`,
`launch_template_id`, `permissions_boundary_arn`, `region`,
`deploy_from_main_only`, `delete_secrets_on_destroy`,
`delete_backups_on_destroy`, `backup_full_seconds`,
`backup_incremental_seconds`, `backup_wal_seconds` — three strings the
infrastructure names, a region, three booleans, and three periods in seconds.
The struct is exported whole and the fields are read directly; a command that
needs the backup bucket reads `acct.Properties.BackupBucket`.

Two lookups then turn a domain into something a command can act on, and they
belong here rather than in each command because nearly every command starts
with them:

- **The space at a domain.** `acct.Space(ctx, domain)` is the one
  non-terminated instance tagged `Space=<domain>`; `acct.Spaces(ctx)` is all of
  them, sorted by domain. A `Space` is exactly the facts `space list` prints —
  the domain, the instance state, the public address — plus the instance id
  every other subcommand's output quotes. When there is no such instance the
  error is `*NoSpaceError`, whose message is `no space at '<domain>'`, the
  diagnostic `space stop`, `space start`, `space status`, `secrets`, `deploy`
  and `restore` all share.
- **The zone a domain belongs to.** `acct.Zone(ctx, domain)` is the account's
  hosted zone whose name is the longest suffix of the domain, which is the zone
  the space's records are written in. `acct.Delegation(ctx, zone, domain)` is
  the companion: the name of the child zone an `NS` record in that zone
  delegates the domain away to, or the empty string. Both facts are DNS facts
  about a domain and a zone, so both are here; what `space create` does with a
  non-empty delegation — the refusal text and its exit code — is D7's, and so
  is everything about records beyond reading and changing them.

**What is deliberately not here.** The secrets object's path
`/ikigenba/<domain>/<app>` and the backup prefix `<domain>/<app>/` are D5's and
D9's; the `space` inline policy, the role name, the `A` records and their TTL
are D7's; waiting, step output, and every exit code other than the two this
design fixes belong to the command designs. This package answers "what is in
this account", not "what should happen next".

## REQUIREMENTS

- R-Y7GF-A1BT: Package `internal/cloud` MUST export the type `Opener func(ctx context.Context, profile, region string) (Clients, error)` and a `Clients` struct whose fields are exactly `EC2 EC2`, `SSM SSM`, `Route53 Route53`, `S3 S3`, `IAM IAM`, and `STS STS`.
- R-Y8OB-NT2I: Package `internal/cloud` MUST export an `Error` struct whose fields are exactly `Service string`, `Operation string`, `Subject string`, `Code string`, and `Err error`, with the methods `Error() string` and `Unwrap() error`, where `Unwrap` returns `Err`.
- R-Y9W8-1KT7: `(*cloud.Error).Error` MUST return `<Service> <Operation> <Subject>: <Code>` when `Subject` is not empty and `<Service> <Operation>: <Code>` when it is, and MUST use the message of `Err` in place of `<Code>` when `Code` is empty, verified at least by reproducing `ssm GetParameter /ikigenba/account: ParameterNotFound`, `ec2 RunInstances: InsufficientInstanceCapacity`, and `route53 ChangeResourceRecordSets: Throttling`.
- R-YB44-FCJW: Package `internal/cloud` MUST export the type `InstanceState string` with exactly the constants `StatePending = "pending"`, `StateRunning = "running"`, `StateShuttingDown = "shutting-down"`, `StateTerminated = "terminated"`, `StateStopping = "stopping"`, and `StateStopped = "stopped"`; an `Instance` struct whose fields are exactly `ID string`, `Space string`, `State InstanceState`, and `Address string`; an `Address` struct whose fields are exactly `AllocationID string`, `AssociationID string`, `IP string`, and `Space string`; and a `LaunchSpec` struct whose fields are exactly `LaunchTemplateID string`, `InstanceProfile string`, and `Space string`.
- R-YCC0-T4AL: Package `internal/cloud` MUST export an `EC2` interface whose methods are exactly `ListSpaceInstances(ctx context.Context) ([]Instance, error)`, `DescribeInstance(ctx context.Context, id string) (Instance, error)`, `RunInstance(ctx context.Context, spec LaunchSpec) (Instance, error)`, `StartInstance(ctx context.Context, id string) error`, `StopInstance(ctx context.Context, id string) error`, `TerminateInstance(ctx context.Context, id string) error`, `InstanceChecksPassed(ctx context.Context, id string) (bool, error)`, `ListSpaceAddresses(ctx context.Context) ([]Address, error)`, `AllocateAddress(ctx context.Context, space string) (Address, error)`, `AssociateAddress(ctx context.Context, allocationID, instanceID string) error`, `DisassociateAddress(ctx context.Context, associationID string) error`, and `ReleaseAddress(ctx context.Context, allocationID string) error`.
- R-YDJX-6W1A: Package `internal/cloud` MUST export a `Parameter` struct whose fields are exactly `Name string` and `Value string`, and an `SSM` interface whose methods are exactly `GetParameter(ctx context.Context, name string) (string, error)`, `PutSecureParameter(ctx context.Context, name, value string) error`, `ListParameters(ctx context.Context, prefix string) ([]Parameter, error)`, and `DeleteParameter(ctx context.Context, name string) error`.
- R-YERT-KNRZ: Package `internal/cloud` MUST export a `Zone` struct whose fields are exactly `ID string` and `Name string`; a `Record` struct whose fields are exactly `Name string`, `Type string`, `TTL int64`, and `Values []string`; the type `ChangeAction string` with exactly the constants `ChangeUpsert = "UPSERT"` and `ChangeDelete = "DELETE"`; a `RecordChange` struct whose fields are exactly `Action ChangeAction` and `Record Record`; and the type `ChangeStatus string` with exactly the constants `ChangePending = "PENDING"` and `ChangeInsync = "INSYNC"`.
- R-YH7M-C79D: Package `internal/cloud` MUST export a `Route53` interface whose methods are exactly `ListZones(ctx context.Context) ([]Zone, error)`, `ListRecords(ctx context.Context, zoneID string) ([]Record, error)`, `ChangeRecords(ctx context.Context, zoneID string, changes []RecordChange) (string, error)`, and `ChangeStatus(ctx context.Context, changeID string) (ChangeStatus, error)`.
- R-YIFI-PZ02: Package `internal/cloud` MUST export an `Object` struct whose fields are exactly `Key string`, `Size int64`, and `Modified time.Time`, and an `S3` interface whose methods are exactly `ListObjects(ctx context.Context, bucket, prefix string) ([]Object, error)`, `GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)`, `PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error`, and `DeleteObjects(ctx context.Context, bucket string, keys []string) error`.
- R-YJNF-3QQR: Package `internal/cloud` MUST export a `RoleSpec` struct whose fields are exactly `Name string`, `AssumeRolePolicy string`, and `PermissionsBoundaryARN string`, and an `IAM` interface whose methods are exactly `RoleExists(ctx context.Context, name string) (bool, error)`, `CreateRole(ctx context.Context, spec RoleSpec) error`, `PutRolePolicy(ctx context.Context, role, policy, document string) error`, `DeleteRolePolicy(ctx context.Context, role, policy string) error`, `InstanceProfileRoles(ctx context.Context, name string) ([]string, bool, error)`, `CreateInstanceProfile(ctx context.Context, name string) error`, `AddRoleToInstanceProfile(ctx context.Context, profile, role string) error`, `RemoveRoleFromInstanceProfile(ctx context.Context, profile, role string) error`, `DeleteInstanceProfile(ctx context.Context, name string) error`, and `DeleteRole(ctx context.Context, name string) error`.
- R-YKVB-HIHG: Package `internal/cloud` MUST export an `STS` interface whose only method is `CallerAccountID(ctx context.Context) (string, error)`.
- R-YM37-VA85: Package `internal/cloud/awssdk` MUST export `Open(ctx context.Context, profile, region string) (cloud.Clients, error)` whose type is assignable to `cloud.Opener`, and every field of the `cloud.Clients` it returns with a nil error MUST be non-nil.
- R-YNB4-91YU: `awssdk.Open` MUST pass `profile` to the AWS SDK's shared-config loader as the shared-config profile to load, with no trimming, case change, or other normalisation, MUST make every client it returns call the region named by `region` when `region` is not empty and the region that profile's shared configuration names when it is empty, and MUST return a non-nil error and no clients when that configuration cannot be loaded.
- R-YOJ0-MTPJ: Every non-nil error returned by a method of any client `awssdk.Open` returns MUST be a `*cloud.Error` whose `Service` and `Operation` are the service and AWS API operation that method's mapping names, whose `Subject` is the subject that mapping names or empty when it names none, whose `Err` wraps the error the SDK returned, and whose `Code` is the value of `ErrorCode()` when the SDK error satisfies `smithy.APIError` and empty when it does not.
- R-YPQX-0LG8: Each method of each client `awssdk.Open` returns MUST invoke no AWS API operation other than the one its mapping names.
- R-YQYT-ED6X: `ListSpaceInstances`, `ListParameters`, `ListObjects`, `ListZones`, and `ListRecords`, as `awssdk` implements them, MUST follow the AWS API's pagination to the last page and return the items of every page, verified against a fake endpoint that answers with more than one page.
- R-YS6P-S4XM: `DeleteParameter`, `DeleteObjects`, `DisassociateAddress`, `ReleaseAddress`, `DeleteRolePolicy`, `RemoveRoleFromInstanceProfile`, `DeleteInstanceProfile`, and `DeleteRole`, as `awssdk` implements them, MUST return a nil error when the thing they name does not exist.
- R-YTEM-5WOB: In `awssdk` the `EC2` methods MUST map to the service `ec2` with no subject and the operations `ListSpaceInstances`→`DescribeInstances`, `DescribeInstance`→`DescribeInstances`, `RunInstance`→`RunInstances`, `StartInstance`→`StartInstances`, `StopInstance`→`StopInstances`, `TerminateInstance`→`TerminateInstances`, `InstanceChecksPassed`→`DescribeInstanceStatus`, `ListSpaceAddresses`→`DescribeAddresses`, `AllocateAddress`→`AllocateAddress`, `AssociateAddress`→`AssociateAddress`, `DisassociateAddress`→`DisassociateAddress`, and `ReleaseAddress`→`ReleaseAddress`; `RunInstance` MUST launch one instance from `spec.LaunchTemplateID` with the instance profile named by `spec.InstanceProfile` and MUST return the instance as `RunInstances` reports it, without waiting for it to run; `InstanceChecksPassed` MUST return true only when both the instance status and the system status the response carries are `ok`; and `Instance.Address` and `Address.AssociationID` MUST be empty when the response carries none.
- R-YUMI-JOF0: In `awssdk`, `ListSpaceInstances` MUST request only instances matching both the filter `tag:Project=ikigenba` and the filter `tag-key=Space` and MUST set each returned `Instance.Space` to the value of that instance's `Space` tag; `ListSpaceAddresses` MUST request only addresses matching both of those filters and MUST set each returned `Address.Space` likewise; and `RunInstance` and `AllocateAddress` MUST apply the tags `Project=ikigenba` and `Space=<space>` — the instance's and the instance's volumes' on `RunInstance`, the address's on `AllocateAddress` — in the same request that creates the resource.
- R-YVUE-XG5P: In `awssdk` the `SSM` methods MUST map to the service `ssm` with the parameter name as the subject and the operations `GetParameter`→`GetParameter`, `PutSecureParameter`→`PutParameter`, `ListParameters`→`GetParametersByPath`, and `DeleteParameter`→`DeleteParameter`; `GetParameter` and `ListParameters` MUST request decryption; `ListParameters` MUST recurse below `prefix` and MUST carry `prefix` as its subject; and `PutSecureParameter` MUST write type `SecureString` and MUST overwrite an existing value.
- R-YX2B-B7WE: In `awssdk` the `Route53` methods MUST map to the service `route53` with no subject and the operations `ListZones`→`ListHostedZones`, `ListRecords`→`ListResourceRecordSets`, `ChangeRecords`→`ChangeResourceRecordSets`, and `ChangeStatus`→`GetChange`; `ChangeRecords` MUST submit every element of `changes` in one change batch, each as the action its `Action` names with the name, type, TTL and values of its `Record`, and MUST return the change id the response carries; and `ChangeStatus` MUST accept that change id unchanged and return the status the response carries.
- R-YYA7-OZN3: In `awssdk`, `ListZones` MUST return each `Zone.ID` with any `/hostedzone/` prefix removed and each `Zone.Name` with its trailing dot removed, `ListRecords` MUST return each `Record.Name` as the API reports it except for its trailing dot, leaving any `\052` or other escape undecoded, and `ChangeRecords` MUST send each `Record.Name` as it was given with no escaping or unescaping of its own, so that a `Record` read by `ListRecords` and passed back in a `ChangeDelete` names the same record set.
- R-Z0Q0-GJ4H: In `awssdk` the `S3` methods MUST map to the service `s3` with `<bucket>/<key>` as the subject — `<bucket>/<prefix>` for `ListObjects` and `<bucket>` for `DeleteObjects` — and the operations `ListObjects`→`ListObjectsV2`, `GetObject`→`GetObject`, `PutObject`→`PutObject`, and `DeleteObjects`→`DeleteObjects`; `ListObjects` MUST return every object whose key begins with `prefix`, with its key, size and last-modified time; `PutObject` MUST send `size` as the object's length; and `DeleteObjects` MUST delete every key in `keys`, in requests of at most 1000 keys each.
- R-Z1XW-UAV6: In `awssdk` the `IAM` methods MUST map to the service `iam` with no subject and the operations `RoleExists`→`GetRole`, `CreateRole`→`CreateRole`, `PutRolePolicy`→`PutRolePolicy`, `DeleteRolePolicy`→`DeleteRolePolicy`, `InstanceProfileRoles`→`GetInstanceProfile`, `CreateInstanceProfile`→`CreateInstanceProfile`, `AddRoleToInstanceProfile`→`AddRoleToInstanceProfile`, `RemoveRoleFromInstanceProfile`→`RemoveRoleFromInstanceProfile`, `DeleteInstanceProfile`→`DeleteInstanceProfile`, and `DeleteRole`→`DeleteRole`; `CreateRole` MUST create the role named `spec.Name` with `spec.AssumeRolePolicy` as its trust policy and `spec.PermissionsBoundaryARN` as its permissions boundary; `RoleExists` and `InstanceProfileRoles` MUST report absence as a false second result with a nil error; and `InstanceProfileRoles` MUST return the names of the roles the instance profile holds.
- R-Z35T-82LV: In `awssdk`, `CallerAccountID` MUST map to the service `sts`, the operation `GetCallerIdentity`, and no subject, and MUST return the account id the response carries.
- R-Z4DP-LUCK: Package `internal/account` MUST export a `Properties` struct whose fields are exactly `Domain string`, `BackupBucket string`, `LaunchTemplateID string`, `PermissionsBoundaryARN string`, `Region string`, `DeployFromMainOnly bool`, `DeleteSecretsOnDestroy bool`, `DeleteBackupsOnDestroy bool`, `BackupFullSeconds int`, `BackupIncrementalSeconds int`, and `BackupWALSeconds int`, decoded from the JSON keys `domain`, `backup_bucket`, `launch_template_id`, `permissions_boundary_arn`, `region`, `deploy_from_main_only`, `delete_secrets_on_destroy`, `delete_backups_on_destroy`, `backup_full_seconds`, `backup_incremental_seconds`, and `backup_wal_seconds` respectively.
- R-Z5LL-ZM39: Package `internal/account` MUST export `PropertiesParameter = "/ikigenba/account"`.
- R-Z6TI-DDTY: Package `internal/account` MUST export an `Account` struct whose fields are exactly `Profile string`, `Properties Properties`, and `Clients cloud.Clients`; `Open(ctx context.Context, deps seam.Deps, profile string) (*Account, error)`; and the methods `CallerAccountID(ctx context.Context) (string, error)`, `Zone(ctx context.Context, domain string) (cloud.Zone, error)`, `Delegation(ctx context.Context, zone cloud.Zone, domain string) (string, error)`, `Spaces(ctx context.Context) ([]Space, error)`, and `Space(ctx context.Context, domain string) (Space, error)` on `*Account`.
- R-Z81E-R5KN: Package `internal/account` MUST export a `Space` struct whose fields are exactly `Domain string`, `ID string`, `State cloud.InstanceState`, and `Address string`.
- R-Z99B-4XBC: Package `internal/account` MUST export a `NoSpaceError` struct whose only field is `Domain string` and whose `Error()` returns `no space at '<Domain>'`, and a `NoZoneError` struct whose only field is `Domain string` and whose `Error()` returns `no hosted zone for '<Domain>'`.
- R-ZAH7-IP21: `account.Open` MUST call `deps.Cloud` with `profile` unchanged and an empty region, MUST read `PropertiesParameter` with the `SSM` of the clients that call returned, and on success MUST then call `deps.Cloud` exactly once more with `profile` unchanged and `Properties.Region` as the region and MUST return an `*Account` whose `Clients` are the clients of that second call and whose `Profile` is `profile`; when the read or the decode fails it MUST return a nil `*Account` and MUST NOT call `deps.Cloud` a second time; verified with a fake `cloud.Opener` that records the profile and region of every call.
- R-ZBP3-WGSQ: `account.Open` MUST return a nil `*Account` and an error whose message begins `/ikigenba/account: ` when the parameter's value is not a JSON object, or when any of the eleven keys `Properties` names is absent from it or holds a value of another JSON type, and MUST ignore any other key the object carries.
- R-ZCX0-A8JF: No package of this module other than `internal/account` and `cmd/devctl` MUST reference the `Cloud` field of `seam.Deps` in a non-test file, verified by a test over the packages' syntax trees.
- R-ZE4W-O0A4: `(*Account).Spaces` MUST return one `Space` for each distinct `Space` tag value among the instances `Clients.EC2.ListSpaceInstances` reports whose `State` is not `cloud.StateTerminated`, sorted by `Domain`, each carrying that tag value as `Domain` and the instance's id, state and address, and MUST return an empty result and a nil error when there are none.
- R-ZFCT-1S0T: `(*Account).Space` MUST return the `Space` whose `Domain` equals `domain` among those `Spaces` reports, MUST return a `*NoSpaceError` for `domain` when there is none, and MUST return a non-nil error naming `domain` when two or more non-terminated instances carry that `Space` tag value.
- R-ZGKP-FJRI: `(*Account).Zone` MUST return the zone of `Clients.Route53.ListZones` with the longest `Name` such that `domain` equals that name or ends in a dot followed by it, and MUST return a `*NoZoneError` for `domain` when no zone matches.
- R-ZJ0I-738W: `(*Account).Delegation` MUST return the longest name among the `NS` records of `zone` that is not `zone.Name` and for which `domain` equals that name or ends in a dot followed by it, and MUST return the empty string and a nil error when there is no such record.
- R-ZK8E-KUZL: `(*Account).CallerAccountID` MUST return what `Clients.STS.CallerAccountID` returns.
- R-ZLGA-YMQA: When a command fails because a method of `Account.Clients` returned an error that `errors.As` matches to a `*cloud.Error`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line on stderr, write nothing further to stdout, and return 1, verified at least with fake clients reproducing `devctl: ssm GetParameter /ikigenba/account: ParameterNotFound`, `devctl: ec2 RunInstances: InsufficientInstanceCapacity`, and `devctl: route53 ChangeResourceRecordSets: Throttling`.
- R-ZMO7-CEGZ: When a command fails because `(*Account).Space` returned a `*NoSpaceError`, `cli.Run` MUST write the single line `devctl: no space at '<domain>'` to stderr, write nothing further to stdout, and return 1, verified at least for `space stop`, `space start`, `space status`, `secrets push`, `deploy`, and `restore`.

