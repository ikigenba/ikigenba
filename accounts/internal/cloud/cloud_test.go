package cloud

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestInterfaceContracts(t *testing.T) {
	// R-WKRR-39LH R-KZMA-AN7S R-WN7J-UT2V R-WOFG-8KTK
	type ec2Contract interface {
		RunInstance(context.Context, Launch) (Instance, error)
		DescribeInstance(context.Context, string) (Instance, error)
		FindInstances(context.Context, Tags, func([]Instance) bool) error
		StopInstance(context.Context, string) error
		StartInstance(context.Context, string) error
		TerminateInstance(context.Context, string) error
		SetInstanceType(context.Context, string, string) error
		AllocateAddress(context.Context, Tags) (Address, error)
		DescribeAddress(context.Context, string) (Address, error)
		FindAddresses(context.Context, Tags, func([]Address) bool) error
		AssociateAddress(context.Context, string, string) (string, error)
		DisassociateAddress(context.Context, string) error
		ReleaseAddress(context.Context, string) error
		CreateVolume(context.Context, VolumeSpec) (Volume, error)
		DescribeVolume(context.Context, string) (Volume, error)
		FindVolumes(context.Context, Tags, func([]Volume) bool) error
		AttachVolume(context.Context, string, string, string) error
		ModifyVolume(context.Context, string, int32) error
		VolumeModification(context.Context, string) (Modification, error)
		DeleteVolume(context.Context, string) error
		TagResource(context.Context, string, Tags) error
	}
	type dnsContract interface {
		FindRecords(context.Context, string, string) ([]Record, error)
		ChangeRecord(context.Context, string, RecordChange) (string, error)
		ChangeInsync(context.Context, string) (bool, error)
	}
	type stsContract interface {
		Account(context.Context) (string, error)
		CallerOf(context.Context, string, string) (Caller, error)
	}
	type cloudContract interface {
		ec2Contract
		dnsContract
		stsContract
	}
	type presignContract interface {
		PresignCallerIdentity(context.Context, string, time.Duration) (string, error)
	}
	type metadataContract interface {
		Instance(context.Context) (InstanceFacts, error)
	}

	// Assignments in both directions require matching method sets.
	checkEC2 := func(c ec2Contract) EC2 { return c }
	checkDNS := func(c dnsContract) Route53 { return c }
	checkSTS := func(c stsContract) STS { return c }
	checkCloud := func(c cloudContract) Cloud { return c }
	checkPresigner := func(c presignContract) Presigner { return c }
	checkMetadata := func(c metadataContract) Metadata { return c }
	_ = func(c EC2) ec2Contract { return c }
	_ = func(c Route53) dnsContract { return c }
	_ = func(c STS) stsContract { return c }
	_ = func(c Cloud) cloudContract { return c }
	_ = func(c Presigner) presignContract { return c }
	_ = func(c Metadata) metadataContract { return c }
	var open Opener = func(_ context.Context, region string) (Cloud, error) {
		return nil, errors.New(region)
	}
	c, err := open(t.Context(), "example-region")
	if err == nil || err.Error() != "example-region" || checkCloud(c) != nil {
		t.Fatalf("opener returned %v, %v", c, err)
	}
	if checkEC2(c) != nil || checkDNS(c) != nil || checkSTS(c) != nil {
		t.Fatal("the cloud zero value should provide nil operation interfaces")
	}
	var p Presigner
	var m Metadata
	if checkPresigner(p) != nil || checkMetadata(m) != nil {
		t.Fatal("the host interface zero values should be nil")
	}
}

func TestDataShapes(t *testing.T) {
	// Exact, named field shapes for R-WPNC-MCK9 R-WQV9-04AY R-WS35-DW1N R-WUIY-5FJ1.
	tags := Tags(map[string]string{"test": "value"})
	_ = struct {
		Template, InstanceProfile, Type string
		RootGB                          int32
		ClientToken                     string
		Tags, InstanceTags              Tags
	}(Launch{"template", "profile", "t4g.small", 12, "token", tags, nil})
	_ = struct {
		ID, State, Type, AZ, RootVolume string
		Tags                            Tags
	}(Instance{"i-test", InstancePending, "t4g.small", "az", "vol-root", tags})
	_ = struct {
		AllocationID, AssociationID, InstanceID, IP string
		Tags                                        Tags
	}(Address{"eip-test", "assoc-test", "i-test", "192.0.2.1", tags})
	_ = struct {
		AZ          string
		SizeGB      int32
		ClientToken string
		Tags        Tags
	}(VolumeSpec{"az", 32, "token", tags})
	attachment := Attachment{"i-test", "/dev/sdf", AttachmentAttaching}
	_ = struct{ InstanceID, Device, State string }(attachment)
	_ = struct {
		ID, State, AZ string
		SizeGB        int32
		Attachment    Attachment
		Tags          Tags
	}(Volume{"vol-data", VolumeInUse, "az", 32, attachment, tags})
	_ = struct {
		State    string
		TargetGB int32
	}(Modification{ModificationModifying, 64})
	_ = struct {
		Name, Type string
		Values     []string
		TTL        int64
	}(Record{"test.example", "A", []string{"192.0.2.1"}, 12})
	_ = struct {
		Action, Name, IP string
		TTL              int64
	}(RecordChange{RecordCreate, "test.example", "192.0.2.1", 12})
	_ = struct{ Account, Arn, UserID string }(Caller{"account", "arn", "user"})
	_ = struct{ InstanceID, Type, AMI, Region string }(InstanceFacts{"i-test", "t4g.small", "ami-test", "region"})
	if tags["test"] != "value" {
		t.Fatal("tags did not preserve the supplied pair")
	}
}

func TestConstants(t *testing.T) {
	type literal string
	tests := []struct {
		got, want []literal
	}{
		// R-WQV9-04AY
		{[]literal{InstancePending, InstanceRunning, InstanceShuttingDown, InstanceTerminated, InstanceStopping, InstanceStopped},
			[]literal{"pending", "running", "shutting-down", "terminated", "stopping", "stopped"}},
		// R-WTB1-RNSC
		{[]literal{VolumeCreating, VolumeAvailable, VolumeInUse, VolumeDeleting, VolumeDeleted, VolumeError,
			AttachmentAttaching, AttachmentAttached, AttachmentDetaching, AttachmentDetached, AttachmentBusy,
			ModificationModifying, ModificationOptimizing, ModificationCompleted, ModificationFailed},
			[]literal{"creating", "available", "in-use", "deleting", "deleted", "error", "attaching", "attached", "detaching", "detached", "busy", "modifying", "optimizing", "completed", "failed"}},
		// R-WUIY-5FJ1
		{[]literal{RecordCreate, RecordDelete}, []literal{"CREATE", "DELETE"}},
		// R-WVQU-J79Q
		{[]literal{TagKind, TagHost, TagOperation, TagAccountsURL, TagOrphaned, KindHost, KindHostData, OrphanedTrue},
			[]literal{"Kind", "Host", "Operation", "AccountsURL", "Orphaned", "host", "host-data", "true"}},
		// R-WY6N-AQR4
		{[]literal{CodeNoResponse, CodeTagContract, CodeBadResponse, BindingHeader}, []literal{"NoResponse", "TagContract", "BadResponse", "X-Ikigenba-Accounts"}},
	}
	for _, test := range tests {
		if !reflect.DeepEqual(test.got, test.want) {
			t.Errorf("constants = %v, want %v", test.got, test.want)
		}
	}
	if CallerTimeout != 10*time.Second || MaxCallerBytes != 64<<10 {
		t.Errorf("caller limits = %v and %v", CallerTimeout, MaxCallerBytes)
	}
}

func TestErrors(t *testing.T) {
	// R-WWYQ-WZ0F R-X0MG-2A8I R-X1UC-G1Z7
	cause := errors.New("a URL and a secret")
	e := Error{"ec2", "DescribeInstances", "i-test", "Missing", ClassNotFound, cause}
	_ = struct {
		Service, Operation, Subject, Code string
		Class                             Class
		Err                               error
	}(e)
	if e.Error() != "ec2 DescribeInstances i-test: Missing" || !errors.Is(&e, cause) {
		t.Errorf("error text or unwrap: %v", &e)
	}
	e.Subject = ""
	if e.Error() != "ec2 DescribeInstances: Missing" {
		t.Errorf("error without subject: %v", &e)
	}
	var zero Class
	classes := map[Class]bool{ClassRefused: true, ClassNotFound: true, ClassMismatch: true, ClassTransient: true}
	if ClassRefused != zero || len(classes) != 4 {
		t.Errorf("invalid classes: %v", classes)
	}
	if ClassOf(fmt.Errorf("outer: %w", &e)) != ClassNotFound ||
		ClassOf(&Error{Class: ClassMismatch, Err: &e}) != ClassMismatch ||
		ClassOf(context.DeadlineExceeded) != ClassTransient {
		t.Fatal("ClassOf returned a class other than that of the first cloud error, or transient for a bare error")
	}
}
