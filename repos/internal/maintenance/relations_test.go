package maintenance_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	busevents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func relationServer(t *testing.T, f *fixtureConfig, body func(*http.Request)) *httptest.Server {
	t.Helper()
	bus := busevents.New(busevents.Config{Service: "repos", Sink: &busevents.Capture{}, Stderr: io.Discard, Now: f.clock.Now, Rand: bytes.NewReader(make([]byte, 4096)), Telemetry: f.cfg.Telemetry, Emits: smarthttp.Emits()})
	t.Cleanup(func() { bus.Shutdown(context.Background()) })
	h := telemetry.Middleware(f.cfg.Telemetry, identity.Require(smarthttp.Handler(smarthttp.Config{
		Store: f.st, Git: f.g, Limits: f.lim, Telemetry: f.cfg.Telemetry, Events: bus,
	})))
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body != nil && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-receive-pack") {
			body(r)
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func relationClient(t *testing.T, f *fixtureConfig, dir, request string, args ...string) {
	t.Helper()
	args = append([]string{"-c", "http.extraHeader=X-User-Id: owner", "-c", "http.extraHeader=X-Request-Id: " + request}, args...)
	gitOut(t, f.g, dir, args...)
}

type relationResult struct {
	out []byte
	err error
}

func relationPush(t *testing.T, f *fixtureConfig, dir, url string) <-chan relationResult {
	t.Helper()
	cmd := f.g.Command(testContext(t), dir, nil, "-c", "http.extraHeader=X-User-Id: owner", "-c", "http.extraHeader=X-Request-Id: push", "push", url, "HEAD:refs/heads/"+store.DefaultBranch)
	done := make(chan relationResult, 1)
	go func() { out, err := cmd.CombinedOutput(); done <- relationResult{out, err} }()
	return done
}

func relationPushSucceeded(t *testing.T, done <-chan relationResult) {
	t.Helper()
	r := receive(t, done)
	if r.err != nil {
		t.Fatalf("push: %v: %s", r.err, r.out)
	}
}

func relationWorking(t *testing.T, f *fixtureConfig, s *httptest.Server) (string, string) {
	t.Helper()
	dir := filepath.Join(f.dir, "working")
	relationClient(t, f, f.dir, "setup", "clone", s.URL+"/a.git", dir)
	writeFile(t, filepath.Join(dir, "file"), "pushed content\n")
	gitOut(t, f.g, dir, "add", "file")
	gitOut(t, f.g, dir, "commit", "-m", "pushed fixture")
	return dir, strings.TrimSpace(gitOut(t, f.g, dir, "rev-parse", "HEAD"))
}

func relationHold(t *testing.T, f *fixtureConfig) (<-chan string, <-chan struct{}, func()) {
	t.Helper()
	entered, returned, gate := make(chan string, 1), make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	f.cfg.Hold = func(ctx context.Context, id string) {
		entered <- id
		select {
		case <-gate:
		case <-ctx.Done():
		}
		close(returned)
	}
	return entered, returned, release
}

func relationDrainTimers(f *fixtureConfig) {
	for {
		select {
		case <-f.clock.requests:
		default:
			return
		}
	}
}

func relationEventOrder(t *testing.T, f *fixtureConfig, first, second string) {
	t.Helper()
	a, b := -1, -1
	for i, e := range repoEvents(t, f, f.repos[0].ID) {
		if e.Name == first {
			a = i
		}
		if e.Name == second {
			b = i
		}
	}
	if a < 0 || b <= a {
		t.Fatalf("event order %s then %s: %+v", first, second, repoEvents(t, f, f.repos[0].ID))
	}
}

func TestCloneCompletesWhileMaintenanceHeld(t *testing.T) {
	// R-HAQ7-LUBC
	f := fixture(t, 1)
	s := relationServer(t, f, nil)
	entered, returned, release := relationHold(t, f)
	cycle := runCycle(testContext(t), f.cfg)
	if id := receive(t, entered); id != f.repos[0].ID {
		t.Fatalf("Hold repository %q", id)
	}
	p := f.lim.Pressure()
	if p.Write.Active != 1 || p.Read.Active != 0 || !f.lim.Busy(f.repos[0].ID) {
		t.Fatalf("maintenance grant/read slot: %+v", p)
	}
	want := strings.TrimSpace(gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "rev-parse", "HEAD"))
	dir := filepath.Join(f.dir, "cloned")
	relationClient(t, f, f.dir, "clone", "clone", s.URL+"/a.git", dir)
	if got := strings.TrimSpace(gitOut(t, f.g, dir, "rev-parse", "HEAD")); got != want {
		t.Fatalf("clone HEAD %q, want %q", got, want)
	}
	if got := gitOut(t, f.g, dir, "status", "--porcelain"); got != "" {
		t.Fatalf("clone working tree: %q", got)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	content, err := root.ReadFile("file")
	if closeErr := root.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || string(content) != "fixture\n" {
		t.Fatalf("clone working-tree content %q: %v", content, err)
	}
	absent(t, returned)
	absent(t, cycle)
	for _, e := range events(t, f) {
		if e.RequestID == "clone" && e.Name == "operation.waited" {
			t.Fatalf("clone waited: %+v", e)
		}
	}
	release()
	receive(t, cycle)
	assertIdle(t, f)
}

func TestPushQueuesBehindMaintenance(t *testing.T) {
	// R-HBY3-ZM21
	f := fixture(t, 1)
	s := relationServer(t, f, nil)
	dir, want := relationWorking(t, f, s)
	relationDrainTimers(f)
	entered, returned, release := relationHold(t, f)
	cycle := runCycle(testContext(t), f.cfg)
	receive(t, entered)
	before := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "show-ref")
	push := relationPush(t, f, dir, s.URL+"/a.git")
	// The advertisement has no lock; the receive-pack POST waits on it.
	timer(t, f.clock, 23*time.Second)
	timer(t, f.clock, 17*time.Second)
	p := f.lim.Pressure()
	if p.Write.Active != 1 || p.Write.Queued != 1 || !f.lim.Busy(f.repos[0].ID) {
		t.Fatalf("push not queued behind maintenance: %+v", p)
	}
	absent(t, returned)
	absent(t, push)
	absent(t, cycle)
	if got := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "show-ref"); got != before {
		t.Fatalf("queued push changed refs: %q", got)
	}
	for _, e := range repoEvents(t, f, f.repos[0].ID) {
		if e.Name == "repo.pushed" || e.Name == "maintenance.finished" {
			t.Fatalf("held maintenance/push ended: %+v", e)
		}
	}
	release()
	receive(t, cycle)
	relationPushSucceeded(t, push)
	if got := strings.TrimSpace(gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "rev-parse", "HEAD")); got != want {
		t.Fatalf("push HEAD %q, want %q", got, want)
	}
	relationEventOrder(t, f, "maintenance.finished", "repo.pushed")
	assertIdle(t, f)
}

type relationPipe struct {
	*io.PipeReader
	read chan struct{}
	once sync.Once
}

func (p *relationPipe) Read(b []byte) (int, error) {
	p.once.Do(func() { close(p.read) })
	return p.PipeReader.Read(b)
}

func TestMaintenanceQueuesBehindPush(t *testing.T) {
	// R-GZ2D-NERH
	f := fixture(t, 1)
	gate, reading := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releasePush := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(releasePush)
	s := relationServer(t, f, func(r *http.Request) {
		original := r.Body
		rd, wr := io.Pipe()
		r.Body = &relationPipe{PipeReader: rd, read: reading}
		go func() {
			defer func() { _ = original.Close(); _ = wr.Close() }()
			select {
			case <-gate:
				_, _ = io.Copy(wr, original)
			case <-r.Context().Done():
			}
		}()
	})
	dir, want := relationWorking(t, f, s)
	relationDrainTimers(f)
	push := relationPush(t, f, dir, s.URL+"/a.git")
	// Read begins after git http-backend starts; the empty pipe holds it.
	receive(t, reading)
	timer(t, f.clock, 23*time.Second)
	timer(t, f.clock, 23*time.Second)
	beforeRefs := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "show-ref")
	beforeObjects := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "count-objects", "-v")
	entered, returned, releaseMaintenance := relationHold(t, f)
	cycle := runCycle(testContext(t), f.cfg)
	timer(t, f.clock, 17*time.Second)
	p := f.lim.Pressure()
	if p.Write.Active != 1 || p.Write.Queued != 1 || !f.lim.Busy(f.repos[0].ID) {
		t.Fatalf("maintenance not queued behind push: %+v", p)
	}
	absent(t, entered)
	absent(t, push)
	absent(t, cycle)
	if got := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "count-objects", "-v"); got != beforeObjects {
		t.Fatalf("maintenance ran while push in flight: %q, before %q", got, beforeObjects)
	}
	if got := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "show-ref"); got != beforeRefs {
		t.Fatalf("blocked push changed refs: %q", got)
	}
	for _, e := range repoEvents(t, f, f.repos[0].ID) {
		if e.Name == "maintenance.finished" || e.Name == "repo.pushed" {
			t.Fatalf("in-flight push/maintenance ended: %+v", e)
		}
	}
	releasePush()
	if id := receive(t, entered); id != f.repos[0].ID {
		t.Fatalf("Hold repository %q", id)
	}
	relationPushSucceeded(t, push)
	afterPush := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "show-ref")
	if got := strings.TrimSpace(gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "rev-parse", "HEAD")); got != want {
		t.Fatalf("push HEAD %q, want %q", got, want)
	}
	absent(t, returned)
	releaseMaintenance()
	receive(t, cycle)
	relationEventOrder(t, f, "repo.pushed", "maintenance.finished")
	if got := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "show-ref"); got != afterPush {
		t.Fatalf("maintenance changed pushed refs: %q, want %q", got, afterPush)
	}
	if got := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "count-objects", "-v"); !strings.Contains(got, "count: 0\n") || !strings.Contains(got, "packs: 1\n") {
		t.Fatalf("maintenance did not collect pushed objects: %q", got)
	}
	assertIdle(t, f)
}
