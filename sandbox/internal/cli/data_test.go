package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func dataTestPaths(t *testing.T) paths {
	t.Helper()
	root := t.TempDir()
	return paths{state: root, config: root, root: path.Join(root, "ikigenba/sandbox"), units: path.Join(root, "systemd/user")}
}
func dataTestRegistry(t *testing.T, p paths, reg registry) {
	t.Helper()
	if err := withRegistry(p, func(r *registry) error { *r = reg; return nil }); err != nil {
		t.Fatal(err)
	}
}
func TestDataNames(t *testing.T) {
	// R-ENE0-OCLN R-0I4N-RIZW R-EOLX-24CC
	for _, name := range []string{"a", "1", "a-b", "nginx", strings.Repeat("x", 63)} {
		if !validSandboxName(name) {
			t.Fatalf("sandbox %q refused", name)
		}
	}
	for _, name := range []string{"", "-a", "a-", "a--b", "A", "é", strings.Repeat("x", 64)} {
		if validSandboxName(name) {
			t.Fatalf("sandbox %q accepted", name)
		}
	}
	for _, name := range []string{"a", "1", "a-b", "a--b", strings.Repeat("x", 63)} {
		if !usableAppName(name) {
			t.Fatalf("app %q refused", name)
		}
	}
	for _, name := range []string{"", "nginx", "-a", "a-", "A", "é", strings.Repeat("x", 64)} {
		if usableAppName(name) {
			t.Fatalf("app %q accepted", name)
		}
	}
	for input, want := range map[string]string{"/tmp/Feature_X": "feature-x", "/tmp/wip": "wip", "/tmp/__Éa..B!!": "a-b", "/tmp/é": "", "/tmp/A---2": "a-2"} {
		if got := derivedName(input); got != want {
			t.Fatalf("%q: %q != %q", input, got, want)
		}
	}
}
func TestDataRootSelection(t *testing.T) {
	// R-0KKG-J2HA R-0LSC-WU7Z R-8A9R-GL9H R-8BHN-UD06
	for _, tc := range []struct{ home, state, config, wstate, wconfig string }{{"/home/dev", "/tmp/a/../state", "/tmp/a/../config", "/tmp/state", "/tmp/config"}, {"/home/dev/../user", "relative", "", "/home/user/.local/state", "/home/user/.config"}, {"relative", "/", "/", "/", "/"}} {
		r := invocation{deps: seam.Deps{Getenv: func(k string) string {
			return map[string]string{"HOME": tc.home, "XDG_STATE_HOME": tc.state, "XDG_CONFIG_HOME": tc.config}[k]
		}}}
		p, err := r.roots(true)
		if err != nil {
			t.Fatal(err)
		}
		if p.state != tc.wstate || p.config != tc.wconfig || p.root != path.Join(tc.wstate, "ikigenba/sandbox") || p.units != path.Join(tc.wconfig, "systemd/user") {
			t.Fatalf("roots %#v", p)
		}
	}
}
func TestDataLayout(t *testing.T) {
	// R-EZL0-I20L R-F0SW-VTRA R-F20T-9LHZ R-F38P-ND8O
	// R-F4GM-14ZD R-RAL0-PDMM R-RD0T-GX40 R-ZEB1-HI5Q
	// R-F6WE-SOGR R-F84B-6G7G R-F9C7-K7Y5 R-FAK3-XZOU
	// R-YQG2-RGNZ R-FBS0-BRFJ R-FCZW-PJ68 R-FE7T-3AWX
	// R-FHVI-8M50 R-FJ3E-MDVP R-FKBB-05ME
	p := paths{root: "/state/ikigenba/sandbox", units: "/config/systemd/user"}
	base := p.root + "/wip"
	pairs := [][2]string{{p.registryPath(), p.root + "/registry.json"}, {p.registryLock(), p.root + "/registry.json.lock"}, {p.lock("wip"), p.root + "/wip.lock"}, {p.data("wip"), base}, {p.binary("wip", "auth"), base + "/bin/auth"}, {p.stage("wip"), base + "/stage"}, {p.stageBinary("wip", "auth"), base + "/stage/bin/auth"}, {p.stageNginx("wip"), base + "/stage/nginx/nginx.conf"}, {p.stageEnv("wip", "auth"), base + "/stage/env/auth.env"}, {p.env("wip", "auth"), base + "/env/auth.env"}, {p.services("wip"), base + "/services.json"}, {p.nginx("wip"), base + "/nginx"}, {p.nginxConf("wip"), base + "/nginx/nginx.conf"}, {p.nginxPID("wip"), base + "/nginx/nginx.pid"}, {p.appDir("wip", "auth"), base + "/apps/auth"}, {p.appState("wip", "auth"), base + "/apps/auth/state"}, {p.token("wip"), base + "/token"}, {path.Join(p.units, socketUnit("wip", "auth")), p.units + "/sandbox-wip-auth.socket"}, {path.Join(p.units, serviceUnit("wip", "auth")), p.units + "/sandbox-wip-auth.service"}, {path.Join(p.units, nginxUnit("wip")), p.units + "/sandbox-wip-nginx.service"}}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Fatalf("%q != %q", pair[0], pair[1])
		}
	}
}
func TestDataSocketAndOrigins(t *testing.T) {
	// R-FLJ7-DXD3 R-FMR3-RP3S R-0QNY-FX6R R-0RVU-TOXG R-0T3R-7GO5 R-LTAU-0RJC
	e := registryEntry{Name: "wip", Port: 7400, Apps: []registryApp{{Name: "auth"}, {Name: "dummy"}}}
	if socketPath(1000, e.Port, "auth") != "/run/user/1000/sandbox/7400/auth.sock" {
		t.Fatal("socket path")
	}
	if len(socketPath(4294967294, 7499, strings.Repeat("a", 63))) != 102 {
		t.Fatal("maximum socket length")
	}
	for _, name := range []string{"wip", "other"} {
		for _, root := range []string{"/a", "/long/home"} {
			t.Setenv("HOME", root)
			t.Setenv("XDG_RUNTIME_DIR", root)
			t.Setenv("XDG_STATE_HOME", root)
			t.Setenv("XDG_CONFIG_HOME", root)
			e.Name = name
			if socketPath(1000, e.Port, "auth") != "/run/user/1000/sandbox/7400/auth.sock" {
				t.Fatal("environment changed socket")
			}
		}
	}
	e.Name = "wip"
	if appOrigin(e, "auth") != "http://auth.wip.localhost:7400" || defaultOrigin(e) != "http://wip.localhost:7400" || callbackOrigin(e) != "http://localhost:7400" {
		t.Fatal("origins")
	}
	want := []string{"sandbox-wip-auth.service", "sandbox-wip-auth.socket", "sandbox-wip-dummy.service", "sandbox-wip-dummy.socket", "sandbox-wip-nginx.service"}
	if !reflect.DeepEqual(unitNames(e), want) {
		t.Fatal(unitNames(e))
	}
}
func TestDataUnitValues(t *testing.T) {
	// R-XG3W-86HL R-XHBS-LY8A R-XJRL-DHPO
	in := "/tmp/a b%c$d"
	if unitArgument(in) != `"/tmp/a b%%c$$d"` || unitExecutable(in) != `"/tmp/a b%%c$d"` || unitPath(in) != "/tmp/a b%%c$d" {
		t.Fatal("unit path escaping")
	}
}
func TestDataRegistryShapeAndSorting(t *testing.T) {
	// R-FWIA-TV1C R-FXQ7-7MS1 R-LKWF-H37Z R-LQZX-DXXG
	p := dataTestPaths(t)
	dataTestRegistry(t, p, registry{Sandboxes: []registryEntry{{Name: "z", Port: 7401, Worktree: "/gone/z", Apps: nil}, {Name: "a", Port: 7400, Worktree: "/tree/a", Apps: []registryApp{{Name: "z"}, {Name: "a", Default: true}}}}})
	b, err := os.ReadFile(p.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string][]map[string]json.RawMessage
	if err = json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 || len(raw["sandboxes"]) != 2 {
		t.Fatal(string(b))
	}
	for _, e := range raw["sandboxes"] {
		if len(e) != 4 {
			t.Fatal("entry fields", e)
		}
	}
	r, err := readRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Sandboxes[0].Name != "a" || r.Sandboxes[0].Apps[0].Name != "a" || len(r.Sandboxes[1].Apps) != 0 {
		t.Fatal(r)
	}
	if _, ok := r.find("a"); !ok {
		t.Fatal("known entry missing")
	}
	if _, ok := r.find("missing"); ok {
		t.Fatal("unknown known")
	}
	var apps []map[string]json.RawMessage
	if err = json.Unmarshal(raw["sandboxes"][0]["apps"], &apps); err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if len(app) != 2 {
			t.Fatal(app)
		}
	}
	if string(raw["sandboxes"][1]["apps"]) != "[]" {
		t.Fatal("nil apps encoded")
	}
	dataTestRegistry(t, p, registry{})
	b, err = os.ReadFile(p.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{\n  \"sandboxes\": []\n}\n" {
		t.Fatal(string(b))
	}
}
func TestDataMissingRegistry(t *testing.T) {
	// R-LM4B-UUYO R-LNC8-8MPD
	p := dataTestPaths(t)
	r, err := readRegistry(p)
	if err != nil || len(r.Sandboxes) != 0 {
		t.Fatal(r, err)
	}
	if _, err = os.Stat(p.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created root", err)
	}
}
func TestDataRegistryValidation(t *testing.T) {
	// R-8CPK-84QV
	valid := `{"sandboxes":[{"name":"wip","port":7400,"worktree":"/tmp/wip","apps":[{"name":"auth","default":true}]}]}`
	cases := []string{`{`, `null`, `[]`, `{}`, `{"sandboxes":null}`, `{"sandboxes":{}}`, `{"sandboxes":[null]}`}
	for _, key := range []string{"name", "port", "worktree", "apps"} {
		var r map[string][]map[string]any
		if err := json.Unmarshal([]byte(valid), &r); err != nil {
			t.Fatal(err)
		}
		delete(r["sandboxes"][0], key)
		b, _ := json.Marshal(r)
		cases = append(cases, string(b))
	}
	for _, key := range []string{"name", "default"} {
		var r map[string][]map[string]any
		if err := json.Unmarshal([]byte(valid), &r); err != nil {
			t.Fatal(err)
		}
		delete(r["sandboxes"][0]["apps"].([]any)[0].(map[string]any), key)
		b, _ := json.Marshal(r)
		cases = append(cases, string(b))
	}
	for _, replace := range [][2]string{{`"wip"`, `null`}, {`"wip"`, `1`}, {`7400`, `"7400"`}, {`7400`, `null`}, {`7400`, `7399`}, {`7400`, `7500`}, {`"/tmp/wip"`, `false`}, {`"/tmp/wip"`, `null`}, {`"auth"`, `null`}, {`"auth"`, `false`}, {`true`, `null`}, {`true`, `"true"`}, {`"wip"`, `"a--b"`}, {`"auth"`, `"nginx"`}, {`"auth"`, `"A"`}, {`[{"name":"auth","default":true}]`, `null`}, {`[{"name":"auth","default":true}]`, `[{"name":"auth","default":false},{"name":"auth","default":false}]`}, {`[{"name":"auth","default":true}]`, `[{"name":"auth","default":true},{"name":"dummy","default":true}]`}} {
		cases = append(cases, strings.ReplaceAll(valid, replace[0], replace[1]))
	}
	entry := `{"name":"wip","port":7400,"worktree":"/tmp/wip","apps":[]}`
	cases = append(cases, `{"sandboxes":[`+entry+`,`+strings.Replace(entry, "7400", "7401", 1)+`]}`, `{"sandboxes":[`+entry+`,`+strings.Replace(entry, `"wip"`, `"other"`, 1)+`]}`)
	for i, content := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p := dataTestPaths(t)
			if err := os.MkdirAll(p.root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p.registryPath(), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readRegistry(p); err == nil {
				t.Fatal("accepted invalid", content)
			} else {
				var f failure
				if !errors.As(err, &f) || f.code != 1 || !strings.HasPrefix(f.message, p.registryPath()+": ") {
					t.Fatal(err)
				}
			}
		})
	}
	p := dataTestPaths(t)
	if err := os.MkdirAll(p.registryPath(), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := readRegistry(p)
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatal(err)
	}
	p = dataTestPaths(t)
	if err = os.MkdirAll(p.root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p.registryPath(), []byte(valid), 0000); err != nil {
		t.Fatal(err)
	}
	if _, err = readRegistry(p); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatal("unreadable registry", err)
	}
}
func TestDataAtomicRegistry(t *testing.T) {
	// R-LPS1-066R
	p := dataTestPaths(t)
	dataTestRegistry(t, p, registry{Sandboxes: []registryEntry{{Name: "a", Port: 7400, Worktree: "/a"}}})
	before, err := os.ReadFile(p.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	old := path.Join(t.TempDir(), "old")
	if err = os.Link(p.registryPath(), old); err != nil {
		t.Fatal(err)
	}
	dataTestRegistry(t, p, registry{Sandboxes: []registryEntry{{Name: "b", Port: 7401, Worktree: "/b"}}})
	after, err := os.ReadFile(filepath.Clean(old))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("hardlink changed")
	}
	now, err := os.ReadFile(p.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(now, before) {
		t.Fatal("registry not changed")
	}
}
func TestDataPortAllocation(t *testing.T) {
	// R-GQZS-E4KP R-GS7O-RWBE R-LPN4-VGB9
	r := registry{Sandboxes: []registryEntry{{Name: "one", Port: 7401}, {Name: "two", Port: 7400}, {Name: "three", Port: 7403}}}
	e, err := allocateEntry(&r, "new", "/new", nil)
	if err != nil || e.Port != 7402 || len(e.Apps) != 0 {
		t.Fatal(e, err)
	}
	again, err := allocateEntry(&r, "new", "/new", nil)
	if err != nil || again.Port != 7402 {
		t.Fatal(again, err)
	}
	r = registry{}
	for port := 7400; port <= 7499; port++ {
		r.Sandboxes = append(r.Sandboxes, registryEntry{Name: fmt.Sprintf("name%d", port), Port: port, Worktree: "/tree", Apps: []registryApp{}})
	}
	p := dataTestPaths(t)
	dataTestRegistry(t, p, r)
	before, err := os.ReadFile(p.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	err = withRegistry(p, func(r *registry) error { _, err := allocateEntry(r, "new", "/new", nil); return err })
	if err == nil {
		t.Fatal("exhaustion accepted")
	}
	var stderr bytes.Buffer
	call := invocation{stderr: &stderr}
	if code := call.report(err); code != 2 {
		t.Fatal(code)
	}
	want := "sandbox: no free port: every port from 7400 to 7499 belongs to a sandbox\n\nrun 'sandbox ls' and wipe a sandbox you no longer need\n"
	if stderr.String() != want {
		t.Fatal(stderr.String())
	}
	after, err := os.ReadFile(p.registryPath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("registry changed", err)
	}
}
func TestDataUnitClash(t *testing.T) {
	// R-LUIQ-EJA1 R-LVQM-SB0Q
	r := registry{Sandboxes: []registryEntry{{Name: "a-b", Port: 7401, Apps: []registryApp{{Name: "c"}}}, {Name: "a", Port: 7400, Apps: nil}}}
	err := checkUnitClash(r, "a", []registryApp{{Name: "b-c"}})
	want := "unit 'sandbox-a-b-c.service' would also belong to sandbox 'a-b'\n\nrename this worktree or the app, or wipe sandbox 'a-b' once it is down"
	if err == nil || err.Error() != want {
		t.Fatal(err)
	}
	before := len(r.Sandboxes)
	if _, err = allocateEntry(&r, "a", "/a", []registryApp{{Name: "b-c"}}); err == nil || len(r.Sandboxes) != before {
		t.Fatal("clash allocated", err)
	}
	if err = checkUnitClash(r, "a", []registryApp{{Name: "auth"}}); err != nil {
		t.Fatal(err)
	}
	r.Sandboxes = append(r.Sandboxes, registryEntry{Name: "a-b-c", Port: 7402, Apps: []registryApp{{Name: "d"}}})
	err = checkUnitClash(r, "a", []registryApp{{Name: "b-c-d"}, {Name: "b-c"}})
	want = "unit 'sandbox-a-b-c-d.service' would also belong to sandbox 'a-b-c'\n\nrename this worktree or the app, or wipe sandbox 'a-b-c' once it is down"
	if err == nil || err.Error() != want {
		t.Fatal("lowest unit not selected", err)
	}
	r = registry{Sandboxes: []registryEntry{
		{Name: "a-b-c", Port: 7402, Apps: []registryApp{{Name: "d"}}},
		{Name: "a-b", Port: 7401, Apps: []registryApp{{Name: "c-d"}}},
	}}
	err = checkUnitClash(r, "a", []registryApp{{Name: "b-c-d"}})
	want = "unit 'sandbox-a-b-c-d.service' would also belong to sandbox 'a-b'\n\nrename this worktree or the app, or wipe sandbox 'a-b' once it is down"
	if err == nil || err.Error() != want {
		t.Fatal("lowest owner not selected", err)
	}
}
func TestDataLock(t *testing.T) {
	// R-D4CI-20WP R-D5KE-FSNE R-GESS-KF5R
	p := path.Join(t.TempDir(), "missing", "sandbox.lock")
	unlock, err := lockFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Clean(p), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("same-process lock not exclusive: %v", err)
	}
	unlock()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
}
func TestDataBlockingLock(t *testing.T) {
	// R-D6SA-TKE3
	p := path.Join(t.TempDir(), "held.lock")
	unlock, err := lockFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ready := make(chan int)
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		ready <- syscall.Gettid()
		release, err := lockFile(p)
		if err == nil {
			release()
		}
		done <- err
	}()
	tid := <-ready
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(fmt.Sprintf("/proc/self/task/%d/syscall", tid))
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(b))
		if len(fields) > 0 && fields[0] == strconv.Itoa(syscall.SYS_FLOCK) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock waiter did not enter flock", string(b))
		}
		runtime.Gosched()
	}
	select {
	case err := <-done:
		t.Fatalf("lock did not wait: %v", err)
	default:
	}
	unlock()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDataPrivateEnvAndToken(t *testing.T) {
	// R-F6WE-SOGR R-FE7T-3AWX
	p := dataTestPaths(t)
	worktree := path.Join(t.TempDir(), "wip")
	manifest := path.Join(worktree, "dummy/etc/manifest.toml")
	if err := os.MkdirAll(path.Dir(manifest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("app = \"dummy\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deps := seam.Deps{Dir: worktree, EUID: 1000, Getenv: func(k string) string {
		return map[string]string{"XDG_STATE_HOME": p.state, "XDG_CONFIG_HOME": p.config}[k]
	}, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		switch cmd.Path {
		case "git":
			return seam.Result{Stdout: []byte(worktree + "\n")}, nil
		case "go":
			if err := os.WriteFile(cmd.Args[2], []byte("binary"), 0600); err != nil {
				return seam.Result{}, err
			}
		case "nginx":
			info, err := os.Stat(p.stageEnv("wip", "dummy"))
			if err != nil {
				return seam.Result{}, err
			}
			if info.Mode().Perm() != 0600 {
				t.Errorf("staged env permissions %o", info.Mode().Perm())
			}
		}
		return seam.Result{Stdout: []byte("inactive\n")}, nil
	}}
	if code, _, errout := dataRun(t, []string{"up"}, "", deps); code != 0 {
		t.Fatalf("up %d: %s", code, errout)
	}
	info, err := os.Stat(p.env("wip", "dummy"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("env permissions %o", info.Mode().Perm())
	}
	if code, _, errout := dataRun(t, []string{"token", "set"}, "ikp_example", deps); code != 0 {
		t.Fatalf("token set %d: %s", code, errout)
	}
	info, err = os.Stat(p.token("wip"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("token permissions %o", info.Mode().Perm())
	}
}

func TestDataClashingUpDoesNothing(t *testing.T) {
	// R-LUIQ-EJA1 R-LVQM-SB0Q
	p := dataTestPaths(t)
	worktree := path.Join(t.TempDir(), "a")
	manifest := path.Join(worktree, "b-c/etc/manifest.toml")
	if err := os.MkdirAll(path.Dir(manifest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("app = \"b-c\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dataTestRegistry(t, p, registry{Sandboxes: []registryEntry{{Name: "a-b", Port: 7400, Worktree: "/other", Apps: []registryApp{{Name: "c"}}}}})
	before, err := os.ReadFile(p.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	deps := seam.Deps{Dir: worktree, EUID: 1000, Getenv: func(k string) string {
		return map[string]string{"XDG_STATE_HOME": p.state, "XDG_CONFIG_HOME": p.config}[k]
	}, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		runs++
		if cmd.Path != "git" {
			t.Errorf("clashing up ran %s", cmd.Path)
		}
		return seam.Result{Stdout: []byte(worktree + "\n")}, nil
	}}
	code, out, errout := dataRun(t, []string{"up"}, "", deps)
	want := "sandbox: unit 'sandbox-a-b-c.service' would also belong to sandbox 'a-b'\n\nrename this worktree or the app, or wipe sandbox 'a-b' once it is down\n"
	if code != 2 || out != "" || errout != want || runs != 1 {
		t.Fatalf("clashing up: %d %q %q, runs%d", code, out, errout, runs)
	}
	after, err := os.ReadFile(p.registryPath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("clash changed registry", err)
	}
	entries, err := os.ReadDir(p.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "registry.json" && e.Name() != "registry.json.lock" && e.Name() != "a.lock" {
			t.Fatal("clash created", e.Name())
		}
	}
	if _, err = os.Stat(p.units); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("clash created units", err)
	}
}

func dataRun(t *testing.T, args []string, input string, deps seam.Deps) (int, string, string) {
	t.Helper()
	var out, errout bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(input), &out, &errout, deps)
	if code < 0 || code > 3 {
		t.Fatalf("invalid exit %d", code)
	}
	return code, out.String(), errout.String()
}
