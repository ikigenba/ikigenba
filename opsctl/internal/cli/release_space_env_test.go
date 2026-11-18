package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func transitionSpaceFixture(t *testing.T, command string) (*transitionFixture, []string, release.Release) {
	t.Helper()
	f := newTransitionFixture(t)
	r := release.Release{SHA: transitionSHA, Label: "candidate/test"}
	if command == "activate" {
		return f, []string{command, r.SHA, r.Label}, r
	}
	r.SHA = transitionOld
	f.addRelease(r.SHA, "dummy")
	f.write("opt/ikigenba/releases/"+r.SHA+"/label", r.Label+"\n", 0o644)
	f.link("current", transitionSHA)
	f.link("previous", r.SHA)
	f.deps.Executable = func() (string, error) {
		return filepath.Join(f.root, "opt/ikigenba/releases", r.SHA, "opsctl/bin/opsctl"), nil
	}
	return f, []string{command}, r
}

func TestTransitionReadsConfigurationBeforeAnyStep(t *testing.T) {
	// R-3HE5-F0BG
	for _, command := range []string{"activate", "rollback"} {
		for _, tc := range []struct{ key, value, message string }{
			{"aws.region", "", "aws.region not set"},
			{"host.name", ".", "host.name not set"},
			{"host.apex", "dummy", "host.apex is set but host.name 'localhost' has no parent domain"},
			{"apps.stop_seconds", "4", "apps.stop_seconds (4) is not greater than apps.drain_seconds (5)"},
			{"corrupt store", "", ""},
		} {
			t.Run(command+"/"+tc.key, func(t *testing.T) {
				f, args, _ := transitionSpaceFixture(t, command)
				store := config.Store{Root: f.root}
				message := tc.message
				if tc.key == "corrupt store" {
					p := filepath.Join(config.Dir, config.FileName)
					f.write(p, `{"hosts.domain": false}`, 0o600)
					message = filepath.Join(f.root, p)
				} else {
					writeSpaceTestSettings(t, store, map[string]string{tc.key: tc.value})
					if tc.key == "host.apex" {
						writeSpaceTestSettings(t, store, map[string]string{"host.name": "LOCALHOST."})
					}
				}
				before := releaseTree(t, f.root)
				f.deps.Cloud.Open = func(context.Context, string) (cloud.Client, error) {
					t.Fatal("opened cloud after configuration failure")
					return nil, nil
				}
				code, out, diagnostic := f.run(args...)
				matches := diagnostic == "opsctl: "+message+"\n"
				if tc.key == "corrupt store" {
					matches = strings.HasPrefix(diagnostic, "opsctl: ") && strings.Contains(diagnostic, message) && strings.Count(diagnostic, "\n") == 1 && strings.HasSuffix(diagnostic, "\n")
				}
				if code != 1 || out != "" || !matches || len(f.commands) != 0 {
					t.Fatalf("exit=%d stdout=%q stderr=%q commands=%v", code, out, diagnostic, f.commands)
				}
				if !reflect.DeepEqual(before, releaseTree(t, f.root)) {
					t.Fatal("configuration failure changed host files")
				}
			})
		}
	}
}

func TestTransitionValidatesSpaceSettingsBeforeWriting(t *testing.T) {
	// R-3IM1-SS25
	for _, command := range []string{"activate", "rollback"} {
		t.Run(command, func(t *testing.T) {
			f, args, r := transitionSpaceFixture(t, command)
			writeTransitionSpaceManifests(f, r.SHA)
			writeSpaceTestSettings(t, config.Store{Root: f.root}, map[string]string{"hosts.zone_id": "invalid\x00value"})
			before := releaseTree(t, f.root)
			var calls []string
			f.deps.Cloud = transitionSpaceCloud(&calls)
			code, out, diagnostic := f.run(args...)
			want := "secrets: failed: alpha: IKIGENBA_HOSTS_ZONE_ID holds a NUL, CR or LF\n"
			if code != 1 || diagnostic != "opsctl: "+command+" failed\n" || !strings.HasSuffix(out, want) || strings.Contains(out, "invalid") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, diagnostic)
			}
			if !reflect.DeepEqual(calls, []string{"open region", "read /sbx.example.test/alpha"}) {
				t.Fatalf("cloud calls %v", calls)
			}
			if !reflect.DeepEqual(before, releaseTree(t, f.root)) {
				t.Fatal("secrets step failure changed host files")
			}
		})
	}
}

func TestTransitionPassesSpaceSettingsToEveryApp(t *testing.T) {
	// R-QUTX-71LP R-3HE5-F0BG R-3IM1-SS25 R-3JTY-6JSU R-787U-OHML
	type observations struct {
		code            int
		out, diagnostic string
		calls           []string
		files           map[string]string
		modes           map[string]os.FileMode
		commands        []host.Command
	}
	for _, command := range []string{"activate", "rollback"} {
		t.Run(command, func(t *testing.T) {
			run := func(t *testing.T, hosts map[string]string, space apps.SpaceEnv) observations {
				t.Helper()
				f, args, r := transitionSpaceFixture(t, command)
				writeTransitionSpaceManifests(f, r.SHA)
				store := config.Store{Root: f.root}
				writeSpaceTestSettings(t, store, map[string]string{
					"apps.drain_seconds": "13", "apps.stop_seconds": "20", "host.apex": "dummy",
				})
				writeSpaceTestSettings(t, store, hosts)
				configBefore := f.read("etc/ikigenba/config.json")
				var got observations
				f.deps.Cloud = transitionSpaceCloud(&got.calls)
				got.code, got.out, got.diagnostic = f.run(args...)
				if got.code != 0 || got.diagnostic != "" || !strings.Contains(got.out, "secrets: ok (2 keys)\n") || !strings.Contains(got.out, "env: ok (2 apps)\n") {
					t.Fatalf("exit=%d stdout=%q stderr=%q", got.code, got.out, got.diagnostic)
				}
				wantCalls := []string{"open region", "read /sbx.example.test/alpha", "open region", "read /sbx.example.test/dummy"}
				if !reflect.DeepEqual(got.calls, wantCalls) {
					t.Fatalf("cloud calls %v, want %v", got.calls, wantCalls)
				}
				if f.read("etc/ikigenba/config.json") != configBefore {
					t.Fatal("changed configuration")
				}
				got.files = make(map[string]string)
				got.modes = make(map[string]os.FileMode)
				for file, value := range releaseTree(t, f.root) {
					name, err := filepath.Rel(f.root, file)
					if err != nil {
						t.Fatal(err)
					}
					info, err := os.Lstat(file)
					if err != nil {
						t.Fatal(err)
					}
					got.modes[name] = info.Mode()
					got.files[name] = strings.ReplaceAll(value, f.root, "/fixture-root")
				}
				delete(got.files, "etc/ikigenba/config.json")
				for _, command := range f.commands {
					command.Args = slices.Clone(command.Args)
					for i, arg := range command.Args {
						arg = strings.ReplaceAll(arg, f.root, "/fixture-root")
						if strings.HasPrefix(arg, "/fixture-root/run/ikigenba/.services-") {
							arg = "/fixture-root/run/ikigenba/.services-temp"
						}
						command.Args[i] = arg
					}
					command.Dir = strings.ReplaceAll(command.Dir, f.root, "/fixture-root")
					got.commands = append(got.commands, command)
				}
				for _, app := range []string{"alpha", "dummy"} {
					m := apps.Manifest{App: app, Secrets: []string{"TOKEN", "TOKEN"}}
					name := "etc/opt/ikigenba/" + app + "/env"
					want, err := apps.ReleaseEnv(m, map[string]string{"TOKEN": app + "-secret"}, apps.Timeouts{DrainSeconds: 13, StopSeconds: 20}, r, space)
					if err != nil || string(want) != got.files[name] {
						t.Fatalf("%s = %q, want %q (%v)", name, got.files[name], want, err)
					}
					got.files[name] = spaceEnvWithoutHostsLines(got.files[name])
				}
				return got
			}
			want := run(t, nil, apps.SpaceEnv{Region: "region"})
			for _, tc := range []struct {
				name  string
				hosts map[string]string
				space apps.SpaceEnv
			}{
				{"partial", map[string]string{"hosts.domain": "", "hosts.zone_id": " no such zone "}, apps.SpaceEnv{Region: "region", HostsZoneID: " no such zone "}},
				{"all set", map[string]string{"hosts.domain": "UNUSED.INVALID.", "hosts.zone_id": "unused-zone", "hosts.launch_template": `unknown\"template`, "hosts.instance_profile": "\t "}, apps.SpaceEnv{Region: "region", HostsDomain: "UNUSED.INVALID.", HostsZoneID: "unused-zone", HostsLaunchTemplate: `unknown\"template`, HostsInstanceProfile: "\t "}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got := run(t, tc.hosts, tc.space)
					if got.code != want.code || got.out != want.out || got.diagnostic != want.diagnostic || !reflect.DeepEqual(got.calls, want.calls) {
						t.Fatalf("changed command result: exit %d stdout %q stderr %q cloud %v", got.code, got.out, got.diagnostic, got.calls)
					}
					if !reflect.DeepEqual(got.files, want.files) || !reflect.DeepEqual(got.modes, want.modes) {
						t.Fatal("hosts settings changed host files outside provisioner entries")
					}
					if !reflect.DeepEqual(got.commands, want.commands) {
						t.Fatalf("hosts settings changed command trace: got %+v, want %+v", got.commands, want.commands)
					}
				})
			}
		})
	}
}

func writeTransitionSpaceManifests(f *transitionFixture, sha string) {
	f.addRelease(sha, "alpha")
	for _, name := range []string{"alpha", "dummy"} {
		f.write("opt/ikigenba/releases/"+sha+"/"+name+"/etc/manifest.toml", "app='"+name+"'\nsecrets=['TOKEN','TOKEN']\n", 0o644)
	}
}

func transitionSpaceCloud(calls *[]string) cloud.Env {
	return cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
		*calls = append(*calls, "open "+region)
		return initSecretClient{read: func(parameter string) (map[string]string, error) {
			*calls = append(*calls, "read "+parameter)
			return map[string]string{"TOKEN": filepath.Base(parameter) + "-secret"}, nil
		}}, nil
	}}
}

func writeSpaceTestSettings(t *testing.T, store config.Store, values map[string]string) {
	t.Helper()
	for k, v := range values {
		if err := store.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func spaceEnvWithoutHostsLines(env string) string {
	var stripped strings.Builder
	for line := range strings.Lines(env) {
		if !strings.HasPrefix(line, "IKIGENBA_HOSTS_") {
			stripped.WriteString(line)
		}
	}
	return stripped.String()
}
