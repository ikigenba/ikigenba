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
	configType := reflect.TypeFor[Config]()
	if configType.Name() != "Config" || configType.Kind() != reflect.Struct {
		t.Fatalf("Config name/kind = %q/%s, want exported defined struct", configType.Name(), configType.Kind())
	}
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Tools", typeOf: reflect.TypeFor[[]Tool]()},
		{name: "Deferred", typeOf: reflect.TypeFor[[]DeferredGroup]()},
		{name: "Settings", typeOf: reflect.TypeFor[Settings]()},
		{name: "Output", typeOf: reflect.TypeFor[*OutputContract]()},
		{name: "Log", typeOf: reflect.TypeFor[*Log]()},
		{name: "Limits", typeOf: reflect.TypeFor[Limits]()},
	}
	if configType.NumField() != len(wantFields) {
		t.Fatalf("Config field count = %d, want exactly %d", configType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := configType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf || !field.IsExported() {
			t.Fatalf("Config field %d = %s %s (exported=%t), want %s %s exported", index, field.Name, field.Type, field.IsExported(), want.name, want.typeOf)
		}
	}
}

func TestMessageDoneDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0B78-ZYU3
	assertEventWrapper(t, "MessageDone", reflect.TypeFor[MessageDone](), "Message", reflect.TypeFor[Message]())
}

func TestOutputDoneDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-TOUQ-SVMB
	assertEventWrapper(t, "OutputDone", reflect.TypeFor[OutputDone](), "Value", reflect.TypeFor[json.RawMessage]())
}

func TestToolCallDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0CF5-DQKS
	assertEventWrapper(t, "ToolCall", reflect.TypeFor[ToolCall](), "Use", reflect.TypeFor[ToolUse]())
}

func TestToolReturnDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0DN1-RIBH
	assertEventWrapper(t, "ToolReturn", reflect.TypeFor[ToolReturn](), "Result", reflect.TypeFor[ToolResult]())
}

func assertEventWrapper(t *testing.T, name string, wrapper reflect.Type, fieldName string, fieldType reflect.Type) {
	t.Helper()
	if wrapper.Name() != name || wrapper.Kind() != reflect.Struct {
		t.Fatalf("%s name/kind = %q/%s, want exported defined struct", name, wrapper.Name(), wrapper.Kind())
	}
	if wrapper.NumField() != 1 {
		t.Fatalf("%s field count = %d, want exactly one", name, wrapper.NumField())
	}
	field := wrapper.Field(0)
	if field.Name != fieldName || field.Type != fieldType || !field.IsExported() || field.Anonymous {
		t.Fatalf("%s.%s = %s (exported=%t, anonymous=%t), want %s exported", name, field.Name, field.Type, field.IsExported(), field.Anonymous, fieldType)
	}
	if !wrapper.Implements(reflect.TypeFor[Event]()) {
		t.Fatalf("%s does not implement Event", name)
	}
}

func TestConversationConstructionFixesOrchestrationConfiguration(t *testing.T) {
	// R-NW62-A0NM
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpointA, err := NewEndpoint(auth, WithBaseURL("https://one.invalid/messages"))
	if err != nil {
		t.Fatal(err)
	}
	endpointB, err := NewEndpoint(auth, WithBaseURL("https://two.invalid/responses"))
	if err != nil {
		t.Fatal(err)
	}
	tool := fixtureTool{name: "fixed_tool", schema: json.RawMessage(`{"type":"object"}`)}
	cfg := Config{
		Tools:    []Tool{tool},
		Settings: Settings{Options: Options{"temperature": "0.2", "stop": `["fixed"]`}},
	}
	first, err := New(AnthropicMessagesWire(), endpointA, "model-a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(OpenAIResponsesWire(), endpointB, "model-b", cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools[0] = fixtureTool{name: "mutated", schema: json.RawMessage(`{"type":"object"}`)}
	cfg.Settings.Options["stop"] = `["mutated"]`

	firstProvider := first.provider.(*composedProvider)
	secondProvider := second.provider.(*composedProvider)
	if reflect.TypeOf(firstProvider.wire) != reflect.TypeOf(AnthropicMessagesWire()) {
		t.Fatalf("first construction wire = %T, want Anthropic", firstProvider.wire)
	}
	if reflect.TypeOf(secondProvider.wire) != reflect.TypeOf(OpenAIResponsesWire()) {
		t.Fatalf("second construction wire = %T, want OpenAI Responses", secondProvider.wire)
	}
	if first.identity.Model != "model-a" || second.identity.Model != "model-b" ||
		firstProvider.endpoint.config.baseURL.String() != "https://one.invalid/messages" ||
		secondProvider.endpoint.config.baseURL.String() != "https://two.invalid/responses" {
		t.Fatalf("construction identities/endpoints changed: %#v/%#v", first.identity, second.identity)
	}
	for index, conversation := range []*Conversation{first, second} {
		if len(conversation.tools) != 1 || conversation.tools[0].Name() != "fixed_tool" ||
			conversation.settings.Options["stop"] != `["fixed"]` {
			t.Fatalf("conversation %d construction config changed: tools=%v settings=%#v", index, toolNames(conversation.tools), conversation.settings)
		}
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

	type symbolCheck struct {
		name string
		got  reflect.Type
		want reflect.Type
		kind reflect.Kind
	}
	symbols := []symbolCheck{
		{name: "New", got: reflect.TypeOf(New), want: reflect.TypeOf(func(WireFormat, Endpoint, string, Config) (*Conversation, error) { return nil, nil }), kind: reflect.Func},
		{name: "NewEndpoint", got: reflect.TypeOf(NewEndpoint), want: reflect.TypeOf(func(Authenticator, ...EndpointOption) (Endpoint, error) { return Endpoint{}, nil }), kind: reflect.Func},
		{name: "EndpointOption", got: reflect.TypeFor[EndpointOption](), kind: reflect.Func},
		{name: "WithBaseURL", got: reflect.TypeOf(WithBaseURL), want: reflect.TypeOf(func(string) EndpointOption { return nil }), kind: reflect.Func},
		{name: "Endpoint", got: reflect.TypeFor[Endpoint](), kind: reflect.Struct},
		{name: "Authenticator", got: reflect.TypeFor[Authenticator](), kind: reflect.Interface},
		{name: "WireFormat", got: reflect.TypeFor[WireFormat](), kind: reflect.Interface},
		{name: "AnthropicMessagesWire", got: reflect.TypeOf(AnthropicMessagesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "GeminiGenerateContentWire", got: reflect.TypeOf(GeminiGenerateContentWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "ChatWire", got: reflect.TypeOf(ChatWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "ResponsesWire", got: reflect.TypeOf(ResponsesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "OpenAIChatWire", got: reflect.TypeOf(OpenAIChatWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "OpenAIResponsesWire", got: reflect.TypeOf(OpenAIResponsesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "XAIChatWire", got: reflect.TypeOf(XAIChatWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "XAIResponsesWire", got: reflect.TypeOf(XAIResponsesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "Rotator", got: reflect.TypeFor[Rotator](), kind: reflect.Interface},
		{name: "APIKeyRotator", got: reflect.TypeOf(APIKeyRotator), want: reflect.TypeOf(func(string) Rotator { return nil }), kind: reflect.Func},
		{name: "OAuthRotator", got: reflect.TypeOf(OAuthRotator), want: reflect.TypeOf(func(TokenStore) Rotator { return nil }), kind: reflect.Func},
		{name: "Token", got: reflect.TypeFor[Token](), kind: reflect.Struct},
		{name: "TokenStore", got: reflect.TypeFor[TokenStore](), kind: reflect.Interface},
		{name: "FileTokenStore", got: reflect.TypeOf(FileTokenStore), want: reflect.TypeOf(func(string) TokenStore { return nil }), kind: reflect.Func},
		{name: "AuthMode", got: reflect.TypeFor[AuthMode](), kind: reflect.String},
		{name: "Rotation", got: reflect.TypeFor[Rotation](), kind: reflect.Struct},
		{name: "EndpointSpec", got: reflect.TypeFor[EndpointSpec](), kind: reflect.Struct},
		{name: "Offering.Authenticator", got: reflect.TypeOf(Offering.Authenticator), want: reflect.TypeOf(func(Offering, Rotator) (Authenticator, error) { return nil, nil }), kind: reflect.Func},
	}
	for _, symbol := range symbols {
		if symbol.got == nil || symbol.got.Kind() != symbol.kind {
			t.Fatalf("%s type/kind = %v, want present %s", symbol.name, symbol.got, symbol.kind)
		}
		if symbol.want != nil && symbol.got != symbol.want {
			t.Fatalf("%s type = %s, want %s", symbol.name, symbol.got, symbol.want)
		}
	}
}

// R-NY6T-G0IY
func TestNewDeclarationTakesWireFormatAndRejectsNilWire(t *testing.T) {
	if got, want := reflect.TypeOf(New), reflect.TypeOf(func(WireFormat, Endpoint, string, Config) (*Conversation, error) { return nil, nil }); got != want {
		t.Fatalf("New type = %s, want %s", got, want)
	}

	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := NewEndpoint(auth, WithBaseURL("https://example.invalid/wire"))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := New(nil, endpoint, "model", Config{})
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

func assertFunctionSignature(t *testing.T, got, want reflect.Type) {
	t.Helper()
	if got.NumIn() != want.NumIn() || got.NumOut() != want.NumOut() {
		t.Fatalf("signature %s does not match %s", got, want)
	}
	for index := range got.NumIn() {
		if got.In(index) != want.In(index) {
			t.Fatalf("parameter %d = %s, want %s", index, got.In(index), want.In(index))
		}
	}
	for index := range got.NumOut() {
		if got.Out(index) != want.Out(index) {
			t.Fatalf("result %d = %s, want %s", index, got.Out(index), want.Out(index))
		}
	}
}

type architectureToolInput struct {
	Query string `json:"query" jsonschema:"required"`
}

func TestNewToolDeclarationIsExact(t *testing.T) {
	// R-DI9Q-Q8US
	got := reflect.TypeOf(NewTool[architectureToolInput])
	want := reflect.TypeOf(func(string, string, func(context.Context, architectureToolInput) (string, error), func(architectureToolInput) Access) (Tool, error) {
		return nil, nil
	})
	assertFunctionSignature(t, got, want)
}

func TestMustToolDeclarationIsExact(t *testing.T) {
	// R-DJHN-40LH
	got := reflect.TypeOf(MustTool[architectureToolInput])
	want := reflect.TypeOf(func(string, string, func(context.Context, architectureToolInput) (string, error), func(architectureToolInput) Access) Tool {
		return nil
	})
	assertFunctionSignature(t, got, want)
}

func TestNewToolFromSchemaDeclarationIsExact(t *testing.T) {
	// R-DKPJ-HSC6
	got := reflect.TypeOf(NewToolFromSchema)
	want := reflect.TypeOf(func(string, string, json.RawMessage, func(context.Context, json.RawMessage) (string, error), func(json.RawMessage) Access) (Tool, error) {
		return nil, nil
	})
	assertFunctionSignature(t, got, want)
}

func TestValidateToolSchemaDeclarationIsExact(t *testing.T) {
	// R-07JJ-UNM0
	got := reflect.TypeOf(ValidateToolSchema)
	want := reflect.TypeOf(func(json.RawMessage) error { return nil })
	assertFunctionSignature(t, got, want)
}

func TestFramerAndSSEFramesDeclarationsAreExact(t *testing.T) {
	// R-ZGPR-FPAQ
	// R-ZHXN-TH1F
	wantSignature := reflect.TypeOf(func(io.Reader) iter.Seq2[[]byte, error] { return nil })
	framerType := reflect.TypeFor[Framer]()
	if framerType.Name() != "Framer" || framerType.Kind() != reflect.Func {
		t.Fatalf("Framer name/kind = %q/%s, want exported defined function type", framerType.Name(), framerType.Kind())
	}
	if framerType.NumIn() != 1 || framerType.In(0) != wantSignature.In(0) || framerType.NumOut() != 1 || framerType.Out(0) != wantSignature.Out(0) {
		t.Fatalf("Framer signature = %s, want func%s", framerType, strings.TrimPrefix(wantSignature.String(), "func"))
	}
	sseType := reflect.TypeOf(SSEFrames)
	if sseType != wantSignature {
		t.Fatalf("SSEFrames signature = %s, want exactly %s", sseType, wantSignature)
	}
	if !sseType.AssignableTo(framerType) {
		t.Fatalf("SSEFrames type %s is not assignable to Framer %s", sseType, framerType)
	}
	var assigned Framer = SSEFrames
	if assigned == nil {
		t.Fatal("SSEFrames assignment unexpectedly produced a nil Framer")
	}
}

func TestConversationPublicShape(t *testing.T) {
	// R-WEW7-DNV4
	// R-7KE4-A3OZ
	pointerType := reflect.TypeFor[*Conversation]()
	addSystem, ok := pointerType.MethodByName("AddSystem")
	if !ok {
		t.Fatal("*Conversation has no exported AddSystem method")
	}
	wantAddSystem := reflect.TypeOf(func(*Conversation, string) error { return nil })
	if addSystem.Type != wantAddSystem {
		t.Fatalf("AddSystem type = %s, want %s", addSystem.Type, wantAddSystem)
	}
	wantMethods := map[string]reflect.Type{
		"Close":     reflect.TypeOf(func(*Conversation) error { return nil }),
		"Release":   reflect.TypeOf(func(*Conversation, Savepoint) error { return nil }),
		"Restore":   reflect.TypeOf(func(*Conversation, Savepoint) error { return nil }),
		"Savepoint": reflect.TypeOf(func(*Conversation) (Savepoint, error) { return Savepoint{}, nil }),
	}
	for name, want := range wantMethods {
		method, exists := pointerType.MethodByName(name)
		if !exists {
			t.Fatalf("*Conversation has no exported %s method", name)
		}
		if method.Type != want {
			t.Fatalf("%s type = %s, want %s", name, method.Type, want)
		}
	}
}

func TestDeferredGroupDeclarationIsExact(t *testing.T) {
	// R-0PU1-L7QF
	groupType := reflect.TypeFor[DeferredGroup]()
	if groupType.Name() != "DeferredGroup" || groupType.Kind() != reflect.Struct {
		t.Fatalf("DeferredGroup name/kind = %q/%s, want exported DeferredGroup struct", groupType.Name(), groupType.Kind())
	}
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Name", typeOf: reflect.TypeFor[string]()},
		{name: "Blurb", typeOf: reflect.TypeFor[string]()},
		{name: "Tools", typeOf: reflect.TypeFor[[]Tool]()},
	}
	if groupType.NumField() != len(wantFields) {
		t.Fatalf("DeferredGroup field count = %d, want exactly %d", groupType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := groupType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf || !field.IsExported() {
			t.Fatalf("DeferredGroup field %d = %s %s (exported=%t), want %s %s (exported=true)", index, field.Name, field.Type, field.IsExported(), want.name, want.typeOf)
		}
	}
}

func TestIdentityPublicShape(t *testing.T) {
	// R-YVZG-XLOX
	identityType := reflect.TypeOf(Identity{})
	wantNames := []string{"Endpoint", "AuthMode", "Model"}
	if identityType.Kind() != reflect.Struct || identityType.NumField() != len(wantNames) {
		t.Fatalf("Identity kind/field count = %s/%d, want struct/%d", identityType.Kind(), identityType.NumField(), len(wantNames))
	}
	for index, wantName := range wantNames {
		field := identityType.Field(index)
		if field.Name != wantName || field.Type != reflect.TypeOf("") || !field.IsExported() {
			t.Fatalf("Identity field %d = %s %s (exported=%t), want %s string (exported=true)", index, field.Name, field.Type, field.IsExported(), wantName)
		}
	}
}

func TestCategoryDeclaration(t *testing.T) {
	// R-ZAM9-IUL9
	categoryType := reflect.TypeFor[Category]()
	if categoryType.Name() != "Category" || categoryType.Kind() != reflect.Int {
		t.Fatalf("Category name/kind = %q/%s, want exported defined Category with underlying int", categoryType.Name(), categoryType.Kind())
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
	errorType := reflect.TypeFor[Error]()
	if errorType.Name() != "Error" || errorType.Kind() != reflect.Struct {
		t.Fatalf("Error name/kind = %q/%s, want exported named Error struct", errorType.Name(), errorType.Kind())
	}
	wantFields := []struct {
		name     string
		typeOf   reflect.Type
		exported bool
	}{
		{name: "Category", typeOf: reflect.TypeFor[Category](), exported: true},
		{name: "Status", typeOf: reflect.TypeFor[int](), exported: true},
		{name: "Code", typeOf: reflect.TypeFor[string](), exported: true},
		{name: "Message", typeOf: reflect.TypeFor[string](), exported: true},
		{name: "RetryAfter", typeOf: reflect.TypeFor[time.Duration](), exported: true},
		{name: "Endpoint", typeOf: reflect.TypeFor[Identity](), exported: true},
	}
	var exportedFields []reflect.StructField
	for index := range errorType.NumField() {
		if field := errorType.Field(index); field.IsExported() {
			exportedFields = append(exportedFields, field)
		}
	}
	if len(exportedFields) != len(wantFields) {
		t.Fatalf("Error exported field count = %d, want exactly %d", len(exportedFields), len(wantFields))
	}
	for index, want := range wantFields {
		field := exportedFields[index]
		if field.Name != want.name || field.Type != want.typeOf || field.IsExported() != want.exported || field.Anonymous {
			t.Fatalf("Error field %d = %s %s (exported=%t, anonymous=%t), want %s %s (exported=%t, anonymous=false)", index, field.Name, field.Type, field.IsExported(), field.Anonymous, want.name, want.typeOf, want.exported)
		}
	}

	pointerType := reflect.TypeFor[*Error]()
	if !pointerType.Implements(reflect.TypeFor[error]()) {
		t.Fatal("*Error does not implement error")
	}
	if !pointerType.Implements(reflect.TypeFor[interface{ Unwrap() error }]()) {
		t.Fatal("*Error does not implement interface { Unwrap() error }")
	}
}

func TestRetryableDeclaration(t *testing.T) {
	// R-ZD22-AE2N
	got := reflect.TypeOf(Retryable)
	want := reflect.TypeOf(func(error) bool { return false })
	if got != want {
		t.Fatalf("Retryable type = %s, want exactly %s", got, want)
	}
}

func TestRoleDeclaration(t *testing.T) {
	// R-YX7D-BDFM
	roleType := reflect.TypeFor[Role]()
	if roleType.Name() != "Role" || roleType.Kind() != reflect.Int {
		t.Fatalf("Role name/kind = %q/%s, want defined Role with underlying int", roleType.Name(), roleType.Kind())
	}
	wantNames := []string{"RoleSystem", "RoleUser", "RoleAssistant", "RoleTool"}
	wantValues := []Role{0, 1, 2, 3}
	gotValues := []Role{RoleSystem, RoleUser, RoleAssistant, RoleTool}
	if len(wantNames) != len(gotValues) || !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("Role values %v = %v, want %v", wantNames, gotValues, wantValues)
	}
}

func TestMessageDeclaration(t *testing.T) {
	// R-YYF9-P56B
	assertExactStructFields(t, reflect.TypeFor[Message](), []exactStructField{
		{name: "Role", typeOf: reflect.TypeFor[Role]()},
		{name: "Blocks", typeOf: reflect.TypeFor[[]Block]()},
	})
}

func TestHistoryDeclaration(t *testing.T) {
	// R-YZN6-2WX0
	historyType := reflect.TypeFor[History]()
	if historyType.Name() != "History" || historyType.Kind() != reflect.Slice || historyType.Elem() != reflect.TypeFor[Message]() {
		t.Fatalf("History name/kind/element = %q/%s/%s, want defined History slice of Message", historyType.Name(), historyType.Kind(), historyType.Elem())
	}
}

func TestUsageDeclaration(t *testing.T) {
	// R-ND0W-8GRT
	assertExactStructFields(t, reflect.TypeFor[Usage](), []exactStructField{
		{name: "InputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "CachedTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite5mTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite1hTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "OutputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "ReasoningTokens", typeOf: reflect.TypeFor[int64]()},
	})
}

func TestCostDeclarationIsExact(t *testing.T) {
	// R-NHWH-RJQL
	costType := reflect.TypeFor[Cost]()
	if costType.Name() != "Cost" || costType.Kind() != reflect.Int64 {
		t.Fatalf("Cost name/kind = %q/%s, want exported defined int64", costType.Name(), costType.Kind())
	}
}

func TestRateTierDeclarationIsExact(t *testing.T) {
	// R-NJ4E-5BHA
	assertExactStructFields(t, reflect.TypeFor[RateTier](), []exactStructField{
		{name: "MinInputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "InputUncached", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheReadInput", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite5m", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite1h", typeOf: reflect.TypeFor[int64]()},
		{name: "Output", typeOf: reflect.TypeFor[int64]()},
	})
}

func TestPricingDeclarationAndCostMethodAreExact(t *testing.T) {
	// R-NKCA-J37Z
	// R-NLK6-WUYO
	assertExactStructFields(t, reflect.TypeFor[Pricing](), []exactStructField{
		{name: "Tiers", typeOf: reflect.TypeFor[[]RateTier]()},
	})

	pricingType := reflect.TypeFor[Pricing]()
	method, ok := pricingType.MethodByName("Cost")
	wantMethod := reflect.TypeOf(func(Pricing, Usage) Cost { return 0 })
	if !ok || method.Type != wantMethod {
		t.Fatalf("Pricing.Cost = %v (present=%t), want exact value-receiver signature %s", method.Type, ok, wantMethod)
	}
}

func TestTextDeclaration(t *testing.T) {
	// R-Z0V2-GONP
	assertExactBlockStruct(t, reflect.TypeFor[Text](), []exactStructField{
		{name: "Text", typeOf: reflect.TypeFor[string]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestReasoningDeclaration(t *testing.T) {
	// R-Z22Y-UGEE
	assertExactBlockStruct(t, reflect.TypeFor[Reasoning](), []exactStructField{
		{name: "Text", typeOf: reflect.TypeFor[string]()},
		{name: "Redacted", typeOf: reflect.TypeFor[bool]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestToolUseDeclaration(t *testing.T) {
	// R-Z3AV-8853
	assertExactBlockStruct(t, reflect.TypeFor[ToolUse](), []exactStructField{
		{name: "ID", typeOf: reflect.TypeFor[string]()},
		{name: "Name", typeOf: reflect.TypeFor[string]()},
		{name: "Input", typeOf: reflect.TypeFor[json.RawMessage]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestToolResultDeclaration(t *testing.T) {
	// R-Z4IR-LZVS
	assertExactBlockStruct(t, reflect.TypeFor[ToolResult](), []exactStructField{
		{name: "ToolUseID", typeOf: reflect.TypeFor[string]()},
		{name: "Content", typeOf: reflect.TypeFor[string]()},
		{name: "IsError", typeOf: reflect.TypeFor[bool]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestReasoningModeDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-ZWKG-EPXR
	want := []ReasoningMode{0, 1, 2, 3, 4}
	got := []ReasoningMode{ReasoningDefault, ReasoningOff, ReasoningOn, ReasoningEffort, ReasoningBudget}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReasoningMode values = %v, want %v", got, want)
	}
	assertDefinedIntNamed(t, reflect.TypeFor[ReasoningMode](), "ReasoningMode", []string{
		"ReasoningDefault", "ReasoningOff", "ReasoningOn", "ReasoningEffort", "ReasoningBudget",
	}, len(got))
}

func TestEffortDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-NU3H-L95J
	want := []Effort{0, 1, 2, 3, 4, 5, 6}
	got := []Effort{EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Effort values = %v, want %v", got, want)
	}
	assertDefinedIntNamed(t, reflect.TypeFor[Effort](), "Effort", []string{
		"EffortNone", "EffortMinimal", "EffortLow", "EffortMedium", "EffortHigh", "EffortXHigh", "EffortMax",
	}, len(got))
}

func TestReasoningConfigDeclarationHasExactNeutralReasoningFields(t *testing.T) {
	// R-ZXSC-SHOG
	assertExactStructFields(t, reflect.TypeFor[ReasoningConfig](), []exactStructField{
		{name: "Mode", typeOf: reflect.TypeFor[ReasoningMode]()},
		{name: "Effort", typeOf: reflect.TypeFor[Effort]()},
		{name: "Budget", typeOf: reflect.TypeFor[int]()},
	})
}

func TestToolChoiceDeclarationHasExactNeutralSelectionFields(t *testing.T) {
	// R-0085-K15U
	assertExactStructFields(t, reflect.TypeFor[ToolChoice](), []exactStructField{
		{name: "Mode", typeOf: reflect.TypeFor[ToolChoiceMode]()},
		{name: "Name", typeOf: reflect.TypeFor[string]()},
	})
}

func TestToolChoiceModeDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-01G1-XSWJ
	want := []ToolChoiceMode{0, 1, 2, 3}
	got := []ToolChoiceMode{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired, ToolChoiceTool}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ToolChoiceMode values = %v, want %v", got, want)
	}
	assertDefinedIntNamed(t, reflect.TypeFor[ToolChoiceMode](), "ToolChoiceMode", []string{
		"ToolChoiceAuto", "ToolChoiceNone", "ToolChoiceRequired", "ToolChoiceTool",
	}, len(got))
}

func assertDefinedIntNamed(t *testing.T, got reflect.Type, name string, wantNames []string, values int) {
	t.Helper()
	if got.Name() != name || got.Kind() != reflect.Int {
		t.Fatalf("%s = %q/%s, want defined type %s with underlying int", name, got.Name(), got.Kind(), name)
	}
	if len(wantNames) != values {
		t.Fatalf("%s constants = %d, want %d", name, values, len(wantNames))
	}
}

type exactStructField struct {
	name   string
	typeOf reflect.Type
}

func assertExactBlockStruct(t *testing.T, got reflect.Type, want []exactStructField) {
	t.Helper()
	assertExactStructFields(t, got, want)
	if !got.Implements(reflect.TypeFor[Block]()) {
		t.Fatalf("%s does not implement Block as a value", got)
	}
}

func assertExactStructFields(t *testing.T, got reflect.Type, want []exactStructField) {
	t.Helper()
	if got.Name() == "" {
		t.Fatalf("%s name = %q, want exported named type", got, got.Name())
	}
	if got.Kind() != reflect.Struct || got.NumField() != len(want) {
		t.Fatalf("%s kind/field count = %s/%d, want struct/%d", got, got.Kind(), got.NumField(), len(want))
	}
	for index, wantField := range want {
		field := got.Field(index)
		if field.Name != wantField.name || field.Type != wantField.typeOf || !field.IsExported() || field.Anonymous {
			t.Fatalf("%s field %d = %s %s (exported=%t, anonymous=%t), want %s %s (exported=true, anonymous=false)", got, index, field.Name, field.Type, field.IsExported(), field.Anonymous, wantField.name, wantField.typeOf)
		}
	}
}
