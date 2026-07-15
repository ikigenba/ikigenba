package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	appdb "appkit/db"
	reposdb "repos/internal/db"
	"repos/internal/repos"
)

func TestIssueSessionCreatesFreshWorktreeAndPinsInstructionsBeforeSend(t *testing.T) {
	// R-F4R4-YHBH
	// R-F76X-Q0SV
	fixture := newFixture(t, 1, time.Minute)
	canonical, remote := fixture.addRepo(t, "alpha")
	wantTip := advanceRemote(t, remote)
	issue := 41
	protocol := &protocolStub{issue: IssueContent{
		Title: "Broken widget", Body: "Repair the widget.",
		Comments: []string{"First comment", "Second comment"},
	}}
	fixture.config.Protocol = protocol
	instructionPath := filepath.Join(fixture.stateRoot, "sessions", "issue-one", "instructions.md")
	var sent string
	firstSend := true
	fixture.config.Factory = AgentFactoryFunc(func(config ConversationConfig) Agent {
		return agentFunc(func(_ context.Context, text string) error {
			if firstSend {
				firstSend = false
				contents, err := os.ReadFile(instructionPath)
				if err != nil {
					t.Errorf("instructions did not exist before Send: %v", err)
				}
				if string(contents) != text {
					t.Errorf("pinned instructions = %q, Send text = %q", contents, text)
				}
				sent = text
			}
			return nil
		})
	})
	runner := fixture.runner(t)
	session, err := runner.Enqueue(context.Background(), SessionRequest{
		ID: "issue-one", RepoName: "alpha", OwnerEmail: "owner@example.com", IssueNumber: &issue,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if session.Branch != "ikibot/issue-41" {
		t.Fatalf("branch = %q", session.Branch)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Dispatch(ctx)
	waitStatus(t, fixture.store, session.ID, repos.StatusSucceeded)
	worktree := filepath.Join(fixture.stateRoot, "sessions", session.ID, "worktree")
	if got := gitOutput(t, worktree, "rev-parse", "HEAD"); got != wantTip {
		t.Fatalf("worktree tip = %s, want fresh origin tip %s", got, wantTip)
	}
	if got := gitOutput(t, worktree, "branch", "--show-current"); got != "ikibot/issue-41" {
		t.Fatalf("worktree branch = %q", got)
	}
	if got := gitOutput(t, canonical, "branch", "--show-current"); got != "main" {
		t.Fatalf("canonical branch changed to %q", got)
	}
	for _, want := range []string{"Broken widget", "Repair the widget.", "First comment", "Second comment"} {
		if !strings.Contains(sent, want) {
			t.Errorf("instructions missing %q: %q", want, sent)
		}
	}
	if protocol.fetches != 1 {
		t.Fatalf("github peer fetches = %d, want 1", protocol.fetches)
	}

	fixture.addRepo(t, "beta")
	failed := "inspected failure"
	if err := fixture.store.InsertSession(context.Background(), repos.Session{
		ID: "old-failure", RepoName: "beta", OwnerEmail: "owner@example.com",
		IssueNumber: &issue, Attempt: 1, Branch: "ikibot/issue-41", Instructions: "old",
		Status: repos.StatusFailed, Error: &failed, CreatedAt: fixture.clock.Now(), LogPath: "old.jsonl",
	}); err != nil {
		t.Fatal(err)
	}
	next, err := runner.Enqueue(context.Background(), SessionRequest{
		ID: "issue-two", RepoName: "beta", OwnerEmail: "owner@example.com", IssueNumber: &issue,
	})
	if err != nil || next.Attempt != 2 || next.Branch != "ikibot/issue-41.2" {
		t.Fatalf("next attempt = %#v, %v", next, err)
	}

	manualText := "manual text\nverbatim\n"
	manual, err := runner.Enqueue(context.Background(), SessionRequest{
		ID: "manual", RepoName: "alpha", OwnerEmail: "owner@example.com", Instructions: manualText,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, fixture.store, manual.ID, repos.StatusSucceeded)
	contents, err := os.ReadFile(filepath.Join(fixture.stateRoot, "sessions", manual.ID, "instructions.md"))
	if err != nil || string(contents) != manualText {
		t.Fatalf("manual instructions = %q, %v", contents, err)
	}
}

func TestDispatcherEnforcesGlobalAndPerRepoCapsInFIFOOrder(t *testing.T) {
	// R-F8EU-3SJK
	fixture := newFixture(t, 2, time.Minute)
	for _, name := range []string{"one", "two", "three"} {
		fixture.addRepo(t, name)
	}
	started := make(chan string, 10)
	release := make(chan struct{}, 10)
	var mu sync.Mutex
	active, peak := 0, 0
	fixture.config.Factory = AgentFactoryFunc(func(ConversationConfig) Agent {
		return agentFunc(func(ctx context.Context, text string) error {
			mu.Lock()
			active++
			if active > peak {
				peak = active
			}
			mu.Unlock()
			started <- text
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			mu.Lock()
			active--
			mu.Unlock()
			return nil
		})
	})
	runner := fixture.runner(t)
	for i, name := range []string{"one", "two", "three"} {
		fixture.clock.Advance(time.Second)
		if _, err := runner.Enqueue(context.Background(), SessionRequest{
			ID: fmt.Sprintf("fifo-%d", i), RepoName: name, OwnerEmail: "owner@example.com", Instructions: name,
		}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Dispatch(ctx)
	waitForStarts(t, started, map[string]bool{"one": true, "two": true})
	assertStatus(t, fixture.store, "fifo-0", repos.StatusRunning)
	assertStatus(t, fixture.store, "fifo-1", repos.StatusRunning)
	assertStatus(t, fixture.store, "fifo-2", repos.StatusQueued)
	release <- struct{}{}
	if got := waitStart(t, started); got != "three" {
		t.Fatalf("third admission = %q, want FIFO session three", got)
	}
	release <- struct{}{}
	release <- struct{}{}
	for i := range 3 {
		waitStatus(t, fixture.store, fmt.Sprintf("fifo-%d", i), repos.StatusSucceeded)
	}

	for i := range 2 {
		fixture.clock.Advance(time.Second)
		if _, err := runner.Enqueue(context.Background(), SessionRequest{
			ID: fmt.Sprintf("same-%d", i), RepoName: "one", OwnerEmail: "owner@example.com", Instructions: fmt.Sprintf("same-%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := waitStart(t, started); got != "same-0" {
		t.Fatalf("same-repo first = %q", got)
	}
	assertStatus(t, fixture.store, "same-1", repos.StatusQueued)
	release <- struct{}{}
	if got := waitStart(t, started); got != "same-1" {
		t.Fatalf("same-repo second = %q", got)
	}
	release <- struct{}{}
	waitStatus(t, fixture.store, "same-1", repos.StatusSucceeded)
	mu.Lock()
	defer mu.Unlock()
	if peak != 2 {
		t.Fatalf("peak concurrency = %d, want exactly 2", peak)
	}
}

func TestTTLAndUserCancellationAreClassifiedAndReleaseRepo(t *testing.T) {
	// R-F9MQ-HKA9
	fixture := newFixture(t, 2, 25*time.Millisecond)
	fixture.addRepo(t, "alpha")
	started := make(chan string, 5)
	fixture.config.Factory = AgentFactoryFunc(func(ConversationConfig) Agent {
		return agentFunc(func(ctx context.Context, text string) error {
			started <- text
			<-ctx.Done()
			return ctx.Err()
		})
	})
	runner := fixture.runner(t)
	ctx, cancelDispatch := context.WithCancel(context.Background())
	defer cancelDispatch()
	go runner.Dispatch(ctx)
	for index, id := range []string{"ttl", "after-ttl"} {
		if index > 0 {
			fixture.clock.Advance(time.Second)
		}
		if _, err := runner.Enqueue(context.Background(), SessionRequest{
			ID: id, RepoName: "alpha", OwnerEmail: "owner@example.com", Instructions: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := waitStart(t, started); got != "ttl" {
		t.Fatalf("first start = %q", got)
	}
	timed := waitStatus(t, fixture.store, "ttl", repos.StatusFailed)
	if timed.Error == nil || *timed.Error != "session TTL exceeded" || timed.EndedAt == nil {
		t.Fatalf("TTL terminal fields = %#v", timed)
	}
	if got := waitStart(t, started); got != "after-ttl" {
		t.Fatalf("repo was not released after TTL; start = %q", got)
	}
	waitStatus(t, fixture.store, "after-ttl", repos.StatusFailed)

	if _, err := runner.Enqueue(context.Background(), SessionRequest{
		ID: "cancel", RepoName: "alpha", OwnerEmail: "owner@example.com", Instructions: "cancel",
	}); err != nil {
		t.Fatal(err)
	}
	if got := waitStart(t, started); got != "cancel" {
		t.Fatalf("cancel start = %q", got)
	}
	if !runner.Cancel("cancel") {
		t.Fatal("Cancel returned false for running session")
	}
	cancelled := waitStatus(t, fixture.store, "cancel", repos.StatusCancelled)
	if cancelled.EndedAt == nil {
		t.Fatal("cancelled session has no ended_at")
	}
}

func TestRecoverSweepsRunningAndPreservesQueuedForDispatch(t *testing.T) {
	// R-FAUM-VC0Y
	fixture := newFixture(t, 1, time.Minute)
	fixture.addRepo(t, "alpha")
	now := fixture.clock.Now()
	for _, session := range []repos.Session{
		{ID: "orphan", RepoName: "alpha", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "orphan", Instructions: "old", Status: repos.StatusRunning, CreatedAt: now, LogPath: "old.jsonl"},
		{ID: "survivor", RepoName: "alpha", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "ikibot/session-survivor", Instructions: "queued", Status: repos.StatusQueued, CreatedAt: now.Add(time.Second), LogPath: filepath.Join(fixture.stateRoot, "sessions", "survivor", "output.jsonl")},
	} {
		if err := fixture.store.InsertSession(context.Background(), session); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan string, 1)
	fixture.config.Factory = AgentFactoryFunc(func(ConversationConfig) Agent {
		return agentFunc(func(_ context.Context, text string) error { started <- text; return nil })
	})
	runner := fixture.runner(t)
	count, err := runner.Recover(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("Recover = %d, %v; want 1, nil", count, err)
	}
	orphan := waitStatus(t, fixture.store, "orphan", repos.StatusFailed)
	if orphan.Error == nil || *orphan.Error != "interrupted by restart" {
		t.Fatalf("orphan = %#v", orphan)
	}
	assertStatus(t, fixture.store, "survivor", repos.StatusQueued)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Dispatch(ctx)
	if got := waitStart(t, started); got != "queued" {
		t.Fatalf("recovered queued instructions = %q", got)
	}
	waitStatus(t, fixture.store, "survivor", repos.StatusSucceeded)
}

func TestModelValidationRejectsBadBootConfigurationAndAcceptsDefaultPricing(t *testing.T) {
	// R-FC2J-93RN
	for _, config := range []ModelConfig{
		{Provider: "anthropic", Model: "unknown-model", APIKey: "key"},
		{Provider: "anthropic", Model: "claude-opus-4-8", APIKey: ""},
	} {
		if _, err := ValidateModel(config); err == nil || !strings.Contains(err.Error(), config.Provider+"/"+config.Model) {
			t.Fatalf("ValidateModel(%#v) error = %v; want named pair", config, err)
		}
	}
	bad := ModelConfig{Provider: "anthropic", Model: "unknown-model", APIKey: "key"}
	if _, err := New(Config{Model: bad}); err == nil || !strings.Contains(err.Error(), "anthropic/unknown-model") {
		t.Fatalf("runner construction reached dependency/route setup before model validation: %v", err)
	}
	provider, err := ValidateModel(DefaultModelConfig("fixture-key"))
	if err != nil {
		t.Fatalf("default model did not pass real pricing registry: %v", err)
	}
	if _, ok := provider.Pricing("claude-opus-4-8"); !ok {
		t.Fatal("default model absent from real provider pricing table")
	}
}

type fixture struct {
	store     *repos.Store
	git       *repos.Git
	clock     *fakeClock
	stateRoot string
	config    Config
}

func newFixture(t *testing.T, maxRun int, ttl time.Duration) *fixture {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	migrations, err := reposdb.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := appdb.Migrate(context.Background(), db, migrations); err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	clock := &fakeClock{now: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	f := &fixture{store: repos.NewStore(db), clock: clock, stateRoot: stateRoot}
	f.git = repos.NewGit(filepath.Join(stateRoot, "repos"), staticToken("fixture"))
	f.config = Config{Store: f.store, Git: f.git, Clock: clock, StateRoot: stateRoot,
		MaxRun: maxRun, TTL: ttl, Model: DefaultModelConfig("fixture-key")}
	return f
}

func (f *fixture) runner(t *testing.T) *Runner {
	t.Helper()
	runner, err := New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func (f *fixture) addRepo(t *testing.T, name string) (string, string) {
	t.Helper()
	remote := newBareRemote(t, name)
	if err := f.git.Clone(context.Background(), "file://"+filepath.ToSlash(remote), name); err != nil {
		t.Fatalf("clone %s: %v", name, err)
	}
	if err := f.store.InsertRepo(context.Background(), repos.Repo{
		Name: name, OwnerEmail: "owner@example.com", CloneURL: "file://" + filepath.ToSlash(remote),
		DefaultBranch: "main", CreatedAt: f.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.stateRoot, "repos", name), remote
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type agentFunc func(context.Context, string) error

func (f agentFunc) Send(ctx context.Context, text string) error { return f(ctx, text) }

type protocolStub struct {
	issue   IssueContent
	fetches int
}

func (p *protocolStub) FetchIssue(context.Context, string, int) (IssueContent, error) {
	p.fetches++
	return p.issue, nil
}
func (*protocolStub) PostQueued(context.Context, string, int) error { return nil }

func waitStatus(t *testing.T, store *repos.Store, id, status string) repos.Session {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		session, err := store.GetSession(context.Background(), id)
		if err == nil && session.Status == status {
			return session
		}
		time.Sleep(5 * time.Millisecond)
	}
	session, err := store.GetSession(context.Background(), id)
	t.Fatalf("session %s = %#v, %v; never reached %s", id, session, err, status)
	return repos.Session{}
}

func assertStatus(t *testing.T, store *repos.Store, id, status string) {
	t.Helper()
	session, err := store.GetSession(context.Background(), id)
	if err != nil || session.Status != status {
		t.Fatalf("session %s status = %q, %v; want %q", id, session.Status, err, status)
	}
}

func waitStart(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case value := <-started:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent start")
		return ""
	}
}

func waitForStarts(t *testing.T, started <-chan string, want map[string]bool) {
	t.Helper()
	got := map[string]bool{}
	for range len(want) {
		got[waitStart(t, started)] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("started = %v, want %v", got, want)
	}
}

func newBareRemote(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, name+".git")
	gitRun(t, "", "init", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(root, "seed")
	gitRun(t, "", "init", "--initial-branch=main", seed)
	commitFile(t, seed, "initial")
	gitRun(t, seed, "remote", "add", "origin", remote)
	gitRun(t, seed, "push", "origin", "main")
	return remote
}

func advanceRemote(t *testing.T, remote string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "advance")
	gitRun(t, "", "clone", remote, dir)
	commitFile(t, dir, "advanced")
	gitRun(t, dir, "push", "origin", "main")
	return gitOutput(t, dir, "rev-parse", "HEAD")
}

func commitFile(t *testing.T, dir, contents string) {
	t.Helper()
	gitRun(t, dir, "config", "user.email", "fixture@example.com")
	gitRun(t, dir, "config", "user.name", "Fixture")
	if err := os.WriteFile(filepath.Join(dir, contents+".txt"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", contents)
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
