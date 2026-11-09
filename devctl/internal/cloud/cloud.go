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
}

// Address describes a public address allocated to a space.
type Address struct {
	AllocationID  string
	AssociationID string
	InstanceID    string
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

// ObjectPageSize bounds one object-listing callback.
const ObjectPageSize = 1000

// S3 is the object storage service boundary.
type S3 interface {
	ListObjects(ctx context.Context, bucket, prefix string, each func(page []Object) bool) error
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

// Space describes a space's current instance and persistent address.
type Space struct {
	Domain    string
	ID        string
	State     InstanceState
	Address   string
	ElasticIP Address
}

// NoSpaceError reports a label with neither an instance nor an address.
type NoSpaceError struct{ Domain string }

func (e *NoSpaceError) Error() string { return fmt.Sprintf("no space at '%s'", e.Domain) }

// DuplicateSpaceError reports ambiguous active instances.
type DuplicateSpaceError struct {
	Domain string
	IDs    []string
}

func (e *DuplicateSpaceError) Error() string {
	return fmt.Sprintf("'%s' has more than one active instance (%s)", e.Domain, strings.Join(e.IDs, ", "))
}

// ExitCode reports an ambiguous instance registry.
func (e *DuplicateSpaceError) ExitCode() int { return 2 }

// DuplicateAddressError reports ambiguous persistent addresses.
type DuplicateAddressError struct {
	Domain string
	IPs    []string
}

func (e *DuplicateAddressError) Error() string {
	return fmt.Sprintf("'%s' has more than one Elastic IP (%s)", e.Domain, strings.Join(e.IPs, ", "))
}

// ExitCode reports an ambiguous address registry.
func (e *DuplicateAddressError) ExitCode() int { return 2 }

// NoInstanceError reports a space without a host to act on.
type NoInstanceError struct{ Domain string }

func (e *NoInstanceError) Error() string { return fmt.Sprintf("'%s' has no instance", e.Domain) }

func registry(ctx context.Context, ec2 EC2, domain string) (map[string][]Instance, map[string][]Address, error) {
	instances, err := ec2.ListSpaceInstances(ctx, domain)
	if err != nil {
		return nil, nil, err
	}
	addresses, err := ec2.ListSpaceAddresses(ctx, domain)
	if err != nil {
		return nil, nil, err
	}
	hosts := make(map[string][]Instance)
	ips := make(map[string][]Address)
	for _, instance := range instances {
		if instance.State != StateTerminated {
			hosts[instance.Space] = append(hosts[instance.Space], instance)
		}
	}
	for _, address := range addresses {
		ips[address.Space] = append(ips[address.Space], address)
	}
	return hosts, ips, nil
}

func registeredSpace(domain string, hosts []Instance, addresses []Address) (Space, error) {
	if len(hosts) > 1 {
		ids := make([]string, len(hosts))
		for i, host := range hosts {
			ids[i] = host.ID
		}
		sort.Strings(ids)
		return Space{}, &DuplicateSpaceError{Domain: domain, IDs: ids}
	}
	if len(addresses) > 1 {
		ips := make([]string, len(addresses))
		for i, address := range addresses {
			ips[i] = address.IP
		}
		sort.Strings(ips)
		return Space{}, &DuplicateAddressError{Domain: domain, IPs: ips}
	}
	if len(hosts) == 0 && len(addresses) == 0 {
		return Space{}, &NoSpaceError{Domain: domain}
	}
	result := Space{Domain: domain}
	if len(hosts) == 1 {
		result.ID, result.State, result.Address = hosts[0].ID, hosts[0].State, hosts[0].Address
	}
	if len(addresses) == 1 {
		result.ElasticIP, result.Address = addresses[0], addresses[0].IP
	}
	return result, nil
}

// Spaces lists all labels carried by a live instance or allocated address.
func Spaces(ctx context.Context, ec2 EC2, domain string) ([]Space, error) {
	hosts, ips, err := registry(ctx, ec2, domain)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	for name := range hosts {
		names[name] = true
	}
	for name := range ips {
		names[name] = true
	}
	domains := make([]string, 0, len(names))
	for name := range names {
		domains = append(domains, name)
	}
	sort.Strings(domains)
	result := make([]Space, 0, len(domains))
	for _, name := range domains {
		space, err := registeredSpace(name, hosts[name], ips[name])
		if err != nil {
			return nil, err
		}
		result = append(result, space)
	}
	return result, nil
}

// LookupSpace looks up only the requested label's instance and address.
func LookupSpace(ctx context.Context, ec2 EC2, domain, space string) (Space, error) {
	hosts, ips, err := registry(ctx, ec2, domain)
	if err != nil {
		return Space{}, err
	}
	return registeredSpace(space, hosts[space], ips[space])
}

// LookupInstance refuses an address-only space, which has no host to act on.
func LookupInstance(ctx context.Context, ec2 EC2, domain, space string) (Space, error) {
	result, err := LookupSpace(ctx, ec2, domain, space)
	if err != nil {
		return Space{}, err
	}
	if result.ID == "" {
		return Space{}, &NoInstanceError{Domain: space}
	}
	return result, nil
}
