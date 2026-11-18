package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInitHostsSettingsChangeOnlyEnvironmentAndRequiredRestarts(t *testing.T) {
	// R-8B7E-V6D8
	f := prepareInitSpaceFixture(t)
	baseline := f.run("")
	if baseline.code != 0 || baseline.diagnostic != "" {
		t.Fatalf("baseline exit %d stdout %q stderr %q", baseline.code, baseline.out, baseline.diagnostic)
	}
	before := treeState(t, f.deps.Root)
	for key, value := range map[string]string{
		"hosts.domain": "UNUSED.INVALID.", "hosts.zone_id": "", "hosts.launch_template": ` unresolved\" template `,
	} {
		f.set(key, value)
	}
	wantLines := "IKIGENBA_HOSTS_DOMAIN=UNUSED.INVALID.\nIKIGENBA_HOSTS_LAUNCH_TEMPLATE= unresolved\\\" template \n"
	afterSet := f.run("")
	assertInitSpaceEquivalent(t, baseline, afterSet, true)
	after := treeState(t, f.deps.Root)
	beforeConfig := before["etc/ikigenba/config.json"]
	configured := after["etc/ikigenba/config.json"]
	after["etc/ikigenba/config.json"] = beforeConfig
	for _, app := range []string{"alpha", "notes"} {
		path := "etc/opt/ikigenba/" + app + "/env"
		entry := after[path]
		if entry.Content != before[path].Content+wantLines {
			t.Fatalf("%s environment = %q, want %q", app, entry.Content, before[path].Content+wantLines)
		}
		entry.Content = strings.TrimSuffix(entry.Content, wantLines)
		after[path] = entry
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("setting hosts keys changed unrelated host files")
	}
	after["etc/ikigenba/config.json"] = configured
	for _, app := range []string{"alpha", "notes"} {
		path := "etc/opt/ikigenba/" + app + "/env"
		entry := after[path]
		entry.Content += wantLines
		after[path] = entry
	}
	assertInitSpaceEquivalent(t, baseline, f.run(""), false)
	if !reflect.DeepEqual(after, treeState(t, f.deps.Root)) {
		t.Fatal("unchanged settings rewrote host file contents")
	}
	for _, key := range []string{"hosts.domain", "hosts.zone_id", "hosts.launch_template"} {
		if err := f.store.Del(key); err != nil {
			t.Fatal(err)
		}
	}
	assertInitSpaceEquivalent(t, baseline, f.run(""), true)
	if !reflect.DeepEqual(before, treeState(t, f.deps.Root)) {
		t.Fatal("deleting hosts keys did not recover the baseline host state")
	}
}

func TestInitHostsSettingsCommandFailuresStopAfterPublishing(t *testing.T) {
	// R-8B7E-V6D8 R-VDVO-T2OF R-LXGW-8J1R
	const query = "systemctl show --property=ActiveState ikigenba-alpha.socket"
	const restart = "systemctl restart ikigenba-alpha.service"
	for _, command := range []string{query, restart} {
		t.Run(command, func(t *testing.T) {
			f := prepareInitSpaceFixture(t)
			baseline := f.run("")
			if baseline.code != 0 {
				t.Fatalf("baseline failed: %+v", baseline)
			}
			f.set("hosts.instance_profile", "unknown profile")
			got := f.run(command)
			wantErr := "opsctl: " + command + ": exit status 7\n\n> fixture command failure\n"
			if got.code != 1 || got.out != baseline.out || got.diagnostic != wantErr {
				t.Fatalf("exit %d stdout %q stderr %q, want 1 and %q", got.code, got.out, got.diagnostic, wantErr)
			}
			alpha := lastInitCommand(baseline.commands, "systemctl show --property=LoadState --property=UnitFileState ikigenba-alpha.socket")
			if alpha == -1 {
				t.Fatal("baseline did not examine alpha enablement")
			}
			want := append(slices.Clone(baseline.commands[:alpha+1]), query)
			if command == restart {
				want = append(want, restart)
			}
			if !reflect.DeepEqual(got.commands, want) {
				t.Fatalf("command trace = %v, want %v", got.commands, want)
			}
			written := treeState(t, f.deps.Root)
			for _, app := range []string{"alpha", "notes"} {
				data := written[filepath.Join("etc/opt/ikigenba", app, "env")].Content
				if !strings.HasSuffix(data, "IKIGENBA_HOSTS_INSTANCE_PROFILE=unknown profile\n") {
					t.Fatalf("%s environment not published: %q", app, data)
				}
			}
			assertInitSpaceEquivalent(t, baseline, f.run(""), false)
		})
	}
}

func TestInitInvalidStoredHostsValueFailsBeforePublishingApps(t *testing.T) {
	// R-7VZ3-U540
	f := prepareInitSpaceFixture(t)
	baseline := f.run("")
	if baseline.code != 0 {
		t.Fatalf("baseline failed: %+v", baseline)
	}
	f.set("hosts.domain", "invalid\x00domain")
	before := treeState(t, f.deps.Root)
	got := f.run("")
	wantErr := "opsctl: alpha: IKIGENBA_HOSTS_DOMAIN holds a NUL, CR or LF\n"
	if got.code != 1 || got.out != baseline.out || got.diagnostic != wantErr {
		t.Fatalf("exit %d stdout %q stderr %q, want 1 and %q", got.code, got.out, got.diagnostic, wantErr)
	}
	for _, command := range got.commands {
		if strings.HasPrefix(command, "systemctl show --property=ActiveState ") || command == "systemctl restart ikigenba-alpha.service" || command == "systemctl restart ikigenba-notes.service" || command == "systemctl enable ikigenba-services.service" {
			t.Fatalf("app systemd command after configuration failure: %q", command)
		}
	}
	if !reflect.DeepEqual(before, treeState(t, f.deps.Root)) {
		t.Fatal("invalid hosts value changed host files")
	}
}

type initSpaceFixture struct {
	t     *testing.T
	deps  cli.Deps
	store config.Store
}

func prepareInitSpaceFixture(t *testing.T) initSpaceFixture {
	t.Helper()
	deps := readyStateGuardDeps(t)
	writeCLIInstallFile(t, filepath.Join(deps.Root, "etc/letsencrypt/live/api.example.com/fullchain.pem"), "not-due-certificate\n")
	sha := strings.Repeat("a", 40)
	path := filepath.Join(deps.Root, "opt/ikigenba/releases", sha)
	writeCLIInstallFile(t, filepath.Join(path, "release.json"), `{"sha":"`+sha+`"}`)
	for _, app := range []string{"alpha", "notes"} {
		writeCLIInstallFile(t, filepath.Join(path, app, "etc/manifest.toml"), "app='"+app+"'\nsecrets=['TOKEN']\n")
		writeCLIInstallFile(t, filepath.Join(path, app, "bin", app), "binary")
	}
	if err := os.Symlink("releases/"+sha, filepath.Join(deps.Root, "opt/ikigenba/current")); err != nil {
		t.Fatal(err)
	}
	f := initSpaceFixture{t: t, deps: deps, store: config.Store{Root: deps.Root}}
	f.set("aws.region", "configured-region")
	f.set("backup.s3_uri", "s3://fixture/backups/")
	if got := f.run(""); got.code != 0 {
		t.Fatalf("initial init failed: %+v", got)
	}
	return f
}

func (f initSpaceFixture) set(key, value string) {
	f.t.Helper()
	if err := f.store.Set(key, value); err != nil {
		f.t.Fatal(err)
	}
}

type initSpaceObservations struct {
	out, diagnostic string
	code            int
	commands, calls []string
}

func (f initSpaceFixture) run(failure string) initSpaceObservations {
	var got initSpaceObservations
	f.deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		line := command.Name + " " + strings.Join(command.Args, " ")
		got.commands = append(got.commands, line)
		if line == failure {
			return host.Result{ExitCode: 7, Stderr: []byte("fixture command failure\n")}, nil
		}
		if result, ok := servicesFixtureCommand(command); ok {
			return result, nil
		}
		if strings.HasPrefix(line, "systemctl show --property=LoadState --property=UnitFileState ") {
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
		}
		if strings.HasPrefix(line, "systemctl show --property=ActiveState ") {
			return host.Result{Stdout: []byte("ActiveState=active\n")}, nil
		}
		return host.Result{}, nil
	}
	f.deps.Cloud.Open = func(_ context.Context, region string) (cloud.Client, error) {
		got.calls = append(got.calls, "open "+region)
		return initSpaceCloud{calls: &got.calls}, nil
	}
	got.out, got.diagnostic, got.code = invoke([]string{"init"}, f.deps)
	return got
}

type initSpaceCloud struct {
	cloud.Client
	calls *[]string
}

func (c initSpaceCloud) ReadSecrets(_ context.Context, parameter string) (map[string]string, error) {
	*c.calls = append(*c.calls, "read "+parameter)
	return map[string]string{"TOKEN": "fixture-value"}, nil
}

func assertInitSpaceEquivalent(t *testing.T, baseline, got initSpaceObservations, changed bool) {
	t.Helper()
	if baseline.code != got.code || baseline.out != got.out || baseline.diagnostic != got.diagnostic || !reflect.DeepEqual(baseline.calls, got.calls) {
		t.Fatalf("baseline %+v, got %+v", baseline, got)
	}
	var wantCommands []string
	for i, command := range baseline.commands {
		wantCommands = append(wantCommands, command)
		if changed {
			for _, app := range []string{"alpha", "notes"} {
				inspection := "systemctl show --property=LoadState --property=UnitFileState ikigenba-" + app + ".socket"
				if command == inspection && i == lastInitCommand(baseline.commands, inspection) {
					wantCommands = append(wantCommands, "systemctl show --property=ActiveState ikigenba-"+app+".socket", "systemctl restart ikigenba-"+app+".service")
				}
			}
		}
	}
	if !reflect.DeepEqual(wantCommands, got.commands) {
		t.Fatalf("commands = %v, want %v", got.commands, wantCommands)
	}
}

func lastInitCommand(commands []string, command string) int {
	for i := len(commands) - 1; i >= 0; i-- {
		if commands[i] == command {
			return i
		}
	}
	return -1
}
