package awssdk

import (
	"context"
	"maps"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const (
	allocateAddress   = "AllocateAddress"
	describeAddresses = "DescribeAddresses"
)

func (a *adapter) AllocateAddress(ctx context.Context, tags cloud.Tags) (cloud.Address, error) {
	if !validKind(tags, cloud.KindHost) {
		return cloud.Address{}, ec2Error(allocateAddress, "", cloud.CodeTagContract)
	}
	out, err := a.ec2.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		Domain:            types.DomainTypeVpc,
		TagSpecifications: []types.TagSpecification{tagSpec(types.ResourceTypeElasticIp, tags)},
	})
	if err != nil {
		return cloud.Address{}, failed("ec2", allocateAddress, "", err)
	}
	return cloud.Address{AllocationID: aws.ToString(out.AllocationId), IP: aws.ToString(out.PublicIp), Tags: maps.Clone(tags)}, nil
}

func (a *adapter) DescribeAddress(ctx context.Context, allocationID string) (cloud.Address, error) {
	out, err := a.ec2.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{AllocationIds: []string{allocationID}})
	if err != nil {
		return cloud.Address{}, failed("ec2", describeAddresses, allocationID, err)
	}
	for _, address := range out.Addresses {
		if aws.ToString(address.AllocationId) == allocationID {
			return fromAddress(&address), nil
		}
	}
	return cloud.Address{}, ec2Error(describeAddresses, allocationID, "InvalidAllocationID.NotFound")
}

func (a *adapter) FindAddresses(ctx context.Context, tags cloud.Tags, each func([]cloud.Address) bool) error {
	if !validFilter(tags) {
		return ec2Error(describeAddresses, "", cloud.CodeTagContract)
	}
	out, err := a.ec2.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{Filters: tagFilters(tags)})
	if err != nil {
		return failed("ec2", describeAddresses, "", err)
	}
	if len(out.Addresses) > 0 {
		addresses := make([]cloud.Address, 0, len(out.Addresses))
		for _, address := range out.Addresses {
			addresses = append(addresses, fromAddress(&address))
		}
		each(addresses)
	}
	return nil
}

func fromAddress(a *types.Address) cloud.Address {
	return cloud.Address{
		AllocationID:  aws.ToString(a.AllocationId),
		AssociationID: aws.ToString(a.AssociationId),
		InstanceID:    aws.ToString(a.InstanceId),
		IP:            aws.ToString(a.PublicIp),
		Tags:          fromTags(a.Tags),
	}
}

func (a *adapter) AssociateAddress(ctx context.Context, allocationID, instanceID string) (string, error) {
	out, err := a.ec2.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId: aws.String(allocationID),
		InstanceId:   aws.String(instanceID),
	})
	if err != nil {
		return "", failed("ec2", "AssociateAddress", allocationID, err)
	}
	return aws.ToString(out.AssociationId), nil
}

func (a *adapter) DisassociateAddress(ctx context.Context, associationID string) error {
	_, err := a.ec2.DisassociateAddress(ctx, &ec2.DisassociateAddressInput{AssociationId: aws.String(associationID)})
	err = failed("ec2", "DisassociateAddress", associationID, err)
	if err != nil && cloud.ClassOf(err) == cloud.ClassNotFound {
		return nil
	}
	return err
}

func (a *adapter) ReleaseAddress(ctx context.Context, allocationID string) error {
	_, err := a.ec2.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{AllocationId: aws.String(allocationID)})
	err = failed("ec2", "ReleaseAddress", allocationID, err)
	if err != nil && cloud.ClassOf(err) == cloud.ClassNotFound {
		return nil
	}
	return err
}
