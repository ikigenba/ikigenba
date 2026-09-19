// Package cloud defines the provider-neutral cloud boundary.
package cloud

import (
	"context"
	"fmt"
	"io"
	"sort"
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

// Session is an authenticated set of cloud clients and its caller identity.
type Session struct {
	AccountID string
	Clients   Clients
}

// Connect opens cloud clients and establishes the caller's account identity.
func Connect(ctx context.Context, open Opener, profile, region string) (Session, error) {
	clients, err := open(ctx, profile, region)
	if err != nil {
		return Session{}, err
	}

	accountID, err := clients.STS.CallerAccountID(ctx)
	if err != nil {
		return Session{}, err
	}

	return Session{AccountID: accountID, Clients: clients}, nil
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

// NotFoundError reports that a named cloud resource does not exist.
type NotFoundError struct {
	Kind string
	Name string
}

// Error describes the missing resource.
func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no %s '%s'", e.Kind, e.Name)
}

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
	Domain           string
	Space            string
}

// EC2 is the compute service boundary.
type EC2 interface {
	LaunchTemplate(ctx context.Context, name string) (string, error)
	ListSpaceInstances(ctx context.Context, domain string) ([]Instance, error)
	DescribeInstance(ctx context.Context, id string) (Instance, error)
	RunInstance(ctx context.Context, spec LaunchSpec) (Instance, error)
	LaunchReady(ctx context.Context, spec LaunchSpec) (bool, error)
	StartInstance(ctx context.Context, id string) error
	StopInstance(ctx context.Context, id string) error
	TerminateInstance(ctx context.Context, id string) error
	InstanceChecksPassed(ctx context.Context, id string) (bool, error)
	ListSpaceAddresses(ctx context.Context, domain string) ([]Address, error)
	AllocateAddress(ctx context.Context, domain, space string) (Address, error)
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
	Zone(ctx context.Context, name string) (Zone, error)
	ListRecords(ctx context.Context, zoneID string) ([]Record, error)
	FindRecord(ctx context.Context, zoneID, name, recordType string) (Record, bool, error)
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
	PermissionsBoundary(ctx context.Context, name string) (string, error)
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

// Space describes the instance registered for a space domain.
type Space struct {
	Domain  string
	ID      string
	State   InstanceState
	Address string
}

// NoSpaceError reports that a space has no registered instance.
type NoSpaceError struct {
	Domain string
}

// Error describes the absent space.
func (e *NoSpaceError) Error() string {
	return fmt.Sprintf("no space at '%s'", e.Domain)
}

// Spaces lists the non-terminated instances registered as spaces.
func Spaces(ctx context.Context, ec2 EC2, domain string) ([]Space, error) {
	instances, err := ec2.ListSpaceInstances(ctx, domain)
	if err != nil {
		return nil, err
	}

	spaces := make([]Space, 0, len(instances))
	seen := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if instance.State == StateTerminated {
			continue
		}
		if _, exists := seen[instance.Space]; exists {
			return nil, fmt.Errorf("duplicate space '%s'", instance.Space)
		}
		seen[instance.Space] = struct{}{}
		spaces = append(spaces, Space{
			Domain:  instance.Space,
			ID:      instance.ID,
			State:   instance.State,
			Address: instance.Address,
		})
	}

	sort.Slice(spaces, func(i, j int) bool { return spaces[i].Domain < spaces[j].Domain })
	return spaces, nil
}

// LookupSpace finds one space in the domain's instance registry.
func LookupSpace(ctx context.Context, ec2 EC2, domain, space string) (Space, error) {
	spaces, err := Spaces(ctx, ec2, domain)
	if err != nil {
		return Space{}, err
	}
	for _, candidate := range spaces {
		if candidate.Domain == space {
			return candidate, nil
		}
	}
	return Space{}, &NoSpaceError{Domain: space}
}
