package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

type upFixture struct {
	t                                          *testing.T
	root, worktree, state, config, data, units string
	calls                                      []seam.Cmd
	active                                     bool
	exec                                       func(seam.Cmd) (seam.Result, error)
}

func newUpFixture(t *testing.T, names ...string) *upFixture {
	t.Helper()
	root := t.TempDir()
	f := &upFixture{t: t, root: root, worktree: filepath.Join(root, "wip"), state: filepath.Join(root, "state a b%c$d"), config: filepath.Join(root, "config")}
	f.data = filepath.Join(f.state, "ikigenba", "sandbox", "wip")
	f.units = filepath.Join(f.config, "systemd", "user")
	if err := os.MkdirAll(f.worktree, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		f.put(filepath.Join(f.worktree, name, "etc", "manifest.toml"), "app = "+fmt.Sprintf("%q", name)+"\ndefault = false\n", 0644)
	}
	return f
}

func (f *upFixture) put(path, content string, mode os.FileMode) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *upFixture) registry(apps ...registryApp) {
	f.t.Helper()
	if apps == nil {
		apps = []registryApp{}
	}
	b, err := json.Marshal(registry{Sandboxes: []registryEntry{{Name: "wip", Port: 7400, Worktree: f.worktree, Apps: apps}}})
	if err != nil {
		f.t.Fatal(err)
	}
	f.put(filepath.Join(f.state, "ikigenba", "sandbox", "registry.json"), string(b), 0600)
}

func (f *upFixture) records() []registryEntry {
	f.t.Helper()
	b, err := upReadBytes(filepath.Join(f.state, "ikigenba", "sandbox", "registry.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	var reg registry
	if err = json.Unmarshal(b, &reg); err != nil {
		f.t.Fatal(err)
	}
	return reg.Sandboxes
}

func (f *upFixture) run(args ...string) (int, string, string) {
	f.t.Helper()
	var out, diagnostic bytes.Buffer
	deps := seam.Deps{Dir: f.worktree, EUID: 1000, Getenv: func(key string) string {
		switch key {
		case "HOME":
			return f.root
		case "XDG_STATE_HOME":
			return f.state
		case "XDG_CONFIG_HOME":
			return f.config
		}
		return ""
	}, Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		f.calls = append(f.calls, c)
		if f.exec != nil {
			return f.exec(c)
		}
		return f.answer(c)
	}, Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		f.t.Fatal("unexpected Stream")
		return seam.Result{}, nil
	}}
	code := runChecked(context.Background(), f.t, args, strings.NewReader(""), &out, &diagnostic, deps)
	return code, out.String(), diagnostic.String()
}

func (f *upFixture) answer(c seam.Cmd) (seam.Result, error) {
	f.t.Helper()
	switch c.Path {
	case "git":
		return seam.Result{Stdout: []byte(f.worktree + "\n")}, nil
	case "go":
		f.put(c.Args[2], "new:"+filepath.Base(c.Dir), 0755)
	case "systemctl":
		if len(c.Args) > 1 && c.Args[1] == "show" {
			state := "inactive\n"
			if f.active {
				state = "active\n"
			}
			return seam.Result{Stdout: []byte(state)}, nil
		}
	}
	return seam.Result{}, nil
}

// Rooted operations keep fixture access inside the named temporary parent.
func upReadBytes(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	b, readErr := root.ReadFile(filepath.Base(path))
	closeErr := root.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return b, nil
}

func upChmod(path string, mode os.FileMode) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	chmodErr := root.Chmod(filepath.Base(path), mode)
	closeErr := root.Close()
	if chmodErr != nil {
		return chmodErr
	}
	return closeErr
}

func upRead(t *testing.T, path string) string {
	t.Helper()
	b, err := upReadBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func upMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s exists or stat failed: %v", path, err)
	}
}
func upSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			snapshot[rel] = "directory"
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := upReadBytes(path)
		if err != nil {
			return err
		}
		snapshot[rel] = fmt.Sprintf("%o:%s", info.Mode(), b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestUpSuccessfulDeployment(t *testing.T) {
	// R-RGOI-M8C3 R-RHWF-002S R-XXC9-Z9PP R-XYK6-D1GE R-Y0ZZ-4KXS
	// R-Y4NO-9W5V R-U16K-JJPF R-RQFP-OE9N R-RRNM-260C R-ZFIX-V9WF
	// R-YRTR-JJ92 R-KSXM-UDTO R-YU9K-B2QG R-YVHG-OUH5 R-Z2SU-ZGXB R-IJUW-KM7L
	// R-Z58N-R0EP R-ZINJ-YHKC R-YGUO-3LKT R-S1ET-4BXW R-RP7T-AMIY
	f := newUpFixture(t, "dummy", "auth")
	f.exec = func(c seam.Cmd) (seam.Result, error) {
		if c.Path == "go" {
			entries := f.records()
			if len(entries) != 1 || entries[0].Port != 7400 || entries[0].Worktree != f.worktree || len(entries[0].Apps) != 0 {
				t.Fatalf("prebuild registry: %+v", entries)
			}
		}
		if c.Path == "nginx" {
			info, err := os.Stat(filepath.Join(f.data, "nginx"))
			if err != nil || !info.IsDir() {
				t.Fatalf("nginx directory: %v", err)
			}
			config := upRead(t, filepath.Join(f.data, "stage", "nginx", "nginx.conf"))
			want := string(renderNginxConfig(f.data, f.worktree, "wip", 7400, 1000, []appInfo{{Name: "auth"}, {Name: "dummy"}}))
			if config != want {
				t.Fatalf("staged configuration mismatch")
			}
			for _, app := range []string{"auth", "dummy"} {
				info, err := os.Stat(filepath.Join(f.data, "stage", "env", app+".env"))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("staged env mode: %v %v", info, err)
				}
			}
		}
		if c.Path == "systemctl" && c.Args[1] == "daemon-reload" {
			for _, app := range []string{"auth", "dummy"} {
				if info, err := os.Stat(filepath.Join(f.data, "apps", app, "state")); err != nil || !info.IsDir() {
					t.Fatalf("app state: %v", err)
				}
				want := string(renderAppEnv(f.data, "wip", 7400, appInfo{Name: app}))
				if upRead(t, filepath.Join(f.data, "env", app+".env")) != want {
					t.Fatal("deployed env mismatch")
				}
				if upRead(t, filepath.Join(f.units, "sandbox-wip-"+app+".socket")) != string(renderAppSocket(registryEntry{Name: "wip", Port: 7400}, app, 1000)) {
					t.Fatal("socket content mismatch")
				}
				if upRead(t, filepath.Join(f.units, "sandbox-wip-"+app+".service")) != string(renderAppService(paths{state: f.state, config: f.config, root: filepath.Join(f.state, "ikigenba", "sandbox"), units: f.units}, registryEntry{Name: "wip", Port: 7400}, app)) {
					t.Fatal("service mismatch")
				}
			}
			if upRead(t, filepath.Join(f.data, "services.json")) != string(renderServices("wip", 7400, 1000, []appInfo{{Name: "auth"}, {Name: "dummy"}})) {
				t.Fatal("services mismatch")
			}
			if upRead(t, filepath.Join(f.units, "sandbox-wip-nginx.service")) != string(renderNginxUnit(f.data, "wip")) {
				t.Fatal("nginx unit mismatch")
			}
			if upRead(t, filepath.Join(f.data, "nginx", "nginx.conf")) != string(renderNginxConfig(f.data, f.worktree, "wip", 7400, 1000, []appInfo{{Name: "auth"}, {Name: "dummy"}})) {
				t.Fatal("nginx config mismatch")
			}
		}
		return f.answer(c)
	}
	code, out, diagnostic := f.run("up")
	if code != 0 || diagnostic != "" || out != "auth   http://auth.wip.localhost:7400\ndummy  http://dummy.wip.localhost:7400\n" {
		t.Fatalf("%d %q %q", code, out, diagnostic)
	}
	want := []seam.Cmd{{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: f.worktree}}
	for _, app := range []string{"auth", "dummy"} {
		want = append(want, seam.Cmd{Path: "go", Args: []string{"build", "-o", filepath.Join(f.data, "stage", "bin", app), "./cmd/" + app}, Dir: filepath.Join(f.worktree, app)})
	}
	want = append(want, seam.Cmd{Path: "nginx", Args: []string{"-t", "-p", filepath.Join(f.data, "nginx"), "-c", filepath.Join(f.data, "stage", "nginx", "nginx.conf"), "-e", "/dev/null"}, Dir: "/"}, seam.Cmd{Path: "systemctl", Args: []string{"--user", "daemon-reload"}, Dir: "/"}, seam.Cmd{Path: "systemctl", Args: []string{"--user", "show", "--property=ActiveState", "--value", "sandbox-wip-nginx.service"}, Dir: "/"})
	for _, unit := range []string{"auth.socket", "dummy.socket", "auth.service", "dummy.service", "nginx.service"} {
		verb := "start"
		if strings.HasSuffix(unit, ".service") && unit != "nginx.service" {
			verb = "restart"
		}
		want = append(want, seam.Cmd{Path: "systemctl", Args: []string{"--user", verb, "sandbox-wip-" + unit}, Dir: "/"})
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("runs\n%+v\nwant\n%+v", f.calls, want)
	}
	upMissing(t, filepath.Join(f.data, "stage"))
	for _, dir := range []string{"bin", "env"} {
		entries, err := os.ReadDir(filepath.Join(f.data, dir))
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		want := []string{"auth", "dummy"}
		if dir == "env" {
			want = []string{"auth.env", "dummy.env"}
		}
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("%s: %v", dir, names)
		}
	}
	if got := f.records()[0].Apps; !reflect.DeepEqual(got, []registryApp{{Name: "auth"}, {Name: "dummy"}}) {
		t.Fatalf("recorded apps: %+v", got)
	}
}

func TestUpWithoutAuthAndReload(t *testing.T) {
	// R-RJ4B-DRTH R-Z6GK-4S5E R-XZS2-QT73 R-YZ55-U5P8 R-Z0D2-7XFX R-RYZ0-CSGI
	f := newUpFixture(t, "dummy")
	code, out, diagnostic := f.run("up")
	if code != 0 || diagnostic != "" || out != "dummy  http://dummy.wip.localhost:7400\n" {
		t.Fatalf("%d %q %q", code, out, diagnostic)
	}
	f.active = true
	f.calls = nil
	f.put(filepath.Join(f.data, "apps", "dummy", "state", "auth.db"), "persistent", 0600)
	other := filepath.Join(f.state, "ikigenba", "sandbox", "other")
	f.put(filepath.Join(other, "token"), "other", 0600)
	before := upSnapshot(t, other)
	oldLink := filepath.Join(f.root, "old-binary")
	if err := os.Link(filepath.Join(f.data, "bin", "dummy"), oldLink); err != nil {
		t.Fatal(err)
	}
	f.put(filepath.Join(f.data, "bin", "dummy"), "old-binary", 0755)
	code, _, diagnostic = f.run("up")
	if code != 0 || diagnostic != "" {
		t.Fatalf("%d %q", code, diagnostic)
	}
	if upRead(t, oldLink) != "old-binary" || upRead(t, filepath.Join(f.data, "bin", "dummy")) != "new:dummy" {
		t.Fatal("binary not replaced by rename")
	}
	if upRead(t, filepath.Join(f.data, "apps", "dummy", "state", "auth.db")) != "persistent" {
		t.Fatal("state changed")
	}
	if !reflect.DeepEqual(upSnapshot(t, other), before) {
		t.Fatal("other sandbox changed")
	}
	want := []seam.Cmd{{Path: "systemctl", Args: []string{"--user", "start", "sandbox-wip-dummy.socket"}, Dir: "/"}, {Path: "systemctl", Args: []string{"--user", "restart", "sandbox-wip-dummy.service"}, Dir: "/"}, {Path: "systemctl", Args: []string{"--user", "reload", "sandbox-wip-nginx.service"}, Dir: "/"}}
	if !reflect.DeepEqual(f.calls[len(f.calls)-3:], want) {
		t.Fatalf("redeploy runs: %+v", f.calls)
	}
}

func TestUpAppUnitText(t *testing.T) {
	// R-XNFA-ISXR R-XM7E-5172
	for _, state := range []string{"/home/me/.local/state", "/tmp/a b%c$d"} {
		p := paths{root: filepath.Join(state, "ikigenba", "sandbox")}
		entry := registryEntry{Name: "wip", Port: 7400}
		wantSocket := "[Unit]\nDescription=sandbox wip: dummy socket\n\n[Socket]\nListenStream=/run/user/1000/sandbox/7400/dummy.sock\nRemoveOnStop=yes\n"
		if string(renderAppSocket(entry, "dummy", 1000)) != wantSocket {
			t.Fatal("socket text")
		}
		data := strings.ReplaceAll(filepath.Join(state, "ikigenba", "sandbox", "wip"), "%", "%%")
		wantService := "[Unit]\nDescription=sandbox wip: dummy\nRequires=sandbox-wip-dummy.socket\nAfter=sandbox-wip-dummy.socket\n\n[Service]\nType=notify\nExecStart=\"" + data + "/bin/dummy\"\nWorkingDirectory=" + data + "/apps/dummy\nEnvironmentFile=" + data + "/env/dummy.env\nTimeoutStopSec=10\n"
		if got := string(renderAppService(p, entry, "dummy")); got != wantService {
			t.Fatalf("%q want %q", got, wantService)
		}
	}
}

func TestURLListingAndCommand(t *testing.T) {
	// R-ZHFN-KPTN R-IMAP-C5OZ R-ZL3C-Q11Q R-ZMB9-3SSF R-ZNJ5-HKJ4
	for _, apps := range [][]registryApp{{{Name: "auth"}, {Name: "dummy"}}, {{Name: "auth"}, {Name: "dummy", Default: true}}, {{Name: "auth"}}} {
		f := newUpFixture(t, "auth")
		f.registry(apps...)
		f.active = true
		want := "auth   http://auth.wip.localhost:7400\ndummy  http://dummy.wip.localhost:7400\n"
		if len(apps) == 1 {
			want = "auth  http://auth.wip.localhost:7400\n"
		} else if apps[1].Default {
			want += "dummy  http://wip.localhost:7400\n"
		}
		code, out, diagnostic := f.run("url")
		if code != 0 || out != want || diagnostic != "" {
			t.Fatalf("%d %q %q", code, out, diagnostic)
		}
		if len(f.calls) != 2 || f.calls[1].Path != "systemctl" || !reflect.DeepEqual(f.calls[1].Args, []string{"--user", "show", "--property=ActiveState", "--value", "sandbox-wip-nginx.service"}) {
			t.Fatalf("url runs %+v", f.calls)
		}
		f.active = false
		code, out, diagnostic = f.run("url")
		if code != 2 || out != "" || diagnostic != "sandbox: sandbox 'wip' is down\n\nrun 'sandbox up' to start it\n" {
			t.Fatalf("down url %d %q %q", code, out, diagnostic)
		}
		for _, runnerErr := range []bool{false, true} {
			f.exec = func(c seam.Cmd) (seam.Result, error) {
				if c.Path == "systemctl" {
					if runnerErr {
						return seam.Result{}, errors.New("runner failed")
					}
					return seam.Result{ExitCode: 1, Output: []byte("failed\n")}, nil
				}
				return f.answer(c)
			}
			code, out, diagnostic = f.run("url")
			want := "sandbox: systemctl --user: exit status 1\n\n> failed\n"
			if runnerErr {
				want = "sandbox: systemctl --user: runner failed\n"
			}
			if code != 1 || out != "" || diagnostic != want {
				t.Fatalf("failed url %d %q %q", code, out, diagnostic)
			}
		}
	}
}

func upStable(t *testing.T, f *upFixture, ignoreNginx bool) map[string]string {
	t.Helper()
	got := upSnapshot(t, f.data)
	for key := range got {
		if key == "stage" || strings.HasPrefix(key, "stage/") || (ignoreNginx && (key == "nginx" || strings.HasPrefix(key, "nginx/"))) {
			delete(got, key)
		}
	}
	for key, value := range upSnapshot(t, f.units) {
		got["units/"+key] = value
	}
	got["registry"] = upRead(t, filepath.Join(f.state, "ikigenba", "sandbox", "registry.json"))
	return got
}

func TestUpBuildFailureIsolation(t *testing.T) {
	// R-RMS0-J31K R-Y9J9-SZ4N R-YAR6-6QVC R-YBZ2-KIM1 R-RNZW-WUS9 R-RP7T-AMIY
	for _, known := range []bool{false, true} {
		for _, runnerErr := range []bool{false, true} {
			t.Run(fmt.Sprintf("known%t/error%t", known, runnerErr), func(t *testing.T) {
				f := newUpFixture(t, "auth", "dummy")
				if known {
					code, _, diagnostic := f.run("up")
					if code != 0 {
						t.Fatal(diagnostic)
					}
					f.active = true
					f.put(filepath.Join(f.data, "apps", "auth", "state", "auth.db"), "original", 0600)
				}
				f.put(filepath.Join(f.data, "stage", "old"), "stale", 0644)
				f.calls = nil
				var before map[string]string
				if known {
					before = upStable(t, f, false)
				}
				builds := 0
				f.exec = func(c seam.Cmd) (seam.Result, error) {
					if c.Path == "go" {
						builds++
						if builds == 1 && !known {
							before = upStable(t, f, false)
						} else if !reflect.DeepEqual(upStable(t, f, false), before) {
							t.Fatal("deployment changed between builds")
						}
						upMissing(t, filepath.Join(f.data, "stage", "old"))
						if builds == 2 {
							if runnerErr {
								return seam.Result{}, errors.New("build runner failed")
							}
							return seam.Result{ExitCode: 1, Output: []byte("# github.com/ikigenba/ikigenba/dummy/cmd/dummy\ncmd/dummy/main.go:41:2: undefined: render\n")}, nil
						}
					}
					return f.answer(c)
				}
				code, out, diagnostic := f.run("up")
				want := "sandbox: build dummy: exit status 1\n\n> # github.com/ikigenba/ikigenba/dummy/cmd/dummy\n> cmd/dummy/main.go:41:2: undefined: render\n"
				if runnerErr {
					want = "sandbox: build dummy: build runner failed\n"
				}
				if code != 1 || out != "" || diagnostic != want {
					t.Fatalf("%d %q %q", code, out, diagnostic)
				}
				if len(f.calls) != 3 || builds != 2 {
					t.Fatalf("runs after failure: %+v", f.calls)
				}
				upMissing(t, filepath.Join(f.data, "stage"))
				after := upStable(t, f, false)
				// The sandbox's empty parent directory may be removed during staging cleanup.
				delete(before, ".")
				delete(after, ".")
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("failed build changed deployment: before %+v after %+v", before, after)
				}
				if !known && len(f.records()[0].Apps) != 0 {
					t.Fatal("failed first up recorded apps")
				}
			})
		}
	}
}

func TestUpConfigurationRunUsesUnquotedPaths(t *testing.T) {
	// R-RHWF-002S
	p := paths{root: "/tmp/a b%c$d/ikigenba/sandbox"}
	want := seam.Cmd{Path: "nginx", Args: []string{"-t", "-p", "/tmp/a b%c$d/ikigenba/sandbox/wip/nginx", "-c", "/tmp/a b%c$d/ikigenba/sandbox/wip/stage/nginx/nginx.conf", "-e", "/dev/null"}, Dir: "/"}
	called := false
	i := invocation{ctx: context.Background(), stdout: io.Discard, stderr: io.Discard, deps: seam.Deps{Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		called = true
		if !reflect.DeepEqual(c, want) {
			t.Fatalf("configuration run %+v", c)
		}
		return seam.Result{}, nil
	}}}
	if code := i.upExec(upNginxTestCommand(p, "wip"), "test nginx configuration", ""); code != 0 || !called {
		t.Fatalf("code %d called %t", code, called)
	}
}

func TestUpConfigurationRefusalIsolation(t *testing.T) {
	// R-RSVI-FXR1 R-RU3E-TPHQ R-RVBB-7H8F R-X4D8-ZUG2
	for _, state := range []string{"new", "absent", "empty", "existing"} {
		for _, runnerErr := range []bool{false, true} {
			t.Run(state+fmt.Sprint(runnerErr), func(t *testing.T) {
				f := newUpFixture(t, "auth", "dummy")
				if state != "new" {
					f.registry(registryApp{Name: "auth"}, registryApp{Name: "dummy"})
				}
				if state == "existing" {
					code, _, diagnostic := f.run("up")
					if code != 0 {
						t.Fatal(diagnostic)
					}
					f.active = true
					f.put(filepath.Join(f.data, "nginx", "nginx.pid"), "existing pid", 0600)
					if err := os.MkdirAll(filepath.Join(f.data, "nginx", "client_body"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				if state == "empty" {
					if err := os.MkdirAll(filepath.Join(f.data, "nginx"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				if state == "existing" {
					if err := os.RemoveAll(filepath.Join(f.worktree, "dummy")); err != nil {
						t.Fatal(err)
					}
					f.put(filepath.Join(f.worktree, "extra", "etc", "manifest.toml"), "app = \"extra\"\n", 0644)
				}
				f.calls = nil
				var stable map[string]string
				originalNginx := upSnapshot(t, filepath.Join(f.data, "nginx"))
				f.exec = func(c seam.Cmd) (seam.Result, error) {
					if c.Path == "go" && stable == nil {
						stable = upStable(t, f, true)
					}
					if c.Path == "nginx" {
						nginxBeforeFake := upSnapshot(t, filepath.Join(f.data, "nginx"))
						wantNginxBefore := originalNginx
						if state == "new" || state == "absent" {
							wantNginxBefore = map[string]string{".": "directory"}
						}
						if !reflect.DeepEqual(nginxBeforeFake, wantNginxBefore) {
							t.Fatalf("nginx changed before test: got %+v want %+v", nginxBeforeFake, wantNginxBefore)
						}
						if state == "existing" {
							// R-RRNM-260C
							config := upRead(t, filepath.Join(f.data, "stage", "nginx", "nginx.conf"))
							want := string(renderNginxConfig(f.data, f.worktree, "wip", 7400, 1000, []appInfo{{Name: "auth"}, {Name: "extra"}}))
							if config != want {
								t.Fatal("existing sandbox staged configuration mismatch")
							}
						}
						atTest := upStable(t, f, true)
						if !reflect.DeepEqual(atTest, stable) {
							t.Fatalf("changed before config test: %+v %+v", stable, atTest)
						}
						if state == "new" || state == "absent" {
							f.put(filepath.Join(f.data, "nginx", "nginx.pid"), "created pid", 0600)
						}
						if state == "existing" {
							if err := os.MkdirAll(filepath.Join(f.data, "nginx", "proxy"), 0700); err != nil {
								t.Fatal(err)
							}
						}
						if runnerErr {
							return seam.Result{}, errors.New("nginx runner failed")
						}
						return seam.Result{ExitCode: 1, Output: []byte("a\nb\n")}, nil
					}
					return f.answer(c)
				}
				code, out, diagnostic := f.run("up")
				want := "sandbox: test nginx configuration: exit status 1\n\n> a\n> b\n"
				if runnerErr {
					want = "sandbox: test nginx configuration: nginx runner failed\n"
				}
				if code != 1 || out != "" || diagnostic != want {
					t.Fatalf("%d %q %q", code, out, diagnostic)
				}
				if len(f.calls) != 4 || f.calls[3].Path != "nginx" {
					t.Fatalf("runs %+v", f.calls)
				}
				upMissing(t, filepath.Join(f.data, "stage"))
				after := upStable(t, f, true)
				delete(after, ".")
				delete(stable, ".")
				if !reflect.DeepEqual(after, stable) {
					t.Fatalf("changed after refusal: %+v %+v", after, stable)
				}
				if state == "new" || state == "absent" {
					upMissing(t, filepath.Join(f.data, "nginx"))
				} else {
					got := upSnapshot(t, filepath.Join(f.data, "nginx"))
					if state == "existing" {
						delete(got, "proxy")
					}
					if !reflect.DeepEqual(got, originalNginx) {
						t.Fatalf("original nginx changed %+v %+v", got, originalNginx)
					}
				}
			})
		}
	}
}

func TestUpPostConfigurationProgramFailures(t *testing.T) {
	// R-Z8WC-WBMS R-ZA49-A3DH R-ZBC5-NV46 R-ZCK2-1MUV R-ZDRY-FELK R-ZEZU-T6C9 R-S3UL-VVFA
	// R-Y39Z-HTKS
	for _, step := range []string{"daemon-reload", "show", "dummy.socket", "dummy.service", "nginx.start", "nginx.reload"} {
		for _, runnerErr := range []bool{false, true} {
			t.Run(step+fmt.Sprint(runnerErr), func(t *testing.T) {
				f := newUpFixture(t, "auth", "dummy")
				f.active = step == "nginx.reload"
				var atFailure map[string]string
				failureIndex := 0
				f.exec = func(c seam.Cmd) (seam.Result, error) {
					fail := c.Path == "systemctl" && c.Args[1] == step
					if c.Path == "systemctl" && len(c.Args) == 3 {
						fail = fail || strings.HasSuffix(c.Args[2], "-"+step) || ((step == "nginx.start" || step == "nginx.reload") && c.Args[2] == "sandbox-wip-nginx.service")
					}
					if fail {
						failureIndex = len(f.calls)
						atFailure = upStable(t, f, false)
						if runnerErr {
							return seam.Result{}, errors.New("runner failed")
						}
						output := "Failed to connect to bus: No medium found\n"
						if step == "dummy.service" {
							output = "failed\n"
						}
						return seam.Result{ExitCode: 1, Output: []byte(output)}, nil
					}
					return f.answer(c)
				}
				code, out, diagnostic := f.run("up")
				action := "systemctl --user " + step
				detail := ""
				if step == "show" {
					action = "systemctl --user"
				}
				if step == "dummy.socket" || step == "dummy.service" {
					action = "start sandbox-wip-" + step
					detail = "run 'sandbox logs dummy' for its journal"
				}
				if strings.HasPrefix(step, "nginx.") {
					action = strings.TrimPrefix(step, "nginx.") + " sandbox-wip-nginx.service"
					detail = "run 'sandbox logs' for the journal"
				}
				want := "sandbox: " + action + ": exit status 1\n\n> Failed to connect to bus: No medium found\n"
				if step == "dummy.service" {
					want = "sandbox: " + action + ": exit status 1\n\n> failed\n"
				}
				if detail != "" {
					want += "\n" + detail + "\n"
				}
				if runnerErr {
					want = "sandbox: " + action + ": runner failed\n"
				}
				if code != 1 || out != "" || diagnostic != want {
					t.Fatalf("%d %q %q want %q", code, out, diagnostic, want)
				}
				if atFailure == nil {
					t.Fatal("failure runner not reached")
				}
				if len(f.calls) != failureIndex {
					t.Fatalf("runs after failure: %+v", f.calls)
				}
				last := f.calls[len(f.calls)-1]
				if last.Path != "systemctl" {
					t.Fatalf("last command %+v", last)
				}
				upMissing(t, filepath.Join(f.data, "stage"))
				if !reflect.DeepEqual(upStable(t, f, false), atFailure) {
					t.Fatal("deployed files changed after program failed")
				}
			})
		}
	}
}

func TestUpIgnoresFragmentContents(t *testing.T) {
	// R-54NM-Q9H2
	f := newUpFixture(t, "dummy")
	code, out, diagnostic := f.run("up")
	if code != 0 || diagnostic != "" {
		t.Fatalf("%d %q", code, diagnostic)
	}
	before := upRead(t, filepath.Join(f.data, "nginx", "nginx.conf"))
	f.put(filepath.Join(f.worktree, "dummy", "etc", "nginx.conf"), "server {", 0644)
	f.put(filepath.Join(f.worktree, "dummy", "etc", "nginx.conf.bad"), "unreadable", 0000)
	if err := os.Mkdir(filepath.Join(f.worktree, "dummy", "etc", "nginx.conf.d"), 0000); err != nil {
		t.Fatal(err)
	}
	code, afterOut, diagnostic := f.run("up")
	if code != 0 || diagnostic != "" || afterOut != out {
		t.Fatalf("%d %q %q", code, afterOut, diagnostic)
	}
	if upRead(t, filepath.Join(f.data, "nginx", "nginx.conf")) != before {
		t.Fatal("fragment contents altered generated config")
	}
}

func TestUpCheckOrderAndRefusalIsolation(t *testing.T) {
	// R-RKC7-RJK6 R-RLK4-5BAV
	for _, known := range []bool{false, true} {
		for _, fault := range []string{"noapps", "manifest", "icon", "secrets", "clash", "ports"} {
			if known && fault == "ports" {
				continue
			}
			t.Run(fmt.Sprintf("%t/%s", known, fault), func(t *testing.T) {
				f := newUpFixture(t, "b-c")
				f.worktree = filepath.Join(f.root, "a")
				if err := os.Rename(filepath.Join(f.root, "wip"), f.worktree); err != nil {
					t.Fatal(err)
				}
				f.data = filepath.Join(f.state, "ikigenba", "sandbox", "a")
				reg := registry{Sandboxes: []registryEntry{}}
				if known {
					reg.Sandboxes = append(reg.Sandboxes, registryEntry{Name: "a", Port: 7400, Worktree: f.worktree, Apps: []registryApp{{Name: "b-c"}}})
					f.put(filepath.Join(f.data, "bin", "b-c"), "previous", 0755)
					f.put(filepath.Join(f.units, "sandbox-a-b-c.service"), "previous", 0644)
					f.active = true
				}
				// Later failures coexist so only the earliest refusal can be reported.
				if fault == "noapps" || fault == "clash" || fault == "ports" {
					for port := 7400; port <= 7499; port++ {
						if known && port == 7400 {
							continue
						}
						reg.Sandboxes = append(reg.Sandboxes, registryEntry{Name: fmt.Sprintf("held%d", port), Port: port, Worktree: "/unused", Apps: []registryApp{}})
					}
				}
				if fault != "ports" {
					port := 7499
					if fault == "clash" || fault == "noapps" {
						for n := range reg.Sandboxes {
							if reg.Sandboxes[n].Port == 7499 {
								reg.Sandboxes = append(reg.Sandboxes[:n], reg.Sandboxes[n+1:]...)
								break
							}
						}
					}
					reg.Sandboxes = append(reg.Sandboxes, registryEntry{Name: "a-b", Port: port, Worktree: "/other", Apps: []registryApp{{Name: "c"}}})
				}
				if len(reg.Sandboxes) > 0 {
					b, err := json.Marshal(reg)
					if err != nil {
						t.Fatal(err)
					}
					f.put(filepath.Join(f.state, "ikigenba", "sandbox", "registry.json"), string(b), 0600)
				}
				manifest := filepath.Join(f.worktree, "b-c", "etc", "manifest.toml")
				want := ""
				switch fault {
				case "noapps":
					if err := os.Remove(manifest); err != nil {
						t.Fatal(err)
					}
					want = "sandbox: no apps in " + f.worktree + "\n\nan app is a directory holding etc/manifest.toml\n"
				case "manifest":
					f.put(manifest, "app = \"b-c\"\nport = 1\nsecrets = [\"MISSING\"]\n", 0644)
					want = "sandbox: b-c: etc/manifest.toml: 'port' is not allowed; the sandbox gives the app its socket\n"
				case "icon":
					f.put(manifest, "app = \"b-c\"\nsecrets = [\"MISSING\"]\n", 0644)
					f.put(filepath.Join(f.worktree, "b-c", "share", "icon.svg"), "<svg/>", 0000)
					want = "sandbox: b-c: share/icon.svg: permission denied\n"
				case "secrets":
					f.put(manifest, "app = \"b-c\"\nsecrets = [\"MISSING\"]\n", 0644)
					want = "sandbox: secrets missing from " + filepath.Join(f.config, "ikigenba", "sandbox", "secrets.toml") + "\n\nb-c MISSING\n"
				case "clash":
					want = "sandbox: unit 'sandbox-a-b-c.service' would also belong to sandbox 'a-b'\n\nrename this worktree or the app, or wipe sandbox 'a-b' once it is down\n"
				case "ports":
					want = "sandbox: no free port: every port from 7400 to 7499 belongs to a sandbox\n\nrun 'sandbox ls' and wipe a sandbox you no longer need\n"
				}
				beforeData := upSnapshot(t, f.data)
				beforeUnits := upSnapshot(t, f.units)
				registryPath := filepath.Join(f.state, "ikigenba", "sandbox", "registry.json")
				beforeRegistry, registryErr := upReadBytes(registryPath)
				code, out, diagnostic := f.run("up")
				if code != 2 || out != "" || diagnostic != want {
					t.Fatalf("%d %q %q want %q", code, out, diagnostic, want)
				}
				if len(f.calls) != 1 || f.calls[0].Path != "git" {
					t.Fatalf("checks ran programs %+v", f.calls)
				}
				if !reflect.DeepEqual(upSnapshot(t, f.data), beforeData) || !reflect.DeepEqual(upSnapshot(t, f.units), beforeUnits) {
					t.Fatal("checks changed deployed files")
				}
				afterRegistry, afterErr := upReadBytes(registryPath)
				if os.IsNotExist(registryErr) {
					if !os.IsNotExist(afterErr) {
						t.Fatal("checks created registry")
					}
				} else if afterErr != nil || !bytes.Equal(beforeRegistry, afterRegistry) {
					t.Fatal("checks changed registry")
				}
				root := filepath.Join(f.state, "ikigenba", "sandbox")
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					switch entry.Name() {
					case "a", "a.lock", "registry.json", "registry.json.lock":
					default:
						t.Fatalf("check created unexpected entry %s", entry.Name())
					}
				}
			})
		}
	}
}

func TestUpRemovedApps(t *testing.T) {
	// R-YI2K-HDBI R-S06W-QK77 R-YKID-8WSW R-YLQ9-MOJL R-YMY6-0GAA
	for _, units := range []bool{false, true} {
		for _, fail := range []string{"", "exit", "runner"} {
			if !units && fail != "" {
				continue
			}
			t.Run(fmt.Sprintf("%t/%s", units, fail), func(t *testing.T) {
				f := newUpFixture(t, "auth", "extra")
				f.registry(registryApp{Name: "auth"}, registryApp{Name: "dummy"})
				f.active = true
				f.put(filepath.Join(f.data, "apps", "dummy", "state", "db"), "keep", 0600)
				f.put(filepath.Join(f.data, "bin", "dummy"), "old", 0755)
				f.put(filepath.Join(f.data, "env", "dummy.env"), "old", 0600)
				// An unrecorded app directory is kept and never considered a removed app.
				f.put(filepath.Join(f.data, "apps", "unrecorded", "state", "db"), "also keep", 0600)
				if units {
					for _, suffix := range []string{"socket", "service"} {
						f.put(filepath.Join(f.units, "sandbox-wip-dummy."+suffix), "old", 0644)
					}
				}
				kept := upSnapshot(t, filepath.Join(f.data, "apps"))
				var atFailure map[string]string
				failureIndex := 0
				f.exec = func(c seam.Cmd) (seam.Result, error) {
					if c.Path == "systemctl" && c.Args[1] == "stop" {
						if got := f.records()[0].Apps; !reflect.DeepEqual(got, []registryApp{{Name: "auth"}, {Name: "dummy"}}) {
							t.Fatalf("registry changed before stop %+v", got)
						}
						if fail != "" {
							failureIndex = len(f.calls)
							atFailure = upStable(t, f, false)
							if fail == "runner" {
								return seam.Result{}, errors.New("stop runner failed")
							}
							return seam.Result{ExitCode: 1, Output: []byte("stop failed\n")}, nil
						}
					}
					return f.answer(c)
				}
				code, out, diagnostic := f.run("up")
				if fail != "" {
					want := "sandbox: stop sandbox-wip-dummy.socket: exit status 1\n\n> stop failed\n"
					if fail == "runner" {
						want = "sandbox: stop sandbox-wip-dummy.socket: stop runner failed\n"
					}
					if code != 1 || out != "" || diagnostic != want {
						t.Fatalf("%d %q %q", code, out, diagnostic)
					}
					if !reflect.DeepEqual(upStable(t, f, false), atFailure) {
						t.Fatal("stop failure changed files")
					}
					if len(f.calls) != failureIndex {
						t.Fatalf("runs after stop failure: %+v", f.calls)
					}
					return
				}
				if code != 0 || diagnostic != "" {
					t.Fatalf("%d %q", code, diagnostic)
				}
				stops := []string{}
				reloadSeen := false
				for _, c := range f.calls {
					if c.Path == "systemctl" && c.Args[1] == "daemon-reload" {
						reloadSeen = true
					}
					if c.Path == "systemctl" && c.Args[1] == "stop" {
						if reloadSeen {
							t.Fatal("stop after daemon-reload")
						}
						stops = append(stops, c.Args[2])
					}
				}
				want := []string{}
				if units {
					want = []string{"sandbox-wip-dummy.socket", "sandbox-wip-dummy.service"}
				}
				if !reflect.DeepEqual(stops, want) {
					t.Fatalf("stops %v", stops)
				}
				for _, path := range []string{filepath.Join(f.units, "sandbox-wip-dummy.socket"), filepath.Join(f.units, "sandbox-wip-dummy.service"), filepath.Join(f.data, "bin", "dummy"), filepath.Join(f.data, "env", "dummy.env")} {
					upMissing(t, path)
				}
				for key, value := range kept {
					if upSnapshot(t, filepath.Join(f.data, "apps"))[key] != value {
						t.Fatalf("kept app changed %s", key)
					}
				}
			})
		}
	}
}

func TestUpRecordsAppsBeforeInstallingFiles(t *testing.T) {
	// R-X5L5-DM6R R-Z1KY-LP6M
	for _, fault := range []string{"socket", "bin", "env", "services", "app", "nginx", "nginxunit", "readonly"} {
		t.Run(fault, func(t *testing.T) {
			f := newUpFixture(t, "auth")
			code, _, diagnostic := f.run("up")
			if code != 0 {
				t.Fatal(diagnostic)
			}
			f.put(filepath.Join(f.worktree, "dummy", "etc", "manifest.toml"), "app = \"dummy\"\n", 0644)
			path := ""
			isFile := false
			switch fault {
			case "socket":
				path = filepath.Join(f.units, "sandbox-wip-dummy.socket")
			case "bin":
				path = filepath.Join(f.data, "bin")
				isFile = true
			case "env":
				path = filepath.Join(f.data, "env", "dummy.env")
			case "services":
				path = filepath.Join(f.data, "services.json")
			case "app":
				path = filepath.Join(f.data, "apps", "dummy")
				isFile = true
			case "nginx":
				path = filepath.Join(f.data, "nginx", "nginx.conf")
			case "nginxunit":
				path = filepath.Join(f.units, "sandbox-wip-nginx.service")
			case "readonly":
				path = f.units
			}
			if fault == "readonly" {
				if err := upChmod(path, 0500); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := upChmod(path, 0700); err != nil {
						t.Error(err)
					}
				}()
			} else {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if isFile {
					f.put(path, "obstruction", 0600)
				} else if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			f.calls = nil
			code, out, diagnostic := f.run("up")
			errorPath := path
			reason := syscall.EISDIR.Error()
			if fault == "bin" {
				reason = syscall.ENOTDIR.Error()
			}
			if fault == "app" {
				errorPath = filepath.Join(path, "state")
				reason = syscall.ENOTDIR.Error()
			}
			if fault == "readonly" {
				errorPath = filepath.Join(path, "sandbox-wip-dummy.service")
				reason = syscall.EACCES.Error()
			}
			wantDiagnostic := "sandbox: " + errorPath + ": " + reason + "\n"
			if code != 1 || out != "" || diagnostic != wantDiagnostic {
				t.Fatalf("%d %q %q want %q", code, out, diagnostic, wantDiagnostic)
			}
			if !reflect.DeepEqual(f.records()[0].Apps, []registryApp{{Name: "auth"}, {Name: "dummy"}}) {
				t.Fatalf("registry before file failure %+v", f.records())
			}
			for _, c := range f.calls {
				if c.Path == "systemctl" {
					t.Fatalf("run after file failure %+v", c)
				}
			}
			upMissing(t, filepath.Join(f.data, "stage"))
		})
	}
}

func TestUpGatedManifestMCPVariants(t *testing.T) {
	// R-RVPU-1JS5
	for _, setting := range []string{"mcp = true\n", "mcp = false\n", ""} {
		t.Run(strings.TrimSpace(setting), func(t *testing.T) {
			f := newUpFixture(t, "auth", "dummy")
			f.put(filepath.Join(f.worktree, "dummy", "etc", "manifest.toml"), "app = \"dummy\"\ndescription = \"Demo\"\n"+setting, 0644)
			code, _, diagnostic := f.run("up")
			if code != 0 || diagnostic != "" {
				t.Fatalf("%d %q", code, diagnostic)
			}
			http := routingFind(t, parseRoutingConfig(t, upRead(t, filepath.Join(f.data, "nginx", "nginx.conf"))), "http")
			server := routingServer(t, http, "dummy.wip.localhost")
			for _, words := range [][]string{{"location", "/"}, {"location", "=", "/mcp"}, {"location", "^~", "/mcp/"}} {
				location := routingFind(t, server.children, words...)
				routingKeys(t, location.children, "auth_request", "auth_request_set", "auth_request_set", "error_page", "proxy_pass", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header")
				routingFind(t, location.children, "auth_request", "/_sandbox/auth")
				routingFind(t, location.children, "auth_request_set", "$sandbox_user_id", "$upstream_http_x_user_id")
				routingFind(t, location.children, "auth_request_set", "$sandbox_user_email", "$upstream_http_x_user_email")
				routingFind(t, location.children, "proxy_pass", "http://app_dummy")
				routingHeaders(t, location, "$sandbox_user_id", "$sandbox_user_email")
			}
		})
	}
}

func TestUpCheckRefusalsLeaveAbsentRegistry(t *testing.T) {
	// R-RLK4-5BAV
	for _, fault := range []string{"noapps", "manifest", "icon", "secrets"} {
		t.Run(fault, func(t *testing.T) {
			f := newUpFixture(t, "dummy")
			manifest := filepath.Join(f.worktree, "dummy", "etc", "manifest.toml")
			switch fault {
			case "noapps":
				if err := os.Remove(manifest); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				f.put(manifest, "app = 1\n", 0644)
			case "icon":
				f.put(filepath.Join(f.worktree, "dummy", "share", "icon.svg"), "<svg/>", 0000)
			case "secrets":
				f.put(manifest, "app = \"dummy\"\nsecrets = [\"MISSING\"]\n", 0644)
			}
			code, out, diagnostic := f.run("up")
			if code != 2 || out != "" || diagnostic == "" {
				t.Fatalf("%d %q %q", code, out, diagnostic)
			}
			if len(f.calls) != 1 || f.calls[0].Path != "git" {
				t.Fatalf("check runs %+v", f.calls)
			}
			upMissing(t, filepath.Join(f.state, "ikigenba", "sandbox", "registry.json"))
			upMissing(t, f.data)
			upMissing(t, f.units)
			entries, err := os.ReadDir(filepath.Join(f.state, "ikigenba", "sandbox"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "wip.lock" {
				t.Fatalf("check left entries %v", entries)
			}
		})
	}
}

func TestUpRemovedAppsSortedFromRegistry(t *testing.T) {
	// R-S06W-QK77
	f := newUpFixture(t, "auth")
	f.registry(registryApp{Name: "zeta"}, registryApp{Name: "dummy"}, registryApp{Name: "auth"})
	for _, app := range []string{"zeta", "dummy"} {
		for _, suffix := range []string{"socket", "service"} {
			f.put(filepath.Join(f.units, "sandbox-wip-"+app+"."+suffix), "old", 0644)
		}
	}
	code, _, diagnostic := f.run("up")
	if code != 0 || diagnostic != "" {
		t.Fatalf("%d %q", code, diagnostic)
	}
	stops := []string{}
	for _, c := range f.calls {
		if c.Path == "systemctl" && c.Args[1] == "stop" {
			stops = append(stops, c.Args[2])
		}
	}
	want := []string{"sandbox-wip-dummy.socket", "sandbox-wip-dummy.service", "sandbox-wip-zeta.socket", "sandbox-wip-zeta.service"}
	if !reflect.DeepEqual(stops, want) {
		t.Fatalf("stop order %v", stops)
	}
}

func TestUpSuccessfulRedeploymentKeepsAuthState(t *testing.T) {
	// R-YZ55-U5P8
	f := newUpFixture(t, "auth", "dummy")
	code, _, diagnostic := f.run("up")
	if code != 0 || diagnostic != "" {
		t.Fatalf("%d %q", code, diagnostic)
	}
	f.active = true
	path := filepath.Join(f.data, "apps", "auth", "state", "auth.db")
	f.put(path, "original auth state\x00bytes", 0600)
	before := upSnapshot(t, filepath.Join(f.data, "apps"))
	code, _, diagnostic = f.run("up")
	if code != 0 || diagnostic != "" {
		t.Fatalf("%d %q", code, diagnostic)
	}
	if got := upSnapshot(t, filepath.Join(f.data, "apps")); !reflect.DeepEqual(got, before) {
		t.Fatalf("existing app entries changed: got %+v want %+v", got, before)
	}
	if upRead(t, path) != "original auth state\x00bytes" {
		t.Fatal("auth state bytes changed")
	}
}

func TestUpAppRecordUpdatePreservesUnrelatedRegistryEntries(t *testing.T) {
	// R-S1ET-4BXW
	f := newUpFixture(t, "auth", "dummy")
	f.put(filepath.Join(f.worktree, "dummy", "etc", "manifest.toml"), "app = \"dummy\"\ndefault = true\n", 0644)
	p := paths{state: f.state, config: f.config, root: filepath.Join(f.state, "ikigenba", "sandbox"), units: f.units}
	untouched := registryEntry{Name: "untouched", Port: 7498, Worktree: "/untouched", Apps: []registryApp{{Name: "widget", Default: true}}}
	updated := registryEntry{Name: "updated", Port: 7499, Worktree: "/updated", Apps: []registryApp{{Name: "old"}}}
	reg := registry{Sandboxes: []registryEntry{{Name: "wip", Port: 7400, Worktree: f.worktree, Apps: []registryApp{{Name: "auth"}}}, untouched, updated}}
	b, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	f.put(p.registryPath(), string(b), 0600)
	f.active = true
	updateMade := false
	added := registryEntry{Name: "added", Port: 7497, Worktree: "/added", Apps: []registryApp{{Name: "new", Default: true}}}
	updated.Apps = []registryApp{{Name: "replacement", Default: true}}
	f.exec = func(c seam.Cmd) (seam.Result, error) {
		if c.Path == "nginx" {
			// Another deployment completes its registry update while this one's
			// staging has finished and its app-record update has not begun.
			finished := make(chan error, 1)
			go func() {
				finished <- withRegistry(p, func(reg *registry) error {
					for n := range reg.Sandboxes {
						if reg.Sandboxes[n].Name == updated.Name {
							reg.Sandboxes[n] = updated
						}
					}
					reg.Sandboxes = append(reg.Sandboxes, added)
					return nil
				})
			}()
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			updateMade = true
		}
		return f.answer(c)
	}
	code, out, diagnostic := f.run("up")
	wantOut := "auth   http://auth.wip.localhost:7400\ndummy  http://dummy.wip.localhost:7400\ndummy  http://wip.localhost:7400\n"
	if code != 0 || out != wantOut || diagnostic != "" || !updateMade {
		t.Fatalf("%d %q %q update %t", code, out, diagnostic, updateMade)
	}
	want := map[string]registryEntry{"wip": {Name: "wip", Port: 7400, Worktree: f.worktree, Apps: []registryApp{{Name: "auth"}, {Name: "dummy", Default: true}}}, untouched.Name: untouched, updated.Name: updated, added.Name: added}
	got := map[string]registryEntry{}
	for _, entry := range f.records() {
		got[entry.Name] = entry
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry: got %+v want %+v", got, want)
	}
}
