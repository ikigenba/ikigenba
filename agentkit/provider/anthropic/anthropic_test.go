package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentkit/provider"
	"agentkit/trace"
)

// TestR_18QA_L3I4_AuthUsesAnthropicAPIKeyHeader pins that the
// Anthropic backend authenticates with the ANTHROPIC_API_KEY value
// using Anthropic's documented `x-api-key` header (plus the required
// `anthropic-version` header), and that the request is a POST to the
// Messages API endpoint over the configured first-party host. The
// pin enforces:
//
//   - empty key is rejected at construction (no silent 401 round-trip)
//   - x-api-key carries the exact key string
//   - anthropic-version is set
//   - request method/path match the Messages API contract
//   - no Authorization: Bearer / OAuth header is sent
//
// R-18QA-L3I4: authentication uses ANTHROPIC_API_KEY as a bearer
// credential per Anthropic's documented header format; no OAuth /
// Bedrock / Vertex routing in v1.
func TestR_18QA_L3I4_AuthUsesAnthropicAPIKeyHeader(t *testing.T) {
	if _, err := New("", "claude-haiku-4-5"); err == nil {
		t.Errorf("New(\"\", model) must reject empty ANTHROPIC_API_KEY")
	}
	if _, err := New("sk-ant-test", ""); err == nil {
		t.Errorf("New(key, \"\") must reject empty model")
	}

	const apiKey = "sk-ant-test-1234"
	var (
		gotMethod  string
		gotPath    string
		gotKey     string
		gotVersion string
		gotAuth    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		// Force the Stream call to fail after we've recorded the
		// headers so the test stays fast and free of SSE plumbing.
		http.Error(w, "stop", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := New(apiKey, "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.baseURL = srv.URL

	_, streamErr := c.Stream(context.Background(), provider.Request{Model: "claude-haiku-4-5"})
	if streamErr == nil {
		t.Fatalf("Stream against 500 server must return *provider.Error")
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotKey != apiKey {
		t.Errorf("x-api-key = %q, want %q", gotKey, apiKey)
	}
	if gotVersion == "" {
		t.Errorf("anthropic-version header must be set")
	}
	if strings.HasPrefix(strings.ToLower(gotAuth), "bearer ") {
		t.Errorf("Authorization: Bearer must not be sent (got %q); R-18QA-L3I4 requires x-api-key only", gotAuth)
	}
}

// TestR_0LK7_BGEX_StreamsViaServerSentEvents pins that the
// Anthropic backend consumes the Messages API SSE transcript and
// emits normalized provider events in arrival order. The pin
// covers the documented event types — message_start,
// content_block_start/delta/stop for text + thinking + tool_use,
// message_delta, message_stop — and asserts:
//
//   - text_delta fragments surface as EventTextDelta in order
//   - a thinking block round-trips Text and Signature
//   - a tool_use block emits one EventToolUse with reassembled
//     JSON input
//   - message_stop produces EventUsage followed by EventDone with
//     the reported stop_reason
//   - the request hits the SSE Messages API endpoint with the
//     SSE Accept header (so the backend is talking to Anthropic
//     over HTTPS+SSE rather than the `claude` binary).
//
// R-0LK7-BGEX: ikigai-cli's Anthropic backend talks to the
// Anthropic Messages API directly over HTTPS using SSE for
// streaming responses; it does not delegate to the real `claude`
// binary.
func TestR_0LK7_BGEX_StreamsViaServerSentEvents(t *testing.T) {
	const transcript = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":11,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan: "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"call tool"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-abc"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello "}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_42","name":"read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"/etc/hosts\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":17}}

event: message_stop
data: {"type":"message_stop"}

`
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("accept")
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(transcript))
	}))
	defer srv.Close()

	c, err := New("sk-ant-test", "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.baseURL = srv.URL

	ch, err := c.Stream(context.Background(), provider.Request{Model: "claude-haiku-4-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("accept header = %q, want text/event-stream", gotAccept)
	}

	var events []provider.Event
	for ev := range ch {
		events = append(events, ev)
	}

	var (
		text               strings.Builder
		thinkings          []provider.EventThinking
		toolUses           []provider.EventToolUse
		usages             []provider.EventUsage
		dones              []provider.EventDone
		thinkingBeforeText bool
	)
	sawText := false
	for _, e := range events {
		switch v := e.(type) {
		case provider.EventTextDelta:
			text.WriteString(v.Text)
			sawText = true
		case provider.EventThinking:
			if !sawText {
				thinkingBeforeText = true
			}
			thinkings = append(thinkings, v)
		case provider.EventToolUse:
			toolUses = append(toolUses, v)
		case provider.EventUsage:
			usages = append(usages, v)
		case provider.EventDone:
			dones = append(dones, v)
		}
	}

	if got, want := text.String(), "hello world"; got != want {
		t.Errorf("text deltas concatenated = %q, want %q", got, want)
	}
	if len(thinkings) != 1 || thinkings[0].Text != "plan: call tool" || thinkings[0].Signature != "sig-abc" {
		t.Errorf("thinking events = %+v, want one with Text=%q Signature=%q", thinkings, "plan: call tool", "sig-abc")
	}
	if !thinkingBeforeText {
		t.Errorf("thinking event must precede text events to match transcript order")
	}
	if len(toolUses) != 1 {
		t.Fatalf("tool_use events = %d, want 1", len(toolUses))
	}
	tu := toolUses[0]
	if tu.ID != "toolu_42" || tu.Name != "read" {
		t.Errorf("tool_use id/name = %q/%q, want toolu_42/read", tu.ID, tu.Name)
	}
	if got := strings.Join(strings.Fields(string(tu.Input)), ""); got != `{"path":"/etc/hosts"}` {
		t.Errorf("tool_use input = %s, want {\"path\":\"/etc/hosts\"}", string(tu.Input))
	}
	if len(usages) != 1 || usages[0].InputTokens != 11 || usages[0].OutputTokens != 17 {
		t.Errorf("usage events = %+v, want one with InputTokens=11 OutputTokens=17", usages)
	}
	if len(dones) != 1 || dones[0].StopReason != "tool_use" {
		t.Errorf("done events = %+v, want one with StopReason=tool_use", dones)
	}
	// EventUsage must precede EventDone.
	var sawUsage bool
	for _, e := range events {
		if _, ok := e.(provider.EventUsage); ok {
			sawUsage = true
		}
		if _, ok := e.(provider.EventDone); ok {
			if !sawUsage {
				t.Errorf("EventDone arrived before EventUsage")
			}
		}
	}
}

// TestR_1TGL_373X_CacheTokenUsageStatistics pins that
// cache_read_input_tokens and cache_creation_input_tokens reported
// by Anthropic's Messages API on either message_start or
// message_delta usage blobs are surfaced on the final EventUsage.
//
// R-1TGL-373X: cache-token usage statistics are populated on the
// result event's usage object from the Messages API response.
func TestR_1TGL_373X_CacheTokenUsageStatistics(t *testing.T) {
	const transcript = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":7,"output_tokens":1,"cache_read_input_tokens":42,"cache_creation_input_tokens":13}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":19,"cache_read_input_tokens":50,"cache_creation_input_tokens":21}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(transcript))
	}))
	defer srv.Close()

	c, err := New("k", "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.baseURL = srv.URL

	ch, err := c.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var usages []provider.EventUsage
	for e := range ch {
		if u, ok := e.(provider.EventUsage); ok {
			usages = append(usages, u)
		}
	}
	if len(usages) != 1 {
		t.Fatalf("got %d EventUsage, want 1: %+v", len(usages), usages)
	}
	got := usages[0]
	want := provider.EventUsage{
		InputTokens:              7,
		OutputTokens:             19,
		CacheReadInputTokens:     50,
		CacheCreationInputTokens: 21,
	}
	if got != want {
		t.Errorf("EventUsage = %+v, want %+v", got, want)
	}
}

// TestR_4AH9_0G8M_ToolUsePassthroughIsomorphic pins that the
// Anthropic backend translates tool_use and tool_result content
// blocks as a passthrough to/from provider.Message blocks. The
// inbound side is already exercised by the SSE parser test;
// this test pins the outbound side: a Request whose Messages
// contain ToolUseBlock + ToolResultBlock + ThinkingBlock + TextBlock
// is encoded to Messages API JSON with the documented field names
// and unchanged payloads, including the tool_use input JSON and
// the thinking signature byte-for-byte.
//
// R-4AH9-0G8M: tool_use round-trip is direct — the Messages API's
// tool_use and tool_result content blocks are isomorphic to
// stream-json blocks; the translation is essentially a passthrough
// with field-name normalization.
func TestR_4AH9_0G8M_ToolUsePassthroughIsomorphic(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		http.Error(w, "stop", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := New("sk-ant-test", "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.baseURL = srv.URL

	req := provider.Request{
		Model: "claude-haiku-4-5",
		Messages: []provider.Message{
			{
				Role: provider.RoleAssistant,
				Blocks: []provider.Block{
					provider.ThinkingBlock{Text: "plan: read the file", Signature: "sig-xyz"},
					provider.TextBlock{Text: "let me look"},
					provider.ToolUseBlock{
						ID:    "toolu_42",
						Name:  "read",
						Input: json.RawMessage(`{"path":"/etc/hosts"}`),
					},
				},
			},
			{
				Role: provider.RoleUser,
				Blocks: []provider.Block{
					provider.ToolResultBlock{
						ToolUseID: "toolu_42",
						Content:   "127.0.0.1 localhost\n",
						IsError:   false,
					},
				},
			},
		},
		Tools: []provider.Tool{
			{Name: "read", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		},
	}

	if _, streamErr := c.Stream(context.Background(), req); streamErr == nil {
		t.Fatalf("Stream against 500 server must return *provider.Error")
	}
	if len(gotBody) == 0 {
		t.Fatalf("server received no request body")
	}

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("payload not JSON: %v\nbody: %s", err, gotBody)
	}
	if payload.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want claude-haiku-4-5", payload.Model)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(payload.Messages))
	}

	asst := payload.Messages[0]
	if asst.Role != "assistant" {
		t.Errorf("messages[0].role = %q, want assistant", asst.Role)
	}
	if len(asst.Content) != 3 {
		t.Fatalf("assistant content blocks = %d, want 3", len(asst.Content))
	}

	var thinking struct {
		Type      string `json:"type"`
		Thinking  string `json:"thinking"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(asst.Content[0], &thinking); err != nil {
		t.Fatalf("thinking block not JSON: %v", err)
	}
	if thinking.Type != "thinking" || thinking.Thinking != "plan: read the file" || thinking.Signature != "sig-xyz" {
		t.Errorf("thinking block = %+v, want type=thinking thinking=%q signature=sig-xyz", thinking, "plan: read the file")
	}

	var text struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(asst.Content[1], &text); err != nil {
		t.Fatalf("text block not JSON: %v", err)
	}
	if text.Type != "text" || text.Text != "let me look" {
		t.Errorf("text block = %+v, want type=text text=%q", text, "let me look")
	}

	var toolUse struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(asst.Content[2], &toolUse); err != nil {
		t.Fatalf("tool_use block not JSON: %v", err)
	}
	if toolUse.Type != "tool_use" || toolUse.ID != "toolu_42" || toolUse.Name != "read" {
		t.Errorf("tool_use block = %+v, want type=tool_use id=toolu_42 name=read", toolUse)
	}
	if got := strings.Join(strings.Fields(string(toolUse.Input)), ""); got != `{"path":"/etc/hosts"}` {
		t.Errorf("tool_use input = %s, want {\"path\":\"/etc/hosts\"} (passthrough)", string(toolUse.Input))
	}

	user := payload.Messages[1]
	if user.Role != "user" {
		t.Errorf("messages[1].role = %q, want user", user.Role)
	}
	if len(user.Content) != 1 {
		t.Fatalf("user content blocks = %d, want 1", len(user.Content))
	}
	var toolResult struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error"`
	}
	if err := json.Unmarshal(user.Content[0], &toolResult); err != nil {
		t.Fatalf("tool_result block not JSON: %v", err)
	}
	if toolResult.Type != "tool_result" || toolResult.ToolUseID != "toolu_42" {
		t.Errorf("tool_result = %+v, want type=tool_result tool_use_id=toolu_42", toolResult)
	}
	if toolResult.Content != "127.0.0.1 localhost\n" {
		t.Errorf("tool_result content = %q, want passthrough of original string", toolResult.Content)
	}
	if toolResult.IsError {
		t.Errorf("tool_result is_error = true, want false (block had IsError=false)")
	}

	if len(payload.Tools) != 1 || payload.Tools[0].Name != "read" {
		t.Fatalf("tools = %+v, want one entry named read", payload.Tools)
	}
	if got := strings.Join(strings.Fields(string(payload.Tools[0].InputSchema)), ""); got != `{"type":"object","properties":{"path":{"type":"string"}}}` {
		t.Errorf("tool input_schema = %s, want passthrough of provided schema", payload.Tools[0].InputSchema)
	}
}

// TestR_2E6V_LAPQ_OneMillionContextSuffixGatesBetaHeader pins that
// a model ID carrying the documented [1m] suffix triggers
// 1M-context mode on the wire: the suffix is stripped from the
// `model` field sent to the Messages API, and the request carries
// the documented `anthropic-beta: context-1m-2025-08-07` header.
// A model without the suffix must NOT carry that header — the
// gate is keyed on the suffix, not on the backend.
//
// R-2E6V-LAPQ: 1M-context support. When a model ID carries the
// `[1m]` suffix the request is sent in 1M-context mode per
// Anthropic's current API conventions for that model.
func TestR_2E6V_LAPQ_OneMillionContextSuffixGatesBetaHeader(t *testing.T) {
	type captured struct {
		beta  string
		model string
	}

	run := func(t *testing.T, model string) captured {
		t.Helper()
		var got captured
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got.beta = r.Header.Get("anthropic-beta")
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(body, &payload)
			got.model = payload.Model
			http.Error(w, "stop", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c, err := New("sk-ant-test", model)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		c.baseURL = srv.URL
		if _, streamErr := c.Stream(context.Background(), provider.Request{Model: model}); streamErr == nil {
			t.Fatalf("Stream against 500 server must return *provider.Error")
		}
		return got
	}

	t.Run("with [1m] suffix", func(t *testing.T) {
		got := run(t, "claude-sonnet-4-6[1m]")
		if got.beta != "context-1m-2025-08-07" {
			t.Errorf("anthropic-beta = %q, want %q", got.beta, "context-1m-2025-08-07")
		}
		if got.model != "claude-sonnet-4-6" {
			t.Errorf("wire model = %q, want suffix stripped to %q", got.model, "claude-sonnet-4-6")
		}
	})

	t.Run("without suffix", func(t *testing.T) {
		got := run(t, "claude-haiku-4-5")
		if got.beta != "" {
			t.Errorf("anthropic-beta = %q for non-1M model; want empty", got.beta)
		}
		if got.model != "claude-haiku-4-5" {
			t.Errorf("wire model = %q, want %q", got.model, "claude-haiku-4-5")
		}
	})
}

// TestAnthropicSatisfiesProviderClient is a compile-time assertion
// that *Client implements provider.Client. Keeping it as a test
// rather than a `var _` line gives a named failure point.
func TestAnthropicSatisfiesProviderClient(t *testing.T) {
	var _ provider.Client = (*Client)(nil)
}

// TestR_92NN_7DNI_RawFlagBoundaries pins that when a Tracer is
// attached, the Anthropic client emits all three boundaries required
// by R-92NN-7DNI:
//
//  1. [stdin>] — every stdin event (logged by the caller via LogStdinEvent)
//  2. [llm>]  — every outbound HTTP request body+URL (LogRequest)
//  3. [<llm]  — every inbound HTTP response / SSE event (LogResponse / LogSSEPair)
//
// API key values must never appear in trace output.
func TestR_92NN_7DNI_RawFlagBoundaries(t *testing.T) {
	// Minimal SSE transcript that completes one turn.
	const sseBody = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	mkServer := func(t *testing.T) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(sseBody))
		}))
	}

	drain := func(ch <-chan provider.Event) {
		for range ch {
		}
	}

	// R-92NN-7DNI boundary 2: [llm>] appears for each outbound request.
	t.Run("llm_outbound_boundary", func(t *testing.T) {
		var traceBuf strings.Builder
		tr := trace.New(&traceBuf)
		srv := mkServer(t)
		defer srv.Close()

		c, _ := New("sk-test-r92nn", "claude-haiku-4-5")
		c.baseURL = srv.URL
		c.SetTracer(tr)

		ch, err := c.Stream(context.Background(), provider.Request{Model: "claude-haiku-4-5"})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		drain(ch)
		if !strings.Contains(traceBuf.String(), "[llm>]") {
			t.Errorf("trace missing [llm>] tag; trace=%q", traceBuf.String())
		}
	})

	// R-92NN-7DNI boundary 3: [<llm] appears for each inbound response.
	t.Run("llm_inbound_boundary", func(t *testing.T) {
		var traceBuf strings.Builder
		tr := trace.New(&traceBuf)
		srv := mkServer(t)
		defer srv.Close()

		c, _ := New("sk-test-r92nn", "claude-haiku-4-5")
		c.baseURL = srv.URL
		c.SetTracer(tr)

		ch, err := c.Stream(context.Background(), provider.Request{Model: "claude-haiku-4-5"})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		drain(ch)
		if !strings.Contains(traceBuf.String(), "[<llm]") {
			t.Errorf("trace missing [<llm] tag; trace=%q", traceBuf.String())
		}
	})

	// R-92NN-7DNI boundary 1: [stdin>] appears for each stdin event.
	t.Run("stdin_boundary", func(t *testing.T) {
		var traceBuf strings.Builder
		tr := trace.New(&traceBuf)
		tr.LogStdinEvent([]byte(`{"type":"user","message":{"role":"user","content":[]}}`))
		if !strings.Contains(traceBuf.String(), "[stdin>]") {
			t.Errorf("trace missing [stdin>] tag; trace=%q", traceBuf.String())
		}
	})

	// R-92NN-7DNI: API key values must never appear in trace output.
	t.Run("api_key_redacted", func(t *testing.T) {
		const secretKey = "sk-test-r92nn-secret-99999"
		var traceBuf strings.Builder
		tr := trace.New(&traceBuf, secretKey)
		srv := mkServer(t)
		defer srv.Close()

		c, _ := New(secretKey, "claude-haiku-4-5")
		c.baseURL = srv.URL
		c.SetTracer(tr)

		ch, _ := c.Stream(context.Background(), provider.Request{Model: "claude-haiku-4-5"})
		drain(ch)
		if strings.Contains(traceBuf.String(), secretKey) {
			t.Errorf("trace contains unredacted API key; trace=%q", traceBuf.String())
		}
	})
}
