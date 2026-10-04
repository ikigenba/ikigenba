package cli_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

type startBytes struct {
	sync.Mutex
	bytes.Buffer
	calls int
}

func (b *startBytes) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	b.calls++
	return b.Buffer.Write(p)
}
func (b *startBytes) text() string { b.Lock(); defer b.Unlock(); return b.String() }

type startRand struct{}

func (startRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0x5a
	}
	return len(p), nil
}

type startFixture struct {
	p         cli.Process
	env       map[string]string
	mu        sync.Mutex
	out, err  startBytes
	capture   telemetry.Capture
	ln        net.Listener
	notify    *net.UnixConn
	done      chan int
	cancel    context.CancelCauseFunc
	http      *http.Client
	mcp       *mcp.Client
	keys      []string
	inherited int
	unset     []string
}

func newStartFixture(t *testing.T) *startFixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	gitPath, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	f := &startFixture{env: map[string]string{}, done: make(chan int, 1)}
	dir := t.TempDir()
	f.env = map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "1", "PATH": filepath.Dir(gitPath), "HOME": dir, "XDG_CONFIG_HOME": dir, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_TERMINAL_PROMPT": "0", "TMPDIR": dir}
	f.ln, e = net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = f.ln.Close() })
	short, e := os.MkdirTemp("", "sites-ready-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	f.notify, e = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = f.notify.Close() })
	f.env["NOTIFY_SOCKET"] = f.notify.LocalAddr().String()
	f.p = cli.Process{Dir: dir, Pid: 123, Stdout: &f.out, Stderr: &f.err, Rand: startRand{}, Now: func() time.Time { return time.Date(2020, 1, 2, 3, 4, 5, 123456789, time.FixedZone("test", 3600)) }, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Sink: &f.capture, Banner: page.New(pages.ServiceName, cli.Version).Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: cli.Version, Telemetry: w})
	}, LookupEnv: func(k string) (string, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.keys = append(f.keys, k)
		v, ok := f.env[k]
		return v, ok
	}, Environ: func() []string {
		f.mu.Lock()
		defer f.mu.Unlock()
		var a []string
		for k, v := range f.env {
			a = append(a, k+"="+v)
		}
		return a
	}, Unsetenv: func(k string) error { f.unset = append(f.unset, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("fd=%d", fd)
		}
		f.inherited++
		return f.ln, nil
	}}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", f.ln.Addr().String())
	}}
	f.http = &http.Client{Transport: transport, Timeout: 3 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	f.mcp = mcp.NewClient(mcp.ClientConfig{Endpoint: "http://sites/mcp", HTTPClient: f.http, Name: "test", Version: "test"})
	return f
}
func (f *startFixture) set(k, v string) { f.mu.Lock(); f.env[k] = v; f.mu.Unlock() }
func (f *startFixture) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	f.cancel = cancel
	go func() { f.done <- cli.Run(ctx, f.p) }()
	_ = f.notify.SetReadDeadline(time.Now().Add(3 * time.Second))
	b := make([]byte, 64)
	n, _, e := f.notify.ReadFromUnix(b)
	if e != nil {
		t.Fatal(e)
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("notify=%q", b[:n])
	}
	t.Cleanup(func() { cancel(errors.New("test stop")) })
}
func (f *startFixture) stop(t *testing.T) int {
	t.Helper()
	f.cancel(errors.New("test stop"))
	select {
	case code := <-f.done:
		return code
	case <-time.After(3 * time.Second):
		t.Fatal("run did not stop")
		return -1
	}
}
func (f *startFixture) call(t *testing.T, name, args string) map[string]any {
	t.Helper()
	r, e := f.mcp.CallTool(context.Background(), identity.Caller{UserID: "owner"}, name, json.RawMessage(args))
	if e != nil || r.IsError() {
		t.Fatalf("%s: %#v %v", name, r, e)
	}
	var result map[string]any
	if e = json.Unmarshal(startStructured(t, r), &result); e != nil {
		t.Fatal(e)
	}
	return result
}
func (f *startFixture) get(t *testing.T, path string) *http.Response {
	t.Helper()
	r, e := http.NewRequest(http.MethodGet, "http://sites"+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-User-Id", "owner")
	r.Header.Set("X-Request-Id", "request-fixed")
	res, e := f.http.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	return res
}

// R-V89J-WCJE R-V9HG-A4A3 R-VAPC-NW0S R-VBX9-1NRH R-VD55-FFI6 R-VFKY-6YZK R-V6ZN-HU9K R-2VXD-WPI1 R-SLH1-GJRQ
func TestRunStartupRefusals(t *testing.T) {
	for _, tc := range []struct{ name, pid, count, setting, want string }{{"setting", "123", "1", "abc", "DRAIN_SECONDS is 'abc', not a positive whole number of seconds"}, {"missing", "124", "1", "", "no socket was passed in\n\nrun it under systemd, with a listening socket passed in"}, {"zero", "123", "0", "", "no socket was passed in\n\nrun it under systemd, with a listening socket passed in"}, {"space", "123", " 1", "", "no socket was passed in\n\nrun it under systemd, with a listening socket passed in"}, {"plus", "123", "+1", "", "no socket was passed in\n\nrun it under systemd, with a listening socket passed in"}, {"many", "123", "0002", "", "0002 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in"}, {"overflow", "123", "999999999999999999999999", "", "999999999999999999999999 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStartFixture(t)
			f.p.Banner = func(page.User) page.Banner { t.Error("refusal called banner"); return page.Banner{} }
			f.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Error("refusal called MCP"); return nil }
			defer startNoNotification(t, f)
			f.set("LISTEN_PID", tc.pid)
			f.set("LISTEN_FDS", tc.count)
			f.set("DRAIN_SECONDS", tc.setting)
			f.p.Database = filepath.Join(t.TempDir(), "outside.db")
			code := cli.Run(context.Background(), f.p)
			if code != cli.ExitUsage || f.err.text() != "sites: "+tc.want+"\n" || f.out.text() != "" || f.err.calls != 1 {
				t.Fatalf("code=%d stderr=%q", code, f.err.text())
			}
			if f.inherited != 0 || len(f.unset) != 0 || len(f.capture.Events()) != 0 {
				t.Fatal("refusal touched socket/events")
			}
			entries, e := os.ReadDir(f.p.Dir)
			if e != nil || len(entries) != 0 {
				t.Fatalf("entries=%v %v", entries, e)
			}
			if _, e = os.Stat(f.p.Database); !os.IsNotExist(e) {
				t.Fatal("database touched")
			}
			for _, k := range f.keys {
				if k == "PATH" || k == "NOTIFY_SOCKET" || k == "IKIGENBA_SERVICES" {
					t.Fatalf("premature lookup %s", k)
				}
			}
		})
	}
}

// R-VGSU-KQQ9 R-VI0Q-YIGY R-VJ8N-CA7N R-VKGJ-Q1YC R-VRRY-0OEI
func TestRunSocketAndGitRefusals(t *testing.T) {
	for _, failure := range []string{"inherit", "git", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			f := newStartFixture(t)
			defer startNoNotification(t, f)
			f.set("LISTEN_FDS", "0001")
			f.p.Inherit = func(fd uintptr) (net.Listener, error) {
				f.inherited++
				if fd != 3 {
					t.Error("wrong fd")
				}
				if failure == "inherit" {
					return nil, errors.New("descriptor refused")
				}
				return f.ln, nil
			}
			if failure == "git" {
				f.set("PATH", t.TempDir())
			}
			check := startGuardRefusal(t, f)
			defer check()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			code := cli.Run(ctx, f.p)
			want := ""
			wantCode := cli.ExitSuccess
			if failure == "inherit" {
				want = "sites: descriptor refused\n"
				wantCode = cli.ExitServerFailed
			}
			if failure == "git" {
				want = "sites: git not found on PATH\n"
				wantCode = cli.ExitServerFailed
			}
			if code != wantCode || f.err.text() != want || f.out.text() != "" || (want != "" && f.err.calls != 1) {
				t.Fatalf("code=%d stderr=%q", code, f.err.text())
			}
			if len(f.keys) < 4 || strings.Join(f.keys[:4], ",") != "DRAIN_SECONDS,SITE_MAX_BYTES,OPERATION_SECONDS,REPOS_DIR" {
				t.Fatalf("settings lookup order=%v", f.keys)
			}
			if f.inherited != 1 || !startSameKeys(f.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
				t.Fatalf("socket=%d unset=%v", f.inherited, f.unset)
			}
			entries, _ := os.ReadDir(f.p.Dir)
			if len(entries) != 0 || len(f.capture.Events()) != 0 {
				t.Fatal("refusal changed state")
			}
		})
	}
}

// R-VMWC-HLFQ R-VO48-VD6F
func TestRunFilesystemRefusals(t *testing.T) {
	for _, path := range []string{"state", "state/sites.db", "cache", "cache/sites"} {
		t.Run(path, func(t *testing.T) {
			f := newStartFixture(t)
			check := startGuardRefusal(t, f)
			defer check()
			target := filepath.Join(f.p.Dir, path)
			if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(target, []byte("not a database"), 0600); e != nil {
				t.Fatal(e)
			}
			var openingError error
			if strings.HasPrefix(path, "cache") {
				g, e := git.Find(f.env["PATH"], f.p.Environ)
				if e != nil {
					t.Fatal(e)
				}
				_, openingError = cache.Open(cache.Config{Root: filepath.Join(f.p.Dir, "cache/sites"), Repos: filepath.Join(f.p.Dir, "missing-repos"), Git: g, Limits: limits.New(settings.Defaults(), limits.Clock{After: f.p.After})})
			} else {
				catalog, e := store.Open(context.Background(), store.Config{Source: filepath.Join(f.p.Dir, "state/sites.db"), Now: f.p.Now, Rand: f.p.Rand})
				openingError = e
				if catalog != nil {
					_ = catalog.Close()
				}
			}
			if openingError == nil {
				t.Fatal("filesystem trap did not refuse opening")
			}
			code := cli.Run(context.Background(), f.p)
			prefix := "sites: cannot open database state/sites.db: "
			if strings.HasPrefix(path, "cache") {
				prefix = "sites: cannot create directory cache/sites: "
				if _, e := os.Stat(filepath.Join(f.p.Dir, "state/sites.db")); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := os.Stat(filepath.Join(f.p.Dir, "cache")); !os.IsNotExist(e) {
					t.Fatal("cache created")
				}
			}
			b, _ := startReadFile(target)
			if string(b) != "not a database" || code != cli.ExitServerFailed || f.err.text() != prefix+openingError.Error()+"\n" || f.err.calls != 1 || f.out.text() != "" || len(f.capture.Events()) != 0 {
				t.Fatalf("code=%d stderr=%q", code, f.err.text())
			}
		})
	}
}

// R-XLYD-QI9J
func TestRunNotificationFailure(t *testing.T) {
	f := newStartFixture(t)
	check := startGuardAccept(t, f)
	defer check()
	a := filepath.Join(t.TempDir(), "missing-socket")
	f.set("NOTIFY_SOCKET", a)
	if code := cli.Run(context.Background(), f.p); code != cli.ExitServerFailed || !strings.HasPrefix(f.err.text(), "sites: ") || !strings.Contains(f.err.text(), a) || strings.Count(f.err.text(), "\n") != 1 || f.err.calls != 1 || f.out.text() != "" || len(f.capture.Events()) != 0 {
		t.Fatalf("code=%d stderr=%q", code, f.err.text())
	}
}

// R-VPC5-94X4 R-VSZU-EG57 R-XTWE-ZGKT R-XQ8P-U5CQ R-BDLE-HF7Y R-XMTE-HNSH R-W8UJ-DGS8 R-XQH3-MZ0K R-YULW-P307 R-XYWI-R1Q5 R-YPQB-601F
func TestRunFirstStartComposition(t *testing.T) {
	for _, services := range []string{"", "missing", "malformed"} {
		t.Run(services, func(t *testing.T) {
			f := newStartFixture(t)
			if services != "" {
				path := filepath.Join(t.TempDir(), "services")
				f.set("IKIGENBA_SERVICES", path)
				if services == "malformed" {
					if e := os.WriteFile(path, []byte("broken"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			trace := filepath.Join(t.TempDir(), "trace")
			f.set("GIT_TRACE2_EVENT", trace)
			calls := 0
			f.p.MCP = func(w *telemetry.Writer) *mcp.Server {
				calls++
				return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: cli.Version, Telemetry: w})
			}
			f.start(t)
			if _, e := os.Stat(filepath.Join(f.p.Dir, "state/sites.db")); e != nil {
				t.Fatal(e)
			}
			entries, e := os.ReadDir(filepath.Join(f.p.Dir, "cache/sites"))
			if e != nil || len(entries) != 0 {
				t.Fatal(entries, e)
			}
			if _, e = os.Stat(trace); !os.IsNotExist(e) {
				t.Fatal("git ran at startup")
			}
			result := f.call(t, "list", "{}")
			if a, ok := result["sites"].([]any); !ok || len(a) != 0 {
				t.Fatal(result)
			}
			res := f.get(t, "/about")
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode != 200 || !bytes.Contains(body, []byte(cli.Version)) {
				t.Fatalf("about %d %s", res.StatusCode, body)
			}
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatalf("exit=%d", code)
			}
			events := f.capture.Events()
			if len(events) < 4 || events[0].Name != "service.started" || events[len(events)-1].Name != "service.stopping" || len(events[0].Attrs) != 1 || events[0].Attrs["version"] != cli.Version || events[0].RequestID != "" || events[0].User != "" {
				t.Fatal(events)
			}
			for _, e := range events {
				if e.Service != "sites" || !e.Time.Equal(f.p.Now().UTC().Truncate(time.Microsecond)) {
					t.Fatalf("envelope=%#v", e)
				}
				if e.Name != "service.started" && e.Name != "service.stopping" && e.RequestID != "request-fixed" && e.RequestID != strings.Repeat("5a", 16) {
					t.Fatal("missing request id")
				}
			}
			if calls != 1 || f.out.text() != "" || f.err.text() != "" {
				t.Fatalf("calls=%d stderr=%q", calls, f.err.text())
			}
			allowed := map[string]bool{"DRAIN_SECONDS": true, "SITE_MAX_BYTES": true, "OPERATION_SECONDS": true, "REPOS_DIR": true, "IKIGENBA_SERVICES": true, "LISTEN_PID": true, "LISTEN_FDS": true, "NOTIFY_SOCKET": true, "PATH": true}
			for _, k := range f.keys {
				if !allowed[k] {
					t.Fatal(k)
				}
			}
		})
	}
}

func startStructured(t *testing.T, r mcp.Result) json.RawMessage {
	t.Helper()
	b, e := r.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var result map[string]json.RawMessage
	if e = json.Unmarshal(b, &result); e != nil {
		t.Fatal(e)
	}
	return result["structuredContent"]
}

func startRepo(t *testing.T, f *startFixture, root string) string {
	t.Helper()
	repo := filepath.Join(root, "rep_0123456789abcdef.git")
	if e := os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	gitPath, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	run := func(input string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, gitPath, args...)
		cmd.Env = append(f.p.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
		cmd.Stdin = strings.NewReader(input)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, out, e)
		}
		return strings.TrimSpace(string(out))
	}
	run("", "init", "--bare", "--initial-branch=main", repo)
	run("", "--git-dir="+repo, "config", "ikigenba.owner", "owner")
	blob := run("hello site", "--git-dir="+repo, "hash-object", "-w", "--stdin")
	tree := run("100644 blob "+blob+"\tindex.html\n", "--git-dir="+repo, "mktree")
	sha := run("fixture\n", "--git-dir="+repo, "commit-tree", tree)
	run("", "--git-dir="+repo, "update-ref", "refs/heads/main", sha)
	return sha
}

// R-VLOG-3TP1 R-2YD6-O8ZF R-YIEW-VDL9 R-2ZL3-20Q4 R-YKUP-MX2N R-YM2M-0OTC R-4AOP-838L R-4BWL-LUZA
func TestRunRepositoriesAndPaths(t *testing.T) {
	for _, relative := range []bool{false, true} {
		t.Run(map[bool]string{true: "relative", false: "absolute"}[relative], func(t *testing.T) {
			f := newStartFixture(t)
			outer := t.TempDir()
			root := filepath.Join(outer, "repos-b/state/repos")
			f.p.Dir = filepath.Join(outer, "sites")
			if e := os.MkdirAll(f.p.Dir, 0700); e != nil {
				t.Fatal(e)
			}
			value := root
			if relative {
				value = "../repos-b/state/repos"
			}
			f.set("REPOS_DIR", value)
			f.set("TMPDIR", outer)

			services := filepath.Join(outer, "services.json")
			f.set("IKIGENBA_SERVICES", services)
			f.start(t)
			f.call(t, "list", "{}")
			if _, e := os.Stat(root); !os.IsNotExist(e) {
				t.Fatal("repos created at startup")
			}
			sha := startRepo(t, f, root)
			f.set("REPOS_DIR", filepath.Join(outer, "wrong"))
			f.set("IKIGENBA_SERVICES", filepath.Join(outer, "wrong-services"))
			if e := os.WriteFile(services, []byte(`{"services":[{"name":"sites","url":"https://sites.example.test","description":"Sites","socket":"/missing","enabled":true,"mcp":true}]}`), 0600); e != nil {
				t.Fatal(e)
			}
			trace := filepath.Join(f.p.Dir, "git-trace")
			f.set("GIT_TRACE2_EVENT", trace)
			before := startSnapshot(t, outer, f.p.Dir)
			site := f.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef","visibility":"public","listed":true}`)
			if site["id"] != "sit_5a5a5a5a5a5a5a5a" || site["url"] != "https://sites.example.test/blog/" {
				t.Fatal(site)
			}
			published := f.call(t, "publish", `{"name":"blog"}`)
			if published["commit"] != sha {
				t.Fatal(published)
			}
			if info, e := os.Stat(filepath.Join(f.p.Dir, "cache/sites", site["id"].(string), sha)); e != nil || !info.IsDir() {
				t.Fatal(info, e)
			}
			res := f.get(t, "/blog/")
			body, e := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if e != nil || res.StatusCode != 200 || string(body) != "hello site" {
				t.Fatalf("response=%d %q %v", res.StatusCode, body, e)
			}
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code, f.err.text())
			}
			if _, e := os.Stat(trace); e != nil {
				t.Fatal("git did not receive supplied environment", e)
			}
			after := startSnapshot(t, outer, f.p.Dir)
			if len(before) != len(after) {
				t.Fatalf("outside entries before=%v after=%v", before, after)
			}
			for p, v := range before {
				if after[p] != v {
					t.Fatalf("outside changed %s", p)
				}
			}
		})
	}
}
func startSnapshot(t *testing.T, root, excluded string) map[string]string {
	t.Helper()
	out := map[string]string{}
	e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == excluded {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, e := os.Readlink(p)
			if e != nil {
				return e
			}
			out[p] = "symlink:" + target
			return nil
		}
		if d.IsDir() {
			out[p] = "directory"
			return nil
		}
		b, e := startReadFile(p)
		if e != nil {
			return e
		}
		out[p] = string(b)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}

// R-2X5A-AH8Q R-YFZ4-3U3V
func TestRunCatalogPersistence(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "external"}[external], func(t *testing.T) {
			f := newStartFixture(t)
			root := t.TempDir()
			startRepo(t, f, root)
			f.set("REPOS_DIR", root)
			database := ""
			if external {
				database = filepath.Join(t.TempDir(), "catalog.db")
				f.p.Database = database
			}
			f.start(t)
			f.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef"}`)
			if f.stop(t) != cli.ExitSuccess {
				t.Fatal(f.err.text())
			}
			for _, same := range []bool{true, false} {
				again := newStartFixture(t)
				if same && !external {
					again.p.Dir = f.p.Dir
				}
				if external && same {
					again.p.Database = database
				}
				again.start(t)
				r := again.call(t, "list", "{}")
				sites := r["sites"].([]any)
				want := 0
				if same {
					want = 1
				}
				if len(sites) != want {
					t.Fatalf("same=%v sites=%v", same, sites)
				}
				if same && external {
					if _, e := os.Stat(filepath.Join(again.p.Dir, "state")); !os.IsNotExist(e) {
						t.Fatal("external database created state")
					}
				}
				if again.stop(t) != cli.ExitSuccess {
					t.Fatal(again.err.text())
				}
			}
			if external {
				if _, e := os.Stat(filepath.Join(f.p.Dir, "state")); !os.IsNotExist(e) {
					t.Fatal("external database created state")
				}
			}
		})
	}
}

func startReadFile(path string) ([]byte, error) {
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.Base(path))
}

// R-XTWE-ZGKT R-XQH3-MZ0K R-W8UJ-DGS8
func TestRunStartupLeavesTreesAndRecordsOnlyLifecycle(t *testing.T) {
	f := newStartFixture(t)
	f.set("REPOS_DIR", filepath.Join(t.TempDir(), "absent"))
	catalog, e := store.Open(context.Background(), store.Config{Source: filepath.Join(f.p.Dir, "state/sites.db"), Now: f.p.Now, Rand: f.p.Rand})
	if e != nil {
		t.Fatal(e)
	}
	site, e := catalog.Create(context.Background(), store.Draft{Owner: "owner", Name: "restored", Repo: "rep_0123456789abcdef", Ref: "main", Visibility: store.Public, Listed: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = catalog.Publish(context.Background(), site.ID, strings.Repeat("a", 40)); e != nil {
		t.Fatal(e)
	}
	if e = catalog.Close(); e != nil {
		t.Fatal(e)
	}

	tree := filepath.Join(f.p.Dir, "cache/sites", "sit_0123456789abcdef", "0123456789abcdef0123456789abcdef01234567")
	if e := os.MkdirAll(tree, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(tree, "saved.html"), []byte("saved tree"), 0600); e != nil {
		t.Fatal(e)
	}
	before := startSnapshot(t, f.p.Dir, "")
	f.start(t)
	if f.stop(t) != cli.ExitSuccess {
		t.Fatal(f.err.text())
	}
	after := startSnapshot(t, f.p.Dir, "")
	startAssertPermittedStartup(t, f.p.Dir, before, after)
	events := f.capture.Events()
	if len(events) != 2 || events[0].Name != "service.started" || events[1].Name != "service.stopping" {
		t.Fatal(events)
	}
}

// R-VSZU-EG57 R-XQ8P-U5CQ
func TestRunAbstractAndAbsentNotification(t *testing.T) {
	t.Run("abstract", func(t *testing.T) {
		f := newStartFixture(t)
		_ = f.notify.Close()
		a := "@" + filepath.Base(filepath.Dir(f.env["NOTIFY_SOCKET"]))
		conn, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: a, Net: "unixgram"})
		if e != nil {
			t.Fatal(e)
		}
		f.notify = conn
		t.Cleanup(func() { _ = conn.Close() })
		f.set("NOTIFY_SOCKET", a)
		f.start(t)
		f.call(t, "list", "{}")
		if f.stop(t) != cli.ExitSuccess {
			t.Fatal(f.err.text())
		}
		startNoNotification(t, f)
	})
	for _, unset := range []bool{true, false} {
		t.Run(map[bool]string{true: "unset", false: "empty"}[unset], func(t *testing.T) {
			f := newStartFixture(t)
			if unset {
				delete(f.env, "NOTIFY_SOCKET")
			} else {
				f.set("NOTIFY_SOCKET", "")
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			f.cancel = cancel
			go func() { f.done <- cli.Run(ctx, f.p) }()
			t.Cleanup(func() { cancel(errors.New("test stop")) })
			f.call(t, "list", "{}")
			if f.stop(t) != cli.ExitSuccess {
				t.Fatal(f.err.text())
			}
			startNoNotification(t, f)
			events := f.capture.Events()
			if len(events) == 0 || events[0].Name != "service.started" {
				t.Fatal(events)
			}
		})
	}
}

func startSameKeys(got, want []string) bool {
	allowed := map[string]bool{}
	for _, key := range want {
		allowed[key] = true
	}
	seen := map[string]bool{}
	for _, key := range got {
		if !allowed[key] {
			return false
		}
		seen[key] = true
	}
	for _, key := range want {
		if !seen[key] {
			return false
		}
	}
	return true
}
func startDatagram(conn *net.UnixConn) (string, error) {
	raw, e := conn.SyscallConn()
	if e != nil {
		return "", e
	}
	var data [128]byte
	var n int
	var received error
	if e = raw.Control(func(fd uintptr) { n, _, received = syscall.Recvfrom(int(fd), data[:], syscall.MSG_DONTWAIT) }); e != nil {
		return "", e
	}
	if received != nil {
		return "", received
	}
	return string(data[:n]), nil
}
func startNoNotification(t *testing.T, f *startFixture) {
	t.Helper()
	data, e := startDatagram(f.notify)
	if !errors.Is(e, syscall.EAGAIN) && !errors.Is(e, syscall.EWOULDBLOCK) {
		t.Errorf("unexpected notification=%q error=%v", data, e)
	}
}

type startAcceptTrap struct {
	net.Listener
	accepted atomic.Int32
}

func (l *startAcceptTrap) Accept() (net.Conn, error) {
	conn, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	l.accepted.Add(1)
	_ = conn.Close()
	return nil, errors.New("unexpected startup acceptance")
}
func startGuardAccept(t *testing.T, f *startFixture) func() {
	t.Helper()
	pending, e := net.Dial("tcp", f.ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = pending.Close() })
	trap := &startAcceptTrap{Listener: f.ln}
	inherit := f.p.Inherit
	f.p.Inherit = func(fd uintptr) (net.Listener, error) {
		_, e := inherit(fd)
		if e != nil {
			return nil, e
		}
		return trap, nil
	}
	return func() {
		if trap.accepted.Load() != 0 {
			t.Error("startup refusal accepted a queued connection")
		}
		startNoNotification(t, f)
	}
}
func startGuardRefusal(t *testing.T, f *startFixture) func() {
	t.Helper()
	f.p.Banner = func(page.User) page.Banner { t.Error("startup refusal called Banner"); return page.Banner{} }
	f.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Error("startup refusal called MCP"); return nil }
	return startGuardAccept(t, f)
}
func startAssertPermittedStartup(t *testing.T, dir string, before, after map[string]string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, name := range []string{"state", "state/sites.db", "state/sites.db-journal", "state/sites.db-wal", "state/sites.db-shm", "cache", "cache/sites"} {
		allowed[filepath.Join(dir, name)] = true
	}
	for p, v := range before {
		sidecar := p == filepath.Join(dir, "state/sites.db-journal") || p == filepath.Join(dir, "state/sites.db-wal") || p == filepath.Join(dir, "state/sites.db-shm")
		if !sidecar && after[p] != v {
			t.Errorf("startup changed or removed %s", p)
		}
	}
	for p := range after {
		if _, exists := before[p]; !exists && !allowed[p] {
			t.Errorf("startup created unexpected %s", p)
		}
	}
}

type startReadyListener struct {
	net.Listener
	notify       *net.UnixConn
	once         sync.Once
	beforeAccept chan error
}

func (l *startReadyListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		data, e := startDatagram(l.notify)
		if e == nil && data != "READY=1" {
			e = errors.New("notification was not READY=1")
		}
		l.beforeAccept <- e
	})
	return l.Listener.Accept()
}

// R-VSZU-EG57 R-XTWE-ZGKT
func TestRunReadinessPrecedesAcceptAndOccursOnce(t *testing.T) {
	f := newStartFixture(t)
	before := startSnapshot(t, f.p.Dir, "")
	ready := &startReadyListener{Listener: f.ln, notify: f.notify, beforeAccept: make(chan error, 1)}
	inherit := f.p.Inherit
	f.p.Inherit = func(fd uintptr) (net.Listener, error) { _, e := inherit(fd); return ready, e }
	ctx, cancel := context.WithCancelCause(context.Background())
	f.cancel = cancel
	t.Cleanup(func() { cancel(errors.New("test stop")) })
	go func() { f.done <- cli.Run(ctx, f.p) }()
	select {
	case e := <-ready.beforeAccept:
		if e != nil {
			t.Fatalf("Accept began before readiness: %v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no Accept")
	}
	after := startSnapshot(t, f.p.Dir, "")
	startAssertPermittedStartup(t, f.p.Dir, before, after)
	f.call(t, "list", "{}")
	if f.stop(t) != cli.ExitSuccess {
		t.Fatal(f.err.text())
	}
	startNoNotification(t, f)
}

// R-VSZU-EG57 R-VRRY-0OEI R-VMWC-HLFQ R-VO48-VD6F
func TestRunCancellationAtStartupErrorChecks(t *testing.T) {
	// Inject cancellation at each observation of the context, including the
	// observation made after an opening failure. This drives startup without
	// relying on a filesystem race or a real-time wait.
	for _, path := range []string{"state", "state/sites.db", "cache", "cache/sites"} {
		t.Run(path, func(t *testing.T) {
			canceledRuns := 0
			liveFailures := 0
			for observation := 1; observation <= 32; observation++ {
				f := newStartFixture(t)
				check := startGuardRefusal(t, f)
				target := filepath.Join(f.p.Dir, path)
				if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(target, []byte("opening trap"), 0600); e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithCancel(context.Background())
				driven := &startCancelObservation{Context: ctx, cancel: cancel, at: observation}
				code := cli.Run(driven, f.p)
				check()
				if ctx.Err() != nil {
					canceledRuns++
					if code != cli.ExitSuccess || f.err.text() != "" || f.out.text() != "" || len(f.capture.Events()) != 0 {
						t.Fatalf("canceled observation=%d code=%d stderr=%q", observation, code, f.err.text())
					}
				} else {
					liveFailures++
					if code != cli.ExitServerFailed {
						t.Fatalf("live error code=%d", code)
					}
				}
				cancel()
			}
			if canceledRuns == 0 || liveFailures == 0 {
				t.Fatalf("canceled=%d live=%d", canceledRuns, liveFailures)
			}
		})
	}
}

type startCancelObservation struct {
	context.Context
	cancel context.CancelFunc
	at     int
	reads  atomic.Int32
}

func (c *startCancelObservation) Err() error {
	if int(c.reads.Add(1)) == c.at {
		c.cancel()
	}
	return c.Context.Err()
}

// R-VRRY-0OEI
func TestRunCancellationBeforeReady(t *testing.T) {
	f := newStartFixture(t)
	check := startGuardRefusal(t, f)
	defer check()
	ctx, cancel := context.WithCancel(context.Background())
	lookup := f.p.LookupEnv
	f.p.LookupEnv = func(k string) (string, bool) {
		if k == "NOTIFY_SOCKET" {
			cancel()
		}
		return lookup(k)
	}
	if code := cli.Run(ctx, f.p); code != cli.ExitSuccess || f.err.text() != "" || f.out.text() != "" || len(f.capture.Events()) != 0 {
		t.Fatalf("code=%d stderr=%q", code, f.err.text())
	}
	if _, e := os.Stat(filepath.Join(f.p.Dir, "state/sites.db")); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(f.p.Dir, "cache/sites")); e != nil {
		t.Fatal(e)
	}
}

// R-VRRY-0OEI
func TestRunCancellationDuringServicesLookupWithoutNotify(t *testing.T) {
	for _, unset := range []bool{true, false} {
		t.Run(map[bool]string{true: "unset", false: "empty"}[unset], func(t *testing.T) {
			f := newStartFixture(t)
			if unset {
				delete(f.env, "NOTIFY_SOCKET")
			} else {
				f.set("NOTIFY_SOCKET", "")
			}
			check := startGuardRefusal(t, f)
			defer check()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lookup := f.p.LookupEnv
			f.p.LookupEnv = func(key string) (string, bool) {
				if key == "IKIGENBA_SERVICES" {
					cancel()
				}
				return lookup(key)
			}
			if code := cli.Run(ctx, f.p); code != cli.ExitSuccess || f.out.text() != "" || f.err.text() != "" || len(f.capture.Events()) != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q events=%v", code, f.out.text(), f.err.text(), f.capture.Events())
			}
		})
	}
}

// R-VRRY-0OEI
func TestRunCancellationDuringMCPConstructionWithoutNotify(t *testing.T) {
	for _, unset := range []bool{true, false} {
		t.Run(map[bool]string{true: "unset", false: "empty"}[unset], func(t *testing.T) {
			f := newStartFixture(t)
			if unset {
				delete(f.env, "NOTIFY_SOCKET")
			} else {
				f.set("NOTIFY_SOCKET", "")
			}
			check := startGuardAccept(t, f)
			defer check()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			factory := f.p.MCP
			calls := 0
			f.p.MCP = func(w *telemetry.Writer) *mcp.Server { calls++; cancel(); return factory(w) }
			if code := cli.Run(ctx, f.p); code != cli.ExitSuccess || f.out.text() != "" || f.err.text() != "" || len(f.capture.Events()) != 0 || calls != 1 {
				t.Fatalf("code=%d stdout=%q stderr=%q events=%v calls=%d", code, f.out.text(), f.err.text(), f.capture.Events(), calls)
			}
		})
	}
}

// R-XTWE-ZGKT
func TestRunStartupCreatesOnlyDeclaredEntries(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "default-database", true: "external-database"}[external], func(t *testing.T) {
			f := newStartFixture(t)
			state := filepath.Join(f.p.Dir, "state")
			cacheRoot := filepath.Join(f.p.Dir, "cache")
			for _, dir := range []string{state, cacheRoot} {
				if e := os.Mkdir(dir, 0700); e != nil {
					t.Fatal(e)
				}
			}
			catalogDir := state
			catalogName := "sites.db"
			if external {
				catalogDir = t.TempDir()
				catalogName = "catalog.db"
				f.p.Database = filepath.Join(catalogDir, catalogName)
			}
			fd, e := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = syscall.Close(fd) }()
			watches := map[int]string{}
			for _, dir := range []string{f.p.Dir, state, cacheRoot, catalogDir} {
				if _, seen := func() (int, bool) {
					for wd, path := range watches {
						if path == dir {
							return wd, true
						}
					}
					return 0, false
				}(); seen {
					continue
				}
				wd, e := syscall.InotifyAddWatch(fd, dir, syscall.IN_CREATE|syscall.IN_DELETE)
				if e != nil {
					t.Fatal(e)
				}
				watches[wd] = dir
			}
			before := startSnapshot(t, f.p.Dir, "")
			f.start(t)
			after := startSnapshot(t, f.p.Dir, "")
			startAssertPermittedStartup(t, f.p.Dir, before, after)
			allowed := map[string]bool{filepath.Join(cacheRoot, "sites"): true}
			for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
				allowed[filepath.Join(catalogDir, catalogName+suffix)] = true
			}
			seenCatalog := false
			seenCache := false
			var buffer [16384]byte
			for {
				n, e := syscall.Read(fd, buffer[:])
				if errors.Is(e, syscall.EAGAIN) || errors.Is(e, syscall.EWOULDBLOCK) {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				if n == 0 {
					break
				}
				for at := 0; at < n; {
					if n-at < 16 {
						t.Fatal("truncated inotify header")
					}
					wd := int(binary.NativeEndian.Uint32(buffer[at : at+4]))
					mask := binary.NativeEndian.Uint32(buffer[at+4 : at+8])
					size := int(binary.NativeEndian.Uint32(buffer[at+12 : at+16]))
					if size > n-at-16 {
						t.Fatal("truncated inotify name")
					}
					if mask&syscall.IN_Q_OVERFLOW != 0 {
						t.Fatal("inotify overflow lost startup observations")
					}
					dir, ok := watches[wd]
					if !ok {
						t.Fatalf("unknown inotify watch %d", wd)
					}
					name := strings.TrimRight(string(buffer[at+16:at+16+size]), "\x00")
					path := filepath.Join(dir, name)
					if mask&(syscall.IN_CREATE|syscall.IN_DELETE) != 0 && !allowed[path] {
						t.Errorf("startup changed undeclared entry %s mask=%#x", path, mask)
					}
					if mask&syscall.IN_CREATE != 0 {
						seenCatalog = seenCatalog || path == filepath.Join(catalogDir, catalogName)
						seenCache = seenCache || path == filepath.Join(cacheRoot, "sites")
					}
					at += 16 + size
				}
			}
			if !seenCatalog || !seenCache {
				t.Errorf("inotify missed expected startup creations catalog=%v cache=%v", seenCatalog, seenCache)
			}
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code, f.err.text())
			}
		})
	}
}

// R-XTWE-ZGKT R-VMWC-HLFQ
func TestRunExplicitDatabaseDoesNotCreateMissingParents(t *testing.T) {
	for _, location := range []string{"external-nested", "inside-nested", "symlink-parent"} {
		t.Run(location, func(t *testing.T) {
			f := newStartFixture(t)
			outside := t.TempDir()
			watchPath := outside
			parent := filepath.Join(outside, "missing", "nested")
			if location == "inside-nested" {
				watchPath = f.p.Dir
				parent = filepath.Join(f.p.Dir, "missing", "nested")
			}
			if location == "symlink-parent" {
				alias := filepath.Join(f.p.Dir, "catalog-parent")
				if e := os.Symlink(outside, alias); e != nil {
					t.Fatal(e)
				}
				parent = filepath.Join(alias, "missing", "nested")
			}
			f.p.Database = filepath.Join(parent, "catalog.db")
			beforeDir := startSnapshot(t, f.p.Dir, "")
			beforeOutside := startSnapshot(t, outside, "")
			fd, e := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = syscall.Close(fd) }()
			if _, e = syscall.InotifyAddWatch(fd, watchPath, syscall.IN_CREATE|syscall.IN_DELETE); e != nil {
				t.Fatal(e)
			}
			check := startGuardRefusal(t, f)
			defer check()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lookup := f.p.LookupEnv
			f.p.LookupEnv = func(key string) (string, bool) {
				if key == "NOTIFY_SOCKET" {
					cancel()
				}
				return lookup(key)
			}
			code := cli.Run(ctx, f.p)
			if code != cli.ExitServerFailed || f.out.text() != "" || f.err.calls != 1 || !strings.HasPrefix(f.err.text(), "sites: cannot open database state/sites.db: ") || strings.Count(f.err.text(), "\n") != 1 || len(f.capture.Events()) != 0 {
				t.Fatalf("code=%d stderr=%q", code, f.err.text())
			}
			if _, e := os.Stat(parent); !os.IsNotExist(e) {
				t.Fatalf("missing parent changed: %v", e)
			}
			afterDir := startSnapshot(t, f.p.Dir, "")
			afterOutside := startSnapshot(t, outside, "")
			if len(beforeDir) != len(afterDir) || len(beforeOutside) != len(afterOutside) {
				t.Fatalf("startup created undeclared entries: beforeDir=%v afterDir=%v beforeOutside=%v afterOutside=%v", beforeDir, afterDir, beforeOutside, afterOutside)
			}
			for p, v := range beforeDir {
				if afterDir[p] != v {
					t.Errorf("startup changed %s", p)
				}
			}
			for p, v := range beforeOutside {
				if afterOutside[p] != v {
					t.Errorf("startup changed %s", p)
				}
			}
			var data [4096]byte
			n, e := syscall.Read(fd, data[:])
			if !errors.Is(e, syscall.EAGAIN) && !errors.Is(e, syscall.EWOULDBLOCK) {
				t.Errorf("startup created/deleted an undeclared parent: inotify bytes=%d err=%v", n, e)
			}
		})
	}
}

// R-XTWE-ZGKT R-VMWC-HLFQ R-VO48-VD6F
func TestRunDefaultPathsRespectPhysicalDirectory(t *testing.T) {
	for _, hook := range []string{"state", "catalog", "cache", "cache-sites"} {
		for _, outward := range []bool{true, false} {
			t.Run(hook+map[bool]string{true: "-outward", false: "-inward"}[outward], func(t *testing.T) {
				f := newStartFixture(t)
				outside := t.TempDir()
				targetRoot := outside
				if !outward {
					targetRoot = filepath.Join(f.p.Dir, "inside-target")
					if e := os.Mkdir(targetRoot, 0700); e != nil {
						t.Fatal(e)
					}
				}
				link := filepath.Join(f.p.Dir, hook)
				target := targetRoot
				switch hook {
				case "catalog":
					if e := os.Mkdir(filepath.Join(f.p.Dir, "state"), 0700); e != nil {
						t.Fatal(e)
					}
					link = filepath.Join(f.p.Dir, "state/sites.db")
					target = filepath.Join(targetRoot, "catalog.db")
				case "cache-sites":
					if e := os.Mkdir(filepath.Join(f.p.Dir, "cache"), 0700); e != nil {
						t.Fatal(e)
					}
					link = filepath.Join(f.p.Dir, "cache/sites")
				}
				if e := os.Symlink(target, link); e != nil {
					t.Fatal(e)
				}
				before := startSnapshot(t, outside, "")
				fd, e := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
				if e != nil {
					t.Fatal(e)
				}
				defer func() { _ = syscall.Close(fd) }()
				if _, e = syscall.InotifyAddWatch(fd, outside, syscall.IN_CREATE|syscall.IN_DELETE); e != nil {
					t.Fatal(e)
				}
				if outward {
					check := startGuardRefusal(t, f)
					defer check()
					code := cli.Run(context.Background(), f.p)
					prefix := "sites: cannot open database state/sites.db: "
					if strings.HasPrefix(hook, "cache") {
						prefix = "sites: cannot create directory cache/sites: "
						if _, e := os.Stat(filepath.Join(f.p.Dir, "state/sites.db")); e != nil {
							t.Fatal(e)
						}
					}
					if code != cli.ExitServerFailed || !strings.HasPrefix(f.err.text(), prefix) || f.err.calls != 1 || strings.Count(f.err.text(), "\n") != 1 || f.out.text() != "" || len(f.capture.Events()) != 0 {
						t.Fatalf("code=%d stderr=%q", code, f.err.text())
					}
				} else {
					f.start(t)
					f.call(t, "list", "{}")
					if code := f.stop(t); code != cli.ExitSuccess {
						t.Fatal(code, f.err.text())
					}
					if f.out.text() != "" || f.err.text() != "" {
						t.Fatal(f.out.text(), f.err.text())
					}
				}
				after := startSnapshot(t, outside, "")
				if len(before) != len(after) {
					t.Fatalf("outside entries changed before=%v after=%v", before, after)
				}
				for p, v := range before {
					if after[p] != v {
						t.Errorf("outside entry changed %s", p)
					}
				}
				var events [4096]byte
				n, e := syscall.Read(fd, events[:])
				if !errors.Is(e, syscall.EAGAIN) && !errors.Is(e, syscall.EWOULDBLOCK) {
					t.Errorf("outside transient write bytes=%d err=%v", n, e)
				}
			})
		}
	}
	t.Run("directory-symlink", func(t *testing.T) {
		f := newStartFixture(t)
		physical := f.p.Dir
		alias := filepath.Join(t.TempDir(), "workdir")
		if e := os.Symlink(physical, alias); e != nil {
			t.Fatal(e)
		}
		f.p.Dir = alias
		f.start(t)
		f.call(t, "list", "{}")
		if code := f.stop(t); code != cli.ExitSuccess {
			t.Fatal(code, f.err.text())
		}
		for _, path := range []string{"state/sites.db", "cache/sites"} {
			if _, e := os.Stat(filepath.Join(physical, path)); e != nil {
				t.Fatal(e)
			}
		}
	})
}
