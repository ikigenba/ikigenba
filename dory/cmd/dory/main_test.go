package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/ikigenba/ikigenba/agentkit"
)

// R-HHHD-S7DV
func TestBinaryWiresRealProcessDependencies(t *testing.T) {
	const (
		apiKey = "binary-wiring-api-key"
		reply  = "binary boundary reply"
	)

	provider := newBinaryProvider(t, apiKey, reply)
	binary, moduleRoot := buildBinary(t)
	home := t.TempDir()
	stdout := mustRunBinary(t, binary, moduleRoot, home, apiKey, provider.server.URL)
	provider.assertOneValidRequest(t)
	sessionID := assertBinaryOutput(t, stdout, reply)
	assertSessionStore(t, home, sessionID)
}

func buildBinary(t *testing.T) (string, string) {
	t.Helper()
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "dory")
	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go executable: %v", err)
	}
	build := &exec.Cmd{
		Path: goExecutable,
		Args: []string{"go", "build", "-o", binary, "./cmd/dory"},
	}
	build.Dir = moduleRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build real binary: %v\n%s", buildErr, output)
	}
	return binary, moduleRoot
}

func mustRunBinary(t *testing.T, binary, moduleRoot, home, apiKey, baseURL string) string {
	t.Helper()
	command := &exec.Cmd{
		Path: binary,
		Args: append([]string{binary}, binaryArgs(t, baseURL)...),
	}
	command.Dir = moduleRoot
	command.Stdin = strings.NewReader("exercise the real binary\n")
	command.Env = isolatedBinaryEnv(home, apiKey)
	stdout, runErr := command.Output()
	stderr := ""
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		stderr = string(exitErr.Stderr)
	}
	if runErr != nil {
		t.Fatalf("run real binary: %v\nstdout:\n%s\nstderr:\n%s", runErr, stdout, stderr)
	}
	return string(stdout)
}

func assertBinaryOutput(t *testing.T, stdout, reply string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	assertAssistantLine(t, lines, reply)
	sessionID := assertSingleSessionSummary(t, lines)
	parsed, err := uuid.Parse(sessionID)
	if err != nil || parsed.String() != sessionID || strings.ToLower(sessionID) != sessionID {
		t.Fatalf("summary session id %q is not a canonical lowercase UUID: %v", sessionID, err)
	}
	if parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 {
		t.Fatalf("summary session id %q has version %d variant %v, want RFC 4122 version 4", sessionID, parsed.Version(), parsed.Variant())
	}
	return sessionID
}

func assertSessionStore(t *testing.T, home, sessionID string) {
	t.Helper()
	sessions := filepath.Join(home, ".dory", "sessions")
	entries, err := os.ReadDir(sessions)
	if err != nil {
		t.Fatalf("read session directory %q: %v", sessions, err)
	}
	wantFile := sessionID + ".db"
	if len(entries) != 1 || entries[0].Name() != wantFile {
		t.Fatalf("session entries = %v, want exactly %q", entryNames(entries), wantFile)
	}
}

type binaryProvider struct {
	server *httptest.Server

	mu       sync.Mutex
	requests int
	errors   []error
}

func newBinaryProvider(t *testing.T, apiKey, reply string) *binaryProvider {
	t.Helper()
	provider := &binaryProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		provider.requests++

		if request.Method != http.MethodPost {
			provider.errors = append(provider.errors, fmt.Errorf("method = %q, want POST", request.Method))
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+apiKey {
			provider.errors = append(provider.errors, fmt.Errorf("authorization = %q", got))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			provider.errors = append(provider.errors, fmt.Errorf("decode request: %w", err))
			http.Error(w, "malformed request", http.StatusBadRequest)
			return
		}
		if body["model"] == "" || body["messages"] == nil {
			provider.errors = append(provider.errors, fmt.Errorf("request body lacks model or messages: %#v", body))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":\"stop\"}]}\n\n", reply)
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *binaryProvider) assertOneValidRequest(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.requests != 1 || len(p.errors) != 0 {
		t.Fatalf("provider received %d requests with errors %v, want one valid request", p.requests, p.errors)
	}
}

func binaryArgs(t *testing.T, baseURL string) []string {
	t.Helper()
	model := ""
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if offering.ID == agentkit.OfferingOpenAIChat {
				model = entry.Model
				break
			}
		}
		if model != "" {
			break
		}
	}
	if model == "" {
		t.Fatal("catalog has no OpenAI chat offering")
	}

	args := make([]string, 0, 24)
	for _, role := range []string{"supervisor", "worker"} {
		for _, setting := range []string{
			"provider=openai",
			"model=" + model,
			"wire=chat",
			"auth=api_key",
			"base_url=" + baseURL,
		} {
			args = append(args, "-c", role+"."+setting)
		}
	}
	return args
}

func isolatedBinaryEnv(home, apiKey string) []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, "OPENAI_API_KEY=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "HOME="+home, "OPENAI_API_KEY="+apiKey)
}

func assertAssistantLine(t *testing.T, lines []string, reply string) {
	t.Helper()
	want := "1 assistant › " + reply
	for _, line := range lines {
		if line == want {
			return
		}
	}
	t.Fatalf("stdout lines = %q, want assistant line %q", lines, want)
}

func assertSingleSessionSummary(t *testing.T, lines []string) string {
	t.Helper()
	const prefix = "· session  "
	var matches []string
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			matches = append(matches, strings.TrimPrefix(line, prefix))
		}
	}
	if len(matches) != 1 || matches[0] == "" || strings.TrimSpace(matches[0]) != matches[0] {
		t.Fatalf("session summary values = %q, want exactly one unpadded value", matches)
	}
	return matches[0]
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	return names
}
