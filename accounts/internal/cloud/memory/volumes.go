package memory

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func (v volume) latest(now time.Time) cloud.Modification {
	if len(v.Modifications) == 0 {
		return cloud.Modification{}
	}
	m := v.Modifications[len(v.Modifications)-1]
	result := cloud.Modification{State: cloud.ModificationModifying, TargetGB: m.Target}
	elapsed := now.Sub(m.Since)
	switch {
	case elapsed >= 2*ModifyDelay:
		result.State = cloud.ModificationCompleted
	case elapsed >= ModifyDelay:
		result.State = cloud.ModificationOptimizing
	}
	return result
}

func (v volume) at(s state, now time.Time) (cloud.Volume, bool) {
	value := v.Value
	switch {
	case s.terminated(value.Attachment.InstanceID, now):
		if s.Instances[value.Attachment.InstanceID].Value.RootVolume == value.ID {
			return cloud.Volume{}, false
		}
		value.Attachment = cloud.Attachment{}
		value.State = cloud.VolumeAvailable
	case value.Attachment.State == cloud.AttachmentAttaching && now.Sub(v.Attached) >= VolumeDelay:
		value.Attachment.State = cloud.AttachmentAttached
	case value.State == cloud.VolumeCreating && now.Sub(v.Created) >= VolumeDelay:
		value.State = cloud.VolumeAvailable
	}
	if m := v.latest(now); m.State == cloud.ModificationOptimizing || m.State == cloud.ModificationCompleted {
		value.SizeGB = m.TargetGB
	} else if len(v.Modifications) > 1 {
		value.SizeGB = v.Modifications[len(v.Modifications)-2].Target
	}
	return value, true
}

func (s state) volume(id string, now time.Time) (cloud.Volume, bool) {
	v, ok := s.Volumes[id]
	if !ok {
		return cloud.Volume{}, false
	}
	return v.at(s, now)
}

// CreateVolume creates a data volume and records a supplied client token.
func (c *Cloud) CreateVolume(_ context.Context, spec cloud.VolumeSpec) (cloud.Volume, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("CreateVolume", "")
	now := c.cfg.Now()
	if t, ok := c.state.VolumeTokens[spec.ClientToken]; ok {
		if spec.AZ != t.Spec.AZ || spec.SizeGB != t.Spec.SizeGB || !maps.Equal(spec.Tags, t.Spec.Tags) {
			return cloud.Volume{}, op.reject("IdempotentParameterMismatch")
		}
		value, _ := c.state.volume(t.ID, now)
		value.Tags = maps.Clone(value.Tags)
		return value, nil
	}
	if spec.Tags[cloud.TagKind] != cloud.KindHostData || forbiddenTags(spec.Tags) {
		return cloud.Volume{}, op.reject(cloud.CodeTagContract)
	}
	if err := checkMutation(op, []cloud.Tags{spec.Tags}, spec.AZ, spec.ClientToken); err != nil {
		return cloud.Volume{}, err
	}
	id, err := randomID(c, op, "vol-", c.state.Volumes)
	if err != nil {
		return cloud.Volume{}, err
	}
	spec.Tags = ownedTags(spec.Tags)
	spec.AZ, spec.ClientToken = strings.Clone(spec.AZ), strings.Clone(spec.ClientToken)
	value := cloud.Volume{ID: id, State: cloud.VolumeCreating, AZ: spec.AZ, SizeGB: spec.SizeGB, Tags: spec.Tags}
	next := c.state.snapshot()
	next.Volumes[id] = volume{Value: value, Created: now}
	if spec.ClientToken != "" {
		next.VolumeTokens[spec.ClientToken] = volumeToken{Spec: spec, ID: id}
	}
	if err := c.save(next, op); err != nil {
		return cloud.Volume{}, err
	}
	value.Tags = maps.Clone(value.Tags)
	return value, nil
}

// DescribeVolume returns the volume's current state.
func (c *Cloud) DescribeVolume(_ context.Context, id string) (cloud.Volume, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, exists := c.state.volume(id, c.cfg.Now())
	if !exists {
		return cloud.Volume{}, ec2("DescribeVolumes", id).reject("InvalidVolume.NotFound")
	}
	value.Tags = maps.Clone(value.Tags)
	return value, nil
}

// FindVolumes lists the volumes matching all tags.
func (c *Cloud) FindVolumes(_ context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	return find(c, ec2("DescribeVolumes", ""), tags, func(s state) map[string]volume { return s.Volumes },
		func(s state, v volume, now time.Time) (cloud.Volume, cloud.Tags, bool) {
			value, visible := v.at(s, now)
			value.Tags = maps.Clone(value.Tags)
			return value, value.Tags, visible
		}, each)
}

// AttachVolume starts attaching an available volume.
func (c *Cloud) AttachVolume(_ context.Context, id, instanceID, device string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("AttachVolume", id)
	now := c.cfg.Now()
	value, exists := c.state.volume(id, now)
	if !exists {
		return op.reject("InvalidVolume.NotFound")
	}
	switch {
	case value.Attachment.InstanceID == instanceID && value.Attachment.State != "":
		return nil
	case value.Attachment.InstanceID != "":
		return op.reject("VolumeInUse")
	case value.State != cloud.VolumeAvailable:
		return op.reject("IncorrectState")
	}
	i, exists := c.state.Instances[instanceID]
	if !exists {
		return op.reject("InvalidInstanceID.NotFound")
	}
	host := i.at(now)
	if host.State != cloud.InstanceRunning && host.State != cloud.InstanceStopped {
		return op.reject("IncorrectInstanceState")
	}
	if value.AZ != host.AZ {
		return op.reject("InvalidParameterValue")
	}
	if err := checkMutation(op, nil, device); err != nil {
		return err
	}
	v := c.state.Volumes[id]
	v.Value.State = cloud.VolumeInUse
	v.Value.Attachment = cloud.Attachment{InstanceID: host.ID, Device: strings.Clone(device), State: cloud.AttachmentAttaching}
	v.Attached = now
	next := c.state.snapshot()
	next.Volumes[v.Value.ID] = v
	return c.save(next, op)
}

// ModifyVolume begins growing the volume.
func (c *Cloud) ModifyVolume(_ context.Context, id string, sizeGB int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("ModifyVolume", id)
	now := c.cfg.Now()
	value, exists := c.state.volume(id, now)
	if !exists {
		return op.reject("InvalidVolume.NotFound")
	}
	if sizeGB <= value.SizeGB {
		return op.reject("InvalidParameterValue")
	}
	v := c.state.Volumes[id]
	if m := v.latest(now); m.State != "" && m.State != cloud.ModificationCompleted {
		return op.reject("IncorrectModificationState")
	}
	count := 0
	for _, m := range v.Modifications {
		if elapsed := now.Sub(m.Since); elapsed >= 0 && elapsed < 24*time.Hour {
			count++
		}
	}
	if count >= MaxModifications {
		return op.reject("VolumeModificationRateExceeded")
	}
	v.Modifications = append(slices.Clone(v.Modifications), modification{Target: sizeGB, Since: now})
	next := c.state.snapshot()
	next.Volumes[v.Value.ID] = v
	return c.save(next, op)
}

// VolumeModification returns the current phase of the latest modification.
func (c *Cloud) VolumeModification(_ context.Context, id string) (cloud.Modification, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	if _, exists := c.state.volume(id, now); !exists {
		return cloud.Modification{}, ec2("DescribeVolumesModifications", id).reject("InvalidVolume.NotFound")
	}
	return c.state.Volumes[id].latest(now), nil
}
