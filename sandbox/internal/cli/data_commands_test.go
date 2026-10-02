package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

type dcFixture struct {
	t          *testing.T
	base, work string
	p          paths
	env        map[string]string
	calls      []seam.Cmd
	answer     func(seam.Cmd) (seam.Result, error)
}

func dcNew(t *testing.T) *dcFixture {
	t.Helper()
	b := t.TempDir()
	f := &dcFixture{t: t, base: b, work: filepath.Join(b, "wip")}
	f.p = paths{state: filepath.Join(b, "state"), config: filepath.Join(b, "config")}
	f.p.root = filepath.Join(f.p.state, "ikigenba/sandbox")
	f.p.units = filepath.Join(f.p.config, "systemd/user")
	f.env = map[string]string{"HOME": b, "XDG_STATE_HOME": f.p.state, "XDG_CONFIG_HOME": f.p.config}
	return f
}
func (f *dcFixture) deps() seam.Deps {
	return seam.Deps{Dir: f.work, EUID: 1000, Getenv: func(k string) string { return f.env[k] }, Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		f.calls = append(f.calls, c)
		if f.answer != nil {
			return f.answer(c)
		}
		if c.Path == "git" {
			return seam.Result{Stdout: []byte(f.work + "\n")}, nil
		}
		return seam.Result{Stdout: []byte("inactive\n")}, nil
	}, Stream: func(_ context.Context, c seam.Cmd, _ io.Writer) (seam.Result, error) {
		f.calls = append(f.calls, c)
		return seam.Result{}, nil
	}}
}
func (f *dcFixture) run(in io.Reader, args ...string) (int, string, string) {
	f.t.Helper()
	var out, err bytes.Buffer
	code := runChecked(context.Background(), f.t, args, in, &out, &err, f.deps())
	return code, out.String(), err.String()
}
func (f *dcFixture) seed() {
	dataTestRegistry(f.t, f.p, registry{Sandboxes: []registryEntry{{Name: "wip", Port: 7400, Worktree: f.work, Apps: []registryApp{{Name: "auth"}}}}})
}
func dcSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	rootFS, err := os.OpenRoot(filepath.Clean(root))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rootFS.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		i, e := d.Info()
		if e != nil {
			return e
		}
		v := i.Mode().String()
		if !d.IsDir() {
			relative, e := filepath.Rel(root, p)
			if e != nil {
				return e
			}
			b, e := rootFS.ReadFile(relative)
			if e != nil {
				return e
			}
			v += string(b)
		}
		m[p] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func dcWant(t *testing.T, code int, out, err string, wcode int, werr string) {
	t.Helper()
	if code != wcode || out != "" || err != werr {
		t.Fatalf("got (%d,%q,%q), want (%d,empty,%q)", code, out, err, wcode, werr)
	}
}
func dcUnknown(name string, explicit bool) string {
	hint := "run 'sandbox up' to create it"
	if explicit {
		hint = "run 'sandbox ls' to see every sandbox"
	}
	return "sandbox: no sandbox '" + dataPrinted(name, false) + "'\n\n" + hint + "\n"
}

var dcWorkCommands = [][]string{{"up"}, {"url"}, {"status"}, {"logs"}, {"token"}, {"token", "set"}, {"down"}, {"wipe"}}

func TestDataCommandFindingFailures(t *testing.T) {
	// R-EIIF-59MV R-YLKH-8DP7 R-CVT7-DMPU R-LH3U-724E R-LIBQ-KTV3 R-LJCV-IO94
	for _, args := range dcWorkCommands {
		for _, kind := range []string{"empty", "checkout", "runner", "name-empty", "name-long", "newline", "invalid", "invalid-parent", "partial", "valid-control", "valid-utf8"} {
			t.Run(strings.Join(args, "-")+"/"+kind, func(t *testing.T) {
				f := dcNew(t)
				work := f.work
				wantCode := 2
				want := ""
				switch kind {
				case "empty":
					f.work = ""
					want = "sandbox: the current directory no longer exists\n"
				case "checkout":
					f.work = "/tmp/a\nb"
					want = "sandbox: '/tmp/a\\x0ab' is not inside a git checkout\n"
				case "runner":
					wantCode = 1
					want = "sandbox: git rev-parse --show-toplevel: no executable\n"
				case "name-empty":
					work = "/tmp/!!!"
				case "name-long":
					work = "/tmp/" + strings.Repeat("a", 64)
				case "newline":
					work = "/tmp/a\nb"
				case "invalid":
					work = "/tmp/a\xffb"
				case "invalid-parent":
					work = "/tmp/a\xff/b"
				case "partial":
					work = "/tmp/caf\xc3"
				case "valid-control":
					work = "/tmp/x\u0085y"
				case "valid-utf8":
					work = "/tmp/café"
				}
				f.answer = func(c seam.Cmd) (seam.Result, error) {
					if c.Path != "git" {
						t.Fatalf("unexpected %v", c)
					}
					if !reflect.DeepEqual(c, seam.Cmd{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: f.work}) {
						t.Fatalf("git seam %v", c)
					}
					if kind == "runner" {
						return seam.Result{}, errors.New("no executable")
					}
					if kind == "checkout" {
						return seam.Result{ExitCode: 128}, nil
					}
					return seam.Result{Stdout: []byte(work + "\n")}, nil
				}
				if strings.HasPrefix(kind, "name-") {
					want = "sandbox: worktree '" + filepath.Base(work) + "' does not give a usable sandbox name\n\na name needs a letter or digit and at most 63 characters\n"
				}
				switch kind {
				case "newline":
					want = "sandbox: worktree '/tmp/a\\x0ab' holds a control character or is not valid UTF-8\n"
				case "invalid":
					want = "sandbox: worktree '/tmp/a\\xffb' holds a control character or is not valid UTF-8\n"
				case "invalid-parent":
					want = "sandbox: worktree '/tmp/a\\xff/b' holds a control character or is not valid UTF-8\n"
				case "partial":
					want = "sandbox: worktree '/tmp/caf\\xc3' holds a control character or is not valid UTF-8\n"
				}
				if strings.HasPrefix(kind, "valid-") {
					want = dcUnknown(derivedName(work), false)
					if args[0] == "up" {
						f.env["HOME"] = "relative"
						f.env["XDG_STATE_HOME"] = ""
						want = "sandbox: HOME is not an absolute path\n"
					}
				}
				before := dcSnapshot(t, f.base)
				c, o, e := f.run(forbiddenReader{t}, args...)
				dcWant(t, c, o, e, wantCode, want)
				if !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
					t.Fatal("failure changed filesystem")
				}
				n := 1
				if kind == "empty" {
					n = 0
				}
				if len(f.calls) != n {
					t.Fatalf("calls %v", f.calls)
				}
			})
		}
	}
}
func TestDataCommandUnknownOwnership(t *testing.T) {
	// R-LLZF-Q536 R-Y5PS-9D26 R-YMSD-M5FW R-GCCZ-SVOD
	for _, args := range dcWorkCommands {
		if args[0] == "up" {
			continue
		}
		t.Run("unknown/"+strings.Join(args, "-"), func(t *testing.T) {
			f := dcNew(t)
			before := dcSnapshot(t, f.base)
			c, o, e := f.run(forbiddenReader{t}, args...)
			dcWant(t, c, o, e, 2, dcUnknown("wip", false))
			if len(f.calls) != 1 || !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
				t.Fatal("unknown had side effects")
			}
		})
	}
	for _, args := range dcWorkCommands {
		for _, recorded := range []string{"/gone/wip", "/gone/a\nb"} {
			t.Run("owner/"+strings.Join(args, "-")+recorded, func(t *testing.T) {
				f := dcNew(t)
				dataTestRegistry(t, f.p, registry{Sandboxes: []registryEntry{{Name: "wip", Port: 7400, Worktree: recorded, Apps: []registryApp{}}}})
				before := dcSnapshot(t, f.base)
				c, o, e := f.run(forbiddenReader{t}, args...)
				dcWant(t, c, o, e, 2, "sandbox: sandbox 'wip' belongs to another worktree: "+dataPrinted(recorded, false)+"\n\nrename this worktree, or wipe that sandbox with 'sandbox wipe wip' once it is down\n")
				if len(f.calls) != 1 || !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
					t.Fatal("ownership refusal had side effects")
				}
			})
		}
	}
	for _, cmd := range []string{"down", "wipe"} {
		for _, name := range []string{"missing", "a\nb", "Bad", "a--b"} {
			t.Run(cmd+name, func(t *testing.T) {
				f := dcNew(t)
				before := dcSnapshot(t, f.base)
				c, o, e := f.run(forbiddenReader{t}, cmd, name)
				dcWant(t, c, o, e, 2, dcUnknown(name, true))
				if len(f.calls) != 0 || !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
					t.Fatal("named refusal side effects")
				}
			})
		}
	}
}
func TestDataCommandPrecedence(t *testing.T) {
	// R-LKKR-WFZT R-IG77-FAZI R-IHF3-T2Q7 R-LJJM-YLLS
	for _, home := range []string{"", "relative"} {
		for _, args := range [][]string{{"up"}, {"down", "wip"}, {"wipe", "wip"}, {"ls"}} {
			t.Run(home+strings.Join(args, "-"), func(t *testing.T) {
				f := dcNew(t)
				f.env["HOME"] = home
				f.env["XDG_STATE_HOME"] = ""
				f.env["XDG_CONFIG_HOME"] = "/bad\nconfig"
				before := dcSnapshot(t, f.base)
				c, o, e := f.run(forbiddenReader{t}, args...)
				dcWant(t, c, o, e, 2, "sandbox: HOME is not an absolute path\n")
				if !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
					t.Fatal("HOME failure created files")
				}
			})
		}
	}
	f := dcNew(t)
	f.env["HOME"] = "relative"
	f.env["XDG_CONFIG_HOME"] = ""
	c, o, e := f.run(forbiddenReader{t}, "status")
	dcWant(t, c, o, e, 2, dcUnknown("wip", false))
	f = dcNew(t)
	f.env["HOME"] = ""
	c, o, e = f.run(forbiddenReader{t}, "ls")
	dcWant(t, c, o, e, 0, "")
	for _, cmd := range []string{"down", "wipe"} {
		f = dcNew(t)
		f.env["HOME"] = "relative"
		f.env["XDG_STATE_HOME"] = ""
		c, o, e = f.run(forbiddenReader{t}, cmd, "a\nb")
		dcWant(t, c, o, e, 2, dcUnknown("a\nb", true))
	}
	// Each earlier worktree check wins even when every later input is invalid.
	for _, stage := range []string{"dir", "git", "work", "name", "home", "root", "unknown"} {
		f = dcNew(t)
		f.env["HOME"] = "relative"
		f.env["XDG_STATE_HOME"] = ""
		work := "/tmp/!!!"
		want := ""
		switch stage {
		case "dir":
			f.work = ""
			want = "sandbox: the current directory no longer exists\n"
		case "git":
			want = "sandbox: '" + f.work + "' is not inside a git checkout\n"
		case "work":
			work = "/tmp/a\nb"
			want = "sandbox: worktree '/tmp/a\\x0ab' holds a control character or is not valid UTF-8\n"
		case "name":
			want = "sandbox: worktree '!!!' does not give a usable sandbox name\n\na name needs a letter or digit and at most 63 characters\n"
		case "home":
			work = f.work
			want = "sandbox: HOME is not an absolute path\n"
		case "root":
			work = f.work
			f.env["XDG_STATE_HOME"] = f.p.state + "\n"
			want = "sandbox: state directory '" + f.p.state + "\\x0a' holds a control character, quote, backslash or a character systemd rejects\n"
		case "unknown":
			work = f.work
			f.env["XDG_STATE_HOME"] = f.p.state
			want = dcUnknown("wip", false)
		}
		f.answer = func(seam.Cmd) (seam.Result, error) {
			if stage == "git" {
				return seam.Result{ExitCode: 1}, nil
			}
			return seam.Result{Stdout: []byte(work + "\n")}, nil
		}
		c, o, e = f.run(forbiddenReader{t}, "status")
		dcWant(t, c, o, e, 2, want)
	}
}
func TestDataCommandRootCharacters(t *testing.T) {
	// R-XR2Z-O45U
	cases := []struct {
		key, value, printedRoot string
		bad                     bool
	}{{"HOME", "/home/a\tb", "/home/a\\x09b/.local/state", true}, {"HOME", "/home/q\"r", "/home/q\"r/.local/state", true}, {"XDG_STATE_HOME", "/tmp/q\"r", "/tmp/q\"r", true}, {"XDG_STATE_HOME", "/tmp/it's", "/tmp/it's", true}, {"XDG_STATE_HOME", "/tmp/a\\b", "/tmp/a\\b", true}, {"XDG_STATE_HOME", "/tmp/a\xffb", "/tmp/a\\xffb", true}, {"XDG_STATE_HOME", "/tmp/a\ufdd0", "/tmp/a\ufdd0", true}, {"XDG_STATE_HOME", "/tmp/x\u0085y", "/tmp/x\u0085y", false}, {"XDG_STATE_HOME", "/tmp/café", "/tmp/café", false}}
	for _, tc := range cases {
		commands := []string{"ls"}
		if tc.bad {
			commands = append(commands, "up")
		}
		for _, cmd := range commands {
			t.Run(cmd+tc.value, func(t *testing.T) {
				f := dcNew(t)
				f.env[tc.key] = tc.value
				if tc.key == "HOME" {
					f.env["XDG_STATE_HOME"] = ""
				}
				if !tc.bad { // Use only a reporting command for accepted roots outside our temporary directory.
					f.env["XDG_STATE_HOME"] = filepath.Join(f.base, filepath.Base(tc.value))
					c, o, e := f.run(forbiddenReader{t}, "ls")
					dcWant(t, c, o, e, 0, "")
					return
				}
				before := dcSnapshot(t, f.base)
				c, o, e := f.run(forbiddenReader{t}, cmd)
				dcWant(t, c, o, e, 2, "sandbox: state directory '"+tc.printedRoot+"' holds a control character, quote, backslash or a character systemd rejects\n")
				if !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
					t.Fatal("root refusal changed filesystem")
				}
			})
		}
	}
	for _, cmd := range []string{"up", "ls"} {
		f := dcNew(t)
		f.env["XDG_CONFIG_HOME"] = "/tmp/c\nd"
		c, o, e := f.run(forbiddenReader{t}, cmd)
		if cmd == "up" {
			dcWant(t, c, o, e, 2, "sandbox: config directory '/tmp/c\\x0ad' holds a control character, quote, backslash or a character systemd rejects\n")
		} else {
			dcWant(t, c, o, e, 0, "")
		}
	}
	f := dcNew(t)
	f.env["XDG_CONFIG_HOME"] = "/tmp/q\"r"
	r := invocation{deps: f.deps()}
	if _, err := r.roots(true); err != nil {
		t.Fatal(err)
	}
}
func TestDataCommandNamedAndConfigIndependence(t *testing.T) {
	// R-EJQB-J1DK R-0JCK-5AQL R-GB53-F3XO R-EVXB-CQSI
	for _, args := range [][]string{{"down", "wip"}, {"wipe", "wip"}, {"ls"}} {
		var previous []seam.Cmd
		for _, dir := range []string{"", "/gone", "/a\nb"} {
			f := dcNew(t)
			f.seed()
			f.work = dir
			dataTestRegistry(t, f.p, registry{Sandboxes: []registryEntry{{Name: "wip", Port: 7400, Worktree: "/gone/other", Apps: []registryApp{{Name: "auth"}}}}})
			c, _, e := f.run(forbiddenReader{t}, args...)
			if c != 0 || e != "" {
				t.Fatalf("%v: %d %s", args, c, e)
			}
			for _, call := range f.calls {
				if call.Path == "git" {
					t.Fatal("named command invoked git")
				}
			}
			if previous != nil && !reflect.DeepEqual(previous, f.calls) {
				t.Fatal("Dir changes runner calls")
			}
			previous = f.calls
		}
	}
	for _, args := range [][]string{{"ls"}, {"url"}, {"status"}, {"logs"}, {"token"}, {"token", "set"}} {
		f := dcNew(t)
		f.seed()
		if err := os.MkdirAll(f.p.data("wip"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.p.token("wip"), []byte("ikp_original"), 0600); err != nil {
			t.Fatal(err)
		}
		c, o, e := f.run(strings.NewReader("ikp_new"), args...)
		calls := append([]seam.Cmd(nil), f.calls...)
		for _, value := range []string{"", "relative", "/a\nb"} {
			f.env["HOME"] = value
			f.env["XDG_CONFIG_HOME"] = value
			f.calls = nil
			cc, oo, ee := f.run(strings.NewReader("ikp_new"), args...)
			if cc != c || ee != e || (!reflect.DeepEqual(calls, f.calls)) || oo != o {
				t.Fatalf("%v config-dependent: (%d,%q,%q) vs (%d,%q,%q)", args, cc, oo, ee, c, o, e)
			}
		}
	}
}
func dcLockFree(t *testing.T, p string, want bool) {
	t.Helper()
	f, err := os.OpenFile(filepath.Clean(p), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if want && err != nil {
		t.Fatalf("lock %s unavailable: %v", p, err)
	}
	if !want && !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("lock %s not held: %v", p, err)
	}
}
func TestDataCommandRunnerLocksAndStates(t *testing.T) {
	// R-D807-7C4S R-GOJZ-ML3B R-0O85-ODPD R-ICJI-9ZRF R-IDRE-NRI4 R-LX3F-ASMX R-LYBB-OKDM R-LZJ8-2C4B
	for _, cmd := range []string{"down", "wipe"} {
		f := dcNew(t)
		f.seed()
		f.answer = func(c seam.Cmd) (seam.Result, error) {
			if c.Path == "git" {
				return seam.Result{Stdout: []byte(f.work + "\n")}, nil
			}
			if c.Dir != "/" {
				t.Fatalf("external Dir %q", c.Dir)
			}
			dcLockFree(t, f.p.lock("wip"), false)
			dcLockFree(t, f.p.registryLock(), true)
			if c.Args[1] == "show" {
				if !reflect.DeepEqual(c.Args[:4], []string{"--user", "show", "--property=ActiveState", "--value"}) || len(c.Args) != 5 {
					t.Fatalf("state cmd %v", c)
				}
				return seam.Result{Stdout: []byte("inactive\n")}, nil
			}
			return seam.Result{}, nil
		}
		c, o, e := f.run(forbiddenReader{t}, cmd)
		dcWant(t, c, o, e, 0, "")
	}
	for _, state := range []string{"active", "reloading", "refreshing", "inactive", "failed", "activating", "deactivating", "active\n", "", "unknown"} {
		f := dcNew(t)
		f.seed()
		f.answer = func(c seam.Cmd) (seam.Result, error) {
			if !reflect.DeepEqual(c, seam.Cmd{Path: "systemctl", Args: []string{"--user", "show", "--property=ActiveState", "--value", "sandbox-wip-nginx.service"}, Dir: "/"}) {
				t.Fatalf("sandbox state %v", c)
			}
			return seam.Result{Stdout: []byte(state + "\n")}, nil
		}
		c, o, e := f.run(forbiddenReader{t}, "ls")
		if c != 0 || e != "" {
			t.Fatalf("state error %d %s", c, e)
		}
		want := "down"
		if state == "active" || state == "reloading" || state == "refreshing" {
			want = "up"
		}
		if !strings.Contains(o, "  "+want+strings.Repeat(" ", 7-len(want))) {
			t.Fatalf("state %q report %q", state, o)
		}
	}
	for _, runnerErr := range []bool{false, true} {
		f := dcNew(t)
		f.seed()
		f.answer = func(seam.Cmd) (seam.Result, error) {
			if runnerErr {
				return seam.Result{}, errors.New("cannot execute")
			}
			return seam.Result{ExitCode: 8, Output: []byte("bad unit\n")}, nil
		}
		c, o, e := f.run(forbiddenReader{t}, "ls")
		want := "sandbox: systemctl --user: exit status 8\n\n> bad unit\n"
		if runnerErr {
			want = "sandbox: systemctl --user: cannot execute\n"
		}
		dcWant(t, c, o, e, 1, want)
	}
}

type dcReader struct {
	t    *testing.T
	p    string
	read bool
}

func (r *dcReader) Read(b []byte) (int, error) {
	dcLockFree(r.t, r.p, false)
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(b, "ikp_changed"), nil
}
func TestDataCommandTokenReadLock(t *testing.T) { // R-D983-L3VH
	f := dcNew(t)
	f.seed()
	c, o, e := f.run(&dcReader{t: t, p: f.p.lock("wip")}, "token", "set")
	dcWant(t, c, o, e, 0, "")
}
func dcWaitFlock(t *testing.T, tid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(fmt.Sprintf("/proc/self/task/%d/syscall", tid))
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(b))
		if len(fields) > 0 && fields[0] == strconv.Itoa(syscall.SYS_FLOCK) {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("command never blocked in flock")
}
func TestDataCommandWaitReread(t *testing.T) {
	// R-DBNW-CNCV R-GIGH-PQDU R-LTFQ-5HEU R-LVVI-X0W8 R-GM46-V1LX R-DAFZ-YVM6
	for _, cmd := range []string{"down", "wipe", "token set", "up"} {
		for _, change := range []string{"none", "removed", "owner"} {
			t.Run(cmd+"/"+change, func(t *testing.T) {
				f := dcNew(t)
				f.seed()
				baselineCode, baselineOut, baselineErr := 0, "", ""
				if cmd == "up" {
					baselineCode, baselineOut, baselineErr = f.run(strings.NewReader("ikp_changed"), "up")
					f.calls = nil
				}
				unlock, err := lockFile(f.p.lock("wip"))
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
				before := dcSnapshot(t, f.base)
				var out, diagnostic dcBuffer
				tid := make(chan int, 1)
				done := make(chan int, 1)
				args := strings.Fields(cmd)
				deps := f.deps()
				go func() {
					runtime.LockOSThread()
					defer runtime.UnlockOSThread()
					tid <- syscall.Gettid()
					done <- runChecked(context.Background(), t, args, strings.NewReader("ikp_changed"), &out, &diagnostic, deps)
				}()
				dcWaitFlock(t, <-tid)
				dcLockFree(t, f.p.registryLock(), true)
				if out.Len() != 0 || diagnostic.Len() != 0 {
					t.Fatal("waiting command wrote output")
				}
				if !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
					t.Fatal("waiting command wrote files")
				}
				if change != "none" {
					reg := registry{Sandboxes: []registryEntry{}}
					if change == "owner" {
						reg.Sandboxes = []registryEntry{{Name: "wip", Port: 7400, Worktree: "/different/wip", Apps: []registryApp{}}}
					}
					dataTestRegistry(t, f.p, reg)
				}
				unlock()
				select {
				case code := <-done:
					switch {
					case change == "owner":
						dcWant(t, code, out.String(), diagnostic.String(), 2, "sandbox: sandbox 'wip' belongs to another worktree: /different/wip\n\nrename this worktree, or wipe that sandbox with 'sandbox wipe wip' once it is down\n")
					case change == "removed" && cmd != "up":
						dcWant(t, code, out.String(), diagnostic.String(), 2, dcUnknown("wip", false))
					case cmd != "up":
						dcWant(t, code, out.String(), diagnostic.String(), 0, "")
					case code != baselineCode || out.String() != baselineOut || diagnostic.String() != baselineErr:
						t.Fatalf("up wait differs from free lock: %d %q %q", code, out.String(), diagnostic.String())
					}
				case <-time.After(5 * time.Second):
					t.Fatal("command did not complete after release")
				}
				dcLockFree(t, f.p.lock("wip"), true)
			})
		}
	}
}
func TestDataCommandReadOnlyUnderLocks(t *testing.T) {
	// R-GPRW-0CU0 R-FVAE-G3AN
	for _, args := range [][]string{{"ls"}, {"url"}, {"status"}, {"logs"}, {"token"}} {
		f := dcNew(t)
		f.seed()
		if err := os.MkdirAll(f.p.data("wip"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.p.token("wip"), []byte("ikp_original"), 0600); err != nil {
			t.Fatal(err)
		}
		a, err := lockFile(f.p.lock("wip"))
		if err != nil {
			t.Fatal(err)
		}
		b, err := lockFile(f.p.registryLock())
		if err != nil {
			t.Fatal(err)
		}
		before := dcSnapshot(t, f.base)
		done := make(chan struct{})
		go func() {
			defer close(done)
			c, _, e := f.run(forbiddenReader{t}, args...)
			if args[0] == "url" {
				if c != 2 {
					t.Errorf("url code %d", c)
				}
			} else if c != 0 || e != "" {
				t.Errorf("%v: %d %s", args, c, e)
			}
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			a()
			b()
			t.Fatal("report command waited on lock")
		}
		a()
		b()
		if !reflect.DeepEqual(before, dcSnapshot(t, f.base)) {
			t.Fatal("report changed filesystem")
		}
	}
}
func TestDataCommandFilesystemLifecycle(t *testing.T) {
	// R-RE8P-UOUP R-FP6W-J8L6 R-RFGM-8GLE R-LG0T-Y097 R-LH8Q-BRZW R-LIGM-PJQL R-LJOJ-3BHA R-G3TP-4HHI R-LS7T-RPO5 R-G69H-W0YW R-GG0O-Y6WG R-DFBL-HYKY R-D807-7C4S
	f := newUpFixture(t, "auth")
	f.registry(registryApp{Name: "auth"})
	f.put(filepath.Join(f.data, "apps/auth/state/keep"), "state contents", 0600)
	f.put(filepath.Join(f.data, "token"), "ikp_original", 0600)
	f.put(filepath.Join(f.units, "sandbox-wip-x-auth.service"), "other sandbox", 0644)
	f.put(filepath.Join(f.root, "outside"), "outside sentinel", 0644)
	outsideBefore := dcOutside(t, f.root, filepath.Join(f.state, "ikigenba/sandbox"), f.units)
	original := f.records()[0]
	kept := dcSnapshot(t, filepath.Join(f.data, "apps"))
	f.exec = func(c seam.Cmd) (seam.Result, error) {
		if c.Path != "git" {
			dcLockFree(t, filepath.Join(f.state, "ikigenba/sandbox/wip.lock"), false)
			dcLockFree(t, filepath.Join(f.state, "ikigenba/sandbox/registry.json.lock"), true)
		}
		return f.answer(c)
	}
	for _, cmd := range []string{"up", "up", "down", "down"} {
		c, o, e := f.run(cmd)
		if cmd == "up" {
			if o != "auth  http://auth.wip.localhost:7400\n" {
				t.Fatalf("up output %q", o)
			}
			o = ""
		}
		dcWant(t, c, o, e, 0, "")
		if !reflect.DeepEqual(kept, dcSnapshot(t, filepath.Join(f.data, "apps"))) {
			t.Fatal("kept app state changed")
		}
		b, err := os.ReadFile(filepath.Join(f.data, "token"))
		if err != nil || string(b) != "ikp_original" {
			t.Fatal("kept token changed")
		}
		b, err = os.ReadFile(filepath.Join(f.units, "sandbox-wip-x-auth.service"))
		if err != nil || string(b) != "other sandbox" {
			t.Fatal("prefix selected unrelated unit")
		}
		b, err = os.ReadFile(filepath.Join(f.root, "outside"))
		if err != nil || string(b) != "outside sentinel" {
			t.Fatal("outside sentinel changed")
		}
		reg := f.records()
		if len(reg) != 1 || reg[0].Name != original.Name || reg[0].Port != original.Port || reg[0].Worktree != original.Worktree {
			t.Fatalf("entry changed: %v", reg)
		}
		if !reflect.DeepEqual(outsideBefore, dcOutside(t, f.root, filepath.Join(f.state, "ikigenba/sandbox"), f.units)) {
			t.Fatal("changed outside permitted trees")
		}
		entries, err := os.ReadDir(f.data)
		if err != nil {
			t.Fatal(err)
		}
		allowed := map[string]bool{"bin": true, "stage": true, "env": true, "services.json": true, "nginx": true, "apps": true, "token": true}
		for _, entry := range entries {
			if !allowed[entry.Name()] {
				t.Fatalf("left data residue %s", entry.Name())
			}
		}
		if cmd == "down" {
			for _, name := range []string{"bin", "stage", "env", "services.json", "nginx"} {
				if _, err := os.Stat(filepath.Join(f.data, name)); !os.IsNotExist(err) {
					t.Fatalf("generated %s survives down", name)
				}
			}
		}
		rootEntries, err := os.ReadDir(filepath.Join(f.state, "ikigenba/sandbox"))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range rootEntries {
			if !map[string]bool{"registry.json": true, "registry.json.lock": true, "wip.lock": true, "wip": true}[entry.Name()] {
				t.Fatalf("root residue %s", entry.Name())
			}
		}
		units, err := os.ReadDir(f.units)
		if err != nil {
			t.Fatal(err)
		}
		for _, unit := range units {
			if unit.Name() == "sandbox-wip-x-auth.service" {
				continue
			}
			if !map[string]bool{"sandbox-wip-nginx.service": true, "sandbox-wip-auth.socket": true, "sandbox-wip-auth.service": true}[unit.Name()] {
				t.Fatalf("unit residue %s", unit.Name())
			}
		}
	}
	c, o, e := f.run("wipe", "wip")
	dcWant(t, c, o, e, 0, "")
	for _, name := range []string{"registry.json", "registry.json.lock", "wip.lock"} {
		if _, err := os.Stat(filepath.Join(f.state, "ikigenba/sandbox", name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(f.data); !os.IsNotExist(err) {
		t.Fatal("wipe kept data")
	}
}
func TestDataCommandRegistryWaitPreservesChanges(t *testing.T) {
	// R-GNC3-8TCM R-LWYJ-62RF
	for _, clash := range []bool{false, true} {
		t.Run(fmt.Sprint(clash), func(t *testing.T) {
			f := newUpFixture(t, "x-auth")
			f.registry()
			p := paths{root: filepath.Join(f.state, "ikigenba/sandbox")}
			unlock, err := lockFile(p.registryLock())
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			tid := make(chan int, 1)
			done := make(chan struct{})
			var code int
			var out, diagnostic string
			go func() {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				tid <- syscall.Gettid()
				code, out, diagnostic = f.run("up")
				close(done)
			}()
			dcWaitFlock(t, <-tid)
			reg := registry{Sandboxes: []registryEntry{{Name: "wip", Port: 7400, Worktree: f.worktree, Apps: []registryApp{}}, {Name: "wip-x", Port: 7401, Worktree: "/other", Apps: []registryApp{{Name: "auth"}}}}}
			if !clash {
				reg.Sandboxes[1].Name = "other"
			}
			if err := writeRegistry(p, reg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(p.registryPath())
			if err != nil {
				t.Fatal(err)
			}
			unlock()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("up did not resume")
			}
			if clash {
				dcWant(t, code, out, diagnostic, 2, "sandbox: unit 'sandbox-wip-x-auth.service' would also belong to sandbox 'wip-x'\n\nrename this worktree or the app, or wipe sandbox 'wip-x' once it is down\n")
				after, err := os.ReadFile(p.registryPath())
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("clash changed registry")
				}
				for _, call := range f.calls {
					if call.Path != "git" {
						t.Fatalf("clash ran %v", call)
					}
				}
			} else {
				if out != "x-auth  http://x-auth.wip.localhost:7400\n" {
					t.Fatalf("up output %q", out)
				}
				dcWant(t, code, "", diagnostic, 0, "")
				r, err := readRegistry(p)
				if err != nil {
					t.Fatal(err)
				}
				if e, ok := r.find("other"); !ok || e.Port != 7401 || e.Worktree != "/other" {
					t.Fatalf("lost concurrent entry %v", r)
				}
			}
		})
	}
}
func TestDataCommandContinuousLock(t *testing.T) {
	// R-DAFZ-YVM6
	f := dcNew(t)
	f.seed()
	steps := make(chan int)
	advance := make(chan struct{})
	firstDone := make(chan int, 1)
	secondDone := make(chan int, 1)
	tid := make(chan int, 1)
	first := f.deps()
	count := 0
	first.Exec = func(_ context.Context, _ seam.Cmd) (seam.Result, error) {
		count++
		dcLockFree(t, f.p.lock("wip"), false)
		steps <- count
		<-advance
		return seam.Result{Stdout: []byte("inactive\n")}, nil
	}
	go func() {
		var o, e bytes.Buffer
		firstDone <- runChecked(context.Background(), t, []string{"down", "wip"}, forbiddenReader{t}, &o, &e, first)
	}()
	<-steps
	second := f.deps()
	second.Exec = func(_ context.Context, _ seam.Cmd) (seam.Result, error) {
		dcLockFree(t, f.p.lock("wip"), false)
		return seam.Result{Stdout: []byte("inactive\n")}, nil
	}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid <- syscall.Gettid()
		var o, e bytes.Buffer
		secondDone <- runChecked(context.Background(), t, []string{"down", "wip"}, forbiddenReader{t}, &o, &e, second)
	}()
	waiting := <-tid
	dcWaitFlock(t, waiting)
	// Keep the first command gated at every runner call. The second must stay a blocked waiter throughout.
	for {
		advance <- struct{}{}
		select {
		case <-steps:
			dcWaitFlock(t, waiting)
			select {
			case code := <-secondDone:
				t.Fatalf("second command interleaved, exit %d", code)
			default:
			}
		case code := <-firstDone:
			if code != 0 {
				t.Fatalf("first exit %d", code)
			}
			select {
			case code := <-secondDone:
				if code != 0 {
					t.Fatalf("second exit %d", code)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("second never completed")
			}
			dcLockFree(t, f.p.lock("wip"), true)
			return
		case <-time.After(5 * time.Second):
			t.Fatal("first stalled")
		}
	}
}

func dcOutside(t *testing.T, base string, allowed ...string) map[string]string {
	t.Helper()
	m := dcSnapshot(t, base)
	for p := range m {
		for _, a := range allowed {
			if p == a || strings.HasPrefix(p, a+"/") || strings.HasPrefix(a, p+"/") {
				delete(m, p)
				break
			}
		}
	}
	return m
}

type dcBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *dcBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *dcBuffer) Len() int       { b.mu.Lock(); defer b.mu.Unlock(); return b.b.Len() }
func (b *dcBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
