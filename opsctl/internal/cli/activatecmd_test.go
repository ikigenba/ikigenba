package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

var transitionSHA = strings.Repeat("a", 40)
var transitionOld = strings.Repeat("b", 40)
var transitionUnused = strings.Repeat("c", 40)

type transitionFixture struct {
	t            *testing.T
	root         string
	deps         Deps
	commands     []host.Command
	disabled     map[string]bool
	active       map[string]bool
	fail         string
	failJournal  bool
	secretsCalls int
	cloud        *transitionCloud
}

func newTransitionFixture(t *testing.T) *transitionFixture {
	t.Helper()
	f := &transitionFixture{t: t, root: t.TempDir(), disabled: map[string]bool{}, active: map[string]bool{}}
	f.cloud = &transitionCloud{f: f}
	f.deps = Deps{Root: f.root, EUID: 0, Getenv: func(string) string { return "" }, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }, Execute: f.execute, Executable: func() (string, error) {
		return filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA, "opsctl/bin/opsctl"), nil
	}, Exec: func(string, []string, []string) error { t.Fatal("unexpected exec"); return nil }, Cloud: cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
		if region != "region" {
			t.Fatalf("region %q", region)
		}
		return f.cloud, nil
	}}}
	f.addRelease(transitionSHA, "dummy")
	f.write("etc/nginx/conf.d/keep", "keep", 0o644)
	for _, unit := range []string{"ikigenba.slice", "ikigenba-core.slice", "ikigenba-apps.slice"} {
		f.write("etc/systemd/system/"+unit, "MemoryMax=4096M\n", 0o644)
	}
	f.write("etc/letsencrypt/live/sbx.example.test/fullchain.pem", "cert", 0o644)
	store := config.Store{Root: f.root}
	for _, kv := range [][2]string{{"aws.region", "region"}, {"host.name", "SBX.Example.Test."}, {"backup.s3_uri", "s3://bucket/host"}} {
		if err := store.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func (f *transitionFixture) write(p, data string, mode os.FileMode) {
	f.t.Helper()
	name := filepath.Join(f.root, p)
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), mode); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		f.t.Fatal(err)
	}
}
func (f *transitionFixture) addRelease(sha string, apps ...string) {
	f.write("opt/ikigenba/releases/"+sha+"/opsctl/bin/opsctl", "binary", 0o755)
	f.write("opt/ikigenba/releases/"+sha+"/release.json", `{"sha":"`+sha+`"}`, 0o644)
	for _, app := range apps {
		base := "opt/ikigenba/releases/" + sha + "/" + app
		f.write(base+"/bin/"+app, "binary", 0o777)
		f.write(base+"/etc/manifest.toml", "app='"+app+"'\n", 0o666)
	}
}
func (f *transitionFixture) link(name, sha string) {
	f.t.Helper()
	if err := os.Symlink("releases/"+sha, filepath.Join(f.root, "opt/ikigenba", name)); err != nil {
		f.t.Fatal(err)
	}
}
func (f *transitionFixture) execute(_ context.Context, c host.Command) (host.Result, error) {
	f.commands = append(f.commands, c)
	joined := c.Name + " " + strings.Join(c.Args, " ")
	if joined == f.fail {
		return host.Result{ExitCode: 1, Stdout: []byte("command output\n"), Stderr: []byte("error output\n")}, nil
	}
	switch c.Name {
	case "id":
		if reflect.DeepEqual(c.Args, []string{"--user", "ikigenba"}) {
			return host.Result{Stdout: []byte("1001\n")}, nil
		}
		return host.Result{Stdout: []byte("ikigenba\n")}, nil
	case "systemctl":
		if c.Args[0] == "show" {
			unit := c.Args[len(c.Args)-1]
			name := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(unit, "ikigenba-"), ".service"), ".socket")
			if strings.Contains(strings.Join(c.Args, " "), "UnitFileState") {
				s := "enabled"
				if f.disabled[name] {
					s = "disabled"
				}
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + s + "\n")}, nil
			}
			state := "inactive"
			if f.active[name] {
				state = "active"
			}
			return host.Result{Stdout: []byte("ActiveState=" + state + "\n")}, nil
		}
		if c.Args[0] == "start" || c.Args[0] == "restart" {
			name := strings.TrimSuffix(strings.TrimPrefix(c.Args[1], "ikigenba-"), ".service")
			f.active[name] = true
		}
	case "journalctl":
		if f.failJournal {
			return host.Result{ExitCode: 1}, nil
		}
		return host.Result{Stdout: []byte("journal first\njournal second\n")}, nil
	case "getent":
		return host.Result{Stdout: []byte("owner:x:" + c.Args[1] + ":1001::/tmp:/bin/false\n")}, nil
	case "zstd":
		if !reflect.DeepEqual(c.Args, []string{"--quiet", "--stdout"}) || c.Stdin == nil {
			f.t.Fatalf("invalid snapshot compressor command %+v", c)
		}
		data, err := io.ReadAll(c.Stdin)
		if err != nil {
			f.t.Fatal(err)
		}
		reader := tar.NewReader(bytes.NewReader(data))
		for {
			_, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				f.t.Fatalf("snapshot compressor input: %v", err)
			}
		}
		// A reversible process-fixture frame keeps the archive observable.
		return host.Result{Stdout: append([]byte{0x28, 0xb5, 0x2f, 0xfd}, data...)}, nil
	}
	return host.Result{}, nil
}
func (f *transitionFixture) run(args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := Run(args, nil, &out, &err, f.deps)
	return code, out.String(), err.String()
}
func (f *transitionFixture) read(p string) string {
	f.t.Helper()
	fs, err := os.OpenRoot(f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	data, err := fs.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}
func (f *transitionFixture) missing(p string) {
	f.t.Helper()
	if _, err := os.Lstat(filepath.Join(f.root, p)); !errors.Is(err, os.ErrNotExist) {
		f.t.Fatalf("%s exists: %v", p, err)
	}
}

type transitionCloud struct {
	f         *transitionFixture
	objects   map[string][]byte
	failNames map[string]bool
	putCalls  []string
}

func (c *transitionCloud) GetObject(context.Context, string) (io.ReadCloser, error) {
	return nil, cloud.ErrNotFound
}
func (c *transitionCloud) PutObject(_ context.Context, key string, r io.Reader) error {
	data, e := io.ReadAll(r)
	c.putCalls = append(c.putCalls, key)
	for name := range c.failNames {
		if strings.Contains(key, "/snapshots/"+name+"/") {
			return fmt.Errorf("upload %s failed", name)
		}
	}
	if c.objects == nil {
		c.objects = map[string][]byte{}
	}
	if _, exists := c.objects[key]; exists {
		return cloud.ErrAlreadyExists
	}
	c.objects[key] = data
	return e
}
func (c *transitionCloud) ListObjects(context.Context, string) ([]cloud.Object, error) {
	return nil, nil
}
func (c *transitionCloud) ReadSecrets(_ context.Context, name string) (map[string]string, error) {
	c.f.secretsCalls++
	if name != "/sbx.example.test/dummy" {
		return nil, fmt.Errorf("unexpected secrets parameter %s", name)
	}
	return map[string]string{"TOKEN": "secret", "OTHER": "second"}, nil
}

func TestActivateGrammarAndIdentity(t *testing.T) {
	// R-AQIB-Q6SC R-SBPO-06JN R-SCXK-DYAC
	for _, tc := range []struct {
		args []string
		want string
	}{{nil, "activate needs SHA"}, {[]string{transitionSHA, "label", "third"}, "activate takes SHA and an optional LABEL"}, {[]string{"bad"}, "activate takes a full commit sha, not 'bad'"}, {[]string{transitionSHA, "bad label"}, "label 'bad label' may hold only letters, digits, '.', '_', '/' and '-'"}} {
		f := newTransitionFixture(t)
		code, out, err := f.run(append([]string{"activate"}, tc.args...)...)
		if code != 2 || out != "" || err != "opsctl: "+tc.want+"\n\nsee 'opsctl activate --help' for usage\n" || len(f.commands) != 0 {
			t.Fatalf("%d %q %q", code, out, err)
		}
	}
	for _, fault := range []string{"wrong executable", "missing binary", "executable error", "missing metadata", "wrong metadata"} {
		t.Run(fault, func(t *testing.T) {
			f := newTransitionFixture(t)
			expected := "opsctl: activate " + transitionSHA + " must be run by /opt/ikigenba/releases/" + transitionSHA + "/opsctl/bin/opsctl\n"
			switch fault {
			case "wrong executable":
				f.deps.Executable = func() (string, error) { return filepath.Join(f.root, "elsewhere"), nil }
			case "missing binary":
				if e := os.Remove(filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA, "opsctl/bin/opsctl")); e != nil {
					t.Fatal(e)
				}
			case "executable error":
				f.deps.Executable = func() (string, error) { return "", errors.New("exe failed") }
			case "missing metadata":
				if e := os.Remove(filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA, "release.json")); e != nil {
					t.Fatal(e)
				}
				expected = "opsctl: /opt/ikigenba/releases/" + transitionSHA + "/release.json is missing; unpack the release again\n"
			case "wrong metadata":
				f.write("opt/ikigenba/releases/"+transitionSHA+"/release.json", `{"sha":"other"}`, 0o644)
				expected = "opsctl: /opt/ikigenba/releases/" + transitionSHA + "/release.json names other; unpack the release again\n"
			}
			code, out, err := f.run("activate", transitionSHA)
			if code != 1 || out != "" || err != expected || len(f.commands) != 0 {
				t.Fatalf("%d %q %q", code, out, err)
			}
		})
	}
}
func TestActivateConfigurationInert(t *testing.T) {
	// R-B1HF-64GL
	for _, tc := range []struct{ key, val, want string }{{"aws.region", "", "aws.region not set"}, {"host.name", "", "host.name not set"}, {"apps.stop_seconds", "2", "apps.stop_seconds (2) is not greater than apps.drain_seconds (5)"}, {"host.apex", "dummy", "host.apex is set but host.name 'example' has no parent domain"}} {
		f := newTransitionFixture(t)
		store := config.Store{Root: f.root}
		if e := store.Set(tc.key, tc.val); e != nil {
			t.Fatal(e)
		}
		if tc.key == "host.apex" {
			if e := store.Set("host.name", "example"); e != nil {
				t.Fatal(e)
			}
		}
		code, out, err := f.run("activate", transitionSHA)
		if code != 1 || out != "" || err != "opsctl: "+tc.want+"\n" || len(f.commands) != 0 {
			t.Fatalf("%d %q %q", code, out, err)
		}
	}
}
func TestActivateSuccessfulReleaseAndReactivation(t *testing.T) {
	// R-SLGV-2CH7 R-SJ12-ASZT R-B3X7-XNXZ R-BDOE-ZTVJ R-BIK0-IWUB R-BKZT-AGBP R-SU05-QQO2 R-SSS9-CYXD R-BR3B-7B16 R-BSB7-L2RV R-BVYW-QDZY R-BYEP-HXHC
	f := newTransitionFixture(t)
	f.addRelease(transitionOld, "dropped")
	f.link("current", transitionOld)
	f.addRelease(transitionUnused, "unused")
	f.write("opt/ikigenba/releases/.unpack.abc/keep", "keep", 0o644)
	f.write("opt/ikigenba/releases/other/keep", "keep", 0o644)
	f.write("etc/systemd/system/ikigenba-dropped.service", "old service", 0o644)
	f.write("etc/systemd/system/ikigenba-dropped.socket", "old socket", 0o644)
	f.write("etc/opt/ikigenba/dropped/env", "old env", 0o640)
	f.write("var/opt/ikigenba/dropped/state/keep", "state", 0o600)
	f.write("var/opt/ikigenba/dummy/state/keep", "dummy state", 0o600)
	f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='dummy'\nsecrets=['TOKEN','OTHER','TOKEN']\n", 0o666)
	configBefore := f.read("etc/ikigenba/config.json")
	code, out, err := f.run("activate", transitionSHA, "rc/test")
	if code != 0 || err != "" {
		t.Fatalf("%d\n%s\n%s", code, out, err)
	}
	expectedSteps := []string{"release", "layout", "manifests", "secrets", "resources", "label", "env", "units", "links", "systemd", "nginx", "services", "litestream", "service", "retention"}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(expectedSteps) {
		t.Fatalf("output %s", out)
	}
	for i, step := range expectedSteps {
		if !strings.HasPrefix(lines[i], step+": ok (") {
			t.Fatalf("step %d %q", i, lines[i])
		}
	}
	for _, want := range []string{"release: ok (aaaaaaa, rc/test)", "secrets: ok (2 keys)", "units: ok (1 app; dropped stopped and removed, state kept)", "services: ok (1 service)", "service: ok (dummy aaaaaaa active)", "retention: ok (removed 1 release)"} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if f.secretsCalls != 1 {
		t.Fatalf("secret calls %d", f.secretsCalls)
	}
	if f.read("opt/ikigenba/releases/"+transitionSHA+"/label") != "rc/test\n" {
		t.Fatal("label")
	}
	env := f.read("etc/opt/ikigenba/dummy/env")
	for _, want := range []string{"TOKEN=\"secret\"\n", "IKIGENBA_RELEASE=rc/test\n"} {
		if !strings.Contains(env, want) {
			t.Fatalf("env %s lacks %s", env, want)
		}
	}
	if f.read("etc/ikigenba/config.json") != configBefore || f.read("var/opt/ikigenba/dropped/state/keep") != "state" || f.read("var/opt/ikigenba/dummy/state/keep") != "dummy state" {
		t.Fatal("state/config changed")
	}
	for _, p := range []string{"opt/ikigenba/releases/" + transitionUnused, "etc/opt/ikigenba/dropped", "etc/systemd/system/ikigenba-dropped.service", "etc/systemd/system/ikigenba-dropped.socket"} {
		f.missing(p)
	}
	if f.read("opt/ikigenba/releases/.unpack.abc/keep") != "keep" || f.read("opt/ikigenba/releases/other/keep") != "keep" {
		t.Fatal("retention extras")
	}
	for _, c := range f.commands {
		if len(c.Args) > 0 && (c.Args[0] == "restart" || c.Args[0] == "start") && strings.HasSuffix(c.Args[len(c.Args)-1], ".socket") {
			t.Fatalf("socket restarted %+v", c)
		}
	}
	p, _ := os.Readlink(filepath.Join(f.root, "opt/ikigenba/previous"))
	if p != "releases/"+transitionOld {
		t.Fatal(p)
	}
	f.commands = nil
	code, out, err = f.run("activate", transitionSHA)
	if code != 0 || err != "" || !strings.Contains(out, "previous bbbbbbb kept") {
		t.Fatalf("reactivate %d %s %s", code, out, err)
	}
	f.missing("opt/ikigenba/releases/" + transitionSHA + "/label")
	if !strings.Contains(out, "service: ok (dummy aaaaaaa active)") {
		t.Fatal(out)
	}
}

func TestActivatePrewriteStepsLeaveHostUntouched(t *testing.T) {
	// R-SK8Y-OKQI R-BB8M-8AE5 R-BCGI-M24U R-SQCG-LFFZ R-BX6T-45QN
	for _, tc := range []struct {
		name, step, reason string
		setup              func(*transitionFixture)
	}{
		{"tree order", "release", "dummy: sbin is not allowed; an app holds only bin, libexec, lib, share and etc", func(f *transitionFixture) {
			f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/sbin/other", "x", 0o644)
		}},
		{"bin link", "release", "dummy: bin/dummy is missing", func(f *transitionFixture) {
			p := filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA, "dummy/bin/dummy")
			if e := os.Remove(p); e != nil {
				f.t.Fatal(e)
			}
			if e := os.Symlink("../etc/manifest.toml", p); e != nil {
				f.t.Fatal(e)
			}
		}},
		{"extra bin", "release", "dummy: bin holds more than dummy", func(f *transitionFixture) {
			f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/bin/other", "x", 0o644)
		}},
		{"uninitialised", "layout", "host is not initialised", func(f *transitionFixture) {
			if e := os.Remove(filepath.Join(f.root, "etc/systemd/system/ikigenba.slice")); e != nil {
				f.t.Fatal(e)
			}
		}},
		{"no manifest", "manifests", "dummy: no etc/manifest.toml", func(f *transitionFixture) {
			if e := os.Remove(filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA, "dummy/etc/manifest.toml")); e != nil {
				f.t.Fatal(e)
			}
		}},
		{"wrong manifest", "manifests", "dummy: etc/manifest.toml: app must be 'dummy'", func(f *transitionFixture) {
			f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='other'", 0o644)
		}},
		{"reserved app", "manifests", "dummy: 'services' is not a usable app name", func(f *transitionFixture) {
			f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='services'\n", 0o644)
		}},
		{"resources", "resources", "dummy: etc/manifest.toml: memory_max 8192M is more than ikigenba-apps.slice's MemoryMax 4096M", func(f *transitionFixture) {
			f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='dummy'\n[resources]\nmemory_max='8G'", 0o644)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTransitionFixture(t)
			f.write("etc/opt/ikigenba/dummy/env", "old env", 0o640)
			f.write("etc/systemd/system/ikigenba-dummy.service", "old unit", 0o644)
			f.write("opt/ikigenba/releases/"+transitionSHA+"/label", "old label\n", 0o644)
			tc.setup(f)
			code, out, err := f.run("activate", transitionSHA, "new-label")
			if code != 1 || err != "opsctl: activate failed\n" || !strings.Contains(out, tc.step+": failed: "+tc.reason) {
				t.Fatalf("%d %s %s", code, out, err)
			}
			if f.read("etc/opt/ikigenba/dummy/env") != "old env" || f.read("etc/systemd/system/ikigenba-dummy.service") != "old unit" || f.read("opt/ikigenba/releases/"+transitionSHA+"/label") != "old label\n" {
				t.Fatal("prewrite mutation")
			}
			f.missing("opt/ikigenba/current")
			for _, c := range f.commands {
				if c.Name != "chown" && (c.Name != "systemctl" || c.Args[0] != "show") {
					t.Fatalf("unexpected prewrite command %+v", c)
				}
			}
		})
	}
}
func TestActivateCoreFirstDisabledAndServiceFailure(t *testing.T) {
	// R-SNWN-TVYL R-SP4K-7NPA R-SJ12-ASZT R-BX6T-45QN
	for _, journalFails := range []bool{false, true} {
		t.Run(fmt.Sprint(journalFails), func(t *testing.T) {
			f := newTransitionFixture(t)
			f.addRelease(transitionSHA, "alpha", "zcore")
			f.write("opt/ikigenba/releases/"+transitionSHA+"/zcore/etc/manifest.toml", "app='zcore'\n[resources]\nslice='core'", 0o644)
			f.addRelease(transitionOld, "dummy")
			f.link("current", transitionOld)
			f.disabled["dummy"] = true
			f.active["zcore"] = true
			f.fail = "systemctl restart ikigenba-zcore.service"
			f.failJournal = journalFails
			code, out, err := f.run("activate", transitionSHA)
			if code != 1 || !strings.Contains(out, "service: failed: zcore: service failed to start\n") || strings.Contains(out, "retention:") {
				t.Fatalf("%d %s %s", code, out, err)
			}
			want := "opsctl: activate failed\n\n> journal first\n> journal second\n"
			if journalFails {
				want = "opsctl: activate failed\n\ncould not read the journal: journalctl: exit status 1\n"
			}
			if err != want {
				t.Fatalf("stderr %q", err)
			}
			for _, c := range f.commands {
				if c.Name == "systemctl" && (c.Args[0] == "restart" || c.Args[0] == "start") && strings.Contains(strings.Join(c.Args, " "), "ikigenba-alpha") {
					t.Fatal("later app started")
				}
			}
			if f.read("opt/ikigenba/releases/"+transitionOld+"/release.json") == "" {
				t.Fatal("old release removed")
			}
		})
	}
	f := newTransitionFixture(t)
	f.addRelease(transitionOld, "dummy")
	f.link("current", transitionOld)
	f.disabled["dummy"] = true
	code, out, err := f.run("activate", transitionSHA)
	if code != 0 || err != "" || !strings.Contains(out, "service: ok (dummy aaaaaaa disabled)") {
		t.Fatalf("%d %s %s", code, out, err)
	}
	for _, c := range f.commands {
		if c.Name == "systemctl" && (c.Args[0] == "enable" || c.Args[0] == "start" || c.Args[0] == "restart") && strings.Contains(strings.Join(c.Args, " "), "ikigenba-dummy") {
			t.Fatalf("disabled app action %+v", c)
		}
	}
}
func TestActivateChownAndModesNoSymlinkFollowing(t *testing.T) {
	// R-SLGV-2CH7
	f := newTransitionFixture(t)
	f.write("outside", "outside", 0o666)
	link := filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA, "dummy/etc/linked")
	if e := os.Symlink(filepath.Join(f.root, "outside"), link); e != nil {
		t.Fatal(e)
	}
	code, out, err := f.run("activate", transitionSHA)
	if code != 0 {
		t.Fatalf("%d %s %s", code, out, err)
	}
	c := f.commands[0]
	if c.Name != "chown" || !reflect.DeepEqual(c.Args, []string{"--recursive", "--no-dereference", "root:root", filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA)}) {
		t.Fatalf("chown %+v", c)
	}
	for _, p := range []string{"dummy/bin/dummy", "dummy/etc/manifest.toml"} {
		info, e := os.Lstat(filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA, p))
		if e != nil || info.Mode().Perm()&0o022 != 0 {
			t.Fatalf("mode %s %v %v", p, info, e)
		}
	}
	info, e := os.Lstat(filepath.Join(f.root, "outside"))
	if e != nil || info.Mode().Perm() != 0o666 {
		t.Fatalf("outside %v %v", info, e)
	}
}
func TestActivateCutoverAndSnapshotFailure(t *testing.T) {
	// R-SRKC-Z76O R-BHC4-553M R-B3X7-XNXZ R-BB8M-8AE5
	for _, failSnapshot := range []bool{false, true} {
		t.Run(fmt.Sprint(failSnapshot), func(t *testing.T) {
			f := newTransitionFixture(t)
			f.write("opt/legacy/etc/manifest.toml", "app='legacy'", 0o644)
			f.write("opt/legacy/bin/legacy", "binary", 0o755)
			f.write("etc/opt/ikigenba/legacy/env", "moved", 0o640)
			f.write("var/opt/ikigenba/legacy/state/keep", "state", 0o600)
			f.write("var/lib/ikigenba/services.json", "old", 0o644)
			f.write("opt/aws/keep", "keep", 0o644)
			f.write("etc/systemd/system/ikigenba-legacy.service", "ExecStart="+filepath.Join(f.root, "opt/legacy/bin/legacy")+"\n", 0o644)
			if failSnapshot {
				f.fail = "zstd --quiet --stdout"
			}
			code, out, err := f.run("activate", transitionSHA)
			if failSnapshot {
				if code != 1 || !strings.Contains(out, "snapshot: failed:") || strings.Contains(out, "cutover:") {
					t.Fatalf("%d %s %s", code, out, err)
				}
				if f.read("opt/legacy/bin/legacy") != "binary" {
					t.Fatal("cutover before snapshot")
				}
				return
			}
			if code != 0 || err != "" || !strings.Contains(out, "layout: ok (per app; cutover of 1 app)") || !strings.Contains(out, "snapshot: ok (1 service)") || !strings.Contains(out, "cutover: ok (removed 1 app from /opt)") {
				t.Fatalf("%d %s %s", code, out, err)
			}
			f.missing("opt/legacy")
			f.missing("var/lib/ikigenba/services.json")
			if f.read("opt/aws/keep") != "keep" || f.read("var/opt/ikigenba/legacy/state/keep") != "state" {
				t.Fatal("cutover lost unrelated data")
			}
			if len(f.cloud.objects) != 1 {
				t.Fatalf("snapshot objects %v", f.cloud.objects)
			}
		})
	}
}

func TestActivateNewDataModesAndDroppedFailure(t *testing.T) {
	// R-SU05-QQO2 R-SJ12-ASZT R-BX6T-45QN R-BYEP-HXHC
	f := newTransitionFixture(t)
	f.addRelease(transitionOld, "dropped")
	f.link("current", transitionOld)
	f.write("etc/systemd/system/ikigenba-dropped.service", "old service", 0o644)
	f.write("etc/systemd/system/ikigenba-dropped.socket", "old socket", 0o644)
	f.write("etc/opt/ikigenba/dropped/env", "old env", 0o640)
	f.write("var/opt/ikigenba/dropped/state/file", "saved", 0o600)
	f.fail = "systemctl disable --now ikigenba-dropped.socket ikigenba-dropped.service"
	code, out, err := f.run("activate", transitionSHA, "label")
	if code != 1 || !strings.Contains(out, "units: failed: dropped: systemctl disable --now ikigenba-dropped.socket ikigenba-dropped.service: exit status 1\n") || strings.Contains(out, "links:") || err != "opsctl: activate failed\n\n> command output\n> error output\n" {
		t.Fatalf("%d %s %s", code, out, err)
	}
	for _, item := range []struct{ p, want string }{{"etc/systemd/system/ikigenba-dropped.service", "old service"}, {"etc/systemd/system/ikigenba-dropped.socket", "old socket"}, {"etc/opt/ikigenba/dropped/env", "old env"}, {"var/opt/ikigenba/dropped/state/file", "saved"}, {"opt/ikigenba/releases/" + transitionSHA + "/label", "label\n"}} {
		if f.read(item.p) != item.want {
			t.Fatalf("%s changed", item.p)
		}
	}
	info, e := os.Lstat(filepath.Join(f.root, "var/opt/ikigenba/dummy"))
	if e != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("data mode %v %v", info, e)
	}
	n := 0
	for _, c := range f.commands {
		if c.Name == "chown" && reflect.DeepEqual(c.Args, []string{"ikigenba:ikigenba", filepath.Join(f.root, "var/opt/ikigenba/dummy")}) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("new data chown count %d", n)
	}
}
func TestActivateLayoutGuardsAndDefaultOrder(t *testing.T) {
	// R-BB8M-8AE5 R-BCGI-M24U
	for _, fault := range []string{"invalid current", "unmoved state", "unmoved env", "duplicate default"} {
		t.Run(fault, func(t *testing.T) {
			f := newTransitionFixture(t)
			want := ""
			switch fault {
			case "invalid current":
				f.write("opt/ikigenba/current", "bad", 0o644)
				want = "layout: failed: /opt/ikigenba/current does not name a release\n"
			case "unmoved state", "unmoved env":
				f.write("opt/legacy/etc/manifest.toml", "app='legacy'", 0o644)
				if fault == "unmoved state" {
					f.write("opt/legacy/state/file", "x", 0o600)
					want = "layout: failed: legacy: /opt/legacy/state has not moved; install legacy first\n"
				} else {
					want = "layout: failed: legacy: /opt/legacy/etc/env has not moved; install legacy first\n"
				}
			case "duplicate default":
				f.addRelease(transitionSHA, "alpha")
				for _, app := range []string{"alpha", "dummy"} {
					f.write("opt/ikigenba/releases/"+transitionSHA+"/"+app+"/etc/manifest.toml", "app='"+app+"'\ndefault=true", 0o644)
				}
				want = "manifests: failed: dummy: alpha is already the default app\n"
			}
			code, out, err := f.run("activate", transitionSHA)
			if code != 1 || !strings.HasSuffix(out, want) || err != "opsctl: activate failed\n" {
				t.Fatalf("%d %s %s", code, out, err)
			}
			f.missing("etc/opt/ikigenba/dummy/env")
		})
	}
}

func TestActivateSystemdAndReadyServiceCommandSequences(t *testing.T) {
	// R-BR3B-7B16 R-SNWN-TVYL
	f := newTransitionFixture(t)
	f.addRelease(transitionSHA, "alpha", "beta", "zcore", "acore")
	for _, name := range []string{"acore", "zcore"} {
		f.write("opt/ikigenba/releases/"+transitionSHA+"/"+name+"/etc/manifest.toml", "app='"+name+"'\n[resources]\nslice='core'", 0o644)
	}
	f.addRelease(transitionOld, "beta")
	f.link("current", transitionOld)
	f.disabled["beta"] = true
	f.active["acore"] = true
	f.active["dummy"] = true
	code, out, err := f.run("activate", transitionSHA)
	if code != 0 || err != "" {
		t.Fatalf("%d %s %s", code, out, err)
	}
	var systemd, service []string
	for _, c := range f.commands {
		if c.Name != "systemctl" {
			continue
		}
		args := strings.Join(c.Args, " ")
		if c.Args[0] == "daemon-reload" || c.Args[0] == "enable" {
			systemd = append(systemd, args)
		}
		if len(c.Args) > 1 && strings.HasSuffix(c.Args[len(c.Args)-1], ".service") && c.Args[len(c.Args)-1] != "litestream.service" && (c.Args[0] == "start" || c.Args[0] == "restart" || strings.HasPrefix(args, "show --property=ActiveState ")) {
			service = append(service, args)
		}
	}
	wantSystemd := []string{"daemon-reload"}
	for _, name := range []string{"acore", "alpha", "dummy", "zcore"} {
		wantSystemd = append(wantSystemd, "enable ikigenba-"+name+".service", "enable --now ikigenba-"+name+".socket")
	}
	wantSystemd = append(wantSystemd, "enable ikigenba-services.service")
	if !reflect.DeepEqual(systemd, wantSystemd) {
		t.Fatalf("systemd %v want %v", systemd, wantSystemd)
	}
	var wantService []string
	for _, name := range []string{"acore", "zcore", "alpha", "dummy"} {
		unit := "ikigenba-" + name + ".service"
		action := "start"
		if name == "acore" || name == "dummy" {
			action = "restart"
		}
		wantService = append(wantService, "show --property=ActiveState "+unit, action+" "+unit, "show --property=ActiveState "+unit)
	}
	if !reflect.DeepEqual(service, wantService) {
		t.Fatalf("service sequence %v want %v", service, wantService)
	}
	var serviceLines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "service:") {
			serviceLines = append(serviceLines, line)
		}
	}
	wantLines := []string{"service: ok (acore aaaaaaa active)", "service: ok (zcore aaaaaaa active)", "service: ok (alpha aaaaaaa active)", "service: ok (beta aaaaaaa disabled)", "service: ok (dummy aaaaaaa active)"}
	if !reflect.DeepEqual(serviceLines, wantLines) {
		t.Fatalf("service lines %v", serviceLines)
	}
}

func TestActivateGeneratedConfigurationsAndReloads(t *testing.T) {
	// R-BSB7-L2RV
	f := newTransitionFixture(t)
	if err := (config.Store{Root: f.root}).Set("host.apex", "dummy"); err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		f.commands = nil
		code, out, err := f.run("activate", transitionSHA)
		if code != 0 || err != "" {
			t.Fatalf("%d %s %s", code, out, err)
		}
		nginxBytes := f.read("etc/nginx/conf.d/ikigenba.conf")
		for _, hook := range []string{"dummy.sbx.example.test example.test;", "ssl_certificate     /etc/letsencrypt/live/sbx.example.test/fullchain.pem;", "unix:/run/ikigenba/dummy.sock"} {
			if !strings.Contains(nginxBytes, hook) {
				t.Fatalf("nginx missing %q in %s", hook, nginxBytes)
			}
		}
		servicesBytes := f.read("run/ikigenba/services.json")
		wantServices := "{\n  \"services\": [\n    { \"name\": \"dummy\", \"url\": \"https://dummy.sbx.example.test\", \"description\": \"\", \"socket\": \"/run/ikigenba/dummy.sock\", \"enabled\": true, \"mcp\": false }\n  ]\n}\n"
		if servicesBytes != wantServices {
			t.Fatalf("services %q", servicesBytes)
		}
		wantLitestream := "region: 'region'\nsocket:\n  enabled: true\n  path: '" + filepath.Join(f.root, "var/run/litestream.sock") + "'\n  permissions: 0600\nretention:\n  enabled: false\ndbs: []\n"
		if got := f.read("etc/litestream.yml"); got != wantLitestream {
			t.Fatalf("litestream %q want %q", got, wantLitestream)
		}
		reloads, restarts := 0, 0
		for _, c := range f.commands {
			if c.Name == "systemctl" && reflect.DeepEqual(c.Args, []string{"reload-or-restart", "nginx"}) {
				reloads++
			}
			if c.Name == "systemctl" && reflect.DeepEqual(c.Args, []string{"restart", "litestream.service"}) {
				restarts++
			}
		}
		wantRestarts := 1
		detail := "updated"
		if run == 1 {
			wantRestarts = 0
			detail = "unchanged"
		}
		if reloads != 1 || restarts != wantRestarts || !strings.Contains(out, "litestream: ok ("+detail+")\n") {
			t.Fatalf("reloads=%d restarts=%d output=%s", reloads, restarts, out)
		}
	}
}

func TestActivateReleaseModesPreserveUnrelatedBits(t *testing.T) {
	// R-SLGV-2CH7
	f := newTransitionFixture(t)
	base := "opt/ikigenba/releases/" + transitionSHA
	cases := []struct {
		name          string
		before, after os.FileMode
	}{{base + "/dummy/bin/dummy", 0o777, 0o755}, {base + "/dummy/etc/manifest.toml", 0o666, 0o644}, {base + "/dummy/libexec/helper", 0o751, 0o751}, {base + "/dummy/share/data", 0o464, 0o444}}
	for _, tc := range cases {
		if strings.HasSuffix(tc.name, "/manifest.toml") {
			f.write(tc.name, "app='dummy'", tc.before)
		} else {
			f.write(tc.name, "file", tc.before)
		}
	}
	if err := os.Chmod(filepath.Join(f.root, base), 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	code, out, err := f.run("activate", transitionSHA)
	if code != 0 {
		t.Fatalf("%d %s %s", code, out, err)
	}
	for _, tc := range cases {
		info, e := os.Lstat(filepath.Join(f.root, tc.name))
		if e != nil || info.Mode().Perm() != tc.after {
			t.Fatalf("mode %s got %v %v want %v", tc.name, info, e, tc.after)
		}
	}
	info, e := os.Lstat(filepath.Join(f.root, base))
	if e != nil || info.Mode().Perm() != 0o755 || info.Mode()&os.ModeSticky == 0 {
		t.Fatalf("folder unrelated bits %v %v", info, e)
	}
}

func TestActivateSnapshotWholeSelectionOneTimestampAndFirstFailure(t *testing.T) {
	// R-SRKC-Z76O
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			f := newTransitionFixture(t)
			f.write("opt/alpha/etc/manifest.toml", "app='alpha'", 0o644)
			f.write("etc/opt/ikigenba/alpha/env", "moved", 0o640)
			for _, name := range []string{"alpha", "beta", "charlie"} {
				f.write("var/opt/ikigenba/"+name+"/state/file", name, 0o600)
			}
			nowCalls, openCalls := 0, 0
			f.deps.Now = func() time.Time { nowCalls++; return time.Date(2026, 1, 2, 3, 4, 5+nowCalls, 0, time.UTC) }
			f.deps.Cloud.Open = func(_ context.Context, region string) (cloud.Client, error) {
				openCalls++
				if region != "region" {
					t.Fatalf("snapshot region %q", region)
				}
				return f.cloud, nil
			}
			if fail {
				f.cloud.failNames = map[string]bool{"alpha": true, "beta": true}
			}
			code, out, err := f.run("activate", transitionSHA)
			if nowCalls != 1 || openCalls != 1 {
				t.Fatalf("snapshot calls time %d cloud %d", nowCalls, openCalls)
			}
			wantCalls := []string{}
			for _, name := range []string{"alpha", "beta", "charlie"} {
				wantCalls = append(wantCalls, "s3://bucket/host/snapshots/"+name+"/2026-01-02T03:04:06Z.tar.zst")
			}
			if !reflect.DeepEqual(f.cloud.putCalls, wantCalls) {
				t.Fatalf("snapshot selection/timestamp %v want %v", f.cloud.putCalls, wantCalls)
			}
			if fail {
				if code != 1 || !strings.Contains(out, "snapshot: failed: alpha: upload alpha failed\n") || strings.Contains(out, "cutover:") || err != "opsctl: activate failed\n" {
					t.Fatalf("%d %s %s", code, out, err)
				}
				if len(f.cloud.objects) != 1 || f.cloud.objects[wantCalls[2]] == nil {
					t.Fatalf("successful object lost %v", f.cloud.objects)
				}
				if f.read("opt/alpha/etc/manifest.toml") != "app='alpha'" {
					t.Fatal("cutover ran on failed snapshot")
				}
				return
			}
			if code != 0 || err != "" || !strings.Contains(out, "snapshot: ok (3 services)\n") || len(f.cloud.objects) != 3 {
				t.Fatalf("%d %s %s objects %v", code, out, err, f.cloud.objects)
			}
		})
	}
}

func TestActivateSuccessfulStartStillRequiresActiveFinalState(t *testing.T) {
	// R-SNWN-TVYL R-SP4K-7NPA
	f := newTransitionFixture(t)
	f.addRelease(transitionSHA, "alpha", "beta")
	finalShows := 0
	f.deps.Execute = func(ctx context.Context, c host.Command) (host.Result, error) {
		result, err := f.execute(ctx, c)
		if c.Name == "systemctl" && reflect.DeepEqual(c.Args, []string{"show", "--property=ActiveState", "ikigenba-alpha.service"}) {
			finalShows++
			if finalShows == 2 {
				return host.Result{Stdout: []byte("ActiveState=inactive\n")}, nil
			}
		}
		return result, err
	}
	code, out, stderr := f.run("activate", transitionSHA)
	if code != 1 || !strings.HasSuffix(out, "service: failed: alpha: service failed to start\n") || strings.Contains(out, "retention:") || stderr != "opsctl: activate failed\n\n> journal first\n> journal second\n" {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	if finalShows != 2 || !f.active["alpha"] {
		t.Fatalf("start succeeded but final readiness not checked: shows %d active %v", finalShows, f.active)
	}
	var sequence []string
	for _, c := range f.commands {
		if c.Name == "systemctl" && (c.Args[0] == "start" || c.Args[0] == "restart") && (c.Args[len(c.Args)-1] == "ikigenba-beta.service" || c.Args[len(c.Args)-1] == "ikigenba-dummy.service") {
			t.Fatalf("later app started %+v", c)
		}
		if c.Name == "systemctl" && c.Args[len(c.Args)-1] == "ikigenba-alpha.service" && (c.Args[0] == "show" || c.Args[0] == "start") {
			sequence = append(sequence, c.Name+" "+strings.Join(c.Args, " "))
		}
		if c.Name == "journalctl" {
			sequence = append(sequence, c.Name+" "+strings.Join(c.Args, " "))
		}
	}
	want := []string{"systemctl show --property=ActiveState ikigenba-alpha.service", "systemctl start ikigenba-alpha.service", "systemctl show --property=ActiveState ikigenba-alpha.service", "journalctl --unit ikigenba-alpha.service --no-pager --lines 50"}
	if !reflect.DeepEqual(sequence, want) {
		t.Fatalf("readiness failure sequence %v want %v", sequence, want)
	}
}

func (f *transitionFixture) seedOpsctlLink(kind string) {
	f.t.Helper()
	switch kind {
	case "missing":
		f.missing("usr/local/bin/opsctl")
	case "file":
		f.write("usr/local/bin/opsctl", "stale binary", 0o755)
	case "wrong symlink":
		f.write("usr/local/bin/stale", "stale binary", 0o755)
		if err := os.Symlink("stale", filepath.Join(f.root, "usr/local/bin/opsctl")); err != nil {
			f.t.Fatal(err)
		}
	default:
		f.t.Fatalf("unknown opsctl link fixture %q", kind)
	}
}

func (f *transitionFixture) requireReleaseLink(name, target string) os.FileInfo {
	f.t.Helper()
	p := filepath.Join(f.root, name)
	info, err := os.Lstat(p)
	if err != nil {
		f.t.Fatalf("%s: %v", name, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		f.t.Fatalf("%s: expected symbolic link, got %v", name, info.Mode())
	}
	got, err := os.Readlink(p)
	if err != nil || got != target {
		f.t.Fatalf("%s: target %q, error %v, want %q", name, got, err, target)
	}
	return info
}

func TestActivateRepairsOpsctlLink(t *testing.T) {
	// R-SSS9-CYXD
	for _, state := range []string{"fresh", "transition", "reactivation"} {
		for _, kind := range []string{"missing", "file", "wrong symlink"} {
			t.Run(state+"/"+kind, func(t *testing.T) {
				f := newTransitionFixture(t)
				f.addRelease(transitionOld, "dummy")
				var currentBefore, previousBefore os.FileInfo
				switch state {
				case "fresh":
					f.link("previous", transitionOld)
				case "transition":
					f.link("current", transitionOld)
				case "reactivation":
					f.link("current", transitionSHA)
					f.link("previous", transitionOld)
					currentBefore = f.requireReleaseLink("opt/ikigenba/current", "releases/"+transitionSHA)
					previousBefore = f.requireReleaseLink("opt/ikigenba/previous", "releases/"+transitionOld)
				}
				f.seedOpsctlLink(kind)
				code, out, stderr := f.run("activate", transitionSHA)
				if code != 0 || stderr != "" {
					t.Fatalf("activate: %d\n%s\n%s", code, out, stderr)
				}
				f.requireReleaseLink("usr/local/bin/opsctl", release.CurrentOpsctl)
				currentAfter := f.requireReleaseLink("opt/ikigenba/current", "releases/"+transitionSHA)
				previousDetail := "none"
				if state == "fresh" {
					f.missing("opt/ikigenba/previous")
				} else {
					previousAfter := f.requireReleaseLink("opt/ikigenba/previous", "releases/"+transitionOld)
					previousDetail = "bbbbbbb"
					if state == "reactivation" {
						previousDetail += " kept"
						if !os.SameFile(currentBefore, currentAfter) || !os.SameFile(previousBefore, previousAfter) {
							t.Fatal("reactivation rewrote current or previous")
						}
					}
				}
				if !strings.Contains(out, "links: ok (current aaaaaaa, previous "+previousDetail+")\n") {
					t.Fatal(out)
				}
			})
		}
	}
}
