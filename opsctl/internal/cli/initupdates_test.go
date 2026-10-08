package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInitLooksUpEveryToolInOrder(t *testing.T) {
	// R-79AT-YTAY
	names := []string{"nginx", "certbot", "systemctl", "litestream", "git"}
	for _, missing := range append([]string{""}, names...) {
		t.Run("missing="+missing, func(t *testing.T) {
			deps := initDeps(t, nil)
			deps.Now = func() time.Time { return time.Date(2031, 4, 2, 12, 0, 0, 0, time.UTC) }
			deps.Getenv = func(string) string { return "" }
			var lookedUp []string
			deps.LookPath = func(name string) (string, error) {
				lookedUp = append(lookedUp, name)
				if name == missing {
					return "", errors.New("lookup failed")
				}
				return "/fixture/tools/" + name, nil
			}
			stdout, _, _ := invoke([]string{"init"}, deps)
			if !reflect.DeepEqual(lookedUp, names) {
				t.Fatalf("LookPath calls = %v, want %v", lookedUp, names)
			}
			var want strings.Builder
			for _, name := range names {
				if name == missing {
					_, _ = fmt.Fprintf(&want, "%s: failed: not found on PATH\n", name)
				} else {
					_, _ = fmt.Fprintf(&want, "%s: ok (/fixture/tools/%s)\n", name, name)
				}
			}
			if !strings.HasPrefix(stdout, want.String()) {
				t.Fatalf("tool report = %q, want prefix %q", stdout, want.String())
			}
		})
	}
}

func TestInitWritesCompleteReportBeforeSetup(t *testing.T) {
	// R-7AIQ-CL1N
	for _, finding := range []bool{false, true} {
		t.Run(fmt.Sprintf("finding=%t", finding), func(t *testing.T) {
			deps := initDeps(t, map[string]string{
				dns.KeyProvider: "route53", dns.KeyZones: "example.com:ZA,deep.example.com:ZB",
				"host.name": "API.Deep.Example.Com.", "acme.email": "operator@example.com",
			})
			deps.Now = func() time.Time { return time.Date(2031, 4, 2, 12, 0, 0, 0, time.UTC) }
			deps.Getenv = func(string) string { return "" }
			provider := &fakeDNSProvider{records: map[string][]dns.Record{
				"ZA": {{Name: "example.com", Type: "SOA"}, {Name: "example.com", Type: "NS", Values: []string{"ns1"}}},
				"ZB": {{Name: "deep.example.com", Type: "SOA"}, {Name: "deep.example.com", Type: "NS", Values: []string{"ns1"}}},
			}}
			deps.LookPath = func(name string) (string, error) {
				if finding && name == "nginx" {
					return "", errors.New("missing")
				}
				return foundInitTools(name)
			}
			deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
			deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns1"}, nil }
			var resolved []string
			deps.LookupHost = func(_ context.Context, name string) ([]string, error) {
				resolved = append(resolved, name)
				return []string{"192.0.2.1"}, nil
			}
			nginxLine := "nginx: ok (/bin/nginx)\n"
			if finding {
				nginxLine = "nginx: failed: not found on PATH\n"
				provider.recordsErr = map[string]error{"ZA": errors.New("unreachable")}
			}
			zoneLine := "zone example.com: ok (route53 ZA, 1 nameservers delegated)\n"
			if finding {
				zoneLine = "zone example.com: failed: unreachable\n"
			}
			want := nginxLine +
				"certbot: ok (/bin/certbot)\nsystemctl: ok (/bin/systemctl)\nlitestream: ok (/bin/litestream)\ngit: ok (/bin/git)\n" +
				"dns.provider: ok (route53)\ndns.zones: ok (example.com,deep.example.com)\nhost.name: ok (api.deep.example.com)\n" +
				"timeouts: ok (drain 5s, stop 10s)\n" + zoneLine +
				"zone deep.example.com: ok (route53 ZB, 1 nameservers delegated)\n" +
				"host api.deep.example.com: ok (zone deep.example.com)\nwildcard api.deep.example.com: ok (192.0.2.1)\n"
			var stdout, stderr bytes.Buffer
			beforeReport := treeState(t, deps.Root)
			output := initReportWriter{write: func(data []byte) (int, error) {
				if !reflect.DeepEqual(treeState(t, deps.Root), beforeReport) {
					t.Fatal("persistent setup state changed before the complete report was written")
				}
				return stdout.Write(data)
			}}
			setupObserved := false
			deps.Execute = func(context.Context, host.Command) (host.Result, error) {
				setupObserved = true
				if stdout.String() != want {
					t.Fatalf("setup started with incomplete report %q, want %q", stdout.String(), want)
				}
				return host.Result{}, errors.New("end fixture at first setup action")
			}
			cli.Run([]string{"init"}, nil, output, &stderr, deps)
			if stdout.String() != want {
				t.Fatalf("report = %q, want %q", stdout.String(), want)
			}
			for line := range strings.Lines(want) {
				if strings.Contains(stderr.String(), line) {
					t.Errorf("report line was also written to stderr: %q", line)
				}
			}
			if !finding && !setupObserved {
				t.Fatal("fixture never observed setup after its successful checks")
			}
			if !reflect.DeepEqual(provider.recordCalls, []string{"ZA", "ZB"}) ||
				!reflect.DeepEqual(resolved, []string{"api.deep.example.com", "_opsctl-preflight.api.deep.example.com"}) {
				t.Fatalf("eligible checks: zones %v, host lookups %v", provider.recordCalls, resolved)
			}
		})
	}
}

type initReportWriter struct {
	write func([]byte) (int, error)
}

func (w initReportWriter) Write(data []byte) (int, error) { return w.write(data) }

func TestInitAppsStepUsesCurrentSettingsAndKeepsResources(t *testing.T) {
	// R-YRDI-GFZ1
	deps := initDeps(t, map[string]string{
		dns.KeyProvider: "route53", dns.KeyZones: "example.com:ZONE", "host.name": "HOST.Example.Com.",
		"acme.email": "operator@example.com", "aws.region": "us-east-2", "backup.s3_uri": "s3://bucket/host/",
		"apps.drain_seconds": "7", "apps.stop_seconds": "19",
	})
	deps.Now = func() time.Time { return time.Date(2031, 4, 2, 12, 0, 0, 0, time.UTC) }
	deps.Getenv = func(string) string { return "" }
	if err := os.MkdirAll(filepath.Join(deps.Root, "etc/nginx/conf.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes", "tasks", "todos"} {
		appDir := filepath.Join(deps.Root, "opt", name)
		writeCLIInstallFile(t, filepath.Join(appDir, "bin", name), "installed binary\n")
		env := "DRAIN_SECONDS=5\n"
		if name == "notes" {
			env += apps.ServicesEnv + "=old-path\n"
		}
		writeCLIInstallFile(t, filepath.Join(deps.Root, "etc/opt/ikigenba", name, "env"), env)
		writeCLIInstallFile(t, filepath.Join(appDir, "etc", "env"), "LEGACY=preserved\nDRAIN_SECONDS=1\n")
		writeCLIInstallFile(t, filepath.Join(appDir, "etc", "manifest.toml"), "app = \""+name+"\"\n[resources]\ncpu_weight = 350\nmemory_max = \"512M\"\nslice = \"core\"\ngo_memory_limit = \"384M\"\ndelegate = true\noom_policy = \"continue\"\n")
		if name == "todos" {
			writeCLIInstallFile(t, filepath.Join(appDir, "etc", "manifest.toml"), "app = \"todos\"\n")
		}
		writeCLIInstallFile(t, filepath.Join(deps.Root, "etc/systemd/system", "ikigenba-"+name+".service"), "old service\n")
	}
	writeCLIInstallFile(t, filepath.Join(deps.Root, "var/opt/ikigenba/orphan/state/kept"), "data only\n")
	provider := &fakeDNSProvider{records: map[string][]dns.Record{
		"ZONE": {{Name: "example.com", Type: "SOA"}, {Name: "example.com", Type: "NS", Values: []string{"ns1"}}},
	}}
	deps.LookPath = foundInitTools
	deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
	deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns1"}, nil }
	deps.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
	var controls []string
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		if result, handled := servicesFixtureCommand(command); handled {
			return result, nil
		}
		if command.Name == "systemctl" && len(command.Args) > 0 {
			unit := command.Args[len(command.Args)-1]
			switch command.Args[0] {
			case "show":
				state := "enabled"
				if unit != "ikigenba-notes.socket" {
					state = "disabled"
				}
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\n")}, nil
			case "is-active":
				return host.Result{Stdout: []byte("active\n")}, nil
			}
			if strings.HasPrefix(unit, "ikigenba-notes.") || strings.HasPrefix(unit, "ikigenba-tasks.") {
				controls = append(controls, strings.Join(command.Args, " "))
			}
		}
		return host.Result{}, nil
	}
	for run := range 3 {
		drain, stop := 7, 19
		if run == 2 {
			drain, stop = 9, 23
			store := config.Store{Root: deps.Root}
			for key, value := range map[string]string{"apps.drain_seconds": "9", "apps.stop_seconds": "23"} {
				if err := store.Set(key, value); err != nil {
					t.Fatal(err)
				}
			}
		}
		controls = nil
		_, stderr, code := invoke([]string{"init"}, deps)
		if code != 0 || stderr != "" {
			t.Fatalf("run %d failed: exit %d stderr %q", run, code, stderr)
		}
		for _, name := range []string{"notes", "tasks", "todos"} {
			env, err := fs.ReadFile(os.DirFS(deps.Root), "etc/opt/ikigenba/"+name+"/env")
			if err != nil {
				t.Fatal(err)
			}
			assertInitSettingLines(t, env, []string{fmt.Sprintf("DRAIN_SECONDS=%d\n", drain), apps.ServicesEnv + "=" + apps.ServicesPath + "\n"})
			legacy, err := fs.ReadFile(os.DirFS(deps.Root), "opt/"+name+"/etc/env")
			if err != nil || string(legacy) != "LEGACY=preserved\nDRAIN_SECONDS=1\n" {
				t.Fatalf("legacy environment changed: %q, %v", legacy, err)
			}
			unit, err := fs.ReadFile(os.DirFS(deps.Root), "etc/systemd/system/ikigenba-"+name+".service")
			if err != nil {
				t.Fatal(err)
			}
			if name == "todos" {
				assertInitSettingLines(t, unit, []string{fmt.Sprintf("TimeoutStopSec=%d\n", stop), "EnvironmentFile=" + filepath.Join(deps.Root, "etc/opt/ikigenba", name, "env") + "\n", "CPUWeight=100\n", "MemoryMax=134217728\n", "Slice=ikigenba-apps.slice\n", "Environment=GOMEMLIMIT=100663296\n"})
			} else {
				assertInitSettingLines(t, unit, []string{fmt.Sprintf("TimeoutStopSec=%d\n", stop), "EnvironmentFile=" + filepath.Join(deps.Root, "etc/opt/ikigenba", name, "env") + "\n", "CPUWeight=350\n", "MemoryMax=536870912\n", "Slice=ikigenba-core.slice\n", "MemoryLow=32M\n", "Environment=GOMEMLIMIT=402653184\n", "Delegate=yes\n", "OOMPolicy=continue\n"})
			}
		}
		for _, relative := range []string{"opt/orphan", "etc/opt/ikigenba/orphan", "etc/systemd/system/ikigenba-orphan.service"} {
			if _, err := os.Lstat(filepath.Join(deps.Root, relative)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("data-only service acquired %s: %v", relative, err)
			}
		}
		data, err := os.ReadFile(filepath.Join(deps.Root, "var/opt/ikigenba/orphan/state/kept"))
		if err != nil || string(data) != "data only\n" {
			t.Fatalf("data-only service changed: %q, %v", data, err)
		}
		wantControls := []string{"restart ikigenba-notes.service"}
		if run == 1 {
			wantControls = nil
		}
		if !reflect.DeepEqual(controls, wantControls) {
			t.Errorf("run %d app controls = %v, want %v (tasks stays disabled)", run, controls, wantControls)
		}
	}
	before := map[string][]byte{}
	for _, name := range []string{"notes", "tasks", "todos"} {
		for _, path := range []string{"etc/opt/ikigenba/" + name + "/env", "etc/systemd/system/ikigenba-" + name + ".service"} {
			data, err := fs.ReadFile(os.DirFS(deps.Root), path)
			if err != nil {
				t.Fatal(err)
			}
			before[path] = data
		}
	}
	store := config.Store{Root: deps.Root}
	for key, value := range map[string]string{"apps.drain_seconds": "11", "apps.stop_seconds": "29"} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	execute := deps.Execute
	deps.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		result, err := execute(ctx, command)
		if command.Name == "systemctl" && strings.Join(command.Args, " ") == "restart ikigenba-renew-certificate.timer" {
			writeCLIInstallFile(t, filepath.Join(deps.Root, "opt/tasks/etc/manifest.toml"), "app = \"tasks\"\n[resources]\nio_weight = 50\n")
		}
		return result, err
	}
	controls = nil
	_, stderr, code := invoke([]string{"init"}, deps)
	want := "opsctl: tasks: etc/manifest.toml: 'resources.io_weight' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy\n"
	if code != 1 || stderr != want {
		t.Fatalf("invalid apps manifest = %d %q, want 1 %q", code, stderr, want)
	}
	for path, want := range before {
		got, err := fs.ReadFile(os.DirFS(deps.Root), path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("app changed on invalid manifest: %s = %q, %v", path, got, err)
		}
	}
	if len(controls) != 0 {
		t.Errorf("invalid apps manifest caused controls %v", controls)
	}

}

func assertInitSettingLines(t *testing.T, data []byte, settings []string) {
	t.Helper()
	for _, want := range settings {
		key, _, _ := strings.Cut(want, "=")
		var got []string
		for line := range strings.Lines(string(data)) {
			if strings.HasPrefix(line, key+"=") {
				got = append(got, line)
			}
		}
		if !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("%s settings = %q, want exactly %q", key, got, want)
		}
	}
}
