package cli_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

const wantDNSUsage = `Usage: opsctl dns <subcommand> [options] [arguments]

Manage DNS records in the zones opsctl owns, through the configured provider.

Subcommands:
  list ZONE               print every record in ZONE, one line per value
  add NAME TYPE VALUE     add VALUE to the record NAME/TYPE, creating it if absent
  remove NAME TYPE VALUE  remove VALUE from the record NAME/TYPE; succeeds if absent
  check                   verify every configured zone is reachable and delegated
  acme-auth               certbot --manual-auth-hook: add the DNS-01 challenge record
  acme-cleanup            certbot --manual-cleanup-hook: remove the challenge record

Options (add, remove, acme-auth, acme-cleanup):
  --timeout DURATION  how long to wait for the change to be live (default 2m)
  --ttl SECONDS       TTL when add creates a record (default 300; acme-auth uses 60)

Configuration keys:
  dns.provider                the active provider; only 'route53' is supported
  dns.zones                   comma-separated zones opsctl owns
  dns.<provider>.zone.<zone>  the provider's id for <zone>

NAME is mapped to a zone by longest suffix match against dns.zones.
`

type dnsCall struct {
	zoneID   string
	name     string
	typ      string
	ttl      int
	value    string
	deadline time.Time
}

type fakeDNSProvider struct {
	records    map[string][]dns.Record
	recordsErr map[string]error
	addErr     error
	removeErr  error
	adds       []dnsCall
	removes    []dnsCall
}

func (p *fakeDNSProvider) Records(_ context.Context, zoneID string) ([]dns.Record, error) {
	if err := p.recordsErr[zoneID]; err != nil {
		return nil, err
	}
	return p.records[zoneID], nil
}

func (p *fakeDNSProvider) Add(ctx context.Context, zoneID, name, typ string, ttl int, value string) error {
	deadline, _ := ctx.Deadline()
	p.adds = append(p.adds, dnsCall{zoneID, name, typ, ttl, value, deadline})
	return p.addErr
}

func (p *fakeDNSProvider) Remove(ctx context.Context, zoneID, name, typ, value string) error {
	deadline, _ := ctx.Deadline()
	p.removes = append(p.removes, dnsCall{zoneID: zoneID, name: name, typ: typ, value: value, deadline: deadline})
	return p.removeErr
}

func TestDNSHelp(t *testing.T) {
	// R-LHM2-TQFG
	for _, help := range []string{"--help", "-h"} {
		stdout, stderr, code := invoke([]string{"dns", help}, depsAt(t, 1))
		if code != 0 || stdout != wantDNSUsage || stderr != "" {
			t.Errorf("%s: exit %d stdout %q stderr %q", help, code, stdout, stderr)
		}
	}
}

func TestDNSMissingAndUnknownSubcommand(t *testing.T) {
	// R-LITZ-7I65
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"dns"}, "opsctl: no dns subcommand given\n\nsee 'opsctl dns --help' for usage\n"},
		{[]string{"dns", "wat"}, "opsctl: unknown dns subcommand 'wat'\n\nsee 'opsctl dns --help' for usage\n"},
	}
	for _, tc := range cases {
		stdout, stderr, code := invoke(tc.args, depsAt(t, 0))
		if code != 2 || stdout != "" || stderr != tc.want {
			t.Errorf("%q: exit %d stdout %q stderr %q, want %q", tc.args, code, stdout, stderr, tc.want)
		}
	}
}

func TestDNSCommandsOpenConfiguredStore(t *testing.T) {
	// R-LK1V-L9WU
	for _, args := range [][]string{
		{"dns", "list", "example.com"},
		{"dns", "add", "a.example.com", "TXT", "v"},
		{"dns", "remove", "a.example.com", "TXT", "v"},
		{"dns", "check"},
		{"dns", "acme-auth"},
		{"dns", "acme-cleanup"},
	} {
		provider := &fakeDNSProvider{records: map[string][]dns.Record{
			"ZONE": {{Name: "example.com", Type: "SOA"}, {Name: "example.com", Type: "NS", Values: []string{"ns.example"}}},
		}}
		opened := 0
		deps := configuredDNSDeps(t, provider, "example.com")
		deps.DNS.Open = func(_ context.Context, name string) (dns.Provider, error) {
			opened++
			if name != "route53" {
				t.Errorf("provider = %q", name)
			}
			return provider, nil
		}
		deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns.example"}, nil }
		deps.Getenv = func(key string) string {
			if key == "CERTBOT_DOMAIN" {
				return "example.com"
			}
			return "token"
		}
		_, _, _ = invoke(args, deps)
		if opened != 1 {
			t.Errorf("%q: Env.Open calls = %d, want 1", args, opened)
		}
	}

	missing := []struct {
		key  string
		seed map[string]string
	}{
		{dns.KeyProvider, nil},
		{dns.KeyZones, map[string]string{dns.KeyProvider: "route53"}},
		{dns.ZoneKey("route53", "example.com"), map[string]string{dns.KeyProvider: "route53", dns.KeyZones: "example.com"}},
	}
	for _, tc := range missing {
		deps := depsAt(t, 0)
		for key, value := range tc.seed {
			if err := (config.Store{Root: deps.Root}).Set(key, value); err != nil {
				t.Fatal(err)
			}
		}
		deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
			t.Error("Env.Open called for incomplete configuration")
			return nil, errors.New("unexpected")
		}
		stdout, stderr, code := invoke([]string{"dns", "list", "example.com"}, deps)
		want := "opsctl: " + tc.key + " not set\n"
		if code != 1 || stdout != "" || stderr != want {
			t.Errorf("missing %s: exit %d stdout %q stderr %q, want %q", tc.key, code, stdout, stderr, want)
		}
	}
}

func TestDNSList(t *testing.T) {
	// R-LL9R-Z1NJ
	provider := &fakeDNSProvider{records: map[string][]dns.Record{"ZONE": {
		{Name: "z.example.com", Type: "TXT", TTL: 10, Values: []string{"second", "first value"}},
		{Name: "a.example.com", Type: "AAAA", TTL: 20, Values: []string{"::1"}},
		{Name: "a.example.com", Type: "A", TTL: 30, Values: []string{"192.0.2.1"}},
	}}}
	deps := configuredDNSDeps(t, provider, "example.com", "other.test")
	stdout, stderr, code := invoke([]string{"dns", "list", "EXAMPLE.COM."}, deps)
	want := "a.example.com A 30 192.0.2.1\na.example.com AAAA 20 ::1\nz.example.com TXT 10 first value\nz.example.com TXT 10 second\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("list: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, want)
	}

	stdout, stderr, code = invoke([]string{"dns", "list", "missing.test"}, deps)
	wantErr := "opsctl: zone not configured: missing.test\n\nconfigured zones: example.com,other.test\n"
	if code != 2 || stdout != "" || stderr != wantErr {
		t.Errorf("unconfigured: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, wantErr)
	}
}

func TestDNSAddAndRemove(t *testing.T) {
	// R-LMHO-CTE8
	provider := &fakeDNSProvider{}
	deps := configuredDNSDeps(t, provider, "example.com")
	start := time.Now()
	stdout, stderr, code := invoke([]string{"dns", "add", "--ttl", "45", "--timeout", "3s", "A.Example.com.", "txt", "hello"}, deps)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("add: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	assertDNSCall(t, provider.adds, "a.example.com", 45, "hello", start.Add(3*time.Second))

	start = time.Now()
	stdout, stderr, code = invoke([]string{"dns", "remove", "--timeout", "5s", "a.example.com", "txt", "hello"}, deps)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("remove: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	assertDNSCall(t, provider.removes, "a.example.com", 0, "hello", start.Add(5*time.Second))

	defaultProvider := &fakeDNSProvider{}
	start = time.Now()
	stdout, stderr, code = invoke(
		[]string{"dns", "add", "default.example.com", "TXT", "value"},
		configuredDNSDeps(t, defaultProvider, "example.com"),
	)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("default add: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	assertDNSCall(t, defaultProvider.adds, "default.example.com", 300, "value", start.Add(2*time.Minute))
}

func TestDNSChangeUsageAndZoneErrors(t *testing.T) {
	// R-LNPK-QL4X
	for _, args := range [][]string{
		{"dns", "add", "name", "TXT"},
		{"dns", "remove", "name", "TXT", "value", "extra"},
		{"dns", "add", "--ttl", "0", "name", "TXT", "value"},
		{"dns", "add", "--ttl", "no", "name", "TXT", "value"},
		{"dns", "remove", "--timeout", "0s", "name", "TXT", "value"},
		{"dns", "add", "--timeout", "forever", "name", "TXT", "value"},
	} {
		stdout, stderr, code := invoke(args, configuredDNSDeps(t, &fakeDNSProvider{}, "example.com"))
		if code != 2 || stdout != "" || stderr == "" {
			t.Errorf("%q: exit %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
	stdout, stderr, code := invoke([]string{"dns", "add", "x.invalid", "TXT", "v"}, configuredDNSDeps(t, &fakeDNSProvider{}, "example.com"))
	want := "opsctl: no configured zone contains 'x.invalid'\n\nconfigured zones: example.com\n"
	if code != 2 || stdout != "" || stderr != want {
		t.Errorf("no zone: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, want)
	}
}

func TestDNSChangeFailures(t *testing.T) {
	// R-LOXH-4CVM
	for _, tc := range []struct {
		subcommand string
		args       []string
	}{
		{"add", []string{"x.example.com", "TXT", "v"}},
		{"remove", []string{"x.example.com", "TXT", "v"}},
		{"acme-auth", nil},
		{"acme-cleanup", nil},
	} {
		provider := &fakeDNSProvider{
			addErr:    fmt.Errorf("waiting: %w", context.DeadlineExceeded),
			removeErr: fmt.Errorf("waiting: %w", context.DeadlineExceeded),
		}
		deps := configuredDNSDeps(t, provider, "example.com")
		deps.Getenv = func(key string) string {
			if key == "CERTBOT_DOMAIN" {
				return "example.com"
			}
			return "v"
		}
		args := append([]string{"dns", tc.subcommand, "--timeout", "7s"}, tc.args...)
		stdout, stderr, code := invoke(args, deps)
		name := "x.example.com"
		if strings.HasPrefix(tc.subcommand, "acme-") {
			name = "_acme-challenge.example.com"
		}
		want := "opsctl: change to " + name + " not confirmed within 7s\n"
		if code != 1 || stdout != "" || stderr != want {
			t.Errorf("%s deadline: exit %d stdout %q stderr %q, want %q", tc.subcommand, code, stdout, stderr, want)
		}
	}

	for _, tc := range []struct {
		subcommand string
		args       []string
	}{
		{"add", []string{"x.example.com", "TXT", "v"}},
		{"remove", []string{"x.example.com", "TXT", "v"}},
		{"acme-auth", nil},
		{"acme-cleanup", nil},
	} {
		provider := &fakeDNSProvider{
			addErr:    errors.New("provider exploded"),
			removeErr: errors.New("provider exploded"),
		}
		deps := configuredDNSDeps(t, provider, "example.com")
		deps.Getenv = func(key string) string {
			if key == "CERTBOT_DOMAIN" {
				return "example.com"
			}
			return "v"
		}
		args := append([]string{"dns", tc.subcommand}, tc.args...)
		stdout, stderr, code := invoke(args, deps)
		if code != 1 || stdout != "" || stderr != "opsctl: provider exploded\n" {
			t.Errorf("%s provider: exit %d stdout %q stderr %q", tc.subcommand, code, stdout, stderr)
		}
	}
}

func TestDNSCheckAllZones(t *testing.T) {
	// R-LQ5D-I4MB
	provider := &fakeDNSProvider{
		records: map[string][]dns.Record{
			"ZONE": {
				{Name: "example.com", Type: "SOA"},
				{Name: "example.com", Type: "NS", Values: []string{"ns1.example", "ns2.example"}},
			},
			"ZONE2": {
				{Name: "provider.test", Type: "SOA"},
				{Name: "provider.test", Type: "NS", Values: []string{"ns.provider"}},
			},
			"ZONE3": {
				{Name: "undelegated.test", Type: "SOA"},
				{Name: "undelegated.test", Type: "NS", Values: []string{"ns.expected"}},
			},
		},
		recordsErr: map[string]error{"ZONE4": errors.New("unreachable")},
	}
	deps := configuredDNSDeps(t, provider, "example.com", "wrong.test", "undelegated.test", "other.test")
	deps.DNS.LookupNS = func(_ context.Context, zone string) ([]string, error) {
		switch zone {
		case "example.com":
			return []string{"NS2.EXAMPLE.", "ns1.example"}, nil
		case "wrong.test":
			return []string{"ns.provider"}, nil
		case "undelegated.test":
			return []string{"ns.actual"}, nil
		default:
			t.Fatalf("lookup unexpected zone %q", zone)
			return nil, nil
		}
	}
	stdout, stderr, code := invoke([]string{"dns", "check"}, deps)
	want := "example.com: ok (route53 ZONE, 2 nameservers delegated)\n" +
		"wrong.test: failed: provider reports zone provider.test\n" +
		"undelegated.test: failed: nameservers are not delegated\n" +
		"other.test: failed: unreachable\n"
	if code != 1 || stdout != want || stderr != "" {
		t.Errorf("check: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, want)
	}

	healthyProvider := &fakeDNSProvider{records: map[string][]dns.Record{
		"ZONE": {
			{Name: "example.com", Type: "SOA"},
			{Name: "example.com", Type: "NS", Values: []string{"ns1.example"}},
		},
		"ZONE2": {
			{Name: "other.test", Type: "SOA"},
			{Name: "other.test", Type: "NS", Values: []string{"ns1.other"}},
		},
	}}
	healthyDeps := configuredDNSDeps(t, healthyProvider, "example.com", "other.test")
	healthyDeps.DNS.LookupNS = func(_ context.Context, zone string) ([]string, error) {
		switch zone {
		case "example.com":
			return []string{"ns1.example"}, nil
		case "other.test":
			return []string{"ns1.other"}, nil
		default:
			t.Fatalf("lookup unexpected zone %q", zone)
			return nil, nil
		}
	}
	stdout, stderr, code = invoke([]string{"dns", "check"}, healthyDeps)
	want = "example.com: ok (route53 ZONE, 1 nameservers delegated)\n" +
		"other.test: ok (route53 ZONE2, 1 nameservers delegated)\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("healthy check: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, want)
	}
}

func TestDNSACMEHooks(t *testing.T) {
	// R-LRD9-VWD0
	// R-LSL6-9O3P
	provider := &fakeDNSProvider{}
	deps := configuredDNSDeps(t, provider, "example.com")
	deps.Getenv = func(key string) string {
		switch key {
		case "CERTBOT_DOMAIN":
			return "example.com"
		case "CERTBOT_VALIDATION":
			return "token"
		default:
			return ""
		}
	}
	for _, subcommand := range []string{"acme-auth", "acme-cleanup"} {
		stdout, stderr, code := invoke([]string{"dns", subcommand, "--timeout", "4s"}, deps)
		if code != 0 || stdout != "" || stderr != "" {
			t.Errorf("%s: exit %d stdout %q stderr %q", subcommand, code, stdout, stderr)
		}
	}
	assertDNSCall(t, provider.adds, "_acme-challenge.example.com", 60, "token", time.Now().Add(4*time.Second))
	assertDNSCall(t, provider.removes, "_acme-challenge.example.com", 0, "token", time.Now().Add(4*time.Second))

	for _, tc := range []struct {
		subcommand, hook, domain, validation string
	}{
		{"acme-auth", "--manual-auth-hook", "", "token"},
		{"acme-auth", "--manual-auth-hook", "example.com", ""},
		{"acme-cleanup", "--manual-cleanup-hook", "", "token"},
		{"acme-cleanup", "--manual-cleanup-hook", "example.com", ""},
	} {
		deps.Getenv = func(key string) string {
			if key == "CERTBOT_DOMAIN" {
				return tc.domain
			}
			return tc.validation
		}
		stdout, stderr, code := invoke([]string{"dns", tc.subcommand}, deps)
		want := "opsctl: " + tc.subcommand + " must be run by certbot as " + tc.hook + "\n"
		if code != 2 || stdout != "" || stderr != want {
			t.Errorf("%s missing env: exit %d stdout %q stderr %q, want %q", tc.subcommand, code, stdout, stderr, want)
		}
	}
}

func TestDNSSubcommandsRequireRoot(t *testing.T) {
	// R-LTT2-NFUE
	for _, subcommand := range []string{"list", "add", "remove", "check", "acme-auth", "acme-cleanup"} {
		opened := false
		deps := depsAt(t, 1)
		deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
			opened = true
			return &fakeDNSProvider{}, nil
		}
		stdout, stderr, code := invoke([]string{"dns", subcommand}, deps)
		if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" || opened {
			t.Errorf("%s: exit %d stdout %q stderr %q opened %v", subcommand, code, stdout, stderr, opened)
		}
	}
}

func configuredDNSDeps(t *testing.T, provider dns.Provider, zones ...string) cli.Deps {
	t.Helper()
	deps := depsAt(t, 0)
	store := config.Store{Root: deps.Root}
	entries := map[string]string{dns.KeyProvider: "route53", dns.KeyZones: strings.Join(zones, ",")}
	for i, zone := range zones {
		id := "ZONE"
		if i > 0 {
			id = fmt.Sprintf("ZONE%d", i+1)
		}
		entries[dns.ZoneKey("route53", zone)] = id
	}
	for key, value := range entries {
		if err := store.Set(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
	return deps
}

func assertDNSCall(t *testing.T, calls []dnsCall, name string, ttl int, value string, wantDeadline time.Time) {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want one", calls)
	}
	call := calls[0]
	if call.zoneID != "ZONE" || call.name != name || call.typ != "TXT" || call.ttl != ttl || call.value != value {
		t.Errorf("call = %#v, want zone %q name %q type %q ttl %d value %q", call, "ZONE", name, "TXT", ttl, value)
	}
	if call.deadline.IsZero() || call.deadline.Sub(wantDeadline) > time.Second || wantDeadline.Sub(call.deadline) > time.Second {
		t.Errorf("deadline = %v, want near %v", call.deadline, wantDeadline)
	}
}
