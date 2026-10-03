package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

// R-PV6R-OUIR R-Q3Q2-D8PM R-KV7D-86WG
func TestServeUnsetLookupValuesAreIgnored(t *testing.T) {
	t.Run("PATH", func(t *testing.T) {
		f := newServeFixture(t)
		f.listen(t)
		lookup := f.p.LookupEnv
		f.p.LookupEnv = func(k string) (string, bool) {
			if k == "PATH" {
				v, _ := lookup(k)
				return v, false
			}
			return lookup(k)
		}
		if code := cli.Run(t.Context(), f.p); code != cli.ExitServerFailed || f.stderr.text() != "repos: git not found on PATH\n" {
			t.Fatalf("unset PATH respected false value: %d %q", code, f.stderr.text())
		}
		assertEmptyDirectory(t, f.dir)
	})
	f := newServeFixture(t)
	f.notification(t)
	f.listen(t)
	original := filepath.Join(f.dir, "unpublished-services")
	writeServeServices(t, original, "https://unset.example")
	f.set("IKIGENBA_SERVICES", original)
	lookup := f.p.LookupEnv
	f.p.LookupEnv = func(k string) (string, bool) {
		v, ok := lookup(k)
		if k == "NOTIFY_SOCKET" || k == "IKIGENBA_SERVICES" {
			return v, false
		}
		return v, ok
	}
	started := make(chan struct{})
	var once sync.Once
	f.p.Sink = serveSink(t, func(ctx context.Context, e telemetry.Event) error {
		if e.Name == "service.started" {
			once.Do(func() { close(started) })
		}
		return f.capture.Deliver(ctx, e)
	})
	f.launch(t)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("unset notify did not start")
	}
	noServeNotification(t, f.notify)
	created := f.tool(t, "create", `{"name":"alpha"}`)
	if url := created["clone_url"].(string); strings.HasPrefix(url, "https://unset.example") {
		t.Fatalf("unset services path used: %s", url)
	}
	f.stop(t, "unset stop", cli.ExitSuccess)
}

// R-Q3Q2-D8PM
func TestServeAbstractNotificationExactlyOnce(t *testing.T) {
	f := newServeFixture(t)
	f.notification(t)
	_ = f.notify.Close()
	address := "@" + f.env["NOTIFY_SOCKET"]
	var err error
	f.notify, err = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	f.set("NOTIFY_SOCKET", address)
	f.listen(t)
	f.launch(t)
	f.ready(t)
	f.request(t, "/", "abstract")
	f.stop(t, "abstract stop", cli.ExitSuccess)
	noServeNotification(t, f.notify)
}

// R-4D5L-S59I R-KP3V-BC6Z
func TestServeCancellationDuringVerificationFinishesOnlyVerificationEvents(t *testing.T) {
	f := newServeFixture(t)
	g, err := git.Find(filepath.Dir(f.gitPath), f.p.Environ)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.Context(), store.Config{Source: filepath.Join(f.dir, "state", "repos.db"), Root: filepath.Join(f.dir, "state", "repos"), Git: g, Now: f.p.Now, Rand: f.random})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := s.Create(t.Context(), "owner", "broken")
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
	ctx, cancel := context.WithCancelCause(t.Context())
	fixedNow := f.p.Now
	f.p.Now = func() time.Time { cancel(errors.New("verify signal")); return fixedNow() }
	f.p.Sink = serveSink(t, func(context.Context, telemetry.Event) error { return telemetry.ErrRejected })
	if code := cli.Run(ctx, f.p); code != cli.ExitSuccess {
		t.Fatalf("verification cancellation exit %d", code)
	}
	if f.mcpCalls.Load() != 0 || f.bannerCalls.Load() != 0 || f.stdout.text() != "" {
		t.Fatalf("cancelled verify started service")
	}
	noServeNotification(t, f.notify)
	lines := f.stderr.lines()
	if len(lines) != 1 || !strings.Contains(lines[0], `"event":"repo.unavailable"`) || !strings.Contains(lines[0], repo.ID) {
		t.Fatalf("verification events not finished before return: %q", f.stderr.text())
	}
}

// R-EBWA-KLXK R-QJKR-C9CN R-RAID-H7PG
func TestServeMaintenanceWiringAndStopLast(t *testing.T) {
	f := newServeFixture(t)
	finished := make(chan telemetry.Event, 1)
	f.p.Sink = serveSink(t, func(ctx context.Context, e telemetry.Event) error {
		_ = f.capture.Deliver(ctx, e)
		if e.Name == "maintenance.finished" {
			finished <- e
		}
		return nil
	})
	f.start(t)
	interval := takeServeTimer(t, f, 24*time.Hour)
	created := f.tool(t, "create", `{"name":"alpha"}`)
	interval.ch <- f.p.Now()
	next := takeServeTimer(t, f, 24*time.Hour)
	takeServeTimer(t, f, 600*time.Second)
	select {
	case e := <-finished:
		if e.Attrs["repo"] != created["id"] || e.RequestID != "" || e.User != "" || !e.Time.Equal(f.p.Now().UTC().Truncate(time.Microsecond)) {
			t.Fatalf("maintenance wiring %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("injected interval did not run real maintenance")
	}
	f.stop(t, "maintenance stop", cli.ExitSuccess)
	events := f.capture.Events()
	if events[len(events)-1].Name != "service.stopping" {
		t.Fatalf("maintenance recorded after stopping: %v", events)
	}
	assertServeScheduleStopped(t, f, next)
}

// R-QS42-0NJI
func TestServeEmptyFirstRequestIDDrawsInjectedBytes(t *testing.T) {
	f := newServeFixture(t)
	f.start(t)
	r, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+f.listener.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-User-Id", "owner")
	r.Header["X-Request-Id"] = []string{"", "ignored-second-value"}
	resp, err := f.client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	f.stop(t, "empty id", cli.ExitSuccess)
	count := 0
	for _, e := range f.capture.Events() {
		if strings.HasPrefix(e.Name, "request.") {
			count++
			if e.RequestID != strings.Repeat("01", 16) {
				t.Fatalf("empty-first request id %q", e.RequestID)
			}
		}
	}
	if count != 2 {
		t.Fatalf("request events %d", count)
	}
}

// R-SE0J-BHLH
func TestServeDirOverridesPrivateWorkingDirectory(t *testing.T) {
	f := newServeFixture(t)
	other := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	}()
	f.start(t)
	f.tool(t, "create", `{"name":"alpha"}`)
	f.stop(t, "private cwd", cli.ExitSuccess)
	assertEmptyDirectory(t, other)
	for _, path := range []string{"state/repos.db", "state/repos"} {
		if _, err = os.Stat(filepath.Join(f.p.Dir, path)); err != nil {
			t.Fatal(err)
		}
	}
}

// R-RAID-H7PG R-KV7D-86WG R-KNVY-XKGA
func TestServeUsesInjectedBannerAndMCPServerResults(t *testing.T) {
	f := newServeFixture(t)
	f.p.Banner = func(u page.User) page.Banner {
		f.bannerCalls.Add(1)
		if u.Email != "caller@example.test" {
			t.Errorf("banner user %+v", u)
		}
		return page.Banner{Service: "injected-banner", Version: cli.Version, Email: u.Email}
	}
	f.p.MCP = func(w *telemetry.Writer) *mcp.Server {
		f.mcpCalls.Add(1)
		f.writer.Store(w)
		return mcp.NewServer(mcp.ServerConfig{Name: "injected-mcp", Version: cli.Version, Telemetry: w, Instructions: func(context.Context) string { return "injected instructions" }})
	}
	f.start(t)
	r, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+f.listener.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-User-Id", "owner")
	r.Header.Set("X-User-Email", "caller@example.test")
	resp, err := f.client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.Contains(string(body), "injected-banner "+cli.Version) || f.bannerCalls.Load() != 1 {
		t.Fatalf("injected banner not rendered: %d %s", resp.StatusCode, body)
	}
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://" + f.listener.Addr().String() + "/mcp", HTTPClient: f.client()})
	result, err := client.CallTool(t.Context(), identity.Caller{UserID: "owner", RequestID: "injected-mcp-request"}, "list", nil)
	if err != nil || result.IsError() {
		t.Fatalf("injected MCP result %v %v", result, err)
	}
	raw, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var info map[string]string
	if err = json.Unmarshal(envelope.Meta["io.modelcontextprotocol/serverInfo"], &info); err != nil {
		t.Fatal(err)
	}
	if len(info) != 2 || info["name"] != "injected-mcp" || info["version"] != cli.Version {
		t.Fatalf("MCP factory result ignored: %s", raw)
	}
	f.stop(t, "injected outputs", cli.ExitSuccess)
	if f.mcpCalls.Load() != 1 {
		t.Fatalf("MCP factory calls %d", f.mcpCalls.Load())
	}
}

// R-4D5L-S59I
func TestServeDoneContextOverridesUnopenableState(t *testing.T) {
	f := newServeFixture(t)
	if err := os.WriteFile(filepath.Join(f.dir, "state"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	f.notification(t)
	f.listen(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := cli.Run(ctx, f.p); code != cli.ExitSuccess || f.stderr.text() != "" || f.stdout.text() != "" {
		t.Fatalf("cancelled open failure %d %q %q", code, f.stderr.text(), f.stdout.text())
	}
	noServeNotification(t, f.notify)
	body, err := os.ReadFile(filepath.Join(f.dir, "state"))
	if err != nil || string(body) != "unchanged" {
		t.Fatalf("state altered: %q %v", body, err)
	}
}
