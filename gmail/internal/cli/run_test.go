package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/cli"
	"github.com/ikigenba/ikigenba/gmail/internal/settings"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
)

// R-VNCO-AVLH R-VOKK-ONC6
func TestDefaultHTTP(t *testing.T) {
	if cli.DialTimeout != 10*time.Second || cli.TLSHandshakeTimeout != 10*time.Second || cli.ResponseHeaderTimeout != 60*time.Second {
		t.Fatal("timeouts")
	}
	a, b := cli.DefaultHTTP(), cli.DefaultHTTP()
	if a == nil || b == nil || a == b || a.Timeout != 0 || b.Timeout != 0 {
		t.Fatal("clients")
	}
	ta, ok := a.Transport.(*http.Transport)
	if !ok || ta == nil {
		t.Fatal("transport")
	}
	tb, ok := b.Transport.(*http.Transport)
	if !ok || tb == nil || ta == tb || a.Transport == http.DefaultTransport || b.Transport == http.DefaultTransport {
		t.Fatal("transport isolation")
	}
	for _, tr := range []*http.Transport{ta, tb} {
		if tr.TLSHandshakeTimeout != cli.TLSHandshakeTimeout || tr.ResponseHeaderTimeout != cli.ResponseHeaderTimeout {
			t.Fatal("bounds")
		}
	}
}

// R-X509-4S81 R-XERG-6Y5L R-X685-IJYQ
func TestRunRejectsBeforeOpening(t *testing.T) {
	if cli.SweepInterval != time.Hour {
		t.Fatal(cli.SweepInterval)
	}
	for _, env := range []map[string]string{{"TRANSFER_MAX_ACTIVE": "0"}, {}, {"LISTEN_PID": "123", "LISTEN_FDS": "2"}, {"LISTEN_PID": "12", "LISTEN_FDS": "1"}, {"LISTEN_PID": "123", "LISTEN_FDS": "nonsense"}} {
		t.Run(strings.Join([]string{env["TRANSFER_MAX_ACTIVE"], env["LISTEN_PID"], env["LISTEN_FDS"]}, "/"), func(t *testing.T) {
			dir := t.TempDir()
			out, stderr := &outputBuffer{}, &outputBuffer{}
			var looked []string
			p := cli.Process{LookupEnv: func(k string) (string, bool) { looked = append(looked, k); v, ok := env[k]; return v, ok }, Pid: 123, Dir: dir, Stdout: out, Stderr: stderr, Version: "test", Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit called"); return nil, nil }, Unsetenv: func(string) error { t.Fatal("unset called"); return nil }}
			if code := cli.Run(context.Background(), p); code != cli.ExitUsage {
				t.Fatal(code)
			}
			want := "gmail: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
			if env["LISTEN_FDS"] == "2" {
				want = "gmail: 2 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"
			}
			if env["TRANSFER_MAX_ACTIVE"] != "" {
				_, err := settings.Read(p.LookupEnv)
				want = "gmail: " + err.Error() + "\n"
				for _, k := range looked {
					if k == "LISTEN_PID" || k == "LISTEN_FDS" || k == "IKIGENBA_SERVICES" {
						t.Fatal(looked)
					}
				}
			}
			if stderr.String() != want || stderr.writes != 1 || out.String() != "" {
				t.Fatalf("out %q err %q writes %d", out.String(), stderr.String(), stderr.writes)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal(entries, err)
			}
		})
	}
}

// R-XH78-YHMZ R-XJN1-Q14D
func TestRunDatabaseRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state"), []byte("regular"), 0600); err != nil {
		t.Fatal(err)
	}
	_, openErr := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "gmail.db"), Migrations: app.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})
	if openErr == nil {
		t.Fatal("fixture unexpectedly opens")
	}
	socketDir, err := os.MkdirTemp("", "gmail-refusal-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(socketDir, "notify"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notify.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	accepts := &atomic.Int32{}
	out, stderr := &outputBuffer{}, &outputBuffer{}
	capture := &telemetry.Capture{}
	p := cli.Process{LookupEnv: func(k string) (string, bool) {
		v, ok := map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String()}[k]
		return v, ok
	}, Pid: 123, Dir: dir, Stdout: out, Stderr: stderr, Version: "test", Inherit: func(uintptr) (net.Listener, error) { return observedListener{Listener: ln, accepts: accepts}, nil }, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, Rand: &runRandom{}, Sink: capture, EventSink: &events.Capture{}}
	if code := cli.Run(context.Background(), p); code != cli.ExitServerFailed {
		t.Fatal(code)
	}
	if accepts.Load() != 0 {
		t.Fatal("accepted before database refusal")
	}
	text, err := lifecycleDatagram(notify)
	if !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("unexpected notification %q: %v", text, err)
	}
	line := stderr.String()
	if line != "gmail: cannot open database state/gmail.db: "+strings.ReplaceAll(openErr.Error(), "\n", " ")+"\n" || strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") || stderr.writes != 1 || out.String() != "" || len(capture.Events()) != 0 {
		t.Fatalf("%q events %#v", line, capture.Events())
	}
}

// R-VIH2-RSMP R-VJOZ-5KDE R-VS89-TYK9 R-XFZC-KPWA
func TestRunProcessAndEnvironment(t *testing.T) {
	var looked, removed []string
	var mu sync.Mutex
	count := 0
	f := startRun(t, runOptions{Modify: func(p *cli.Process) {
		lookup := p.LookupEnv
		inherit := p.Inherit
		p.LookupEnv = func(k string) (string, bool) { mu.Lock(); looked = append(looked, k); mu.Unlock(); return lookup(k) }
		p.Unsetenv = func(k string) error { removed = append(removed, k); return nil }
		p.Inherit = func(fd uintptr) (net.Listener, error) { count++; return inherit(fd) }
	}})
	if r := f.call(fixtureCaller, "transfers", map[string]any{}); r.IsError() {
		t.Fatal(r)
	}
	if code := f.stop(); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if count != 1 || !reflect.DeepEqual(removed, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatal(count, removed)
	}
	allowed := map[string]bool{}
	for _, k := range []string{"DRAIN_SECONDS", "TRANSFER_MAX_BYTES", "TRANSFER_MAX_ACTIVE", "TRANSFER_MAX_QUEUED", "TRANSFER_KEEP_DAYS", "TRANSFER_KEEP_COUNT", "DRAFT_KEEP_DAYS", "DRAFT_KEEP_COUNT", "IKIGENBA_SERVICES", "LISTEN_PID", "LISTEN_FDS", "NOTIFY_SOCKET"} {
		allowed[k] = true
	}
	for _, k := range looked {
		if !allowed[k] {
			t.Fatal(k)
		}
	}
}
func compose(f *runFixture, c identity.Caller) string {
	r := f.call(c, "compose", map[string]any{"to": []string{"recipient@example.test"}, "subject": "subject", "body": "body"})
	if r.IsError() {
		f.t.Fatalf("compose %#v", resultJSON(f.t, r))
	}
	return resultJSON(f.t, r)["id"].(string)
}
func pull(f *runFixture, c identity.Caller, path string) string {
	r := f.call(c, "pull", map[string]any{"message": "message", "attachment": "part", "path": path})
	if r.IsError() {
		f.t.Fatalf("pull %#v", resultJSON(f.t, r))
	}
	return resultJSON(f.t, r)["id"].(string)
}

// R-VPSH-2F2V R-VTG6-7QAY R-VUO2-LI1N R-VX3V-D1J1
func TestRunWiresMailTransfersDraftsAndSinks(t *testing.T) {
	f := startRun(t, runOptions{Rand: fixedRandom(0x5a)})
	if r := f.call(fixtureCaller, "labels", map[string]any{}); r.IsError() {
		t.Fatal(r)
	}
	id := pull(f, fixtureCaller, "pull.bin")
	if id != "xfr_"+strings.Repeat("5a", 8) {
		t.Fatal(id)
	}
	v := f.awaitTransfer(fixtureCaller, id)
	if v["status"] != store.StatusDone {
		t.Fatal(v)
	}
	draft := compose(f, fixtureCaller)
	if draft != "drf_"+strings.Repeat("5a", 8) {
		t.Fatal(draft)
	}
	if code := f.stop(); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	d, err := f.reopen().GetDraft(context.Background(), fixtureCaller.UserID, draft)
	if err != nil || d.ID != draft {
		t.Fatal(d, err)
	}
	cc, ac := f.SiblingRequests()
	if len(cc) == 0 || len(f.Mail.Requests()) == 0 {
		t.Fatal("missing requests")
	}
	puts := 0
	for _, r := range ac {
		if r.Method == "PUT" {
			puts++
			if r.Path != "/files/pull.bin" || r.Header.Get("X-User-Id") != fixtureCaller.UserID || !bytes.Equal(r.Body, []byte("mail")) {
				t.Fatal(r)
			}
		}
	}
	if puts != 1 {
		t.Fatal(puts)
	}
	for _, r := range f.Mail.Requests() {
		if r.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatal(r.Header)
		}
	}
	seen := map[string]bool{}
	es := f.Telemetry.Events()
	for _, e := range es {
		if e.Name == "sibling.called" {
			target, _ := e.Attrs["target"].(string)
			if e.User == fixtureCaller.UserID {
				seen[target] = true
			}
		}
	}
	if !seen["connections"] || !seen["artifacts"] || es[0].Name != "service.started" || es[len(es)-1].Name != "service.stopping" {
		t.Fatal(es)
	}
	bus := f.Events.Events()
	var transfers []string
	for _, e := range bus {
		if strings.HasPrefix(e.Name, "transfer.") {
			transfers = append(transfers, e.Name)
		}
	}
	if !reflect.DeepEqual(transfers, []string{"transfer.started", "transfer.finished"}) {
		t.Fatal(bus)
	}
}

// R-462E-DSQG R-MZRL-M1W7 R-XZ3F-IVYP
func TestRunDiskAndBackgroundIdentity(t *testing.T) {
	f := startRun(t, runOptions{})
	diskOnlyDB(t, f.Dir)
	c := identity.Caller{UserID: "another-owner", Email: "a@example.test", RequestID: "another-request"}
	id := pull(f, c, "pull.bin")
	if v := f.awaitTransfer(c, id); v["status"] != "done" {
		t.Fatal(v)
	}
	draft := compose(f, c)
	r := f.call(c, "push", map[string]any{"path": "push.bin", "draft": draft})
	if r.IsError() {
		t.Fatal(resultJSON(t, r))
	}
	pid := resultJSON(t, r)["id"].(string)
	if v := f.awaitTransfer(c, pid); v["status"] != "done" {
		t.Fatal(v)
	}
	diskOnlyDB(t, f.Dir)
	if f.stop() != cli.ExitSuccess {
		t.Fatal("exit")
	}
	diskOnlyDB(t, f.Dir)
	cc, ac := f.SiblingRequests()
	get := false
	put := false
	for _, r := range cc {
		assertIdentity(t, r, c)
	}
	for _, r := range ac {
		assertIdentity(t, r, c)
		if r.Path == "/files/push.bin" && r.Method == "GET" {
			get = true
		}
		if r.Path == "/files/pull.bin" && r.Method == "PUT" {
			put = true
		}
	}
	if !get || !put {
		t.Fatal(ac)
	}
}
func diskOnlyDB(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "state" || !entries[0].IsDir() {
		t.Fatal(entries, err)
	}
	entries, err = os.ReadDir(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "gmail.db") || !e.Type().IsRegular() {
			t.Fatal(e)
		}
		found = found || e.Name() == "gmail.db"
	}
	if !found {
		t.Fatal("missing db")
	}
}
func assertIdentity(t *testing.T, r observedRequest, c identity.Caller) {
	t.Helper()
	for k, v := range map[string]string{"X-User-Id": c.UserID, "X-User-Email": c.Email, "X-Request-Id": c.RequestID} {
		if !reflect.DeepEqual(r.Header.Values(k), []string{v}) {
			t.Fatal(r, k, v)
		}
	}
}

// R-N0ZH-ZTMW
func TestRunForwardsCallerForForegroundTools(t *testing.T) {
	f := startRun(t, runOptions{})
	for _, u := range []string{"first", "second"} {
		c := identity.Caller{UserID: u, Email: u + "@example.test", RequestID: u + "-request"}
		for _, name := range []string{"labels", "search", "show", "compose", "send", "discard"} {
			var args any = map[string]any{}
			if name == "search" {
				args = map[string]any{"query": "mail"}
			}
			if name == "show" {
				args = map[string]any{"message": "message"}
			}
			if name == "compose" {
				args = map[string]any{"to": []string{"recipient@example.test"}}
			}
			if name == "send" || name == "discard" {
				args = map[string]any{"draft": compose(f, c)}
			}
			before, _ := f.SiblingRequests()
			r := f.call(c, name, args)
			if r.IsError() {
				t.Fatalf("%s %s %#v", u, name, resultJSON(t, r))
			}
			after, _ := f.SiblingRequests()
			if len(after) <= len(before) {
				t.Fatal(name)
			}
			for _, req := range after[len(before):] {
				if req.Path != "/token/google" || req.Method != "GET" {
					t.Fatal(req)
				}
				assertIdentity(t, req, c)
			}
		}
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("exit")
	}
}

// R-H1HG-JZFW R-H2PC-XR6L
func TestRunMissingSiblings(t *testing.T) {
	for _, missingConnections := range []bool{true, false} {
		t.Run(map[bool]string{true: "connections", false: "artifacts"}[missingConnections], func(t *testing.T) {
			f := startRun(t, runOptions{MissingConnections: missingConnections, MissingArtifacts: !missingConnections})
			names := []string{"pull", "push"}
			if missingConnections {
				names = []string{"labels", "compose", "pull"}
			}
			for _, name := range names {
				args := toolArgs(name)
				assertRefusal(t, f.call(fixtureCaller, name, args), "unavailable: "+tools.RefuseUnavailable)
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("exit")
			}
			lifecycleEqual(t, f.Out.String(), "")
			lifecycleEqual(t, f.Err.String(), "")
			s := f.reopen()
			ts, e := s.ListTransfers(context.Background(), fixtureCaller.UserID)
			if e != nil || len(ts) != 0 {
				t.Fatal(ts, e)
			}
			ds, e := s.ListDrafts(context.Background(), fixtureCaller.UserID)
			if e != nil || len(ds) != 0 {
				t.Fatal(ds, e)
			}
			if len(f.Mail.Requests()) != 0 {
				t.Fatal(f.Mail.Requests())
			}
			for _, e := range f.Telemetry.Events() {
				if e.Name == "gmail.called" || e.Name == "sibling.called" && (missingConnections || e.Attrs["target"] == "artifacts") {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-H3X9-BIXA R-X8NY-A3G4
func TestRunUnreadableServicesStillServes(t *testing.T) {
	for _, mode := range []string{"unset", "empty", "absent", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			p := ""
			if mode != "unset" && mode != "empty" {
				p = filepath.Join(t.TempDir(), "services")
				if mode == "invalid" {
					if err := os.WriteFile(p, []byte("invalid"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			f := startRun(t, runOptions{ServicesPath: &p, Modify: func(process *cli.Process) {
				if mode == "unset" {
					lookup := process.LookupEnv
					process.LookupEnv = func(key string) (string, bool) {
						if key == services.Variable {
							return "", false
						}
						return lookup(key)
					}
				}
			}})
			if r := f.call(fixtureCaller, "transfers", map[string]any{}); r.IsError() {
				t.Fatal(r)
			}
			assertRefusal(t, f.call(fixtureCaller, "labels", map[string]any{}), "unavailable: "+tools.RefuseUnavailable)
			if f.stop() != cli.ExitSuccess {
				t.Fatal("exit")
			}
			lifecycleEqual(t, f.Out.String(), "")
			lifecycleEqual(t, f.Err.String(), "")
			if len(f.Mail.Requests()) != 0 {
				t.Fatal(f.Mail.Requests())
			}
			for _, e := range f.Telemetry.Events() {
				if e.Name == "sibling.called" || e.Name == "gmail.called" {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-YOBU-GHYE
func TestRunConnectURLComesFromServices(t *testing.T) {
	for _, suffix := range []string{"", "/"} {
		t.Run(suffix, func(t *testing.T) {
			f := startRun(t, runOptions{ConnectionsURL: "https://connect.custom" + suffix, ConnectionsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(404)
				_, _ = io.WriteString(w, `{"error":"not_connected"}`)
			})})
			f.Client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.Base + "/mcp", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r = r.Clone(r.Context())
				r.Host = "arbitrary.invalid"
				r.Header.Set("X-Forwarded-Proto", "ftp")
				return http.DefaultTransport.RoundTrip(r)
			})}, Name: "test", Version: "test"})
			for _, name := range []string{"search", "pull", "compose"} {
				r := f.call(fixtureCaller, name, toolArgs(name))
				text := resultText(t, r)
				if !r.IsError() || !strings.HasPrefix(text, "not_connected: ") || !strings.HasSuffix(text, " https://connect.custom/google/connect") {
					t.Fatal(text)
				}
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("exit")
			}
		})
	}
}

type unsafeRandom struct {
	active  atomic.Int32
	overlap atomic.Bool
	n       byte
}

func (r *unsafeRandom) Read(p []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

// R-VM4R-X3US
func TestRunSharesOneRandomLock(t *testing.T) {
	r := &unsafeRandom{}
	f := startRun(t, runOptions{Rand: r})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := identity.Caller{UserID: fixtureCaller.UserID, Email: fixtureCaller.Email}
			compose(f, c)
			pull(f, c, fmt.Sprint("concurrent/", i))
		}(i)
	}
	wg.Wait()
	if f.stop() != cli.ExitSuccess {
		t.Fatal("exit")
	}
	if r.overlap.Load() {
		t.Fatal("concurrent random reads")
	}
}

func toolArgs(name string) map[string]any {
	switch name {
	case "search":
		return map[string]any{"query": "mail"}
	case "pull":
		return map[string]any{"message": "message", "attachment": "part", "path": "pull.bin"}
	case "push":
		return map[string]any{"path": "push.bin", "draft": "drf_" + strings.Repeat("5a", 8)}
	case "compose":
		return map[string]any{"to": []string{"recipient@example.test"}}
	default:
		return map[string]any{}
	}
}

// R-VKWV-JC43 R-VYBR-QT9Q
func TestRunDefaultsDirectoryAndSocketSinks(t *testing.T) {
	for _, defaults := range []bool{true, false} {
		t.Run(map[bool]string{true: "socket", false: "injected"}[defaults], func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			now := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
			f := startRun(t, runOptions{Dir: dir, DefaultSinks: defaults, Now: func() time.Time { return now }, Modify: func(p *cli.Process) { p.Dir = ""; p.Unsetenv = nil }})
			id := pull(f, fixtureCaller, "pull.bin")
			if v := f.awaitTransfer(fixtureCaller, id); v["status"] != "done" {
				t.Fatal(v)
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("exit")
			}
			s := f.reopen()
			tr, err := s.GetTransfer(context.Background(), fixtureCaller.UserID, id)
			if err != nil || !tr.Started.Equal(now) || !tr.Finished.Equal(now) {
				t.Fatal(tr, err)
			}
			es := f.Telemetry.Events()
			if len(es) == 0 || es[0].Name != "service.started" || es[len(es)-1].Name != "service.stopping" {
				t.Fatal(es)
			}
			for _, e := range es {
				if !e.Time.Equal(now) {
					t.Fatal(e)
				}
			}
			bus := f.Events.Events()
			if len(bus) != 2 || bus[0].Name != "transfer.started" || bus[1].Name != "transfer.finished" {
				t.Fatal(bus)
			}
			for _, e := range bus {
				if !e.Time.Equal(now) {
					t.Fatal(e)
				}
			}
		})
	}
}

type observedListener struct {
	net.Listener
	accepts *atomic.Int32
}

func (l observedListener) Accept() (net.Conn, error) { l.accepts.Add(1); return l.Listener.Accept() }
