package dns

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

type fakeProvider struct {
	records     []Record
	recordsErr  error
	recordsZone string
	addArgs     []any
	addErr      error
	removeArgs  []string
	removeErr   error
}

func (f *fakeProvider) Records(_ context.Context, zoneID string) ([]Record, error) {
	f.recordsZone = zoneID
	return f.records, f.recordsErr
}

func (f *fakeProvider) Add(_ context.Context, zoneID, name, typ string, ttl int, value string) error {
	f.addArgs = []any{zoneID, name, typ, ttl, value}
	return f.addErr
}

func (f *fakeProvider) Remove(_ context.Context, zoneID, name, typ, value string) error {
	f.removeArgs = []string{zoneID, name, typ, value}
	return f.removeErr
}

func newStore(t *testing.T, values map[string]string) config.Store {
	t.Helper()
	store := config.Store{Root: t.TempDir()}
	for key, value := range values {
		if err := store.Set(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	return store
}

// R-KT83-6BLK
func TestVocabulary(t *testing.T) {
	if KeyProvider != "dns.provider" || KeyZones != "dns.zones" {
		t.Fatalf("keys = %q, %q", KeyProvider, KeyZones)
	}
	if got := ZoneKey("route53", "example.com"); got != "dns.route53.zone.example.com" {
		t.Fatalf("ZoneKey = %q", got)
	}
	if ErrNotConfigured.Error() != "dns is not configured" ||
		ErrNoZone.Error() != "no configured zone contains the name" ||
		ErrUnknownProvider.Error() != "unknown dns provider" {
		t.Fatal("exported sentinel errors have unexpected text")
	}
}

// R-KUFZ-K3C9 R-KVNV-XV2Y R-KWVS-BMTN R-KY3O-PEKC
func TestExportedShapesAndSignatures(t *testing.T) {
	typesAndFields := []struct {
		value any
		names []string
		types []reflect.Type
	}{
		{Zone{}, []string{"Name", "ID"}, []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[string]()}},
		{Record{}, []string{"Name", "Type", "TTL", "Values"}, []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[string](), reflect.TypeFor[int](), reflect.TypeFor[[]string]()}},
		{Env{}, []string{"Open", "LookupNS"}, []reflect.Type{reflect.TypeFor[func(context.Context, string) (Provider, error)](), reflect.TypeFor[func(context.Context, string) ([]string, error)]()}},
		{CheckResult{}, []string{"ZoneName", "Nameservers", "Delegated"}, []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[[]string](), reflect.TypeFor[bool]()}},
		{Client{}, []string{"Provider", "Zones"}, []reflect.Type{reflect.TypeFor[Provider](), reflect.TypeFor[[]Zone]()}},
	}
	for _, item := range typesAndFields {
		typ := reflect.TypeOf(item.value)
		if typ.NumField() != len(item.names) {
			t.Fatalf("%s has %d fields", typ.Name(), typ.NumField())
		}
		for i, name := range item.names {
			field := typ.Field(i)
			if field.Name != name || field.Type != item.types[i] {
				t.Fatalf("%s field %d = %s %s", typ.Name(), i, field.Name, field.Type)
			}
		}
	}

	var _ Provider = (*fakeProvider)(nil)
	signatures := []struct {
		got  reflect.Type
		want reflect.Type
	}{
		{reflect.TypeOf(Open), reflect.TypeFor[func(context.Context, config.Store, Env) (*Client, error)]()},
		{reflect.TypeOf((*Client).ZoneFor), reflect.TypeFor[func(*Client, string) (Zone, error)]()},
		{reflect.TypeOf((*Client).Records), reflect.TypeFor[func(*Client, context.Context, Zone) ([]Record, error)]()},
		{reflect.TypeOf((*Client).Add), reflect.TypeFor[func(*Client, context.Context, string, string, int, string) error]()},
		{reflect.TypeOf((*Client).Remove), reflect.TypeFor[func(*Client, context.Context, string, string, string) error]()},
		{reflect.TypeOf((*Client).Check), reflect.TypeFor[func(*Client, context.Context, Zone) (CheckResult, error)]()},
	}
	for _, signature := range signatures {
		if signature.got != signature.want {
			t.Fatalf("signature = %s, want %s", signature.got, signature.want)
		}
	}
}

// R-KZBL-36B1
func TestOpenRejectsIncompleteConfigurationBeforeProvider(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]string
		wantKey string
	}{
		{"missing provider", nil, KeyProvider},
		{"empty provider", map[string]string{KeyProvider: ""}, KeyProvider},
		{"missing zones", map[string]string{KeyProvider: "route53"}, KeyZones},
		{"empty zones", map[string]string{KeyProvider: "route53", KeyZones: ""}, KeyZones},
		{"missing zone id", map[string]string{KeyProvider: "route53", KeyZones: "example.com"}, ZoneKey("route53", "example.com")},
		{"empty zone id", map[string]string{KeyProvider: "route53", KeyZones: "example.com", ZoneKey("route53", "example.com"): ""}, ZoneKey("route53", "example.com")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			_, err := Open(t.Context(), newStore(t, test.values), Env{Open: func(context.Context, string) (Provider, error) {
				called = true
				return &fakeProvider{}, nil
			}})
			if !errors.Is(err, ErrNotConfigured) || !strings.Contains(err.Error(), test.wantKey) {
				t.Fatalf("Open error = %v, want ErrNotConfigured naming %q", err, test.wantKey)
			}
			if called {
				t.Fatal("provider opened before all configuration was validated")
			}
		})
	}
}

// R-L0JH-GY1Q
func TestOpenNormalisesZonesAndPreservesOrder(t *testing.T) {
	store := newStore(t, map[string]string{
		KeyProvider:                            "route53",
		KeyZones:                               " Example.COM. , deep.EXAMPLE.com ",
		ZoneKey("route53", "example.com"):      "Z1",
		ZoneKey("route53", "deep.example.com"): "Z2",
	})
	client, err := Open(t.Context(), store, Env{Open: func(context.Context, string) (Provider, error) {
		return &fakeProvider{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Zone{{Name: "example.com", ID: "Z1"}, {Name: "deep.example.com", ID: "Z2"}}
	if !reflect.DeepEqual(client.Zones, want) {
		t.Fatalf("zones = %#v, want %#v", client.Zones, want)
	}
}

// R-L1RD-UPSF
func TestOpenProviderRegistry(t *testing.T) {
	store := newStore(t, map[string]string{
		KeyProvider:                       "route53",
		KeyZones:                          "example.com",
		ZoneKey("route53", "example.com"): "Z1",
	})
	sentinel := errors.New("open failed")
	var gotProvider string
	_, err := Open(t.Context(), store, Env{Open: func(_ context.Context, provider string) (Provider, error) {
		gotProvider = provider
		return nil, sentinel
	}})
	if !reflect.DeepEqual(err, sentinel) || gotProvider != "route53" {
		t.Fatalf("Open = %v, provider %q", err, gotProvider)
	}
	_, err = Open(t.Context(), store, Env{})
	if !errors.Is(err, ErrUnknownProvider) || !strings.Contains(err.Error(), "route53") {
		t.Fatalf("nil registry error = %v", err)
	}
}

// R-L2ZA-8HJ4
func TestZoneForUsesLongestLabelBoundedSuffix(t *testing.T) {
	client := Client{Zones: []Zone{{Name: "example.com", ID: "parent"}, {Name: "deep.example.com", ID: "child"}}}
	zone, err := client.ZoneFor("API.DEEP.EXAMPLE.COM.")
	if err != nil || zone.ID != "child" {
		t.Fatalf("ZoneFor child = %#v, %v", zone, err)
	}
	zone, err = client.ZoneFor("EXAMPLE.COM.")
	if err != nil || zone.ID != "parent" {
		t.Fatalf("ZoneFor apex = %#v, %v", zone, err)
	}
	for _, name := range []string{"notexample.com", "example.com.invalid"} {
		_, err = client.ZoneFor(name)
		if !errors.Is(err, ErrNoZone) {
			t.Fatalf("ZoneFor(%q) error = %v", name, err)
		}
	}
}

// R-L476-M99T
func TestClientDelegatesNormalisedMutationsAndPreservesErrors(t *testing.T) {
	addErr := errors.New("add failed")
	removeErr := errors.New("remove failed")
	provider := &fakeProvider{addErr: addErr, removeErr: removeErr}
	client := Client{Provider: provider, Zones: []Zone{{Name: "example.com", ID: "Z1"}}}
	if err := client.Add(t.Context(), "Probe.EXAMPLE.com.", "txt", 60, "Value"); !reflect.DeepEqual(err, addErr) {
		t.Fatalf("Add error = %v", err)
	}
	if want := []any{"Z1", "probe.example.com", "TXT", 60, "Value"}; !reflect.DeepEqual(provider.addArgs, want) {
		t.Fatalf("Add args = %#v, want %#v", provider.addArgs, want)
	}
	if err := client.Remove(t.Context(), "Probe.EXAMPLE.com.", "txt", "Value"); !reflect.DeepEqual(err, removeErr) {
		t.Fatalf("Remove error = %v", err)
	}
	if want := []string{"Z1", "probe.example.com", "TXT", "Value"}; !reflect.DeepEqual(provider.removeArgs, want) {
		t.Fatalf("Remove args = %#v, want %#v", provider.removeArgs, want)
	}
}

// R-L5F3-010I
func TestCheckFindsApexRecordsAndComparesDelegationAsSet(t *testing.T) {
	provider := &fakeProvider{records: []Record{
		{Name: "example.com", Type: "NS", Values: []string{"NS2.EXAMPLE.NET.", "ns1.example.net"}},
		{Name: "example.com", Type: "SOA", Values: []string{"soa value"}},
	}}
	store := newStore(t, map[string]string{
		KeyProvider:                       "route53",
		KeyZones:                          "example.com",
		ZoneKey("route53", "example.com"): "Z1",
	})
	client, err := Open(t.Context(), store, Env{
		Open: func(context.Context, string) (Provider, error) { return provider, nil },
		LookupNS: func(_ context.Context, zone string) ([]string, error) {
			if zone != "example.com" {
				t.Fatalf("lookup zone = %q", zone)
			}
			return []string{"ns1.example.net.", "ns2.example.net"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Check(t.Context(), client.Zones[0])
	if err != nil {
		t.Fatal(err)
	}
	if result.ZoneName != "example.com" || !result.Delegated || !reflect.DeepEqual(result.Nameservers, provider.records[0].Values) {
		t.Fatalf("Check = %#v", result)
	}
	if provider.recordsZone != "Z1" {
		t.Fatalf("Records zone = %q", provider.recordsZone)
	}

	provider.records = []Record{{Name: "example.com", Type: "NS", Values: []string{"ns1.example.net"}}}
	if _, err := client.Check(t.Context(), client.Zones[0]); err == nil {
		t.Fatal("Check succeeded without SOA")
	}
}

// R-L6MZ-DSR7
func TestCheckUsesDefaultResolverWhenLookupNSIsNil(t *testing.T) {
	original := defaultLookupNS
	t.Cleanup(func() { defaultLookupNS = original })
	called := false
	defaultLookupNS = func(_ context.Context, zone string) ([]string, error) {
		called = true
		if zone != "example.com" {
			t.Fatalf("lookup zone = %q", zone)
		}
		return []string{"ns.example.net."}, nil
	}
	provider := &fakeProvider{records: []Record{
		{Name: "example.com", Type: "SOA"},
		{Name: "example.com", Type: "NS", Values: []string{"ns.example.net"}},
	}}
	client := Client{Provider: providerWithResolver{Provider: provider}, Zones: []Zone{{Name: "example.com", ID: "Z1"}}}
	result, err := client.Check(t.Context(), client.Zones[0])
	if err != nil || !called || !result.Delegated {
		t.Fatalf("Check = %#v, %v; default called = %v", result, err, called)
	}
}
