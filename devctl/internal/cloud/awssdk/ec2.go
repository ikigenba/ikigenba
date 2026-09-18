package awssdk

import (
	"context"
	"errors"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type ec2API interface {
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	RunInstances(context.Context, *ec2.RunInstancesInput, ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	StartInstances(context.Context, *ec2.StartInstancesInput, ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error)
	StopInstances(context.Context, *ec2.StopInstancesInput, ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error)
	TerminateInstances(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	DescribeInstanceStatus(context.Context, *ec2.DescribeInstanceStatusInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceStatusOutput, error)
	DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	AllocateAddress(context.Context, *ec2.AllocateAddressInput, ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error)
	AssociateAddress(context.Context, *ec2.AssociateAddressInput, ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error)
	DisassociateAddress(context.Context, *ec2.DisassociateAddressInput, ...func(*ec2.Options)) (*ec2.DisassociateAddressOutput, error)
	ReleaseAddress(context.Context, *ec2.ReleaseAddressInput, ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error)
}

type ec2Client struct {
	sdk ec2API
}

func (c *ec2Client) ListSpaceInstances(ctx context.Context) ([]cloud.Instance, error) {
	input := &ec2.DescribeInstancesInput{Filters: spaceFilters()}
	var instances []cloud.Instance
	for {
		output, err := c.sdk.DescribeInstances(ctx, input)
		if err != nil {
			return nil, ec2Error("DescribeInstances", err)
		}
		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				instances = append(instances, cloudInstance(instance))
			}
		}
		if output.NextToken == nil || *output.NextToken == "" {
			return instances, nil
		}
		input.NextToken = output.NextToken
	}
}

func (c *ec2Client) DescribeInstance(ctx context.Context, id string) (cloud.Instance, error) {
	output, err := c.sdk.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
	if err != nil {
		return cloud.Instance{}, ec2Error("DescribeInstances", err)
	}
	for _, reservation := range output.Reservations {
		if len(reservation.Instances) != 0 {
			return cloudInstance(reservation.Instances[0]), nil
		}
	}
	return cloud.Instance{}, nil
}

func (c *ec2Client) RunInstance(ctx context.Context, spec cloud.LaunchSpec) (cloud.Instance, error) {
	tags := spaceTags(spec.Space)
	output, err := c.sdk.RunInstances(ctx, &ec2.RunInstancesInput{
		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),
		LaunchTemplate: &types.LaunchTemplateSpecification{
			LaunchTemplateId: aws.String(spec.LaunchTemplateID),
		},
		IamInstanceProfile: &types.IamInstanceProfileSpecification{Name: aws.String(spec.InstanceProfile)},
		TagSpecifications: []types.TagSpecification{
			{ResourceType: types.ResourceTypeInstance, Tags: tags},
			{ResourceType: types.ResourceTypeVolume, Tags: tags},
		},
	})
	if err != nil {
		return cloud.Instance{}, ec2Error("RunInstances", err)
	}
	if len(output.Instances) == 0 {
		return cloud.Instance{}, nil
	}
	return cloudInstance(output.Instances[0]), nil
}

func (c *ec2Client) StartInstance(ctx context.Context, id string) error {
	_, err := c.sdk.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}})
	return wrapEC2("StartInstances", err)
}

func (c *ec2Client) StopInstance(ctx context.Context, id string) error {
	_, err := c.sdk.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}})
	return wrapEC2("StopInstances", err)
}

func (c *ec2Client) TerminateInstance(ctx context.Context, id string) error {
	_, err := c.sdk.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: []string{id}})
	return wrapEC2("TerminateInstances", err)
}

func (c *ec2Client) InstanceChecksPassed(ctx context.Context, id string) (bool, error) {
	output, err := c.sdk.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{InstanceIds: []string{id}})
	if err != nil {
		return false, ec2Error("DescribeInstanceStatus", err)
	}
	if len(output.InstanceStatuses) == 0 {
		return false, nil
	}
	status := output.InstanceStatuses[0]
	return status.InstanceStatus != nil && status.InstanceStatus.Status == types.SummaryStatusOk &&
		status.SystemStatus != nil && status.SystemStatus.Status == types.SummaryStatusOk, nil
}

func (c *ec2Client) ListSpaceAddresses(ctx context.Context) ([]cloud.Address, error) {
	output, err := c.sdk.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{Filters: spaceFilters()})
	if err != nil {
		return nil, ec2Error("DescribeAddresses", err)
	}
	addresses := make([]cloud.Address, 0, len(output.Addresses))
	for _, address := range output.Addresses {
		addresses = append(addresses, cloudAddress(address))
	}
	return addresses, nil
}

func (c *ec2Client) AllocateAddress(ctx context.Context, space string) (cloud.Address, error) {
	output, err := c.sdk.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		Domain: types.DomainTypeVpc,
		TagSpecifications: []types.TagSpecification{{
			ResourceType: types.ResourceTypeElasticIp,
			Tags:         spaceTags(space),
		}},
	})
	if err != nil {
		return cloud.Address{}, ec2Error("AllocateAddress", err)
	}
	return cloud.Address{AllocationID: aws.ToString(output.AllocationId), IP: aws.ToString(output.PublicIp), Space: space}, nil
}

func (c *ec2Client) AssociateAddress(ctx context.Context, allocationID, instanceID string) error {
	_, err := c.sdk.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId: aws.String(allocationID),
		InstanceId:   aws.String(instanceID),
	})
	return wrapEC2("AssociateAddress", err)
}

func (c *ec2Client) DisassociateAddress(ctx context.Context, associationID string) error {
	_, err := c.sdk.DisassociateAddress(ctx, &ec2.DisassociateAddressInput{AssociationId: aws.String(associationID)})
	if ec2APIErrorCode(err) == "InvalidAssociationID.NotFound" {
		return nil
	}
	return wrapEC2("DisassociateAddress", err)
}

func (c *ec2Client) ReleaseAddress(ctx context.Context, allocationID string) error {
	_, err := c.sdk.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{AllocationId: aws.String(allocationID)})
	if ec2APIErrorCode(err) == "InvalidAllocationID.NotFound" {
		return nil
	}
	return wrapEC2("ReleaseAddress", err)
}

func spaceFilters() []types.Filter {
	return []types.Filter{
		{Name: aws.String("tag:Project"), Values: []string{"ikigenba"}},
		{Name: aws.String("tag-key"), Values: []string{"Space"}},
	}
}

func spaceTags(space string) []types.Tag {
	return []types.Tag{
		{Key: aws.String("Project"), Value: aws.String("ikigenba")},
		{Key: aws.String("Space"), Value: aws.String(space)},
	}
}

func cloudInstance(instance types.Instance) cloud.Instance {
	return cloud.Instance{
		ID:      aws.ToString(instance.InstanceId),
		Space:   tagValue(instance.Tags, "Space"),
		State:   cloud.InstanceState(instance.State.Name),
		Address: aws.ToString(instance.PublicIpAddress),
	}
}

func cloudAddress(address types.Address) cloud.Address {
	return cloud.Address{
		AllocationID:  aws.ToString(address.AllocationId),
		AssociationID: aws.ToString(address.AssociationId),
		IP:            aws.ToString(address.PublicIp),
		Space:         tagValue(address.Tags, "Space"),
	}
}

func tagValue(tags []types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

func wrapEC2(operation string, err error) error {
	if err == nil {
		return nil
	}
	return ec2Error(operation, err)
}

func ec2Error(operation string, err error) *cloud.Error {
	return &cloud.Error{Service: "ec2", Operation: operation, Code: ec2APIErrorCode(err), Err: err}
}

func ec2APIErrorCode(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}
	return ""
}
