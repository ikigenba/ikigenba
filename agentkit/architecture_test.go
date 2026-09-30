package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var (
	_ Event = MessageDone{}
	_ Event = ToolCall{}
	_ Event = ToolReturn{}
	_ Event = OutputDone{}
)

func TestConfigDeclarationIsExact(t *testing.T) {
	// R-TYGN-9I06
	cfg := Config(struct {
		Tools    []Tool
		Deferred []DeferredGroup
		Settings Settings
		Output   *OutputContract
		Log      *Log
		Limits   Limits
	}{Tools: []Tool{phase17Tool("config_tool")}, Deferred: []DeferredGroup{{Name: "group"}}})
	if len(cfg.Tools) != 1 || len(cfg.Deferred) != 1 || cfg.Deferred[0].Name != "group" || cfg.Output != nil || cfg.Log != nil {
		t.Fatalf("Config fields did not carry the constructed values: %+v", cfg)
	}
}

func TestMessageDoneDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0B78-ZYU3
	done := MessageDone(struct{ Message Message }{Message: Message{Role: RoleAssistant}})
	var event Event = done
	if got, ok := event.(MessageDone); !ok || got.Message.Role != RoleAssistant {
		t.Fatalf("Event = %#v, want MessageDone carrying its Message", event)
	}
}

func TestOutputDoneDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-TOUQ-SVMB
	done := OutputDone(struct{ Value json.RawMessage }{Value: json.RawMessage(`{"ok":true}`)})
	var event Event = done
	if got, ok := event.(OutputDone); !ok || string(got.Value) != `{"ok":true}` {
		t.Fatalf("Event = %#v, want OutputDone carrying its Value", event)
	}
}

func TestToolCallDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0CF5-DQKS
	call := ToolCall(struct{ Use ToolUse }{Use: ToolUse{Name: "lookup"}})
	var event Event = call
	if got, ok := event.(ToolCall); !ok || got.Use.Name != "lookup" {
		t.Fatalf("Event = %#v, want ToolCall carrying its Use", event)
	}
}

func TestToolReturnDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0DN1-RIBH
	ret := ToolReturn(struct{ Result ToolResult }{Result: ToolResult{ToolUseID: "lookup"}})
	var event Event = ret
	if got, ok := event.(ToolReturn); !ok || got.Result.ToolUseID != "lookup" {
		t.Fatalf("Event = %#v, want ToolReturn carrying its Result", event)
	}
}

func TestConversationConstructionFixesOrchestrationConfiguration(t *testing.T) {
	// R-NW62-A0NM
	frame := func(event, payload string) string { return "event: " + event + "\ndata: " + payload + "\n\n" }
	finalText := frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	loadGroup := frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"load_1","name":"load_tools","input":{}}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"names\":[\"records\"]}"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	responses := []string{finalText, loadGroup, finalText}
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		bodies = append(bodies, body)
		if len(bodies) > len(responses) {
			t.Errorf("request %d exceeded scripted responses", len(bodies))
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, responses[len(bodies)-1])
	}))
	t.Cleanup(server.Close)

	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := NewEndpoint(auth, WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Tools:    []Tool{phase17Tool("fixed_tool")},
		Deferred: []DeferredGroup{{Name: "records", Blurb: "fixed blurb", Tools: []Tool{phase17Tool("records_lookup")}}},
		Settings: Settings{Options: Options{"temperature": "0.25", "stop": `["fixed"]`, "max_output_tokens": "64"}},
	}
	conversation, err := New(AnthropicMessagesWire(), endpoint, "model-a", cfg)
	if err != nil {
		t.Fatal(err)
	}

	cfg.Tools[0] = phase17Tool("mutated_tool")
	cfg.Deferred[0].Tools[0] = phase17Tool("mutated_lookup")
	cfg.Deferred[0].Blurb = "mutated blurb"
	cfg.Deferred[0].Name = "mutated_records"
	cfg.Settings.Options["temperature"] = "0.75"
	cfg.Settings.Options["stop"] = `["mutated"]`
	cfg.Settings.Options["top_p"] = "0.5"
	cfg.Settings.Options["max_output_tokens"] = "128"

	for _, prompt := range []string{"first", "second"} {
		stream := conversation.Send(context.Background(), Text{Text: prompt})
		drainStream(stream)
		if stream.Err() != nil {
			t.Fatalf("Send(%q) = %v", prompt, stream.Err())
		}
	}
	if len(bodies) != len(responses) {
		t.Fatalf("requests = %d, want %d", len(bodies), len(responses))
	}

	type sentTool struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var loadedMember bool
	for index, body := range bodies {
		var request struct {
			Tools         []sentTool `json:"tools"`
			Temperature   *float64   `json:"temperature"`
			TopP          *float64   `json:"top_p"`
			MaxTokens     *int64     `json:"max_tokens"`
			StopSequences []string   `json:"stop_sequences"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("request %d body %s: %v", index, body, err)
		}
		if request.Temperature == nil || *request.Temperature != 0.25 || request.TopP != nil ||
			request.MaxTokens == nil || *request.MaxTokens != 64 ||
			!slices.Equal(request.StopSequences, []string{"fixed"}) {
			t.Fatalf("request %d settings changed after construction: %s", index, body)
		}
		names := make([]string, len(request.Tools))
		for toolIndex, tool := range request.Tools {
			names[toolIndex] = tool.Name
			if tool.Name == loadToolsName && (!strings.Contains(tool.Description, "fixed blurb") || strings.Contains(tool.Description, "mutated")) {
				t.Fatalf("request %d loader catalog changed after construction: %q", index, tool.Description)
			}
		}
		if !slices.Contains(names, "fixed_tool") || !slices.Contains(names, loadToolsName) ||
			slices.Contains(names, "mutated_tool") || slices.Contains(names, "mutated_lookup") {
			t.Fatalf("request %d tools changed after construction: %v", index, names)
		}
		if slices.Contains(names, "records_lookup") {
			loadedMember = true
		}
	}
	if !loadedMember {
		t.Fatal("loading the deferred group never advertised the constructed member records_lookup")
	}
}

// R-KAG7-PUS7
func TestConversationIdentityMatchesOfferingAndRotatorWithAndWithoutBaseURLOverride(t *testing.T) {
	var offering Offering
	found := false
	for _, entry := range Catalog() {
		for _, candidate := range entry.Offerings {
			for _, spec := range candidate.Endpoints {
				if spec.AuthMode == AuthModeAPIKey {
					offering, found = candidate, true
				}
			}
			if found {
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no cataloged offering accepts AuthModeAPIKey")
	}
	const wantEndpoint = "anthropic-messages"
	if got := string(offering.ID); got != wantEndpoint {
		t.Fatalf("selected Offering.ID = %q, want fixture %q", got, wantEndpoint)
	}

	rotator := APIKeyRotator("test-key")
	const wantAuthMode = "api_key"
	if got := string(rotator.AuthMode()); got != wantAuthMode {
		t.Fatalf("rotator.AuthMode = %q, want fixture %q", got, wantAuthMode)
	}
	auth, err := offering.Authenticator(rotator)
	if err != nil {
		t.Fatalf("Authenticator: %v", err)
	}

	for _, tc := range []struct {
		name string
		opts []EndpointOption
	}{
		{name: "offering default base URL", opts: nil},
		{name: "WithBaseURL override", opts: []EndpointOption{WithBaseURL("https://override.invalid/v1")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, err := NewEndpoint(auth, tc.opts...)
			if err != nil {
				t.Fatalf("NewEndpoint: %v", err)
			}
			conversation, err := New(offering.WireFormat, endpoint, offering.WireModel, Config{})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := conversation.identity.Endpoint; got != wantEndpoint {
				t.Fatalf("Identity.Endpoint = %q, want offering identity %q", got, wantEndpoint)
			}
			if got := conversation.identity.AuthMode; got != wantAuthMode {
				t.Fatalf("Identity.AuthMode = %q, want rotator mode %q", got, wantAuthMode)
			}
		})
	}
}

// R-LYDD-8J87
func TestConstructionSeamIsExactAndSufficientForEveryOffering(t *testing.T) {
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			t.Run(entry.Model+"/"+string(offering.ID), func(t *testing.T) {
				var rotator Rotator
				var endpointSpec EndpointSpec
				for _, spec := range offering.Endpoints {
					if spec.AuthMode == AuthModeAPIKey {
						rotator = APIKeyRotator("test-key")
						endpointSpec = spec
						break
					}
				}
				if rotator == nil {
					rotator = &tokenSourceStub{}
					for _, spec := range offering.Endpoints {
						if spec.AuthMode == AuthModeOAuth {
							endpointSpec = spec
							break
						}
					}
				}
				authenticator, err := offering.Authenticator(rotator)
				if err != nil {
					t.Fatalf("Authenticator: %v", err)
				}
				baseURL := endpointSpec.BaseURL
				if baseURL == "" {
					baseURL = "https://example.test"
				}
				endpoint, err := NewEndpoint(authenticator, WithBaseURL(baseURL))
				if err != nil {
					t.Fatalf("NewEndpoint: %v", err)
				}
				conversation, err := New(offering.WireFormat, endpoint, offering.WireModel, Config{})
				if err != nil || conversation == nil {
					t.Fatalf("New = (%v, %v), want non-nil conversation and nil error", conversation, err)
				}
			})
		}
	}

	// Every seam symbol is usable with its declared signature.
	_ = typed[func(WireFormat, Endpoint, string, Config) (*Conversation, error)](New)
	_ = typed[func(Authenticator, ...EndpointOption) (Endpoint, error)](NewEndpoint)
	_ = typed[func(string) EndpointOption](WithBaseURL)
	_ = typed[Endpoint](Endpoint{})
	_ = typed[Authenticator](authFunc(nil))
	_ = typed[func() WireFormat](AnthropicMessagesWire)
	_ = typed[func() WireFormat](GeminiGenerateContentWire)
	_ = typed[func() WireFormat](ChatWire)
	_ = typed[func() WireFormat](ResponsesWire)
	_ = typed[func() WireFormat](OpenAIChatWire)
	_ = typed[func() WireFormat](OpenAIResponsesWire)
	_ = typed[func() WireFormat](XAIChatWire)
	_ = typed[func() WireFormat](XAIResponsesWire)
	_ = typed[Rotator](&tokenSourceStub{})
	_ = typed[func(string) Rotator](APIKeyRotator)
	_ = typed[func(TokenStore) Rotator](OAuthRotator)
	_ = typed[Token](Token{})
	_ = typed[func(string) TokenStore](FileTokenStore)
	_ = typed[Rotation](Rotation{})
	_ = typed[EndpointSpec](EndpointSpec{})
	_ = typed[func(Offering, Rotator) (Authenticator, error)](Offering.Authenticator)
	mode := AuthMode("api_key")
	if string(mode) != string(AuthModeAPIKey) {
		t.Fatalf("AuthMode(%q) != AuthModeAPIKey %q", mode, AuthModeAPIKey)
	}
}

// R-BAV0-6DSH
func TestNewDeclarationTakesWireFormatAndRejectsNilWire(t *testing.T) {
	newConversation := typed[func(wire WireFormat, endpoint Endpoint, model string, cfg Config) (*Conversation, error)](New)

	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := NewEndpoint(auth, WithBaseURL("https://example.invalid/wire"))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := newConversation(nil, endpoint, "model", Config{})
	if conversation != nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(nil, ...) = (%v, %v), want nil ErrInvalidConfig", conversation, err)
	}

	// Each built-in wire speaks its own request grammar; the request New's
	// conversation sends is the observable proof of which codec was selected.
	anthropicGrammar := []string{"max_tokens", "messages", "model", "stream"}
	chatGrammar := []string{"max_completion_tokens", "messages", "model", "stream", "stream_options"}
	responsesGrammar := []string{"input", "max_output_tokens", "model", "store", "stream"}
	geminiGrammar := []string{"contents", "generationConfig"}
	for _, test := range []struct {
		name             string
		wire             WireFormat
		keys             []string
		anthropicVersion bool
	}{
		{"anthropic_messages", AnthropicMessagesWire(), anthropicGrammar, true},
		{"openai_responses", OpenAIResponsesWire(), responsesGrammar, false},
		{"responses", ResponsesWire(), responsesGrammar, false},
		{"openai_chat_completions", OpenAIChatWire(), chatGrammar, false},
		{"chat", ChatWire(), chatGrammar, false},
		{"gemini_generate_content", GeminiGenerateContentWire(), geminiGrammar, false},
		{"xai_chat", XAIChatWire(), chatGrammar, false},
		{"xai_responses", XAIResponsesWire(), responsesGrammar, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			type captured struct {
				header http.Header
				body   []byte
			}
			requests := make(chan captured, 1)
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				requests <- captured{header: request.Header.Clone(), body: body}
			}))
			defer server.Close()
			endpoint, err := NewEndpoint(auth, WithBaseURL(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := New(test.wire, endpoint, "model", Config{Settings: Settings{Options: Options{"max_output_tokens": "64"}}})
			if err != nil {
				t.Fatal(err)
			}
			stream := conversation.Send(context.Background(), Text{Text: "hello"})
			drainStream(stream)
			if err := stream.Err(); err != nil {
				t.Fatal(err)
			}
			request := <-requests
			var document map[string]json.RawMessage
			if err := json.Unmarshal(request.body, &document); err != nil {
				t.Fatalf("request body %s: %v", request.body, err)
			}
			keys := make([]string, 0, len(document))
			for key := range document {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, test.keys) {
				t.Fatalf("request top-level keys = %v, want the %s grammar %v; body %s", keys, test.name, test.keys, request.body)
			}
			if got := request.header.Get("anthropic-version") != ""; got != test.anthropicVersion {
				t.Fatalf("anthropic-version header present = %t, want %t", got, test.anthropicVersion)
			}
		})
	}
}

type architectureToolInput struct {
	Query string `json:"query" jsonschema:"required"`
}

func TestNewToolDeclarationIsExact(t *testing.T) {
	// R-DI9Q-Q8US
	newTool := typed[func(name, description string, fn func(ctx context.Context, in architectureToolInput) (string, error), access func(in architectureToolInput) Access) (Tool, error)](NewTool[architectureToolInput])
	tool, err := newTool("lookup", "look something up", func(context.Context, architectureToolInput) (string, error) { return "", nil }, func(architectureToolInput) Access { return BlocksAll() })
	if err != nil || tool == nil || tool.Name() != "lookup" {
		t.Fatalf("NewTool = (%v, %v), want tool named lookup", tool, err)
	}
}

func TestMustToolDeclarationIsExact(t *testing.T) {
	// R-DJHN-40LH
	mustTool := typed[func(name, description string, fn func(ctx context.Context, in architectureToolInput) (string, error), access func(in architectureToolInput) Access) Tool](MustTool[architectureToolInput])
	tool := mustTool("lookup", "look something up", func(context.Context, architectureToolInput) (string, error) { return "", nil }, func(architectureToolInput) Access { return BlocksAll() })
	if tool == nil || tool.Name() != "lookup" {
		t.Fatalf("MustTool = %v, want tool named lookup", tool)
	}
}

func TestNewToolFromSchemaDeclarationIsExact(t *testing.T) {
	// R-DKPJ-HSC6
	newToolFromSchema := typed[func(name, description string, schema json.RawMessage, fn func(ctx context.Context, args json.RawMessage) (string, error), access func(args json.RawMessage) Access) (Tool, error)](NewToolFromSchema)
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)
	tool, err := newToolFromSchema("lookup", "look something up", schema, func(context.Context, json.RawMessage) (string, error) { return "", nil }, func(json.RawMessage) Access { return BlocksAll() })
	if err != nil || tool == nil || tool.Name() != "lookup" {
		t.Fatalf("NewToolFromSchema = (%v, %v), want tool named lookup", tool, err)
	}
}

func TestValidateToolSchemaDeclarationIsExact(t *testing.T) {
	// R-07JJ-UNM0
	validateToolSchema := typed[func(schema json.RawMessage) error](ValidateToolSchema)
	if err := validateToolSchema(json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)); err != nil {
		t.Fatalf("ValidateToolSchema(object schema) = %v, want nil", err)
	}
}

func TestFramerAndSSEFramesDeclarationsAreExact(t *testing.T) {
	// R-ZGPR-FPAQ
	// R-ZHXN-TH1F
	framer := Framer(func(io.Reader) iter.Seq2[[]byte, error] { return nil })
	_ = typed[func(io.Reader) iter.Seq2[[]byte, error]](framer)
	sseFrames := typed[func(r io.Reader) iter.Seq2[[]byte, error]](SSEFrames)
	var assigned Framer = SSEFrames
	if sseFrames == nil || assigned == nil {
		t.Fatal("SSEFrames assignment unexpectedly produced a nil function")
	}
}

func TestConversationPublicShape(_ *testing.T) {
	// R-WEW7-DNV4
	// R-7KE4-A3OZ
	_ = typed[func(c *Conversation, text string) error]((*Conversation).AddSystem)
	_ = typed[func(c *Conversation) (Savepoint, error)]((*Conversation).Savepoint)
	_ = typed[func(c *Conversation, sp Savepoint) error]((*Conversation).Restore)
	_ = typed[func(c *Conversation, sp Savepoint) error]((*Conversation).Release)
	_ = typed[func(c *Conversation) error]((*Conversation).Close)
}

func TestDeferredGroupDeclarationIsExact(t *testing.T) {
	// R-0PU1-L7QF
	group := DeferredGroup(struct {
		Name  string
		Blurb string
		Tools []Tool
	}{Name: "group", Blurb: "blurb", Tools: []Tool{phase17Tool("deferred_tool")}})
	if group.Name != "group" || group.Blurb != "blurb" || len(group.Tools) != 1 {
		t.Fatalf("DeferredGroup fields did not carry the constructed values: %+v", group)
	}
}

func TestIdentityPublicShape(t *testing.T) {
	// R-YVZG-XLOX
	identity := Identity(struct {
		Endpoint string
		AuthMode string
		Model    string
	}{Endpoint: "endpoint", AuthMode: "api_key", Model: "model"})
	if identity.Endpoint != "endpoint" || identity.AuthMode != "api_key" || identity.Model != "model" {
		t.Fatalf("Identity fields did not carry the constructed values: %+v", identity)
	}
}

func TestCategoryDeclaration(t *testing.T) {
	// R-ZAM9-IUL9
	category := Category(7)
	if asInt(category) != 7 {
		t.Fatalf("Category(7) = %d, want 7", asInt(category))
	}

	wantNames := []string{
		"CategoryUnknown",
		"CategoryAuth",
		"CategoryInvalidRequest",
		"CategoryRateLimit",
		"CategoryOverloaded",
		"CategoryInsufficientQuota",
		"CategoryTimeout",
		"CategoryTransport",
	}
	wantValues := []Category{0, 1, 2, 3, 4, 5, 6, 7}
	gotValues := []Category{
		CategoryUnknown,
		CategoryAuth,
		CategoryInvalidRequest,
		CategoryRateLimit,
		CategoryOverloaded,
		CategoryInsufficientQuota,
		CategoryTimeout,
		CategoryTransport,
	}
	if len(wantNames) != len(gotValues) || !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("Category values %v = %v, want %v", wantNames, gotValues, wantValues)
	}
}

func TestErrorDeclaration(t *testing.T) {
	// R-B4LX-H3OC
	providerErr := Error{
		Category:   CategoryRateLimit,
		Status:     429,
		Code:       "rate_limited",
		Message:    "slow down",
		RetryAfter: time.Second,
		Endpoint:   Identity{Model: "model"},
	}
	category := typed[Category](providerErr.Category)
	status := typed[int](providerErr.Status)
	code := typed[string](providerErr.Code)
	message := typed[string](providerErr.Message)
	retryAfter := typed[time.Duration](providerErr.RetryAfter)
	endpoint := typed[Identity](providerErr.Endpoint)
	if category != CategoryRateLimit || status != 429 || code != "rate_limited" || message != "slow down" || retryAfter != time.Second || endpoint.Model != "model" {
		t.Fatalf("Error fields did not carry the constructed values: %+v", providerErr)
	}

	var asError error = &providerErr
	var unwrapper interface{ Unwrap() error } = &providerErr
	if asError.Error() == "" {
		t.Fatal("(*Error).Error() returned empty text")
	}
	if unwrapper.Unwrap() != nil {
		t.Fatalf("(*Error).Unwrap() = %v, want nil for an Error with no cause", unwrapper.Unwrap())
	}
}

func TestRetryableDeclaration(t *testing.T) {
	// R-ZD22-AE2N
	retryable := typed[func(err error) bool](Retryable)
	if retryable(nil) {
		t.Fatal("Retryable(nil) = true, want false")
	}
}

func TestRoleDeclaration(t *testing.T) {
	// R-YX7D-BDFM
	role := Role(3)
	if asInt(role) != 3 {
		t.Fatalf("Role(3) = %d, want 3", asInt(role))
	}
	wantNames := []string{"RoleSystem", "RoleUser", "RoleAssistant", "RoleTool"}
	wantValues := []Role{0, 1, 2, 3}
	gotValues := []Role{RoleSystem, RoleUser, RoleAssistant, RoleTool}
	if len(wantNames) != len(gotValues) || !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("Role values %v = %v, want %v", wantNames, gotValues, wantValues)
	}
}

// typed compiles only when v is assignable to T, pinning a declaration by use.
func typed[T any](v T) T { return v }

// asInt compiles only for a type whose underlying type is int.
func asInt[T ~int](v T) int { return int(v) }

// asInt64 compiles only for a type whose underlying type is int64.
func asInt64[T ~int64](v T) int64 { return int64(v) }

// asMessages compiles only for a type whose underlying type is []Message.
func asMessages[T ~[]Message](v T) []Message { return []Message(v) }

func TestMessageDeclaration(t *testing.T) {
	// R-YYF9-P56B
	message := Message(struct {
		Role   Role
		Blocks []Block
	}{Role: RoleUser, Blocks: []Block{Text{Text: "hi"}}})
	if message.Role != RoleUser || len(message.Blocks) != 1 {
		t.Fatalf("Message fields did not carry the constructed values: %+v", message)
	}
}

func TestHistoryDeclaration(t *testing.T) {
	// R-YZN6-2WX0
	history := History([]Message{{Role: RoleUser}})
	messages := asMessages(history)
	if len(messages) != 1 || messages[0].Role != RoleUser {
		t.Fatalf("History did not carry the constructed messages: %+v", history)
	}
}

func TestUsageDeclaration(t *testing.T) {
	// R-ND0W-8GRT
	usage := Usage(struct {
		InputTokens        int64
		CachedTokens       int64
		CacheWrite5mTokens int64
		CacheWrite1hTokens int64
		OutputTokens       int64
		ReasoningTokens    int64
	}{InputTokens: 1, CachedTokens: 2, CacheWrite5mTokens: 3, CacheWrite1hTokens: 4, OutputTokens: 5, ReasoningTokens: 6})
	if usage.InputTokens != 1 || usage.CachedTokens != 2 || usage.CacheWrite5mTokens != 3 || usage.CacheWrite1hTokens != 4 || usage.OutputTokens != 5 || usage.ReasoningTokens != 6 {
		t.Fatalf("Usage fields did not carry the constructed values: %+v", usage)
	}
}

func TestCostDeclarationIsExact(t *testing.T) {
	// R-BEIP-BP0K
	cost := Cost(int64(1_500_000_000))
	if asInt64(cost) != 1_500_000_000 {
		t.Fatalf("Cost(1500000000) = %d, want 1500000000", asInt64(cost))
	}
}

func TestRateTierDeclarationIsExact(t *testing.T) {
	// R-NJ4E-5BHA
	tier := RateTier(struct {
		MinInputTokens int64
		InputUncached  int64
		CacheReadInput int64
		CacheWrite5m   int64
		CacheWrite1h   int64
		Output         int64
	}{MinInputTokens: 1, InputUncached: 2, CacheReadInput: 3, CacheWrite5m: 4, CacheWrite1h: 5, Output: 6})
	if tier.MinInputTokens != 1 || tier.InputUncached != 2 || tier.CacheReadInput != 3 || tier.CacheWrite5m != 4 || tier.CacheWrite1h != 5 || tier.Output != 6 {
		t.Fatalf("RateTier fields did not carry the constructed values: %+v", tier)
	}
}

func TestPricingDeclarationAndCostMethodAreExact(t *testing.T) {
	// R-NKCA-J37Z
	// R-NLK6-WUYO
	pricing := Pricing(struct {
		Tiers []RateTier
	}{Tiers: []RateTier{{Output: 1}}})
	if len(pricing.Tiers) != 1 || pricing.Tiers[0].Output != 1 {
		t.Fatalf("Pricing fields did not carry the constructed values: %+v", pricing)
	}

	cost := typed[func(p Pricing, u Usage) Cost](Pricing.Cost)
	if got := cost(Pricing{}, Usage{InputTokens: 1000, OutputTokens: 1000}); got != 0 {
		t.Fatalf("Pricing{}.Cost(usage) = %d, want 0", got)
	}
}

func TestTextDeclaration(t *testing.T) {
	// R-Z0V2-GONP
	text := Text(struct {
		Text     string
		Provider json.RawMessage
	}{Text: "hello", Provider: json.RawMessage(`{}`)})
	var block Block = text
	if got, ok := block.(Text); !ok || got.Text != "hello" || string(got.Provider) != "{}" {
		t.Fatalf("Text as Block = %+v, want the constructed Text", block)
	}
}

func TestReasoningDeclaration(t *testing.T) {
	// R-Z22Y-UGEE
	reasoning := Reasoning(struct {
		Text     string
		Redacted bool
		Provider json.RawMessage
	}{Text: "think", Redacted: true, Provider: json.RawMessage(`{}`)})
	var block Block = reasoning
	if got, ok := block.(Reasoning); !ok || got.Text != "think" || !got.Redacted || string(got.Provider) != "{}" {
		t.Fatalf("Reasoning as Block = %+v, want the constructed Reasoning", block)
	}
}

func TestToolUseDeclaration(t *testing.T) {
	// R-Z3AV-8853
	toolUse := ToolUse(struct {
		ID       string
		Name     string
		Input    json.RawMessage
		Provider json.RawMessage
	}{ID: "id", Name: "name", Input: json.RawMessage(`{"a":1}`), Provider: json.RawMessage(`{}`)})
	var block Block = toolUse
	if got, ok := block.(ToolUse); !ok || got.ID != "id" || got.Name != "name" || string(got.Input) != `{"a":1}` || string(got.Provider) != "{}" {
		t.Fatalf("ToolUse as Block = %+v, want the constructed ToolUse", block)
	}
}

func TestToolResultDeclaration(t *testing.T) {
	// R-Z4IR-LZVS
	toolResult := ToolResult(struct {
		ToolUseID string
		Content   string
		IsError   bool
		Provider  json.RawMessage
	}{ToolUseID: "id", Content: "out", IsError: true, Provider: json.RawMessage(`{}`)})
	var block Block = toolResult
	if got, ok := block.(ToolResult); !ok || got.ToolUseID != "id" || got.Content != "out" || !got.IsError || string(got.Provider) != "{}" {
		t.Fatalf("ToolResult as Block = %+v, want the constructed ToolResult", block)
	}
}

func TestReasoningModeDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-ZWKG-EPXR
	mode := ReasoningMode(4)
	if asInt(mode) != 4 {
		t.Fatalf("ReasoningMode(4) = %d, want 4", asInt(mode))
	}
	want := []ReasoningMode{0, 1, 2, 3, 4}
	got := []ReasoningMode{ReasoningDefault, ReasoningOff, ReasoningOn, ReasoningEffort, ReasoningBudget}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReasoningMode values = %v, want %v", got, want)
	}
}

func TestEffortDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-NU3H-L95J
	effort := Effort(6)
	if asInt(effort) != 6 {
		t.Fatalf("Effort(6) = %d, want 6", asInt(effort))
	}
	want := []Effort{0, 1, 2, 3, 4, 5, 6}
	got := []Effort{EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Effort values = %v, want %v", got, want)
	}
}

func TestReasoningConfigDeclarationHasExactNeutralReasoningFields(t *testing.T) {
	// R-ZXSC-SHOG
	config := ReasoningConfig(struct {
		Mode   ReasoningMode
		Effort Effort
		Budget int
	}{Mode: ReasoningBudget, Effort: EffortHigh, Budget: 1024})
	if config.Mode != ReasoningBudget || config.Effort != EffortHigh || config.Budget != 1024 {
		t.Fatalf("ReasoningConfig fields did not carry the constructed values: %+v", config)
	}
}

func TestToolChoiceDeclarationHasExactNeutralSelectionFields(t *testing.T) {
	// R-0085-K15U
	choice := ToolChoice(struct {
		Mode ToolChoiceMode
		Name string
	}{Mode: ToolChoiceTool, Name: "tool"})
	if choice.Mode != ToolChoiceTool || choice.Name != "tool" {
		t.Fatalf("ToolChoice fields did not carry the constructed values: %+v", choice)
	}
}

func TestToolChoiceModeDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-01G1-XSWJ
	mode := ToolChoiceMode(3)
	if asInt(mode) != 3 {
		t.Fatalf("ToolChoiceMode(3) = %d, want 3", asInt(mode))
	}
	want := []ToolChoiceMode{0, 1, 2, 3}
	got := []ToolChoiceMode{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired, ToolChoiceTool}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ToolChoiceMode values = %v, want %v", got, want)
	}
}
