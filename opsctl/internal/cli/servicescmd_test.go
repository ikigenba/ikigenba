package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// servicesFixtureCommand supplies process results for publication's account and
// ownership setup. Other commands remain visible to each workflow fixture.
func servicesFixtureCommand(command host.Command) (host.Result, bool) {
	if command.Name == "id" && strings.Join(command.Args, " ") == "--user ikigenba" {
		return host.Result{Stdout: []byte("998\n")}, true
	}
	if command.Name == "id" && strings.Join(command.Args, " ") == "--group --name ikigenba" {
		return host.Result{Stdout: []byte("ikigenba\n")}, true
	}
	if command.Name == "chown" && len(command.Args) == 3 && command.Args[0] == "root:ikigenba" && strings.HasSuffix(command.Args[1], "/var/lib/ikigenba") {
		return host.Result{}, true
	}
	return host.Result{}, false
}

func TestInstallServicesStagePublishesNormalizedNameAndStopsOnError(t *testing.T) {
	// R-YNH5-YAF8
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			fixture := newCLIInstallFixture(t)
			if err := os.RemoveAll(filepath.Join(fixture.root, "var")); err != nil {
				t.Fatal(err)
			}
			// A blocker in var/lib is encountered only by services publication.
			if fail {
				writeCLIInstallFile(t, filepath.Join(fixture.root, "var", "lib"), "blocked")
			}
			stdout, stderr, code := fixture.invoke()
			if fail {
				if code != 1 || stderr != "opsctl: install failed\n" || !strings.Contains(stdout, "\nservices: failed: ") || strings.Contains(stdout, "litestream:") || strings.Contains(stdout, "service:") {
					t.Fatalf("outcome %d %q %q", code, stdout, stderr)
				}
				if fixture.commandCount("systemctl start ikigenba-notes.service") != 0 {
					t.Fatal("service started after services failure")
				}
			} else {
				if code != 0 || stderr != "" || !strings.Contains(stdout, "nginx: ok (notes.host.example, host.example)\nservices: ok (notes added)\nlitestream: ok (state/notes.db)\n") {
					t.Fatalf("outcome %d %q %q", code, stdout, stderr)
				}
				data, err := os.ReadFile(filepath.Join(fixture.root, "var/lib/ikigenba/services.json"))
				if err != nil || !strings.Contains(string(data), `"name": "notes"`) || !strings.Contains(string(data), `"socket": "/run/ikigenba/notes.sock"`) {
					t.Fatalf("services %q %v", data, err)
				}
			}
			calls := 0
			for _, command := range fixture.commands {
				if command.Name == "id" && strings.Join(command.Args, " ") == "--user ikigenba" {
					calls++
				}
			}
			if calls != 2 {
				t.Fatalf("account calls = %d, want install plus exactly one services publication", calls)
			}
		})
	}
}

func TestNginxServicesPublicationAfterSuccessOnly(t *testing.T) {
	// R-K5Q5-3WQS R-8SVM-0ZF3
	for _, failure := range []string{"", "nginx", "services"} {
		t.Run(failure, func(t *testing.T) {
			root := configuredNginxRoot(t)
			if err := os.MkdirAll(filepath.Join(root, "etc/nginx/conf.d"), 0o750); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "var/lib/ikigenba/services.json")
			writeCLIInstallFile(t, path, "previous\n")
			var calls []string
			deps := cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
				calls = append(calls, c.Name+" "+strings.Join(c.Args, " "))
				if (failure == "nginx" && c.Name == "nginx") || (failure == "services" && c.Name == "chown") {
					return host.Result{ExitCode: 7, Stdout: []byte("out\n"), Stderr: []byte("err")}, nil
				}
				if result, ok := servicesFixtureCommand(c); ok {
					return result, nil
				}
				return host.Result{}, nil
			}}
			stdout, stderr, code := invoke([]string{"nginx", "apply"}, deps)
			filesystem, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = filesystem.Close() })
			data, err := filesystem.ReadFile("var/lib/ikigenba/services.json")
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(calls, "\n")
			if stdout != "" {
				t.Fatalf("stdout %q", stdout)
			}
			if failure == "" {
				if code != 0 || stderr != "" || string(data) != "{\n  \"services\": []\n}\n" {
					t.Fatalf("success %d %q %q", code, stderr, data)
				}
			} else {
				if code != 1 || !strings.HasSuffix(stderr, "\n\n> out\n> err\n") || string(data) != "previous\n" {
					t.Fatalf("failure %d %q %q", code, stderr, data)
				}
			}
			count := strings.Count(joined, "id --user ikigenba")
			want := 1
			if failure == "nginx" {
				want = 0
			}
			if count != want {
				t.Fatalf("services calls %d, want %d: %s", count, want, joined)
			}
			if strings.Count(joined, "systemctl reload-or-restart nginx") > 1 || strings.Count(joined, "nginx -t") != 1 {
				t.Fatalf("nginx repeated: %s", joined)
			}
		})
	}
}

func TestLifecycleServicesStageRunsOnceAfterNginx(t *testing.T) {
	// R-YNH5-YAF8
	for _, action := range []string{"disable", "enable", "uninstall"} {
		for _, failure := range []string{"", "before nginx", "nginx", "services"} {
			t.Run(action+"/"+failure, func(t *testing.T) {
				root := uninstallCommandRoot(t, true)
				writeUninstallFile(t, root, "opt/notes/share/icon.svg", "notes")
				if action == "enable" {
					writeUninstallFile(t, root, "var/lib/ikigenba/services.json", `{ "services": [{"name":"notes","url":"https://notes.example.test","icon":"notes","enabled":false}] }`)
				}
				disabled := action == "enable"
				count := 0
				nginxDone := false
				afterServices := false
				deps := cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
					args := strings.Join(c.Args, " ")
					if c.Name == "id" && args == "--user ikigenba" {
						count++
						if !nginxDone {
							t.Fatal("services before nginx success")
						}
					}
					if c.Name == "chown" && len(c.Args) == 3 && strings.HasSuffix(c.Args[1], "/var/lib/ikigenba") {
						if failure == "services" {
							return host.Result{}, errors.New("blocked\r\nnow")
						}
						afterServices = true
					}
					if c.Name == "nginx" {
						if failure == "nginx" {
							return host.Result{ExitCode: 7}, nil
						}
					}
					if c.Name == "systemctl" {
						if c.Args[0] == "show" {
							state := "enabled"
							active := "active"
							if disabled {
								state = "disabled"
								active = "inactive"
							}
							return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\nActiveState=" + active + "\n")}, nil
						}
						if c.Args[0] == "is-active" {
							return host.Result{Stdout: []byte("active\n")}, nil
						}
						if failure == "before nginx" && (c.Args[0] == "stop" || c.Args[0] == "enable") {
							return host.Result{ExitCode: 7}, nil
						}
						if args == "reload-or-restart nginx" {
							nginxDone = true
						}
						if c.Args[0] == "disable" {
							disabled = true
						}
						if c.Args[0] == "enable" {
							disabled = false
						}
						if (args == "start ikigenba-notes.service" || args == "restart litestream.service") && !afterServices {
							t.Fatal("later action before services publication")
						}
					}
					if c.Name == filepath.Join(root, "opt/notes/bin/notes") {
						return host.Result{Stdout: []byte("v1\n")}, nil
					}
					if result, ok := servicesFixtureCommand(c); ok {
						return result, nil
					}
					return host.Result{}, nil
				}}
				stdout, stderr, code := invoke([]string{action, "notes"}, deps)
				wantCount := 1
				if failure == "nginx" || failure == "before nginx" {
					wantCount = 0
				}
				if count != wantCount {
					t.Fatalf("calls %d, want %d", count, wantCount)
				}
				if failure == "" {
					want := "services: ok (notes removed)\n"
					if action == "disable" {
						want = "services: ok (notes disabled)\n"
					}
					if action == "enable" {
						want = "services: ok (notes enabled)\n"
					}
					if code != 0 || stderr != "" || !strings.Contains(stdout, want) {
						t.Fatalf("success %d %q %q", code, stdout, stderr)
					}
				} else {
					if code != 1 || stderr != "opsctl: "+action+" failed\n" {
						t.Fatalf("failure %d %q %q", code, stdout, stderr)
					}
					if failure == "services" && (!strings.HasSuffix(stdout, `services: failed: own services file: blocked\r\nnow`+"\n") || strings.Contains(stdout, "litestream:") || strings.Contains(stdout, "service:")) {
						t.Fatalf("services failure report %q", stdout)
					}
					if wantCount == 0 && strings.Contains(stdout, "services:") {
						t.Fatalf("reported unattempted services: %q", stdout)
					}
				}
			})
		}
	}
}

func TestInitServicesPublicationOrdersSetupAndPassesDependencies(t *testing.T) {
	// R-341K-A2GT R-8SVM-0ZF3
	for _, failure := range []string{"", "nginx", "services"} {
		for _, disabled := range []bool{false, true} {
			t.Run(failure+fmt.Sprint(disabled), func(t *testing.T) {
				deps := initDeps(t, map[string]string{dns.KeyProvider: "route53", dns.KeyZones: "example.test:Z", "host.name": "API.Example.Test.", "acme.email": "admin@example.test", "aws.region": "region", "backup.s3_uri": "s3://bucket/host/", "backup.host_files_seconds": "0", "backup.service_files_seconds": "0", "backup.service_db_seconds": "3600", "backup.service_wal_seconds": "1", "apps.drain_seconds": "7", "apps.stop_seconds": "19"})
				provider := &fakeDNSProvider{records: map[string][]dns.Record{"Z": {{Name: "example.test", Type: "SOA"}, {Name: "example.test", Type: "NS", Values: []string{"ns1"}}}}}
				deps.LookPath = foundInitTools
				deps.DNS.Open = func(context.Context, string) (dns.Provider, error) { return provider, nil }
				deps.DNS.LookupNS = func(context.Context, string) ([]string, error) { return []string{"ns1"}, nil }
				deps.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
				writeUninstallFile(t, deps.Root, "etc/nginx/conf.d/ikigenba.conf", "previous")
				writeUninstallFile(t, deps.Root, "var/lib/ikigenba/services.json", "previous services\n")
				writeUninstallFile(t, deps.Root, "opt/notes/etc/manifest.toml", "app = \"notes\"\n")
				writeUninstallFile(t, deps.Root, "etc/opt/ikigenba/notes/env", "DRAIN_SECONDS=5\nOTHER=keep\n")
				writeUninstallFile(t, deps.Root, "opt/notes/bin/notes", "binary")
				nginxDone := false
				servicesDone := false
				calls := 0
				restarts := 0
				deps.Execute = func(_ context.Context, c host.Command) (host.Result, error) {
					args := strings.Join(c.Args, " ")
					if c.Name == "nginx" && failure == "nginx" {
						return host.Result{ExitCode: 7}, nil
					}
					if args == "reload-or-restart nginx" {
						nginxDone = true
					}
					if c.Name == "id" && args == "--user ikigenba" {
						calls++
						if !nginxDone {
							t.Fatal("services before nginx")
						}
					}
					if c.Name == "chown" && len(c.Args) == 3 && strings.HasSuffix(c.Args[1], "/var/lib/ikigenba") {
						if failure == "services" {
							return host.Result{}, errors.New("services blocked")
						}
						servicesDone = true
					}
					if c.Name == "systemctl" {
						if c.Args[0] == "show" {
							state := "enabled"
							if disabled {
								state = "disabled"
							}
							return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\n")}, nil
						}
						if c.Args[0] == "is-active" {
							return host.Result{Stdout: []byte("active\n")}, nil
						}
						if args == "enable litestream.service" && !servicesDone {
							t.Fatal("replication before services")
						}
						if args == "restart ikigenba-notes.service" {
							restarts++
						}
					}
					if result, ok := servicesFixtureCommand(c); ok {
						return result, nil
					}
					return host.Result{}, nil
				}
				stdout, stderr, code := invoke([]string{"init"}, deps)
				if strings.Contains(stdout, "services:") {
					t.Fatalf("services leaked to preflight: %q", stdout)
				}
				wantCalls := 1
				if failure == "nginx" {
					wantCalls = 0
				}
				if calls != wantCalls {
					t.Fatalf("calls %d want %d outcome %d %q %q", calls, wantCalls, code, stdout, stderr)
				}
				if failure == "nginx" {
					data, err := os.ReadFile(filepath.Join(deps.Root, "var/lib/ikigenba/services.json"))
					if err != nil || string(data) != "previous services\n" {
						t.Fatalf("nginx failure changed services file: %q %v", data, err)
					}
				}
				if failure != "" {
					if code != 1 || stderr == "" {
						t.Fatalf("failure %d %q", code, stderr)
					}
					if _, err := os.Stat(filepath.Join(deps.Root, "etc/litestream.yml")); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("later setup ran: %v", err)
					}
					return
				}
				if code != 0 || stderr != "" {
					t.Fatalf("success %d %q", code, stderr)
				}
				data, err := os.ReadFile(filepath.Join(deps.Root, "etc/opt/ikigenba/notes/env"))
				if err != nil || string(data) != "DRAIN_SECONDS=7\nOTHER=keep\n"+apps.ServicesEnv+"="+apps.ServicesPath+"\n" {
					t.Fatalf("env %q %v", data, err)
				}
				unit, err := os.ReadFile(filepath.Join(deps.Root, "etc/systemd/system/ikigenba-notes.service"))
				if err != nil || !strings.Contains(string(unit), "TimeoutStopSec=19\n") {
					t.Fatalf("unit %q %v", unit, err)
				}
				wantRestarts := 1
				if disabled {
					wantRestarts = 0
				}
				if restarts != wantRestarts {
					t.Fatalf("restarts %d want %d", restarts, wantRestarts)
				}
			})
		}
	}
}

func TestInstallServicesReportsEntryChangesWithAndWithoutIcons(t *testing.T) {
	// R-YNH5-YAF8
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			fixture := newCLIInstallFixture(t)
			if err := os.Remove(filepath.Join(fixture.root, apps.ServicesPath)); err != nil {
				t.Fatal(err)
			}
			fixture.disabled = disabled
			base := fixture.manifest
			for _, step := range []struct {
				name, description, icon, change string
				mcp                             bool
			}{
				{name: "iconless first install", change: "added"},
				{name: "iconless reinstall", change: "unchanged"},
				{name: "description changed", description: "Notes service", change: "updated"},
				{name: "MCP changed", description: "Notes service", mcp: true, change: "updated"},
				{name: "icon gained", description: "Notes service", mcp: true, icon: "<svg/>", change: "updated"},
				{name: "icon changed", description: "Notes service", mcp: true, icon: "<svg>updated</svg>", change: "updated"},
				{name: "icon lost", description: "Notes service", mcp: true, change: "updated"},
			} {
				fixture.manifest = fmt.Sprintf("description = %q\nmcp = %t\n", step.description, step.mcp) + base
				fixture.icon = step.icon
				stdout, stderr, code := fixture.invoke()
				want := "services: ok (notes " + step.change + ")\n"
				if step.change == "unchanged" {
					want = "services: ok (unchanged)\n"
				}
				if code != 0 || stderr != "" || !strings.Contains(stdout, want) {
					t.Fatalf("%s outcome %d %q %q", step.name, code, stdout, stderr)
				}
				data, err := os.ReadFile(filepath.Join(fixture.root, apps.ServicesPath))
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Services []struct {
						Name, URL, Description, Socket string
						Icon                           *string
						Enabled, MCP                   bool
					}
				}
				if err := json.Unmarshal(data, &document); err != nil {
					t.Fatal(err)
				}
				if len(document.Services) != 1 {
					t.Fatalf("%s entry count %s", step.name, data)
				}
				entry := document.Services[0]
				if entry.Name != "notes" || entry.URL != "https://notes.host.example" || entry.Description != step.description || entry.Socket != "/run/ikigenba/notes.sock" || entry.Enabled == disabled || entry.MCP != step.mcp {
					t.Fatalf("%s entry %s", step.name, data)
				}
				if (entry.Icon == nil) != (step.icon == "") || entry.Icon != nil && *entry.Icon != step.icon {
					t.Fatalf("%s icon %s", step.name, data)
				}
			}
		})
	}
}
