// Package cloud defines the provider-neutral cloud boundary.
package cloud

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Opener opens cloud clients for a profile and region.
type Opener func(ctx context.Context, profile, region string) (Clients, error)

// Clients contains the clients for every cloud service devctl uses.
type Clients struct {
	EC2     EC2
	SSM     SSM
	Route53 Route53
	S3      S3
	IAM     IAM
	STS     STS
}

// Error describes a failed cloud service operation.
type Error struct {
	Service   string
	Operation string
	Subject   string
	Code      string
	Err       error
}

// Error formats the service operation and provider error code.
func (e *Error) Error() string {
	code := e.Code
	if code == "" && e.Err != nil {
		code = e.Err.Error()
	}
	if e.Subject == "" {
		return fmt.Sprintf("%s %s: %s", e.Service, e.Operation, code)
	}
	return fmt.Sprintf("%s %s %s: %s", e.Service, e.Operation, e.Subject, code)
}

// Unwrap returns the underlying provider error.
func (e *Error) Unwrap() error { return e.Err }

// InstanceState is the lifecycle state of a compute instance.
type InstanceState string

const (
	// StatePending means an instance is starting.
	StatePending InstanceState = "pending"
	// StateRunning means an instance is running.
	StateRunning InstanceState = "running"
	// StateShuttingDown means an instance is shutting down permanently.
	StateShuttingDown InstanceState = "shutting-down"
	// StateTerminated means an instance has terminated.
	StateTerminated InstanceState = "terminated"
	// StateStopping means an instance is stopping.
	StateStopping InstanceState = "stopping"
	// StateStopped means an instance is stopped.
	StateStopped InstanceState = "stopped"
)

// Instance describes a compute instance.
type Instance struct {
	ID      string
	Space   string
	State   InstanceState
	Address string
}

// Address describes a public address allocated to a space.
type Address struct {
	AllocationID  string
	AssociationID string
	IP            string
	Space         string
}

// LaunchSpec describes an instance to launch.
type LaunchSpec struct {
	LaunchTemplateID string
	InstanceProfile  string
	Space            string
}

// EC2 is the compute service boundary.
type EC2 interface {
	ListSpaceInstances(ctx context.Context) ([]Instance, error)
	DescribeInstance(ctx context.Context, id string) (Instance, error)
	RunInstance(ctx context.Context, spec LaunchSpec) (Instance, error)
	StartInstance(ctx context.Context, id string) error
	StopInstance(ctx context.Context, id string) error
	TerminateInstance(ctx context.Context, id string) error
	InstanceChecksPassed(ctx context.Context, id string) (bool, error)
	ListSpaceAddresses(ctx context.Context) ([]Address, error)
	AllocateAddress(ctx context.Context, space string) (Address, error)
	AssociateAddress(ctx context.Context, allocationID, instanceID string) error
	DisassociateAddress(ctx context.Context, associationID string) error
	ReleaseAddress(ctx context.Context, allocationID string) error
}

// Parameter is a named configuration parameter.
type Parameter struct {
	Name  string
	Value string
}

// SSM is the parameter service boundary.
type SSM interface {
	GetParameter(ctx context.Context, name string) (string, error)
	PutSecureParameter(ctx context.Context, name, value string) error
	ListParameters(ctx context.Context, prefix string) ([]Parameter, error)
	DeleteParameter(ctx context.Context, name string) error
}

// Zone describes a hosted DNS zone.
type Zone struct {
	ID   string
	Name string
}

// Record describes a DNS record set.
type Record struct {
	Name   string
	Type   string
	TTL    int64
	Values []string
}

// ChangeAction is the operation applied to a DNS record set.
type ChangeAction string

const (
	// ChangeUpsert creates or replaces a record set.
	ChangeUpsert ChangeAction = "UPSERT"
	// ChangeDelete deletes a record set.
	ChangeDelete ChangeAction = "DELETE"
)

// RecordChange pairs a DNS record with its requested action.
type RecordChange struct {
	Action ChangeAction
	Record Record
}

// ChangeStatus is the state of a DNS change.
type ChangeStatus string

const (
	// ChangePending means a DNS change is still propagating.
	ChangePending ChangeStatus = "PENDING"
	// ChangeInsync means a DNS change has propagated.
	ChangeInsync ChangeStatus = "INSYNC"
)

// Route53 is the DNS service boundary.
type Route53 interface {
	ListZones(ctx context.Context) ([]Zone, error)
	ListRecords(ctx context.Context, zoneID string) ([]Record, error)
	ChangeRecords(ctx context.Context, zoneID string, changes []RecordChange) (string, error)
	ChangeStatus(ctx context.Context, changeID string) (ChangeStatus, error)
}

// Object describes an object stored in a bucket.
type Object struct {
	Key      string
	Size     int64
	Modified time.Time
}

// S3 is the object storage service boundary.
type S3 interface {
	ListObjects(ctx context.Context, bucket, prefix string) ([]Object, error)
	PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
}

// RoleSpec describes an IAM role to create.
type RoleSpec struct {
	Name                   string
	AssumeRolePolicy       string
	PermissionsBoundaryARN string
}

// IAM is the identity service boundary.
type IAM interface {
	RoleExists(ctx context.Context, name string) (bool, error)
	CreateRole(ctx context.Context, spec RoleSpec) error
	PutRolePolicy(ctx context.Context, role, policy, document string) error
	DeleteRolePolicy(ctx context.Context, role, policy string) error
	InstanceProfileRoles(ctx context.Context, name string) ([]string, bool, error)
	CreateInstanceProfile(ctx context.Context, name string) error
	AddRoleToInstanceProfile(ctx context.Context, profile, role string) error
	RemoveRoleFromInstanceProfile(ctx context.Context, profile, role string) error
	DeleteInstanceProfile(ctx context.Context, name string) error
	DeleteRole(ctx context.Context, name string) error
}

// STS is the caller identity service boundary.
type STS interface {
	CallerAccountID(ctx context.Context) (string, error)
}
