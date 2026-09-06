package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
)

// R-B7BA-NWHT
func TestOpenRequiresAPIKeyEnvironmentVariable(t *testing.T) {
	cfg := openConfig(t, "http://provider.invalid")
	cfg.Getenv = func(name string) string {
		if name != "OPENAI_API_KEY" {
			t.Fatalf("Getenv name = %q, want OPENAI_API_KEY", name)
		}
		return ""
	}
	if _, err := Open(cfg); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("Open error = %v, want error naming OPENAI_API_KEY", err)
	}
}

// R-B7BA-NWHT
func TestOpenUsesAPIKeyFromPlannedEnvironmentVariable(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := successfulServer(t, requests)
	cfg := openConfig(t, server.URL)
	cfg.Getenv = func(name string) string {
		if name != "OPENAI_API_KEY" {
			t.Fatalf("Getenv name = %q, want OPENAI_API_KEY", name)
		}
		return "key-from-getenv"
	}
	session, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stream := session.Send(context.Background(), "authenticate")
	drain(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	request := <-requests
	if got, want := request.header.Get("Authorization"), "Bearer key-from-getenv"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
}

// R-B8J7-1O8I
func TestOpenOAuthReadsTokenFileAndNamesSourceFailure(t *testing.T) {
	cfg := openConfig(t, "http://provider.invalid")
	cfg.Wire = "responses"
	cfg.Auth = "oauth"
	cfg.AuthFile = filepath.Join(t.TempDir(), "missing-token.json")
	if _, err := Open(cfg); err == nil || !strings.Contains(err.Error(), cfg.AuthFile) {
		t.Fatalf("missing OAuth file error = %v, want diagnostic naming %q", err, cfg.AuthFile)
	}
	cfg.AuthFile = filepath.Join(t.TempDir(), "unreadable-token.json")
	if err := os.Mkdir(cfg.AuthFile, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(cfg); err == nil || !strings.Contains(err.Error(), cfg.AuthFile) {
		t.Fatalf("unreadable OAuth file error = %v, want diagnostic naming %q", err, cfg.AuthFile)
	}
	cfg.AuthFile = filepath.Join(t.TempDir(), "tokenless.json")
	if err := os.WriteFile(cfg.AuthFile, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(cfg); err == nil || !strings.Contains(err.Error(), cfg.AuthFile) {
		t.Fatalf("tokenless OAuth file error = %v, want diagnostic naming %q", err, cfg.AuthFile)
	}

	requests := make(chan capturedRequest, 1)
	server := successfulServer(t, requests)
	cfg.BaseURL = server.URL
	cfg.AuthFile = filepath.Join(t.TempDir(), "valid-token.json")
	claims := `{"https://api.openai.com/auth":{"chatgpt_account_id":"account-from-file"}}`
	token := "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
	if err := os.WriteFile(cfg.AuthFile, []byte(`{"access_token":"`+token+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open with valid OAuth token file: %v", err)
	}
	if session.Plan().AuthMode != agentkit.AuthModeOAuth || session.Plan().AuthFile != cfg.AuthFile {
		t.Fatalf("OAuth plan = %+v", session.Plan())
	}
	stream := session.Send(context.Background(), "authenticate")
	drain(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	request := <-requests
	if got, want := request.header.Get("Authorization"), "Bearer "+token; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
	if got, want := request.header.Get("ChatGPT-Account-Id"), "account-from-file"; got != want {
		t.Fatalf("ChatGPT-Account-Id header = %q, want %q", got, want)
	}
}

// R-V7Y5-BG9I
// R-VCTQ-UJ8A
func TestOpenRoutesSingleTextPromptToOverrideURL(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := successfulServer(t, requests)
	cfg := openConfig(t, server.URL)
	session, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if session.Plan().BaseURL != server.URL {
		t.Fatalf("session base URL = %q, want %q", session.Plan().BaseURL, server.URL)
	}
	stream := session.Send(context.Background(), "one exact prompt")
	drain(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	request := <-requests
	if request.path != "/" {
		t.Fatalf("request path = %q, want override server root", request.path)
	}
	var messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(request.body["messages"], &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != "user" || messages[0].Content != "one exact prompt" {
		t.Fatalf("messages = %+v, want one user text prompt", messages)
	}
}

// R-V961-P807
func TestOpenProvidesSixRootedToolsWithGitExcluded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("visible marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "hidden.txt"), []byte("hidden marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tools, err := sessionTools(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	wantNames := []string{"Bash", "Read", "Write", "Edit", "Glob", "Grep"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("tool names = %q, want exactly %q", names, wantNames)
	}
	readResult, err := tools[1].Call(context.Background(), json.RawMessage(`{"file_path":"visible.txt"}`))
	if err != nil || !strings.Contains(readResult, "visible marker") {
		t.Fatalf("rooted Read result = %q, error %v", readResult, err)
	}
	globResult, err := tools[4].Call(context.Background(), json.RawMessage(`{"pattern":"**/*.txt"}`))
	if err != nil || !strings.Contains(globResult, "visible.txt") || strings.Contains(globResult, "hidden.txt") {
		t.Fatalf("Glob result = %q, error %v, want visible and no .git file", globResult, err)
	}
	grepResult, err := tools[5].Call(context.Background(), json.RawMessage(`{"pattern":"marker"}`))
	if err != nil || !strings.Contains(grepResult, "visible.txt") || strings.Contains(grepResult, "hidden.txt") {
		t.Fatalf("Grep result = %q, error %v, want visible and no .git file", grepResult, err)
	}

	requests := make(chan capturedRequest, 1)
	server := successfulServer(t, requests)
	cfg := openConfig(t, server.URL)
	cfg.Root = root
	session, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stream := session.Send(context.Background(), "declare tools")
	drain(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	request := <-requests
	var declarations []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(request.body["tools"], &declarations); err != nil {
		t.Fatal(err)
	}
	declaredNames := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		declaredNames = append(declaredNames, declaration.Function.Name)
	}
	wantDeclarations := slices.Clone(wantNames)
	slices.Sort(wantDeclarations)
	if !slices.Equal(declaredNames, wantDeclarations) {
		t.Fatalf("provider tool declarations = %q, want %q", declaredNames, wantDeclarations)
	}
}

// R-VADY-2ZQW
func TestOpenPassesUnknownSettingsThroughUntilSend(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := successfulServer(t, requests)
	cfg := openConfig(t, server.URL)
	cfg.Settings = map[string]string{"unknown_future_option": "verbatim value"}
	session, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open rejected passthrough settings: %v", err)
	}
	stream := session.Send(context.Background(), "validate later")
	drain(stream)
	if !errors.Is(stream.Err(), agentkit.ErrInvalidConfig) {
		t.Fatalf("Send error = %v, want agentkit.ErrInvalidConfig", stream.Err())
	}
	select {
	case request := <-requests:
		t.Fatalf("invalid setting reached provider: %+v", request)
	default:
	}
}

// R-VBLU-GRHL
func TestOpenPassesLogToConversation(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := successfulServer(t, requests)
	var output bytes.Buffer
	cfg := openConfig(t, server.URL)
	cfg.Log = agentkit.NewLog(&output, func() time.Time { return time.Unix(1, 0).UTC() })
	session, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stream := session.Send(context.Background(), "logged turn")
	drain(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if output.Len() == 0 || !bytes.Contains(output.Bytes(), []byte(`"type"`)) {
		t.Fatalf("log output = %q, want conversation records", output.Bytes())
	}
}

type capturedRequest struct {
	path   string
	header http.Header
	body   map[string]json.RawMessage
}

func successfulServer(t *testing.T, requests chan<- capturedRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		data, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(data, &body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests <- capturedRequest{path: request.URL.Path, header: request.Header.Clone(), body: body}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

func openConfig(t *testing.T, baseURL string) Config {
	t.Helper()
	return Config{
		Provider: "openai",
		Model:    "gpt-5.6-sol",
		Wire:     "chat",
		Auth:     "api_key",
		BaseURL:  baseURL,
		Home:     t.TempDir(),
		Getenv:   func(string) string { return "test-api-key" },
		Root:     t.TempDir(),
	}
}

func drain(stream *agentkit.Stream) {
	for event := range stream.Events() {
		_ = event
	}
}
