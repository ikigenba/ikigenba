package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appkitdb "appkit/db"
	"appkit/server"

	"cron/internal/crontab"
	"cron/internal/db"
	"cron/internal/event"
)

func newHandler(t *testing.T) (http.Handler, *crontab.Store) {
	t.Helper()
	ctx := context.Background()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := crontab.NewStore(conn)
	var h http.Handler
	_, err = server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/cron",
		AuthServer: "https://auth.example.test",
		Version:    "0.1.0",
		Service:    "cron",
		Publishes:  event.Publishes(store),
		DB:         conn,
		Register: func(rt *server.Router) error {
			var err error
			h, err = NewHandler(store, rt)
			return err
		},
	})
	if err != nil {
		t.Fatalf("build test router: %v", err)
	}
	if h == nil {
		t.Fatalf("NewHandler returned nil handler")
	}
	return h, store
}

func rpc(t *testing.T, h http.Handler, method string, params any) map[string]any {
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
	req.Header.Set("X-Owner-Id", "owner-123")
	req.Header.Set("X-Client-Id", "client-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d", method, rec.Code)
	}

	var resp struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode rpc response: %v\nbody: %s", err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("%s returned JSON-RPC error: %v", method, resp.Error)
	}
	return resp.Result
}

// toolCall issues a tools/call and returns the complete tool result.
func toolCall(t *testing.T, h http.Handler, name string, args map[string]any) map[string]any {
	t.Helper()
	return rpc(t, h, "tools/call", map[string]any{"name": name, "arguments": args})
}

// call returns the machine-readable rendering and error flag used by most
// behavior tests.
func call(t *testing.T, h http.Handler, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	result := toolCall(t, h, name, args)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in result: %v", result)
	}
	payload, _ := result["structuredContent"].(map[string]any)
	if payload == nil {
		t.Fatalf("no structuredContent in result: %v", result)
	}
	isErr, _ := result["isError"].(bool)
	return payload, isErr
}

func TestToolsList_ComposesCronAndChassisTools(t *testing.T) {
	h, _ := newHandler(t)
	result := rpc(t, h, "tools/list", map[string]any{})
	tools, _ := result["tools"].([]any)
	// R-LS2J-73T5
	if len(tools) != 7 {
		t.Fatalf("tools/list returned %d tools, want exactly 7: %+v", len(tools), tools)
	}
	got := map[string]bool{}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		if got[name] {
			t.Fatalf("duplicate tool %q in tools/list: %+v", name, tools)
		}
		got[name] = true
		if tool["description"] == "" {
			t.Errorf("tool %q has an empty description", name)
		}
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema == nil || schema["type"] != "object" {
			t.Errorf("tool %q inputSchema is not an object schema: %v", name, tool["inputSchema"])
		}
	}
	for _, want := range []string{"create", "list", "get", "update", "delete", "health", "reflection"} {
		if !got[want] {
			t.Errorf("tools/list missing %q: %+v", want, tools)
		}
	}
	for name := range got {
		switch name {
		case "create", "list", "get", "update", "delete", "health", "reflection":
		default:
			t.Errorf("unexpected tool %q in tools/list: %+v", name, tools)
		}
	}
}

// R-6V3A-PW11
func TestToolsList_DomainOutputSchemasMatchResults(t *testing.T) {
	h, _ := newHandler(t)
	result := rpc(t, h, "tools/list", map[string]any{})
	tools, _ := result["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, raw := range tools {
		desc, _ := raw.(map[string]any)
		byName[desc["name"].(string)] = desc
	}

	for _, name := range []string{"create", "get", "update"} {
		schema, _ := byName[name]["outputSchema"].(map[string]any)
		assertObjectSchema(t, name, schema, []string{"name", "expr", "created_at", "updated_at", "last_slot"})
		props := schema["properties"].(map[string]any)
		for _, field := range []string{"name", "expr", "created_at", "updated_at"} {
			if got := props[field].(map[string]any)["type"]; got != "string" {
				t.Errorf("%s %s type = %v, want string", name, field, got)
			}
		}
		if got := props["last_slot"].(map[string]any)["type"]; !reflect.DeepEqual(got, []any{"string", "null"}) {
			t.Errorf("%s last_slot type = %#v, want [string null]", name, got)
		}
	}
	listSchema, _ := byName["list"]["outputSchema"].(map[string]any)
	assertObjectSchema(t, "list", listSchema, []string{"items"})
	items := listSchema["properties"].(map[string]any)["items"].(map[string]any)
	if items["type"] != "array" || items["items"] == nil {
		t.Errorf("list items schema = %v, want typed array with item schema", items)
	}
	deleteSchema, _ := byName["delete"]["outputSchema"].(map[string]any)
	assertObjectSchema(t, "delete", deleteSchema, []string{"ok"})
	if got := deleteSchema["properties"].(map[string]any)["ok"].(map[string]any)["type"]; got != "boolean" {
		t.Errorf("delete ok type = %v, want boolean", got)
	}
}

func assertObjectSchema(t *testing.T, name string, schema map[string]any, required []string) {
	t.Helper()
	if schema == nil || schema["type"] != "object" {
		t.Fatalf("%s outputSchema = %v, want non-nil object schema", name, schema)
	}
	got, _ := schema["required"].([]any)
	if len(got) != len(required) {
		t.Fatalf("%s required = %v, want %v", name, got, required)
	}
	for i, want := range required {
		if got[i] != want {
			t.Errorf("%s required[%d] = %v, want %q", name, i, got[i], want)
		}
	}
}

// R-6TVE-C4AC
func TestDomainTools_ReturnMatchingStructuredAndTextResults(t *testing.T) {
	h, _ := newHandler(t)
	tests := []struct {
		name  string
		args  map[string]any
		check func(*testing.T, map[string]any)
	}{
		{"create", map[string]any{"name": "nightly", "expr": "0 3 * * *"}, func(t *testing.T, got map[string]any) {
			if got["name"] != "nightly" || got["last_slot"] != nil {
				t.Fatalf("create structured result = %v", got)
			}
		}},
		{"list", map[string]any{}, func(t *testing.T, got map[string]any) {
			if _, ok := got["items"].([]any); !ok {
				t.Fatalf("list items = %#v, want array", got["items"])
			}
		}},
		{"get", map[string]any{"name": "nightly"}, func(t *testing.T, got map[string]any) {
			if got["name"] != "nightly" {
				t.Fatalf("get structured result = %v", got)
			}
		}},
		{"update", map[string]any{"name": "nightly", "expr": "15 4 * * *"}, func(t *testing.T, got map[string]any) {
			if got["expr"] != "15 4 * * *" {
				t.Fatalf("update structured result = %v", got)
			}
		}},
		{"delete", map[string]any{"name": "nightly"}, func(t *testing.T, got map[string]any) {
			if got["ok"] != true {
				t.Fatalf("delete structured result = %v", got)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toolCall(t, h, tt.name, tt.args)
			structured, _ := result["structuredContent"].(map[string]any)
			if structured == nil {
				t.Fatalf("missing structuredContent: %v", result)
			}
			content, _ := result["content"].([]any)
			if len(content) != 1 {
				t.Fatalf("content = %v, want one mirrored text block", content)
			}
			block, _ := content[0].(map[string]any)
			var mirrored map[string]any
			if err := json.Unmarshal([]byte(block["text"].(string)), &mirrored); err != nil {
				t.Fatalf("decode mirrored text: %v", err)
			}
			if !reflect.DeepEqual(mirrored, structured) {
				t.Fatalf("mirrored text = %#v, structuredContent = %#v", mirrored, structured)
			}
			tt.check(t, structured)
		})
	}
}

// TestCreate_RejectsBadExpr: the MCP boundary parses the expr and fails loudly,
// naming the bad field, before touching the store.
func TestCreate_RejectsBadExpr(t *testing.T) {
	h, store := newHandler(t)

	payload, isErr := call(t, h, "create", map[string]any{
		"name": "broken", "expr": "0 99 * * *", // hour 99 out of range
	})
	if !isErr {
		t.Fatalf("bad expr should be a tool error, got success: %v", payload)
	}
	// R-6WB7-3NRQ
	if payload["code"] != "validation" {
		t.Fatalf("wrong structured error code: %v", payload)
	}
	if msg, _ := payload["message"].(string); !strings.Contains(msg, "hour") {
		t.Fatalf("error message should name the bad field 'hour': %q", msg)
	}
	// Nothing must have been persisted.
	if _, err := store.Get(context.Background(), "broken"); err == nil {
		t.Fatalf("bad-expr row must not be persisted")
	}
}

// R-6XJ3-HFIF
func TestCreate_DuplicateNameReturnsConflict(t *testing.T) {
	h, _ := newHandler(t)
	args := map[string]any{"name": "nightly", "expr": "0 3 * * *"}
	if _, isErr := call(t, h, "create", args); isErr {
		t.Fatal("initial create failed")
	}
	payload, isErr := call(t, h, "create", args)
	if !isErr || payload["code"] != "conflict" {
		t.Fatalf("duplicate create = %v, isError=%v; want conflict", payload, isErr)
	}
}

// R-6YQZ-V794
func TestMissingScheduleReturnsNotFound(t *testing.T) {
	h, _ := newHandler(t)
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"get", map[string]any{"name": "missing"}},
		{"update", map[string]any{"name": "missing", "expr": "0 3 * * *"}},
		{"delete", map[string]any{"name": "missing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, isErr := call(t, h, tc.name, tc.args)
			if !isErr || payload["code"] != "not_found" {
				t.Fatalf("%s missing result = %v, isError=%v; want not_found", tc.name, payload, isErr)
			}
		})
	}
}

// R-6ZYW-8YZT
func TestCreate_InvalidNameReturnsValidation(t *testing.T) {
	h, _ := newHandler(t)
	payload, isErr := call(t, h, "create", map[string]any{"name": "Bad Name", "expr": "0 3 * * *"})
	if !isErr || payload["code"] != "validation" {
		t.Fatalf("invalid name result = %v, isError=%v; want validation", payload, isErr)
	}
	if msg, _ := payload["message"].(string); !strings.Contains(msg, "constraint") {
		t.Fatalf("invalid-name message should identify the constraint: %q", msg)
	}
}

// R-716S-MQQI
func TestNoRetiredJSONResultInProductionSource(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")
	needle := "JSON" + "Result"
	for _, dir := range []string{"internal", "cmd"} {
		root := filepath.Join(moduleRoot, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(body, []byte(needle)) {
				t.Errorf("%s contains retired result helper token", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

// TestCreate_WrongFieldCount also fails at the boundary.
func TestCreate_WrongFieldCount(t *testing.T) {
	h, _ := newHandler(t)
	payload, isErr := call(t, h, "create", map[string]any{
		"name": "short", "expr": "* * *",
	})
	if !isErr {
		t.Fatalf("expected validation error, got %v", payload)
	}
}

// TestCreateThenListGet: a valid expr round-trips through the store and the live
// family appears in reflection.
func TestCreateThenListGet(t *testing.T) {
	h, _ := newHandler(t)
	if _, isErr := call(t, h, "create", map[string]any{
		"name": "nightly", "expr": "0 3 * * *",
	}); isErr {
		t.Fatalf("valid create should succeed")
	}

	list, isErr := call(t, h, "list", map[string]any{})
	if isErr {
		t.Fatalf("list errored")
	}
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %v", list)
	}
	get, isErr := call(t, h, "get", map[string]any{"name": "nightly"})
	if isErr {
		t.Fatalf("get errored")
	}
	if get["name"] != "nightly" || get["expr"] != "0 3 * * *" {
		t.Fatalf("get returned wrong entry: %v", get)
	}

	refl, isErr := call(t, h, "reflection", map[string]any{})
	if isErr {
		t.Fatalf("reflection errored")
	}
	pubs, _ := refl["publishes"].([]any)
	if len(pubs) != 1 {
		t.Fatalf("want 1 published family, got %v", refl["publishes"])
	}
	first, _ := pubs[0].(map[string]any)
	if first["kind"] != event.Kind || first["subject"] != "/<schedule name>" || !strings.Contains(first["description"].(string), "nightly") {
		t.Fatalf("wrong published family: %v", first)
	}
}

// R-PRP2-GJP7
func TestReflectionPublishesOneLiveTickFamily(t *testing.T) {
	h, _ := newHandler(t)
	for _, name := range []string{"nightly", "bill-sweep"} {
		if _, isErr := call(t, h, "create", map[string]any{"name": name, "expr": "0 3 * * *"}); isErr {
			t.Fatalf("create %q failed", name)
		}
	}
	index, isErr := call(t, h, "reflection", map[string]any{})
	if isErr {
		t.Fatal("reflection index errored")
	}
	families, _ := index["publishes"].([]any)
	if len(families) != 1 {
		t.Fatalf("families = %v, want exactly one", index["publishes"])
	}
	family, _ := families[0].(map[string]any)
	description, _ := family["description"].(string)
	if family["kind"] != event.Kind || family["subject"] != "/<schedule name>" || !strings.Contains(description, "bill-sweep") || !strings.Contains(description, "nightly") {
		t.Fatalf("unexpected reflected family: %v", family)
	}
	detail, isErr := call(t, h, "reflection", map[string]any{"kind": event.Kind})
	if isErr {
		t.Fatal("reflection detail errored")
	}
	schema, _ := detail["schema"].(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	example, _ := detail["example"].(map[string]any)
	for _, name := range []string{"name", "scheduled_for", "fired_at"} {
		if properties[name] == nil || example[name] == nil {
			t.Fatalf("schema/example disagree or omit %q: schema=%v example=%v", name, schema, example)
		}
	}
	if _, isErr := call(t, h, "delete", map[string]any{"name": "nightly"}); isErr {
		t.Fatal("delete nightly failed")
	}
	after, isErr := call(t, h, "reflection", map[string]any{})
	if isErr {
		t.Fatal("reflection after delete errored")
	}
	families, _ = after["publishes"].([]any)
	if len(families) != 1 {
		t.Fatalf("families after delete = %v, want exactly one", after["publishes"])
	}
	description, _ = families[0].(map[string]any)["description"].(string)
	if strings.Contains(description, "nightly") || !strings.Contains(description, "bill-sweep") {
		t.Fatalf("live description after delete = %q", description)
	}
}
