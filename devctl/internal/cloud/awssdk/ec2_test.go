package awssdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type fakeEC2 struct {
	err error

	describeInstances func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	describeAddresses func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error)
	runInstances      func(*ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error)
	allocateAddress   func(*ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error)
	instanceStatus    *ec2.DescribeInstanceStatusOutput
}

func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if f.describeInstances != nil {
		return f.describeInstances(in)
	}
	return nil, f.err
}

func (f *fakeEC2) RunInstances(_ context.Context, in *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	if f.runInstances != nil {
		return f.runInstances(in)
	}
	return nil, f.err
}

func (f *fakeEC2) StartInstances(context.Context, *ec2.StartInstancesInput, ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error) {
	return nil, f.err
}

func (f *fakeEC2) StopInstances(context.Context, *ec2.StopInstancesInput, ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error) {
	return nil, f.err
}

func (f *fakeEC2) TerminateInstances(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	return nil, f.err
}

func (f *fakeEC2) DescribeInstanceStatus(context.Context, *ec2.DescribeInstanceStatusInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceStatusOutput, error) {
	if f.instanceStatus != nil {
		return f.instanceStatus, nil
	}
	return nil, f.err
}

func (f *fakeEC2) DescribeAddresses(_ context.Context, in *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	if f.describeAddresses != nil {
		return f.describeAddresses(in)
	}
	return nil, f.err
}

func (f *fakeEC2) AllocateAddress(_ context.Context, in *ec2.AllocateAddressInput, _ ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
	if f.allocateAddress != nil {
		return f.allocateAddress(in)
	}
	return nil, f.err
}

func (f *fakeEC2) AssociateAddress(context.Context, *ec2.AssociateAddressInput, ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error) {
	return nil, f.err
}

func (f *fakeEC2) DisassociateAddress(context.Context, *ec2.DisassociateAddressInput, ...func(*ec2.Options)) (*ec2.DisassociateAddressOutput, error) {
	return nil, f.err
}

func (f *fakeEC2) ReleaseAddress(context.Context, *ec2.ReleaseAddressInput, ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
	return nil, f.err
}

func TestEC2SpaceFilteringAndMapping(t *testing.T) {
	// R-YUMI-JOF0
	wantFilters := []types.Filter{
		{Name: aws.String("tag:Project"), Values: []string{"ikigenba"}},
		{Name: aws.String("tag-key"), Values: []string{"Space"}},
	}
	page := 0
	fake := &fakeEC2{}
	fake.describeInstances = func(in *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		if !reflect.DeepEqual(in.Filters, wantFilters) {
			t.Fatalf("filters = %#v, want %#v", in.Filters, wantFilters)
		}
		page++
		if page == 1 {
			if in.NextToken != nil {
				t.Fatalf("first next token = %q, want nil", aws.ToString(in.NextToken))
			}
			return &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{{Instances: []types.Instance{{
					InstanceId: aws.String("i-one"),
					State:      &types.InstanceState{Name: types.InstanceStateNameRunning},
					Tags:       []types.Tag{{Key: aws.String("Space"), Value: aws.String("one.example")}},
				}}}},
				NextToken: aws.String("next"),
			}, nil
		}
		if aws.ToString(in.NextToken) != "next" {
			t.Fatalf("second next token = %q, want next", aws.ToString(in.NextToken))
		}
		return &ec2.DescribeInstancesOutput{
			Reservations: []types.Reservation{{Instances: []types.Instance{{
				InstanceId:      aws.String("i-two"),
				PublicIpAddress: aws.String("192.0.2.2"),
				State:           &types.InstanceState{Name: types.InstanceStateNameStopped},
				Tags:            []types.Tag{{Key: aws.String("Space"), Value: aws.String("two.example")}},
			}}}},
		}, nil
	}
	fake.describeAddresses = func(in *ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error) {
		if !reflect.DeepEqual(in.Filters, wantFilters) {
			t.Fatalf("address filters = %#v, want %#v", in.Filters, wantFilters)
		}
		return &ec2.DescribeAddressesOutput{Addresses: []types.Address{{
			AllocationId: aws.String("eipalloc-one"),
			PublicIp:     aws.String("192.0.2.3"),
			Tags:         []types.Tag{{Key: aws.String("Space"), Value: aws.String("one.example")}},
		}}}, nil
	}

	client := &ec2Client{sdk: fake}
	instances, err := client.ListSpaceInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantInstances := []cloud.Instance{
		{ID: "i-one", Space: "one.example", State: cloud.StateRunning, Address: ""},
		{ID: "i-two", Space: "two.example", State: cloud.StateStopped, Address: "192.0.2.2"},
	}
	if !reflect.DeepEqual(instances, wantInstances) {
		t.Fatalf("instances = %#v, want %#v", instances, wantInstances)
	}
	addresses, err := client.ListSpaceAddresses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantAddresses := []cloud.Address{{AllocationID: "eipalloc-one", AssociationID: "", IP: "192.0.2.3", Space: "one.example"}}
	if !reflect.DeepEqual(addresses, wantAddresses) {
		t.Fatalf("addresses = %#v, want %#v", addresses, wantAddresses)
	}
}

func TestEC2CreationTagsAndRunMapping(t *testing.T) {
	// R-YTEM-5WOB R-YUMI-JOF0
	fake := &fakeEC2{}
	fake.runInstances = func(in *ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error) {
		if aws.ToInt32(in.MinCount) != 1 || aws.ToInt32(in.MaxCount) != 1 {
			t.Fatalf("counts = %d, %d; want 1, 1", aws.ToInt32(in.MinCount), aws.ToInt32(in.MaxCount))
		}
		if got := aws.ToString(in.LaunchTemplate.LaunchTemplateId); got != "lt-one" {
			t.Fatalf("launch template = %q, want lt-one", got)
		}
		if got := aws.ToString(in.IamInstanceProfile.Name); got != "profile-one" {
			t.Fatalf("instance profile = %q, want profile-one", got)
		}
		assertTagSpecifications(t, in.TagSpecifications, []types.ResourceType{types.ResourceTypeInstance, types.ResourceTypeVolume}, "one.example")
		return &ec2.RunInstancesOutput{Instances: []types.Instance{{
			InstanceId: aws.String("i-one"),
			State:      &types.InstanceState{Name: types.InstanceStateNamePending},
			Tags: []types.Tag{
				{Key: aws.String("Project"), Value: aws.String("ikigenba")},
				{Key: aws.String("Space"), Value: aws.String("one.example")},
			},
		}}}, nil
	}
	fake.allocateAddress = func(in *ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error) {
		if in.Domain != types.DomainTypeVpc {
			t.Fatalf("domain = %q, want vpc", in.Domain)
		}
		assertTagSpecifications(t, in.TagSpecifications, []types.ResourceType{types.ResourceTypeElasticIp}, "one.example")
		return &ec2.AllocateAddressOutput{AllocationId: aws.String("eipalloc-one"), PublicIp: aws.String("192.0.2.1")}, nil
	}
	client := &ec2Client{sdk: fake}
	instance, err := client.RunInstance(context.Background(), cloud.LaunchSpec{
		LaunchTemplateID: "lt-one", InstanceProfile: "profile-one", Space: "one.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := (cloud.Instance{ID: "i-one", Space: "one.example", State: cloud.StatePending}); instance != want {
		t.Fatalf("instance = %#v, want %#v", instance, want)
	}
	address, err := client.AllocateAddress(context.Background(), "one.example")
	if err != nil {
		t.Fatal(err)
	}
	if want := (cloud.Address{AllocationID: "eipalloc-one", IP: "192.0.2.1", Space: "one.example"}); address != want {
		t.Fatalf("address = %#v, want %#v", address, want)
	}
}

func TestEC2OperationMappingsAndChecks(t *testing.T) {
	// R-YTEM-5WOB
	boom := errors.New("boom")
	client := &ec2Client{sdk: &fakeEC2{err: boom}}
	tests := []struct {
		operation string
		call      func() error
	}{
		{"DescribeInstances", func() error { _, err := client.ListSpaceInstances(context.Background()); return err }},
		{"DescribeInstances", func() error { _, err := client.DescribeInstance(context.Background(), "i-one"); return err }},
		{"RunInstances", func() error { _, err := client.RunInstance(context.Background(), cloud.LaunchSpec{}); return err }},
		{"StartInstances", func() error { return client.StartInstance(context.Background(), "i-one") }},
		{"StopInstances", func() error { return client.StopInstance(context.Background(), "i-one") }},
		{"TerminateInstances", func() error { return client.TerminateInstance(context.Background(), "i-one") }},
		{"DescribeInstanceStatus", func() error { _, err := client.InstanceChecksPassed(context.Background(), "i-one"); return err }},
		{"DescribeAddresses", func() error { _, err := client.ListSpaceAddresses(context.Background()); return err }},
		{"AllocateAddress", func() error { _, err := client.AllocateAddress(context.Background(), "space"); return err }},
		{"AssociateAddress", func() error { return client.AssociateAddress(context.Background(), "allocation", "instance") }},
		{"DisassociateAddress", func() error { return client.DisassociateAddress(context.Background(), "association") }},
		{"ReleaseAddress", func() error { return client.ReleaseAddress(context.Background(), "allocation") }},
	}
	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			var got *cloud.Error
			if err := test.call(); !errors.As(err, &got) {
				t.Fatalf("error = %v, want *cloud.Error", err)
			}
			if got.Service != "ec2" || got.Operation != test.operation || got.Subject != "" || !errors.Is(got, boom) {
				t.Fatalf("error = %#v, want ec2 %s with no subject wrapping boom", got, test.operation)
			}
		})
	}

	statusClient := func(instance, system types.SummaryStatus) *ec2Client {
		return &ec2Client{sdk: &fakeEC2{instanceStatus: &ec2.DescribeInstanceStatusOutput{InstanceStatuses: []types.InstanceStatus{{
			InstanceStatus: &types.InstanceStatusSummary{Status: instance},
			SystemStatus:   &types.InstanceStatusSummary{Status: system},
		}}}}}
	}
	for _, test := range []struct {
		instance types.SummaryStatus
		system   types.SummaryStatus
		want     bool
	}{
		{types.SummaryStatusOk, types.SummaryStatusOk, true},
		{types.SummaryStatusImpaired, types.SummaryStatusOk, false},
		{types.SummaryStatusOk, types.SummaryStatusImpaired, false},
	} {
		got, err := statusClient(test.instance, test.system).InstanceChecksPassed(context.Background(), "i-one")
		if err != nil || got != test.want {
			t.Fatalf("checks(%q, %q) = %v, %v; want %v, nil", test.instance, test.system, got, err, test.want)
		}
	}
}

func assertTagSpecifications(t *testing.T, got []types.TagSpecification, resourceTypes []types.ResourceType, space string) {
	t.Helper()
	if len(got) != len(resourceTypes) {
		t.Fatalf("tag specifications = %#v, want %d", got, len(resourceTypes))
	}
	wantTags := []types.Tag{
		{Key: aws.String("Project"), Value: aws.String("ikigenba")},
		{Key: aws.String("Space"), Value: aws.String(space)},
	}
	for i, resourceType := range resourceTypes {
		if got[i].ResourceType != resourceType || !reflect.DeepEqual(got[i].Tags, wantTags) {
			t.Fatalf("tag specification %d = %#v, want %q %#v", i, got[i], resourceType, wantTags)
		}
	}
}
