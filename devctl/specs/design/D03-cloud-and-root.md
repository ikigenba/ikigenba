# D03-cloud-and-root

Cloud interfaces use devctl vocabulary; the AWS adapter remains the sole SDK
consumer. The platform is one root domain in one account: the root and its
region come from the checkout's root file (D04), the root is also the name of
the AWS shared-config profile, and both reach this package as opaque strings.
Nothing is configured about the account itself. `Connect` opens the clients
and immediately asks STS for the account id, so the first call any cloud
command makes is `GetCallerIdentity`, and an expired or missing SSO session
fails there with the SDK's own words relayed after `sts GetCallerIdentity: `.
A code the SDK carries up from some other operation it made along the way — the
SSO token refresh, say — belongs to that operation, not to this one, and is not
reported as this one's code.

Everything Terraform made is named after the root and found by that name:
the hosted zone, the launch template, and the permissions boundary policy are
looked up by name, and each lookup answers absence with a `NotFoundError`
whose message is the story's (`no hosted zone 'ikigenba.dev'`). The bucket is
used by name and never looked up; the key pair is the launch template's
business. Per-space resources are tagged `Domain=<root>` and
`Space=<space domain>`; `Spaces` and `LookupSpace` turn those tags into the
space registry every command reads, and a space that is not there is a
`NoSpaceError`. The S3 client addresses the bucket path-style because the
bucket's name has dots.

Route 53 gains a point read, `FindRecord`, which is what `apex` uses to read
the root's `A` record and what `space create` uses to see whether a name is
delegated away by an `NS` record; upserts, deletes, and the `INSYNC` wait use
the same `ChangeRecords` and `ChangeStatus` the space's own two records use.
`IAM.PutRolePolicy` replaces a role's inline policy whole, which is how
`create` writes it and how `apex set` and `apex clear` add and remove the
`_acme-challenge.<root>` permission. `LaunchReady` is a side-effect-free
probe: it asks EC2 whether a launch spec would be accepted, which is how a
caller confirms a just-created instance profile has become usable before it
commits to a real launch.

## REQUIREMENTS

- R-Y7GF-A1BT: Package `internal/cloud` MUST export the type `Opener func(ctx context.Context, profile, region string) (Clients, error)` and a `Clients` struct whose fields are exactly `EC2 EC2`, `SSM SSM`, `Route53 Route53`, `S3 S3`, `IAM IAM`, and `STS STS`.

- R-8YCX-BKJM: Package `internal/cloud` MUST export a `Session` struct whose fields are exactly `AccountID string` and `Clients Clients`, and `Connect(ctx context.Context, open Opener, profile, region string) (Session, error)`.

- R-Q5B4-FD08: `cloud.Connect` MUST call `open` exactly once with `profile` and `region` unchanged, MUST then call `CallerAccountID` on the `STS` of the clients that call returned before, and instead of, any other method of any of those clients, and MUST return a `Session` whose `Clients` are those clients and whose `AccountID` is what `CallerAccountID` returned; when `open` or `CallerAccountID` returns an error it MUST return the zero `Session` and that error unchanged; verified with a fake `Opener` that records its arguments and fake clients that record every call.

- R-Y8OB-NT2I: Package `internal/cloud` MUST export an `Error` struct whose fields are exactly `Service string`, `Operation string`, `Subject string`, `Code string`, and `Err error`, with the methods `Error() string` and `Unwrap() error`, where `Unwrap` returns `Err`.

- R-Q6J0-T4QX: `(*cloud.Error).Error` MUST return `<Service> <Operation> <Subject>: <Code>` when `Subject` is not empty and `<Service> <Operation>: <Code>` when it is, and MUST use the message of `Err` in place of `<Code>` when `Code` is empty, verified at least by reproducing `ssm GetParameter /sbx1.ikigenba.dev/crm: ParameterNotFound`, `ec2 RunInstances: InsufficientInstanceCapacity`, `route53 ChangeResourceRecordSets: Throttling`, and, for an empty `Code` and an `Err` whose message is an arbitrary string `<m>`, `sts GetCallerIdentity: <m>`.

- R-Q7QX-6WHM: Package `internal/cloud` MUST export a `NotFoundError` struct whose fields are exactly `Kind string` and `Name string` and whose `Error()` returns `no <Kind> '<Name>'`, verified at least by reproducing `no hosted zone 'ikigenba.dev'`, `no launch template 'ikigenba.dev'`, and `no permissions boundary 'ikigenba.dev'`.

- R-Q8YT-KO8B: Package `internal/cloud` MUST export the type `InstanceState string` with exactly the constants `StatePending = "pending"`, `StateRunning = "running"`, `StateShuttingDown = "shutting-down"`, `StateTerminated = "terminated"`, `StateStopping = "stopping"`, and `StateStopped = "stopped"`; an `Instance` struct whose fields are exactly `ID string`, `Space string`, `State InstanceState`, and `Address string`; an `Address` struct whose fields are exactly `AllocationID string`, `AssociationID string`, `IP string`, and `Space string`; and a `LaunchSpec` struct whose fields are exactly `LaunchTemplateID string`, `InstanceProfile string`, `Domain string`, and `Space string`.

- R-QA6P-YFZ0: Package `internal/cloud` MUST export an `EC2` interface whose methods are exactly `LaunchTemplate(ctx context.Context, name string) (string, error)`, `ListSpaceInstances(ctx context.Context, domain string) ([]Instance, error)`, `DescribeInstance(ctx context.Context, id string) (Instance, error)`, `RunInstance(ctx context.Context, spec LaunchSpec) (Instance, error)`, `LaunchReady(ctx context.Context, spec LaunchSpec) (bool, error)`, `StartInstance(ctx context.Context, id string) error`, `StopInstance(ctx context.Context, id string) error`, `TerminateInstance(ctx context.Context, id string) error`, `InstanceChecksPassed(ctx context.Context, id string) (bool, error)`, `ListSpaceAddresses(ctx context.Context, domain string) ([]Address, error)`, `AllocateAddress(ctx context.Context, domain, space string) (Address, error)`, `AssociateAddress(ctx context.Context, allocationID, instanceID string) error`, `DisassociateAddress(ctx context.Context, associationID string) error`, and `ReleaseAddress(ctx context.Context, allocationID string) error`.

- R-YDJX-6W1A: Package `internal/cloud` MUST export a `Parameter` struct whose fields are exactly `Name string` and `Value string`, and an `SSM` interface whose methods are exactly `GetParameter(ctx context.Context, name string) (string, error)`, `PutSecureParameter(ctx context.Context, name, value string) error`, `ListParameters(ctx context.Context, prefix string) ([]Parameter, error)`, and `DeleteParameter(ctx context.Context, name string) error`.

- R-YERT-KNRZ: Package `internal/cloud` MUST export a `Zone` struct whose fields are exactly `ID string` and `Name string`; a `Record` struct whose fields are exactly `Name string`, `Type string`, `TTL int64`, and `Values []string`; the type `ChangeAction string` with exactly the constants `ChangeUpsert = "UPSERT"` and `ChangeDelete = "DELETE"`; a `RecordChange` struct whose fields are exactly `Action ChangeAction` and `Record Record`; and the type `ChangeStatus string` with exactly the constants `ChangePending = "PENDING"` and `ChangeInsync = "INSYNC"`.

- R-QBEM-C7PP: Package `internal/cloud` MUST export a `Route53` interface whose methods are exactly `Zone(ctx context.Context, name string) (Zone, error)`, `ListRecords(ctx context.Context, zoneID string) ([]Record, error)`, `FindRecord(ctx context.Context, zoneID, name, recordType string) (Record, bool, error)`, `ChangeRecords(ctx context.Context, zoneID string, changes []RecordChange) (string, error)`, and `ChangeStatus(ctx context.Context, changeID string) (ChangeStatus, error)`.

- R-CCED-PY62: Package `internal/cloud` MUST export an `Object` struct whose fields are exactly `Key string`, `Size int64`, and `Modified time.Time`, and an `S3` interface whose methods are exactly `ListObjects(ctx context.Context, bucket, prefix string) ([]Object, error)`, `PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error`, and `DeleteObjects(ctx context.Context, bucket string, keys []string) error`.

- R-QCMI-PZGE: Package `internal/cloud` MUST export a `RoleSpec` struct whose fields are exactly `Name string`, `AssumeRolePolicy string`, and `PermissionsBoundaryARN string`, and an `IAM` interface whose methods are exactly `PermissionsBoundary(ctx context.Context, name string) (string, error)`, `RoleExists(ctx context.Context, name string) (bool, error)`, `CreateRole(ctx context.Context, spec RoleSpec) error`, `PutRolePolicy(ctx context.Context, role, policy, document string) error`, `DeleteRolePolicy(ctx context.Context, role, policy string) error`, `InstanceProfileRoles(ctx context.Context, name string) ([]string, bool, error)`, `CreateInstanceProfile(ctx context.Context, name string) error`, `AddRoleToInstanceProfile(ctx context.Context, profile, role string) error`, `RemoveRoleFromInstanceProfile(ctx context.Context, profile, role string) error`, `DeleteInstanceProfile(ctx context.Context, name string) error`, and `DeleteRole(ctx context.Context, name string) error`.

- R-YKVB-HIHG: Package `internal/cloud` MUST export an `STS` interface whose only method is `CallerAccountID(ctx context.Context) (string, error)`.

- R-QDUF-3R73: Package `internal/cloud` MUST export a `Space` struct whose fields are exactly `Domain string`, `ID string`, `State InstanceState`, and `Address string`; `Spaces(ctx context.Context, ec2 EC2, domain string) ([]Space, error)`; and `LookupSpace(ctx context.Context, ec2 EC2, domain, space string) (Space, error)`.

- R-QF2B-HIXS: Package `internal/cloud` MUST export a `NoSpaceError` struct whose only field is `Domain string` and whose `Error()` returns `no space at '<Domain>'`.

- R-2ZKD-LLTT: `cloud.Spaces` MUST return one `Space` for each instance `ec2.ListSpaceInstances(ctx, domain)` reports whose `State` is not `StateTerminated`, carrying the instance's `Space` as `Domain` and its `ID`, `State`, and `Address`, sorted by `Domain`; MUST return a non-nil error whose message names that duplicated `Space` value rather than choosing when two or more such instances carry the same `Space`; MUST return an empty result and a nil error when there are none; and MUST NOT count terminated instances towards a duplicate.

- R-QHI4-92F6: `cloud.LookupSpace` MUST return the `Space` whose `Domain` equals `space` among those `Spaces(ctx, ec2, domain)` returns, MUST return a `*NoSpaceError` whose `Domain` is `space` when there is none, and MUST return the error `Spaces` returns when it returns one.

- R-YM37-VA85: Package `internal/cloud/awssdk` MUST export `Open(ctx context.Context, profile, region string) (cloud.Clients, error)` whose type is assignable to `cloud.Opener`, and every field of the `cloud.Clients` it returns with a nil error MUST be non-nil.

- R-YNB4-91YU: `awssdk.Open` MUST pass `profile` to the AWS SDK's shared-config loader as the shared-config profile to load, with no trimming, case change, or other normalisation, MUST make every client it returns call the region named by `region` when `region` is not empty and the region that profile's shared configuration names when it is empty, and MUST return a non-nil error and no clients when that configuration cannot be loaded.

- R-CG22-V9E5: Package `internal/cloud/awssdk` MUST export `ConfigLoader func(ctx context.Context, profile, region string) (aws.Config, error)` and `OpenWithLoader(ctx context.Context, profile, region string, load ConfigLoader) (cloud.Clients, error)`; OpenWithLoader MUST use only the supplied loader for configuration and credentials, passing profile and region unchanged.

- R-CH9Z-914U: `awssdk.Open` MUST delegate through OpenWithLoader with the production shared-config loader; adapter tests MUST exercise OpenWithLoader using a supplied configuration, fake credentials and in-memory HTTP transport, with no default credential chain, home directory, process environment or network access.

- R-QIQ0-MU5V: The `S3` client `awssdk.OpenWithLoader` returns MUST address every request path-style: for a bucket `<bucket>` the request URL's path is `/<bucket>` or begins `/<bucket>/` and its host does not begin `<bucket>.`; verified through the in-memory transport for a bucket name containing a dot, for at least `ListObjects`, `PutObject`, and `DeleteObjects`.

- R-VV8S-ZVE7: Every non-nil error returned by a method of any client `awssdk.Open` returns, other than the `*cloud.NotFoundError` that `LaunchTemplate`, `Zone`, and `PermissionsBoundary` return for absence, MUST be a `*cloud.Error` whose `Service` and `Operation` are the service and AWS API operation that method's mapping names, whose `Subject` is the subject that mapping names or empty when it names none, whose `Code` is the value of `ErrorCode()` of the first `smithy.APIError` found by unwrapping the SDK error with the search stopping at any `*smithy.OperationError` other than the SDK error itself, and empty when the search stops or ends without finding one, so that a code belonging to a nested operation the `cloud.Error` does not name is never reported as that operation's code, and whose `Err` is the error the SDK returned with its outermost `*smithy.OperationError`, when the SDK error is one, replaced by the error that operation error wraps, so that the message of `Err` does not repeat the service and operation the `cloud.Error` already names; verified at least with constructed chains, needing no network: one whose outermost `*smithy.OperationError` is its only operation error and wraps an error satisfying `smithy.APIError`, for which `Code` is that error's `ErrorCode()` and the message of `Err` is that of that error; and one whose outermost `*smithy.OperationError` wraps, through further wrapping errors, a second `*smithy.OperationError` whose own wrapped error satisfies `smithy.APIError`, for which `Code` is empty and the message of `Err` is that of the error the outermost operation error wraps.

- R-YPQX-0LG8: Each method of each client `awssdk.Open` returns MUST invoke no AWS API operation other than the one its mapping names.

- R-QL5T-EDN9: `ListSpaceInstances`, `ListParameters`, `ListObjects`, `ListRecords`, and `PermissionsBoundary`, as `awssdk` implements them, MUST follow the AWS API's pagination to the last page and consider the items of every page, verified against a fake endpoint that answers with more than one page.

- R-YS6P-S4XM: `DeleteParameter`, `DeleteObjects`, `DisassociateAddress`, `ReleaseAddress`, `DeleteRolePolicy`, `RemoveRoleFromInstanceProfile`, `DeleteInstanceProfile`, and `DeleteRole`, as `awssdk` implements them, MUST return a nil error when the thing they name does not exist.

- R-QMDP-S5DY: In `awssdk` the `EC2` methods MUST map to the service `ec2` with no subject and the operations `LaunchTemplate`→`DescribeLaunchTemplates`, `ListSpaceInstances`→`DescribeInstances`, `DescribeInstance`→`DescribeInstances`, `RunInstance`→`RunInstances`, `StartInstance`→`StartInstances`, `StopInstance`→`StopInstances`, `TerminateInstance`→`TerminateInstances`, `InstanceChecksPassed`→`DescribeInstanceStatus`, `ListSpaceAddresses`→`DescribeAddresses`, `AllocateAddress`→`AllocateAddress`, `AssociateAddress`→`AssociateAddress`, `DisassociateAddress`→`DisassociateAddress`, and `ReleaseAddress`→`ReleaseAddress`; `RunInstance` MUST launch one instance from `spec.LaunchTemplateID` with the instance profile named by `spec.InstanceProfile` and MUST return the instance as `RunInstances` reports it, without waiting for it to run; `InstanceChecksPassed` MUST return true only when both the instance status and the system status the response carries are `ok`; and `Instance.Address` and `Address.AssociationID` MUST be empty when the response carries none.

- R-QOTI-JOVC: In `awssdk`, `LaunchTemplate` MUST request only launch templates matching the filter `launch-template-name=<name>`, MUST return the id of the launch template the response carries, and MUST return a `*cloud.NotFoundError` whose `Kind` is `launch template` and whose `Name` is `name` when the response carries none.

- R-H32Z-PBQW: In `awssdk`, `LaunchReady` MUST call the service `ec2` operation `RunInstances` with the launch template `spec.LaunchTemplateID` and the instance profile named by `spec.InstanceProfile`, exactly as `RunInstance` does but with its dry-run flag set so that no instance is created; it MUST return `true` and a nil error when that call reports the request would have succeeded (error code `DryRunOperation`), MUST return `false` and a nil error when that call reports the instance profile is not yet usable (error code `InvalidParameterValue` whose message contains `Invalid IAM Instance Profile name`), and MUST return a `*cloud.Error` for operation `RunInstances` for any other error.

- R-QQ1E-XGM1: In `awssdk`, `ListSpaceInstances` MUST request only instances matching both the filter `tag:Domain=<domain>` and the filter `tag-key=Space` and MUST set each returned `Instance.Space` to the value of that instance's `Space` tag; `ListSpaceAddresses` MUST request only addresses matching both of those filters and MUST set each returned `Address.Space` likewise; and `RunInstance` and `AllocateAddress` MUST apply the tags `Domain=<domain>` and `Space=<space>` — from `spec.Domain` and `spec.Space` to the instance and the instance's volumes on `RunInstance`, and from `domain` and `space` to the address on `AllocateAddress` — in the same request that creates the resource.

- R-YVUE-XG5P: In `awssdk` the `SSM` methods MUST map to the service `ssm` with the parameter name as the subject and the operations `GetParameter`→`GetParameter`, `PutSecureParameter`→`PutParameter`, `ListParameters`→`GetParametersByPath`, and `DeleteParameter`→`DeleteParameter`; `GetParameter` and `ListParameters` MUST request decryption; `ListParameters` MUST recurse below `prefix` and MUST carry `prefix` as its subject; and `PutSecureParameter` MUST write type `SecureString` and MUST overwrite an existing value.

- R-QR9B-B8CQ: In `awssdk` the `Route53` methods MUST map to the service `route53` with no subject and the operations `Zone`→`ListHostedZonesByName`, `ListRecords`→`ListResourceRecordSets`, `FindRecord`→`ListResourceRecordSets`, `ChangeRecords`→`ChangeResourceRecordSets`, and `ChangeStatus`→`GetChange`; `ChangeRecords` MUST submit every element of `changes` in one change batch, each as the action its `Action` names with the name, type, TTL and values of its `Record`, and MUST return the change id the response carries; and `ChangeStatus` MUST accept that change id unchanged and return the status the response carries.

- R-QSH7-P03F: In `awssdk`, `Zone` MUST request the listing to start at the DNS name `name`, MUST return the first hosted zone the response lists whose name, trailing dot removed, equals `name`, and MUST return a `*cloud.NotFoundError` whose `Kind` is `hosted zone` and whose `Name` is `name` when the response lists no such zone.

- R-QTP4-2RU4: In `awssdk`, `FindRecord` MUST request the listing to start at the record name `name` and the record type `recordType` with at most one item, MUST return that record set and a true second result when the response's first record set has type `recordType` and a name that, trailing dot removed and with a leading `\052` counted equal to a leading `*` in `name`, equals `name`, and MUST return the zero `Record`, false, and a nil error otherwise.

- R-QUX0-GJKT: In `awssdk`, `Zone` MUST return `Zone.ID` with any `/hostedzone/` prefix removed and `Zone.Name` with its trailing dot removed, `ListRecords` and `FindRecord` MUST return each `Record.Name` as the API reports it except for its trailing dot, leaving any `\052` or other escape undecoded, and `ChangeRecords` MUST send each `Record.Name` as it was given with no escaping or unescaping of its own, so that a `Record` read by `ListRecords` or `FindRecord` and passed back in a `ChangeDelete` names the same record set.

- R-QW4W-UBBI: In `awssdk` the `IAM` methods MUST map to the service `iam` with no subject and the operations `PermissionsBoundary`→`ListPolicies`, `RoleExists`→`GetRole`, `CreateRole`→`CreateRole`, `PutRolePolicy`→`PutRolePolicy`, `DeleteRolePolicy`→`DeleteRolePolicy`, `InstanceProfileRoles`→`GetInstanceProfile`, `CreateInstanceProfile`→`CreateInstanceProfile`, `AddRoleToInstanceProfile`→`AddRoleToInstanceProfile`, `RemoveRoleFromInstanceProfile`→`RemoveRoleFromInstanceProfile`, `DeleteInstanceProfile`→`DeleteInstanceProfile`, and `DeleteRole`→`DeleteRole`; `CreateRole` MUST create the role named `spec.Name` with `spec.AssumeRolePolicy` as its trust policy and `spec.PermissionsBoundaryARN` as its permissions boundary; `PutRolePolicy` MUST set the inline policy named `policy` of the role named `role` to exactly `document`, replacing any document it held; `RoleExists` MUST report absence as a false first result with a nil error, and `InstanceProfileRoles` MUST report absence as a false second result with a nil error; and `InstanceProfileRoles` MUST return the names of the roles the instance profile holds.

- R-QXCT-8327: In `awssdk`, `PermissionsBoundary` MUST list only the account's customer managed policies (scope `Local`), MUST return the ARN of the listed policy whose name equals `name`, and MUST return a `*cloud.NotFoundError` whose `Kind` is `permissions boundary` and whose `Name` is `name` when no listed policy has that name.

- R-Z35T-82LV: In `awssdk`, `CallerAccountID` MUST map to the service `sts`, the operation `GetCallerIdentity`, and no subject, and MUST return the account id the response carries.

- R-CEU6-HHNG: In `awssdk` the `S3` methods MUST map to the service `s3` with `<bucket>/<key>` as the subject — `<bucket>/<prefix>` for `ListObjects` and `<bucket>` for `DeleteObjects` — and the operations `ListObjects`→`ListObjectsV2`, `PutObject`→`PutObject`, and `DeleteObjects`→`DeleteObjects`; `ListObjects` MUST return every object whose key begins with `prefix`, with its key, size and last-modified time; `PutObject` MUST send `size` as the object's length; and `DeleteObjects` MUST delete every key in `keys`, in requests of at most 1000 keys each.

- R-30S9-ZDKI: In every non-test file of this module outside `cmd/devctl`, a reference to the `Cloud` field of `seam.Deps` MUST appear only as the `open` argument of a call to `cloud.Connect`, and MUST NOT initialise that field in any such file, verified by a test over the packages' syntax trees.

- R-QZSL-ZMJL: When a command fails because `cloud.Connect` or a method of a client of the `cloud.Clients` it opened returned an error that `errors.As` matches to a `*cloud.Error` or a `*cloud.NotFoundError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line on stderr, write nothing further to stdout, and return 1, verified at least with fake clients reproducing `devctl: sts GetCallerIdentity: <m>` for an `STS` whose `CallerAccountID` fails with an empty `Code` and an `Err` whose message is `<m>`, `devctl: ec2 RunInstances: InsufficientInstanceCapacity`, `devctl: route53 ChangeResourceRecordSets: Throttling`, and `devctl: no launch template 'ikigenba.dev'`.

- R-R10I-DEAA: When a command fails because `cloud.LookupSpace` returned a `*cloud.NoSpaceError`, `cli.Run` MUST write the single line `devctl: no space at '<domain>'` to stderr, write nothing further to stdout, and return 1, verified at least for `space stop`, `space start`, `space status`, `space init`, `space restart`, `space logs`, `secrets push`, `deploy`, `remove`, `restore`, and `apex set`.
