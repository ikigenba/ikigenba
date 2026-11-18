package memory

import (
	"context"
	"maps"
	"net/netip"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func (s state) addressAt(a cloud.Address, now time.Time) cloud.Address {
	if s.terminated(a.InstanceID, now) {
		a.InstanceID, a.AssociationID = "", ""
	}
	return a
}

// AllocateAddress claims the lowest unallocated address from TEST-NET-1.
func (c *Cloud) AllocateAddress(_ context.Context, tags cloud.Tags) (cloud.Address, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("AllocateAddress", "")
	if tags[cloud.TagKind] != cloud.KindHost || forbiddenTags(tags) {
		return cloud.Address{}, op.reject(cloud.CodeTagContract)
	}
	used := make(map[string]bool, len(c.state.Addresses))
	for _, a := range c.state.Addresses {
		used[a.IP] = true
	}
	var ip string
	for octet := byte(1); octet < 255; octet++ {
		candidate := netip.AddrFrom4([4]byte{192, 0, 2, octet}).String()
		if !used[candidate] {
			ip = candidate
			break
		}
	}
	if ip == "" {
		return cloud.Address{}, op.reject("AddressLimitExceeded")
	}
	if err := checkMutation(op, []cloud.Tags{tags}); err != nil {
		return cloud.Address{}, err
	}
	id, err := randomID(c, op, "eipalloc-", c.state.Addresses)
	if err != nil {
		return cloud.Address{}, err
	}
	a := cloud.Address{AllocationID: id, IP: ip, Tags: ownedTags(tags)}
	next := c.state.snapshot()
	next.Addresses[a.AllocationID] = a
	if err := c.save(next, op); err != nil {
		return cloud.Address{}, err
	}
	a.Tags = maps.Clone(a.Tags)
	return a, nil
}

// DescribeAddress returns the address and its current association.
func (c *Cloud) DescribeAddress(_ context.Context, id string) (cloud.Address, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, exists := c.state.Addresses[id]
	if !exists {
		return cloud.Address{}, ec2("DescribeAddresses", id).reject("InvalidAllocationID.NotFound")
	}
	a = c.state.addressAt(a, c.cfg.Now())
	a.Tags = maps.Clone(a.Tags)
	return a, nil
}

// FindAddresses lists the addresses matching all tags.
func (c *Cloud) FindAddresses(_ context.Context, tags cloud.Tags, each func([]cloud.Address) bool) error {
	return find(c, ec2("DescribeAddresses", ""), tags, func(s state) map[string]cloud.Address { return s.Addresses },
		func(s state, a cloud.Address, now time.Time) (cloud.Address, cloud.Tags, bool) {
			a = s.addressAt(a, now)
			a.Tags = maps.Clone(a.Tags)
			return a, a.Tags, true
		}, each)
}

// AssociateAddress associates an allocation with a running instance.
func (c *Cloud) AssociateAddress(_ context.Context, allocationID, instanceID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("AssociateAddress", allocationID)
	i, exists := c.state.Instances[instanceID]
	if !exists {
		return "", op.reject("InvalidInstanceID.NotFound")
	}
	now := c.cfg.Now()
	if i.at(now).State != cloud.InstanceRunning {
		return "", op.reject("IncorrectInstanceState")
	}
	a, exists := c.state.Addresses[allocationID]
	if !exists {
		return "", op.reject("InvalidAllocationID.NotFound")
	}
	if a.InstanceID == instanceID {
		return a.AssociationID, nil
	}
	id, err := randomID(c, op, "eipassoc-", c.state.Associations)
	if err != nil {
		return "", err
	}
	a.AssociationID, a.InstanceID = id, i.Value.ID
	next := c.state.snapshot()
	next.Addresses[a.AllocationID], next.Associations[id] = a, true
	if err := c.save(next, op); err != nil {
		return "", err
	}
	return id, nil
}

// DisassociateAddress clears an association if it still exists.
func (c *Cloud) DisassociateAddress(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	for allocationID, a := range c.state.Addresses {
		a = c.state.addressAt(a, now)
		if id != "" && a.AssociationID == id {
			a.AssociationID, a.InstanceID = "", ""
			next := c.state.snapshot()
			next.Addresses[allocationID] = a
			return c.save(next, ec2("DisassociateAddress", id))
		}
	}
	return nil
}

// ReleaseAddress frees an address without an association.
func (c *Cloud) ReleaseAddress(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("ReleaseAddress", id)
	a, exists := c.state.Addresses[id]
	if !exists {
		return nil
	}
	if c.state.addressAt(a, c.cfg.Now()).AssociationID != "" {
		return op.reject("InvalidIPAddress.InUse")
	}
	next := c.state.snapshot()
	delete(next.Addresses, id)
	return c.save(next, op)
}

func mergedTags(tags, extra cloud.Tags) cloud.Tags {
	if matches(tags, extra) {
		return tags
	}
	result := maps.Clone(tags)
	maps.Copy(result, ownedTags(extra))
	return result
}

// TagResource sets the given resource tags, keeping other tags.
func (c *Cloud) TagResource(_ context.Context, id string, tags cloud.Tags) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := ec2("CreateTags", id)
	if hasKind(tags) || forbiddenTags(tags) {
		return op.reject(cloud.CodeTagContract)
	}
	switch {
	case strings.HasPrefix(id, "vol-"):
		v, exists := c.state.volume(id, c.cfg.Now())
		if !exists {
			return op.reject("InvalidVolume.NotFound")
		}
		if matches(v.Tags, tags) {
			return nil
		}
		if err := checkMutation(op, []cloud.Tags{tags}); err != nil {
			return err
		}
		next := c.state.snapshot()
		stored := next.Volumes[id]
		stored.Value.Tags = mergedTags(v.Tags, tags)
		next.Volumes[v.ID] = stored
		return c.save(next, op)
	case strings.HasPrefix(id, "eipalloc-"):
		a, exists := c.state.Addresses[id]
		if !exists {
			return op.reject("InvalidAllocationID.NotFound")
		}
		if matches(a.Tags, tags) {
			return nil
		}
		if err := checkMutation(op, []cloud.Tags{tags}); err != nil {
			return err
		}
		next := c.state.snapshot()
		a.Tags = mergedTags(a.Tags, tags)
		next.Addresses[a.AllocationID] = a
		return c.save(next, op)
	default:
		i, exists := c.state.Instances[id]
		if !exists {
			return op.reject("InvalidInstanceID.NotFound")
		}
		if matches(i.Value.Tags, tags) {
			return nil
		}
		if err := checkMutation(op, []cloud.Tags{tags}); err != nil {
			return err
		}
		next := c.state.snapshot()
		i.Value.Tags = mergedTags(i.Value.Tags, tags)
		next.Instances[i.Value.ID] = i
		return c.save(next, op)
	}
}
