package awssdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type fakeEC2 struct {
	err error

	calls []string

	describeInstances func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	describeAddresses func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error)
	runInstances      func(*ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error)
	allocateAddress   func(*ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error)
	instanceStatus    *ec2.DescribeInstanceStatusOutput
}

func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	f.calls = append(f.calls, "DescribeInstances")
	if f.describeInstances != nil {
		return f.describeInstances(in)
	}
	return nil, f.err
}

func (f *fakeEC2) RunInstances(_ context.Context, in *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	f.calls = append(f.calls, "RunInstances")
	if f.runInstances != nil {
		return f.runInstances(in)
	}
	return nil, f.err
}

func (f *fakeEC2) StartInstances(context.Context, *ec2.StartInstancesInput, ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error) {
	f.calls = append(f.calls, "StartInstances")
	return nil, f.err
}

func (f *fakeEC2) StopInstances(context.Context, *ec2.StopInstancesInput, ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error) {
	f.calls = append(f.calls, "StopInstances")
	return nil, f.err
}

func (f *fakeEC2) TerminateInstances(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	f.calls = append(f.calls, "TerminateInstances")
	return nil, f.err
}

func (f *fakeEC2) DescribeInstanceStatus(context.Context, *ec2.DescribeInstanceStatusInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceStatusOutput, error) {
	f.calls = append(f.calls, "DescribeInstanceStatus")
	if f.instanceStatus != nil {
		return f.instanceStatus, nil
	}
	return nil, f.err
}

func (f *fakeEC2) DescribeAddresses(_ context.Context, in *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	f.calls = append(f.calls, "DescribeAddresses")
	if f.describeAddresses != nil {
		return f.describeAddresses(in)
	}
	return nil, f.err
}

func (f *fakeEC2) AllocateAddress(_ context.Context, in *ec2.AllocateAddressInput, _ ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
	f.calls = append(f.calls, "AllocateAddress")
	if f.allocateAddress != nil {
		return f.allocateAddress(in)
	}
	return nil, f.err
}

func (f *fakeEC2) AssociateAddress(context.Context, *ec2.AssociateAddressInput, ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error) {
	f.calls = append(f.calls, "AssociateAddress")
	return nil, f.err
}

func (f *fakeEC2) DisassociateAddress(context.Context, *ec2.DisassociateAddressInput, ...func(*ec2.Options)) (*ec2.DisassociateAddressOutput, error) {
	f.calls = append(f.calls, "DisassociateAddress")
	return nil, f.err
}

func (f *fakeEC2) ReleaseAddress(context.Context, *ec2.ReleaseAddressInput, ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
	f.calls = append(f.calls, "ReleaseAddress")
	return nil, f.err
}

func TestEC2SpaceFilteringAndMapping(t *testing.T) {
	// R-YTEM-5WOB R-YUMI-JOF0
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
	if want := []string{"RunInstances"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("SDK calls after RunInstance = %v, want %v", fake.calls, want)
	}
	fake.calls = nil
	address, err := client.AllocateAddress(context.Background(), "one.example")
	if err != nil {
		t.Fatal(err)
	}
	if want := (cloud.Address{AllocationID: "eipalloc-one", IP: "192.0.2.1", Space: "one.example"}); address != want {
		t.Fatalf("address = %#v, want %#v", address, want)
	}
	if want := []string{"AllocateAddress"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("SDK calls after AllocateAddress = %v, want %v", fake.calls, want)
	}
}

func TestEC2OperationMappingsAndChecks(t *testing.T) {
	// R-YTEM-5WOB R-YOJ0-MTPJ R-YPQX-0LG8
	boom := &smithy.GenericAPIError{Code: "RequestLimitExceeded", Message: "boom"}
	tests := []struct {
		method    string
		operation string
		call      func(*ec2Client) error
	}{
		{"ListSpaceInstances", "DescribeInstances", func(client *ec2Client) error { _, err := client.ListSpaceInstances(context.Background()); return err }},
		{"DescribeInstance", "DescribeInstances", func(client *ec2Client) error {
			_, err := client.DescribeInstance(context.Background(), "i-one")
			return err
		}},
		{"RunInstance", "RunInstances", func(client *ec2Client) error {
			_, err := client.RunInstance(context.Background(), cloud.LaunchSpec{})
			return err
		}},
		{"StartInstance", "StartInstances", func(client *ec2Client) error { return client.StartInstance(context.Background(), "i-one") }},
		{"StopInstance", "StopInstances", func(client *ec2Client) error { return client.StopInstance(context.Background(), "i-one") }},
		{"TerminateInstance", "TerminateInstances", func(client *ec2Client) error { return client.TerminateInstance(context.Background(), "i-one") }},
		{"InstanceChecksPassed", "DescribeInstanceStatus", func(client *ec2Client) error {
			_, err := client.InstanceChecksPassed(context.Background(), "i-one")
			return err
		}},
		{"ListSpaceAddresses", "DescribeAddresses", func(client *ec2Client) error { _, err := client.ListSpaceAddresses(context.Background()); return err }},
		{"AllocateAddress", "AllocateAddress", func(client *ec2Client) error {
			_, err := client.AllocateAddress(context.Background(), "space")
			return err
		}},
		{"AssociateAddress", "AssociateAddress", func(client *ec2Client) error {
			return client.AssociateAddress(context.Background(), "allocation", "instance")
		}},
		{"DisassociateAddress", "DisassociateAddress", func(client *ec2Client) error { return client.DisassociateAddress(context.Background(), "association") }},
		{"ReleaseAddress", "ReleaseAddress", func(client *ec2Client) error { return client.ReleaseAddress(context.Background(), "allocation") }},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			fake := &fakeEC2{err: boom}
			client := &ec2Client{sdk: fake}
			var got *cloud.Error
			if err := test.call(client); !errors.As(err, &got) {
				t.Fatalf("error = %v, want *cloud.Error", err)
			}
			if got.Service != "ec2" || got.Operation != test.operation || got.Subject != "" ||
				got.Code != boom.ErrorCode() || !errors.Is(got, boom) {
				t.Fatalf("error = %#v, want ec2 %s with no subject and code %s wrapping boom", got, test.operation, boom.ErrorCode())
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
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

func TestEC2MutationExactDispatchAfterSuccess(t *testing.T) {
	// R-YPQX-0LG8
	tests := []struct {
		method    string
		operation string
		call      func(*ec2Client) error
	}{
		{"StartInstance", "StartInstances", func(client *ec2Client) error {
			return client.StartInstance(context.Background(), "i-one")
		}},
		{"StopInstance", "StopInstances", func(client *ec2Client) error {
			return client.StopInstance(context.Background(), "i-one")
		}},
		{"TerminateInstance", "TerminateInstances", func(client *ec2Client) error {
			return client.TerminateInstance(context.Background(), "i-one")
		}},
		{"AssociateAddress", "AssociateAddress", func(client *ec2Client) error {
			return client.AssociateAddress(context.Background(), "allocation", "instance")
		}},
		{"DisassociateAddress", "DisassociateAddress", func(client *ec2Client) error {
			return client.DisassociateAddress(context.Background(), "association")
		}},
		{"ReleaseAddress", "ReleaseAddress", func(client *ec2Client) error {
			return client.ReleaseAddress(context.Background(), "allocation")
		}},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			fake := &fakeEC2{}
			if err := test.call(&ec2Client{sdk: fake}); err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}
}

func TestEC2ReadExactDispatchAfterSuccess(t *testing.T) {
	// R-YPQX-0LG8
	tests := []struct {
		method    string
		operation string
		prepare   func(*fakeEC2)
		call      func(*ec2Client) error
	}{
		{"ListSpaceInstances", "DescribeInstances", func(fake *fakeEC2) {
			fake.describeInstances = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{}, nil
			}
		}, func(client *ec2Client) error {
			_, err := client.ListSpaceInstances(context.Background())
			return err
		}},
		{"ListSpaceAddresses", "DescribeAddresses", func(fake *fakeEC2) {
			fake.describeAddresses = func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error) {
				return &ec2.DescribeAddressesOutput{}, nil
			}
		}, func(client *ec2Client) error {
			_, err := client.ListSpaceAddresses(context.Background())
			return err
		}},
		{"DescribeInstance", "DescribeInstances", func(fake *fakeEC2) {
			fake.describeInstances = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{}, nil
			}
		}, func(client *ec2Client) error {
			_, err := client.DescribeInstance(context.Background(), "i-one")
			return err
		}},
		{"InstanceChecksPassed", "DescribeInstanceStatus", func(fake *fakeEC2) {
			fake.instanceStatus = &ec2.DescribeInstanceStatusOutput{}
		}, func(client *ec2Client) error {
			_, err := client.InstanceChecksPassed(context.Background(), "i-one")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			fake := &fakeEC2{}
			test.prepare(fake)
			if err := test.call(&ec2Client{sdk: fake}); err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
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
