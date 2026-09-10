// Package dns defines DNS records and the provider seam.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

const (
	// KeyProvider identifies the configured DNS provider.
	KeyProvider = "dns.provider"
	// KeyZones identifies the comma-separated configured DNS zones.
	KeyZones = "dns.zones"
)

var (
	// ErrNotConfigured identifies missing DNS configuration.
	ErrNotConfigured = errors.New("dns is not configured")
	// ErrNoZone identifies a name outside every configured zone.
	ErrNoZone = errors.New("no configured zone contains the name")
	// ErrUnknownProvider identifies a provider name absent from the registry.
	ErrUnknownProvider = errors.New("unknown dns provider")
)

// Zone associates a DNS name with its provider identifier.
type Zone struct {
	Name string
	ID   string
}

// Record is one DNS record set.
type Record struct {
	Name   string
	Type   string
	TTL    int
	Values []string
}

// Provider reads and changes records at a DNS provider.
type Provider interface {
	Records(ctx context.Context, zoneID string) ([]Record, error)
	Add(ctx context.Context, zoneID, name, typ string, ttl int, value string) error
	Remove(ctx context.Context, zoneID, name, typ, value string) error
}

// Env is what the process supplies; both fields are optional.
type Env struct {
	Open     func(ctx context.Context, provider string) (Provider, error)
	LookupNS func(ctx context.Context, zone string) ([]string, error)
}

// Client performs DNS operations within the configured zones.
type Client struct {
	Provider Provider
	Zones    []Zone
}

// CheckResult describes a zone's provider identity and public delegation.
type CheckResult struct {
	ZoneName    string
	Nameservers []string
	Delegated   bool
}

type providerWithResolver struct {
	Provider
	lookupNS func(context.Context, string) ([]string, error)
}

// Open loads and validates DNS configuration before opening its provider.
func Open(ctx context.Context, store config.Store, env Env) (*Client, error) {
	provider, err := required(store, KeyProvider)
	if err != nil {
		return nil, err
	}
	zonesValue, err := required(store, KeyZones)
	if err != nil {
		return nil, err
	}

	entries := strings.Split(zonesValue, ",")
	zones := make([]Zone, 0, len(entries))
	for _, entry := range entries {
		name, id, ok := strings.Cut(entry, ":")
		name = strings.TrimSpace(name)
		id = strings.TrimSpace(id)
		if !ok || name == "" || id == "" {
			return nil, notConfigured(fmt.Sprintf("dns.zones malformed: %q", entry))
		}
		zones = append(zones, Zone{Name: normalise(name), ID: id})
	}

	if env.Open == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
	opened, err := env.Open(ctx, provider)
	if err != nil {
		return nil, err
	}
	return &Client{
		Provider: providerWithResolver{Provider: opened, lookupNS: env.LookupNS},
		Zones:    zones,
	}, nil
}

func required(store config.Store, key string) (string, error) {
	value, err := store.Get(key)
	if err != nil {
		if errors.Is(err, config.ErrNotSet) {
			return "", notConfigured(key + " not set")
		}
		return "", err
	}
	if value == "" {
		return "", notConfigured(key + " not set")
	}
	return value, nil
}

type configurationError struct {
	message string
}

func (e configurationError) Error() string {
	return e.message
}

func (e configurationError) Unwrap() error {
	return ErrNotConfigured
}

func notConfigured(message string) error {
	return configurationError{message: message}
}

func normalise(name string) string {
	return strings.TrimSuffix(strings.ToLower(name), ".")
}

// ZoneFor returns the most-specific configured zone containing name.
func (c *Client) ZoneFor(name string) (Zone, error) {
	name = normalise(name)
	best := Zone{}
	found := false
	for _, zone := range c.Zones {
		if name == zone.Name || strings.HasSuffix(name, "."+zone.Name) {
			if !found || len(zone.Name) > len(best.Name) {
				best = zone
				found = true
			}
		}
	}
	if !found {
		return Zone{}, fmt.Errorf("%w: %q", ErrNoZone, name)
	}
	return best, nil
}

// Records returns all records for zone.
func (c *Client) Records(ctx context.Context, zone Zone) ([]Record, error) {
	return c.Provider.Records(ctx, zone.ID)
}

// Add adds value to a DNS record set.
func (c *Client) Add(ctx context.Context, name, typ string, ttl int, value string) error {
	name = normalise(name)
	zone, err := c.ZoneFor(name)
	if err != nil {
		return err
	}
	return c.Provider.Add(ctx, zone.ID, name, strings.ToUpper(typ), ttl, value)
}

// Remove removes value from a DNS record set.
func (c *Client) Remove(ctx context.Context, name, typ, value string) error {
	name = normalise(name)
	zone, err := c.ZoneFor(name)
	if err != nil {
		return err
	}
	return c.Provider.Remove(ctx, zone.ID, name, strings.ToUpper(typ), value)
}

// Check compares a zone's provider records with its public delegation.
func (c *Client) Check(ctx context.Context, zone Zone) (CheckResult, error) {
	records, err := c.Provider.Records(ctx, zone.ID)
	if err != nil {
		return CheckResult{}, err
	}

	result := CheckResult{}
	for _, record := range records {
		if strings.EqualFold(record.Type, "SOA") {
			result.ZoneName = record.Name
			break
		}
	}
	if result.ZoneName == "" {
		return CheckResult{}, fmt.Errorf("zone %q has no SOA record", zone.Name)
	}
	for _, record := range records {
		if strings.EqualFold(record.Type, "NS") && normalise(record.Name) == normalise(result.ZoneName) {
			result.Nameservers = append([]string(nil), record.Values...)
			break
		}
	}

	var delegated []string
	if wrapped, ok := c.Provider.(providerWithResolver); ok && wrapped.lookupNS != nil {
		delegated, err = wrapped.lookupNS(ctx, zone.Name)
	} else {
		delegated, err = resolveDefaultNS(ctx, zone.Name)
	}
	if err != nil {
		return CheckResult{}, err
	}
	result.Delegated = sameNames(result.Nameservers, delegated)
	return result, nil
}

func resolveDefaultNS(ctx context.Context, zoneName string) ([]string, error) {
	records, err := net.DefaultResolver.LookupNS(ctx, zoneName)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(records))
	for i, record := range records {
		names[i] = record.Host
	}
	return names, nil
}

func sameNames(left, right []string) bool {
	left = normalisedNames(left)
	right = normalisedNames(right)
	left = uniqueNames(left)
	right = uniqueNames(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func normalisedNames(names []string) []string {
	result := make([]string, len(names))
	for i, name := range names {
		result[i] = normalise(name)
	}
	sort.Strings(result)
	return result
}

func uniqueNames(names []string) []string {
	if len(names) < 2 {
		return names
	}
	result := names[:1]
	for _, name := range names[1:] {
		if name != result[len(result)-1] {
			result = append(result, name)
		}
	}
	return result
}
