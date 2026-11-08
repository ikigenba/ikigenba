// Package cloud defines the provider-neutral cloud boundary.
package cloud

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
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
	Retired string
}

// Address describes a public address allocated to a space.
type Address struct {
	AllocationID  string
	AssociationID string
	IP            string
	Space         string
	Retired       string
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
	TagRetired(ctx context.Context, ids []string, value string) error
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
	CopyObject(ctx context.Context, bucket, source, key string) error
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

// Spaces lists active spaces, ordered by domain.
func Spaces(ctx context.Context, ec2 EC2, domain string) ([]Space, error) {
	instances, err := ec2.ListSpaceInstances(ctx, domain)
	if err != nil {
		return nil, err
	}
	return activeSpaces(instances)
}

func activeSpaces(instances []Instance) ([]Space, error) {
	groups := make(map[string][]Instance)
	for _, instance := range instances {
		if instance.State != StateTerminated && instance.Retired == "" {
			groups[instance.Space] = append(groups[instance.Space], instance)
		}
	}
	domains := make([]string, 0, len(groups))
	for domain := range groups {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	spaces := make([]Space, 0, len(domains))
	for _, domain := range domains {
		group := groups[domain]
		if len(group) > 1 {
			ids := make([]string, len(group))
			for i, item := range group {
				ids[i] = item.ID
			}
			sort.Strings(ids)
			return nil, &DuplicateSpaceError{Domain: domain, IDs: ids}
		}
		item := group[0]
		spaces = append(spaces, Space{Domain: domain, ID: item.ID, State: item.State, Address: item.Address})
	}
	return spaces, nil
}

// LookupSpace finds the one active instance carrying space.
func LookupSpace(ctx context.Context, ec2 EC2, domain, space string) (Space, error) {
	instances, err := ec2.ListSpaceInstances(ctx, domain)
	if err != nil {
		return Space{}, err
	}
	selected := make([]Instance, 0)
	for _, item := range instances {
		if item.Space == space {
			selected = append(selected, item)
		}
	}
	spaces, err := activeSpaces(selected)
	if err != nil {
		return Space{}, err
	}
	if len(spaces) == 0 {
		return Space{}, &NoSpaceError{Domain: space}
	}
	return spaces[0], nil
}

// DuplicateSpaceError reports ambiguous active instances.
type DuplicateSpaceError struct {
	Domain string
	IDs    []string
}

func (e *DuplicateSpaceError) Error() string {
	return fmt.Sprintf("'%s' has more than one active instance (%s)", e.Domain, strings.Join(e.IDs, ", "))
}

// ExitCode reports an ambiguous operand.
func (e *DuplicateSpaceError) ExitCode() int { return 2 }

// RetiredTag marks resources retained after a move.
const RetiredTag = "Retired"

// RetiredValue formats a retirement timestamp.
func RetiredValue(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// SpaceInstances lists all non-terminated instances, active before retired.
func SpaceInstances(ctx context.Context, ec2 EC2, domain, space string) ([]Instance, error) {
	items, err := ec2.ListSpaceInstances(ctx, domain)
	if err != nil {
		return nil, err
	}
	result := make([]Instance, 0)
	for _, item := range items {
		if item.Space == space && item.State != StateTerminated {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if (result[i].Retired == "") != (result[j].Retired == "") {
			return result[i].Retired == ""
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// SpaceAddresses lists all space addresses, active before retired.
func SpaceAddresses(ctx context.Context, ec2 EC2, domain, space string) ([]Address, error) {
	items, err := ec2.ListSpaceAddresses(ctx, domain)
	if err != nil {
		return nil, err
	}
	result := make([]Address, 0)
	for _, item := range items {
		if item.Space == space {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if (result[i].Retired == "") != (result[j].Retired == "") {
			return result[i].Retired == ""
		}
		return result[i].AllocationID < result[j].AllocationID
	})
	return result, nil
}
