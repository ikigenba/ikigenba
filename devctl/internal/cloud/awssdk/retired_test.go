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

func TestRetiredSDKInventory(t *testing.T) {
	// R-FL3V-HTTM
	const value = "2026-11-08T18:30:00Z"
	tags := []types.Tag{{Key: aws.String(cloud.RetiredTag), Value: aws.String(value)}}
	sdk := &fakeEC2{describeInstances: func(in *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		for _, filter := range in.Filters {
			if aws.ToString(filter.Name) == "tag:"+cloud.RetiredTag {
				t.Fatal("filtered retired resources")
			}
		}
		return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{
			{InstanceId: aws.String("old"), Tags: tags, State: &types.InstanceState{Name: types.InstanceStateNameStopped}},
			{InstanceId: aws.String("new"), State: &types.InstanceState{Name: types.InstanceStateNameRunning}},
		}}}}, nil
	}, describeAddresses: func(in *ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput, error) {
		for _, filter := range in.Filters {
			if aws.ToString(filter.Name) == "tag:"+cloud.RetiredTag {
				t.Fatal("filtered retired addresses")
			}
		}
		return &ec2.DescribeAddressesOutput{Addresses: []types.Address{{AllocationId: aws.String("old"), Tags: tags}, {AllocationId: aws.String("new")}}}, nil
	}}
	client := &ec2Client{sdk: sdk}
	instances, err := client.ListSpaceInstances(t.Context(), "ikigenba.dev")
	if err != nil || len(instances) != 2 || instances[0].Retired != value || instances[1].Retired != "" {
		t.Fatalf("%v %v", instances, err)
	}
	addresses, err := client.ListSpaceAddresses(t.Context(), "ikigenba.dev")
	if err != nil || len(addresses) != 2 || addresses[0].Retired != value || addresses[1].Retired != "" {
		t.Fatalf("%v %v", addresses, err)
	}
}
func TestTagRetiredOneRequestAndError(t *testing.T) {
	// R-FMBR-VLKB
	ids := []string{"i-0a1b2c3d4e5f60718", "eipalloc-0123456789abcdef0"}
	const value = "2026-11-08T18:30:00Z"
	for _, fail := range []bool{false, true} {
		sdk := &fakeEC2{createTags: func(in *ec2.CreateTagsInput) (*ec2.CreateTagsOutput, error) {
			if !reflect.DeepEqual(in.Resources, ids) || len(in.Tags) != 1 || aws.ToString(in.Tags[0].Key) != cloud.RetiredTag || aws.ToString(in.Tags[0].Value) != value {
				t.Fatalf("request %#v", in)
			}
			if fail {
				return nil, &smithy.GenericAPIError{Code: "UnauthorizedOperation"}
			}
			return &ec2.CreateTagsOutput{}, nil
		}}
		err := (&ec2Client{sdk: sdk}).TagRetired(context.Background(), ids, value)
		if !reflect.DeepEqual(sdk.calls, []string{"CreateTags"}) {
			t.Fatal(sdk.calls)
		}
		var ce *cloud.Error
		if fail {
			if !errors.As(err, &ce) || ce.Service != "ec2" || ce.Operation != "CreateTags" || ce.Subject != "" || ce.Error() != "ec2 CreateTags: UnauthorizedOperation" {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}
