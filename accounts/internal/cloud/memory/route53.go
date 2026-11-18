package memory

import (
	"context"
	"slices"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func recordName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

// FindRecords returns records at the exact name in the sandbox's zone.
func (c *Cloud) FindRecords(_ context.Context, zone, name string) ([]cloud.Record, error) {
	if zone != c.cfg.Hosts.ZoneID {
		return nil, dns("ListResourceRecordSets", name).reject("NoSuchHostedZone")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r, exists := c.state.Records[recordName(name)]
	if !exists {
		return []cloud.Record{}, nil
	}
	r.Name, r.Values = name, slices.Clone(r.Values)
	return []cloud.Record{r}, nil
}

// ChangeRecord creates or deletes a matching record in the hosts zone.
func (c *Cloud) ChangeRecord(_ context.Context, zone string, change cloud.RecordChange) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := dns("ChangeResourceRecordSets", change.Name)
	if zone != c.cfg.Hosts.ZoneID {
		return "", op.reject("NoSuchHostedZone")
	}
	if err := checkMutation(op, nil, change.Name, change.IP); err != nil {
		return "", err
	}
	name := strings.Clone(recordName(change.Name))
	label, inDomain := strings.CutSuffix(name, "."+recordName(c.cfg.Hosts.Domain))
	if !inDomain || label == "" {
		return "", op.reject("InvalidChangeBatch")
	}
	r, exists := c.state.Records[name]
	switch change.Action {
	case cloud.RecordCreate:
		if exists {
			return "", op.reject("InvalidChangeBatch")
		}
	case cloud.RecordDelete:
		if !exists || r.Type != "A" || len(r.Values) != 1 || r.Values[0] != change.IP || r.TTL != change.TTL {
			return "", op.reject("InvalidChangeBatch")
		}
	default:
		return "", op.reject("InvalidChangeBatch")
	}
	id, err := randomID(c, op, "C", c.state.Changes)
	if err != nil {
		return "", err
	}
	next := c.state.snapshot()
	if change.Action == cloud.RecordDelete {
		delete(next.Records, name)
	} else {
		next.Records[name] = cloud.Record{Name: name, Type: "A", Values: []string{strings.Clone(change.IP)}, TTL: change.TTL}
	}
	next.Changes[id] = c.cfg.Now()
	if err := c.save(next, op); err != nil {
		return "", err
	}
	return id, nil
}

// ChangeInsync reports whether a change's propagation delay has passed.
func (c *Cloud) ChangeInsync(_ context.Context, id string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	changed, exists := c.state.Changes[id]
	if !exists {
		return false, dns("GetChange", id).reject("NoSuchChange")
	}
	return c.cfg.Now().Sub(changed) >= ChangeDelay, nil
}
