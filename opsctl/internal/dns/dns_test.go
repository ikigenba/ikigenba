package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
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
			t.Fatalf("set %q: %v", key, err)
		}
	}
	return store
}

// R-YSWA-UVTZ
func TestVocabulary(t *testing.T) {
	if KeyProvider != "dns.provider" || KeyZones != "dns.zones" {
		t.Fatalf("keys = %q, %q", KeyProvider, KeyZones)
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
	providerType := reflect.TypeFor[Provider]()
	providerMethods := []struct {
		name string
		typ  reflect.Type
	}{
		{"Add", reflect.TypeFor[func(context.Context, string, string, string, int, string) error]()},
		{"Records", reflect.TypeFor[func(context.Context, string) ([]Record, error)]()},
		{"Remove", reflect.TypeFor[func(context.Context, string, string, string, string) error]()},
	}
	if providerType.NumMethod() != len(providerMethods) {
		t.Fatalf("Provider has %d methods, want %d", providerType.NumMethod(), len(providerMethods))
	}
	for i, want := range providerMethods {
		got := providerType.Method(i)
		if got.Name != want.name || got.Type != want.typ {
			t.Fatalf("Provider method %d = %s %s, want %s %s", i, got.Name, got.Type, want.name, want.typ)
		}
	}
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

// R-YU47-8NKO
func TestOpenRejectsIncompleteConfigurationBeforeProvider(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"missing provider", nil, "dns.provider not set"},
		{"empty provider", map[string]string{KeyProvider: ""}, "dns.provider not set"},
		{"missing zones", map[string]string{KeyProvider: "route53"}, "dns.zones not set"},
		{"empty zones", map[string]string{KeyProvider: "route53", KeyZones: ""}, "dns.zones not set"},
		{"missing separator", map[string]string{KeyProvider: "route53", KeyZones: "example.com"}, `dns.zones malformed: "example.com"`},
		{"empty name", map[string]string{KeyProvider: "route53", KeyZones: " : Z1"}, `dns.zones malformed: " : Z1"`},
		{"empty id", map[string]string{KeyProvider: "route53", KeyZones: "example.com: "}, `dns.zones malformed: "example.com: "`},
		{"later malformed", map[string]string{KeyProvider: "route53", KeyZones: "example.com:Z1, bad"}, `dns.zones malformed: " bad"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			_, err := Open(t.Context(), newStore(t, test.values), Env{Open: func(context.Context, string) (Provider, error) {
				called = true
				return &fakeProvider{}, nil
			}})
			if !errors.Is(err, ErrNotConfigured) || err.Error() != test.want {
				t.Fatalf("Open error = %v, want ErrNotConfigured with text %q", err, test.want)
			}
			if called {
				t.Fatal("provider opened before all configuration was validated")
			}
		})
	}
}

// R-YVC3-MFBD
func TestOpenNormalisesZonesAndPreservesOrder(t *testing.T) {
	store := newStore(t, map[string]string{
		KeyProvider: "route53",
		KeyZones:    " Example.COM. : Z1 , deep.EXAMPLE.com: Z2:part ",
	})
	client, err := Open(t.Context(), store, Env{Open: func(context.Context, string) (Provider, error) {
		return &fakeProvider{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Zone{{Name: "example.com", ID: "Z1"}, {Name: "deep.example.com", ID: "Z2:part"}}
	if !reflect.DeepEqual(client.Zones, want) {
		t.Fatalf("zones = %#v, want %#v", client.Zones, want)
	}
}

// R-L1RD-UPSF
func TestOpenProviderRegistry(t *testing.T) {
	store := newStore(t, map[string]string{
		KeyProvider: "route53",
		KeyZones:    "example.com:Z1",
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
	zoneOrders := [][]Zone{
		{{Name: "example.com", ID: "parent"}, {Name: "deep.example.com", ID: "child"}},
		{{Name: "deep.example.com", ID: "child"}, {Name: "example.com", ID: "parent"}},
	}
	for i, zones := range zoneOrders {
		client := Client{Zones: zones}
		zone, err := client.ZoneFor("API.DEEP.EXAMPLE.COM.")
		if err != nil || zone.ID != "child" {
			t.Fatalf("ZoneFor child with order %d = %#v, %v", i, zone, err)
		}
	}
	client := Client{Zones: zoneOrders[0]}
	zone, err := client.ZoneFor("EXAMPLE.COM.")
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
	client := Client{Provider: provider, Zones: []Zone{
		{Name: "example.com", ID: "parent"},
		{Name: "deep.example.com", ID: "child"},
	}}
	if err := client.Add(t.Context(), "Probe.DEEP.EXAMPLE.com.", "txt", 60, "Value"); !reflect.DeepEqual(err, addErr) {
		t.Fatalf("Add error = %v", err)
	}
	if want := []any{"child", "probe.deep.example.com", "TXT", 60, "Value"}; !reflect.DeepEqual(provider.addArgs, want) {
		t.Fatalf("Add args = %#v, want %#v", provider.addArgs, want)
	}
	if err := client.Remove(t.Context(), "Probe.DEEP.EXAMPLE.com.", "txt", "Value"); !reflect.DeepEqual(err, removeErr) {
		t.Fatalf("Remove error = %v", err)
	}
	if want := []string{"child", "probe.deep.example.com", "TXT", "Value"}; !reflect.DeepEqual(provider.removeArgs, want) {
		t.Fatalf("Remove args = %#v, want %#v", provider.removeArgs, want)
	}
}

// R-L5F3-010I
func TestCheckFindsApexRecordsAndComparesDelegationAsSet(t *testing.T) {
	apexNameservers := []string{"NS2.EXAMPLE.NET.", "ns1.example.net"}
	provider := &fakeProvider{records: []Record{
		{Name: "child.example.com", Type: "NS", Values: []string{"decoy.example.net"}},
		{Name: "EXAMPLE.COM.", Type: "SOA", Values: []string{"soa value"}},
		{Name: "example.com", Type: "NS", Values: apexNameservers},
	}}
	store := newStore(t, map[string]string{
		KeyProvider: "route53",
		KeyZones:    "example.com:Z1",
	})
	delegatedNameservers := []string{"ns1.example.net.", "ns2.example.net"}
	client, err := Open(t.Context(), store, Env{
		Open: func(context.Context, string) (Provider, error) { return provider, nil },
		LookupNS: func(_ context.Context, zone string) ([]string, error) {
			if zone != "example.com" {
				t.Fatalf("lookup zone = %q", zone)
			}
			return delegatedNameservers, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Check(t.Context(), client.Zones[0])
	if err != nil {
		t.Fatal(err)
	}
	if result.ZoneName != "EXAMPLE.COM." || !result.Delegated || !reflect.DeepEqual(result.Nameservers, apexNameservers) {
		t.Fatalf("Check = %#v", result)
	}
	if provider.recordsZone != "Z1" {
		t.Fatalf("Records zone = %q", provider.recordsZone)
	}

	delegatedNameservers = []string{"ns1.example.net", "wrong.example.net"}
	result, err = client.Check(t.Context(), client.Zones[0])
	if err != nil {
		t.Fatal(err)
	}
	if result.Delegated {
		t.Fatalf("Check reported delegation for different nameserver sets: %#v", result)
	}

	provider.records = []Record{{Name: "example.com", Type: "NS", Values: []string{"ns1.example.net"}}}
	if _, err := client.Check(t.Context(), client.Zones[0]); err == nil {
		t.Fatal("Check succeeded without SOA")
	}
}

// R-L6MZ-DSR7
func TestCheckUsesDefaultResolverWhenLookupNSIsNil(t *testing.T) {
	original := net.DefaultResolver
	served := make(chan error, 1)
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			client, server := net.Pipe()
			go func() {
				served <- serveNSResponse(server, "example.com", []string{"ns.example.net"})
			}()
			return client, nil
		},
	}
	t.Cleanup(func() { net.DefaultResolver = original })
	provider := &fakeProvider{records: []Record{
		{Name: "example.com", Type: "SOA"},
		{Name: "example.com", Type: "NS", Values: []string{"ns.example.net"}},
	}}
	store := newStore(t, map[string]string{
		KeyProvider: "route53",
		KeyZones:    "example.com:Z1",
	})
	client, err := Open(t.Context(), store, Env{
		Open: func(context.Context, string) (Provider, error) { return provider, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Check(t.Context(), client.Zones[0])
	if err != nil || !result.Delegated {
		t.Fatalf("Check = %#v, %v", result, err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func serveNSResponse(conn net.Conn, wantName string, nameservers []string) (returnErr error) {
	defer func() {
		returnErr = errors.Join(returnErr, conn.Close())
	}()
	answerHigh, answerLow, err := dnsWireLength(len(nameservers))
	if err != nil || answerHigh != 0 {
		return errors.New("too many DNS answers")
	}
	request := make([]byte, 512)
	n, err := conn.Read(request)
	if err != nil {
		return fmt.Errorf("read DNS request: %w", err)
	}
	request = request[:n]
	framed := len(request) >= 2 && int(binary.BigEndian.Uint16(request[:2])) == len(request)-2
	if framed {
		request = request[2:]
	}
	name, questionEnd, err := dnsQuestion(request)
	if err != nil {
		return err
	}
	if name != wantName {
		return fmt.Errorf("DNS query name = %q, want %q", name, wantName)
	}

	response := append([]byte(nil), request[:2]...)
	response = append(response, 0x81, 0x80, 0, 1, 0, answerLow, 0, 0, 0, 0)
	response = append(response, request[12:questionEnd]...)
	for _, nameserver := range nameservers {
		rdata, err := dnsName(nameserver)
		if err != nil {
			return err
		}
		rdataHigh, rdataLow, err := dnsWireLength(len(rdata))
		if err != nil {
			return err
		}
		response = append(response, 0xc0, 0x0c, 0, 2, 0, 1, 0, 0, 0, 60)
		response = append(response, rdataHigh, rdataLow)
		response = append(response, rdata...)
	}
	if framed {
		payload := response
		payloadHigh, payloadLow, err := dnsWireLength(len(payload))
		if err != nil {
			return err
		}
		response = []byte{payloadHigh, payloadLow}
		response = append(response, payload...)
	}
	if _, err := conn.Write(response); err != nil {
		return fmt.Errorf("write DNS response: %w", err)
	}
	return nil
}

func dnsQuestion(message []byte) (string, int, error) {
	if len(message) < 17 {
		return "", 0, errors.New("short DNS request")
	}
	labels := make([]string, 0)
	position := 12
	for {
		if position >= len(message) {
			return "", 0, errors.New("truncated DNS question")
		}
		length := int(message[position])
		position++
		if length == 0 {
			break
		}
		if position+length > len(message) {
			return "", 0, errors.New("truncated DNS label")
		}
		labels = append(labels, string(message[position:position+length]))
		position += length
	}
	if position+4 > len(message) || binary.BigEndian.Uint16(message[position:position+2]) != 2 {
		return "", 0, errors.New("request is not an NS question")
	}
	return strings.Join(labels, "."), position + 4, nil
}

func dnsName(name string) ([]byte, error) {
	var encoded []byte
	for _, label := range strings.Split(name, ".") {
		labelHigh, labelLow, err := dnsWireLength(len(label))
		if err != nil || labelHigh != 0 {
			return nil, errors.New("DNS label exceeds wire-format limit")
		}
		encoded = append(encoded, labelLow)
		encoded = append(encoded, label...)
	}
	return append(encoded, 0), nil
}

func dnsWireLength(length int) (byte, byte, error) {
	var high byte
	var low byte
	// DNS wire lengths occupy 16 bits, so high=255 and low=255 encode the
	// largest possible value, 65535; another byte cannot be represented.
	for range length {
		if high == 255 && low == 255 {
			return 0, 0, errors.New("DNS field exceeds wire-format limit")
		}
		if low == 255 {
			high++
			low = 0
		} else {
			low++
		}
	}
	return high, low, nil
}
