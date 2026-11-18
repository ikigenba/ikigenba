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
	runInstances      = "RunInstances"
	describeInstances = "DescribeInstances"
)

func (a *adapter) RunInstance(ctx context.Context, l cloud.Launch) (cloud.Instance, error) {
	if !validLaunch(l) {
		return cloud.Instance{}, ec2Error(runInstances, "", cloud.CodeTagContract)
	}
	instanceTags := maps.Clone(l.Tags)
	maps.Copy(instanceTags, l.InstanceTags)
	out, err := a.ec2.RunInstances(ctx, &ec2.RunInstancesInput{
		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),
		LaunchTemplate: &types.LaunchTemplateSpecification{
			LaunchTemplateName: aws.String(l.Template),
			Version:            aws.String("$Default"),
		},
		IamInstanceProfile: &types.IamInstanceProfileSpecification{Name: aws.String(l.InstanceProfile)},
		InstanceType:       types.InstanceType(l.Type),
		ClientToken:        aws.String(l.ClientToken),
		BlockDeviceMappings: []types.BlockDeviceMapping{{
			DeviceName: aws.String("/dev/xvda"),
			Ebs: &types.EbsBlockDevice{
				VolumeType:          types.VolumeTypeGp3,
				VolumeSize:          aws.Int32(l.RootGB),
				DeleteOnTermination: aws.Bool(true),
			},
		}},
		TagSpecifications: []types.TagSpecification{
			tagSpec(types.ResourceTypeInstance, instanceTags),
			tagSpec(types.ResourceTypeVolume, l.Tags),
			tagSpec(types.ResourceTypeNetworkInterface, l.Tags),
		},
	})
	if err != nil {
		return cloud.Instance{}, failed("ec2", runInstances, "", err)
	}
	if len(out.Instances) == 0 {
		return cloud.Instance{}, ec2Error(runInstances, "", cloud.CodeBadResponse)
	}
	return fromInstance(&out.Instances[0]), nil
}

func (a *adapter) DescribeInstance(ctx context.Context, id string) (cloud.Instance, error) {
	out, err := a.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
	if err != nil {
		return cloud.Instance{}, failed("ec2", describeInstances, id, err)
	}
	for _, reservation := range out.Reservations {
		for _, instance := range reservation.Instances {
			if aws.ToString(instance.InstanceId) == id {
				return fromInstance(&instance), nil
			}
		}
	}
	return cloud.Instance{}, ec2Error(describeInstances, id, "InvalidInstanceID.NotFound")
}

func (a *adapter) FindInstances(ctx context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	if !validFilter(tags) {
		return ec2Error(describeInstances, "", cloud.CodeTagContract)
	}
	filters := append(tagFilters(tags), types.Filter{
		Name: aws.String("instance-state-name"),
		Values: []string{
			cloud.InstancePending, cloud.InstanceRunning, cloud.InstanceShuttingDown,
			cloud.InstanceStopping, cloud.InstanceStopped,
		},
	})
	pages := ec2.NewDescribeInstancesPaginator(a.ec2, &ec2.DescribeInstancesInput{Filters: filters})
	for pages.HasMorePages() {
		out, err := pages.NextPage(ctx)
		if err != nil {
			return failed("ec2", describeInstances, "", err)
		}
		var instances []cloud.Instance
		for _, reservation := range out.Reservations {
			for _, instance := range reservation.Instances {
				instances = append(instances, fromInstance(&instance))
			}
		}
		if len(instances) > 0 && !each(instances) {
			return nil
		}
	}
	return nil
}

func fromInstance(instance *types.Instance) cloud.Instance {
	i := cloud.Instance{
		ID:   aws.ToString(instance.InstanceId),
		Type: string(instance.InstanceType),
		Tags: fromTags(instance.Tags),
	}
	if instance.State != nil {
		i.State = string(instance.State.Name)
	}
	if instance.Placement != nil {
		i.AZ = aws.ToString(instance.Placement.AvailabilityZone)
	}
	root := aws.ToString(instance.RootDeviceName)
	if root != "" {
		for _, device := range instance.BlockDeviceMappings {
			if aws.ToString(device.DeviceName) == root && device.Ebs != nil {
				i.RootVolume = aws.ToString(device.Ebs.VolumeId)
				break
			}
		}
	}
	return i
}

func (a *adapter) StopInstance(ctx context.Context, id string) error {
	_, err := a.ec2.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}})
	return failed("ec2", "StopInstances", id, err)
}

func (a *adapter) StartInstance(ctx context.Context, id string) error {
	_, err := a.ec2.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}})
	return failed("ec2", "StartInstances", id, err)
}

func (a *adapter) TerminateInstance(ctx context.Context, id string) error {
	_, err := a.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: []string{id}})
	return failed("ec2", "TerminateInstances", id, err)
}

func (a *adapter) SetInstanceType(ctx context.Context, id, typ string) error {
	_, err := a.ec2.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId:   aws.String(id),
		InstanceType: &types.AttributeValue{Value: aws.String(typ)},
	})
	return failed("ec2", "ModifyInstanceAttribute", id, err)
}
