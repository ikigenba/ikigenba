package memory

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func (i instance) at(now time.Time) cloud.Instance {
	value := i.Value
	elapsed := now.Sub(i.Since)
	switch {
	case value.State == cloud.InstancePending && elapsed >= BootDelay:
		value.State = cloud.InstanceRunning
	case value.State == cloud.InstanceStopping && elapsed >= StopDelay:
		value.State = cloud.InstanceStopped
	case value.State == cloud.InstanceShuttingDown && elapsed >= TerminateDelay:
		value.State = cloud.InstanceTerminated
	}
	return value
}

func (s state) terminated(id string, now time.Time) bool {
	return s.Instances[id].at(now).State == cloud.InstanceTerminated
}

func sameLaunch(a, b cloud.Launch) bool {
	return a.Template == b.Template && a.InstanceProfile == b.InstanceProfile &&
		a.Type == b.Type && a.RootGB == b.RootGB && maps.Equal(a.Tags, b.Tags) &&
		maps.Equal(a.InstanceTags, b.InstanceTags)
}

// RunInstance launches an instance and its root volume.
func (c *Cloud) RunInstance(_ context.Context, l cloud.Launch) (cloud.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("RunInstances", "")
	if l.Template != c.cfg.Hosts.LaunchTemplate || l.InstanceProfile != c.cfg.Hosts.InstanceProfile {
		return cloud.Instance{}, op.reject("UnauthorizedOperation")
	}
	if !allowedType(l.Type) || l.Tags[cloud.TagKind] != cloud.KindHost || hasKind(l.InstanceTags) || forbiddenTags(l.Tags) || forbiddenTags(l.InstanceTags) {
		return cloud.Instance{}, op.reject(cloud.CodeTagContract)
	}
	now := c.cfg.Now()
	if t, ok := c.state.LaunchTokens[l.ClientToken]; ok {
		if !sameLaunch(l, t.Spec) {
			return cloud.Instance{}, op.reject("IdempotentParameterMismatch")
		}
		result := c.state.Instances[t.ID].at(now)
		result.Tags = maps.Clone(result.Tags)
		return result, nil
	}
	if err := checkMutation(op, []cloud.Tags{l.Tags, l.InstanceTags}, l.Template, l.InstanceProfile, l.Type, l.ClientToken, c.cfg.Hosts.Region); err != nil {
		return cloud.Instance{}, err
	}
	id, err := randomID(c, op, "i-", c.state.Instances)
	if err != nil {
		return cloud.Instance{}, err
	}
	rootID, err := randomID(c, op, "vol-", c.state.Volumes)
	if err != nil {
		return cloud.Instance{}, err
	}
	l.Tags, l.InstanceTags = ownedTags(l.Tags), ownedTags(l.InstanceTags)
	l.Template, l.InstanceProfile = strings.Clone(l.Template), strings.Clone(l.InstanceProfile)
	l.Type, l.ClientToken = strings.Clone(l.Type), strings.Clone(l.ClientToken)
	tags := maps.Clone(l.Tags)
	maps.Copy(tags, l.InstanceTags)
	az := c.cfg.Hosts.Region + "a"
	i := cloud.Instance{ID: id, State: cloud.InstancePending, Type: l.Type, AZ: az, RootVolume: rootID, Tags: tags}
	root := cloud.Volume{
		ID: rootID, State: cloud.VolumeInUse, AZ: az, SizeGB: l.RootGB,
		Attachment: cloud.Attachment{InstanceID: id, Device: "/dev/xvda", State: cloud.AttachmentAttached},
		Tags:       l.Tags,
	}
	next := c.state.snapshot()
	next.Instances[id] = instance{Value: i, Since: now}
	next.Volumes[rootID] = volume{Value: root, Created: now}
	if l.ClientToken != "" {
		next.LaunchTokens[l.ClientToken] = launchToken{Spec: l, ID: id}
	}
	if err := c.save(next, op); err != nil {
		return cloud.Instance{}, err
	}
	i.Tags = maps.Clone(i.Tags)
	return i, nil
}

// DescribeInstance returns the instance's current state.
func (c *Cloud) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i, ok := c.state.Instances[id]
	if !ok {
		return cloud.Instance{}, ec2("DescribeInstances", id).reject("InvalidInstanceID.NotFound")
	}
	value := i.at(c.cfg.Now())
	value.Tags = maps.Clone(value.Tags)
	return value, nil
}

// FindInstances lists non-terminated instances matching all tags.
func (c *Cloud) FindInstances(_ context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	return find(c, ec2("DescribeInstances", ""), tags, func(s state) map[string]instance { return s.Instances },
		func(_ state, i instance, now time.Time) (cloud.Instance, cloud.Tags, bool) {
			value := i.at(now)
			value.Tags = maps.Clone(value.Tags)
			return value, value.Tags, value.State != cloud.InstanceTerminated
		}, each)
}

// StopInstance begins stopping a running instance.
func (c *Cloud) StopInstance(_ context.Context, id string) error {
	return c.transition(id, "StopInstances", cloud.InstanceStopping,
		[]string{cloud.InstanceRunning}, []string{cloud.InstanceStopping, cloud.InstanceStopped})
}

// StartInstance begins booting a stopped instance.
func (c *Cloud) StartInstance(_ context.Context, id string) error {
	return c.transition(id, "StartInstances", cloud.InstancePending,
		[]string{cloud.InstanceStopped}, []string{cloud.InstancePending, cloud.InstanceRunning})
}

// TerminateInstance begins terminating an instance.
func (c *Cloud) TerminateInstance(_ context.Context, id string) error {
	return c.transition(id, "TerminateInstances", cloud.InstanceShuttingDown,
		nil, []string{cloud.InstanceShuttingDown, cloud.InstanceTerminated})
}

func (c *Cloud) transition(id, name, to string, from, unchanged []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2(name, id)
	i, found := c.state.Instances[id]
	if !found {
		return op.reject("InvalidInstanceID.NotFound")
	}
	now := c.cfg.Now()
	current := i.at(now).State
	for _, s := range unchanged {
		if s == current {
			return nil
		}
	}
	if len(from) != 0 && current != from[0] {
		return op.reject("IncorrectInstanceState")
	}
	i.Value.State, i.Since = to, now
	next := c.state.snapshot()
	next.Instances[i.Value.ID] = i
	return c.save(next, op)
}

// SetInstanceType changes the type of a stopped instance.
func (c *Cloud) SetInstanceType(_ context.Context, id, typ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("ModifyInstanceAttribute", id)
	i, found := c.state.Instances[id]
	if !found {
		return op.reject("InvalidInstanceID.NotFound")
	}
	if i.at(c.cfg.Now()).State != cloud.InstanceStopped {
		return op.reject("IncorrectInstanceState")
	}
	if !allowedType(typ) {
		return op.reject(cloud.CodeTagContract)
	}
	if i.Value.Type == typ {
		return nil
	}
	if err := checkMutation(op, nil, typ); err != nil {
		return err
	}
	i.Value.Type = strings.Clone(typ)
	next := c.state.snapshot()
	next.Instances[i.Value.ID] = i
	return c.save(next, op)
}
