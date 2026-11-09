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
`NoSpaceError`.

A label carries at most one instance that is not terminated and at most one
Elastic IP, and a space exists while either carries it. `Spaces` and
`LookupSpace` read both listings, the instances and the addresses tagged
under the root, and answer one `Space` per label: the instance's id and
state when there is one, the Elastic IP as EC2 reported it when there is
one, with the id of the instance its association names, whichever instance
that is, and the space's address, which is the Elastic IP's when the label has
one, since that address is the space's for its whole life, and otherwise the
instance's own public address. A space with only its address, as a replace
that could not launch leaves it, has no instance id and no state; a space
with only an instance, as a create that failed before its address step
leaves it, has the zero Elastic IP. Only a label that neither carries is a
`NoSpaceError`; a terminated instance carries nothing. Two instances on one
label, made by hand or by something other than devctl, leave no telling
which one holds the space: `LookupSpace` refuses with a `DuplicateSpaceError`,
exit 2, naming both, and only for that label; `Spaces` refuses the whole
list the same way. Two Elastic IPs on one label leave no telling which one is
the space's, and both lookups refuse them alike with a
`DuplicateAddressError`, exit 2, naming both; a label carrying two of each is
refused for its instances, the older refusal, since the developer must clear
those first either way. Each holds only what the two listings return, which
EC2 bounds by the instances and addresses the account holds under the root.

Every command that needs the space's host — `space stop`, `start`, `status`,
`restart`, `disable`, `enable` and `logs`, `deploy`, `restore`, `rollback`,
`golden capture`, `seed`, `apex set`, and `secrets push` — looks the space up
with `LookupInstance` in place of `LookupSpace`. It is `LookupSpace` with one
more refusal: a space that has its address and no instance is a
`NoInstanceError`, exit 1, since there is no host to act on; the way on is
`space replace` again or `space destroy`, which act on such a space and keep
using `LookupSpace`, as `space list` keeps using `Spaces`. It makes the same
two listing calls and holds nothing more.

`space replace` (D18) moves the one address from the old instance to the new
one with `DisassociateAddress` and `AssociateAddress`, which the `EC2`
interface already carries; `space destroy` (D06) releases it with
`DisassociateAddress` and `ReleaseAddress`. Nothing here tags a resource
after it is made.

The S3 client addresses the bucket path-style because the
bucket's name has dots. `ListObjects` never holds a whole listing: it asks
S3 for one page at a time, at most `ObjectPageSize` keys, the most one
`ListObjectsV2` response carries, and hands each page to the caller's
callback before it asks for the next, following the continuation token
until S3 reports the listing done or the callback returns false. A caller
holds one page and what it folds out of it: a check that a prefix is empty
holds one flag and stops after the first page, and a search for the newest
key holds the newest key seen so far. A failure on any page is the
`ListObjectsV2` error naming the bucket and prefix, wherever it falls; the
pages already handed over stay handed over. A page S3 reports truncated
but gives no continuation token for is a failure of the same kind, not the
end of the listing.

`CopyObject` copies one object to another key in the
same bucket without its bytes passing through devctl, which is how `golden
capture` and `seed` (D15) move snapshots; S3 copies an object of up to 5 GiB
in one such call, and a larger one fails with S3's own error. Its error names
no subject, as the stories show (`s3 CopyObject: SlowDown`), because a copy has
two keys and the story line names neither.

Route 53 gains a point read, `FindRecord`, which is what `apex` uses to read
the root's `A` record and what `space create` uses to see whether a name is
delegated away by an `NS` record; upserts, deletes, and the `INSYNC` wait use
the same `ChangeRecords` and `ChangeStatus` the space's own two records use.
`IAM.PutRolePolicy` replaces a role's inline policy whole, which is how
`create` writes it and how `apex set` and `apex clear` add and remove the
`_acme-challenge.<root>` permission. `LaunchReady` is a side-effect-free
probe: it asks EC2 whether a launch spec would be accepted, which is how a
caller confirms a just-created instance profile has become usable before it
commits to a real launch. `DescribeInstance` answers the same kind of lag the
same way: EC2's API is eventually consistent, so an instance id `RunInstances`
has just returned can be unknown to `DescribeInstances` for a moment, which
reports it as the error code `InvalidInstanceID.NotFound`. That is not a
failure but an instance not yet visible, so `DescribeInstance` returns the
zero `Instance`, exactly as it does for a response that carries no instance,
and a caller polling for a state keeps polling.

## REQUIREMENTS

- R-Y7GF-A1BT: Package `internal/cloud` MUST export the type `Opener func(ctx context.Context, profile, region string) (Clients, error)` and a `Clients` struct whose fields are exactly `EC2 EC2`, `SSM SSM`, `Route53 Route53`, `S3 S3`, `IAM IAM`, and `STS STS`.

- R-8YCX-BKJM: Package `internal/cloud` MUST export a `Session` struct whose fields are exactly `AccountID string` and `Clients Clients`, and `Connect(ctx context.Context, open Opener, profile, region string) (Session, error)`.

- R-Q5B4-FD08: `cloud.Connect` MUST call `open` exactly once with `profile` and `region` unchanged, MUST then call `CallerAccountID` on the `STS` of the clients that call returned before, and instead of, any other method of any of those clients, and MUST return a `Session` whose `Clients` are those clients and whose `AccountID` is what `CallerAccountID` returned; when `open` or `CallerAccountID` returns an error it MUST return the zero `Session` and that error unchanged; verified with a fake `Opener` that records its arguments and fake clients that record every call.

- R-Y8OB-NT2I: Package `internal/cloud` MUST export an `Error` struct whose fields are exactly `Service string`, `Operation string`, `Subject string`, `Code string`, and `Err error`, with the methods `Error() string` and `Unwrap() error`, where `Unwrap` returns `Err`.

- R-Q6J0-T4QX: `(*cloud.Error).Error` MUST return `<Service> <Operation> <Subject>: <Code>` when `Subject` is not empty and `<Service> <Operation>: <Code>` when it is, and MUST use the message of `Err` in place of `<Code>` when `Code` is empty, verified at least by reproducing `ssm GetParameter /sbx1.ikigenba.dev/crm: ParameterNotFound`, `ec2 RunInstances: InsufficientInstanceCapacity`, `route53 ChangeResourceRecordSets: Throttling`, and, for an empty `Code` and an `Err` whose message is an arbitrary string `<m>`, `sts GetCallerIdentity: <m>`.

- R-Q7QX-6WHM: Package `internal/cloud` MUST export a `NotFoundError` struct whose fields are exactly `Kind string` and `Name string` and whose `Error()` returns `no <Kind> '<Name>'`, verified at least by reproducing `no hosted zone 'ikigenba.dev'`, `no launch template 'ikigenba.dev'`, and `no permissions boundary 'ikigenba.dev'`.

- R-OLAI-3GX9: Package `internal/cloud` MUST export the type `InstanceState string` with the constants `StatePending = "pending"`, `StateRunning = "running"`, `StateShuttingDown = "shutting-down"`, `StateTerminated = "terminated"`, `StateStopping = "stopping"`, and `StateStopped = "stopped"`; an `Instance` struct whose fields are exactly `ID string`, `Space string`, `State InstanceState`, and `Address string`; an `Address` struct whose fields are exactly `AllocationID string`, `AssociationID string`, `InstanceID string`, `IP string`, and `Space string`; and a `LaunchSpec` struct whose fields are exactly `LaunchTemplateID string`, `InstanceProfile string`, `Domain string`, and `Space string`.

- R-3YQV-YM7M: Package `internal/cloud` MUST export an `EC2` interface whose methods are exactly `LaunchTemplate(ctx context.Context, name string) (string, error)`, `ListSpaceInstances(ctx context.Context, domain string) ([]Instance, error)`, `DescribeInstance(ctx context.Context, id string) (Instance, error)`, `RunInstance(ctx context.Context, spec LaunchSpec) (Instance, error)`, `LaunchReady(ctx context.Context, spec LaunchSpec) (bool, error)`, `StartInstance(ctx context.Context, id string) error`, `StopInstance(ctx context.Context, id string) error`, `TerminateInstance(ctx context.Context, id string) error`, `InstanceChecksPassed(ctx context.Context, id string) (bool, error)`, `ListSpaceAddresses(ctx context.Context, domain string) ([]Address, error)`, `AllocateAddress(ctx context.Context, domain, space string) (Address, error)`, `AssociateAddress(ctx context.Context, allocationID, instanceID string) error`, `DisassociateAddress(ctx context.Context, associationID string) error`, and `ReleaseAddress(ctx context.Context, allocationID string) error`.

- R-YDJX-6W1A: Package `internal/cloud` MUST export a `Parameter` struct whose fields are exactly `Name string` and `Value string`, and an `SSM` interface whose methods are exactly `GetParameter(ctx context.Context, name string) (string, error)`, `PutSecureParameter(ctx context.Context, name, value string) error`, `ListParameters(ctx context.Context, prefix string) ([]Parameter, error)`, and `DeleteParameter(ctx context.Context, name string) error`.

- R-6ODW-R89G: Package `internal/cloud` MUST export a `Zone` struct whose fields are exactly `ID string` and `Name string`; a `Record` struct whose fields are exactly `Name string`, `Type string`, `TTL int64`, and `Values []string`; the type `ChangeAction string` with the constants `ChangeUpsert = "UPSERT"` and `ChangeDelete = "DELETE"`; a `RecordChange` struct whose fields are exactly `Action ChangeAction` and `Record Record`; and the type `ChangeStatus string` with the constants `ChangePending = "PENDING"` and `ChangeInsync = "INSYNC"`.

- R-QBEM-C7PP: Package `internal/cloud` MUST export a `Route53` interface whose methods are exactly `Zone(ctx context.Context, name string) (Zone, error)`, `ListRecords(ctx context.Context, zoneID string) ([]Record, error)`, `FindRecord(ctx context.Context, zoneID, name, recordType string) (Record, bool, error)`, `ChangeRecords(ctx context.Context, zoneID string, changes []RecordChange) (string, error)`, and `ChangeStatus(ctx context.Context, changeID string) (ChangeStatus, error)`.

- R-7CIE-QH6R: Package `internal/cloud` MUST export an `Object` struct whose fields are exactly `Key string`, `Size int64`, and `Modified time.Time`, and an `S3` interface whose methods are exactly `ListObjects(ctx context.Context, bucket, prefix string, each func(page []Object) bool) error`, `PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error`, `CopyObject(ctx context.Context, bucket, source, key string) error`, and `DeleteObjects(ctx context.Context, bucket string, keys []string) error`.

- R-7DQB-48XG: Package `internal/cloud` MUST export the untyped constant `ObjectPageSize = 1000`, the most objects `ListObjects` hands to one call of `each`.

- R-QCMI-PZGE: Package `internal/cloud` MUST export a `RoleSpec` struct whose fields are exactly `Name string`, `AssumeRolePolicy string`, and `PermissionsBoundaryARN string`, and an `IAM` interface whose methods are exactly `PermissionsBoundary(ctx context.Context, name string) (string, error)`, `RoleExists(ctx context.Context, name string) (bool, error)`, `CreateRole(ctx context.Context, spec RoleSpec) error`, `PutRolePolicy(ctx context.Context, role, policy, document string) error`, `DeleteRolePolicy(ctx context.Context, role, policy string) error`, `InstanceProfileRoles(ctx context.Context, name string) ([]string, bool, error)`, `CreateInstanceProfile(ctx context.Context, name string) error`, `AddRoleToInstanceProfile(ctx context.Context, profile, role string) error`, `RemoveRoleFromInstanceProfile(ctx context.Context, profile, role string) error`, `DeleteInstanceProfile(ctx context.Context, name string) error`, and `DeleteRole(ctx context.Context, name string) error`.

- R-YKVB-HIHG: Package `internal/cloud` MUST export an `STS` interface whose only method is `CallerAccountID(ctx context.Context) (string, error)`.

- R-3ZYS-CDYB: Package `internal/cloud` MUST export a `Space` struct whose fields are exactly `Domain string`, `ID string`, `State InstanceState`, `Address string`, and `ElasticIP Address`; `Spaces(ctx context.Context, ec2 EC2, domain string) ([]Space, error)`; and `LookupSpace(ctx context.Context, ec2 EC2, domain, space string) (Space, error)`.

- R-QF2B-HIXS: Package `internal/cloud` MUST export a `NoSpaceError` struct whose only field is `Domain string` and whose `Error()` returns `no space at '<Domain>'`.

- R-NIT5-O5N2: `cloud.LookupSpace` MUST consider only the instances `ec2.ListSpaceInstances(ctx, domain)` reports whose `Space` equals `space` and whose `State` is not `StateTerminated`, and the addresses `ec2.ListSpaceAddresses(ctx, domain)` reports whose `Space` equals `space`; when there is at most one such instance and at most one such address, and at least one of the two, it MUST return a `Space` whose `Domain` is `space`, whose `ID` and `State` are the instance's or empty when there is none, whose `ElasticIP` is the address exactly as `ListSpaceAddresses` reported it or the zero `Address` when there is none, and whose `Address` is that address's `IP` when there is one and the instance's `Address` otherwise; and it MUST NOT fail because of instances or addresses carrying any other `Space` value; verified at least, for `sbx1.ikigenba.dev`, by a running `i-0e5d4c3b2a1f09876` at `18.118.7.42` and the address `18.118.7.42` with allocation id `eipalloc-01` and association id `eipassoc-01` giving that `ID`, `StateRunning`, `Address` `18.118.7.42`, and that address as `ElasticIP`; by a terminated `i-0c9e94542d98846a8` and the address `18.118.7.42` with allocation id `eipalloc-01` and no association id giving an empty `ID`, an empty `State`, `Address` `18.118.7.42`, and that address as `ElasticIP`; by a running `i-0e5d4c3b2a1f09876` at `3.22.140.61` and no address giving `Address` `3.22.140.61` and the zero `ElasticIP`; by that instance at `3.22.140.61` and the address `18.220.10.5` associated with nothing giving `Address` `18.220.10.5`; by `sbx2.ikigenba.dev` being found while two instances that are not terminated carry `sbx1.ikigenba.dev`; and by `sbx2.ikigenba.dev` being found while the two addresses `18.118.7.42` and `18.190.33.61` carry `sbx1.ikigenba.dev`.

- R-Y156-VX9O: `cloud.LookupSpace` MUST return the zero `Space` and a `*NoSpaceError` whose `Domain` is `space` when neither an instance whose `State` is not `StateTerminated` nor an address carries `space`, and only then; verified at least by a fake whose only instance carrying `sbx1.ikigenba.dev` is terminated and which reports no address carrying it, giving a `*NoSpaceError` whose `Domain` is `sbx1.ikigenba.dev`, by the same fake with the address `18.118.7.42` carrying `sbx1.ikigenba.dev` added giving no error, and by a fake whose only resource carrying `sbx1.ikigenba.dev` is a stopped `i-0c9e94542d98846a8` with no address giving no error and a `Space` whose `ID` is `i-0c9e94542d98846a8` and whose `State` is `StateStopped`.

- R-43MH-HP6E: `cloud.LookupSpace` MUST return a `*DuplicateSpaceError` whose `Domain` is `space` and whose `IDs` are the `ID`s of the instances carrying `space` whose `State` is not `StateTerminated`, sorted ascending, when there are two or more, whatever addresses carry `space`; verified at least through `cli.Run` by `devctl space stop sbx1` while `i-0f1e2d3c4b5a69788` and `i-0c9e94542d98846a8` are both not terminated and carry `sbx1.ikigenba.dev` writing the single stderr line `devctl: 'sbx1.ikigenba.dev' has more than one active instance (i-0c9e94542d98846a8, i-0f1e2d3c4b5a69788)` with empty stdout, exit 2, and `StopInstance` not called.

- R-44UD-VGX3: `cloud.Spaces` MUST return one `Space` for each distinct `Space` value carried by an instance `ec2.ListSpaceInstances(ctx, domain)` reports whose `State` is not `StateTerminated` or by an address `ec2.ListSpaceAddresses(ctx, domain)` reports, each equal to the `Space` that `cloud.LookupSpace(ctx, ec2, domain, <that value>)` would return, sorted by `Domain`, and MUST return an empty result and a nil error when there are none; verified at least by a fake reporting a running `i-0e5d4c3b2a1f09876` at `18.118.7.42` with the address `18.118.7.42` for `sbx1.ikigenba.dev`, the address `3.141.27.86` alone for `sbx2.ikigenba.dev`, a running `i-0a1b2c3d4e5f60718` at `3.22.140.61` alone for `new.ikigenba.dev`, and a terminated instance alone for `gone.ikigenba.dev`, giving exactly the spaces `new.ikigenba.dev`, `sbx1.ikigenba.dev`, and `sbx2.ikigenba.dev` in that order, the last with an empty `ID` and `State` and the `Address` `3.141.27.86`.

- R-NK12-1XDR: `cloud.Spaces` MUST return, rather than choosing, the error `cloud.LookupSpace(ctx, ec2, domain, <value>)` would return for the first `Space` value in `Domain` order that two or more instances whose `State` is not `StateTerminated` or two or more addresses carry: a `*DuplicateSpaceError` when two or more such instances carry it, as R-43MH-HP6E states, and a `*DuplicateAddressError` otherwise, as R-NGDC-WM5O states; verified at least by a fake with `i-0f1e2d3c4b5a69788` and `i-0c9e94542d98846a8` both running and carrying `sbx1.ikigenba.dev` giving a `*DuplicateSpaceError` whose `Domain` is `sbx1.ikigenba.dev` and whose `IDs` are exactly `i-0c9e94542d98846a8` and `i-0f1e2d3c4b5a69788`, and by a fake with the addresses `18.190.33.61` and `18.118.7.42` carrying `sbx1.ikigenba.dev` and those two running instances carrying `sbx2.ikigenba.dev` giving a `*DuplicateAddressError` whose `Domain` is `sbx1.ikigenba.dev` and whose `IPs` are exactly `18.118.7.42` and `18.190.33.61`.

- R-Y2D3-9P0D: `cloud.Spaces` and `cloud.LookupSpace` MUST each return the error `ListSpaceInstances` or `ListSpaceAddresses` returns unchanged, with no `Space`, when either call fails; verified for each of the two functions by a fake whose `ListSpaceInstances` fails, and separately by a fake whose `ListSpaceInstances` succeeds reporting a running instance carrying `sbx1.ikigenba.dev` and whose `ListSpaceAddresses` fails, each giving that call's error, compared with `errors.Is`, and no `Space`.

- R-FF0D-KZ45: Package `internal/cloud` MUST export a `DuplicateSpaceError` struct whose fields are exactly `Domain string` and `IDs []string`, with the methods `Error() string`, returning `'<Domain>' has more than one active instance (<IDs joined by ", ">)`, and `ExitCode() int`, returning 2, verified at least by reproducing `'sbx1.ikigenba.dev' has more than one active instance (i-0c9e94542d98846a8, i-0f1e2d3c4b5a69788)`.

- R-NDXK-52OA: Package `internal/cloud` MUST export a `NoInstanceError` struct whose only field is `Domain string` and whose `Error()` returns `'<Domain>' has no instance`, verified at least by reproducing `'sbx1.ikigenba.dev' has no instance`.

- R-NF5G-IUEZ: Package `internal/cloud` MUST export a `DuplicateAddressError` struct whose fields are exactly `Domain string` and `IPs []string`, with the methods `Error() string`, returning `'<Domain>' has more than one Elastic IP (<IPs joined by ", ">)`, and `ExitCode() int`, returning 2, verified at least by reproducing `'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)`.

- R-NGDC-WM5O: `cloud.LookupSpace` MUST return the zero `Space` and a `*DuplicateAddressError` whose `Domain` is `space` and whose `IPs` are the `IP`s of the addresses carrying `space`, sorted ascending as strings, when two or more addresses carry `space` and fewer than two instances whose `State` is not `StateTerminated` carry it; verified at least by a fake reporting, in this order, the addresses `18.190.33.61` and `18.118.7.42` carrying `sbx1.ikigenba.dev` and one running instance carrying it, giving `IPs` exactly `18.118.7.42` and `18.190.33.61`, and through `cli.Run` by `devctl space replace sbx1` with those two addresses and no other resource carrying `sbx1.ikigenba.dev` writing the single stderr line `devctl: 'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)` with empty stdout, exit 2, no `ssh` or `scp` command run, and none of `DisassociateAddress`, `TerminateInstance`, and `RunInstance` called.

- R-NHL9-ADWD: Package `internal/cloud` MUST export `LookupInstance(ctx context.Context, ec2 EC2, domain, space string) (Space, error)`.

- R-NL8Y-FP4G: `cloud.LookupInstance` MUST return exactly the `Space` and nil error that `cloud.LookupSpace(ctx, ec2, domain, space)` returns when that `Space`'s `ID` is not empty, and MUST call no `EC2` method other than `ListSpaceInstances` and `ListSpaceAddresses`; verified at least, for `sbx1.ikigenba.dev`, by a running `i-0e5d4c3b2a1f09876` and the address `18.118.7.42` giving that `ID`, `StateRunning`, and `Address` `18.118.7.42`, and by a stopped `i-0c9e94542d98846a8` and no address giving that `ID` and `StateStopped`, each with a fake recording every call.

- R-NMGU-TGV5: `cloud.LookupInstance` MUST return the zero `Space` and a `*NoInstanceError` whose `Domain` is `space` when `cloud.LookupSpace(ctx, ec2, domain, space)` returns a nil error and a `Space` whose `ID` is empty; verified at least by a fake whose only instance carrying `sbx1.ikigenba.dev` is terminated and which reports the address `18.118.7.42` with allocation id `eipalloc-01` and no association id carrying it, giving a `*NoInstanceError` whose `Domain` is `sbx1.ikigenba.dev`.

- R-NNOR-78LU: `cloud.LookupInstance` MUST return the zero `Space` and the error `cloud.LookupSpace(ctx, ec2, domain, space)` returns, unchanged, whenever that error is not nil; verified at least by a fake reporting nothing carrying `sbx1.ikigenba.dev` giving a `*NoSpaceError`, by two running instances carrying it giving a `*DuplicateSpaceError`, by two addresses carrying it giving a `*DuplicateAddressError`, each matched with `errors.As` and equal to what `LookupSpace` returns for the same fake, and by a fake whose `ListSpaceAddresses` fails giving that error, compared with `errors.Is`.

- R-YM37-VA85: Package `internal/cloud/awssdk` MUST export `Open(ctx context.Context, profile, region string) (cloud.Clients, error)` whose type is assignable to `cloud.Opener`, and every field of the `cloud.Clients` it returns with a nil error MUST be non-nil.

- R-YNB4-91YU: `awssdk.Open` MUST pass `profile` to the AWS SDK's shared-config loader as the shared-config profile to load, with no trimming, case change, or other normalisation, MUST make every client it returns call the region named by `region` when `region` is not empty and the region that profile's shared configuration names when it is empty, and MUST return a non-nil error and no clients when that configuration cannot be loaded.

- R-CG22-V9E5: Package `internal/cloud/awssdk` MUST export `ConfigLoader func(ctx context.Context, profile, region string) (aws.Config, error)` and `OpenWithLoader(ctx context.Context, profile, region string, load ConfigLoader) (cloud.Clients, error)`; OpenWithLoader MUST use only the supplied loader for configuration and credentials, passing profile and region unchanged.

- R-2WML-KG2W: Adapter tests MUST exercise OpenWithLoader using a supplied configuration, fake credentials and in-memory HTTP transport, with no default credential chain, home directory, process environment or network access.

- R-QIQ0-MU5V: The `S3` client `awssdk.OpenWithLoader` returns MUST address every request path-style: for a bucket `<bucket>` the request URL's path is `/<bucket>` or begins `/<bucket>/` and its host does not begin `<bucket>.`; verified through the in-memory transport for a bucket name containing a dot, for at least `ListObjects`, `PutObject`, and `DeleteObjects`.

- R-VV8S-ZVE7: Every non-nil error returned by a method of any client `awssdk.Open` returns, other than the `*cloud.NotFoundError` that `LaunchTemplate`, `Zone`, and `PermissionsBoundary` return for absence, MUST be a `*cloud.Error` whose `Service` and `Operation` are the service and AWS API operation that method's mapping names, whose `Subject` is the subject that mapping names or empty when it names none, whose `Code` is the value of `ErrorCode()` of the first `smithy.APIError` found by unwrapping the SDK error with the search stopping at any `*smithy.OperationError` other than the SDK error itself, and empty when the search stops or ends without finding one, so that a code belonging to a nested operation the `cloud.Error` does not name is never reported as that operation's code, and whose `Err` is the error the SDK returned with its outermost `*smithy.OperationError`, when the SDK error is one, replaced by the error that operation error wraps, so that the message of `Err` does not repeat the service and operation the `cloud.Error` already names; verified at least with constructed chains, needing no network: one whose outermost `*smithy.OperationError` is its only operation error and wraps an error satisfying `smithy.APIError`, for which `Code` is that error's `ErrorCode()` and the message of `Err` is that of that error; and one whose outermost `*smithy.OperationError` wraps, through further wrapping errors, a second `*smithy.OperationError` whose own wrapped error satisfies `smithy.APIError`, for which `Code` is empty and the message of `Err` is that of the error the outermost operation error wraps.

- R-YPQX-0LG8: Each method of each client `awssdk.Open` returns MUST invoke no AWS API operation other than the one its mapping names.

- R-7EY7-I0O5: `ListSpaceInstances`, `ListParameters`, `ListRecords`, and `PermissionsBoundary`, as `awssdk` implements them, MUST follow the AWS API's pagination to the last page and consider the items of every page, verified against a fake endpoint that answers with more than one page.

- R-YS6P-S4XM: `DeleteParameter`, `DeleteObjects`, `DisassociateAddress`, `ReleaseAddress`, `DeleteRolePolicy`, `RemoveRoleFromInstanceProfile`, `DeleteInstanceProfile`, and `DeleteRole`, as `awssdk` implements them, MUST return a nil error when the thing they name does not exist.

- R-QMDP-S5DY: In `awssdk` the `EC2` methods MUST map to the service `ec2` with no subject and the operations `LaunchTemplate`→`DescribeLaunchTemplates`, `ListSpaceInstances`→`DescribeInstances`, `DescribeInstance`→`DescribeInstances`, `RunInstance`→`RunInstances`, `StartInstance`→`StartInstances`, `StopInstance`→`StopInstances`, `TerminateInstance`→`TerminateInstances`, `InstanceChecksPassed`→`DescribeInstanceStatus`, `ListSpaceAddresses`→`DescribeAddresses`, `AllocateAddress`→`AllocateAddress`, `AssociateAddress`→`AssociateAddress`, `DisassociateAddress`→`DisassociateAddress`, and `ReleaseAddress`→`ReleaseAddress`; `RunInstance` MUST launch one instance from `spec.LaunchTemplateID` with the instance profile named by `spec.InstanceProfile` and MUST return the instance as `RunInstances` reports it, without waiting for it to run; `InstanceChecksPassed` MUST return true only when both the instance status and the system status the response carries are `ok`; and `Instance.Address` and `Address.AssociationID` MUST be empty when the response carries none.

- R-QOTI-JOVC: In `awssdk`, `LaunchTemplate` MUST request only launch templates matching the filter `launch-template-name=<name>`, MUST return the id of the launch template the response carries, and MUST return a `*cloud.NotFoundError` whose `Kind` is `launch template` and whose `Name` is `name` when the response carries none.

- R-H32Z-PBQW: In `awssdk`, `LaunchReady` MUST call the service `ec2` operation `RunInstances` with the launch template `spec.LaunchTemplateID` and the instance profile named by `spec.InstanceProfile`, exactly as `RunInstance` does but with its dry-run flag set so that no instance is created; it MUST return `true` and a nil error when that call reports the request would have succeeded (error code `DryRunOperation`), MUST return `false` and a nil error when that call reports the instance profile is not yet usable (error code `InvalidParameterValue` whose message contains `Invalid IAM Instance Profile name`), and MUST return a `*cloud.Error` for operation `RunInstances` for any other error.

- R-5LKS-2NVC: In `awssdk`, `DescribeInstance` MUST request only the instance `id`; it MUST return the zero `cloud.Instance` and a nil error both when the call fails with an error whose code, found as R-VV8S-ZVE7 finds `Code`, is `InvalidInstanceID.NotFound` and when the response carries no instance, and MUST return a `*cloud.Error` for operation `DescribeInstances` for any other error; verified at least with a fake SDK client that records a request whose `InstanceIds` is exactly `[id]`, one whose `DescribeInstances` fails with an error satisfying `smithy.APIError` whose `ErrorCode()` is `InvalidInstanceID.NotFound`, and one whose `ErrorCode()` is `UnauthorizedOperation` giving a `*cloud.Error` whose `Code` is `UnauthorizedOperation`.

- R-9G1O-WPDJ: In `awssdk`, `ListSpaceInstances` MUST request only instances matching both the filter `tag:Domain=<domain>` and the filter `tag-key=Space` and MUST set each returned `Instance.Space` to the value of that instance's `Space` tag; `ListSpaceAddresses` MUST request only addresses matching both of those filters and MUST set each returned `Address.Space` likewise, and MUST set each returned `Address.InstanceID` to the instance id the `DescribeAddresses` response reports the address associated with, whether or not that instance carries a `Space` tag, and to the empty string when it reports the address associated with no instance; and `RunInstance` and `AllocateAddress` MUST apply the tags `Domain=<domain>` and `Space=<space>` — from `spec.Domain` and `spec.Space` to the instance and the instance's volumes on `RunInstance`, and from `domain` and `space` to the address on `AllocateAddress` — in the same request that creates the resource.

- R-YVUE-XG5P: In `awssdk` the `SSM` methods MUST map to the service `ssm` with the parameter name as the subject and the operations `GetParameter`→`GetParameter`, `PutSecureParameter`→`PutParameter`, `ListParameters`→`GetParametersByPath`, and `DeleteParameter`→`DeleteParameter`; `GetParameter` and `ListParameters` MUST request decryption; `ListParameters` MUST recurse below `prefix` and MUST carry `prefix` as its subject; and `PutSecureParameter` MUST write type `SecureString` and MUST overwrite an existing value.

- R-QR9B-B8CQ: In `awssdk` the `Route53` methods MUST map to the service `route53` with no subject and the operations `Zone`→`ListHostedZonesByName`, `ListRecords`→`ListResourceRecordSets`, `FindRecord`→`ListResourceRecordSets`, `ChangeRecords`→`ChangeResourceRecordSets`, and `ChangeStatus`→`GetChange`; `ChangeRecords` MUST submit every element of `changes` in one change batch, each as the action its `Action` names with the name, type, TTL and values of its `Record`, and MUST return the change id the response carries; and `ChangeStatus` MUST accept that change id unchanged and return the status the response carries.

- R-QSH7-P03F: In `awssdk`, `Zone` MUST request the listing to start at the DNS name `name`, MUST return the first hosted zone the response lists whose name, trailing dot removed, equals `name`, and MUST return a `*cloud.NotFoundError` whose `Kind` is `hosted zone` and whose `Name` is `name` when the response lists no such zone.

- R-QTP4-2RU4: In `awssdk`, `FindRecord` MUST request the listing to start at the record name `name` and the record type `recordType` with at most one item, MUST return that record set and a true second result when the response's first record set has type `recordType` and a name that, trailing dot removed and with a leading `\052` counted equal to a leading `*` in `name`, equals `name`, and MUST return the zero `Record`, false, and a nil error otherwise.

- R-QUX0-GJKT: In `awssdk`, `Zone` MUST return `Zone.ID` with any `/hostedzone/` prefix removed and `Zone.Name` with its trailing dot removed, `ListRecords` and `FindRecord` MUST return each `Record.Name` as the API reports it except for its trailing dot, leaving any `\052` or other escape undecoded, and `ChangeRecords` MUST send each `Record.Name` as it was given with no escaping or unescaping of its own, so that a `Record` read by `ListRecords` or `FindRecord` and passed back in a `ChangeDelete` names the same record set.

- R-QW4W-UBBI: In `awssdk` the `IAM` methods MUST map to the service `iam` with no subject and the operations `PermissionsBoundary`→`ListPolicies`, `RoleExists`→`GetRole`, `CreateRole`→`CreateRole`, `PutRolePolicy`→`PutRolePolicy`, `DeleteRolePolicy`→`DeleteRolePolicy`, `InstanceProfileRoles`→`GetInstanceProfile`, `CreateInstanceProfile`→`CreateInstanceProfile`, `AddRoleToInstanceProfile`→`AddRoleToInstanceProfile`, `RemoveRoleFromInstanceProfile`→`RemoveRoleFromInstanceProfile`, `DeleteInstanceProfile`→`DeleteInstanceProfile`, and `DeleteRole`→`DeleteRole`; `CreateRole` MUST create the role named `spec.Name` with `spec.AssumeRolePolicy` as its trust policy and `spec.PermissionsBoundaryARN` as its permissions boundary; `PutRolePolicy` MUST set the inline policy named `policy` of the role named `role` to exactly `document`, replacing any document it held; `RoleExists` MUST report absence as a false first result with a nil error, and `InstanceProfileRoles` MUST report absence as a false second result with a nil error; and `InstanceProfileRoles` MUST return the names of the roles the instance profile holds.

- R-QXCT-8327: In `awssdk`, `PermissionsBoundary` MUST list only the account's customer managed policies (scope `Local`), MUST return the ARN of the listed policy whose name equals `name`, and MUST return a `*cloud.NotFoundError` whose `Kind` is `permissions boundary` and whose `Name` is `name` when no listed policy has that name.

- R-Z35T-82LV: In `awssdk`, `CallerAccountID` MUST map to the service `sts`, the operation `GetCallerIdentity`, and no subject, and MUST return the account id the response carries.

- R-7HE0-9K5J: In `awssdk` the `S3` methods MUST map to the service `s3` with `<bucket>/<key>` as the subject — `<bucket>/<prefix>` for `ListObjects`, `<bucket>` for `DeleteObjects`, and no subject for `CopyObject` — and the operations `ListObjects`→`ListObjectsV2`, `PutObject`→`PutObject`, `CopyObject`→`CopyObject`, and `DeleteObjects`→`DeleteObjects`; `PutObject` MUST send `size` as the object's length; `CopyObject` MUST make the object at `key` in `bucket` a copy of the object at `source` in `bucket`, replacing any object already at `key`; and `DeleteObjects` MUST delete every key in `keys`, in requests of at most 1000 keys each.

- R-7ILW-NBW8: In `awssdk`, `ListObjects` MUST send every `ListObjectsV2` request for `bucket` with the prefix `prefix` and `MaxKeys` equal to `cloud.ObjectPageSize`, the first with no continuation token and each later one with the `NextContinuationToken` of the response before it, MUST send a further request only while the latest response reports itself truncated, MUST call `each` once for each response that lists at least one object, in the order the responses arrived, with exactly that response's objects in the order it lists them, each with its key, size, and last-modified time, and not at all for a response that lists none, and MUST return a nil error after a response that is not truncated; verified through the in-memory transport by a listing answered in three pages of two, two, and one objects giving exactly three requests, each carrying that prefix and that `MaxKeys`, the second and third carrying the token the response before returned, and three calls of `each` with those objects, and by an empty listing giving one request, no call of `each`, and a nil error.

- R-7JTT-13MX: In `awssdk`, `ListObjects` MUST hold at most one page at a time: it MUST NOT send the request for a page before `each` has returned for the page before it, and once `each` returns false it MUST send no further request, call `each` no more, and return a nil error; verified through the in-memory transport by a listing answered in three pages, recording at each call of `each` the number of requests sent so far, which is that call's position, first, second, or third, and by an `each` that returns false for the first of those pages giving exactly one request, one call of `each`, and a nil error.

- R-7L1P-EVDM: In `awssdk`, when any `ListObjectsV2` request of a `ListObjects` call fails, `ListObjects` MUST return a `*cloud.Error` as R-VV8S-ZVE7 states, for that operation with `<bucket>/<prefix>` as its subject, MUST send no further request, and MUST NOT call `each` for that page or any after it; verified through the in-memory transport for the bucket `ikigenba.dev` and the prefix `sbx1/host/` by a listing whose first response is truncated and whose second request is answered with the error code `SlowDown`, giving exactly two requests, one call of `each`, and an error whose message is exactly `s3 ListObjectsV2 ikigenba.dev/sbx1/host/: SlowDown`.

- R-GSOH-NDVV: In `awssdk`, when a `ListObjectsV2` response of a `ListObjects` call reports itself truncated but carries no `NextContinuationToken`, `ListObjects` MUST return a `*cloud.Error` whose `Service` is `s3`, whose `Operation` is `ListObjectsV2`, whose `Subject` is `<bucket>/<prefix>`, and whose `Err` is not nil, MUST send no further request, and MUST have called `each` for every earlier response that listed at least one object and not for that response; verified through the in-memory transport for the bucket `ikigenba.dev` and the prefix `sbx1/host/` by a listing whose first response is truncated with a token and lists two objects and whose second response is truncated with no token and lists one object, giving exactly two requests, one call of `each` with the first response's two objects, and an error that `errors.As` matches to a `*cloud.Error` with those fields.

- R-SL1F-5YCK: The `CopyObject` request the `S3` client `awssdk.OpenWithLoader` returns sends MUST address `bucket` path-style as R-QIQ0-MU5V states, MUST name `key` as the destination, and MUST carry a copy source that, percent-decoded, is exactly `<bucket>/<source>`; verified through the in-memory transport for the bucket `ikigenba.dev`, the source `sbx1/snapshots/crm/2026-09-12T14:22:51Z.tar.zst`, and the key `golden/demo/crm/2026-09-12T14:22:51Z.tar.zst`, and by a response carrying the error code `SlowDown` giving a `*cloud.Error` whose message is exactly `s3 CopyObject: SlowDown`.

- R-QZSL-ZMJL: When a command fails because `cloud.Connect` or a method of a client of the `cloud.Clients` it opened returned an error that `errors.As` matches to a `*cloud.Error` or a `*cloud.NotFoundError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line on stderr, write nothing further to stdout, and return 1, verified at least with fake clients reproducing `devctl: sts GetCallerIdentity: <m>` for an `STS` whose `CallerAccountID` fails with an empty `Code` and an `Err` whose message is `<m>`, `devctl: ec2 RunInstances: InsufficientInstanceCapacity`, `devctl: route53 ChangeResourceRecordSets: Throttling`, and `devctl: no launch template 'ikigenba.dev'`.

- R-YTFS-2EAU: When a command fails because `cloud.LookupSpace` returned a `*cloud.NoSpaceError`, `cli.Run` MUST write the single line `devctl: no space at '<domain>'` to stderr, write nothing further to stdout, and return 1, verified at least for `space stop`, `space start`, `space status`, `space restart`, `space logs`, `secrets push`, `deploy`, `restore`, and `apex set`.

- R-NOWN-L0CJ: When a command fails because `cloud.LookupInstance` returned a `*cloud.NoInstanceError`, `cli.Run` MUST write the single line `devctl: '<domain>' has no instance` to stderr, write nothing further to stdout, and return 1, verified at least by `devctl space stop sbx1`, `devctl space start sbx1`, `devctl space status sbx1`, `devctl deploy sbx1 release-87`, `devctl rollback sbx1`, `devctl restore sbx1 crm`, `devctl apex set crm.sbx1`, `devctl golden capture sbx1 demo`, and `devctl seed sbx1 demo`, each while the only resource carrying `sbx1.ikigenba.dev` is the address `18.118.7.42` associated with nothing, writing `devctl: 'sbx1.ikigenba.dev' has no instance` with no `ssh` or `scp` command run.
