// Package cloud defines the cloud operations accounts and accountsd use.
package cloud

import (
	"context"
	"errors"
	"time"
)

// Opener opens a cloud for the selected region.
type Opener func(ctx context.Context, region string) (Cloud, error)

// Cloud is the set of infrastructure operations accounts uses.
type Cloud interface {
	EC2
	Route53
	STS
}

// EC2 manages hosts, Elastic IP addresses, and EBS volumes.
type EC2 interface {
	RunInstance(ctx context.Context, l Launch) (Instance, error)
	DescribeInstance(ctx context.Context, id string) (Instance, error)
	FindInstances(ctx context.Context, tags Tags, each func([]Instance) bool) error
	StopInstance(ctx context.Context, id string) error
	StartInstance(ctx context.Context, id string) error
	TerminateInstance(ctx context.Context, id string) error
	SetInstanceType(ctx context.Context, id, typ string) error
	AllocateAddress(ctx context.Context, tags Tags) (Address, error)
	DescribeAddress(ctx context.Context, allocationID string) (Address, error)
	FindAddresses(ctx context.Context, tags Tags, each func([]Address) bool) error
	AssociateAddress(ctx context.Context, allocationID, instanceID string) (string, error)
	DisassociateAddress(ctx context.Context, associationID string) error
	ReleaseAddress(ctx context.Context, allocationID string) error
	CreateVolume(ctx context.Context, s VolumeSpec) (Volume, error)
	DescribeVolume(ctx context.Context, id string) (Volume, error)
	FindVolumes(ctx context.Context, tags Tags, each func([]Volume) bool) error
	AttachVolume(ctx context.Context, volumeID, instanceID, device string) error
	ModifyVolume(ctx context.Context, volumeID string, sizeGB int32) error
	VolumeModification(ctx context.Context, volumeID string) (Modification, error)
	DeleteVolume(ctx context.Context, id string) error
	TagResource(ctx context.Context, id string, tags Tags) error
}

// Route53 manages the DNS records of hosted accounts.
type Route53 interface {
	FindRecords(ctx context.Context, zoneID, name string) ([]Record, error)
	ChangeRecord(ctx context.Context, zoneID string, c RecordChange) (string, error)
	ChangeInsync(ctx context.Context, changeID string) (bool, error)
}

// STS identifies accounts and the hosts presenting signed requests.
type STS interface {
	Account(ctx context.Context) (string, error)
	CallerOf(ctx context.Context, presignedURL, binding string) (Caller, error)
}

// Presigner signs a host's identity request for accounts to execute.
type Presigner interface {
	PresignCallerIdentity(ctx context.Context, binding string, expires time.Duration) (string, error)
}

// Metadata reads the host's instance identity document.
type Metadata interface {
	Instance(ctx context.Context) (InstanceFacts, error)
}

// Tags holds resource tags by key.
type Tags map[string]string

// Launch describes the EC2 resources a host launch creates.
type Launch struct {
	Template, InstanceProfile, Type string
	RootGB                          int32
	ClientToken                     string
	Tags, InstanceTags              Tags
}

// Instance describes an EC2 host and its root volume.
type Instance struct {
	ID, State, Type, AZ, RootVolume string
	Tags                            Tags
}

// Instance state names.
const (
	InstancePending      = "pending"
	InstanceRunning      = "running"
	InstanceShuttingDown = "shutting-down"
	InstanceTerminated   = "terminated"
	InstanceStopping     = "stopping"
	InstanceStopped      = "stopped"
)

// Address describes an Elastic IP and any current association.
type Address struct {
	AllocationID, AssociationID, InstanceID, IP string
	Tags                                        Tags
}

// VolumeSpec describes a data volume to create.
type VolumeSpec struct {
	AZ          string
	SizeGB      int32
	ClientToken string
	Tags        Tags
}

// Volume describes an EBS volume and its first attachment.
type Volume struct {
	ID, State, AZ string
	SizeGB        int32
	Attachment    Attachment
	Tags          Tags
}

// Attachment describes an EBS volume's attachment to a host.
type Attachment struct {
	InstanceID, Device, State string
}

// Modification describes the most recent change to a volume's size.
type Modification struct {
	State    string
	TargetGB int32
}

// Volume, attachment, and modification state names.
const (
	VolumeCreating         = "creating"
	VolumeAvailable        = "available"
	VolumeInUse            = "in-use"
	VolumeDeleting         = "deleting"
	VolumeDeleted          = "deleted"
	VolumeError            = "error"
	AttachmentAttaching    = "attaching"
	AttachmentAttached     = "attached"
	AttachmentDetaching    = "detaching"
	AttachmentDetached     = "detached"
	AttachmentBusy         = "busy"
	ModificationModifying  = "modifying"
	ModificationOptimizing = "optimizing"
	ModificationCompleted  = "completed"
	ModificationFailed     = "failed"
)

// Record describes a DNS record set at an account's name.
type Record struct {
	Name, Type string
	Values     []string
	TTL        int64
}

// RecordChange creates or deletes an A record for an account.
type RecordChange struct {
	Action, Name, IP string
	TTL              int64
}

// Caller identifies the principal of a verified STS request.
type Caller struct {
	Account, Arn, UserID string
}

// InstanceFacts are the fields accountsd reads from IMDS.
type InstanceFacts struct {
	InstanceID, Type, AMI, Region string
}

// DNS change actions, resource tags, adapter codes, and STS limits.
const (
	RecordCreate = "CREATE"
	RecordDelete = "DELETE"

	TagKind        = "Kind"
	TagHost        = "Host"
	TagOperation   = "Operation"
	TagAccountsURL = "AccountsURL"
	TagOrphaned    = "Orphaned"
	KindHost       = "host"
	KindHostData   = "host-data"
	OrphanedTrue   = "true"

	CodeNoResponse  = "NoResponse"
	CodeTagContract = "TagContract"
	CodeBadResponse = "BadResponse"
	BindingHeader   = "X-Ikigenba-Accounts"
	CallerTimeout   = 10 * time.Second
	MaxCallerBytes  = 64 << 10
)

// Class tells the engine how to handle a failed cloud operation.
type Class int

// The classes the engine distinguishes.
const (
	ClassRefused Class = iota
	ClassNotFound
	ClassMismatch
	ClassTransient
)

// Error describes a cloud operation failure and exposes its underlying cause.
type Error struct {
	Service, Operation, Subject, Code string
	Class                             Class
	Err                               error
}

func (e *Error) Error() string {
	operation := e.Service + " " + e.Operation
	if e.Subject != "" {
		operation += " " + e.Subject
	}
	return operation + ": " + e.Code
}

func (e *Error) Unwrap() error {
	return e.Err
}

// ClassOf finds the cloud error's class, treating an unknown error as transient.
func ClassOf(err error) Class {
	var cloudErr *Error
	if errors.As(err, &cloudErr) {
		return cloudErr.Class
	}
	return ClassTransient
}
