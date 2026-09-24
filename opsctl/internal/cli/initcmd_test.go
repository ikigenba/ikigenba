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
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantInitUsage = `Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  litestream                 found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  timeouts                   apps.drain_seconds and apps.stop_seconds are positive
                             whole seconds, stop greater than drain
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  certificate  obtain the host's certificate, or renew it if it is due
  nginx.conf   generate /etc/nginx/conf.d/ikigenba.conf and reload nginx
  litestream   generate /etc/litestream.yml and enable litestream.service
  timers       write the backup and renewal units, enabling each backup timer
               whose period is set and the renewal timer always
  apps         write the drain and stop settings into every installed app,
               restarting each enabled app whose settings changed; a
               disabled app is rewritten and left disabled

Configuration keys:
  host.name           the fully-qualified name this host answers at, at or under a configured zone
  apps.drain_seconds  how long an app may drain when stopped (default 5)
  apps.stop_seconds   how long systemd waits for an app to stop (default 10)
`

func TestInitHelp(t *testing.T) {
	// R-X0W0-0NJ4
	for _, uid := range []int{0, 1000} {
		for _, args := range [][]string{{"init", "--help"}, {"init", "-h"}} {
			deps, assertNoAccess := inertDeps(t, uid)
			stdout, stderr, code := invoke(args, deps)
			assertNoAccess()
			if code != 0 || stdout != wantInitUsage || stderr != "" {
				t.Errorf("uid %d %q: exit %d stdout %q stderr %q", uid, args, code, stdout, stderr)
			}
		}
	}
}

func TestInitRejectsArguments(t *testing.T) {
	// R-LYOS-MASG R-ZBWG-K26K
	want := "opsctl: init takes no arguments\n\nsee 'opsctl init --help' for usage\n"
	for _, args := range [][]string{{"init", "extra"}, {"init", "--help", "extra"}, {"init", "-h", "extra"}} {
		deps, assertNoAccess := inertDeps(t, 0)
		stdout, stderr, code := invoke(args, deps)
		assertNoAccess()
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
	// R-LYOS-MASG R-LZWP-02J5 R-3EJ4-MMM3
	deps := depsAt(t, 0)
	writeCorrupt(t, deps.Root)
	deps.LookPath = func(string) (string, error) {
		t.Fatal("LookPath called after corrupt config read")
		return "", nil
	}
	deps.LookupHost = func(context.Context, string) ([]string, error) {
		t.Fatal("LookupHost called after corrupt config read")
		return nil, nil
	}
	deps.Execute = func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("setup invoked after corrupt config read")
		return host.Result{}, nil
	}
	path := filepath.Join(deps.Root, "etc", "ikigenba", "config.json")
	before := treeState(t, deps.Root)

	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	want := "opsctl: " + path + " is corrupt\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if after := treeState(t, deps.Root); !reflect.DeepEqual(after, before) {
		t.Errorf("Root changed:\nbefore %#v\nafter  %#v", before, after)
	}
}

func TestInitStoreReadFailuresRunNoWork(t *testing.T) {
	// R-LYOS-MASG R-LZWP-02J5
	deps := depsAt(t, 0)
	path := filepath.Join(deps.Root, "etc", "ikigenba", "config.json")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	deps.LookPath = func(string) (string, error) {
		t.Fatal("LookPath called after config read failure")
		return "", nil
	}
	deps.LookupHost = func(context.Context, string) ([]string, error) {
		t.Fatal("LookupHost called after config read failure")
		return nil, nil
	}
	deps.Execute = func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("setup invoked after config read failure")
		return host.Result{}, nil
	}
	_, readErr := (config.Store{Root: deps.Root}).List()
	if readErr == nil || errors.Is(readErr, config.ErrCorrupt) {
		t.Fatalf("store read error = %v, want non-corruption error", readErr)
	}
	before := treeState(t, deps.Root)

	stdout, stderr, code := invoke([]string{"init"}, deps)
	wantStderr := "opsctl: read " + path + ": is a directory\n"
	if code != 1 || stdout != "" || stderr != wantStderr {
		t.Errorf("exit %d stdout %q stderr %q, want exit 1, empty stdout, stderr %q",
			code, stdout, stderr, wantStderr)
	}
	if after := treeState(t, deps.Root); !reflect.DeepEqual(after, before) {
		t.Errorf("Root changed:\nbefore %#v\nafter  %#v", before, after)
	}
}

func TestInitStoreRereadFailureDiscardsPreflight(t *testing.T) {
	// R-LYOS-MASG R-LZWP-02J5
	deps := depsAt(t, 0)
	configDir := filepath.Join(deps.Root, "etc", "ikigenba")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(target, []byte(`{"dns.provider":"route53","dns.zones":"example.com:ZONE","host.name":"example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(configDir, "config.json")); err != nil {
		t.Fatal(err)
	}

	deps.LookPath = foundInitTools
	provider := &fakeDNSProvider{}
	openCalls := 0
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
		openCalls++
		if err := os.Chmod(target, 0); err != nil {
			t.Fatal(err)
		}
		return provider, nil
	}
	t.Cleanup(func() {
		if err := os.Chmod(target, 0o600); err != nil {
			t.Error(err)
		}
	})
	deps.DNS.LookupNS = func(context.Context, string) ([]string, error) {
		t.Fatal("zone lookup invoked after store reread failure")
		return nil, nil
	}
	deps.LookupHost = func(context.Context, string) ([]string, error) {
		t.Fatal("host lookup invoked after store reread failure")
		return nil, nil
	}
	deps.Execute = func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("setup invoked after store reread failure")
		return host.Result{}, nil
	}
	before := treeState(t, deps.Root)

	stdout, stderr, code := invoke([]string{"init"}, deps)
	configPath := filepath.Join(configDir, "config.json")
	wantStderr := "opsctl: open " + configPath + ": permission denied\n"
	if code != 1 || stdout != "" || stderr != wantStderr {
		t.Errorf("exit %d stdout %q stderr %q, want exit 1, empty stdout, stderr %q",
			code, stdout, stderr, wantStderr)
	}
	if openCalls != 1 {
		t.Errorf("provider opens = %d, want 1", openCalls)
	}
	if len(provider.recordCalls) != 0 {
		t.Errorf("provider record calls = %v, want none", provider.recordCalls)
	}
	if after := treeState(t, deps.Root); !reflect.DeepEqual(after, before) {
		t.Errorf("Root changed:\nbefore %#v\nafter  %#v", before, after)
	}
}

func TestInitMissingConfigurationIsReportedAsFindings(t *testing.T) {
	// R-LZWP-02J5
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{name: "missing file"},
		{name: "missing keys", values: map[string]string{"unrelated": "value"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := initDeps(t, tc.values)
			deps.LookPath = foundInitTools
			deps.Execute = func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("setup invoked with missing configuration")
				return host.Result{}, nil
			}
			before := treeState(t, deps.Root)

			stdout, stderr, code := invoke([]string{"init"}, deps)
			want := "dns.provider: failed: not set\ndns.zones: failed: not set\nhost.name: failed: not set\ntimeouts: ok (drain 5s, stop 10s)\n"
			if code != 2 || stderr != "" || !strings.Contains(stdout, want) {
				t.Errorf("exit %d stdout %q stderr %q, want findings %q", code, stdout, stderr, want)
			}
			if after := treeState(t, deps.Root); !reflect.DeepEqual(after, before) {
				t.Errorf("Root changed:\nbefore %#v\nafter  %#v", before, after)
			}
		})
	}
}

func TestInitTimingFindingIsIndependentAndStopsSetup(t *testing.T) {
	// R-X23W-EF9T R-X4JP-5YR7
	for _, tc := range []struct {
		name, drain, stop, finding string
	}{
		{"invalid drain", "0", "30", "apps.drain_seconds is not a positive whole number of seconds: '0'"},
		{"invalid stop", "7", "wrong", "apps.stop_seconds is not a positive whole number of seconds: 'wrong'"},
		{"inconsistent", "30", "7", "apps.stop_seconds (7) is not greater than apps.drain_seconds (30)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := initDeps(t, map[string]string{
				"apps.drain_seconds": tc.drain,
				"apps.stop_seconds":  tc.stop,
				"host.name":          "Example.COM.",
			})
			deps.LookPath = func(name string) (string, error) {
				if name == "certbot" {
					return "", errors.New("missing")
				}
				return "/bin/" + name, nil
			}
			var lookedUp []string
			deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
				lookedUp = append(lookedUp, name)
				return []string{"192.0.2.1"}, nil
			}
			deps.Execute = func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("setup ran after a timing finding")
				return host.Result{}, nil
			}
			before := treeState(t, deps.Root)
			stdout, stderr, code := invoke([]string{"init"}, deps)
			want := "nginx: ok (/bin/nginx)\n" +
				"certbot: failed: not found on PATH\n" +
				"systemctl: ok (/bin/systemctl)\n" +
				"litestream: ok (/bin/litestream)\n" +
				"dns.provider: failed: not set\n" +
				"dns.zones: failed: not set\n" +
				"host.name: ok (example.com)\n" +
				"timeouts: failed: " + tc.finding + "\n" +
				"wildcard example.com: ok (192.0.2.1)\n"
			if code != 2 || stdout != want || stderr != "" {
				t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
			}
			if !reflect.DeepEqual(lookedUp, []string{"example.com", "_opsctl-preflight.example.com"}) {
				t.Errorf("host lookups = %v", lookedUp)
			}
			if after := treeState(t, deps.Root); !reflect.DeepEqual(after, before) {
				t.Errorf("preflight changed host state: before %#v, after %#v", before, after)
			}
		})
	}
}

func TestInitHealthyPreflight(t *testing.T) {
	// R-X4JP-5YR7 R-X23W-EF9T R-X3BS-S70I
	// R-LIU3-NA5F R-LK20-11W4 R-LMHS-SLDI R-ELKW-EVLN
	// R-ZAOK-6AFV R-LOXL-K4UW R-LQ5H-XWLL
	// R-LHM7-9IEQ R-ZIB1-SI40
	// R-JWO0-EHD7 R-5E43-77RM
	// R-YYIY-T743
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
		dns.KeyProvider:                "route53",
		dns.KeyZones:                   "example.com:ZA,deep.example.com:ZB",
		"host.name":                    "API.Deep.Example.Com.",
		"acme.email":                   "stale@example.com",
		"aws.region":                   "us-east-2",
		"backup.s3_uri":                "s3://bucket/host/",
		"backup.host_files_seconds":    "17",
		"backup.service_files_seconds": "0",
		"backup.service_db_seconds":    "3600",
		"backup.service_wal_seconds":   "5",
		"apps.drain_seconds":           "7",
		"apps.stop_seconds":            "19",
	})
	if err := os.MkdirAll(filepath.Join(deps.Root, "etc/nginx/conf.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(deps.Root, "opt", "notes", "etc", "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("app = \"notes\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var lookedUp []string
	deps.LookPath = func(name string) (string, error) {
		lookedUp = append(lookedUp, name)
		return "/bin/" + name, nil
	}
	openCalls := 0
	deps.DNS.Open = func(_ context.Context, name string) (dns.Provider, error) {
		openCalls++
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
			store := config.Store{Root: deps.Root}
			if err := store.Set("acme.email", "admin@example.com"); err != nil {
				t.Fatal(err)
			}
			if err := store.Set("host.apex", "notes"); err != nil {
				t.Fatal(err)
			}
			manifest := "app = \"notes\"\ndefault = true\n" +
				"\n[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n"
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			return []string{"192.0.2.2", "192.0.2.1"}, nil
		}
		return []string{"192.0.2.1", "192.0.2.2", "192.0.2.1"}, nil
	}
	var commands []string
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, strings.Join(append([]string{command.Name}, command.Args...), " "))
		if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show" {
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
		}
		return host.Result{}, nil
	}

	want := "nginx: ok (/bin/nginx)\n" +
		"certbot: ok (/bin/certbot)\n" +
		"systemctl: ok (/bin/systemctl)\n" +
		"litestream: ok (/bin/litestream)\n" +
		"dns.provider: ok (route53)\n" +
		"dns.zones: ok (example.com,deep.example.com)\n" +
		"host.name: ok (api.deep.example.com)\ntimeouts: ok (drain 7s, stop 19s)\n" +
		"zone example.com: ok (route53 ZA, 2 nameservers delegated)\n" +
		"zone deep.example.com: ok (route53 ZB, 1 nameservers delegated)\n" +
		"host api.deep.example.com: ok (zone deep.example.com)\n" +
		"wildcard api.deep.example.com: ok (192.0.2.1,192.0.2.2)\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q, want exit 0 stdout %q", code, stdout, stderr, want)
	}
	if !reflect.DeepEqual(lookedUp, []string{"nginx", "certbot", "systemctl", "litestream"}) {
		t.Errorf("LookPath calls = %v, want nginx, certbot, systemctl, litestream", lookedUp)
	}
	if !reflect.DeepEqual(resolved, []string{"api.deep.example.com", "_opsctl-preflight.api.deep.example.com"}) {
		t.Errorf("LookupHost calls = %v", resolved)
	}
	if openCalls != 1 {
		t.Errorf("provider open calls = %d, want 1", openCalls)
	}
	wantCommands := []string{
		"certbot certonly --non-interactive --agree-tos --email admin@example.com --manual --preferred-challenges dns --manual-auth-hook opsctl dns acme-auth --manual-cleanup-hook opsctl dns acme-cleanup --deploy-hook systemctl try-reload-or-restart nginx --cert-name api.deep.example.com -d api.deep.example.com -d *.api.deep.example.com -d deep.example.com --keep-until-expiring --config-dir " + filepath.Join(deps.Root, "etc/letsencrypt") + " --work-dir " + filepath.Join(deps.Root, "var/lib/letsencrypt") + " --logs-dir " + filepath.Join(deps.Root, "var/log/letsencrypt"),
		"systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"nginx -t",
		"systemctl reload-or-restart nginx",
		"systemctl enable litestream.service",
		"systemctl restart litestream.service",
		"systemctl daemon-reload",
		"systemctl enable ikigenba-backup-host.timer",
		"systemctl restart ikigenba-backup-host.timer",
		"systemctl disable ikigenba-backup-services.timer",
		"systemctl stop ikigenba-backup-services.timer",
		"systemctl enable ikigenba-renew-certificate.timer",
		"systemctl restart ikigenba-renew-certificate.timer",
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("setup commands = %#v, want %#v", commands, wantCommands)
	}
	nginxConfiguration, err := os.ReadFile(filepath.Join(deps.Root, "etc", "nginx", "conf.d", "ikigenba.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, wantFragment := range []string{
		"server_name         notes.api.deep.example.com api.deep.example.com deep.example.com;",
		"/etc/letsencrypt/live/api.deep.example.com/fullchain.pem",
		"/etc/letsencrypt/live/api.deep.example.com/privkey.pem",
	} {
		if !strings.Contains(string(nginxConfiguration), wantFragment) {
			t.Errorf("nginx configuration does not contain %q:\n%s", wantFragment, nginxConfiguration)
		}
	}
	litestreamConfiguration, err := os.ReadFile(filepath.Join(deps.Root, "etc", "litestream.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, wantFragment := range []string{
		filepath.Join(deps.Root, "opt", "notes", "state", "notes.db"),
		"s3://bucket/host/notes/",
		"snapshot:\n  interval: 3600s",
		"sync-interval: 5s",
	} {
		if !strings.Contains(string(litestreamConfiguration), wantFragment) {
			t.Errorf("litestream configuration does not contain %q:\n%s", wantFragment, litestreamConfiguration)
		}
	}
	hostTimer, err := os.ReadFile(filepath.Join(deps.Root, "etc", "systemd", "system", "ikigenba-backup-host.timer"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostTimer), "OnBootSec=17s\nOnUnitActiveSec=17s\n") {
		t.Errorf("host timer does not carry current period:\n%s", hostTimer)
	}
	renewalTimer, err := os.ReadFile(filepath.Join(deps.Root, "etc", "systemd", "system", "ikigenba-renew-certificate.timer"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(renewalTimer), "OnCalendar=*-*-* 00,12:00:00\n") {
		t.Errorf("renewal timer does not carry the fixed schedule:\n%s", renewalTimer)
	}
}

func TestInitStopsAtFirstSetupFailure(t *testing.T) {
	// R-LXGW-8J1R R-JWO0-EHD7
	wantStdout := "nginx: ok (/bin/nginx)\n" +
		"certbot: ok (/bin/certbot)\n" +
		"systemctl: ok (/bin/systemctl)\n" +
		"litestream: ok (/bin/litestream)\n" +
		"dns.provider: ok (route53)\n" +
		"dns.zones: ok (example.com)\n" +
		"host.name: ok (api.example.com)\ntimeouts: ok (drain 5s, stop 10s)\n" +
		"zone example.com: ok (route53 ZA, 1 nameservers delegated)\n" +
		"host api.example.com: ok (zone example.com)\n" +
		"wildcard api.example.com: ok (192.0.2.10)\n"

	tests := []struct {
		name        string
		failCommand string
		label       string
		cause       string
		completed   []string
	}{
		{name: "certificate", failCommand: "certbot certonly", label: "certbot certonly", cause: "certificate transport failed"},
		{name: "nginx", failCommand: "nginx -t", label: "nginx -t", cause: "nginx transport failed", completed: []string{"certificate"}},
		{name: "replication", failCommand: "systemctl enable litestream.service", label: "enable litestream.service", cause: "replication transport failed", completed: []string{"certificate", "nginx"}},
		{name: "timers", failCommand: "systemctl daemon-reload", label: "reload systemd units", cause: "timer transport failed", completed: []string{"certificate", "nginx", "litestream"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider := &fakeDNSProvider{records: map[string][]dns.Record{
				"ZA": {
					{Name: "example.com", Type: "SOA"},
					{Name: "example.com", Type: "NS", Values: []string{"ns1"}},
				},
			}}
			deps := initDeps(t, map[string]string{
				dns.KeyProvider: "route53",
				dns.KeyZones:    "example.com:ZA",
				"host.name":     "api.example.com",
				"acme.email":    "admin@example.com",
				"aws.region":    "us-east-2",
				"backup.s3_uri": "s3://bucket/host/",
			})
			if err := os.MkdirAll(filepath.Join(deps.Root, "etc/nginx/conf.d"), 0o750); err != nil {
				t.Fatal(err)
			}
			deps.LookPath = foundInitTools
			deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
				return provider, nil
			}
			deps.DNS.LookupNS = func(context.Context, string) ([]string, error) {
				return []string{"ns1"}, nil
			}
			deps.LookupHost = func(context.Context, string) ([]string, error) {
				return []string{"192.0.2.10"}, nil
			}

			certbotCommand := "certbot certonly --non-interactive --agree-tos --email admin@example.com --manual --preferred-challenges dns --manual-auth-hook opsctl dns acme-auth --manual-cleanup-hook opsctl dns acme-cleanup --deploy-hook systemctl try-reload-or-restart nginx --cert-name api.example.com -d api.example.com -d *.api.example.com --keep-until-expiring --config-dir " + filepath.Join(deps.Root, "etc/letsencrypt") + " --work-dir " + filepath.Join(deps.Root, "var/lib/letsencrypt") + " --logs-dir " + filepath.Join(deps.Root, "var/log/letsencrypt")
			allCommands := []string{
				certbotCommand,
				"nginx -t",
				"systemctl reload-or-restart nginx",
				"systemctl enable litestream.service",
				"systemctl restart litestream.service",
				"systemctl daemon-reload",
			}
			failIndex := -1
			for i, command := range allCommands {
				if command == tc.failCommand || strings.HasPrefix(command, tc.failCommand+" ") {
					failIndex = i
					break
				}
			}
			if failIndex < 0 {
				t.Fatalf("failure command %q absent from setup sequence", tc.failCommand)
			}

			var commands []string
			deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
				invocation := strings.Join(append([]string{command.Name}, command.Args...), " ")
				commands = append(commands, invocation)
				if invocation == allCommands[failIndex] {
					return host.Result{
						Stdout: []byte(tc.name + " captured stdout\n"),
						Stderr: []byte(tc.name + " captured stderr\n"),
					}, errors.New(tc.cause)
				}
				marker := ""
				switch invocation {
				case certbotCommand:
					marker = "certificate"
				case "systemctl reload-or-restart nginx":
					marker = "nginx"
				case "systemctl restart litestream.service":
					marker = "litestream"
				}
				if marker != "" {
					markerDir := filepath.Join(deps.Root, "var", "lib", "init-completed")
					if err := os.MkdirAll(markerDir, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(markerDir, marker), []byte("complete\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				return host.Result{}, nil
			}

			stdout, stderr, code := invoke([]string{"init"}, deps)
			wantStderr := "opsctl: " + tc.label + ": " + tc.cause + "\n\n" +
				"> " + tc.name + " captured stdout\n" +
				"> " + tc.name + " captured stderr\n"
			if code != 1 || stdout != wantStdout || stderr != wantStderr {
				t.Fatalf("exit %d stdout %q stderr %q, want exit 1 stdout %q stderr %q", code, stdout, stderr, wantStdout, wantStderr)
			}
			wantCommands := allCommands[:failIndex+1]
			if !reflect.DeepEqual(commands, wantCommands) {
				t.Fatalf("setup commands = %#v, want first-error prefix %#v", commands, wantCommands)
			}
			for _, marker := range []string{"certificate", "nginx", "litestream"} {
				_, statErr := os.Stat(filepath.Join(deps.Root, "var", "lib", "init-completed", marker))
				wantPresent := false
				for _, completed := range tc.completed {
					wantPresent = wantPresent || marker == completed
				}
				if wantPresent && statErr != nil {
					t.Errorf("earlier successful %s state was not preserved: %v", marker, statErr)
				}
				if !wantPresent && !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("later %s operation left state: %v", marker, statErr)
				}
			}
			for _, captured := range []string{tc.cause, tc.name + " captured stdout", tc.name + " captured stderr"} {
				if count := strings.Count(stderr, captured); count != 1 {
					t.Errorf("diagnostic contains %q %d times, want once: %q", captured, count, stderr)
				}
			}
		})
	}
}

func TestInitValidatesProviderAndZonesIndependently(t *testing.T) {
	// R-LK20-11W4 R-LMHS-SLDI
	t.Run("provider without zones", func(t *testing.T) {
		deps := initDeps(t, map[string]string{dns.KeyProvider: "route53"})
		deps.LookPath = foundInitTools
		opened := 0
		deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
			opened++
			return &fakeDNSProvider{}, nil
		}
		stdout, stderr, code := invoke([]string{"init"}, deps)
		if code != 2 || stderr != "" || opened != 1 || !strings.Contains(stdout,
			"dns.provider: ok (route53)\ndns.zones: failed: not set\n") {
			t.Errorf("exit %d stdout %q stderr %q opens %d", code, stdout, stderr, opened)
		}
	})

	t.Run("malformed zones without provider", func(t *testing.T) {
		deps := initDeps(t, map[string]string{dns.KeyZones: " Example.COM. : ZONE ,broken"})
		deps.LookPath = foundInitTools
		deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
			t.Fatal("provider opened without dns.provider")
			return nil, nil
		}
		stdout, stderr, code := invoke([]string{"init"}, deps)
		want := "dns.provider: failed: not set\ndns.zones: failed: dns.zones malformed: \"broken\"\n"
		if code != 2 || stderr != "" || !strings.Contains(stdout, want) {
			t.Errorf("exit %d stdout %q stderr %q, want fragment %q", code, stdout, stderr, want)
		}
	})

	t.Run("normalization empties name", func(t *testing.T) {
		deps := initDeps(t, map[string]string{dns.KeyZones: ". : ZONE"})
		deps.LookPath = foundInitTools
		stdout, stderr, code := invoke([]string{"init"}, deps)
		want := "dns.provider: failed: not set\ndns.zones: failed: dns.zones malformed: \". : ZONE\"\n"
		if code != 2 || stderr != "" || !strings.Contains(stdout, want) {
			t.Errorf("exit %d stdout %q stderr %q, want fragment %q", code, stdout, stderr, want)
		}
	})

	t.Run("raw malformed entry", func(t *testing.T) {
		deps := initDeps(t, nil)
		writeInitConfig(t, deps.Root, `{"dns.zones":"example.com:ZONE,bad\nentry"}`)
		deps.LookPath = foundInitTools
		stdout, stderr, code := invoke([]string{"init"}, deps)
		want := "dns.provider: failed: not set\ndns.zones: failed: dns.zones malformed: \"bad\\nentry\"\n"
		if code != 2 || stderr != "" || !strings.Contains(stdout, want) {
			t.Errorf("exit %d stdout %q stderr %q, want fragment %q", code, stdout, stderr, want)
		}
	})

	t.Run("nil opener", func(t *testing.T) {
		deps := initDeps(t, map[string]string{dns.KeyProvider: "route53"})
		deps.LookPath = foundInitTools
		stdout, stderr, code := invoke([]string{"init"}, deps)
		if code != 2 || stderr != "" || !strings.Contains(stdout,
			"dns.provider: failed: unknown dns provider: \"route53\"\n") {
			t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})
}

func TestInitHostZoneDoesNotDependOnProvider(t *testing.T) {
	// R-LOXL-K4UW
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53",
		dns.KeyZones:    "Example.COM.:PARENT, Deep.Example.COM.:CHILD",
		"host.name":     "API.Deep.Example.COM.",
	})
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
		return nil, errors.New("credentials unavailable")
	}
	deps.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stderr != "" || !strings.Contains(stdout,
		"host api.deep.example.com: ok (zone deep.example.com)\n") {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestInitWildcardCanonicalizesAddressesAndRunsBothLookups(t *testing.T) {
	// R-LQ5H-XWLL
	t.Run("canonical equal sets", func(t *testing.T) {
		deps := initDeps(t, map[string]string{"host.name": "Example.COM."})
		deps.LookPath = foundInitTools
		var resolved []string
		deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
			resolved = append(resolved, name)
			switch name {
			case "example.com":
				return []string{"2001:0db8::1", "::ffff:192.0.2.1", "2001:db8::1"}, nil
			case "_opsctl-preflight.example.com":
				return []string{"192.0.2.1", "2001:db8::1"}, nil
			default:
				t.Fatalf("unexpected lookup %q", name)
				return nil, nil
			}
		}
		stdout, stderr, code := invoke([]string{"init"}, deps)
		wantResolved := []string{"example.com", "_opsctl-preflight.example.com"}
		if code != 2 || stderr != "" || !reflect.DeepEqual(resolved, wantResolved) || !strings.Contains(stdout,
			"wildcard example.com: ok (192.0.2.1,2001:db8::1)\n") {
			t.Errorf("exit %d stdout %q stderr %q lookups %v", code, stdout, stderr, resolved)
		}
	})

	t.Run("invalid host result precedes probe error", func(t *testing.T) {
		deps := initDeps(t, map[string]string{"host.name": "example.com"})
		deps.LookPath = foundInitTools
		var resolved []string
		deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
			resolved = append(resolved, name)
			switch name {
			case "example.com":
				return []string{"not-an-ip"}, nil
			case "_opsctl-preflight.example.com":
				return nil, errors.New("probe unavailable")
			default:
				t.Fatalf("unexpected lookup %q", name)
				return nil, nil
			}
		}
		stdout, stderr, code := invoke([]string{"init"}, deps)
		wantResolved := []string{"example.com", "_opsctl-preflight.example.com"}
		if code != 2 || stderr != "" || !reflect.DeepEqual(resolved, wantResolved) || !strings.Contains(stdout,
			"wildcard example.com: failed: invalid address \"not-an-ip\"\n") {
			t.Errorf("exit %d stdout %q stderr %q lookups %v", code, stdout, stderr, resolved)
		}
	})
}

func TestInitFailedPreflightRunsNoSetupAndChangesNoState(t *testing.T) {
	// R-LSLA-PG2Z
	deps := initDeps(t, map[string]string{"host.name": "example.com"})
	deps.LookPath = func(name string) (string, error) {
		if name == "certbot" {
			return "", errors.New("missing")
		}
		return "/bin/" + name, nil
	}
	deps.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
	deps.Execute = func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("setup process invoked after failed preflight")
		return host.Result{}, nil
	}
	before := treeState(t, deps.Root)
	stdout, stderr, code := invoke([]string{"init"}, deps)
	after := treeState(t, deps.Root)
	if code != 2 || stderr != "" || stdout == "" {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("Root changed:\nbefore %#v\nafter  %#v", before, after)
	}
}

func TestInitPreflightIgnoresHostApex(t *testing.T) {
	// R-ZKQU-K1LE
	var wantStdout string
	for _, tc := range []struct {
		name string
		set  bool
		apex string
	}{
		{name: "unset"},
		{name: "empty", set: true},
		{name: "set to invalid apex request", set: true, apex: "site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"host.name": "localhost"}
			if tc.set {
				values["host.apex"] = tc.apex
			}
			deps := initDeps(t, values)
			deps.LookPath = foundInitTools
			var lookedUp []string
			deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
				lookedUp = append(lookedUp, name)
				return []string{"192.0.2.1"}, nil
			}
			deps.Execute = func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("setup invoked after failed preflight")
				return host.Result{}, nil
			}

			stdout, stderr, code := invoke([]string{"init"}, deps)
			if code != 2 || stderr != "" {
				t.Fatalf("exit %d stderr %q, want exit 2 and empty stderr", code, stderr)
			}
			if wantStdout == "" {
				wantStdout = stdout
			} else if stdout != wantStdout {
				t.Errorf("stdout = %q, want byte-identical report %q", stdout, wantStdout)
			}
			wantLookups := []string{"localhost", "_opsctl-preflight.localhost"}
			if !reflect.DeepEqual(lookedUp, wantLookups) {
				t.Errorf("host lookups = %v, want %v", lookedUp, wantLookups)
			}
		})
	}
}

func TestInitInvalidApexFailsAtCertificate(t *testing.T) {
	// R-ZKQU-K1LE R-ZLYQ-XTC3
	provider := &fakeDNSProvider{records: map[string][]dns.Record{
		"ZONE": {
			{Name: "localhost", Type: "SOA"},
			{Name: "localhost", Type: "NS", Values: []string{"ns1"}},
		},
	}}
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53",
		dns.KeyZones:    "localhost:ZONE",
		"host.name":     "LOCALHOST.",
		"host.apex":     "site",
		"acme.email":    "admin@example.com",
	})
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
	deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns1"}, nil }
	deps.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
	deps.Execute = func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("host execution invoked for invalid apex request")
		return host.Result{}, nil
	}
	before := treeState(t, deps.Root)

	wantStdout := "nginx: ok (/bin/nginx)\n" +
		"certbot: ok (/bin/certbot)\n" +
		"systemctl: ok (/bin/systemctl)\n" +
		"litestream: ok (/bin/litestream)\n" +
		"dns.provider: ok (route53)\n" +
		"dns.zones: ok (localhost)\n" +
		"host.name: ok (localhost)\ntimeouts: ok (drain 5s, stop 10s)\n" +
		"zone localhost: ok (route53 ZONE, 1 nameservers delegated)\n" +
		"host localhost: ok (zone localhost)\n" +
		"wildcard localhost: ok (192.0.2.1)\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 1 || stdout != wantStdout || stderr != "opsctl: host.apex is set but host.name 'localhost' has no parent domain\n" {
		t.Errorf("exit %d stdout %q stderr %q, want exit 1 stdout %q and apex diagnostic", code, stdout, stderr, wantStdout)
	}
	if after := treeState(t, deps.Root); !reflect.DeepEqual(after, before) {
		t.Errorf("Root changed:\nbefore %#v\nafter  %#v", before, after)
	}
}

func TestInitAggregatesIndependentFailures(t *testing.T) {
	// R-LIU3-NA5F R-LK20-11W4 R-LMHS-SLDI R-ELKW-EVLN
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
		"litestream: ok (/usr/bin/litestream)\n" +
		"dns.provider: failed: not set\n" +
		"dns.zones: ok (example.com)\n" +
		"host.name: failed: not set\ntimeouts: ok (drain 5s, stop 10s)\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}
}

func TestInitClassifiesDNSOpenFailures(t *testing.T) {
	// R-LK20-11W4 R-LMHS-SLDI
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
			want := "nginx: ok (/bin/nginx)\ncertbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\nlitestream: ok (/bin/litestream)\n" +
				tc.wantDNS + "host.name: failed: not set\ntimeouts: ok (drain 5s, stop 10s)\n"
			if code != 2 || stderr != "" || stdout != want {
				t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
			}
		})
	}
}

func TestInitReportsZoneHostAndWildcardFailures(t *testing.T) {
	// R-ZAOK-6AFV R-LOXL-K4UW R-LQ5H-XWLL
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
	dnsStdout, dnsStderr, dnsCode := invoke([]string{"dns", "check"}, deps)
	if dnsCode != 1 || dnsStderr != "" {
		t.Fatalf("dns check: exit %d stdout %q stderr %q", dnsCode, dnsStdout, dnsStderr)
	}

	prefix := "nginx: ok (/bin/nginx)\ncertbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\nlitestream: ok (/bin/litestream)\n" +
		"dns.provider: ok (route53)\ndns.zones: ok (wrong.test,undelegated.test,error.test)\n" +
		"host.name: ok (outside.example)\ntimeouts: ok (drain 5s, stop 10s)\n" +
		"zone wrong.test: failed: provider zone name is provider.test\n" +
		"zone undelegated.test: failed: nameservers not delegated\n" +
		"zone error.test: failed: unreachable\n" +
		"host outside.example: failed: no configured zone contains it\n"
	want := prefix + "wildcard outside.example: failed: outside.example resolves to 192.0.2.8 but _opsctl-preflight.outside.example resolves to 192.0.2.9\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}
	var wantZoneLines, gotZoneLines strings.Builder
	for line := range strings.Lines(dnsStdout) {
		wantZoneLines.WriteString("zone ")
		wantZoneLines.WriteString(line)
	}
	for line := range strings.Lines(stdout) {
		if strings.HasPrefix(line, "zone ") {
			gotZoneLines.WriteString(line)
		}
	}
	if gotZoneLines.String() != wantZoneLines.String() {
		t.Errorf("init zone lines %q, want dns check lines with prefix %q",
			gotZoneLines.String(), wantZoneLines.String())
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

func TestInitReportEscapesExternalLineBreaks(t *testing.T) {
	// R-ZAOK-6AFV
	provider := &fakeDNSProvider{
		recordsErr: map[string]error{"ZONE": errors.New("zone\nfailed\rhard")},
	}
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53",
		dns.KeyZones:    "example.com:ZONE",
		"host.name":     "example.com",
	})
	deps.LookPath = func(name string) (string, error) {
		return "/tools/" + name + "\nspoof\rline", nil
	}
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
	deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
		if strings.HasPrefix(name, "_opsctl-preflight.") {
			return nil, errors.New("probe\nfailed\rhard")
		}
		return []string{"192.0.2.1"}, nil
	}

	want := "nginx: ok (/tools/nginx\\nspoof\\rline)\n" +
		"certbot: ok (/tools/certbot\\nspoof\\rline)\n" +
		"systemctl: ok (/tools/systemctl\\nspoof\\rline)\n" +
		"litestream: ok (/tools/litestream\\nspoof\\rline)\n" +
		"dns.provider: ok (route53)\n" +
		"dns.zones: ok (example.com)\n" +
		"host.name: ok (example.com)\ntimeouts: ok (drain 5s, stop 10s)\n" +
		"zone example.com: failed: zone\\nfailed\\rhard\n" +
		"host example.com: ok (zone example.com)\n" +
		"wildcard example.com: failed: probe\\nfailed\\rhard\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}
	if lines := strings.Count(stdout, "\n"); lines != 11 {
		t.Errorf("init wrote %d lines for eleven eligible checks: %q", lines, stdout)
	}
}

func TestInitReportEscapesRereadZoneConfiguration(t *testing.T) {
	// R-ZAOK-6AFV
	const (
		providerName = "route\n53"
		zoneName     = "exa\nmple.com"
		zoneID       = "ZONE\rID"
	)
	provider := &fakeDNSProvider{records: map[string][]dns.Record{
		zoneID: {
			{Name: zoneName, Type: "SOA"},
			{Name: zoneName, Type: "NS", Values: []string{"ns1"}},
		},
	}}
	deps := initDeps(t, nil)
	writeInitConfig(t, deps.Root,
		`{"dns.provider":"route\n53","dns.zones":"exa\nmple.com:ZONE\rID"}`)
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(_ context.Context, name string) (dns.Provider, error) {
		if name != providerName {
			t.Fatalf("provider name = %q, want %q", name, providerName)
		}
		return provider, nil
	}
	deps.DNS.LookupNS = func(_ context.Context, name string) ([]string, error) {
		if name != zoneName {
			t.Fatalf("lookup zone = %q, want %q", name, zoneName)
		}
		return []string{"ns1"}, nil
	}

	want := "nginx: ok (/bin/nginx)\n" +
		"certbot: ok (/bin/certbot)\n" +
		"systemctl: ok (/bin/systemctl)\n" +
		"litestream: ok (/bin/litestream)\n" +
		"dns.provider: ok (route\\n53)\n" +
		"dns.zones: ok (exa\\nmple.com)\n" +
		"host.name: failed: not set\ntimeouts: ok (drain 5s, stop 10s)\n" +
		"zone exa\\nmple.com: ok (route\\n53 ZONE\\rID, 1 nameservers delegated)\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}
	if lines := strings.Count(stdout, "\n"); lines != 9 {
		t.Errorf("init wrote %d lines for nine eligible checks: %q", lines, stdout)
	}
}

func TestInitReportEscapesProviderAndHostLookupErrors(t *testing.T) {
	//
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53",
		"host.name":     "example.com",
	})
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) {
		return nil, errors.New("provider\nfailed\rhard")
	}
	deps.LookupHost = func(context.Context, string) ([]string, error) {
		return nil, errors.New("host\nfailed\rhard")
	}

	want := "nginx: ok (/bin/nginx)\n" +
		"certbot: ok (/bin/certbot)\n" +
		"systemctl: ok (/bin/systemctl)\n" +
		"litestream: ok (/bin/litestream)\n" +
		"dns.provider: failed: provider\\nfailed\\rhard\n" +
		"dns.zones: failed: not set\n" +
		"host.name: ok (example.com)\ntimeouts: ok (drain 5s, stop 10s)\n" +
		"wildcard example.com: failed: host\\nfailed\\rhard\n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}
	if lines := strings.Count(stdout, "\n"); lines != 9 {
		t.Errorf("init wrote %d lines for nine eligible checks: %q", lines, stdout)
	}
}

func TestInitRejectsEmptyWildcardAddressSet(t *testing.T) {
	deps := initDeps(t, map[string]string{"host.name": "example.com"})
	deps.LookPath = foundInitTools
	deps.LookupHost = func(context.Context, string) ([]string, error) { return nil, nil }

	want := "nginx: ok (/bin/nginx)\ncertbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\nlitestream: ok (/bin/litestream)\n" +
		"dns.provider: failed: not set\ndns.zones: failed: not set\nhost.name: ok (example.com)\ntimeouts: ok (drain 5s, stop 10s)\n" +
		"wildcard example.com: failed: example.com resolves to  but _opsctl-preflight.example.com resolves to \n"
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 2 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q, want exit 2 stdout %q", code, stdout, stderr, want)
	}
}

func TestInitSuccessfulSetupIsRepeatable(t *testing.T) {
	// R-LW8Z-URB2 R-JWO0-EHD7 R-Y3WS-9B9C R-X3BS-S70I
	provider := &fakeDNSProvider{records: map[string][]dns.Record{
		"ZONE": {
			{Name: "example.com", Type: "SOA"},
			{Name: "example.com", Type: "NS", Values: []string{"ns1"}},
		},
	}}
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53", dns.KeyZones: "example.com:ZONE", "host.name": "example.com",
		"acme.email": "admin@example.com", "aws.region": "us-east-2", "backup.s3_uri": "s3://bucket/host/",
		"apps.drain_seconds": "7", "apps.stop_seconds": "19",
	})
	if err := os.MkdirAll(filepath.Join(deps.Root, "etc/nginx/conf.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(deps.Root, "opt", "notes")
	for _, dir := range []string{filepath.Join(appDir, "bin"), filepath.Join(appDir, "etc"), filepath.Join(deps.Root, "etc", "systemd", "system")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{
		filepath.Join(appDir, "bin", "notes"):                                          "installed binary\n",
		filepath.Join(appDir, "etc", "env"):                                            "OTHER=keep\nDRAIN_SECONDS=5\n",
		filepath.Join(deps.Root, "etc", "systemd", "system", "ikigenba-notes.service"): "old service\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
	deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns1"}, nil }
	deps.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
	enabled := map[string]bool{}
	active := map[string]bool{}
	var appCommands []string
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		appCommands = append(appCommands, strings.Join(append([]string{command.Name}, command.Args...), " "))
		if command.Name == "systemctl" && len(command.Args) > 0 {
			switch command.Args[0] {
			case "show":
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
			case "is-active":
				return host.Result{Stdout: []byte("active\n")}, nil
			}
		}
		if command.Name == "certbot" {
			lineage := filepath.Join(deps.Root, "etc", "letsencrypt", "live", "example.com")
			if err := os.MkdirAll(lineage, 0o700); err != nil {
				t.Fatal(err)
			}
			certificate := filepath.Join(lineage, "fullchain.pem")
			if _, err := os.Stat(certificate); errors.Is(err, os.ErrNotExist) {
				if err := os.WriteFile(certificate, []byte("not-due-certificate\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			return host.Result{}, nil
		}
		if command.Name != "systemctl" || len(command.Args) < 2 {
			return host.Result{}, nil
		}
		unit := command.Args[len(command.Args)-1]
		switch command.Args[0] {
		case "enable":
			enabled[unit] = true
		case "disable":
			enabled[unit] = false
		case "start", "restart":
			active[unit] = true
		case "stop":
			active[unit] = false
		}
		return host.Result{}, nil
	}

	stdout1, stderr1, code1 := invoke([]string{"init"}, deps)
	firstCommands := append([]string(nil), appCommands...)
	afterFirst := treeState(t, deps.Root)
	enabledAfterFirst := cloneBoolMap(enabled)
	activeAfterFirst := cloneBoolMap(active)
	stdout2, stderr2, code2 := invoke([]string{"init"}, deps)
	secondCommands := append([]string(nil), appCommands[len(firstCommands):]...)
	afterSecond := treeState(t, deps.Root)
	want := "nginx: ok (/bin/nginx)\ncertbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\nlitestream: ok (/bin/litestream)\n" +
		"dns.provider: ok (route53)\ndns.zones: ok (example.com)\nhost.name: ok (example.com)\ntimeouts: ok (drain 7s, stop 19s)\n" +
		"zone example.com: ok (route53 ZONE, 1 nameservers delegated)\n" +
		"host example.com: ok (zone example.com)\nwildcard example.com: ok (192.0.2.1)\n"
	if stdout1 != want || stderr1 != "" || code1 != 0 {
		t.Errorf("first run = (%q, %q, %d), want (%q, empty, 0)", stdout1, stderr1, code1, want)
	}
	if stdout1 != stdout2 || stderr1 != stderr2 || code1 != code2 {
		t.Errorf("runs differ: (%q, %q, %d) then (%q, %q, %d)", stdout1, stderr1, code1, stdout2, stderr2, code2)
	}
	if !reflect.DeepEqual(afterFirst, afterSecond) {
		t.Errorf("second setup changed generated state:\nfirst  %#v\nsecond %#v", afterFirst, afterSecond)
	}
	appEnv, err := os.ReadFile(filepath.Join(deps.Root, "opt", "notes", "etc", "env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(appEnv) != "OTHER=keep\nDRAIN_SECONDS=7\n" {
		t.Errorf("app env = %q, want current drain setting", appEnv)
	}
	service, err := os.ReadFile(filepath.Join(deps.Root, "etc", "systemd", "system", "ikigenba-notes.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(service), "TimeoutStopSec=19\n") {
		t.Errorf("app service does not contain current stop setting: %q", service)
	}
	firstJoined := strings.Join(firstCommands, "\n")
	if strings.Count(firstJoined, "systemctl restart ikigenba-notes.service") != 1 ||
		strings.Index(firstJoined, "systemctl restart ikigenba-notes.service") < strings.Index(firstJoined, "systemctl restart ikigenba-renew-certificate.timer") {
		t.Errorf("app restart did not follow timers exactly once: %v", firstCommands)
	}
	for _, command := range secondCommands {
		if command == "systemctl restart ikigenba-notes.service" {
			t.Errorf("unchanged app settings caused mutation on rerun: %v", secondCommands)
			break
		}
	}
	if !reflect.DeepEqual(enabled, enabledAfterFirst) || !reflect.DeepEqual(active, activeAfterFirst) {
		t.Errorf("second setup changed configured unit state: enabled %v -> %v, active %v -> %v",
			enabledAfterFirst, enabled, activeAfterFirst, active)
	}
	for _, unit := range []string{"ikigenba-backup-host.timer", "ikigenba-backup-services.timer"} {
		if enabled[unit] || active[unit] {
			t.Errorf("zero-period %s state = enabled %t active %t, want disabled and stopped", unit, enabled[unit], active[unit])
		}
	}
	const renewal = "ikigenba-renew-certificate.timer"
	if !enabled[renewal] || !active[renewal] {
		t.Errorf("renewal timer state = enabled %t active %t, want enabled and active", enabled[renewal], active[renewal])
	}
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
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

func writeInitConfig(t *testing.T, root, contents string) {
	t.Helper()
	dir := filepath.Join(root, "etc", "ikigenba")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
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
