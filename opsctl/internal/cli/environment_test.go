package cli_test

import (
	"context"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

func TestRunPassesEnvironmentToDNSOperation(t *testing.T) {
	// R-5E43-77RM
	t.Setenv("CERTBOT_DOMAIN", "live.example")
	t.Setenv("CERTBOT_VALIDATION", "live-secret")
	provider := &fakeDNSProvider{records: map[string][]dns.Record{"ZONE": {{Name: "example.com", Type: "SOA"}, {Name: "example.com", Type: "NS", Values: []string{"ns.example"}}}}}
	deps := configuredDNSDeps(t, provider, "example.com")
	deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns.example"}, nil }
	deps.Getenv = nil
	stdout, stderr, code := invoke([]string{"dns", "acme-auth"}, deps)
	if code != 2 || stdout != "" || stderr != "opsctl: acme-auth must be run by certbot as --manual-auth-hook\n" {
		t.Fatalf("nil environment: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	deps.Getenv = func(key string) string {
		if key == "CERTBOT_DOMAIN" {
			return "example.com"
		}
		if key == "CERTBOT_VALIDATION" {
			return "injected-token"
		}
		return ""
	}
	stdout, stderr, code = invoke([]string{"dns", "acme-auth"}, deps)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("supplied environment: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if len(provider.adds) != 1 {
		t.Fatalf("DNS calls = %v", provider.adds)
	}
	call := provider.adds[0]
	if call.name != "_acme-challenge.example.com" || call.value != "injected-token" {
		t.Fatalf("environment not passed to DNS: %v", call)
	}
}

func TestRunNormalizesDefaultTimeBeforeDomainOperation(t *testing.T) {
	// R-5E43-77RM
	deps := depsAt(t, 0)
	store := config.Store{Root: deps.Root}
	for key, value := range map[string]string{
		"backup.s3_uri": "s3://bucket/host/",
		"aws.region":    "us-east-2",
	} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, code := invoke([]string{"backup"}, deps)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}
