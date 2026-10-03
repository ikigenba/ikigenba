package repos_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

func captureEvents(t *testing.T, f *contractFixture) []telemetry.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := f.writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	events := f.capture.Events()
	// R-TQSI-MB99: Inspect every actual delivery in every MCP, git,
	// verification and maintenance flow below, including middleware events.
	for _, event := range events {
		if value, present := event.Attrs["repo"]; present {
			id, ok := value.(string)
			if !ok || !store.ValidID(id) {
				t.Fatalf("%s has invalid repo attribute %#v", event.Name, value)
			}
		}
	}
	return events
}

func eventsNamed(events []telemetry.Event, name string) []telemetry.Event {
	var result []telemetry.Event
	for _, event := range events {
		if event.Name == name {
			result = append(result, event)
		}
	}
	return result
}

func exactEvent(t *testing.T, event telemetry.Event, request, user string, want telemetry.Attrs) {
	t.Helper()
	if event.RequestID != request || event.User != user {
		t.Fatalf("%s envelope=%q,%q, want %q,%q", event.Name, event.RequestID, event.User, request, user)
	}
	if len(event.Attrs) != len(want) {
		t.Fatalf("%s attributes=%#v, want %#v", event.Name, event.Attrs, want)
	}
	for key, value := range want {
		if event.Attrs[key] != value {
			t.Fatalf("%s attribute %s=%#v, want %#v", event.Name, key, event.Attrs[key], value)
		}
	}
}

// R-TS0F-02ZY: Real MCP calls cause all three mutation events and carry
// the exact caller envelope and exactly two string attributes.
func TestMutationEventContracts(t *testing.T) {
	f := newContractFixture(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "repos", Version: cli.Version, Telemetry: f.writer})
	tools.Register(srv, tools.Config{Store: f.store, Limits: f.limits, Telemetry: f.writer})
	server := httptest.NewServer(telemetry.Middleware(f.writer, identity.Require(srv)))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client()})
	var id string
	for i, tc := range []struct{ name, args, event string }{
		{"create", `{"name":"notes"}`, "repo.created"},
		{"rename", `{"repo":"notes","name":"archive"}`, "repo.renamed"},
		{"delete", `{"repo":"archive"}`, "repo.deleted"},
	} {
		request := []string{"create-request", "rename-request", "delete-request"}[i]
		result, err := client.CallTool(t.Context(), identity.Caller{UserID: "owner", RequestID: request}, tc.name, json.RawMessage(tc.args))
		if err != nil || result.IsError() {
			t.Fatalf("%s=%v,%v", tc.name, result, err)
		}
		if i == 0 {
			r, err := f.store.Find(t.Context(), "owner", "notes")
			if err != nil {
				t.Fatal(err)
			}
			id = r.ID
		}
		events := eventsNamed(captureEvents(t, f), tc.event)
		if len(events) != 1 {
			t.Fatalf("%s count=%d", tc.event, len(events))
		}
		exactEvent(t, events[0], request, "owner", telemetry.Attrs{"repo": id, "owner": "owner"})
	}
}

func contractGit(t *testing.T, f *contractFixture, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := f.git.Command(ctx, dir, nil, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

// R-TT8B-DUQN R-TUG7-RMHC: A real client pushes, clones and fetches through
// smart HTTP, recording exact string ref transitions and int64 pack counts.
func TestGitTransferEventContracts(t *testing.T) {
	f := newContractFixture(t)
	repo, err := f.store.Create(t.Context(), "owner", "notes")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(telemetry.Middleware(f.writer, identity.Require(smarthttp.Handler(smarthttp.Config{Store: f.store, Git: f.git, Limits: f.limits, Telemetry: f.writer}))))
	defer server.Close()
	work := filepath.Join(f.dir, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	contractGit(t, f, work, "init", "-b", "main")
	commit := func(text string) string {
		if err := os.WriteFile(filepath.Join(work, "note.txt"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		contractGit(t, f, work, "add", "note.txt")
		contractGit(t, f, work, "commit", "-m", "fixture commit")
		return contractGit(t, f, work, "rev-parse", "HEAD")
	}
	client := func(dir, request string, args ...string) {
		contractGit(t, f, dir, append([]string{"-c", "http.extraHeader=X-User-Id: owner", "-c", "http.extraHeader=X-Request-Id: " + request}, args...)...)
	}
	first := commit("first\n")
	client(work, "push-create", "push", server.URL+"/notes.git", "HEAD:refs/heads/main")
	clone := filepath.Join(f.dir, "clone")
	client(f.dir, "clone-request", "clone", server.URL+"/notes.git", clone)
	second := commit("second\n")
	client(work, "push-update", "push", server.URL+"/notes.git", "HEAD:refs/heads/main")
	client(clone, "fetch-request", "fetch", "origin")
	client(work, "push-topic", "push", server.URL+"/notes.git", "HEAD:refs/heads/topic")
	client(work, "push-delete", "push", server.URL+"/notes.git", ":refs/heads/topic")
	events := captureEvents(t, f)
	pushed := eventsNamed(events, "repo.pushed")
	if len(pushed) != 4 {
		t.Fatalf("push event count=%d", len(pushed))
	}
	zero := strings.Repeat("0", 40)
	for i, want := range []struct{ request, ref, old, new string }{{"push-create", "refs/heads/main", zero, first}, {"push-update", "refs/heads/main", first, second}, {"push-topic", "refs/heads/topic", zero, second}, {"push-delete", "refs/heads/topic", second, zero}} {
		exactEvent(t, pushed[i], want.request, "owner", telemetry.Attrs{"repo": repo.ID, "ref": want.ref, "old": want.old, "new": want.new})
		for _, key := range []string{"old", "new"} {
			sha, ok := pushed[i].Attrs[key].(string)
			if !ok || len(sha) != 40 || strings.IndexFunc(sha, func(r rune) bool { return (r < '0' || r > '9') && (r < 'a' || r > 'f') }) >= 0 {
				t.Fatalf("bad %s SHA %#v", key, pushed[i].Attrs[key])
			}
		}
	}
	fetched := eventsNamed(events, "repo.fetched")
	if len(fetched) < 2 {
		t.Fatalf("fetch event count=%d", len(fetched))
	}
	seen := map[string]bool{}
	for _, event := range fetched {
		if event.RequestID != "clone-request" && event.RequestID != "fetch-request" {
			t.Fatalf("unexpected fetch envelope: %+v", event)
		}
		count, ok := event.Attrs["bytes"].(int64)
		if !ok || count < 0 {
			t.Fatalf("bad pack count %#v", event.Attrs["bytes"])
		}
		exactEvent(t, event, event.RequestID, "owner", telemetry.Attrs{"repo": repo.ID, "bytes": count})
		seen[event.RequestID] = true
	}
	if !seen["clone-request"] || !seen["fetch-request"] {
		t.Fatal("clone or fetch event missing")
	}
}

type eventTimer struct {
	duration time.Duration
	fire     chan time.Time
}
type eventClock struct {
	mu       sync.Mutex
	now      time.Time
	requests chan eventTimer
}

func newEventClock() *eventClock {
	return &eventClock{now: contractNow(), requests: make(chan eventTimer, 16)}
}
func (c *eventClock) read() time.Time         { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *eventClock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }
func (c *eventClock) after(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.requests <- eventTimer{d, ch}
	return ch
}
func takeEvent[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatal("controlled operation did not respond")
		var empty T
		return empty
	}
}
func maintenanceFixture(t *testing.T) (*contractFixture, *eventClock, store.Repo, maintenance.Config) {
	t.Helper()
	f := newContractFixture(t)
	clock := newEventClock()
	s := settings.Defaults()
	s.WriteSlots = 1
	s.QueueLength = 1
	f.limits = limits.New(s, limits.Clock{Now: clock.read, After: clock.after})
	repo, err := f.store.Create(t.Context(), "owner", "notes")
	if err != nil {
		t.Fatal(err)
	}
	return f, clock, repo, maintenance.Config{Store: f.store, Git: f.git, Limits: f.limits, Telemetry: f.writer}
}
func cycleAsync(ctx context.Context, cfg maintenance.Config) <-chan struct{} {
	done := make(chan struct{})
	go func() { maintenance.Cycle(ctx, cfg); close(done) }()
	return done
}

// R-TVO4-5E81 R-TZBT-APG4: An actual cycle waits on a held push grant,
// then runs git gc and records an empty envelope and exact int64 attributes.
func TestMaintenanceWaitAndFinishedEventContracts(t *testing.T) {
	f, clock, repo, cfg := maintenanceFixture(t)
	grant, err := f.limits.Acquire(t.Context(), repo.ID, limits.Push, true)
	if err != nil {
		t.Fatal(err)
	}
	defer grant.Release()
	done := cycleAsync(t.Context(), cfg)
	timer := takeEvent(t, clock.requests)
	if timer.duration != time.Duration(f.limits.Settings().QueueSeconds)*time.Second {
		t.Fatal("not the queue timer")
	}
	clock.advance(123 * time.Microsecond)
	grant.Release()
	takeEvent(t, done)
	events := captureEvents(t, f)
	waited := eventsNamed(events, "operation.waited")
	if len(waited) != 1 {
		t.Fatalf("waited=%+v", waited)
	}
	exactEvent(t, waited[0], "", "", telemetry.Attrs{"repo": repo.ID, "operation": "maintenance", "wait_us": int64(123)})
	finished := eventsNamed(events, "maintenance.finished")
	if len(finished) != 1 {
		t.Fatalf("finished=%+v", finished)
	}
	want := telemetry.Attrs{"repo": repo.ID}
	for _, key := range []string{"duration_us", "size_before", "size_after"} {
		value, ok := finished[0].Attrs[key].(int64)
		if !ok || value < 0 {
			t.Fatalf("bad maintenance %s=%#v", key, finished[0].Attrs[key])
		}
		want[key] = value
	}
	exactEvent(t, finished[0], "", "", want)
}

// R-TWW0-J5YQ: Real maintenance waits are refused by each available limit
// path, recording exact string fields and empty background envelopes.
func TestMaintenanceRejectionEventContracts(t *testing.T) {
	for _, limit := range []string{"queue_length", "queue_seconds", "draining"} {
		t.Run(limit, func(t *testing.T) {
			f, clock, repo, cfg := maintenanceFixture(t)
			grant, err := f.limits.Acquire(t.Context(), repo.ID, limits.Push, true)
			if err != nil {
				t.Fatal(err)
			}
			defer grant.Release()
			if limit == "queue_length" {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				queued := make(chan error, 1)
				go func() {
					g, err := f.limits.Acquire(ctx, repo.ID, limits.Push, true)
					if g != nil {
						g.Release()
					}
					queued <- err
				}()
				takeEvent(t, clock.requests)
				maintenance.Cycle(t.Context(), cfg)
				cancel()
				if err := takeEvent(t, queued); err == nil {
					t.Fatal("cancelled queued fixture succeeded")
				}
			} else {
				done := cycleAsync(t.Context(), cfg)
				timer := takeEvent(t, clock.requests)
				if limit == "queue_seconds" {
					timer.fire <- clock.read()
				} else {
					f.limits.Drain()
				}
				takeEvent(t, done)
			}
			events := eventsNamed(captureEvents(t, f), "operation.rejected")
			if len(events) != 1 {
				t.Fatalf("rejected=%+v", events)
			}
			exactEvent(t, events[0], "", "", telemetry.Attrs{"repo": repo.ID, "operation": "maintenance", "limit": limit})
		})
	}
}

// R-TY3W-WXPF: A fired operation deadline reaches an actual cycle's timeout
// path, with precisely the three string attributes and background envelope.
func TestMaintenanceTimeoutEventContract(t *testing.T) {
	f, _, repo, cfg := maintenanceFixture(t)
	f.limits = limits.New(settings.Defaults(), limits.Clock{Now: contractNow, After: func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- contractNow(); return ch }})
	cfg.Limits = f.limits
	maintenance.Cycle(t.Context(), cfg)
	events := eventsNamed(captureEvents(t, f), "operation.timed_out")
	if len(events) != 1 {
		t.Fatalf("timed_out=%+v", events)
	}
	exactEvent(t, events[0], "", "", telemetry.Attrs{"repo": repo.ID, "operation": "maintenance", "limit": "operation_seconds"})
}

// R-U0JP-OH6T: Verify a damaged real repository with a request-bearing
// context, and prove its event discards that request envelope.
func TestUnavailableEventContract(t *testing.T) {
	f := newContractFixture(t)
	repo, err := f.store.Create(t.Context(), "owner", "notes")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.store.Dir(repo.ID), "HEAD")); err != nil {
		t.Fatal(err)
	}
	ctx := identity.NewContext(t.Context(), identity.Caller{UserID: "owner", RequestID: "verify-request"})
	if err := f.store.Verify(ctx, f.writer); err != nil {
		t.Fatal(err)
	}
	events := eventsNamed(captureEvents(t, f), "repo.unavailable")
	if len(events) != 1 {
		t.Fatalf("unavailable=%+v", events)
	}
	exactEvent(t, events[0], "", "", telemetry.Attrs{"repo": repo.ID})
}
