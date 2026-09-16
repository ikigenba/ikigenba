package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

type dnsCheckRenderProvider struct {
	records map[string][]dns.Record
	errors  map[string]error
	calls   []string
}

func (p *dnsCheckRenderProvider) Records(_ context.Context, zoneID string) ([]dns.Record, error) {
	p.calls = append(p.calls, zoneID)
	if err := p.errors[zoneID]; err != nil {
		return nil, err
	}
	return p.records[zoneID], nil
}

func (*dnsCheckRenderProvider) Add(context.Context, string, string, string, int, string) error {
	return nil
}

func (*dnsCheckRenderProvider) Remove(context.Context, string, string, string, string) error {
	return nil
}

func TestDNSCheckEncodesEveryInterpolatedField(t *testing.T) {
	// R-F6JK-M61Q
	const (
		providerName    = "route\r\n53"
		okZone          = "ok\nzone"
		okID            = "OK\r\nID"
		wrongZone       = "wrong\rzone"
		wrongID         = "WRONG\nID"
		undelegatedZone = "undelegated\nzone"
		undelegatedID   = "UNDELEGATED\rID"
		errorZone       = "error\r\nzone"
		errorID         = "ERROR\nID"
	)

	provider := &dnsCheckRenderProvider{
		records: map[string][]dns.Record{
			okID: {
				{Name: okZone, Type: "SOA"},
				{Name: okZone, Type: "NS", Values: []string{"ns.ok"}},
			},
			wrongID: {
				{Name: "provider\nzone\rname", Type: "SOA"},
				{Name: "provider\nzone\rname", Type: "NS", Values: []string{"ns.wrong"}},
			},
			undelegatedID: {
				{Name: undelegatedZone, Type: "SOA"},
				{Name: undelegatedZone, Type: "NS", Values: []string{"ns.expected"}},
			},
		},
		errors: map[string]error{errorID: errors.New("resolver\nfailed\rhard")},
	}
	store := config.Store{Root: t.TempDir()}
	if err := store.Set(dns.KeyProvider, "route53"); err != nil {
		t.Fatalf("set provider: %v", err)
	}
	if err := store.Set(dns.KeyZones, "seed.test:SEED"); err != nil {
		t.Fatalf("set zones: %v", err)
	}
	client, err := dns.Open(context.Background(), store, dns.Env{
		Open: func(context.Context, string) (dns.Provider, error) { return provider, nil },
		LookupNS: func(_ context.Context, zone string) ([]string, error) {
			switch zone {
			case okZone:
				return []string{"ns.ok"}, nil
			case wrongZone:
				return []string{"ns.wrong"}, nil
			case undelegatedZone:
				return []string{"ns.actual"}, nil
			default:
				t.Fatalf("lookup unexpected zone %q", zone)
				return nil, nil
			}
		},
	})
	if err != nil {
		t.Fatalf("open client: %v", err)
	}
	client.Zones = []dns.Zone{
		{Name: okZone, ID: okID},
		{Name: wrongZone, ID: wrongID},
		{Name: undelegatedZone, ID: undelegatedID},
		{Name: errorZone, ID: errorID},
	}

	var stdout strings.Builder
	code := dnsCheck(&stdout, client, providerName)
	want := "ok\\nzone: ok (route\\r\\n53 OK\\r\\nID, 1 nameservers delegated)\n" +
		"wrong\\rzone: failed: provider zone name is provider\\nzone\\rname\n" +
		"undelegated\\nzone: failed: nameservers not delegated\n" +
		"error\\r\\nzone: failed: resolver\\nfailed\\rhard\n"
	if code != exitFail || stdout.String() != want {
		t.Errorf("check: exit %d stdout %q, want %q", code, stdout.String(), want)
	}
	if lines := strings.Count(stdout.String(), "\n"); lines != len(client.Zones) {
		t.Errorf("check wrote %d lines for %d zones: %q", lines, len(client.Zones), stdout.String())
	}
	if got := strings.Join(provider.calls, "|"); got != strings.Join([]string{okID, wrongID, undelegatedID, errorID}, "|") {
		t.Errorf("check order = %q, want every configured zone in order", got)
	}
}
