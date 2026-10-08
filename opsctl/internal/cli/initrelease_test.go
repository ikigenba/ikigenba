package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func TestInitMakesOpsctlLinkAndKeepsExistingEntry(t *testing.T) {
	// R-VA7Z-NRGC R-S99V-8N29
	root := t.TempDir()
	exe := filepath.Join(root, "bin/opsctl")
	if err := os.MkdirAll(filepath.Dir(exe), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	deps := Deps{Root: root, Executable: func() (string, error) { return exe, nil }}
	if err := ensureInitOpsctl(deps); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, strings.TrimPrefix(release.OpsctlLink, "/"))
	target, err := os.Readlink(link)
	if err != nil || target != "/bin/opsctl" {
		t.Fatal(target, err)
	}
	deps.Executable = func() (string, error) { t.Fatal("existing entry queried executable"); return "", nil }
	if err := ensureInitOpsctl(deps); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dangling", link); err != nil {
		t.Fatal(err)
	}
	if err := ensureInitOpsctl(deps); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(link); err != nil || target != "/dangling" {
		t.Fatal(target, err)
	}
}

func TestInitFreshAppsStepDoesNothing(t *testing.T) {
	// R-VF3L-6UF4
	root := t.TempDir()
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("fresh host ran command")
		return host.Result{}, nil
	}}
	if err := setupInitApps(context.Background(), env, config.Store{Root: root}, Deps{}.Cloud, "host.example.test"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestInitReleasedAppsStepRejectsInputsBeforeWriting(t *testing.T) {
	// R-VBFW-1J71 R-VGBH-KM5T
	root := initReleasedRoot(t)
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("invalid setup ran command")
		return host.Result{}, nil
	}}
	before := releaseTree(t, root)
	err := setupInitApps(context.Background(), env, config.Store{Root: root}, Deps{}.Cloud, "host.example.test")
	if err == nil || err.Error() != "aws.region not set" {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, releaseTree(t, root)) {
		t.Fatal("release or host state changed")
	}
}
func initReleasedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	sha := strings.Repeat("a", 40)
	dir := filepath.Join(root, "opt/ikigenba/releases", sha)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(`{"sha":"`+sha+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/"+sha, filepath.Join(root, "opt/ikigenba/current")); err != nil {
		t.Fatal(err)
	}
	return root
}
func releaseTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fs, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[p] = "dir"
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			value, err := os.Readlink(p)
			out[p] = value
			return err
		}
		relative, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		value, err := fs.ReadFile(relative)
		out[p] = string(value)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInitReleasedWritesUnitsAndRestartsChangedActiveApps(t *testing.T) {
	// R-VCNS-FAXQ R-VDVO-T2OF R-VGBH-KM5T
	root := initReleasedRoot(t)
	sha := strings.Repeat("a", 40)
	appDir := filepath.Join(root, "opt/ikigenba/releases", sha, "auth")
	if err := os.MkdirAll(filepath.Join(appDir, "etc"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "etc/manifest.toml"), []byte("app = \"auth\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(appDir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "bin/auth"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	store := config.Store{Root: root}
	if err := store.Set("aws.region", "fixture-region"); err != nil {
		t.Fatal(err)
	}
	var commands []string
	env := host.Env{Root: root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
		line := strings.Join(c.Args, " ")
		commands = append(commands, line)
		if strings.Contains(line, "LoadState") {
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
		}
		if strings.Contains(line, "ActiveState") {
			return host.Result{Stdout: []byte("ActiveState=active\n")}, nil
		}
		return host.Result{}, nil
	}}
	releaseBefore := releaseTree(t, filepath.Join(root, "opt/ikigenba"))
	if err := setupInitApps(context.Background(), env, store, Deps{}.Cloud, "host.example.test"); err != nil {
		t.Fatal(err)
	}
	if len(commands) < 4 || commands[0] != "daemon-reload" || commands[1] != "enable "+apps.ServicesUnit || commands[len(commands)-1] != "restart ikigenba-auth.service" {
		t.Fatal(commands)
	}
	m := apps.Manifest{App: "auth"}
	r := release.Release{SHA: sha}
	timing, err := apps.ReadTimeouts(store)
	if err != nil {
		t.Fatal(err)
	}
	want, err := apps.ReleaseEnv(m, map[string]string{}, timing, r)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	data, err := fs.ReadFile(filepath.Join(strings.TrimPrefix(apps.EnvRoot, "/"), "auth/env"))
	if err != nil || string(data) != string(want) {
		t.Fatal(string(data), string(want), err)
	}
	unit, err := fs.ReadFile(filepath.Join("etc/systemd/system", apps.ServicesUnit))
	if err != nil || !strings.Contains(string(unit), "Type=oneshot") || !strings.Contains(string(unit), "services") {
		t.Fatal(string(unit), err)
	}
	if !reflect.DeepEqual(releaseBefore, releaseTree(t, filepath.Join(root, "opt/ikigenba"))) {
		t.Fatal("release tree changed")
	}
	before := releaseTree(t, root)
	commands = nil
	if err := setupInitApps(context.Background(), env, store, Deps{}.Cloud, "host.example.test"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(commands, []string{"enable " + apps.ServicesUnit}) {
		t.Fatal("unchanged app commands", commands)
	}
	if !reflect.DeepEqual(before, releaseTree(t, root)) {
		t.Fatal("unchanged setup wrote different files")
	}
}

func TestInitReleasedOpsctlLinkUsesCurrent(t *testing.T) {
	// R-VA7Z-NRGC
	root := initReleasedRoot(t)
	deps := Deps{Root: root, Executable: func() (string, error) { t.Fatal("released host queried executable"); return "", nil }}
	if err := ensureInitOpsctl(deps); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(root, strings.TrimPrefix(release.OpsctlLink, "/")))
	if err != nil || target != release.CurrentOpsctl {
		t.Fatal(target, err)
	}
}

func TestInitMissingExecutableStopsLink(t *testing.T) {
	// R-VA7Z-NRGC
	root := t.TempDir()
	deps := Deps{Root: root, Executable: func() (string, error) { return filepath.Join(root, "missing"), nil }}
	if err := ensureInitOpsctl(deps); err == nil || !strings.HasPrefix(err.Error(), release.OpsctlLink+": ") {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, strings.TrimPrefix(release.OpsctlLink, "/"))); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func addInitReleaseApp(t *testing.T, root, name, manifest string) {
	t.Helper()
	dir := filepath.Join(root, "opt/ikigenba/releases", strings.Repeat("a", 40), name)
	for _, sub := range []string{"etc", "bin"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{"etc/manifest.toml": manifest, "bin/" + name: "fixture"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitReleasedPreparesAllAppEnvironmentsBeforeWriting(t *testing.T) {
	// R-VBFW-1J71
	root := initReleasedRoot(t)
	addInitReleaseApp(t, root, "auth", "app = \"auth\"\n")
	addInitReleaseApp(t, root, "notes", "app = \"notes\"\nsecrets = [\"TOKEN\"]\n")
	store := config.Store{Root: root}
	if err := store.Set("aws.region", "region"); err != nil {
		t.Fatal(err)
	}
	before := releaseTree(t, root)
	remote := cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
		if region != "region" {
			t.Fatal(region)
		}
		return initSecretClient{read: func(parameter string) (map[string]string, error) {
			if parameter != "/host.example.test/notes" {
				t.Fatal(parameter)
			}
			if !reflect.DeepEqual(before, releaseTree(t, root)) {
				t.Fatal("first app was written before final secrets read")
			}
			return nil, cloud.ErrNotFound
		}}, nil
	}}
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("secrets failure executed command")
		return host.Result{}, nil
	}}
	err := setupInitApps(context.Background(), env, store, remote, "host.example.test")
	if err == nil || err.Error() != "notes: no value for 'TOKEN' in /host.example.test/notes" {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, releaseTree(t, root)) {
		t.Fatal("failed secret preparation changed files")
	}
}

type initSecretClient struct {
	read func(string) (map[string]string, error)
}

func (c initSecretClient) ReadSecrets(_ context.Context, p string) (map[string]string, error) {
	return c.read(p)
}
func (initSecretClient) GetObject(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("unexpected object read")
}
func (initSecretClient) PutObject(context.Context, string, io.Reader) error {
	return errors.New("unexpected object write")
}
func (initSecretClient) ListObjects(context.Context, string) ([]cloud.Object, error) {
	return nil, errors.New("unexpected object list")
}

func TestInitReleasedDisabledAndInactiveAppsAreNotRestarted(t *testing.T) {
	// R-VDVO-T2OF
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			root := initReleasedRoot(t)
			addInitReleaseApp(t, root, "auth", "app = \"auth\"\n")
			store := config.Store{Root: root}
			if err := store.Set("aws.region", "region"); err != nil {
				t.Fatal(err)
			}
			var commands []string
			env := host.Env{Root: root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
				line := strings.Join(c.Args, " ")
				commands = append(commands, line)
				if strings.Contains(line, "LoadState") {
					state := "enabled"
					if disabled {
						state = "disabled"
					}
					return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\n")}, nil
				}
				return host.Result{Stdout: []byte("ActiveState=inactive\n")}, nil
			}}
			if err := setupInitApps(context.Background(), env, store, Deps{}.Cloud, "host.example.test"); err != nil {
				t.Fatal(err)
			}
			for _, line := range commands {
				if strings.Contains(line, "restart") || strings.Contains(line, "start ") || strings.Contains(line, "enable ikigenba-auth") {
					t.Fatal(commands)
				}
				if disabled && strings.Contains(line, "ActiveState") {
					t.Fatal("disabled socket queried active state", commands)
				}
			}
		})
	}
}

func TestInitReleasedUsesCurrentLabelSecretsTimingAndExactUnits(t *testing.T) {
	// R-VBFW-1J71 R-VCNS-FAXQ
	root := initReleasedRoot(t)
	sha := strings.Repeat("a", 40)
	label := "candidate/test"
	if err := os.WriteFile(filepath.Join(root, "opt/ikigenba/releases", sha, "label"), []byte(label+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := "app='auth'\nsecrets=['TOKEN']\n[env]\nFEATURE='selected'\n[resources]\nslice='core'\nmemory_max='256M'\ngo_memory_limit='192M'\ncpu_weight=321\n"
	addInitReleaseApp(t, root, "auth", source)
	dataDir := filepath.Join(root, strings.TrimPrefix(apps.DataRoot, "/"), "kept/state")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "kept.db"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeData := releaseTree(t, dataDir)
	store := config.Store{Root: root}
	for k, v := range map[string]string{"aws.region": "configured-region", "apps.drain_seconds": "13", "apps.stop_seconds": "29"} {
		if err := store.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	var calls []string
	remote := cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
		calls = append(calls, region)
		return initSecretClient{read: func(p string) (map[string]string, error) {
			calls = append(calls, p)
			return map[string]string{"TOKEN": "fetched-value", "UNREQUESTED": "ignored"}, nil
		}}, nil
	}}
	env := host.Env{Root: root, Execute: func(_ context.Context, _ host.Command) (host.Result, error) {
		return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
	}}
	if err := setupInitApps(context.Background(), env, store, remote, "normalized.example.test"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"configured-region", "/normalized.example.test/auth"}) {
		t.Fatal(calls)
	}
	m, err := apps.ParseManifest([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	timing := apps.Timeouts{DrainSeconds: 13, StopSeconds: 29}
	r := release.Release{SHA: sha, Label: label}
	want, err := apps.ReleaseEnv(m, map[string]string{"TOKEN": "fetched-value"}, timing, r)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := readReleaseFile(root, filepath.Join(strings.TrimPrefix(apps.EnvRoot, "/"), "auth/env"))
	if err != nil || string(actual) != string(want) {
		t.Fatal(string(actual), string(want), err)
	}
	expectedRoot := t.TempDir()
	expectedEnv := host.Env{Root: expectedRoot}
	if err := apps.WriteReleaseUnits(expectedEnv, "auth", m, timing); err != nil {
		t.Fatal(err)
	}
	if err := apps.WriteServicesUnit(expectedEnv); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{"ikigenba-auth.socket", "ikigenba-auth.service", apps.ServicesUnit} {
		rel := filepath.Join("etc/systemd/system", unit)
		expected, err := readReleaseFile(expectedRoot, rel)
		if err != nil {
			t.Fatal(err)
		}
		expected = []byte(strings.ReplaceAll(string(expected), expectedRoot, root))
		actual, err := readReleaseFile(root, rel)
		if err != nil || string(actual) != string(expected) {
			t.Fatalf("%s bytes = %q, want %q (%v)", unit, actual, expected, err)
		}
	}
	if !reflect.DeepEqual(beforeData, releaseTree(t, dataDir)) {
		t.Fatal("data-only state changed")
	}
	for _, rel := range []string{"opt/auth", "opt/kept", "etc/opt/ikigenba/kept", "etc/systemd/system/ikigenba-kept.service", "etc/systemd/system/ikigenba-kept.socket"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("unexpected data/per-app path %s: %v", rel, err)
		}
	}
}

func TestInitReleasedCommandsRunInOrderAndStopAtEachFailure(t *testing.T) {
	// R-VDVO-T2OF
	want := []string{"daemon-reload", "enable " + apps.ServicesUnit, "show --property=LoadState --property=UnitFileState ikigenba-auth.socket", "show --property=ActiveState ikigenba-auth.socket", "restart ikigenba-auth.service", "show --property=LoadState --property=UnitFileState ikigenba-notes.socket", "show --property=ActiveState ikigenba-notes.socket", "restart ikigenba-notes.service"}
	for failAt := -1; failAt < len(want); failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			root := initReleasedRoot(t)
			for _, app := range []string{"notes", "auth"} {
				addInitReleaseApp(t, root, app, "app='"+app+"'\n")
			}
			store := config.Store{Root: root}
			if err := store.Set("aws.region", "region"); err != nil {
				t.Fatal(err)
			}
			var commands []string
			env := host.Env{Root: root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
				if c.Name != "systemctl" {
					t.Fatal(c.Name)
				}
				line := strings.Join(c.Args, " ")
				commands = append(commands, line)
				if len(commands)-1 == failAt {
					return host.Result{ExitCode: 7}, nil
				}
				if strings.Contains(line, "LoadState") {
					return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
				}
				return host.Result{Stdout: []byte("ActiveState=active\n")}, nil
			}}
			err := setupInitApps(context.Background(), env, store, Deps{}.Cloud, "host.example.test")
			expected := want
			if failAt >= 0 {
				expected = want[:failAt+1]
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) || commandErr.Result.ExitCode != 7 {
					t.Fatalf("failure = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(commands, expected) {
				t.Fatalf("commands=%q want=%q", commands, expected)
			}
		})
	}
}

func readReleaseFile(root, name string) ([]byte, error) {
	fs, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fs.Close() }()
	return fs.ReadFile(name)
}
