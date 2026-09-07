package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/store"
)

const (
	testSessionID = "1d65ee56-9ae8-4fb0-a2f9-24e2778f6210"
	resumeID      = "f444db06-f5a4-44ec-9d2e-e687c21d210b"
)

// R-JL35-HZCX
func TestRunOpensFactoriesBeforeTouchingSessionDisk(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		getenv func(string) string
		want   string
	}{
		{
			name: "first factory",
			getenv: func(string) string {
				return ""
			},
			want: "open supervisor model",
		},
		{
			name: "second factory",
			args: []string{
				"-c", "worker.provider=anthropic", "-c", "worker.model=unlisted-worker",
				"-c", "worker.wire=messages", "-c", "worker.auth=api_key",
			},
			getenv: func(name string) string {
				if name == "OPENAI_API_KEY" {
					return "supervisor-key"
				}
				return ""
			},
			want: "open worker model",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "missing-home")
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), test.args, strings.NewReader("prompt"), &stdout, &stderr, Deps{
				Home: home, Getenv: test.getenv, Now: testNow, SessionID: testSessionID, Root: t.TempDir(),
			})
			if code != int(exitFailure) {
				t.Errorf("Run exit = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.want) || !strings.Contains(stderr.String(), "environment variable") {
				t.Errorf("stderr = %q, want contextual credential error", stderr.String())
			}
			assertPathAbsent(t, filepath.Join(home, ".dory"))
		})
	}
}

// R-JNIY-9IUB
func TestRunCreatesAndResumesExactSession(t *testing.T) {
	baseArgs := chatOfferingArgs(t)
	provider := newCLIProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		writeCLIText(w, "complete", 2, 1, 3, 0)
	})
	args := withBaseURL(baseArgs, provider.server.URL)

	t.Run("new", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		stdout, stderr, code := runCLI(t, args, home, root, testSessionID, nil)
		if code != int(exitSuccess) || stderr != "" || !strings.Contains(stdout, "1 assistant › complete") {
			t.Fatalf("Run = %d, stdout=%q stderr=%q", code, stdout, stderr)
		}
		path := filepath.Join(home, ".dory", "sessions", testSessionID+".db")
		info, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("sessions mode = %o, want 700", info.Mode().Perm())
		}
		session := mustOpenStore(t, path)
		if session.ID() != testSessionID || session.Root() != root {
			t.Errorf("metadata = id %q root %q", session.ID(), session.Root())
		}
		mustCloseStore(t, session)
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
			t.Errorf("session files = %v, %v, want only %s", entries, err, filepath.Base(path))
		}
	})

	t.Run("resume", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		path := filepath.Join(home, ".dory", "sessions", resumeID+".db")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		session, err := store.Create(path, resumeID, root, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Add("1", store.KindPrompt, "earlier", nil); err != nil {
			t.Fatal(err)
		}
		mustCloseStore(t, session)

		stdout, stderr, code := runCLI(t, append(args, "-resume", resumeID), home, root, "unused-new-id", nil)
		if code != int(exitSuccess) || stderr != "" {
			t.Fatalf("Run = %d, stdout=%q stderr=%q", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "2 assistant › complete") || !strings.Contains(stdout, "· session  "+resumeID) {
			t.Errorf("resume stdout = %q, want pass 2 and recorded id", stdout)
		}
	})

	t.Run("missing and mismatch", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		missing := filepath.Join(home, ".dory", "sessions", resumeID+".db")
		_, stderr, code := runCLI(t, append(args, "-resume", resumeID), home, root, "unused", nil)
		if code != int(exitFailure) || !strings.Contains(stderr, missing) {
			t.Errorf("missing Run = %d stderr=%q, want exact path", code, stderr)
		}
		assertPathAbsent(t, filepath.Join(home, ".dory"))

		if err := os.MkdirAll(filepath.Dir(missing), 0o700); err != nil {
			t.Fatal(err)
		}
		storedRoot := t.TempDir()
		session, err := store.Create(missing, resumeID, storedRoot, testNow)
		if err != nil {
			t.Fatal(err)
		}
		mustCloseStore(t, session)
		before := provider.requestCount()
		_, stderr, code = runCLI(t, append(args, "-resume", resumeID), home, root, "unused", nil)
		if code != int(exitFailure) || !strings.Contains(stderr, storedRoot) || !strings.Contains(stderr, root) {
			t.Errorf("mismatch Run = %d stderr=%q", code, stderr)
		}
		if provider.requestCount() != before {
			t.Error("root mismatch reached provider")
		}
		mustCloseStore(t, mustOpenStore(t, missing))
	})
}

// R-JPYR-12BP
// R-K25Q-URQN
func TestRunTracesPassAndAlwaysSummarizesAndCloses(t *testing.T) {
	baseArgs := chatOfferingArgs(t)
	usage := agentkit.Usage{InputTokens: 7, CachedTokens: 2, OutputTokens: 2, ReasoningTokens: 3}
	provider := newCLIProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		writeCLIText(w, "known event", 9, 2, 5, 3)
	})
	home := t.TempDir()
	root := t.TempDir()
	stdout, stderr, code := runCLI(t, withBaseURL(baseArgs, provider.server.URL), home, root, testSessionID, nil)
	if code != int(exitSuccess) || stderr != "" {
		t.Fatalf("success Run = %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	want := "1 assistant › known event\n\n" + expectedSummary(usage, agentkit.Cost(93_000), testSessionID)
	if stdout != want {
		t.Errorf("stdout = %q, want exact %q", stdout, want)
	}
	path := filepath.Join(home, ".dory", "sessions", testSessionID+".db")
	mustCloseStore(t, mustOpenStore(t, path))

	failureProvider := newCLIProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "terminal failure", http.StatusInternalServerError)
	})
	failureHome := t.TempDir()
	stdout, stderr, code = runCLI(t, withBaseURL(baseArgs, failureProvider.server.URL), failureHome, root, resumeID, nil)
	if code != int(exitFailure) {
		t.Errorf("failure Run exit = %d, want 1", code)
	}
	if stdout != expectedSummary(agentkit.Usage{}, 0, resumeID) {
		t.Errorf("failure stdout = %q, want exact zero summary", stdout)
	}
	if !strings.HasPrefix(stderr, "1 error › ") || !strings.Contains(stderr, "500") {
		t.Errorf("failure stderr = %q, want addressed terminal error", stderr)
	}
	mustCloseStore(t, mustOpenStore(t, filepath.Join(failureHome, ".dory", "sessions", resumeID+".db")))
}

// R-JOQU-NAL0
func TestRunInterruptCancelsInflightPass(t *testing.T) {
	baseArgs := chatOfferingArgs(t)
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	provider := newCLIProvider(t, func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		select {
		case <-request.Context().Done():
			close(canceled)
		case <-release:
		}
	})
	t.Cleanup(func() { close(release) })
	interrupts := make(chan struct{}, 1)
	home := t.TempDir()
	root := t.TempDir()
	type outcome struct {
		stdout string
		stderr string
		code   int
	}
	result := make(chan outcome, 1)
	go func() {
		stdout, stderr, code := runCLI(t, withBaseURL(baseArgs, provider.server.URL), home, root, testSessionID, interrupts)
		result <- outcome{stdout: stdout, stderr: stderr, code: code}
	}()
	awaitSignal(t, started, "provider request")
	interrupts <- struct{}{}
	awaitSignal(t, canceled, "request cancellation")
	select {
	case got := <-result:
		if got.code != int(exitFailure) || !strings.HasPrefix(got.stderr, "1 error › ") ||
			!strings.Contains(strings.ToLower(got.stderr), "context canceled") {
			t.Errorf("interrupted Run = %d stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
		}
		if got.stdout != expectedSummary(agentkit.Usage{}, 0, testSessionID) {
			t.Errorf("interrupt stdout = %q, want summary", got.stdout)
		}
		mustCloseStore(t, mustOpenStore(t, filepath.Join(home, ".dory", "sessions", testSessionID+".db")))
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after interrupt")
	}
}

// R-HDTO-MW5S
func TestRunUsesEveryInjectedDependency(t *testing.T) {
	baseArgs := chatOfferingArgs(t)
	home := t.TempDir()
	root := t.TempDir()
	fixture := "injected-root-fixture"
	if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var authorizations []string
	var requests []map[string]any
	responses := []func(io.Writer){
		func(w io.Writer) {
			writeCLIToolCall(w, "delegate-1", "delegate", `{"role":"worker","prompt":"inspect fixture"}`)
		},
		func(w io.Writer) {
			writeCLIToolCall(w, "read-1", "Read", `{"file_path":"fixture.txt"}`)
		},
		func(w io.Writer) { writeCLIText(w, "worker report", 1, 0, 1, 0) },
		func(w io.Writer) { writeCLIText(w, "root report", 1, 0, 1, 0) },
	}
	provider := newCLIProvider(t, func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		requests = append(requests, body)
		if len(responses) == 0 {
			mu.Unlock()
			t.Error("provider response queue exhausted")
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		response := responses[0]
		responses = responses[1:]
		mu.Unlock()
		response(w)
	})
	getenvCalls := 0
	getenv := func(name string) string {
		if name != "OPENAI_API_KEY" {
			t.Errorf("Getenv(%q), want OPENAI_API_KEY", name)
		}
		getenvCalls++
		return "distinctive-injected-key"
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), withBaseURL(baseArgs, provider.server.URL), strings.NewReader("inspect"), &stdout, &stderr, Deps{
		Home: home, Getenv: getenv, Now: testNow, SessionID: testSessionID, Root: root,
	})
	if code != int(exitSuccess) || stderr.Len() != 0 {
		t.Fatalf("Run = %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if getenvCalls != 2 {
		t.Errorf("Getenv calls = %d, want one per role", getenvCalls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(responses) != 0 || provider.requestCount() != 4 {
		t.Errorf("provider script left %d responses after %d requests", len(responses), provider.requestCount())
	}
	for _, authorization := range authorizations {
		if authorization != "Bearer distinctive-injected-key" {
			t.Errorf("Authorization = %q", authorization)
		}
	}
	encodedRequests, _ := json.Marshal(requests)
	if !bytes.Contains(encodedRequests, []byte(fixture)) {
		t.Errorf("provider requests do not contain rooted Read result: %s", encodedRequests)
	}
	path := filepath.Join(home, ".dory", "sessions", testSessionID+".db")
	session := mustOpenStore(t, path)
	if session.ID() != testSessionID || session.Root() != root || !strings.Contains(stdout.String(), "· session  "+testSessionID) {
		t.Errorf("injected metadata/summary not observed")
	}
	for id := int64(1); ; id++ {
		entry, err := session.Fetch(id)
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !entry.Created.Equal(testNow()) {
			t.Errorf("entry %d timestamp = %v, want %v", id, entry.Created, testNow())
		}
	}
	mustCloseStore(t, session)
}

type cliProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	count  int
}

func newCLIProvider(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *cliProvider {
	t.Helper()
	provider := &cliProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		provider.mu.Lock()
		provider.count++
		provider.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		handler(w, request)
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *cliProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

func chatOfferingArgs(t *testing.T) []string {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if offering.ID == agentkit.OfferingOpenAIChat {
				args := make([]string, 0, 20)
				for _, role := range []string{"supervisor", "worker"} {
					for _, setting := range []string{
						"provider=openai", "model=" + entry.Model, "wire=chat", "auth=api_key",
					} {
						args = append(args, "-c", role+"."+setting)
					}
				}
				return args
			}
		}
	}
	t.Fatal("catalog has no OpenAI chat offering")
	return nil
}

func withBaseURL(args []string, baseURL string) []string {
	result := append([]string(nil), args...)
	result = append(result, "-c", "supervisor.base_url="+baseURL, "-c", "worker.base_url="+baseURL)
	return result
}

func runCLI(t *testing.T, args []string, home, root, id string, interrupts <-chan struct{}) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader("prompt"), &stdout, &stderr, Deps{
		Home: home, Getenv: func(string) string { return "test-key" }, Now: testNow,
		SessionID: id, Root: root, Interrupts: interrupts,
	})
	return stdout.String(), stderr.String(), code
}

func writeCLIText(w io.Writer, text string, prompt, cached, completion, reasoning int64) {
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":%d,\"prompt_tokens_details\":{\"cached_tokens\":%d},\"completion_tokens\":%d,\"completion_tokens_details\":{\"reasoning_tokens\":%d}}}\n\n", text, prompt, cached, completion, reasoning)
}

func writeCLIToolCall(w io.Writer, id, name, input string) {
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", id, name, input)
}

func expectedSummary(usage agentkit.Usage, cost agentkit.Cost, id string) string {
	total := usage.InputTokens + usage.CachedTokens + usage.CacheWrite5mTokens +
		usage.CacheWrite1hTokens + usage.OutputTokens + usage.ReasoningTokens
	return fmt.Sprintf("summary\n· tokens   in=%d cache(r=%d w=%d) out=%d reasoning=%d total=%d\n· cost     $%.6f pass\n· session  %s\n",
		usage.InputTokens, usage.CachedTokens, usage.CacheWrite5mTokens+usage.CacheWrite1hTokens,
		usage.OutputTokens, usage.ReasoningTokens, total, float64(cost)/1_000_000_000, id)
}

func testNow() time.Time { return time.Date(2042, 7, 8, 9, 10, 11, 0, time.UTC) }

func mustOpenStore(t *testing.T, path string) *store.Store {
	t.Helper()
	session, err := store.Open(path, testNow)
	if err != nil {
		t.Fatalf("open store %q: %v", path, err)
	}
	return session
}

func mustCloseStore(t *testing.T, session *store.Store) {
	t.Helper()
	if err := session.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
