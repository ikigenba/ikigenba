package awssdk

import (
	"context"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type ec2API interface {
	DescribeLaunchTemplates(context.Context, *ec2.DescribeLaunchTemplatesInput, ...func(*ec2.Options)) (*ec2.DescribeLaunchTemplatesOutput, error)
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

var _ cloud.EC2 = (*ec2Client)(nil)

func (c *ec2Client) LaunchTemplate(ctx context.Context, name string) (string, error) {
	output, err := c.sdk.DescribeLaunchTemplates(ctx, &ec2.DescribeLaunchTemplatesInput{Filters: []types.Filter{{
		Name: aws.String("launch-template-name"), Values: []string{name},
	}}})
	if err != nil {
		return "", ec2Error("DescribeLaunchTemplates", err)
	}
	if len(output.LaunchTemplates) == 0 {
		return "", &cloud.NotFoundError{Kind: "launch template", Name: name}
	}
	return aws.ToString(output.LaunchTemplates[0].LaunchTemplateId), nil
}

func (c *ec2Client) ListSpaceInstances(ctx context.Context, domain string) ([]cloud.Instance, error) {
	input := &ec2.DescribeInstancesInput{Filters: spaceFilters(domain)}
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
	if apiErrorCode(err) == "InvalidInstanceID.NotFound" {
		return cloud.Instance{}, nil
	}
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
	output, err := c.sdk.RunInstances(ctx, runInstancesInput(spec, false))
	if err != nil {
		return cloud.Instance{}, ec2Error("RunInstances", err)
	}
	if len(output.Instances) == 0 {
		return cloud.Instance{}, nil
	}
	return cloudInstance(output.Instances[0]), nil
}

func (c *ec2Client) LaunchReady(ctx context.Context, spec cloud.LaunchSpec) (bool, error) {
	_, err := c.sdk.RunInstances(ctx, runInstancesInput(spec, true))
	if apiErr := boundedAPIError(err); apiErr != nil {
		switch apiErr.ErrorCode() {
		case "DryRunOperation":
			return true, nil
		case "InvalidParameterValue":
			if strings.Contains(apiErr.ErrorMessage(), "Invalid IAM Instance Profile name") {
				return false, nil
			}
		}
	}
	return false, ec2Error("RunInstances", err)
}

func runInstancesInput(spec cloud.LaunchSpec, dryRun bool) *ec2.RunInstancesInput {
	tags := spaceTags(spec.Domain, spec.Space)
	return &ec2.RunInstancesInput{
		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),
		DryRun:   aws.Bool(dryRun),
		LaunchTemplate: &types.LaunchTemplateSpecification{
			LaunchTemplateId: aws.String(spec.LaunchTemplateID),
		},
		IamInstanceProfile: &types.IamInstanceProfileSpecification{Name: aws.String(spec.InstanceProfile)},
		TagSpecifications: []types.TagSpecification{
			{ResourceType: types.ResourceTypeInstance, Tags: tags},
			{ResourceType: types.ResourceTypeVolume, Tags: tags},
		},
	}
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

func (c *ec2Client) ListSpaceAddresses(ctx context.Context, domain string) ([]cloud.Address, error) {
	output, err := c.sdk.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{Filters: spaceFilters(domain)})
	if err != nil {
		return nil, ec2Error("DescribeAddresses", err)
	}
	addresses := make([]cloud.Address, 0, len(output.Addresses))
	for _, address := range output.Addresses {
		addresses = append(addresses, cloudAddress(address))
	}
	return addresses, nil
}

func (c *ec2Client) AllocateAddress(ctx context.Context, domain, space string) (cloud.Address, error) {
	output, err := c.sdk.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		Domain: types.DomainTypeVpc,
		TagSpecifications: []types.TagSpecification{{
			ResourceType: types.ResourceTypeElasticIp,
			Tags:         spaceTags(domain, space),
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

func spaceFilters(domain string) []types.Filter {
	return []types.Filter{
		{Name: aws.String("tag:Domain"), Values: []string{domain}},
		{Name: aws.String("tag-key"), Values: []string{"Space"}},
	}
}

func spaceTags(domain, space string) []types.Tag {
	return []types.Tag{
		{Key: aws.String("Domain"), Value: aws.String(domain)},
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
	return sdkError("ec2", operation, "", err)
}

func ec2APIErrorCode(err error) string {
	return apiErrorCode(err)
}
