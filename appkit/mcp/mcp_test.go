package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"appkit"
	"appkit/mcp"
	"appkit/server"

	"eventplane/consumer"
	"eventplane/outbox"
)

func newHandler(t *testing.T, opts mcp.Options) *mcp.Handler {
	t.Helper()
	h, err := mcp.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func rpc(t *testing.T, h http.Handler, body string, headers map[string]string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, rr.Body.String())
	}
	return resp
}

func resultObject(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing or not object: %#v", resp["result"])
	}
	return result
}

func errorObject(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("error missing or not object: %#v", resp)
	}
	if _, hasResult := resp["result"]; hasResult {
		t.Fatalf("response has result despite error: %#v", resp)
	}
	return errObj
}

func resultText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result := resultObject(t, resp)
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v, want one text item", result["content"])
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] not object: %#v", content[0])
	}
	if item["type"] != "text" {
		t.Fatalf("content[0].type = %v, want text", item["type"])
	}
	text, ok := item["text"].(string)
	if !ok {
		t.Fatalf("content[0].text missing or not string: %#v", item)
	}
	return text
}

func resultTextJSON(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(resultText(t, resp)), &v); err != nil {
		t.Fatalf("decode text JSON: %v", err)
	}
	return v
}

func normalizeJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var normalized any
	if err := json.Unmarshal(b, &normalized); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return normalized
}

func toolsByName(t *testing.T, resp map[string]any) map[string]map[string]any {
	t.Helper()
	result := resultObject(t, resp)
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools missing or not array: %#v", result["tools"])
	}
	byName := map[string]map[string]any{}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("tool item not object: %#v", item)
		}
		name, _ := tool["name"].(string)
		byName[name] = tool
	}
	return byName
}

type staticEventSample struct {
	StaticID string `json:"static_id"`
}

type liveEventSample struct {
	LiveID string `json:"live_id"`
}

type customerEventSample struct {
	CustomerID string `json:"customer_id"`
	Email      string `json:"email,omitempty"`
}

func TestInitializeReturnsOptions(t *testing.T) {
	h := newHandler(t, mcp.Options{
		Service:      "ledger",
		Version:      "v1.2.3",
		Instructions: "Use ledger tools for accounting records.",
	})

	resp := rpc(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, nil)
	result := resultObject(t, resp)
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo missing or not object: %#v", result["serverInfo"])
	}

	// R-MCJJ-NXJR
	if serverInfo["name"] != "ledger" {
		t.Errorf("serverInfo.name = %v, want ledger", serverInfo["name"])
	}
	if serverInfo["version"] != "v1.2.3" {
		t.Errorf("serverInfo.version = %v, want v1.2.3", serverInfo["version"])
	}
	if result["instructions"] != "Use ledger tools for accounting records." {
		t.Errorf("instructions = %v, want Options.Instructions", result["instructions"])
	}
}

func TestToolsListIncludesDeclaredTools(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
		"required": []string{"query"},
	}
	h := newHandler(t, mcp.Options{
		Tools: []mcp.Tool{
			{
				Name:        "search",
				Description: "Search records.",
				InputSchema: schema,
				Handler: func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
					return mcp.TextResult("ok"), nil
				},
			},
			{
				Name:        "save",
				Description: "Save records.",
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
				Handler: func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
					return mcp.TextResult("ok"), nil
				},
			},
		},
	})

	resp := rpc(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, nil)
	byName := toolsByName(t, resp)

	// R-MDRG-1PAG
	gotSearch, ok := byName["search"]
	if !ok {
		t.Fatalf("search descriptor missing from %#v", byName)
	}
	if gotSearch["description"] != "Search records." {
		t.Errorf("search description = %v, want exact declared description", gotSearch["description"])
	}
	if !reflect.DeepEqual(gotSearch["inputSchema"], normalizeJSON(t, schema)) {
		t.Errorf("search schema = %#v, want %#v", gotSearch["inputSchema"], normalizeJSON(t, schema))
	}
	gotSave, ok := byName["save"]
	if !ok {
		t.Fatalf("save descriptor missing from %#v", byName)
	}
	if gotSave["description"] != "Save records." {
		t.Errorf("save description = %v, want exact declared description", gotSave["description"])
	}
}

func TestStandardToolsListAndHealthEnvelope(t *testing.T) {
	h := newHandler(t, mcp.Options{
		Service: "crm",
		Version: "v2.4.6",
		Health: func(ctx context.Context) (map[string]any, error) {
			return map[string]any{"queue": 3}, nil
		},
	})

	listResp := rpc(t, h, `{"jsonrpc":"2.0","id":"standard-list","method":"tools/list"}`, nil)
	byName := toolsByName(t, listResp)

	// R-ML2U-CBQM
	if _, ok := byName["health"]; !ok {
		t.Fatalf("health descriptor missing from zero-declared tool list: %#v", byName)
	}
	if _, ok := byName["reflection"]; !ok {
		t.Fatalf("reflection descriptor missing from zero-declared tool list: %#v", byName)
	}

	callResp := rpc(t, h, `{"jsonrpc":"2.0","id":"health","method":"tools/call","params":{"name":"health"}}`, nil)
	got := resultTextJSON(t, callResp)
	wantEnvelope := appkit.Envelope("v2.4.6", "crm", map[string]any{"queue": 3})
	for _, key := range []string{"status", "service", "version"} {
		if got[key] != wantEnvelope[key] {
			t.Fatalf("health %s = %v, want %v", key, got[key], wantEnvelope[key])
		}
	}
	details, ok := got["details"].(map[string]any)
	if !ok {
		t.Fatalf("health details missing or not object: %#v", got["details"])
	}
	if details["queue"] != float64(3) {
		t.Fatalf("health details.queue = %v, want 3", details["queue"])
	}
}

func TestReflectionReturnsEventIndexAndLivePublishes(t *testing.T) {
	staticEvents := outbox.Registry{{
		Type:        "static.only",
		Description: "Static-only event.",
		Sample:      staticEventSample{StaticID: "s1"},
	}}
	liveEvents := outbox.Registry{{
		Type:        "live.only",
		Description: "Live-only event.",
		Sample:      liveEventSample{LiveID: "l1"},
	}}
	subscriptions := []consumer.Subscription{{
		Source:      "crm",
		Filter:      "customer.*",
		Description: "Customer changes.",
	}}
	h := newHandler(t, mcp.Options{
		Events: staticEvents,
		Publishes: func() outbox.Registry {
			return liveEvents
		},
		Subscriptions: func() []consumer.Subscription {
			return subscriptions
		},
	})

	resp := rpc(t, h, `{"jsonrpc":"2.0","id":"reflection-index","method":"tools/call","params":{"name":"reflection"}}`, nil)
	got := resultTextJSON(t, resp)

	// R-MMAQ-Q3HB
	if !reflect.DeepEqual(got["publishes"], normalizeJSON(t, liveEvents.Index())) {
		t.Fatalf("publishes = %#v, want live provider index %#v", got["publishes"], normalizeJSON(t, liveEvents.Index()))
	}
	publishesJSON, err := json.Marshal(got["publishes"])
	if err != nil {
		t.Fatalf("marshal publishes: %v", err)
	}
	if !strings.Contains(string(publishesJSON), "live.only") {
		t.Fatalf("publishes missing live-only type: %s", publishesJSON)
	}
	if strings.Contains(string(publishesJSON), "static.only") {
		t.Fatalf("publishes included static-only type despite live provider: %s", publishesJSON)
	}
	wantSubscribes := []map[string]any{{
		"source":      "crm",
		"filter":      "customer.*",
		"description": "Customer changes.",
	}}
	if !reflect.DeepEqual(got["subscribes"], normalizeJSON(t, wantSubscribes)) {
		t.Fatalf("subscribes = %#v, want %#v", got["subscribes"], normalizeJSON(t, wantSubscribes))
	}
}

func TestReflectionReturnsEventDetailSchema(t *testing.T) {
	events := outbox.Registry{{
		Type:        "crm.customer.created",
		Description: "A customer was created.",
		Sample:      customerEventSample{CustomerID: "cus_123", Email: "owner@example.com"},
	}}
	h := newHandler(t, mcp.Options{Events: events})

	resp := rpc(t, h, `{"jsonrpc":"2.0","id":"reflection-detail","method":"tools/call","params":{"name":"reflection","arguments":{"event_type":"crm.customer.created"}}}`, nil)
	got := resultTextJSON(t, resp)
	want, err := events.Detail("crm.customer.created")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}

	// R-MNIN-3V80
	if !reflect.DeepEqual(got, normalizeJSON(t, want)) {
		t.Fatalf("detail = %#v, want %#v", got, normalizeJSON(t, want))
	}
	detailJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	if !strings.Contains(string(detailJSON), "customer_id") {
		t.Fatalf("detail payload fields missing from %s", detailJSON)
	}
}

func TestReflectionUnknownEventTypeReturnsToolError(t *testing.T) {
	events := outbox.Registry{
		{
			Type:        "crm.customer.created",
			Description: "A customer was created.",
			Sample:      customerEventSample{CustomerID: "cus_123"},
		},
		{
			Type:        "crm.customer.deleted",
			Description: "A customer was deleted.",
			Sample:      customerEventSample{CustomerID: "cus_123"},
		},
	}
	h := newHandler(t, mcp.Options{Events: events})

	resp := rpc(t, h, `{"jsonrpc":"2.0","id":"reflection-unknown","method":"tools/call","params":{"name":"reflection","arguments":{"event_type":"crm.customer.missing"}}}`, nil)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("unknown event_type returned JSON-RPC error %#v, want tool result", errObj)
	}
	result := resultObject(t, resp)

	// R-MOQJ-HMYP
	if result["isError"] != true {
		t.Fatalf("isError = %v, want true in tool result %#v", result["isError"], result)
	}
	msg := resultText(t, resp)
	if !strings.Contains(msg, "crm.customer.created") || !strings.Contains(msg, "crm.customer.deleted") {
		t.Fatalf("tool error message %q does not name known event types", msg)
	}
}

func TestToolsCallDispatchesRawArgumentsAndResult(t *testing.T) {
	var gotArgs json.RawMessage
	wantResult := map[string]any{
		"content": []map[string]any{{"type": "text", "text": "created"}},
		"meta":    map[string]any{"id": "abc123"},
	}
	h := newHandler(t, mcp.Options{
		Tools: []mcp.Tool{{
			Name:        "create",
			Description: "Create a record.",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				gotArgs = append(json.RawMessage(nil), args...)
				return wantResult, nil
			},
		}},
	})

	resp := rpc(t, h, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create","arguments":{"alpha":1,"nested":true}}}`, nil)

	// R-MEZC-FH15
	if string(gotArgs) != `{"alpha":1,"nested":true}` {
		t.Fatalf("handler args = %s, want raw arguments bytes", gotArgs)
	}
	if !reflect.DeepEqual(resp["result"], normalizeJSON(t, wantResult)) {
		t.Fatalf("result = %#v, want handler map %#v", resp["result"], normalizeJSON(t, wantResult))
	}
}

func TestToolsCallPassesRequestIdentityHeaders(t *testing.T) {
	var gotID server.Identity
	h := newHandler(t, mcp.Options{
		Tools: []mcp.Tool{{
			Name:        "whoami",
			Description: "Return caller identity.",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				gotID = id
				return mcp.TextResult("ok"), nil
			},
		}},
	})

	rpc(t, h, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"whoami","arguments":{}}}`, map[string]string{
		"X-Owner-Email": "owner@example.com",
		"X-Client-Id":   "client-123",
	})

	// R-MG78-T8RU
	if gotID.OwnerEmail != "owner@example.com" {
		t.Errorf("OwnerEmail = %q, want request X-Owner-Email", gotID.OwnerEmail)
	}
	if gotID.ClientID != "client-123" {
		t.Errorf("ClientID = %q, want request X-Client-Id", gotID.ClientID)
	}
}

func TestErrorsForUnknownMethodAndUndeclaredTool(t *testing.T) {
	h := newHandler(t, mcp.Options{})

	unknownMethod := rpc(t, h, `{"jsonrpc":"2.0","id":"bad-method","method":"missing"}`, nil)
	methodErr := errorObject(t, unknownMethod)

	// R-MHF5-70IJ
	if methodErr["code"] != float64(-32601) {
		t.Fatalf("unknown method code = %v, want -32601", methodErr["code"])
	}
	unknownTool := rpc(t, h, `{"jsonrpc":"2.0","id":"bad-tool","method":"tools/call","params":{"name":"absent","arguments":{}}}`, nil)
	toolErr := errorObject(t, unknownTool)
	if _, ok := toolErr["code"]; !ok {
		t.Fatalf("undeclared tool error missing code: %#v", toolErr)
	}
	if toolErr["message"] == "" {
		t.Fatalf("undeclared tool error missing message: %#v", toolErr)
	}
}

func TestMalformedBodyReturnsParseError(t *testing.T) {
	h := newHandler(t, mcp.Options{})

	resp := rpc(t, h, `not json`, nil)
	errObj := errorObject(t, resp)

	// R-MIN1-KS98
	if errObj["code"] != float64(-32700) {
		t.Fatalf("malformed body code = %v, want -32700", errObj["code"])
	}
}

func TestNewRejectsDuplicateAndReservedToolNames(t *testing.T) {
	handler := func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
		return nil, errors.New("unused")
	}

	// R-MJUX-YJZX
	if _, err := mcp.New(mcp.Options{Tools: []mcp.Tool{
		{Name: "dupe", Handler: handler},
		{Name: "dupe", Handler: handler},
	}}); err == nil {
		t.Fatal("New duplicate tool names error = nil, want non-nil")
	}
	if _, err := mcp.New(mcp.Options{Tools: []mcp.Tool{{Name: "health", Handler: handler}}}); err == nil {
		t.Fatal("New health reserved name error = nil, want non-nil")
	}
	if _, err := mcp.New(mcp.Options{Tools: []mcp.Tool{{Name: "reflection", Handler: handler}}}); err == nil {
		t.Fatal("New reflection reserved name error = nil, want non-nil")
	}
}
