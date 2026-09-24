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

	describeInstances       func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	describeAddresses       func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error)
	runInstances            func(*ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error)
	allocateAddress         func(*ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error)
	instanceStatus          *ec2.DescribeInstanceStatusOutput
	describeLaunchTemplates func(*ec2.DescribeLaunchTemplatesInput) (*ec2.DescribeLaunchTemplatesOutput, error)
}

func (f *fakeEC2) DescribeLaunchTemplates(_ context.Context, in *ec2.DescribeLaunchTemplatesInput, _ ...func(*ec2.Options)) (*ec2.DescribeLaunchTemplatesOutput, error) {
	f.calls = append(f.calls, "DescribeLaunchTemplates")
	if f.describeLaunchTemplates != nil {
		return f.describeLaunchTemplates(in)
	}
	return nil, f.err
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

func TestEC2LaunchTemplateLookup(t *testing.T) {
	// R-QOTI-JOVC R-QMDP-S5DY
	fake := &fakeEC2{describeLaunchTemplates: func(in *ec2.DescribeLaunchTemplatesInput) (*ec2.DescribeLaunchTemplatesOutput, error) {
		want := []types.Filter{{Name: aws.String("launch-template-name"), Values: []string{"root.example"}}}
		if !reflect.DeepEqual(in.Filters, want) {
			t.Fatalf("filters = %#v, want %#v", in.Filters, want)
		}
		return &ec2.DescribeLaunchTemplatesOutput{LaunchTemplates: []types.LaunchTemplate{{LaunchTemplateId: aws.String("lt-123")}}}, nil
	}}
	id, err := (&ec2Client{sdk: fake}).LaunchTemplate(context.Background(), "root.example")
	if err != nil || id != "lt-123" {
		t.Fatalf("LaunchTemplate = %q, %v; want lt-123, nil", id, err)
	}
	if want := []string{"DescribeLaunchTemplates"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}

	missing := &fakeEC2{describeLaunchTemplates: func(*ec2.DescribeLaunchTemplatesInput) (*ec2.DescribeLaunchTemplatesOutput, error) {
		return &ec2.DescribeLaunchTemplatesOutput{}, nil
	}}
	_, err = (&ec2Client{sdk: missing}).LaunchTemplate(context.Background(), "missing")
	var notFound *cloud.NotFoundError
	if !errors.As(err, &notFound) || notFound.Kind != "launch template" || notFound.Name != "missing" {
		t.Fatalf("missing error = %#v", err)
	}
}

func TestEC2SpaceFilteringAndMapping(t *testing.T) {
	// R-QMDP-S5DY R-QQ1E-XGM1
	wantFilters := []types.Filter{
		{Name: aws.String("tag:Domain"), Values: []string{"root.example"}},
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
	instances, err := client.ListSpaceInstances(context.Background(), "root.example")
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
	addresses, err := client.ListSpaceAddresses(context.Background(), "root.example")
	if err != nil {
		t.Fatal(err)
	}
	wantAddresses := []cloud.Address{{AllocationID: "eipalloc-one", AssociationID: "", IP: "192.0.2.3", Space: "one.example"}}
	if !reflect.DeepEqual(addresses, wantAddresses) {
		t.Fatalf("addresses = %#v, want %#v", addresses, wantAddresses)
	}
}

func TestEC2CreationTagsAndRunMapping(t *testing.T) {
	// R-QMDP-S5DY R-QQ1E-XGM1
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
		assertTagSpecifications(t, in.TagSpecifications, []types.ResourceType{types.ResourceTypeInstance, types.ResourceTypeVolume}, "root.example", "one.example")
		return &ec2.RunInstancesOutput{Instances: []types.Instance{{
			InstanceId: aws.String("i-one"),
			State:      &types.InstanceState{Name: types.InstanceStateNamePending},
			Tags: []types.Tag{
				{Key: aws.String("Domain"), Value: aws.String("root.example")},
				{Key: aws.String("Space"), Value: aws.String("one.example")},
			},
		}}}, nil
	}
	fake.allocateAddress = func(in *ec2.AllocateAddressInput) (*ec2.AllocateAddressOutput, error) {
		if in.Domain != types.DomainTypeVpc {
			t.Fatalf("domain = %q, want vpc", in.Domain)
		}
		assertTagSpecifications(t, in.TagSpecifications, []types.ResourceType{types.ResourceTypeElasticIp}, "root.example", "one.example")
		return &ec2.AllocateAddressOutput{AllocationId: aws.String("eipalloc-one"), PublicIp: aws.String("192.0.2.1")}, nil
	}
	client := &ec2Client{sdk: fake}
	instance, err := client.RunInstance(context.Background(), cloud.LaunchSpec{
		LaunchTemplateID: "lt-one", InstanceProfile: "profile-one", Domain: "root.example", Space: "one.example",
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
	address, err := client.AllocateAddress(context.Background(), "root.example", "one.example")
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

func TestEC2LaunchReady(t *testing.T) {
	// R-H32Z-PBQW
	spec := cloud.LaunchSpec{LaunchTemplateID: "lt-one", InstanceProfile: "profile-one", Space: "one.example"}
	tests := []struct {
		name    string
		err     error
		want    bool
		wantErr bool
	}{
		{"ready", &smithy.GenericAPIError{Code: "DryRunOperation", Message: "would succeed"}, true, false},
		{"profile not ready", &smithy.GenericAPIError{Code: "InvalidParameterValue", Message: "Invalid IAM Instance Profile name profile-one"}, false, false},
		{"other invalid parameter", &smithy.GenericAPIError{Code: "InvalidParameterValue", Message: "bad launch template"}, false, true},
		{"other error", &smithy.GenericAPIError{Code: "UnauthorizedOperation", Message: "denied"}, false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeEC2{err: test.err}
			got, err := (&ec2Client{sdk: fake}).LaunchReady(context.Background(), spec)
			if got != test.want {
				t.Fatalf("LaunchReady() = %v, want %v", got, test.want)
			}
			if test.wantErr {
				var cloudErr *cloud.Error
				if !errors.As(err, &cloudErr) || cloudErr.Service != "ec2" || cloudErr.Operation != "RunInstances" || !errors.Is(cloudErr, test.err) {
					t.Fatalf("error = %#v, want ec2 RunInstances error wrapping %v", err, test.err)
				}
			} else if err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if want := []string{"RunInstances"}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}

	fake := &fakeEC2{runInstances: func(in *ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error) {
		if !aws.ToBool(in.DryRun) {
			t.Fatal("dry-run = false, want true")
		}
		if aws.ToString(in.LaunchTemplate.LaunchTemplateId) != spec.LaunchTemplateID ||
			aws.ToString(in.IamInstanceProfile.Name) != spec.InstanceProfile {
			t.Fatalf("launch input = %#v, want template %q and profile %q", in, spec.LaunchTemplateID, spec.InstanceProfile)
		}
		if aws.ToInt32(in.MinCount) != 1 || aws.ToInt32(in.MaxCount) != 1 {
			t.Fatalf("counts = %d, %d; want 1, 1", aws.ToInt32(in.MinCount), aws.ToInt32(in.MaxCount))
		}
		assertTagSpecifications(t, in.TagSpecifications, []types.ResourceType{types.ResourceTypeInstance, types.ResourceTypeVolume}, spec.Domain, spec.Space)
		return nil, &smithy.GenericAPIError{Code: "DryRunOperation", Message: "would succeed"}
	}}
	if ready, err := (&ec2Client{sdk: fake}).LaunchReady(context.Background(), spec); !ready || err != nil {
		t.Fatalf("LaunchReady() = %v, %v; want true, nil", ready, err)
	}
}

func TestEC2OperationMappingsAndChecks(t *testing.T) {
	// R-QMDP-S5DY R-VV8S-ZVE7 R-YPQX-0LG8
	boom := &smithy.GenericAPIError{Code: "RequestLimitExceeded", Message: "boom"}
	tests := []struct {
		method    string
		operation string
		call      func(*ec2Client) error
	}{
		{"LaunchTemplate", "DescribeLaunchTemplates", func(client *ec2Client) error {
			_, err := client.LaunchTemplate(context.Background(), "name")
			return err
		}},
		{"ListSpaceInstances", "DescribeInstances", func(client *ec2Client) error {
			_, err := client.ListSpaceInstances(context.Background(), "domain")
			return err
		}},
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
		{"ListSpaceAddresses", "DescribeAddresses", func(client *ec2Client) error {
			_, err := client.ListSpaceAddresses(context.Background(), "domain")
			return err
		}},
		{"AllocateAddress", "AllocateAddress", func(client *ec2Client) error {
			_, err := client.AllocateAddress(context.Background(), "domain", "space")
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
			_, err := client.ListSpaceInstances(context.Background(), "domain")
			return err
		}},
		{"ListSpaceAddresses", "DescribeAddresses", func(fake *fakeEC2) {
			fake.describeAddresses = func(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error) {
				return &ec2.DescribeAddressesOutput{}, nil
			}
		}, func(client *ec2Client) error {
			_, err := client.ListSpaceAddresses(context.Background(), "domain")
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

func TestDescribeInstanceNotYetVisible(t *testing.T) {
	// R-5LKS-2NVC
	const id = "i-absent"
	wantInput := &ec2.DescribeInstancesInput{InstanceIds: []string{id}}
	clientFor := func(t *testing.T, respond func() (*ec2.DescribeInstancesOutput, error)) *ec2Client {
		t.Helper()
		fake := &fakeEC2{describeInstances: func(in *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			if !reflect.DeepEqual(in, wantInput) {
				t.Fatalf("request = %#v, want %#v", in, wantInput)
			}
			return respond()
		}}
		return &ec2Client{sdk: fake}
	}

	t.Run("no instance", func(t *testing.T) {
		got, err := clientFor(t, func() (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{}, nil
		}).DescribeInstance(context.Background(), id)
		if err != nil || got != (cloud.Instance{}) {
			t.Fatalf("DescribeInstance = %#v, %v; want zero, nil", got, err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		notFound := &smithy.GenericAPIError{Code: "InvalidInstanceID.NotFound", Message: "missing"}
		var api smithy.APIError = notFound
		if api.ErrorCode() != "InvalidInstanceID.NotFound" {
			t.Fatalf("ErrorCode = %q", api.ErrorCode())
		}
		got, err := clientFor(t, func() (*ec2.DescribeInstancesOutput, error) {
			return nil, notFound
		}).DescribeInstance(context.Background(), id)
		if err != nil || got != (cloud.Instance{}) {
			t.Fatalf("DescribeInstance = %#v, %v; want zero, nil", got, err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		denied := &smithy.GenericAPIError{Code: "UnauthorizedOperation", Message: "denied"}
		_, err := clientFor(t, func() (*ec2.DescribeInstancesOutput, error) {
			return nil, denied
		}).DescribeInstance(context.Background(), id)
		var cloudErr *cloud.Error
		if !errors.As(err, &cloudErr) || cloudErr.Operation != "DescribeInstances" || cloudErr.Code != "UnauthorizedOperation" {
			t.Fatalf("error = %#v, want *cloud.Error DescribeInstances UnauthorizedOperation", err)
		}
	})
}

func assertTagSpecifications(t *testing.T, got []types.TagSpecification, resourceTypes []types.ResourceType, domain, space string) {
	t.Helper()
	if len(got) != len(resourceTypes) {
		t.Fatalf("tag specifications = %#v, want %d", got, len(resourceTypes))
	}
	wantTags := []types.Tag{
		{Key: aws.String("Domain"), Value: aws.String(domain)},
		{Key: aws.String("Space"), Value: aws.String(space)},
	}
	for i, resourceType := range resourceTypes {
		if got[i].ResourceType != resourceType || !reflect.DeepEqual(got[i].Tags, wantTags) {
			t.Fatalf("tag specification %d = %#v, want %q %#v", i, got[i], resourceType, wantTags)
		}
	}
}
