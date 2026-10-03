package cli_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

// R-SE0J-BHLH R-PYUG-U5QU R-SF8F-P9C6 R-KNVY-XKGA R-KV7D-86WG
// R-7ATG-PX7A R-RAID-H7PG
func TestServePersistentStoreAndInjectedWiring(t *testing.T) {
	f := newServeFixture(t)
	f.start(t)
	if f.mcpCalls.Load() != 1 {
		t.Fatalf("MCP calls %d", f.mcpCalls.Load())
	}
	for _, name := range []string{"state", "state/repos.db", "state/repos"} {
		info, err := os.Stat(filepath.Join(f.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "state/repos.db" && !info.Mode().IsRegular() {
			t.Fatal("catalog not regular")
		}
		if name != "state/repos.db" && !info.IsDir() {
			t.Fatal("state/root not directory")
		}
	}
	assertEmptyDirectory(t, filepath.Join(f.dir, "state", "repos"))
	if repos := f.tool(t, "list", "{}")["repos"].([]any); len(repos) != 0 {
		t.Fatalf("initial repos %v", repos)
	}
	created := f.tool(t, "create", `{"name":"alpha"}`)
	if created["name"] != "alpha" {
		t.Fatalf("create %v", created)
	}
	status, _, body := f.request(t, "/", "page")
	if status != 200 || !bytes.Contains(body, []byte(web.ServiceName+" "+cli.Version)) || f.bannerCalls.Load() != 1 {
		t.Fatalf("page status %d banner calls %d body %s", status, f.bannerCalls.Load(), body)
	}
	status, headers, _ := f.request(t, "/alpha.git/info/refs?service=git-upload-pack", "fetch")
	if status != 200 || headers.Get("Content-Type") != "application/x-git-upload-pack-advertisement" {
		t.Fatalf("git response %d %v", status, headers)
	}
	if f.envCalls.Load() == 0 {
		t.Fatal("real git did not use injected environment")
	}
	f.stop(t, "first stop", cli.ExitSuccess)
	if f.mcpCalls.Load() != 1 {
		t.Fatalf("MCP factory called after startup: %d", f.mcpCalls.Load())
	}
	if f.stdout.text() != "" || f.stderr.text() != "" {
		t.Fatalf("successful run streams %q %q", f.stdout.text(), f.stderr.text())
	}
	events := f.capture.Events()
	if events[0].Name != "service.started" || events[len(events)-1].Name != "service.stopping" {
		t.Fatalf("lifecycle %v", events)
	}
	wantTime := f.p.Now().UTC().Truncate(time.Microsecond)
	for _, e := range events {
		if e.Service != web.ServiceName || !e.Time.Equal(wantTime) {
			t.Fatalf("injected writer envelope %+v", e)
		}
	}
	// A later Run over exactly the same directory must reopen the catalog and git.
	f2 := newServeFixture(t, f.gitPath)
	f2.dir = f.dir
	f2.p.Dir = f.dir
	f2.start(t)
	repos := f2.tool(t, "list", "{}")["repos"].([]any)
	if len(repos) != 1 || repos[0].(map[string]any)["name"] != "alpha" {
		t.Fatalf("persisted repositories %v", repos)
	}
	status, _, _ = f2.request(t, "/alpha.git/info/refs?service=git-upload-pack", "again")
	if status != 200 {
		t.Fatalf("persisted git %d", status)
	}
	f2.stop(t, "second stop", cli.ExitSuccess)
}

// R-Q3Q2-D8PM R-KQBR-P3XO R-KP3V-BC6Z R-KNVY-XKGA R-EBWA-KLXK
func TestServeReadyVerifyAndMaintenanceBeforeFirstAccept(t *testing.T) {
	f := newServeFixture(t)
	g, err := git.Find(filepath.Dir(f.gitPath), f.p.Environ)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.Context(), store.Config{Source: filepath.Join(f.dir, "state", "repos.db"), Root: filepath.Join(f.dir, "state", "repos"), Git: g, Now: f.p.Now, Rand: f.random})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := s.Create(t.Context(), "owner", "damaged")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(filepath.Join(f.dir, "state", "repos", repo.ID+".git")); err != nil {
		t.Fatal(err)
	}
	f.notification(t)
	f.listen(t)
	underlying := f.listener
	checked := make(chan struct{})
	var once sync.Once
	f.p.Inherit = func(uintptr) (net.Listener, error) {
		return &serveListener{Listener: underlying, accept: func() (net.Conn, error) {
			once.Do(func() {
				f.ready(t)
				f.flush(t)
				events := f.capture.Events()
				if len(events) != 2 || events[0].Name != "repo.unavailable" || events[0].Attrs["repo"] != repo.ID || events[1].Name != "service.started" {
					t.Errorf("events before Accept: %+v", events)
				}
				if events[1].RequestID != "" || events[1].User != "" || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"version": cli.Version}) {
					t.Errorf("start envelope %+v", events[1])
				}
				if f.mcpCalls.Load() != 1 {
					t.Errorf("MCP not built before accept")
				}
				timer := takeServeTimer(t, f, 24*time.Hour)
				timer.ch <- f.p.Now()
				close(checked)
			})
			return underlying.Accept()
		}}, nil
	}
	f.launch(t)
	select {
	case <-checked:
	case <-time.After(5 * time.Second):
		t.Fatal("first Accept not reached")
	}
	if status, _, _ := f.request(t, "/", "checked"); status != 200 {
		t.Fatalf("status %d", status)
	}
	next := takeServeTimer(t, f, 24*time.Hour)
	f.stop(t, "verify stop", cli.ExitSuccess)
	noServeNotification(t, f.notify)
	assertServeScheduleStopped(t, f, next)
}

// R-Q4XY-R0GB
func TestServeNotificationFailureHasNoStartedEventOrAccept(t *testing.T) {
	f := newServeFixture(t)
	f.set("NOTIFY_SOCKET", filepath.Join(f.dir, "missing.sock"))
	f.listen(t)
	underlying := f.listener
	f.p.Inherit = func(uintptr) (net.Listener, error) {
		return &serveListener{Listener: underlying, accept: func() (net.Conn, error) {
			t.Error("accept on failed notification")
			return nil, errors.New("unexpected")
		}}, nil
	}
	address, _ := f.p.LookupEnv("NOTIFY_SOCKET")
	conn, expectedError := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if conn != nil {
		_ = conn.Close()
	}
	if expectedError == nil {
		t.Fatal("notification failure fixture unexpectedly connected")
	}

	if code := cli.Run(t.Context(), f.p); code != cli.ExitServerFailed {
		t.Fatalf("exit %d", code)
	}
	if f.stderr.text() != "repos: "+expectedError.Error()+"\n" || len(f.stderr.lines()) != 1 || f.stdout.text() != "" || len(f.capture.Events()) != 0 {
		t.Fatalf("notification failure streams %q %q events %v", f.stdout.text(), f.stderr.text(), f.capture.Events())
	}
}

// R-KXN5-ZQDU R-KV7D-86WG
func TestServeServicesStartToleranceAndCapturedPath(t *testing.T) {
	for _, value := range []string{"", "missing", "malformed", "directory"} {
		t.Run(value, func(t *testing.T) {
			f := newServeFixture(t)
			path := filepath.Join(f.dir, value)
			switch value {
			case "":
				path = ""
			case "malformed":
				if err := os.WriteFile(path, []byte("invalid services"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			f.set("IKIGENBA_SERVICES", path)
			f.start(t)
			if status, _, _ := f.request(t, "/", "services"); status != 200 {
				t.Fatalf("status %d", status)
			}
			f.stop(t, "services stop", cli.ExitSuccess)
			if f.stderr.text() != "" || f.stdout.text() != "" {
				t.Fatalf("services caused diagnostic: %q", f.stderr.text())
			}
		})
	}
	f := newServeFixture(t)
	original := filepath.Join(f.dir, "original-services")
	later := filepath.Join(f.dir, "later-services")
	writeServeServices(t, original, "https://original.example")
	writeServeServices(t, later, "https://ignored.example")
	f.set("IKIGENBA_SERVICES", original)
	f.start(t)
	f.set("IKIGENBA_SERVICES", later)
	if got := f.tool(t, "create", `{"name":"alpha"}`)["clone_url"]; got != "https://original.example/alpha.git" {
		t.Fatalf("services lookup was not captured: %v", got)
	}
	writeServeServices(t, original, "https://refreshed.example")
	if got := f.tool(t, "show", `{"repo":"alpha"}`)["clone_url"]; got != "https://refreshed.example/alpha.git" {
		t.Fatalf("captured path stopped rereading: %v", got)
	}
	f.stop(t, "snapshot stop", cli.ExitSuccess)
}
func writeServeServices(t *testing.T, path, url string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"services": []any{map[string]any{"name": "repos", "url": url, "description": "fixture repos", "socket": "", "enabled": true, "mcp": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

// R-QKSN-Q13C R-QPO9-9424 R-QS42-0NJI R-RAEJ-R7NX
func TestServeUndeliveredConcurrentEventsAndSharedRandom(t *testing.T) {
	f := newServeFixture(t)
	f.p.Sink = serveSink(t, func(ctx context.Context, e telemetry.Event) error {
		_ = f.capture.Deliver(ctx, e)
		return telemetry.ErrRejected
	})
	f.start(t)
	f.flush(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.request(t, "/", "") }()
	}
	// Tool creation also draws from the same source as request ids.
	f.tool(t, "create", `{"name":"alpha"}`)
	wg.Wait()
	f.flush(t)
	f.stop(t, "offline stop", cli.ExitSuccess)
	if f.random.overlap.Load() || f.stderr.overlap.Load() || f.stderr.late.Load() {
		t.Fatalf("overlap/late read or write")
	}
	counts := map[string]int{}
	ids := map[string]int{}
	captured := f.capture.Events()
	for index, line := range f.stderr.lines() {
		if index >= len(captured) {
			t.Fatalf("uncaptured fallback %q", line)
		}
		canonical, err := captured[index].MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if line != "repos: undelivered event: "+string(canonical)+"\n" {
			t.Fatalf("fallback differs from Event.MarshalJSON: %q", line)
		}
		if !strings.HasPrefix(line, "repos: undelivered event: ") || !strings.HasSuffix(line, "\n") {
			t.Fatalf("fallback not one complete write: %q", line)
		}
		var e struct {
			Name      string         `json:"event"`
			User      string         `json:"user"`
			RequestID string         `json:"request_id"`
			Attrs     map[string]any `json:"attrs"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "repos: undelivered event: ")), &e); err != nil {
			t.Fatal(err)
		}
		assertServeEventName(t, e.Name)
		counts[e.Name]++
		if strings.HasPrefix(e.Name, "request.") && e.User != "owner" {
			t.Fatalf("request user %q", e.User)
		}
		if e.Name == "request.started" && e.RequestID != "tool-create" {
			b, err := hex.DecodeString(e.RequestID)
			if err != nil || len(b) != 16 {
				t.Fatalf("random id %q", e.RequestID)
			}
			for _, v := range b {
				if v != b[0] {
					t.Fatalf("request id not bytes read: %x", b)
				}
			}
			ids[e.RequestID]++
		}
	}
	if counts["service.started"] != 1 || counts["service.stopping"] != 1 || counts["request.started"] != 13 || counts["request.finished"] != 13 || len(ids) != 12 {
		t.Fatalf("fallback counts %v ids %v", counts, ids)
	}
	if f.stdout.text() != "" {
		t.Fatalf("stdout %q", f.stdout.text())
	}
}

type serveTemporary struct{}

func (serveTemporary) Error() string   { return "retry accept" }
func (serveTemporary) Temporary() bool { return true }
func (serveTemporary) Timeout() bool   { return false }

// R-QQW5-MVST R-KSRK-GNF2
func TestServeAcceptFailuresRetriesAndNoDefaultLogger(t *testing.T) {
	f := newServeFixture(t)
	f.notification(t)
	f.listen(t)
	underlying := f.listener
	var once sync.Once
	f.p.Inherit = func(uintptr) (net.Listener, error) {
		return &serveListener{Listener: underlying, accept: func() (net.Conn, error) {
			first := false
			once.Do(func() { first = true })
			if first {
				return nil, serveTemporary{}
			}
			return underlying.Accept()
		}}, nil
	}
	f.p.Banner = func(page.User) page.Banner { panic("controlled banner failure") }
	var logger bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logger)
	t.Cleanup(func() { log.SetOutput(old) })
	f.launch(t)
	f.ready(t)
	r, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+f.listener.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-User-Id", "owner")
	if resp, err := f.client().Do(r); err == nil {
		_ = resp.Body.Close()
	}
	f.stop(t, "panic stop", cli.ExitSuccess)
	if logger.Len() != 0 {
		t.Fatalf("default logger %q", logger.String())
	}

	broken := newServeFixture(t, f.gitPath)
	broken.notification(t)
	broken.listen(t)
	ln := broken.listener
	broken.p.Inherit = func(uintptr) (net.Listener, error) {
		return &serveListener{Listener: ln, accept: func() (net.Conn, error) { return nil, errors.New("accept terminal") }}, nil
	}
	broken.launch(t)
	broken.ready(t)
	select {
	case code := <-broken.result:
		broken.stopped = true
		if code != cli.ExitServerFailed {
			t.Fatalf("accept exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accept failure did not stop")
	}
	writes := broken.stderr.lines()
	if len(writes) == 0 || writes[len(writes)-1] != "repos: accept terminal\n" || broken.stdout.text() != "" {
		t.Fatalf("accept failure output %q", broken.stderr.text())
	}
}
