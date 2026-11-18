package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const (
	createVolume        = "CreateVolume"
	describeVolumes     = "DescribeVolumes"
	volumeModifications = "DescribeVolumesModifications"
	createTags          = "CreateTags"
)

func (a *adapter) CreateVolume(ctx context.Context, s cloud.VolumeSpec) (cloud.Volume, error) {
	if !validKind(s.Tags, cloud.KindHostData) {
		return cloud.Volume{}, ec2Error(createVolume, "", cloud.CodeTagContract)
	}
	out, err := a.ec2.CreateVolume(ctx, &ec2.CreateVolumeInput{
		AvailabilityZone:  aws.String(s.AZ),
		Size:              aws.Int32(s.SizeGB),
		ClientToken:       aws.String(s.ClientToken),
		VolumeType:        types.VolumeTypeGp3,
		TagSpecifications: []types.TagSpecification{tagSpec(types.ResourceTypeVolume, s.Tags)},
	})
	if err != nil {
		return cloud.Volume{}, failed("ec2", createVolume, "", err)
	}
	return fromVolume(out.VolumeId, out.State, out.AvailabilityZone, out.Size, out.Attachments, out.Tags), nil
}

func (a *adapter) DescribeVolume(ctx context.Context, id string) (cloud.Volume, error) {
	out, err := a.ec2.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{id}})
	if err != nil {
		return cloud.Volume{}, failed("ec2", describeVolumes, id, err)
	}
	for _, v := range out.Volumes {
		if aws.ToString(v.VolumeId) == id {
			return fromVolume(v.VolumeId, v.State, v.AvailabilityZone, v.Size, v.Attachments, v.Tags), nil
		}
	}
	return cloud.Volume{}, ec2Error(describeVolumes, id, "InvalidVolume.NotFound")
}

func (a *adapter) FindVolumes(ctx context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	if !validFilter(tags) {
		return ec2Error(describeVolumes, "", cloud.CodeTagContract)
	}
	pages := ec2.NewDescribeVolumesPaginator(a.ec2, &ec2.DescribeVolumesInput{Filters: tagFilters(tags)})
	for pages.HasMorePages() {
		out, err := pages.NextPage(ctx)
		if err != nil {
			return failed("ec2", describeVolumes, "", err)
		}
		if len(out.Volumes) == 0 {
			continue
		}
		volumes := make([]cloud.Volume, 0, len(out.Volumes))
		for _, v := range out.Volumes {
			volumes = append(volumes, fromVolume(v.VolumeId, v.State, v.AvailabilityZone, v.Size, v.Attachments, v.Tags))
		}
		if !each(volumes) {
			return nil
		}
	}
	return nil
}

func fromVolume(id *string, state types.VolumeState, az *string, size *int32, attachments []types.VolumeAttachment, tags []types.Tag) cloud.Volume {
	v := cloud.Volume{ID: aws.ToString(id), State: string(state), AZ: aws.ToString(az), SizeGB: aws.ToInt32(size), Tags: fromTags(tags)}
	if len(attachments) > 0 {
		first := attachments[0]
		v.Attachment = cloud.Attachment{InstanceID: aws.ToString(first.InstanceId), Device: aws.ToString(first.Device), State: string(first.State)}
	}
	return v
}

func (a *adapter) AttachVolume(ctx context.Context, volumeID, instanceID, device string) error {
	_, err := a.ec2.AttachVolume(ctx, &ec2.AttachVolumeInput{
		VolumeId: aws.String(volumeID), InstanceId: aws.String(instanceID), Device: aws.String(device),
	})
	return failed("ec2", "AttachVolume", volumeID, err)
}

func (a *adapter) ModifyVolume(ctx context.Context, volumeID string, sizeGB int32) error {
	_, err := a.ec2.ModifyVolume(ctx, &ec2.ModifyVolumeInput{VolumeId: aws.String(volumeID), Size: aws.Int32(sizeGB)})
	return failed("ec2", "ModifyVolume", volumeID, err)
}

func (a *adapter) VolumeModification(ctx context.Context, volumeID string) (cloud.Modification, error) {
	pages := ec2.NewDescribeVolumesModificationsPaginator(a.ec2, &ec2.DescribeVolumesModificationsInput{VolumeIds: []string{volumeID}})
	var newest *types.VolumeModification
	for pages.HasMorePages() {
		out, err := pages.NextPage(ctx)
		if err != nil {
			err = failed("ec2", volumeModifications, volumeID, err)
			if cloud.ClassOf(err) == cloud.ClassNotFound {
				return cloud.Modification{}, nil
			}
			return cloud.Modification{}, err
		}
		for _, item := range out.VolumesModifications {
			if aws.ToString(item.VolumeId) == volumeID && (newest == nil || aws.ToTime(item.StartTime).After(aws.ToTime(newest.StartTime))) {
				newest = &item
			}
		}
	}
	if newest == nil {
		return cloud.Modification{}, nil
	}
	return cloud.Modification{State: string(newest.ModificationState), TargetGB: aws.ToInt32(newest.TargetSize)}, nil
}

func (a *adapter) TagResource(ctx context.Context, id string, tags cloud.Tags) error {
	if hasForbiddenTags(tags) || hasKind(tags) {
		return ec2Error(createTags, id, cloud.CodeTagContract)
	}
	_, err := a.ec2.CreateTags(ctx, &ec2.CreateTagsInput{Resources: []string{id}, Tags: toTags(tags)})
	return failed("ec2", createTags, id, err)
}
