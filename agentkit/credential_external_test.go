package agentkit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/agentkit/anthropic"
)

type signingAuth struct {
	t              *testing.T
	wantContext    context.Context
	applyCallCount int
}

type authContextKey struct{}

type parityAnthropicAuth struct{}

func (parityAnthropicAuth) Apply(context.Context, *http.Request, []byte) error { return nil }
func (parityAnthropicAuth) EndpointIdentity() string                           { return "anthropic" }

type configParityServer struct {
	t        *testing.T
	requests [][]byte
}

func (server *configParityServer) handle(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		server.t.Errorf("read request: %v", err)
		return
	}
	server.requests = append(server.requests, body)
	writer.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n"+
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"+
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"same reply\"}}\n\n"+
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
		"data: {\"type\":\"message_delta\",\"delta\":{\"usage\":{\"output_tokens\":3}}}\n\n"+
		"data: {\"type\":\"message_stop\"}\n\n")
}

func (auth *signingAuth) Apply(ctx context.Context, request *http.Request, body []byte) error {
	auth.applyCallCount++
	if ctx != auth.wantContext {
		auth.t.Error("auth did not receive final request context")
	}
	if !strings.Contains(string(body), `"messages"`) {
		auth.t.Errorf("auth body = %q, want encoded Anthropic body", body)
	}
	request.Header.Set("X-Body-Signature", fmt.Sprint(len(body)))
	return nil
}

func TestGenericKnownWireAcceptsBareAuthApplier(t *testing.T) {
	// R-3KQY-RN5S
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.Header.Get("X-Body-Signature") == "" {
			t.Error("body-signing AuthApplier was not called")
		}
		writer.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), authContextKey{}, "final")
	auth := &signingAuth{t: t, wantContext: ctx}
	endpoint, err := agentkit.NewEndpoint(server.URL, auth)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := agentkit.New(agentkit.KnownWireAnthropicMessages, endpoint, "external-model", agentkit.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for event := range conversation.Send(ctx, agentkit.Text{Text: "hello"}).Events() {
		_ = event
	}
	if auth.applyCallCount != 1 || requestCount != 1 {
		t.Fatalf("calls: auth=%d request=%d", auth.applyCallCount, requestCount)
	}
}

func TestVendorAndRootConfigConstructionHaveWireAndEventParity(t *testing.T) {
	// R-OLM8-8NER
	capture := &configParityServer{t: t}
	server := httptest.NewServer(http.HandlerFunc(capture.handle))
	defer server.Close()

	var logOutput bytes.Buffer
	cfg := newConfigParityConfig(t, &logOutput)
	vendorConversation, rootConversation := newConfigParityConversations(t, server.URL, cfg)
	vendorEvents, vendorErr := collectExternalEvents(vendorConversation.Send(context.Background(), agentkit.Text{Text: "hello"}))
	rootEvents, rootErr := collectExternalEvents(rootConversation.Send(context.Background(), agentkit.Text{Text: "hello"}))

	if vendorErr != nil || rootErr != nil {
		t.Fatalf("Send errors = vendor %v, root %v", vendorErr, rootErr)
	}
	if len(capture.requests) != 2 || !bytes.Equal(capture.requests[0], capture.requests[1]) {
		t.Fatalf("request bodies differ: vendor=%q root=%q", valueAt(capture.requests, 0), valueAt(capture.requests, 1))
	}
	if !reflect.DeepEqual(vendorEvents, rootEvents) {
		t.Fatalf("event streams differ: vendor=%#v root=%#v", vendorEvents, rootEvents)
	}
	assertConfigParityAccounting(t, &logOutput)
}

func newConfigParityConfig(t *testing.T, logOutput io.Writer) agentkit.Config {
	t.Helper()
	tool, err := agentkit.NewTool("lookup", "Look up a value.", func(context.Context, struct {
		Query string `json:"query" jsonschema:"required"`
	}) (string, error) {
		return "unused", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return agentkit.Config{
		Tools:    []agentkit.Tool{tool},
		Settings: agentkit.Settings{ToolChoice: agentkit.ToolChoice{Mode: agentkit.ToolChoiceRequired}},
		Options:  agentkit.ProviderOptions{"metadata": json.RawMessage(`{"user_id":"parity"}`)},
		Log:      agentkit.NewLog(logOutput, nil),
	}
}

func newConfigParityConversations(t *testing.T, baseURL string, cfg agentkit.Config) (*agentkit.Conversation, *agentkit.Conversation) {
	t.Helper()
	const model = "claude-haiku-4-5"
	vendorConversation, err := anthropic.New(anthropic.APIKey("key"), model, anthropic.WithBaseURL(baseURL), anthropic.WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := agentkit.NewEndpoint(baseURL, parityAnthropicAuth{})
	if err != nil {
		t.Fatal(err)
	}
	rootConversation, err := agentkit.New(agentkit.KnownWireAnthropicMessages, endpoint, model, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return vendorConversation, rootConversation
}

func assertConfigParityAccounting(t *testing.T, logOutput io.Reader) {
	t.Helper()
	var accounting []agentkit.LogRecord
	decoder := json.NewDecoder(logOutput)
	for {
		var record agentkit.LogRecord
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record.Type == agentkit.RecordUsage {
			accounting = append(accounting, record)
		}
	}
	if len(accounting) != 2 || accounting[0].Usage == nil || accounting[0].Cost == nil ||
		accounting[1].Usage == nil || accounting[1].Cost == nil ||
		!reflect.DeepEqual(accounting[0].Usage, accounting[1].Usage) || *accounting[0].Cost != *accounting[1].Cost {
		t.Fatalf("usage/cost differ: vendor=%#v root=%#v", valueAt(accounting, 0), valueAt(accounting, 1))
	}
}

func collectExternalEvents(stream *agentkit.Stream) ([]agentkit.Event, error) {
	var events []agentkit.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events, stream.Err()
}

func valueAt[T any](values []T, index int) any {
	if index >= len(values) {
		return nil
	}
	return values[index]
}

func TestVendorCredentialsAndOptionsAreCompileTimeIsolated(t *testing.T) {
	// R-3IB6-03OE
	// R-65FB-U7IK
	temporary := t.TempDir()
	writeCompileFixture(t, temporary)
	output, err := runCompileFixture(temporary)
	if err == nil {
		t.Fatal("cross-vendor credentials and options unexpectedly compiled")
	}
	assertCompileIsolation(t, output)
}

func writeCompileFixture(t *testing.T, directory string) {
	t.Helper()
	module := `module compilefixture

go 1.26

require github.com/ikigenba/ikigenba/agentkit v0.0.0

replace github.com/ikigenba/ikigenba/agentkit => ` + moduleRoot(t) + "\n"
	source := `package compilefixture

import (
	"net/http"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/agentkit/anthropic"
	"github.com/ikigenba/ikigenba/agentkit/gemini"
	"github.com/ikigenba/ikigenba/agentkit/openai"
	"github.com/ikigenba/ikigenba/agentkit/openrouter"
	"github.com/ikigenba/ikigenba/agentkit/xai"
)

func invalid() {
	_, _ = anthropic.New(openai.APIKey("wrong"), "model")
	_, _ = openai.New(anthropic.APIKey("wrong"), "model")
	_, _ = xai.New(openrouter.APIKey("wrong"), "model")
	_, _ = openrouter.New(gemini.APIKey("wrong"), "model")
	_, _ = gemini.New(xai.APIKey("wrong"), "model")
	_, _ = anthropic.New(anthropic.APIKey("key"), "model", openai.WithBaseURL("https://example.test"))
	_, _ = anthropic.New(anthropic.APIKey("key"), "model", openai.WithBaseURL("https://example.test"))
	_, _ = agentkit.NewEndpoint("https://example.test", nil, openai.WithBaseURL("https://example.test"))
}
`
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "compile_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runCompileFixture(directory string) ([]byte, error) {
	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	output, err := command.CombinedOutput()
	return output, err
}

func assertCompileIsolation(t *testing.T, output []byte) {
	t.Helper()
	for _, want := range []string{"missing method apply", "openai.Option", "too many arguments in call to agentkit.NewEndpoint"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("compile failure missing %q:\n%s", want, output)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
