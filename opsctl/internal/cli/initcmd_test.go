package cli_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

const wantInitUsage = `Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  none yet; each setup command adds itself here when it is designed

Configuration keys:
  host.name  the fully-qualified name this host answers at, at or under a configured zone
`

func TestInitHelp(t *testing.T) {
	// R-ED1L-QHES
	for _, args := range [][]string{{"init", "--help"}, {"init", "-h"}} {
		stdout, stderr, code := invoke(args, depsAt(t, 1))
		if code != 0 || stdout != wantInitUsage || stderr != "" {
			t.Errorf("%q: exit %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestInitRejectsArguments(t *testing.T) {
	// R-EE9I-495H
	want := "opsctl: init takes no arguments\n\nsee 'opsctl init --help' for usage\n"
	for _, args := range [][]string{{"init", "extra"}, {"init", "--help", "extra"}, {"init", "-h", "extra"}} {
		stdout, stderr, code := invoke(args, depsAt(t, 0))
		if code != 2 || stdout != "" || stderr != want {
			t.Errorf("%q: exit %d stdout %q stderr %q, want exit 2, empty stdout, stderr %q", args, code, stdout, stderr, want)
		}
	}
}

func TestInitRequiresRootBeforeWork(t *testing.T) {
	// R-EFHE-I0W6
	deps := depsAt(t, 1)
	configDir := filepath.Join(deps.Root, "etc", "ikigenba")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps.LookPath = func(string) (string, error) {
		t.Fatal("LookPath called before root check")
		return "", nil
	}
	deps.LookupHost = func(_ context.Context, _ string) ([]string, error) {
		t.Fatal("LookupHost called before root check")
		return nil, nil
	}

	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Errorf("exit %d stdout %q stderr %q, want root refusal", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"init", "extra"}, deps)
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Errorf("argument case: exit %d stdout %q stderr %q, want root refusal", code, stdout, stderr)
	}
}

func TestInitCorruptConfig(t *testing.T) {
	// R-ESWA-PI1T
	deps := depsAt(t, 0)
	writeCorrupt(t, deps.Root)

	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	want := "opsctl: " + filepath.Join(deps.Root, "etc", "ikigenba", "config.json") + ": config file is corrupt\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestInitHealthyPreflight(t *testing.T) {
	// R-EHX7-9KDK R-EJ53-NC49 R-EKD0-13UY R-ELKW-EVLN
	// R-EMSS-SNCC R-EO0P-6F31 R-EP8L-K6TQ R-EQGH-XYKF R-EROE-BQB4
	provider := &fakeDNSProvider{records: map[string][]dns.Record{
		"ZA": {
			{Name: "example.com", Type: "SOA"},
			{Name: "example.com", Type: "NS", Values: []string{"ns2", "ns1"}},
		},
		"ZB": {
			{Name: "deep.example.com", Type: "SOA"},
			{Name: "deep.example.com", Type: "NS", Values: []string{"ns3"}},
		},
	}}
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53",
		dns.KeyZones:    "example.com:ZA,deep.example.com:ZB",
		"host.name":     "API.Deep.Example.Com.",
	})
	var lookedUp []string
	deps.LookPath = func(name string) (string, error) {
		lookedUp = append(lookedUp, name)
		return "/bin/" + name, nil
	}
	deps.DNS.Open = func(_ context.Context, name string) (dns.Provider, error) {
		if name != "route53" {
			t.Fatalf("opened provider %q", name)
		}
		return provider, nil
	}
	deps.DNS.LookupNS = func(_ context.Context, zone string) ([]string, error) {
		if zone == "example.com" {
			return []string{"NS1.", "ns2"}, nil
		}
		return []string{"ns3"}, nil
	}
	var resolved []string
	deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
		resolved = append(resolved, name)
		if strings.HasPrefix(name, "_opsctl-preflight.") {
			return []string{"192.0.2.2", "192.0.2.1"}, nil
		}
		return []string{"192.0.2.1", "192.0.2.2", "192.0.2.1"}, nil
	}

	want := "nginx: ok (/bin/nginx)\n" +
		"certbot: ok (/bin/certbot)\n" +
		"systemctl: ok (/bin/systemctl)\n" +
		"dns.provider: ok (route53)\n" +
		"dns.zones: ok (example.com,deep.example.com)\n" +
		"host.name: ok (api.deep.example.com)\n" +
		"zone example.com: ok (route53 ZA, 2 nameservers delegated)\n" +
		"zone deep.example.com: ok (route53 ZB, 1 nameservers delegated)\n" +
		"host api.deep.example.com: ok (zone deep.example.com)\n" +
		"wildcard api.deep.example.com: ok (192.0.2.1,192.0.2.2)\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q, want exit 0 stdout %q", code, stdout, stderr, want)
	}
	if !reflect.DeepEqual(lookedUp, []string{"nginx", "certbot", "systemctl"}) {
		t.Errorf("LookPath calls = %v, want nginx, certbot, systemctl", lookedUp)
	}
	if !reflect.DeepEqual(resolved, []string{"api.deep.example.com", "_opsctl-preflight.api.deep.example.com"}) {
		t.Errorf("LookupHost calls = %v", resolved)
	}
}

func TestInitAggregatesIndependentFailures(t *testing.T) {
	// R-EHX7-9KDK R-EJ53-NC49 R-EKD0-13UY R-ELKW-EVLN
	// R-EQGH-XYKF R-EROE-BQB4
	deps := initDeps(t, map[string]string{dns.KeyZones: "example.com:ZONE"})
	deps.LookPath = func(name string) (string, error) {
		if name == "certbot" {
			return "", errors.New("missing")
		}
		return "/usr/bin/" + name, nil
	}
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
		t.Fatal("provider opened without dns.provider")
		return nil, nil
	}
	deps.LookupHost = func(context.Context, string) ([]string, error) {
		t.Fatal("host resolved without host.name")
		return nil, nil
	}

	want := "nginx: ok (/usr/bin/nginx)\n" +
		"certbot: failed: not found on PATH\n" +
		"systemctl: ok (/usr/bin/systemctl)\n" +
		"dns.provider: failed: not set\n" +
		"dns.zones: ok (example.com)\n" +
		"host.name: failed: not set\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}
}

func TestInitClassifiesDNSOpenFailures(t *testing.T) {
	// R-EJ53-NC49 R-EKD0-13UY
	for _, tc := range []struct {
		name, zones string
		openErr     error
		wantDNS     string
	}{
		{"provider", "example.com:ZONE", errors.New("credentials unavailable"),
			"dns.provider: failed: credentials unavailable\ndns.zones: ok (example.com)\n"},
		{"zones", "malformed", nil,
			"dns.provider: ok (route53)\ndns.zones: failed: dns.zones malformed: \"malformed\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := initDeps(t, map[string]string{dns.KeyProvider: "route53", dns.KeyZones: tc.zones})
			deps.LookPath = foundInitTools
			deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
				if tc.openErr != nil {
					return nil, tc.openErr
				}
				return &fakeDNSProvider{}, nil
			}
			stdout, stderr, code := invoke([]string{"init"}, deps)
			want := "nginx: ok (/bin/nginx)\ncertbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\n" +
				tc.wantDNS + "host.name: failed: not set\n"
			if code != 2 || stderr != "" || stdout != want {
				t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
			}
		})
	}
}

func TestInitReportsZoneHostAndWildcardFailures(t *testing.T) {
	// R-EMSS-SNCC R-EO0P-6F31 R-EP8L-K6TQ R-EQGH-XYKF R-EROE-BQB4
	provider := &fakeDNSProvider{
		records: map[string][]dns.Record{
			"WRONG": {{Name: "provider.test", Type: "SOA"}},
			"UNDELEGATED": {
				{Name: "undelegated.test", Type: "SOA"},
				{Name: "undelegated.test", Type: "NS", Values: []string{"ns1"}},
			},
		},
		recordsErr: map[string]error{"ERROR": errors.New("unreachable")},
	}
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53",
		dns.KeyZones:    "wrong.test:WRONG,undelegated.test:UNDELEGATED,error.test:ERROR",
		"host.name":     "outside.example.",
	})
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
	deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"different"}, nil }
	deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
		if strings.HasPrefix(name, "_opsctl-preflight.") {
			return []string{"192.0.2.9"}, nil
		}
		return []string{"192.0.2.8"}, nil
	}

	prefix := "nginx: ok (/bin/nginx)\ncertbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\n" +
		"dns.provider: ok (route53)\ndns.zones: ok (wrong.test,undelegated.test,error.test)\n" +
		"host.name: ok (outside.example)\n" +
		"zone wrong.test: failed: provider reports zone provider.test\n" +
		"zone undelegated.test: failed: nameservers are not delegated\n" +
		"zone error.test: failed: unreachable\n" +
		"host outside.example: failed: no configured zone contains it\n"
	want := prefix + "wildcard outside.example: failed: outside.example resolves to 192.0.2.8 but _opsctl-preflight.outside.example resolves to 192.0.2.9\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}

	deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
		return nil, errors.New("lookup " + name + ": unavailable")
	}
	stdout, _, _ = invoke([]string{"init"}, deps)
	want = prefix + "wildcard outside.example: failed: lookup outside.example: unavailable\n"
	if stdout != want {
		t.Errorf("lookup-error stdout = %q, want %q", stdout, want)
	}
}

func TestInitIsReadOnlyAndRepeatable(t *testing.T) {
	// R-EU47-39SI
	provider := &fakeDNSProvider{records: map[string][]dns.Record{
		"ZONE": {
			{Name: "example.com", Type: "SOA"},
			{Name: "example.com", Type: "NS", Values: []string{"ns1"}},
		},
	}}
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53", dns.KeyZones: "example.com:ZONE", "host.name": "example.com",
	})
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
	deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns1"}, nil }
	deps.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
	before := treeState(t, deps.Root)

	stdout1, stderr1, code1 := invoke([]string{"init"}, deps)
	stdout2, stderr2, code2 := invoke([]string{"init"}, deps)
	after := treeState(t, deps.Root)
	want := "nginx: ok (/bin/nginx)\ncertbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\n" +
		"dns.provider: ok (route53)\ndns.zones: ok (example.com)\nhost.name: ok (example.com)\n" +
		"zone example.com: ok (route53 ZONE, 1 nameservers delegated)\n" +
		"host example.com: ok (zone example.com)\nwildcard example.com: ok (192.0.2.1)\n"
	if stdout1 != want || stderr1 != "" || code1 != 0 {
		t.Errorf("first run = (%q, %q, %d), want (%q, empty, 0)", stdout1, stderr1, code1, want)
	}
	if stdout1 != stdout2 || stderr1 != stderr2 || code1 != code2 {
		t.Errorf("runs differ: (%q, %q, %d) then (%q, %q, %d)", stdout1, stderr1, code1, stdout2, stderr2, code2)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("Root changed:\nbefore %#v\nafter  %#v", before, after)
	}
}

func initDeps(t *testing.T, values map[string]string) cli.Deps {
	t.Helper()
	deps := depsAt(t, 0)
	store := config.Store{Root: deps.Root}
	for key, value := range values {
		if err := store.Set(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	return deps
}

func foundInitTools(name string) (string, error) { return "/bin/" + name, nil }

type treeEntry struct {
	Mode    os.FileMode
	Content string
}

func treeState(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	state := map[string]treeEntry{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := treeEntry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			content, err := fs.ReadFile(os.DirFS(root), filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			item.Content = string(content)
		}
		state[rel] = item
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
