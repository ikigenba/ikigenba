package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appkitdb "appkit/db"
	"appkit/server"

	"crm/internal/crm"
	"crm/internal/db"
)

// newTestHandler builds a Handler over a real crm.Service backed by a fresh,
// migrated temp-file SQLite database (the same approach as crm/store_test.go's
// newTestStore). Outbox is left nil — Save/Log are fully functional without
// event emission (the Phase 4 seam in service.go is guarded), so the tool
// surface is exercised end-to-end with a deterministic clock.
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crm_test.db")
	conn, err := appkitdb.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migs); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	// Deterministic, monotonically-increasing clock so updated_at ordering is
	// stable.
	tick := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	svc := crm.NewService(conn)
	svc.Now = func() time.Time {
		tick = tick.Add(time.Millisecond)
		return tick
	}
	var captured *server.Router
	_, err = server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/crm",
		AuthServer: "https://auth.example.test",
		Version:    testVersion,
		Service:    testService,
		Events:     crm.Events,
		DB:         conn,
		Register: func(rt *server.Router) error {
			captured = rt
			return nil
		},
	})
	if err != nil {
		t.Fatalf("build test router: %v", err)
	}
	if captured == nil {
		t.Fatalf("server.New did not invoke Register")
	}
	h, err := NewHandler(svc, captured)
	if err != nil {
		t.Fatalf("build mcp handler: %v", err)
	}
	return h
}

// jsonRPCResponse is the wire shape we decode tool responses out of. result is
// the MCP tools/call result ({content:[{type,text}], isError?}).
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolResult struct {
	IsError           bool            `json:"isError"`
	StructuredContent map[string]any  `json:"structuredContent"`
	LegacyError       json.RawMessage `json:"error"`
	Content           []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type toolDescriptor struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  map[string]any  `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

const (
	testOwner    = "owner@example.com"
	testClientID = "client-123"
	testVersion  = "test-1.2.3"
	testService  = "crm"
)

// rpc drives a single JSON-RPC request through the real ServeHTTP seam with the
// nginx-injected identity headers set, and returns the decoded response.
func rpc(t *testing.T, h http.Handler, method string, params any) jsonRPCResponse {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(raw))
	req.Header.Set("X-Owner-Id", testOwner)
	req.Header.Set("X-Owner-Email", testOwner)
	req.Header.Set("X-Client-Id", testClientID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response for %s: %v (body=%s)", method, err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("%s returned JSON-RPC error: %d %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp
}

// call drives a tools/call and decodes the inner tool result.
func call(t *testing.T, h http.Handler, name string, args any) toolResult {
	t.Helper()
	resp := rpc(t, h, "tools/call", map[string]any{"name": name, "arguments": args})
	var tr toolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("decode tool result for %s: %v (result=%s)", name, err, resp.Result)
	}
	return tr
}

// callOK asserts the success envelope and that its machine-readable and agent
// text renderings carry the same JSON value.
func callOK(t *testing.T, h http.Handler, name string, args any) map[string]any {
	t.Helper()
	tr := call(t, h, name, args)
	if tr.IsError {
		t.Fatalf("%s unexpectedly returned an error envelope: %s", name, payloadText(tr))
	}
	if len(tr.Content) != 1 || tr.Content[0].Type != "text" {
		t.Fatalf("%s: expected one text content block, got %+v", name, tr.Content)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &m); err != nil {
		t.Fatalf("%s: text payload is not a JSON object: %v (%s)", name, err, tr.Content[0].Text)
	}
	if tr.StructuredContent == nil {
		t.Fatalf("%s: result is missing structuredContent", name)
	}
	if !reflect.DeepEqual(tr.StructuredContent, m) {
		t.Fatalf("%s: structuredContent differs from text JSON: structured=%v text=%v", name, tr.StructuredContent, m)
	}
	return tr.StructuredContent
}

// callErr asserts the structured MCP error shape and returns its {code,message}
// object.
func callErr(t *testing.T, h http.Handler, name string, args any) map[string]any {
	t.Helper()
	tr := call(t, h, name, args)
	if !tr.IsError {
		t.Fatalf("%s: expected an error envelope, got success: %s", name, payloadText(tr))
	}
	if tr.StructuredContent == nil {
		t.Fatalf("%s: error result missing structuredContent: %+v", name, tr)
	}
	if len(tr.LegacyError) != 0 {
		t.Fatalf("%s: result retained legacy top-level error: %s", name, tr.LegacyError)
	}
	if len(tr.Content) != 1 || tr.Content[0].Type != "text" {
		t.Fatalf("%s: expected one text content block, got %+v", name, tr.Content)
	}
	if tr.StructuredContent["message"] != tr.Content[0].Text {
		t.Fatalf("%s: error text does not mirror structured message: %+v", name, tr)
	}
	return tr.StructuredContent
}

func payloadText(tr toolResult) string {
	if len(tr.Content) == 0 {
		return "<no content>"
	}
	return tr.Content[0].Text
}

func toolsList(t *testing.T, h http.Handler) []toolDescriptor {
	t.Helper()
	resp := rpc(t, h, "tools/list", nil)
	var result struct {
		Tools []toolDescriptor `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	return result.Tools
}

func requireTool(t *testing.T, tools []toolDescriptor, name string) toolDescriptor {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing expected tool %q in %+v", name, tools)
	return toolDescriptor{}
}

// id pulls the "id" string off a save summary / get card.
func id(t *testing.T, m map[string]any) string {
	t.Helper()
	v, ok := m["id"].(string)
	if !ok || v == "" {
		t.Fatalf("expected a non-empty string id, got %v", m["id"])
	}
	return v
}

// TestToolsList asserts tools/list returns EXACTLY the eight
// verbs (count and names), each with the required descriptor keys.
func TestToolsList(t *testing.T) {
	h := newTestHandler(t)
	resp := rpc(t, h, "tools/list", nil)

	var result struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	want := []string{
		"search", "get", "save",
		"delete", "log", "health",
		"reflection", "guide",
	}
	// R-MW1X-S9EV
	// R-PGF0-9CS1
	if len(result.Tools) != len(want) {
		t.Fatalf("expected exactly %d tools, got %d: %+v", len(want), len(result.Tools), result.Tools)
	}
	got := map[string]bool{}
	for _, tool := range result.Tools {
		got[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %q has an empty description", tool.Name)
		}
		if tool.InputSchema == nil || tool.InputSchema["type"] != "object" {
			t.Errorf("tool %q inputSchema is not an object schema: %v", tool.Name, tool.InputSchema)
		}
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing expected tool %q", name)
		}
	}
	for _, name := range []string{"search", "get", "save", "delete", "log", "guide"} {
		if !got[name] {
			t.Errorf("missing crm-declared tool %q", name)
		}
	}
	for _, name := range []string{"health", "reflection"} {
		if !got[name] {
			t.Errorf("missing chassis tool %q", name)
		}
	}
}

func TestToolsListDomainOutputSchemas(t *testing.T) {
	h := newTestHandler(t)
	tools := toolsList(t, h)

	// R-5Y60-E30A
	for _, name := range []string{"search", "get", "save", "delete", "log", "guide"} {
		desc := requireTool(t, tools, name)
		if name == "guide" {
			if len(desc.OutputSchema) != 0 {
				t.Errorf("guide must omit outputSchema, got %s", desc.OutputSchema)
			}
			continue
		}
		if len(desc.OutputSchema) == 0 {
			t.Errorf("domain tool %q is missing outputSchema", name)
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(desc.OutputSchema, &schema); err != nil {
			t.Errorf("domain tool %q outputSchema is invalid JSON: %v", name, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("domain tool %q outputSchema is not an object schema: %v", name, schema)
		}
	}
}

func TestDomainToolSuccessResultsAreStructured(t *testing.T) {
	h := newTestHandler(t)
	org := callOK(t, h, "save", map[string]any{
		"type":   "organization",
		"fields": map[string]any{"name": "Structured Corp"},
	})
	orgID := id(t, org)
	deletable := callOK(t, h, "save", map[string]any{
		"type":   "task",
		"fields": map[string]any{"title": "Delete me"},
	})

	// R-5ZDW-RUQZ
	tests := []struct {
		name  string
		args  map[string]any
		shape func(*testing.T, map[string]any)
	}{
		{"search", map[string]any{}, func(t *testing.T, got map[string]any) {
			if len(got) != 2 || got["items"] == nil || got["next_cursor"] == nil {
				t.Fatalf("search result has wrong shape: %+v", got)
			}
		}},
		{"get", map[string]any{"id": orgID}, func(t *testing.T, got map[string]any) {
			if got["id"] != orgID || got["type"] != "organization" {
				t.Fatalf("get result has wrong card shape: %+v", got)
			}
		}},
		{"save", map[string]any{"type": "task", "fields": map[string]any{"title": "Structured save"}}, requireSummaryShape},
		{"delete", map[string]any{"type": "task", "id": id(t, deletable)}, func(t *testing.T, got map[string]any) {
			if len(got) != 1 || got["ok"] != true {
				t.Fatalf("delete result is not exactly {ok:true}: %+v", got)
			}
		}},
		{"log", map[string]any{"subject_id": orgID, "kind": "note", "body": "structured"}, requireSummaryShape},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callOK(t, h, tt.name, tt.args)
			tt.shape(t, got)
		})
	}
}

func requireSummaryShape(t *testing.T, got map[string]any) {
	t.Helper()
	for _, key := range []string{"id", "type", "label", "updated_at"} {
		if value, ok := got[key].(string); !ok || value == "" {
			t.Errorf("summary field %q is missing or empty: %+v", key, got)
		}
	}
}

func TestInitializeInstructionsDescribeDiscoveryFlow(t *testing.T) {
	h := newTestHandler(t)
	resp := rpc(t, h, "initialize", nil)

	var result struct {
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}

	// R-PDZ7-HTAN
	for _, want := range []string{"companies", "people", "guide"} {
		if !strings.Contains(result.Instructions, want) {
			t.Fatalf("initialize instructions missing %q: %q", want, result.Instructions)
		}
	}
	if !strings.Contains(result.Instructions, "pipeline") &&
		!strings.Contains(result.Instructions, "opportunities") {
		t.Fatalf("initialize instructions missing pipeline/opportunities wording: %q", result.Instructions)
	}
}

func TestToolsListSaveDescriptionMovesFieldCatalogToGuide(t *testing.T) {
	h := newTestHandler(t)
	tools := toolsList(t, h)
	save := requireTool(t, tools, "save")

	// R-PF73-VL1C
	for _, forbidden := range []string{"Fields by type", "given_name", "amount_cents"} {
		if strings.Contains(save.Description, forbidden) {
			t.Fatalf("save description still contains field catalog text %q: %q", forbidden, save.Description)
		}
	}
	for _, want := range []string{"force", "emails", "[] to clear", "use log"} {
		if !strings.Contains(save.Description, want) {
			t.Fatalf("save description missing %q: %q", want, save.Description)
		}
	}
}

func TestGuideToolReturnsEmbeddedUsageGuide(t *testing.T) {
	h := newTestHandler(t)
	tr := call(t, h, "guide", map[string]any{})

	// R-PIUT-0W9F
	if tr.IsError {
		t.Fatalf("guide returned an error envelope: %s", payloadText(tr))
	}
	if len(tr.Content) != 1 || tr.Content[0].Type != "text" {
		t.Fatalf("guide expected one text content block, got %+v", tr.Content)
	}
	text := tr.Content[0].Text
	for _, want := range []string{"given_name", "amount_cents", "stage", `"name":"save"`, `"name":"log"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("guide text missing %q: %s", want, text)
		}
	}
}

func TestGuideInputSchemaHasNoRequiredFields(t *testing.T) {
	h := newTestHandler(t)
	guide := requireTool(t, toolsList(t, h), "guide")

	// R-PK2P-EO04
	if guide.Description == "" {
		t.Fatalf("guide description is empty")
	}
	if guide.InputSchema["type"] != "object" {
		t.Fatalf("guide inputSchema is not an object schema: %v", guide.InputSchema)
	}
	if required, ok := guide.InputSchema["required"]; ok {
		t.Fatalf("guide inputSchema should not declare required fields, got %v", required)
	}
}

func TestGuideDocumentsAdvancedUsage(t *testing.T) {
	h := newTestHandler(t)
	tr := call(t, h, "guide", map[string]any{})
	if tr.IsError {
		t.Fatalf("guide returned an error envelope: %s", payloadText(tr))
	}
	text := tr.Content[0].Text

	// R-PLAL-SFQT
	for _, want := range []string{
		"Dedup and `force`",
		"Set replacement",
		"Deal `status`",
		"Filtered search",
		"Correcting an interaction",
		"delete",
		"log",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("guide advanced section missing %q: %s", want, text)
		}
	}
}

func TestGuideToolAcceptsOmittedArguments(t *testing.T) {
	h := newTestHandler(t)
	resp := rpc(t, h, "tools/call", map[string]any{"name": "guide"})

	var tr toolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("decode guide result: %v (result=%s)", err, resp.Result)
	}

	// R-PMII-67HI
	if tr.IsError {
		t.Fatalf("guide with omitted arguments returned an error envelope: %s", payloadText(tr))
	}
	if text := payloadText(tr); !strings.Contains(text, "CRM usage guide") {
		t.Fatalf("guide with omitted arguments returned unexpected text: %s", text)
	}
}

// TestToolsCallVerbs exercises every verb against the real service: create one
// of each entity type via crm_save, get a card, search, log an interaction, and
// delete (then confirm the subsequent get errors).
func TestToolsCallVerbs(t *testing.T) {
	h := newTestHandler(t)

	// health — the gated health envelope plus identity, no inputs.
	health := callOK(t, h, "health", map[string]any{})
	if health["status"] != "ok" {
		t.Fatalf("health status not ok: %+v", health)
	}
	if health["version"] != testVersion || health["service"] != testService {
		t.Fatalf("health version/service mismatch: %+v", health)
	}
	if health["owner_email"] != testOwner || health["client_id"] != testClientID {
		t.Fatalf("health identity mismatch: %+v", health)
	}
	// crm supplies no reporter, so details is always present and empty.
	details, ok := health["details"].(map[string]any)
	if !ok {
		t.Fatalf("health details missing or wrong type: %+v", health["details"])
	}
	if len(details) != 0 {
		t.Fatalf("expected empty details (no reporter), got %+v", details)
	}

	// crm_save — one of each type. Assert the success summary envelope.
	org := callOK(t, h, "save", map[string]any{
		"type":   "organization",
		"fields": map[string]any{"name": "Acme", "domain": "acme.com"},
	})
	if org["type"] != "organization" || org["label"] != "Acme" {
		t.Fatalf("org save summary: %+v", org)
	}
	orgID := id(t, org)

	contact := callOK(t, h, "save", map[string]any{
		"type": "contact",
		"fields": map[string]any{
			"display_name": "Bob",
			"org_id":       orgID,
			"emails":       []map[string]any{{"email": "Bob@Example.com"}},
		},
	})
	if contact["type"] != "contact" || contact["label"] != "Bob" {
		t.Fatalf("contact save summary: %+v", contact)
	}
	contactID := id(t, contact)

	deal := callOK(t, h, "save", map[string]any{
		"type": "deal",
		"fields": map[string]any{
			"name":     "Acme Renewal",
			"org_id":   orgID,
			"stage":    "proposal",
			"contacts": []map[string]any{{"id": contactID, "role": "champion"}},
		},
	})
	if deal["type"] != "deal" || deal["label"] != "Acme Renewal" {
		t.Fatalf("deal save summary: %+v", deal)
	}
	dealID := id(t, deal)

	task := callOK(t, h, "save", map[string]any{
		"type":   "task",
		"fields": map[string]any{"title": "Follow up", "contact_id": contactID},
	})
	if task["type"] != "task" {
		t.Fatalf("task save summary: %+v", task)
	}
	taskID := id(t, task)

	// crm_get — fetch the contact card; assert self fields + attached relations.
	card := callOK(t, h, "get", map[string]any{"id": contactID})
	if card["type"] != "contact" || card["display_name"] != "Bob" {
		t.Fatalf("contact card self fields: %+v", card)
	}
	// Email is normalized to lowercase on the card.
	emails, ok := card["emails"].([]any)
	if !ok || len(emails) != 1 {
		t.Fatalf("contact card emails: %v", card["emails"])
	}
	if e := emails[0].(map[string]any); e["email"] != "bob@example.com" {
		t.Fatalf("email not lowercased on card: %v", e["email"])
	}
	if card["organization"] == nil {
		t.Fatalf("expected organization relation on contact card: %+v", card)
	}

	// crm_log — append an interaction against the contact subject.
	logged := callOK(t, h, "log", map[string]any{
		"subject_id": contactID,
		"kind":       "call",
		"body":       "Discussed renewal.",
	})
	if logged["type"] != "interaction" {
		t.Fatalf("log summary: %+v", logged)
	}
	interactionID := id(t, logged)

	// The interaction shows up on the contact card's recent interactions.
	card = callOK(t, h, "get", map[string]any{"id": contactID})
	ints, ok := card["recent_interactions"].([]any)
	if !ok || len(ints) != 1 {
		t.Fatalf("expected one recent interaction, got %v", card["recent_interactions"])
	}

	// crm_search — unscoped finds entities; scoped + query narrows.
	searchAll := callOK(t, h, "search", map[string]any{})
	items, ok := searchAll["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("unscoped search returned nothing: %+v", searchAll)
	}
	orgHits := callOK(t, h, "search", map[string]any{"type": "organization", "query": "acme"})
	hits, ok := orgHits["items"].([]any)
	if !ok || len(hits) != 1 {
		t.Fatalf("scoped org search: %+v", orgHits)
	}
	if first := hits[0].(map[string]any); first["id"] != orgID {
		t.Fatalf("scoped org search wrong hit: %+v", first)
	}
	// Search scoped to interactions by subject_id filter.
	intHits := callOK(t, h, "search", map[string]any{
		"type":    "interaction",
		"filters": map[string]any{"subject_id": contactID},
	})
	if hits, _ := intHits["items"].([]any); len(hits) != 1 {
		t.Fatalf("interaction subject_id search: %+v", intHits)
	}

	// crm_delete — delete the deal; subsequent get errors not_found.
	delOK := callOK(t, h, "delete", map[string]any{"type": "deal", "id": dealID})
	if delOK["ok"] != true {
		t.Fatalf("delete ok envelope: %+v", delOK)
	}
	notFound := callErr(t, h, "get", map[string]any{"id": dealID})
	if notFound["code"] != "not_found" {
		t.Fatalf("expected not_found after delete, got %+v", notFound)
	}

	// Delete the remaining entities (task, interaction, contact, org) to confirm
	// every type routes through delete.
	for _, d := range []struct{ typ, did string }{
		{"task", taskID},
		{"interaction", interactionID},
		{"contact", contactID},
		{"organization", orgID},
	} {
		if r := callOK(t, h, "delete", map[string]any{"type": d.typ, "id": d.did}); r["ok"] != true {
			t.Fatalf("delete %s: %+v", d.typ, r)
		}
	}
}

func TestDomainToolErrorsUseClosedVocabulary(t *testing.T) {
	h := newTestHandler(t)
	bogusID := "01GHOSTGHOSTGHOSTGHOSTGHOST"
	allowed := map[any]bool{
		"validation": true, "not_found": true, "conflict": true,
		"too_large": true, "source_unavailable": true, "internal": true,
	}

	// R-60LT-5MHO
	tests := []struct {
		name string
		args map[string]any
	}{
		{"search", map[string]any{"type": "widget"}},
		{"get", map[string]any{"id": bogusID}},
		{"save", map[string]any{"type": "widget"}},
		{"delete", map[string]any{"type": "organization", "id": bogusID}},
		{"log", map[string]any{"subject_id": bogusID, "kind": "note"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callErr(t, h, tt.name, tt.args)
			if !allowed[got["code"]] {
				t.Fatalf("%s emitted code outside the closed vocabulary: %+v", tt.name, got)
			}
			if got["code"] == "duplicate" {
				t.Fatalf("%s emitted retired duplicate code: %+v", tt.name, got)
			}
			if len(got) != 2 || got["message"] == "" {
				t.Fatalf("%s error is not exactly {code,message}: %+v", tt.name, got)
			}
		})
	}
}

func TestSaveValidationErrorsAreTyped(t *testing.T) {
	h := newTestHandler(t)

	// R-65HE-OPGG
	for _, tt := range []struct {
		name string
		args map[string]any
	}{
		{"derived status", map[string]any{"type": "deal", "fields": map[string]any{"name": "Big Deal", "status": "won"}}},
		{"missing required field", map[string]any{"type": "organization", "fields": map[string]any{}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := callErr(t, h, "save", tt.args)
			if got["code"] != "validation" {
				t.Fatalf("expected validation code, got %+v", got)
			}
		})
	}
}

func TestGetAndDeleteMissingRowsAreNotFound(t *testing.T) {
	h := newTestHandler(t)
	bogusID := "01GHOSTGHOSTGHOSTGHOSTGHOST"

	// R-631L-X5Z2
	for _, tt := range []struct {
		name string
		args map[string]any
	}{
		{"get", map[string]any{"id": bogusID}},
		{"delete", map[string]any{"type": "organization", "id": bogusID}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := callErr(t, h, tt.name, tt.args)
			if got["code"] != "not_found" {
				t.Fatalf("expected not_found, got %+v", got)
			}
		})
	}
}

func TestDuplicateSaveIsConflictWithExistingID(t *testing.T) {
	h := newTestHandler(t)
	first := callOK(t, h, "save", map[string]any{
		"type": "contact",
		"fields": map[string]any{
			"display_name": "Bob",
			"emails":       []map[string]any{{"email": "bob@example.com"}},
		},
	})
	firstID := id(t, first)

	// R-61TP-JE8D
	dup := callErr(t, h, "save", map[string]any{
		"type": "contact",
		"fields": map[string]any{
			"display_name": "Robert",
			"emails":       []map[string]any{{"email": "Bob@Example.com"}},
		},
	})
	if dup["code"] != "conflict" {
		t.Fatalf("expected conflict code, got %+v", dup)
	}
	msg, _ := dup["message"].(string)
	if !strings.Contains(msg, "existing_id="+firstID) {
		t.Fatalf("conflict message does not name existing row %s: %+v", firstID, dup)
	}

	// force:true on the same call now succeeds and creates a distinct contact.
	forced := callOK(t, h, "save", map[string]any{
		"type":  "contact",
		"force": true,
		"fields": map[string]any{
			"display_name": "Robert",
			"emails":       []map[string]any{{"email": "bob@example.com"}},
		},
	})
	if forcedID := id(t, forced); forcedID == firstID {
		t.Fatalf("force should create a new contact, got same id %s", forcedID)
	}
}

// R-8IP7-FWJ5
// TestToolsCallReflection covers the reflection tool: the no-arg index (the
// four published families, empty subscribes — crm is a producer), the kind
// detail (schema + example), and the corrective error for an unknown kind.
func TestToolsCallReflection(t *testing.T) {
	h := newTestHandler(t)

	// No-arg → the index {publishes, subscribes}.
	idx := callOK(t, h, "reflection", map[string]any{})

	publishes, ok := idx["publishes"].([]any)
	if !ok {
		t.Fatalf("reflection index missing publishes array: %+v", idx)
	}
	got := map[string]bool{}
	for _, p := range publishes {
		m := p.(map[string]any)
		if m["description"] == "" {
			t.Errorf("published kind %v has empty description", m["kind"])
		}
		if m["subject"] != "/<contact id>" {
			t.Errorf("published subject = %q, want /<contact id>", m["subject"])
		}
		got[m["kind"].(string)] = true
	}
	for _, want := range []string{"contact.created", "contact.updated", "contact.tagged", "contact.untagged"} {
		if !got[want] {
			t.Errorf("reflection publishes missing %q: %+v", want, publishes)
		}
	}
	if len(publishes) != 4 {
		t.Fatalf("expected exactly 4 published kinds, got %d: %+v", len(publishes), publishes)
	}

	// crm is a producer: subscribes is present and empty.
	subscribes, ok := idx["subscribes"].([]any)
	if !ok {
		t.Fatalf("reflection index missing subscribes array: %+v", idx)
	}
	if len(subscribes) != 0 {
		t.Fatalf("expected empty subscribes for crm, got %+v", subscribes)
	}

	// kind → the publish detail (schema + example).
	detail := callOK(t, h, "reflection", map[string]any{"kind": "contact.created"})
	if detail["kind"] != "contact.created" || detail["subject"] != "/<contact id>" {
		t.Fatalf("detail addressing mismatch: %+v", detail)
	}
	if detail["description"] == "" {
		t.Fatalf("detail missing description: %+v", detail)
	}
	sch, ok := detail["schema"].(map[string]any)
	if !ok || sch["type"] != "object" {
		t.Fatalf("detail schema not an object schema: %+v", detail["schema"])
	}
	props, ok := sch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("detail schema missing properties: %+v", sch)
	}
	example, ok := detail["example"].(map[string]any)
	if !ok {
		t.Fatalf("detail missing example object: %+v", detail["example"])
	}
	for field := range example {
		if _, ok := props[field]; !ok {
			t.Errorf("example field %q absent from schema: %+v", field, sch)
		}
	}

	// Unknown kind -> corrective error listing valid kinds.
	bad := call(t, h, "reflection", map[string]any{"kind": "contact.nope"})
	if !bad.IsError {
		t.Fatalf("expected reflection unknown event_type to return an error envelope: %s", payloadText(bad))
	}
	msg := payloadText(bad)
	if !strings.Contains(msg, "contact.nope") {
		t.Fatalf("corrective message missing unknown type: %q", msg)
	}
	for _, want := range []string{"contact.created", "contact.updated", "contact.tagged", "contact.untagged"} {
		if !strings.Contains(msg, want) {
			t.Errorf("corrective message missing valid type %q: %q", want, msg)
		}
	}
}
