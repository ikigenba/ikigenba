package maintenance_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

type timerRequest struct {
	duration time.Duration
	fire     chan time.Time
}
type testClock struct {
	mu       sync.Mutex
	now      time.Time
	requests chan timerRequest
	after    func(timerRequest)
	nowHook  func() time.Time
}

func newClock() *testClock {
	return &testClock{now: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), requests: make(chan timerRequest, 128)}
}
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nowHook != nil {
		return c.nowHook()
	}
	return c.now
}
func (c *testClock) advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
func (c *testClock) After(d time.Duration) <-chan time.Time {
	r := timerRequest{duration: d, fire: make(chan time.Time, 1)}
	if c.after != nil {
		c.after(r)
	}
	c.requests <- r
	return r.fire
}
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-testContext(t).Done():
		t.Fatal("synchronization deadline")
		var zero T
		return zero
	}
}
func absent[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case value := <-ch:
		t.Fatalf("unexpected signal: %v", value)
	default:
	}
}
func timer(t *testing.T, c *testClock, want time.Duration) timerRequest {
	t.Helper()
	r := receive(t, c.requests)
	if r.duration != want {
		t.Fatalf("timer %v, want %v", r.duration, want)
	}
	return r
}
func runCycle(ctx context.Context, cfg maintenance.Config) <-chan struct{} {
	done := make(chan struct{})
	go func() { maintenance.Cycle(ctx, cfg); close(done) }()
	return done
}

type fixtureConfig struct {
	cfg     maintenance.Config
	g       *git.Git
	st      *store.Store
	lim     *limits.Limits
	clock   *testClock
	capture *telemetry.Capture
	repos   []store.Repo
	env     []string
	dir     string
}

func fixture(t *testing.T, n int) *fixtureConfig {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixtureConfig{dir: t.TempDir(), clock: newClock(), capture: &telemetry.Capture{}}
	f.env = []string{"PATH=" + filepath.Dir(path), "HOME=" + f.dir, "XDG_CONFIG_HOME=" + f.dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	f.g, err = git.Find(filepath.Dir(path), func() []string { return append([]string(nil), f.env...) })
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 1024)
	for i := range random {
		random[i] = byte(i)
	}
	f.st, err = store.Open(testContext(t), store.Config{Source: filepath.Join(f.dir, "catalog"), Root: filepath.Join(f.dir, "repos"), Git: f.g, Now: f.clock.Now, Rand: bytes.NewReader(random)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.st.Close(); err != nil {
			t.Error(err)
		}
	})
	s := settings.Defaults()
	s.MaintenanceHours = 1
	s.ReadSlots = 2
	s.WriteSlots = 2
	s.QueueLength = 2
	s.QueueSeconds = 17
	s.OperationSeconds = 23
	f.lim = limits.New(s, limits.Clock{Now: f.clock.Now, After: f.clock.After})
	writer := telemetry.New(telemetry.Config{Service: "repos", Sink: f.capture, Stderr: io.Discard, Now: f.clock.Now, Rand: bytes.NewReader(make([]byte, 4096)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { writer.Shutdown(testContext(t), "test") })
	f.cfg = maintenance.Config{Store: f.st, Git: f.g, Limits: f.lim, Telemetry: writer}
	for i := 0; i < n; i++ {
		r, err := f.st.Create(testContext(t), "owner", string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}
		f.repos = append(f.repos, r)
		dir := f.st.Dir(r.ID)
		blob := strings.TrimSpace(gitInput(t, f.g, dir, "fixture\n", "hash-object", "-w", "--stdin"))
		tree := strings.TrimSpace(gitInput(t, f.g, dir, "100644 blob "+blob+"\tfile\n", "mktree"))
		commit := strings.TrimSpace(gitOut(t, f.g, dir, "commit-tree", tree, "-m", "fixture"))
		gitOut(t, f.g, dir, "update-ref", "refs/heads/"+store.DefaultBranch, commit)
	}
	return f
}
func gitInput(t *testing.T, g *git.Git, dir, input string, args ...string) string {
	t.Helper()
	cmd := g.Command(testContext(t), dir, nil, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
func gitOut(t *testing.T, g *git.Git, dir string, args ...string) string {
	t.Helper()
	out, err := g.Output(testContext(t), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
func events(t *testing.T, f *fixtureConfig) []telemetry.Event {
	t.Helper()
	if err := f.cfg.Telemetry.Flush(testContext(t)); err != nil {
		t.Fatal(err)
	}
	return f.capture.Events()
}
func repoEvents(t *testing.T, f *fixtureConfig, id string) []telemetry.Event {
	t.Helper()
	var result []telemetry.Event
	for _, e := range events(t, f) {
		if e.Attrs["repo"] == id {
			result = append(result, e)
		}
	}
	return result
}
func assertFinished(t *testing.T, f *fixtureConfig, id string) telemetry.Event {
	t.Helper()
	es := repoEvents(t, f, id)
	if len(es) == 0 || es[len(es)-1].Name != "maintenance.finished" {
		t.Fatalf("finished events: %+v", es)
	}
	return es[len(es)-1]
}
func assertIdle(t *testing.T, f *fixtureConfig) {
	t.Helper()
	p := f.lim.Pressure()
	if p.Write.Active != 0 || p.Write.Queued != 0 {
		t.Fatalf("write pressure: %+v", p)
	}
	for _, r := range f.repos {
		if f.lim.Busy(r.ID) {
			t.Fatal("maintenance left busy", r.ID)
		}
	}
}
func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func maintenanceContext(t *testing.T) context.Context {
	t.Helper()
	return identity.NewContext(testContext(t), identity.Caller{UserID: "caller", RequestID: "request"})
}
